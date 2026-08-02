// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdio

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdfs"
)

// MkPath may be or'ed into the flag given to OpenFile to create the parent
// directories of the file on the way. It has no os.O_* equivalent: locally one
// would call os.MkdirAll first, which over a network is a round trip the
// server is willing to save you.
const MkPath = 1 << 24

// Create creates the named file for writing, truncating it if it already
// exists, and returns a File open for reading and writing. name is a URL, in
// the same form Open accepts.
//
// Example:
//
//	f, err := xrdio.Create("root://server.example.com:1094//some/path/to/file")
//
// The parent directory must exist; add xrdio.MkPath to an OpenFile flag to
// have the server create it.
func Create(name string) (*File, error) {
	return OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_TRUNC)
}

// OpenFile opens the named file with the given flag, which is a combination of
// the os.O_* constants, optionally or'ed with MkPath. name is a URL, in the
// same form Open accepts, and the scheme selects the transport.
//
// XRootD has no write-only open: O_WRONLY is served by opening for update, so
// a file opened with it can also be read. O_APPEND positions the file at its
// end, and asks the server for an append-only handle.
//
// Example:
//
//	f, err := xrdio.OpenFile(name, os.O_WRONLY|os.O_CREATE|xrdio.MkPath)
func OpenFile(name string, flag int) (*File, error) {
	ctx := context.Background()

	urn, err := Parse(name)
	if err != nil {
		return nil, fmt.Errorf("could not parse %q: %w", name, err)
	}

	mode, opts, err := openMode(flag)
	if err != nil {
		return nil, err
	}

	// The whole URL is passed on, not just the address: the scheme is what
	// selects the transport, and dropping it here is how an https:// URL used
	// to end up dialling the native XRootD port.
	backend, err := xrootd.Dial(ctx, name, urn.User)
	if err != nil {
		return nil, fmt.Errorf("xrdio: could not connect to server %q: %w", urn.Addr, err)
	}

	xf, err := openFrom(ctx, backend.FS(), urn.Path, flag, mode, opts)
	if err != nil {
		backend.Close()
		return nil, fmt.Errorf("xrdio: could not open %q: %w", name, err)
	}
	xf.backend = backend

	return xf, nil
}

// CreateFrom creates the file name for writing via the given filesystem
// handle, truncating it if it already exists. name is the absolute path of the
// file on the server.
func CreateFrom(fs xrdfs.FileSystem, name string) (*File, error) {
	return OpenFileFrom(fs, name, os.O_RDWR|os.O_CREATE|os.O_TRUNC)
}

// OpenFileFrom opens the file name via the given filesystem handle with the
// given flag, which is a combination of the os.O_* constants, optionally
// or'ed with MkPath. name is the absolute path of the file on the server.
func OpenFileFrom(fsys xrdfs.FileSystem, name string, flag int) (*File, error) {
	mode, opts, err := openMode(flag)
	if err != nil {
		return nil, err
	}

	xf, err := openFrom(context.Background(), fsys, name, flag, mode, opts)
	if err != nil {
		return nil, fmt.Errorf("xrdio: could not open %q: %w", name, err)
	}
	return xf, nil
}

// openFrom opens name and settles the file's idea of its own size and
// position, which is what the rest of the io interfaces are written against.
func openFrom(ctx context.Context, fsys xrdfs.FileSystem, name string, flag int, mode xrdfs.OpenMode, opts xrdfs.OpenOptions) (*File, error) {
	f, err := open(ctx, fsys, name, flag, mode, opts)
	if err != nil {
		return nil, err
	}

	xf := &File{fs: fsys, f: f, name: name}

	// A file that was just created or truncated is empty, and asking the
	// server how long it is would be a round trip spent learning zero.
	if opts&xrdfs.OpenOptionsDelete == 0 {
		fi, err := xf.Stat()
		if err != nil {
			_ = f.Close(ctx)
			return nil, fmt.Errorf("could not stat: %w", err)
		}
		xf.size = fi.Size()
	}

	if flag&os.O_APPEND != 0 {
		xf.pos = xf.size
	}

	return xf, nil
}

// open applies the one open that has no single XRootD equivalent: O_CREATE
// without O_EXCL, which asks for the file to be there afterwards and says
// nothing about whether it was there before. kXR_new refuses an existing file
// and kXR_open_updt refuses a missing one, so the two are tried in the order
// that leaves an existing file's contents alone.
func open(ctx context.Context, fsys xrdfs.FileSystem, name string, flag int, mode xrdfs.OpenMode, opts xrdfs.OpenOptions) (xrdfs.File, error) {
	const create = os.O_CREATE | os.O_EXCL

	if flag&create != os.O_CREATE || opts&xrdfs.OpenOptionsDelete != 0 {
		return fsys.Open(ctx, name, mode, opts)
	}

	f, err := fsys.Open(ctx, name, mode, opts|xrdfs.OpenOptionsNew)
	if err == nil || !errors.Is(err, fs.ErrExist) {
		return f, err
	}
	return fsys.Open(ctx, name, mode, opts)
}

// openMode renders an os.O_* flag as the mode and options an XRootD open
// takes.
func openMode(flag int) (xrdfs.OpenMode, xrdfs.OpenOptions, error) {
	var (
		mode  = xrdfs.OpenModeOwnerRead
		opts  xrdfs.OpenOptions
		write = flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC) != 0
	)

	if flag&os.O_WRONLY != 0 && flag&os.O_RDWR != 0 {
		return 0, 0, fmt.Errorf("xrdio: invalid flag: O_WRONLY and O_RDWR together (%#o)", flag)
	}
	if flag&os.O_EXCL != 0 && flag&os.O_CREATE == 0 {
		return 0, 0, fmt.Errorf("xrdio: invalid flag: O_EXCL without O_CREATE (%#o)", flag)
	}
	if !write && flag&(os.O_EXCL|MkPath) != 0 {
		return 0, 0, fmt.Errorf("xrdio: invalid flag: read-only open asking to create (%#o)", flag)
	}

	switch {
	case !write:
		opts |= xrdfs.OpenOptionsOpenRead
	default:
		mode |= xrdfs.OpenModeOwnerWrite
		switch {
		case flag&os.O_APPEND != 0:
			opts |= xrdfs.OpenOptionsOpenAppend
		default:
			opts |= xrdfs.OpenOptionsOpenUpdate
		}
		if flag&os.O_TRUNC != 0 {
			// kXR_delete replaces whatever is there, which is what a
			// truncating open means to everyone who has ever called
			// os.Create.
			opts |= xrdfs.OpenOptionsDelete
		}
		if flag&(os.O_CREATE|os.O_EXCL) == os.O_CREATE|os.O_EXCL {
			opts |= xrdfs.OpenOptionsNew
		}
	}

	if flag&MkPath != 0 {
		opts |= xrdfs.OpenOptionsMkPath
	}

	return mode, opts, nil
}
