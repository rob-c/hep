// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package xrdio provides a File type that implements various interfaces from
// the io package.
//
// Files are opened by URL, and the scheme selects the transport: root, roots,
// xroot and xroots speak the native XRootD protocol, http, https, dav and davs
// go over HTTP data access. Open reads, Create writes, and OpenFile takes the
// os.O_* flags for everything in between — see OpenFile for the two places
// where XRootD does not have the open that os does. The From variants take a
// filesystem handle the caller already has, and leave it open.
package xrdio // import "go-hep.org/x/hep/xrootd/xrdio"

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdfs"
)

// File wraps a xrdfs.File and implements the following interfaces:
//   - io.Closer
//   - io.Reader
//   - io.Writer
//   - io.ReaderAt
//   - io.WriterAt
//   - io.Seeker
//   - fs.File
//
// It also has the Sync, Truncate and Stat methods of an os.File, so a *File
// can stand in for one wherever the code writing to it is not fussy about the
// concrete type.
type File struct {
	// backend is the endpoint this file was opened through, closed with the
	// file. It is nil for a File opened via OpenFrom, which does not own one.
	backend xrootd.Backend
	fs      xrdfs.FileSystem
	f       xrdfs.File

	name string
	pos  int64
	size int64
}

// Open opens the name file, where name is the absolute location of that file
// (xrootd server address and path to the file on that server.)
//
// Example:
//
//	f, err := xrdio.Open("root://server.example.com:1094//some/path/to/file")
//	f, err := xrdio.Open("https://server.example.com:1094/some/path/to/file")
//
// The URL scheme selects the transport: root, roots, xroot and xroots use the
// native XRootD protocol, while http, https, dav and davs use HTTP data access
// (see xrootd.Dial). A bare path with no scheme is treated as native XRootD,
// as before.
func Open(name string) (*File, error) {
	return OpenFile(name, os.O_RDONLY)
}

// OpenFrom opens the file name via the given filesystem handle.
// name is the absolute path of the wanted file on the server.
//
// Example:
//
//	f, err := xrdio.OpenFrom(fs, "/some/path/to/file")
func OpenFrom(fs xrdfs.FileSystem, name string) (*File, error) {
	return OpenFileFrom(fs, name, os.O_RDONLY)
}

// Name returns the name of the file.
func (f *File) Name() string {
	return f.name
}

// Close implements io.Closer.
func (f *File) Close() error {
	if f == nil {
		return os.ErrInvalid
	}

	var (
		err1 = f.f.Close(context.Background())
		err2 error
	)

	if f.backend != nil {
		err2 = f.backend.Close()
	}
	if err1 != nil {
		return fmt.Errorf("xrdio: could not close file %q: %w", f.name, err1)
	}
	if err2 != nil {
		return fmt.Errorf("xrdio: could not close xrd-client: %w", err2)
	}
	return nil
}

// Read implements io.Reader.
func (f *File) Read(data []byte) (int, error) {
	n, err := f.f.ReadAt(data, f.pos)
	f.pos += int64(n)
	if err != nil {
		return n, err
	}
	if f.pos == f.size {
		err = io.EOF
	}
	return n, err
}

// ReadAt implements io.ReaderAt.
func (f *File) ReadAt(data []byte, offset int64) (int, error) {
	return f.f.ReadAt(data, offset)
}

// Write implements io.Writer.
func (f *File) Write(data []byte) (int, error) {
	n, err := f.WriteAt(data, f.pos)
	f.pos += int64(n)
	return n, err
}

// WriteAt implements io.WriterAt.
func (f *File) WriteAt(data []byte, offset int64) (int, error) {
	n, err := f.f.WriteAt(data, offset)
	if end := offset + int64(n); end > f.size {
		// Read reports io.EOF from this, so a file that has been written
		// past its old end has to know it grew.
		f.size = end
	}
	return n, err
}

// Sync commits the file's contents to storage, and reports what the server
// says about that rather than what the local side hopes.
func (f *File) Sync() error {
	err := f.f.Sync(context.Background())
	if err != nil {
		return fmt.Errorf("xrdio: could not sync %q: %w", f.name, err)
	}
	return nil
}

// Truncate changes the size of the file. It does not move the offset a
// following Write would use, which is os.File's behaviour too.
func (f *File) Truncate(size int64) error {
	err := f.f.Truncate(context.Background(), size)
	if err != nil {
		return fmt.Errorf("xrdio: could not truncate %q: %w", f.name, err)
	}
	f.size = size
	return nil
}

// Seek implements io.Seeker
func (f *File) Seek(offset int64, whence int) (int64, error) {
	var pos int64
	switch whence {
	case io.SeekStart:
		pos = offset
	case io.SeekEnd:
		// io.Seeker counts SeekEnd from the end going forward, so a
		// negative offset is what walks back into the file.
		st, err := f.Stat()
		if err != nil {
			return 0, fmt.Errorf("xrdio: could not xrootd-stat %q: %w", f.Name(), err)
		}
		pos = st.Size() + offset
	case io.SeekCurrent:
		pos = f.pos + offset
	default:
		return 0, fmt.Errorf("xrdio: invalid whence %d for %q", whence, f.Name())
	}
	if pos < 0 {
		return 0, fmt.Errorf("xrdio: negative position %d for %q", pos, f.Name())
	}
	f.pos = pos
	return f.pos, nil
}

func (f *File) Stat() (os.FileInfo, error) {
	v, err := f.f.Stat(context.Background())
	return v, err
}

var (
	_ io.Closer   = (*File)(nil)
	_ io.Reader   = (*File)(nil)
	_ io.ReaderAt = (*File)(nil)
	_ io.Writer   = (*File)(nil)
	_ io.WriterAt = (*File)(nil)
	_ io.Seeker   = (*File)(nil)
	_ fs.File     = (*File)(nil)
)
