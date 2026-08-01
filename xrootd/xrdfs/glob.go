// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdfs

import (
	"bytes"
	"context"
	"io/fs"
	"path"
	"slices"
	"strings"
)

// Match reports whether name matches pattern in full.
//
// A shell's fnmatch is no use over a namespace: its "*" swallows separators, so
// "/store/*/f.root" would match three levels down and "**" would mean nothing
// in particular. Here the semantics are the ones [path/filepath.Match] and
// pathlib agree on, because that is what somebody writing "/store/mc/**/*.root"
// means:
//
//   - "*" and "?" stay inside one path component,
//   - "**" crosses them,
//   - "[abc]" is a character class, "[a-z]" a range, and "[!abc]" its negation,
//   - an unclosed "[" is a literal one.
//
// Unlike [path/filepath.Match], a malformed pattern is not an error: there is
// nothing a caller could do about it that the pattern could not say for itself.
func Match(pattern, name string) bool {
	return match([]byte(pattern), []byte(name))
}

// match is the matcher itself: pattern and text, one byte at a time.
//
// Recursive descent rather than a compiled automaton — a pattern is a handful
// of components and a namespace listing is a network round trip, so the cost
// that matters is not here.
func match(pattern, text []byte) bool {
	switch {
	case bytes.HasPrefix(pattern, []byte("**/")):
		// "**/" is zero or more directories, which is the one place a pattern
		// can consume a separator.
		rest := pattern[3:]
		if match(rest, text) {
			return true
		}
		for i := range text {
			if text[i] == '/' && match(rest, text[i+1:]) {
				return true
			}
		}
		return false
	case bytes.HasPrefix(pattern, []byte("**")):
		rest := pattern[2:]
		for i := 0; i <= len(text); i++ {
			if match(rest, text[i:]) {
				return true
			}
		}
		return false
	case len(pattern) == 0:
		return len(text) == 0
	}

	head, tail := pattern[0], pattern[1:]
	switch head {
	case '*':
		// Everything but a separator, so a "*" cannot escape its component.
		bound := bytes.IndexByte(text, '/')
		if bound < 0 {
			bound = len(text)
		}
		for i := 0; i <= bound; i++ {
			if match(tail, text[i:]) {
				return true
			}
		}
		return false
	case '?':
		return len(text) > 0 && text[0] != '/' && match(tail, text[1:])
	case '[':
		set, negated, n, ok := class(pattern)
		if !ok {
			// An unclosed bracket is a literal one, as fnmatch has it.
			return len(text) > 0 && text[0] == '[' && match(tail, text[1:])
		}
		return len(text) > 0 && text[0] != '/' &&
			inClass(set, text[0]) != negated && match(pattern[n:], text[1:])
	default:
		return len(text) > 0 && text[0] == head && match(tail, text[1:])
	}
}

// class returns the body of a character class at the head of pattern, whether
// it is negated, and how many bytes it occupies. ok is false when pattern does
// not start with a class at all.
func class(pattern []byte) (set []byte, negated bool, n int, ok bool) {
	// A "]" at the head of the body is a literal one, so the class "[]]" ends
	// at its second bracket.
	end := bytes.IndexByte(pattern, ']')
	if end <= 1 {
		return nil, false, 0, false
	}
	negated = pattern[1] == '!' || pattern[1] == '^'
	start := 1
	if negated {
		start = 2
	}
	return pattern[start:end], negated, end + 1, true
}

// inClass reports whether b is in a class body, ranges included.
func inClass(set []byte, b byte) bool {
	for i := 0; i < len(set); {
		// "a-z" is a range; a "-" at either end is itself.
		if i+2 < len(set) && set[i+1] == '-' {
			if set[i] <= b && b <= set[i+2] {
				return true
			}
			i += 3
			continue
		}
		if set[i] == b {
			return true
		}
		i++
	}
	return false
}

// HasMagic reports whether pattern has anything in it to match, as opposed to
// naming one path.
func HasMagic(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}

// literalPrefix is the deepest directory of pattern that contains no wildcard.
//
// This is what makes a glob affordable: only the directories a pattern can
// reach are listed, so "/store/mc/**/*.root" never asks about "/store/data".
func literalPrefix(pattern string) string {
	parts := strings.Split(pattern, "/")
	// The last component is the name being matched, not a directory to enter.
	parts = parts[:len(parts)-1]
	var kept []string
	for _, part := range parts {
		if HasMagic(part) {
			break
		}
		kept = append(kept, part)
	}
	if joined := strings.Join(kept, "/"); joined != "" {
		return joined
	}
	return "/"
}

// needsWalk reports whether matching pattern needs a walk rather than one
// listing. A pattern whose only magic is in its last component —
// "/store/*.root" — is answered by listing one directory; anything else
// crosses directories.
func needsWalk(pattern string) bool {
	if strings.Contains(pattern, "**") {
		return true
	}
	if i := strings.LastIndexByte(pattern, '/'); i >= 0 {
		return HasMagic(pattern[:i])
	}
	return false
}

// splitOpaque separates a path from the opaque data appended to it. Opaque data
// is not part of a name — it carries the token a request is authorized with —
// so it is neither matched against nor reported back as part of a path, but it
// does have to travel with every request a walk makes.
func splitOpaque(p string) (base, opaque string) {
	base, cgi, found := strings.Cut(p, "?")
	if !found {
		return base, ""
	}
	return base, "?" + cgi
}

// WalkFunc is called by [Walk] for every entry it visits, and once more for
// every directory whose listing failed. The error a call returns controls the
// walk: [io/fs.SkipDir] on a directory skips its contents, on anything else the
// rest of the directory holding it, [io/fs.SkipAll] ends the walk, and any
// other error ends it and is returned by Walk.
type WalkFunc func(path string, entry EntryStat, err error) error

// Walk visits every entry under root, depth first, calling fn for each one in
// lexical order. Root itself is visited first, as [path/filepath.WalkDir]
// visits it.
//
// A directory that cannot be listed is reported to fn a second time, with the
// error and without descending, rather than aborting the walk: a namespace with
// mixed permissions is the normal case, and it is the caller who knows whether
// one unreadable directory makes the answer wrong.
//
// Opaque data on root — the "?authz=..." an authorized request travels with —
// is carried into every listing below it and reported back in none of the
// paths, so a walk of a token-authorized namespace stays authorized.
func Walk(ctx context.Context, fsys FileSystem, root string, fn WalkFunc) error {
	root, cgi := splitOpaque(root)
	root = path.Clean(root)
	info, err := fsys.Stat(ctx, root+cgi)
	switch {
	case err != nil:
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		err = fn(root, EntryStat{EntryName: path.Base(root)}, err)
	default:
		info.EntryName = path.Base(root)
		err = walk(ctx, fsys, root, cgi, info, fn)
	}
	if err == fs.SkipDir || err == fs.SkipAll {
		return nil
	}
	return err
}

func walk(ctx context.Context, fsys FileSystem, name, cgi string, info EntryStat, fn WalkFunc) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := fn(name, info, nil); err != nil {
		if err == fs.SkipDir && info.IsDir() {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}

	entries, err := fsys.Dirlist(ctx, name+cgi)
	if err != nil {
		// A cancelled walk is not a namespace with an unreadable directory in
		// it: every listing from here on would fail the same way, and reporting
		// each one as a directory that could not be read would turn "stop" into
		// a walk of the whole tree.
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		// The second call names the directory again, so that a caller can tell
		// "this is a directory" from "and it could not be read".
		if err := fn(name, info, err); err != nil && err != fs.SkipDir {
			return err
		}
		return nil
	}
	// The listing belongs to whoever answered it, so the order this walk wants
	// is imposed on a copy of it.
	entries = slices.Clone(entries)
	slices.SortFunc(entries, func(a, b EntryStat) int { return strings.Compare(a.EntryName, b.EntryName) })

	for _, entry := range entries {
		child := path.Join(name, entry.EntryName)
		if !entry.HasStatInfo {
			// A server that answered without stat information leaves no way to
			// tell a directory from a file except by asking about it.
			if st, err := fsys.Stat(ctx, child+cgi); err == nil {
				st.EntryName = entry.EntryName
				entry = st
			}
		}
		if err := walk(ctx, fsys, child, cgi, entry, fn); err != nil {
			if err == fs.SkipDir {
				break
			}
			return err
		}
	}
	return nil
}

// Glob returns the paths matching pattern, sorted, with absolute paths out.
//
// Only the directories a pattern can actually reach are listed: the literal
// prefix is walked, not the whole namespace, so Glob(ctx, fs,
// "/store/mc/**/*.root") never asks about "/store/data". Directories match as
// well as files.
//
// Opaque data on the pattern is carried into every request and reported back in
// none of the paths, exactly as it is by [Walk].
//
// As [path/filepath.Glob] does, Glob ignores the failure to read a directory:
// the only error it returns is the context's, since a namespace with mixed
// permissions is the normal case and a caller who needs to be told about them
// can [Walk] instead.
func Glob(ctx context.Context, fsys FileSystem, pattern string) ([]string, error) {
	return GlobFrom(ctx, fsys, "/", pattern)
}

// GlobFrom is [Glob] with a relative pattern taken from root. An absolute
// pattern is used as it stands, so root is a default rather than a boundary.
func GlobFrom(ctx context.Context, fsys FileSystem, root, pattern string) ([]string, error) {
	target := pattern
	if !strings.HasPrefix(pattern, "/") {
		base, cgi := splitOpaque(root)
		target = strings.TrimRight(base, "/") + "/" + pattern + cgi
	}
	// The opaque data travels with every request this glob makes, and with none
	// of the paths it reports: it is the token the namespace is read with, not
	// part of what anything is called.
	target, cgi := splitOpaque(target)
	if !HasMagic(target) {
		// A pattern with nothing to match is a question about one path, and
		// answering it by listing its parent would be a wasted round trip.
		if _, err := fsys.Stat(ctx, target+cgi); err != nil {
			return nil, ctx.Err()
		}
		return []string{target}, nil
	}

	start := literalPrefix(target)
	var out []string
	collect := func(name string, entry EntryStat, err error) error {
		if err != nil {
			// Whatever could not be read holds nothing that matches, as far as
			// this glob is concerned.
			return nil
		}
		if name != start && Match(target, name) {
			out = append(out, name)
		}
		return nil
	}

	if !needsWalk(target) {
		// The magic is in the last component only, so one listing answers it.
		entries, err := fsys.Dirlist(ctx, start+cgi)
		if err != nil {
			return nil, ctx.Err()
		}
		for _, entry := range entries {
			collect(path.Join(start, entry.EntryName), entry, nil)
		}
	} else if err := Walk(ctx, fsys, start+cgi, collect); err != nil {
		return nil, err
	}

	slices.Sort(out)
	return out, nil
}

// RGlob is [GlobFrom] with pattern matched at any depth below root:
// RGlob(ctx, fs, "/store", "*.root") is Glob(ctx, fs, "/store/**/*.root").
func RGlob(ctx context.Context, fsys FileSystem, root, pattern string) ([]string, error) {
	return GlobFrom(ctx, fsys, root, "**/"+pattern)
}
