// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for namespace globbing and walking.
//
// A glob over a namespace is not a glob over a directory: every component of
// the pattern that is not answered from a listing already in hand costs a
// network round trip, and a pattern read with a shell's fnmatch would ask for
// all of them. Two properties matter and are pinned here — that "*" stays
// inside one path component while "**" crosses them, and that a glob lists only
// the directories its pattern can actually reach.

package xrdfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"testing"
)

// globFS is a namespace built from a list of file paths: the directories are
// the ones the paths imply. It records every listing it is asked for, which is
// what makes the pruning of a glob observable.
type globFS struct {
	dirs   map[string][]string // directory -> the names it holds
	files  map[string]bool
	asked  []string          // every path Dirlist was called with, in order
	fail   map[string]error  // directories that refuse to be listed
	blind  bool              // answer listings without stat information
	stats  []string          // every path Stat was called with, in order
	listed func(path string) // called once a listing has been answered
}

func newGlobFS(paths ...string) *globFS {
	g := &globFS{
		dirs:  map[string][]string{"/": nil},
		files: make(map[string]bool),
		fail:  make(map[string]error),
	}
	for _, p := range paths {
		g.files[p] = true
		for dir, name := path.Split(p); ; dir, name = path.Split(dir) {
			dir = path.Clean(dir)
			name = strings.TrimSuffix(name, "/")
			if !slices.Contains(g.dirs[dir], name) {
				g.dirs[dir] = append(g.dirs[dir], name)
			}
			if dir == "/" {
				break
			}
			if _, ok := g.dirs[dir]; !ok {
				g.dirs[dir] = nil
			}
		}
	}
	return g
}

func (g *globFS) Stat(ctx context.Context, p string) (EntryStat, error) {
	g.stats = append(g.stats, p)
	if err := ctx.Err(); err != nil {
		return EntryStat{}, err
	}
	p, _, _ = strings.Cut(p, "?")
	es := EntryStat{EntryName: path.Base(p), HasStatInfo: true}
	switch {
	case g.files[p]:
		return es, nil
	default:
		if _, ok := g.dirs[p]; !ok {
			return EntryStat{}, fmt.Errorf("%q: %w", p, os.ErrNotExist)
		}
		es.Flags = StatIsDir
		return es, nil
	}
}

func (g *globFS) Dirlist(ctx context.Context, p string) ([]EntryStat, error) {
	g.asked = append(g.asked, p)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p, _, _ = strings.Cut(p, "?")
	if err := g.fail[p]; err != nil {
		return nil, err
	}
	names, ok := g.dirs[p]
	if !ok {
		return nil, fmt.Errorf("%q: %w", p, os.ErrNotExist)
	}
	// Reverse lexical order: a walk that reports its entries in order has to
	// sort them itself rather than trusting the server to have done it.
	sorted := slices.Clone(names)
	slices.Sort(sorted)
	slices.Reverse(sorted)

	out := make([]EntryStat, 0, len(sorted))
	for _, name := range sorted {
		es := EntryStat{EntryName: name, HasStatInfo: !g.blind}
		if _, isDir := g.dirs[path.Join(p, name)]; isDir && !g.blind {
			es.Flags = StatIsDir
		}
		out = append(out, es)
	}
	if g.listed != nil {
		g.listed(p)
	}
	return out, nil
}

// The rest of the interface is not reachable from a walk; a call to any of it
// is a bug in the code under test rather than something to answer.
func (g *globFS) Open(context.Context, string, OpenMode, OpenOptions) (File, error) {
	panic("xrdfs: a glob opened a file")
}
func (g *globFS) RemoveFile(context.Context, string) error { panic("xrdfs: a glob removed a file") }
func (g *globFS) Truncate(context.Context, string, int64) error {
	panic("xrdfs: a glob truncated a file")
}
func (g *globFS) VirtualStat(context.Context, string) (VirtualFSStat, error) {
	panic("xrdfs: a glob asked for the virtual filesystem")
}
func (g *globFS) Mkdir(context.Context, string, OpenMode) error {
	panic("xrdfs: a glob made a directory")
}
func (g *globFS) MkdirAll(context.Context, string, OpenMode) error {
	panic("xrdfs: a glob made a directory")
}
func (g *globFS) RemoveDir(context.Context, string) error { panic("xrdfs: a glob removed a directory") }
func (g *globFS) RemoveAll(context.Context, string) error { panic("xrdfs: a glob removed a directory") }
func (g *globFS) Rename(context.Context, string, string) error {
	panic("xrdfs: a glob renamed an entry")
}
func (g *globFS) Chmod(context.Context, string, OpenMode) error {
	panic("xrdfs: a glob changed permissions")
}
func (g *globFS) Statx(context.Context, []string) ([]StatFlags, error) {
	panic("xrdfs: a glob used statx")
}

var _ FileSystem = (*globFS)(nil)

// TestConformance_AStarStaysInsideOneComponent is the difference between a
// namespace glob and fnmatch: a "*" that swallowed separators would make
// "/store/*/f.root" match three levels down, and every pattern would have to be
// answered by listing the whole namespace.
func TestConformance_AStarStaysInsideOneComponent(t *testing.T) {
	for _, tc := range []struct {
		pattern, name string
		want          bool
	}{
		{"/store/*.root", "/store/data.root", true},
		{"/store/*.root", "/store/mc/data.root", false},
		{"/store/*/f.root", "/store/mc/f.root", true},
		{"/store/*/f.root", "/store/mc/2024/f.root", false},
		{"/store/*/f.root", "/store/f.root", false},
		{"*", "name", true},
		{"*", "a/b", false},
		{"f*", "f", true},
		{"*.root", "", false},
		{"/store/?.root", "/store/a.root", true},
		{"/store/?.root", "/store/ab.root", false},
		{"/a/?/b", "/a//b", false},
		{"/store/data.root", "/store/data.root", true},
		{"/store/data.root", "/store/data.roo", false},
		{"", "", true},
		{"", "a", false},
	} {
		t.Run(fmt.Sprintf("%s vs %s", tc.pattern, tc.name), func(t *testing.T) {
			if got := Match(tc.pattern, tc.name); got != tc.want {
				t.Fatalf("Match(%q, %q) is %v, want %v", tc.pattern, tc.name, got, tc.want)
			}
		})
	}
}

// TestConformance_ADoubleStarCrossesComponents pins the one place a pattern may
// consume a separator. "**/" is zero or more directories, so a pattern written
// against a namespace that is one level deeper than expected still matches.
func TestConformance_ADoubleStarCrossesComponents(t *testing.T) {
	for _, tc := range []struct {
		pattern, name string
		want          bool
	}{
		{"/store/**/f.root", "/store/f.root", true},
		{"/store/**/f.root", "/store/mc/f.root", true},
		{"/store/**/f.root", "/store/mc/2024/f.root", true},
		{"/store/**/f.root", "/data/f.root", false},
		{"/store/**/f.root", "/store/mc/g.root", false},
		{"/store/**", "/store/mc/2024/f.root", true},
		{"/store/**", "/store", false},
		{"**/*.root", "/store/mc/f.root", true},
		{"/store/**/*.root", "/store/f.root", true},
		{"/store/**/*.root", "/store/mc/2024/f.root", true},
		{"/store/**/*.root", "/store/mc/2024/f.txt", false},
		// A "**" that is not a whole component is still a "**": it matches
		// whatever is left, separators included, up to what follows it.
		{"/store/**x", "/store/ax", true},
		{"/store/**x", "/store/a/b/cx", true},
		{"/store/**x", "/store/y", false},
	} {
		t.Run(fmt.Sprintf("%s vs %s", tc.pattern, tc.name), func(t *testing.T) {
			if got := Match(tc.pattern, tc.name); got != tc.want {
				t.Fatalf("Match(%q, %q) is %v, want %v", tc.pattern, tc.name, got, tc.want)
			}
		})
	}
}

// TestConformance_ACharacterClassSelectsOneCharacter covers "[...]", including
// the two rules that are easy to get wrong: a class never matches a separator,
// and a bracket that is never closed is a literal bracket rather than an error.
func TestConformance_ACharacterClassSelectsOneCharacter(t *testing.T) {
	for _, tc := range []struct {
		pattern, name string
		want          bool
	}{
		{"[abc].root", "a.root", true},
		{"[abc].root", "d.root", false},
		{"[a-z].root", "q.root", true},
		{"[a-z].root", "Q.root", false},
		{"[0-9a-f]x", "ex", true},
		{"[0-9a-f]x", "gx", false},
		{"[!abc].root", "d.root", true},
		{"[!abc].root", "a.root", false},
		{"[^abc].root", "d.root", true},
		{"[!abc]", "/", false},
		{"[a-]x", "-x", true},
		{"[-a]x", "-x", true},
		{"/a/[bc]/f", "/a/b/f", true},
		{"/a/[bc]/f", "/a/d/f", false},
		// An unclosed bracket is a literal one, as fnmatch has it.
		{"[abc", "[abc", true},
		{"[abc", "a", false},
		{"a[b", "a[b", true},
		// A "]" straight after the bracket cannot close an empty class.
		{"[]x", "[]x", true},
	} {
		t.Run(fmt.Sprintf("%s vs %s", tc.pattern, tc.name), func(t *testing.T) {
			if got := Match(tc.pattern, tc.name); got != tc.want {
				t.Fatalf("Match(%q, %q) is %v, want %v", tc.pattern, tc.name, got, tc.want)
			}
		})
	}
}

// TestConformance_TheLiteralPrefixBoundsTheSearch pins the two decisions that
// turn a pattern into a plan: where to start looking, and whether one listing
// answers it.
func TestConformance_TheLiteralPrefixBoundsTheSearch(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		prefix  string
		walk    bool
		magic   bool
	}{
		{"/store/mc/**/*.root", "/store/mc", true, true},
		{"/store/*.root", "/store", false, true},
		{"/store/*/f.root", "/store", true, true},
		{"/store/data/f.root", "/store/data", false, false},
		{"/**/f.root", "/", true, true},
		{"/store/**", "/store", true, true},
		{"*.root", "/", false, true},
		{"/a/b/c/*.root", "/a/b/c", false, true},
		{"/a/[0-9]/f", "/a", true, true},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			if got := literalPrefix(tc.pattern); got != tc.prefix {
				t.Errorf("literalPrefix is %q, want %q", got, tc.prefix)
			}
			if got := needsWalk(tc.pattern); got != tc.walk {
				t.Errorf("needsWalk is %v, want %v", got, tc.walk)
			}
			if got := HasMagic(tc.pattern); got != tc.magic {
				t.Errorf("HasMagic is %v, want %v", got, tc.magic)
			}
		})
	}
}

// theTree is a namespace with two top-level branches, so that a glob confined
// to one of them can be seen not to have asked about the other.
func theTree() *globFS {
	return newGlobFS(
		"/store/mc/2023/a.root",
		"/store/mc/2024/b.root",
		"/store/mc/2024/c.txt",
		"/store/mc/README",
		"/store/data/2024/d.root",
		"/store/data/notes.txt",
		"/tmp/e.root",
	)
}

// TestConformance_AGlobNeverListsWhatItCannotMatch is the property that makes
// globbing a namespace affordable at all. Walking from "/" would be correct and
// useless: at a real storage element it is minutes of listings for an answer
// that the pattern says cannot be there.
func TestConformance_AGlobNeverListsWhatItCannotMatch(t *testing.T) {
	fsys := theTree()
	got, err := Glob(context.Background(), fsys, "/store/mc/**/*.root")
	if err != nil {
		t.Fatalf("could not glob: %v", err)
	}
	want := []string{"/store/mc/2023/a.root", "/store/mc/2024/b.root"}
	if !slices.Equal(got, want) {
		t.Fatalf("the glob matched %q, want %q", got, want)
	}
	for _, asked := range fsys.asked {
		if !strings.HasPrefix(asked, "/store/mc") {
			t.Errorf("the glob listed %q, which no path under it can match the pattern", asked)
		}
	}
	if !slices.Contains(fsys.asked, "/store/mc/2024") {
		t.Errorf("the glob never listed /store/mc/2024, and it asked for %q", fsys.asked)
	}
}

// TestConformance_MagicInTheLastComponentIsOneListing: "/store/*.root" names
// the directory it searches, so descending into it would be a round trip spent
// on entries that cannot match.
func TestConformance_MagicInTheLastComponentIsOneListing(t *testing.T) {
	fsys := theTree()
	got, err := Glob(context.Background(), fsys, "/store/*")
	if err != nil {
		t.Fatalf("could not glob: %v", err)
	}
	// Directories match as well as files, which is what pathlib does.
	want := []string{"/store/data", "/store/mc"}
	if !slices.Equal(got, want) {
		t.Fatalf("the glob matched %q, want %q", got, want)
	}
	if want := []string{"/store"}; !slices.Equal(fsys.asked, want) {
		t.Fatalf("the glob listed %q, want just %q", fsys.asked, want)
	}
}

// TestConformance_APatternWithNothingToMatchIsOnePath: a caller who passes a
// plain path is asking whether it exists, and listing its parent to find out
// would be a round trip spent on the rest of the directory.
func TestConformance_APatternWithNothingToMatchIsOnePath(t *testing.T) {
	t.Run("a path that is there", func(t *testing.T) {
		fsys := theTree()
		got, err := Glob(context.Background(), fsys, "/store/mc/README")
		if err != nil {
			t.Fatalf("could not glob: %v", err)
		}
		if want := []string{"/store/mc/README"}; !slices.Equal(got, want) {
			t.Fatalf("the glob matched %q, want %q", got, want)
		}
		if len(fsys.asked) != 0 {
			t.Fatalf("the glob listed %q to answer a question about one path", fsys.asked)
		}
	})

	t.Run("a path that is not", func(t *testing.T) {
		fsys := theTree()
		got, err := Glob(context.Background(), fsys, "/store/mc/nothing")
		if err != nil {
			t.Fatalf("a path that does not exist is no match, not a failure: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("the glob matched %q, want nothing", got)
		}
	})
}

// TestConformance_AGlobIsSortedAndDeduplicated: a walk descends in whatever
// order the listings arrive in, and a caller comparing two runs of the same
// glob should not be made to see that.
func TestConformance_AGlobIsSorted(t *testing.T) {
	got, err := Glob(context.Background(), theTree(), "/store/**/*.root")
	if err != nil {
		t.Fatalf("could not glob: %v", err)
	}
	want := []string{
		"/store/data/2024/d.root",
		"/store/mc/2023/a.root",
		"/store/mc/2024/b.root",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("the glob matched %q, want %q", got, want)
	}
	if !slices.IsSorted(got) {
		t.Fatal("the matches came back out of order")
	}
}

// TestConformance_ARelativePatternIsTakenFromTheRoot, and an absolute one is
// used as it stands: the root is a default, not a boundary.
func TestConformance_ARelativePatternIsTakenFromTheRoot(t *testing.T) {
	ctx := context.Background()

	t.Run("relative", func(t *testing.T) {
		got, err := GlobFrom(ctx, theTree(), "/store/mc", "*/b.root")
		if err != nil {
			t.Fatalf("could not glob: %v", err)
		}
		if want := []string{"/store/mc/2024/b.root"}; !slices.Equal(got, want) {
			t.Fatalf("the glob matched %q, want %q", got, want)
		}
	})

	t.Run("a trailing separator on the root changes nothing", func(t *testing.T) {
		got, err := GlobFrom(ctx, theTree(), "/store/mc/", "*/b.root")
		if err != nil {
			t.Fatalf("could not glob: %v", err)
		}
		if want := []string{"/store/mc/2024/b.root"}; !slices.Equal(got, want) {
			t.Fatalf("the glob matched %q, want %q", got, want)
		}
	})

	t.Run("absolute", func(t *testing.T) {
		got, err := GlobFrom(ctx, theTree(), "/store/mc", "/tmp/*.root")
		if err != nil {
			t.Fatalf("could not glob: %v", err)
		}
		if want := []string{"/tmp/e.root"}; !slices.Equal(got, want) {
			t.Fatalf("the glob matched %q, want %q", got, want)
		}
	})

	t.Run("rglob is a glob at any depth", func(t *testing.T) {
		deep, err := RGlob(ctx, theTree(), "/store", "*.root")
		if err != nil {
			t.Fatalf("could not glob: %v", err)
		}
		same, err := Glob(ctx, theTree(), "/store/**/*.root")
		if err != nil {
			t.Fatalf("could not glob: %v", err)
		}
		if !slices.Equal(deep, same) {
			t.Fatalf("RGlob matched %q, want the same as the equivalent pattern, %q", deep, same)
		}
	})
}

// TestConformance_AWalkVisitsTheRootAndThenEverythingUnderIt in lexical order,
// whatever order the server answered its listings in.
func TestConformance_AWalkVisitsTheRootAndThenEverythingUnderIt(t *testing.T) {
	var got []string
	err := Walk(context.Background(), theTree(), "/store/mc", func(p string, entry EntryStat, err error) error {
		if err != nil {
			t.Errorf("the walk reported %q: %v", p, err)
			return nil
		}
		if entry.EntryName != path.Base(p) {
			t.Errorf("the entry for %q is named %q", p, entry.EntryName)
		}
		got = append(got, p)
		return nil
	})
	if err != nil {
		t.Fatalf("could not walk: %v", err)
	}
	want := []string{
		"/store/mc",
		"/store/mc/2023",
		"/store/mc/2023/a.root",
		"/store/mc/2024",
		"/store/mc/2024/b.root",
		"/store/mc/2024/c.txt",
		"/store/mc/README",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("the walk visited\n%q\nwant\n%q", got, want)
	}
}

// TestConformance_SkipDirPrunesAndSkipAllStops gives a caller the same control
// over a namespace walk that filepath.WalkDir gives over a local one, which is
// the only way to bound a walk that would otherwise cost one round trip per
// directory in a storage element.
func TestConformance_SkipDirPrunesAndSkipAllStops(t *testing.T) {
	t.Run("skipdir on a directory prunes it", func(t *testing.T) {
		fsys := theTree()
		var got []string
		err := Walk(context.Background(), fsys, "/store", func(p string, entry EntryStat, err error) error {
			got = append(got, p)
			if p == "/store/data" {
				return fs.SkipDir
			}
			return nil
		})
		if err != nil {
			t.Fatalf("could not walk: %v", err)
		}
		if slices.Contains(fsys.asked, "/store/data") {
			t.Error("the walk listed a directory it had been told to skip")
		}
		for _, p := range got {
			if strings.HasPrefix(p, "/store/data/") {
				t.Errorf("the walk visited %q below the directory it skipped", p)
			}
		}
		if !slices.Contains(got, "/store/mc/README") {
			t.Errorf("skipping one directory ended the walk: %q", got)
		}
	})

	t.Run("skipdir on a file skips the rest of its directory", func(t *testing.T) {
		var got []string
		err := Walk(context.Background(), theTree(), "/store/mc/2024", func(p string, entry EntryStat, err error) error {
			got = append(got, p)
			if p == "/store/mc/2024/b.root" {
				return fs.SkipDir
			}
			return nil
		})
		if err != nil {
			t.Fatalf("could not walk: %v", err)
		}
		want := []string{"/store/mc/2024", "/store/mc/2024/b.root"}
		if !slices.Equal(got, want) {
			t.Fatalf("the walk visited %q, want %q", got, want)
		}
	})

	t.Run("skipall ends the walk", func(t *testing.T) {
		var got []string
		err := Walk(context.Background(), theTree(), "/store", func(p string, entry EntryStat, err error) error {
			got = append(got, p)
			if p == "/store/data/2024" {
				return fs.SkipAll
			}
			return nil
		})
		if err != nil {
			t.Fatalf("SkipAll is not a failure: %v", err)
		}
		if last := got[len(got)-1]; last != "/store/data/2024" {
			t.Fatalf("the walk went on to %q after it had been stopped", last)
		}
	})

	t.Run("any other error ends the walk and is returned", func(t *testing.T) {
		want := errors.New("enough")
		err := Walk(context.Background(), theTree(), "/store", func(p string, entry EntryStat, err error) error {
			return want
		})
		if !errors.Is(err, want) {
			t.Fatalf("the walk returned %v, want %v", err, want)
		}
	})
}

// TestConformance_ADirectoryThatCannotBeListedDoesNotEndTheWalk. A namespace
// with mixed permissions is the normal case at a shared storage element, and a
// walk that gave up at the first one would be unusable there. The failure is
// reported rather than hidden: it is the caller who knows whether one
// unreadable directory makes the answer wrong.
func TestConformance_ADirectoryThatCannotBeListedDoesNotEndTheWalk(t *testing.T) {
	fsys := theTree()
	fsys.fail["/store/data"] = os.ErrPermission

	var reported []string
	var visited []string
	err := Walk(context.Background(), fsys, "/store", func(p string, entry EntryStat, err error) error {
		if err != nil {
			reported = append(reported, p)
			if !errors.Is(err, os.ErrPermission) {
				t.Errorf("the walk reported %q with %v, want the failure the server gave", p, err)
			}
			if !entry.IsDir() {
				t.Errorf("the failure for %q did not say it was a directory", p)
			}
			return nil
		}
		visited = append(visited, p)
		return nil
	})
	if err != nil {
		t.Fatalf("one unreadable directory ended the walk: %v", err)
	}
	if want := []string{"/store/data"}; !slices.Equal(reported, want) {
		t.Fatalf("the walk reported %q, want %q", reported, want)
	}
	if !slices.Contains(visited, "/store/data") {
		t.Error("the directory that could not be read was never visited")
	}
	if !slices.Contains(visited, "/store/mc/2024/b.root") {
		t.Errorf("the walk stopped at the unreadable directory: %q", visited)
	}

	t.Run("and a glob simply matches nothing there", func(t *testing.T) {
		fsys := theTree()
		fsys.fail["/store/data"] = os.ErrPermission
		got, err := Glob(context.Background(), fsys, "/store/**/*.root")
		if err != nil {
			t.Fatalf("could not glob: %v", err)
		}
		want := []string{"/store/mc/2023/a.root", "/store/mc/2024/b.root"}
		if !slices.Equal(got, want) {
			t.Fatalf("the glob matched %q, want %q", got, want)
		}
	})

	t.Run("unless the caller says one is enough", func(t *testing.T) {
		fsys := theTree()
		fsys.fail["/store/data"] = os.ErrPermission
		want := errors.New("that directory was the point")

		err := Walk(context.Background(), fsys, "/store", func(p string, entry EntryStat, err error) error {
			if err == nil {
				return nil
			}
			return want
		})
		if !errors.Is(err, want) {
			t.Fatalf("the walk returned %v, want %v", err, want)
		}
		if slices.Contains(fsys.asked, "/store/mc") {
			t.Error("the walk went on to the next directory after it had been stopped")
		}
	})

	t.Run("a root that is not there is reported once", func(t *testing.T) {
		var reported []string
		err := Walk(context.Background(), theTree(), "/nowhere", func(p string, entry EntryStat, err error) error {
			if err == nil {
				t.Errorf("the walk visited %q, which does not exist", p)
				return nil
			}
			reported = append(reported, p)
			return nil
		})
		if err != nil {
			t.Fatalf("could not walk: %v", err)
		}
		if want := []string{"/nowhere"}; !slices.Equal(reported, want) {
			t.Fatalf("the walk reported %q, want %q", reported, want)
		}
	})
}

// TestConformance_AListingWithoutStatInformationIsAskedAbout. kXR_dirlist is
// asked for stat information, but a server is free to answer without it, and a
// walk that then took every entry for a file would stop at the first level.
func TestConformance_AListingWithoutStatInformationIsAskedAbout(t *testing.T) {
	fsys := theTree()
	fsys.blind = true

	got, err := Glob(context.Background(), fsys, "/store/**/*.root")
	if err != nil {
		t.Fatalf("could not glob: %v", err)
	}
	want := []string{
		"/store/data/2024/d.root",
		"/store/mc/2023/a.root",
		"/store/mc/2024/b.root",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("the glob matched %q, want %q", got, want)
	}
	if !slices.Contains(fsys.stats, "/store/mc/2024") {
		t.Errorf("the walk never asked what /store/mc/2024 was: %q", fsys.stats)
	}
}

// TestConformance_ACancelledContextEndsTheWalk rather than running to the end
// of a namespace that nobody is waiting for any more.
func TestConformance_ACancelledContextEndsTheWalk(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fsys := theTree()

	var visited int
	err := Walk(ctx, fsys, "/store", func(p string, entry EntryStat, err error) error {
		visited++
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the walk returned %v, want %v", err, context.Canceled)
	}
	if visited != 1 {
		t.Fatalf("the walk visited %d entries after it was cancelled", visited-1)
	}

	t.Run("between one listing and the next", func(t *testing.T) {
		// The listing that was already in flight is answered; what is pinned
		// here is that the walk does not descend into what it returned.
		ctx, cancel := context.WithCancel(context.Background())
		fsys := theTree()
		fsys.listed = func(string) { cancel() }

		var visited []string
		err := Walk(ctx, fsys, "/store", func(p string, entry EntryStat, err error) error {
			visited = append(visited, p)
			return nil
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("the walk returned %v, want %v", err, context.Canceled)
		}
		if want := []string{"/store"}; !slices.Equal(visited, want) {
			t.Fatalf("the walk visited %q, want %q", visited, want)
		}
		if want := []string{"/store"}; !slices.Equal(fsys.asked, want) {
			t.Fatalf("the walk listed %q after it was cancelled, want %q", fsys.asked, want)
		}
	})

	t.Run("and a glob returns it", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := Glob(ctx, theTree(), "/store/**/*.root"); !errors.Is(err, context.Canceled) {
			t.Fatalf("the glob returned %v, want %v", err, context.Canceled)
		}
		if _, err := Glob(ctx, theTree(), "/store/*.root"); !errors.Is(err, context.Canceled) {
			t.Fatalf("the one-listing glob returned %v, want %v", err, context.Canceled)
		}
		if _, err := Glob(ctx, theTree(), "/store/mc/README"); !errors.Is(err, context.Canceled) {
			t.Fatalf("the glob of a plain path returned %v, want %v", err, context.Canceled)
		}
	})
}

// TestConformance_OpaqueDataTravelsWithEveryRequestAndNamesNothing. The
// "?authz=..." on a path is the token the namespace is being read with, not
// part of what anything is called. A glob that matched against it would find
// nothing, and one that dropped it before descending would be refused at the
// first subdirectory.
func TestConformance_OpaqueDataTravelsWithEveryRequestAndNamesNothing(t *testing.T) {
	const cgi = "?authz=token&xrd.wantprot=unix"

	t.Run("a glob", func(t *testing.T) {
		fsys := theTree()
		got, err := Glob(context.Background(), fsys, "/store/mc/**/*.root"+cgi)
		if err != nil {
			t.Fatalf("could not glob: %v", err)
		}
		want := []string{"/store/mc/2023/a.root", "/store/mc/2024/b.root"}
		if !slices.Equal(got, want) {
			t.Fatalf("the glob matched %q, want %q", got, want)
		}
		if len(fsys.asked) == 0 {
			t.Fatal("the glob listed nothing at all")
		}
		for _, asked := range fsys.asked {
			if !strings.HasSuffix(asked, cgi) {
				t.Errorf("the glob listed %q without the authorization it was given", asked)
			}
		}
	})

	t.Run("a walk", func(t *testing.T) {
		fsys := theTree()
		var got []string
		err := Walk(context.Background(), fsys, "/store/mc"+cgi, func(p string, entry EntryStat, err error) error {
			if err != nil {
				t.Errorf("the walk reported %q: %v", p, err)
			}
			got = append(got, p)
			return nil
		})
		if err != nil {
			t.Fatalf("could not walk: %v", err)
		}
		for _, p := range got {
			if strings.Contains(p, "?") {
				t.Errorf("the walk reported %q, which names the token rather than the entry", p)
			}
		}
		if !slices.Contains(got, "/store/mc/2024/b.root") {
			t.Errorf("the walk visited %q, and never reached the files", got)
		}
		for _, asked := range fsys.asked {
			if !strings.HasSuffix(asked, cgi) {
				t.Errorf("the walk listed %q without the authorization it was given", asked)
			}
		}
	})

	t.Run("a pattern with nothing to match", func(t *testing.T) {
		fsys := theTree()
		got, err := Glob(context.Background(), fsys, "/store/mc/README"+cgi)
		if err != nil {
			t.Fatalf("could not glob: %v", err)
		}
		if want := []string{"/store/mc/README"}; !slices.Equal(got, want) {
			t.Fatalf("the glob matched %q, want %q", got, want)
		}
		if want := []string{"/store/mc/README" + cgi}; !slices.Equal(fsys.stats, want) {
			t.Fatalf("the glob asked about %q, want %q", fsys.stats, want)
		}
	})

	t.Run("a relative pattern takes it from the root", func(t *testing.T) {
		fsys := theTree()
		got, err := GlobFrom(context.Background(), fsys, "/store/mc"+cgi, "*/b.root")
		if err != nil {
			t.Fatalf("could not glob: %v", err)
		}
		if want := []string{"/store/mc/2024/b.root"}; !slices.Equal(got, want) {
			t.Fatalf("the glob matched %q, want %q", got, want)
		}
		for _, asked := range fsys.asked {
			if !strings.HasSuffix(asked, cgi) {
				t.Errorf("the glob listed %q without the authorization it was given", asked)
			}
		}
	})
}
