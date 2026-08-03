// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/sync/errgroup"

	"go-hep.org/x/hep/xrootd/xrdcopy"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdio"
)

// File is an open file, local or remote. It behaves like an *os.File, and can
// be handed to anything that reads or writes one — encoding/csv, bufio,
// io.Copy — without that code knowing where the bytes are.
type File interface {
	io.Reader
	io.ReaderAt
	io.Writer
	io.WriterAt
	io.Seeker
	io.Closer

	// Name returns the name the file was opened with.
	Name() string
	// Stat returns the size, modification time and mode of the file.
	Stat() (os.FileInfo, error)
	// Sync waits for the server to have the bytes written so far.
	Sync() error
	// Truncate changes the size of the file.
	Truncate(size int64) error
}

var (
	_ File = (*os.File)(nil)
	_ File = (*xrdio.File)(nil)
)

// ReadFile reads the whole of the named file and returns its contents. name is
// a URL or a local path.
//
// It reads the file into memory, which is the right thing for a text file, a
// configuration or a list of runs, and the wrong thing for a 40 GB dataset:
// for those, use Open and read what you need, or Download to put it on disk.
func ReadFile(name string) ([]byte, error) {
	isLocal, _, err := local(name)
	if err != nil {
		return nil, &Error{Op: "read", Name: name, Err: err}
	}
	if isLocal {
		data, err := os.ReadFile(name)
		return data, wrap("read", name, err)
	}

	f, err := Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, &Error{Op: "read", Name: name, Err: err}
	}
	return data, nil
}

// WriteFile writes data to the named file, creating it if it is not there and
// replacing whatever it held if it is. Any missing parent directories are
// created too. name is a URL or a local path.
func WriteFile(name string, data []byte) error {
	isLocal, _, err := local(name)
	if err != nil {
		return &Error{Op: "write", Name: name, Err: err}
	}
	if isLocal {
		if dir := dirOf(name); dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return wrap("write", name, err)
			}
		}
		return wrap("write", name, os.WriteFile(name, data, 0644))
	}

	f, err := Create(name)
	if err != nil {
		return err
	}

	_, err = f.Write(data)
	if err != nil {
		f.Close()
		return &Error{Op: "write", Name: name, Err: err}
	}
	if err := f.Close(); err != nil {
		return &Error{Op: "write", Name: name, Err: err}
	}
	return nil
}

// Open opens the named file for reading. name is a URL or a local path.
//
// The file must be closed when you are done with it, which in a short program
// means:
//
//	f, err := xrd.Open(name)
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer f.Close()
func Open(name string) (File, error) {
	isLocal, _, err := local(name)
	if err != nil {
		return nil, &Error{Op: "open", Name: name, Err: err}
	}
	if isLocal {
		f, err := os.Open(name)
		if err != nil {
			return nil, wrap("open", name, err)
		}
		return f, nil
	}

	return run("open", name, func(ctx context.Context, fsys xrdfs.FileSystem, path string) (File, error) {
		return xrdio.OpenFileFrom(fsys, path, os.O_RDONLY)
	})
}

// Create creates the named file for writing, replacing whatever it held if it
// was already there, and creating any missing parent directories on the way.
// name is a URL or a local path.
//
// The returned file can be read as well as written: XRootD has no write-only
// open.
func Create(name string) (File, error) {
	isLocal, _, err := local(name)
	if err != nil {
		return nil, &Error{Op: "create", Name: name, Err: err}
	}
	if isLocal {
		if dir := dirOf(name); dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, wrap("create", name, err)
			}
		}
		f, err := os.Create(name)
		if err != nil {
			return nil, wrap("create", name, err)
		}
		return f, nil
	}

	const flag = os.O_RDWR | os.O_CREATE | os.O_TRUNC | xrdio.MkPath
	return run("create", name, func(ctx context.Context, fsys xrdfs.FileSystem, path string) (File, error) {
		return xrdio.OpenFileFrom(fsys, path, flag)
	})
}

// Append opens the named file for writing at its end, creating it if it is not
// there. name is a URL or a local path.
func Append(name string) (File, error) {
	isLocal, _, err := local(name)
	if err != nil {
		return nil, &Error{Op: "append to", Name: name, Err: err}
	}
	if isLocal {
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			return nil, wrap("append to", name, err)
		}
		return f, nil
	}

	const flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND | xrdio.MkPath
	return run("append to", name, func(ctx context.Context, fsys xrdfs.FileSystem, path string) (File, error) {
		return xrdio.OpenFileFrom(fsys, path, flag)
	})
}

// Download copies a remote file to a local one. It is Copy with the direction
// spelled out, and is here because that is the operation people look for.
//
// The transfer is resumable and verified against the server's checksum when
// the server reports one, so a download interrupted halfway can be repeated
// and will continue rather than start again.
func Download(src, dst string) error {
	err := xrdcopy.Copy(context.Background(), dst, src, xrdcopy.Options{
		Resume: true,
		Verify: true,
	})
	return fail("download", src, err)
}

// Upload copies a local file to a remote one.
func Upload(src, dst string) error {
	err := xrdcopy.Copy(context.Background(), dst, src, xrdcopy.Options{Resume: true})
	return fail("upload", src, err)
}

// Copy copies src to dst. Either may be a URL or a local path, so this covers
// downloading, uploading, and copying between two servers — the last of which
// is asked of the servers themselves, so the bytes do not travel through this
// program.
//
// A directory is copied whole, with everything under it.
func Copy(dst, src string) error {
	err := xrdcopy.Copy(context.Background(), dst, src, xrdcopy.Options{
		Recursive: true,
		Resume:    true,
	})
	return fail("copy", src, err)
}

// dirOf is the parent directory of a local name, or "" when the name is in the
// current directory and there is nothing to create.
func dirOf(name string) string {
	dir := filepath.Dir(name)
	if dir == "." {
		return ""
	}
	return dir
}

// Lines reads a text file and returns it split into lines, with the line
// endings removed. It is the shape a list of runs, of file names or of dataset
// names usually arrives in:
//
//	for _, name := range must(xrd.Lines("files.txt")) { ... }
//
// A file ending in a newline does not produce an empty last line, and a file
// written on Windows does not leave a stray carriage return on each one.
func Lines(name string) ([]string, error) {
	data, err := ReadFile(name)
	if err != nil {
		return nil, err
	}

	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return nil, nil
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	return lines, nil
}

// WriteLines writes lines to the named file, one per line, replacing whatever
// it held. It is the other half of Lines.
func WriteLines(name string, lines []string) error {
	var buf strings.Builder
	for _, line := range lines {
		buf.WriteString(line)
		buf.WriteString("\n")
	}
	return WriteFile(name, []byte(buf.String()))
}

// DefaultParallel is how many files DownloadAll fetches at once. It is chosen
// to be quicker than one at a time without being the reason a site's network
// people come looking for you.
const DefaultParallel = 4

// DownloadAll copies several remote files into a local directory, a few at a
// time, and returns the local names in the order they were given. The
// directory is created if it is not there.
//
// This is the loop that is worth not writing yourself: doing it one file at a
// time wastes most of the network, and doing it all at once is how a laptop
// annoys a storage element. Each transfer is resumable, so a run interrupted
// halfway can simply be repeated.
//
// Files are named by the last element of their path. Two files that would land
// on the same local name are refused before anything is transferred, rather
// than one of them silently replacing the other: pass Download the names you
// want in that case.
func DownloadAll(names []string, dir string) ([]string, error) {
	out := make([]string, len(names))
	seen := make(map[string]string, len(names))
	for i, name := range names {
		base, err := baseOf(name)
		if err != nil {
			return nil, err
		}
		if first, dup := seen[base]; dup {
			return nil, &Error{Op: "download", Name: name, Err: fmt.Errorf("would be saved as %q, which is already taken by %q", base, first)}
		}
		seen[base] = name
		out[i] = filepath.Join(dir, base)
	}

	if err := Mkdir(dir); err != nil {
		return nil, err
	}

	var grp errgroup.Group
	grp.SetLimit(DefaultParallel)
	for i, name := range names {
		grp.Go(func() error { return Download(name, out[i]) })
	}
	if err := grp.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

// baseOf is the file name at the end of a name, whichever kind of name it is.
func baseOf(name string) (string, error) {
	isLocal, u, err := local(name)
	if err != nil {
		return "", &Error{Op: "download", Name: name, Err: err}
	}

	base := path.Base(u.Path)
	if isLocal {
		base = filepath.Base(name)
	}
	switch base {
	case "", ".", "/", `\`:
		return "", &Error{Op: "download", Name: name, Err: errors.New("there is no file name at the end of it")}
	}
	return base, nil
}
