// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrd

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"go-hep.org/x/hep/xrootd/xrdfs"
)

// Stat returns what the server knows about a file or directory: its size, its
// modification time, and whether it is a directory. name is a URL or a local
// path.
func Stat(name string) (os.FileInfo, error) {
	isLocal, _, err := local(name)
	if err != nil {
		return nil, &Error{Op: "stat", Name: name, Err: err}
	}
	if isLocal {
		return os.Stat(name)
	}

	return run("stat", name, func(ctx context.Context, fsys xrdfs.FileSystem, path string) (os.FileInfo, error) {
		st, err := fsys.Stat(ctx, path)
		if err != nil {
			return nil, err
		}
		return st, nil
	})
}

// Exists reports whether the named file or directory is there. A server that
// cannot be reached, or that refuses to answer, is an error rather than a
// false: not finding a file and not being able to look are different answers,
// and only one of them means the file is not there.
func Exists(name string) (bool, error) {
	_, err := Stat(name)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	}
	return false, err
}

// List returns the contents of a directory, sorted by name. name is a URL or a
// local path.
//
// The entries carry the size and modification time the server sent with the
// listing, so counting up the size of a directory costs one request rather
// than one per file.
func List(name string) ([]os.FileInfo, error) {
	isLocal, _, err := local(name)
	if err != nil {
		return nil, &Error{Op: "list", Name: name, Err: err}
	}
	if isLocal {
		return listLocal(name)
	}

	return run("list", name, func(ctx context.Context, fsys xrdfs.FileSystem, path string) ([]os.FileInfo, error) {
		entries, err := fsys.Dirlist(ctx, path)
		if err != nil {
			return nil, err
		}
		out := make([]os.FileInfo, len(entries))
		for i, e := range entries {
			out[i] = e
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
		return out, nil
	})
}

func listLocal(name string) ([]os.FileInfo, error) {
	entries, err := os.ReadDir(name)
	if err != nil {
		return nil, err
	}
	out := make([]os.FileInfo, 0, len(entries))
	for _, e := range entries {
		fi, err := e.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, fi)
	}
	return out, nil
}

// Glob returns the names matching a pattern, as URLs ready to be passed to any
// of these functions. The pattern is the shell one — * matches anything within
// a directory, ? matches one character, [a-z] a range — with ** matching
// across directories:
//
//	xrd.Glob("root://storage.example.org//store/user/gopher/*.root")
//	xrd.Glob("root://storage.example.org//store/user/gopher/**/AOD*.root")
//
// A pattern that matches nothing is not an error: the answer is that nothing
// matched.
func Glob(pattern string) ([]string, error) {
	isLocal, u, err := local(pattern)
	if err != nil {
		return nil, &Error{Op: "glob", Name: pattern, Err: err}
	}
	if isLocal {
		return filepath.Glob(pattern)
	}

	return run("glob", pattern, func(ctx context.Context, fsys xrdfs.FileSystem, path string) ([]string, error) {
		paths, err := xrdfs.Glob(ctx, fsys, path)
		if err != nil {
			return nil, err
		}
		out := make([]string, len(paths))
		for i, p := range paths {
			out[i] = urlOf(u, p)
		}
		return out, nil
	})
}

// Find returns every file under a directory, as URLs, deepest last. It is the
// answer to "what is in there?" when the tree is deeper than one level.
//
// Directories are not included, only the files in them. A directory that
// cannot be read is skipped rather than fatal: one unreadable corner of a
// large tree should not cost you the rest of it.
func Find(name string) ([]string, error) {
	var out []string
	err := Walk(name, func(path string, info os.FileInfo, err error) error {
		switch {
		case err != nil:
			return nil // skip what cannot be read
		case info.IsDir():
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Walk calls fn for every file and directory under name, starting with name
// itself. name is a URL or a local path, and the path passed to fn is of the
// same kind, so it can be handed straight back to Open or Stat.
//
// When something cannot be read, fn is called with the error and a nil info;
// returning nil from fn skips it and carries on, and returning the error stops
// the walk. Returning [fs.SkipDir] from a directory skips what is under it.
func Walk(name string, fn func(path string, info os.FileInfo, err error) error) error {
	isLocal, u, err := local(name)
	if err != nil {
		return &Error{Op: "walk", Name: name, Err: err}
	}
	if isLocal {
		return filepath.Walk(name, fn)
	}

	return do("walk", name, func(ctx context.Context, fsys xrdfs.FileSystem, path string) error {
		return xrdfs.Walk(ctx, fsys, path, func(p string, entry xrdfs.EntryStat, err error) error {
			if err != nil {
				return fn(urlOf(u, p), nil, err)
			}
			return fn(urlOf(u, p), entry, nil)
		})
	})
}

// Mkdir creates a directory, together with any of its parents that are
// missing. It is not an error for the directory to be there already. name is a
// URL or a local path.
func Mkdir(name string) error {
	isLocal, _, err := local(name)
	if err != nil {
		return &Error{Op: "create directory", Name: name, Err: err}
	}
	if isLocal {
		return os.MkdirAll(name, 0755)
	}

	return do("create directory", name, func(ctx context.Context, fsys xrdfs.FileSystem, path string) error {
		return fsys.MkdirAll(ctx, path, xrdfs.OpenModeOwnerRead|xrdfs.OpenModeOwnerWrite|xrdfs.OpenModeOwnerExecute)
	})
}

// Remove deletes a file, or a directory that has nothing in it. To delete a
// directory and everything under it, use RemoveAll. name is a URL or a local
// path.
func Remove(name string) error {
	isLocal, _, err := local(name)
	if err != nil {
		return &Error{Op: "remove", Name: name, Err: err}
	}
	if isLocal {
		return os.Remove(name)
	}

	return do("remove", name, func(ctx context.Context, fsys xrdfs.FileSystem, path string) error {
		st, err := fsys.Stat(ctx, path)
		if err == nil && st.IsDir() {
			return fsys.RemoveDir(ctx, path)
		}
		return fsys.RemoveFile(ctx, path)
	})
}

// RemoveAll deletes a path and everything under it. It is not an error for
// there to be nothing there. name is a URL or a local path.
func RemoveAll(name string) error {
	isLocal, _, err := local(name)
	if err != nil {
		return &Error{Op: "remove", Name: name, Err: err}
	}
	if isLocal {
		return os.RemoveAll(name)
	}

	return do("remove", name, func(ctx context.Context, fsys xrdfs.FileSystem, path string) error {
		return fsys.RemoveAll(ctx, path)
	})
}

// Rename moves a file or directory to a new name on the same server. Moving
// between two servers is a copy and a delete, which this does not do for you:
// use Copy and then Remove, so that nothing is deleted before the copy is
// known to have worked.
func Rename(oldname, newname string) error {
	oldLocal, oldURL, err := local(oldname)
	if err != nil {
		return &Error{Op: "rename", Name: oldname, Err: err}
	}
	newLocal, newURL, err := local(newname)
	if err != nil {
		return &Error{Op: "rename", Name: newname, Err: err}
	}

	switch {
	case oldLocal && newLocal:
		return os.Rename(oldname, newname)
	case oldLocal != newLocal:
		return &Error{Op: "rename", Name: oldname, Err: errors.New("cannot rename between this machine and a server: copy it, then remove the original")}
	case endpoint(oldURL) != endpoint(newURL):
		return &Error{Op: "rename", Name: oldname, Err: errors.New("cannot rename between two servers: copy it, then remove the original")}
	}

	return do("rename", oldname, func(ctx context.Context, fsys xrdfs.FileSystem, path string) error {
		return fsys.Rename(ctx, path, newURL.Path)
	})
}
