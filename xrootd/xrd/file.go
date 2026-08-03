// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrd

import (
	"context"
	"io"
	"os"
	"path/filepath"

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
		return os.ReadFile(name)
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
				return &Error{Op: "write", Name: name, Err: err}
			}
		}
		return os.WriteFile(name, data, 0644)
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
		return os.Open(name)
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
				return nil, &Error{Op: "create", Name: name, Err: err}
			}
		}
		return os.Create(name)
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
		return os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
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
	if err != nil {
		return &Error{Op: "download", Name: src, Err: err}
	}
	return nil
}

// Upload copies a local file to a remote one.
func Upload(src, dst string) error {
	err := xrdcopy.Copy(context.Background(), dst, src, xrdcopy.Options{Resume: true})
	if err != nil {
		return &Error{Op: "upload", Name: src, Err: err}
	}
	return nil
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
	if err != nil {
		return &Error{Op: "copy", Name: src, Err: err}
	}
	return nil
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
