// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdhttp // import "go-hep.org/x/hep/xrootd/xrdhttp"

import (
	"context"
	"errors"
	"fmt"
	"io"
	iofs "io/fs"
	"net/http"
	"path"
	"strings"
	"sync"

	"go-hep.org/x/hep/xrootd/xrdfs"
)

// ErrNotSupported reports an xrdfs operation with no HTTP or WebDAV
// equivalent. It is returned rather than silently ignored: an fs method that
// quietly does nothing is worse than one that says it cannot.
var ErrNotSupported = errors.New("xrdhttp: operation not supported over HTTP/WebDAV")

// FS returns a filesystem view of the endpoint, implemented over HTTP verbs
// and the WebDAV extensions (PROPFIND, MKCOL, MOVE).
//
// Two XRootD semantics have no HTTP equivalent and are emulated or refused
// rather than faked:
//
//   - HTTP has no random-access write. A file opened for writing buffers in
//     memory and is uploaded by a single PUT on Sync or Close, so a write is
//     not durable until the file is closed and the close error is checked.
//   - Truncate to a non-zero size, chmod and the virtual-filesystem stat have
//     no equivalent at all and return ErrNotSupported.
func (c *Client) FS() xrdfs.FileSystem { return &davFS{c: c} }

type davFS struct{ c *Client }

var _ xrdfs.FileSystem = (*davFS)(nil)

func (fs *davFS) Dirlist(ctx context.Context, dir string) ([]xrdfs.EntryStat, error) {
	ents, err := fs.c.Dirlist(ctx, dir)
	if err != nil {
		return nil, err
	}
	out := make([]xrdfs.EntryStat, 0, len(ents))
	for _, e := range ents {
		es := xrdfs.EntryStat{
			EntryName:   e.Name,
			HasStatInfo: true,
			EntrySize:   e.Size,
			Mtime:       e.ModTime.Unix(),
			Flags:       xrdfs.StatIsReadable | xrdfs.StatIsWritable,
		}
		if e.IsDir {
			es.Flags |= xrdfs.StatIsDir | xrdfs.StatIsExecutable
		}
		out = append(out, es)
	}
	return out, nil
}

func (fs *davFS) Stat(ctx context.Context, name string) (xrdfs.EntryStat, error) {
	fi, err := fs.c.Stat(ctx, name)
	if err != nil {
		return xrdfs.EntryStat{}, err
	}
	if !fi.Exists {
		// A collection may not answer HEAD; ask WebDAV before concluding the
		// path is absent.
		if es, derr := fs.statViaPropfind(ctx, name); derr == nil {
			return es, nil
		}
		return xrdfs.EntryStat{}, fmt.Errorf("xrdhttp: %q: %w", name, iofs.ErrNotExist)
	}
	return xrdfs.EntryStat{
		EntryName:   path.Base(name),
		HasStatInfo: true,
		EntrySize:   fi.Size,
		Mtime:       fi.ModTime.Unix(),
		Flags:       xrdfs.StatIsReadable | xrdfs.StatIsWritable,
	}, nil
}

// statViaPropfind stats one resource with a Depth: 0 PROPFIND, which is the
// only way to see a collection on a server that does not answer HEAD for one.
func (fs *davFS) statViaPropfind(ctx context.Context, name string) (xrdfs.EntryStat, error) {
	ents, err := fs.c.propfind(ctx, name, "0")
	if err != nil {
		return xrdfs.EntryStat{}, err
	}
	if len(ents) == 0 {
		return xrdfs.EntryStat{}, fmt.Errorf("xrdhttp: PROPFIND %q returned no resource", name)
	}
	e := ents[0]
	es := xrdfs.EntryStat{
		EntryName:   path.Base(name),
		HasStatInfo: true,
		EntrySize:   e.Size,
		Mtime:       e.ModTime.Unix(),
		Flags:       xrdfs.StatIsReadable | xrdfs.StatIsWritable,
	}
	if e.IsDir {
		es.Flags |= xrdfs.StatIsDir | xrdfs.StatIsExecutable
	}
	return es, nil
}

func (fs *davFS) Statx(ctx context.Context, paths []string) ([]xrdfs.StatFlags, error) {
	out := make([]xrdfs.StatFlags, len(paths))
	for i, p := range paths {
		es, err := fs.Stat(ctx, p)
		if err != nil {
			out[i] = xrdfs.StatIsOffline
			continue
		}
		out[i] = es.Flags
	}
	return out, nil
}

// RemoveFile removes a file, reporting one that is not there. That is the
// XRootD contract — kXR_rm answers kXR_NotFound — and it is why this does not
// go through Client.Remove, which is deliberately idempotent.
func (fs *davFS) RemoveFile(ctx context.Context, name string) error {
	return fs.c.remove(ctx, name)
}

// RemoveDir removes an empty collection. WebDAV DELETE on a collection is
// recursive, so emptiness is checked first to keep the XRootD contract.
func (fs *davFS) RemoveDir(ctx context.Context, name string) error {
	ents, err := fs.c.Dirlist(ctx, name)
	if err != nil {
		return err
	}
	if len(ents) != 0 {
		return fmt.Errorf("xrdhttp: directory %q is not empty", name)
	}
	return fs.c.remove(ctx, name)
}

// RemoveAll removes a path and everything under it. A WebDAV DELETE on a
// collection is already recursive. A path that is not there is not an error,
// which is both what os.RemoveAll does and what the name promises.
func (fs *davFS) RemoveAll(ctx context.Context, name string) error {
	return fs.c.Remove(ctx, name)
}

func (fs *davFS) Mkdir(ctx context.Context, name string, perm xrdfs.OpenMode) error {
	return fs.c.mkcol(ctx, name)
}

func (fs *davFS) MkdirAll(ctx context.Context, name string, perm xrdfs.OpenMode) error {
	clean := path.Clean("/" + name)
	var cur string
	for _, part := range strings.Split(strings.Trim(clean, "/"), "/") {
		if part == "" {
			continue
		}
		cur += "/" + part
		// A component that already exists answers 405; that is not an error
		// for MkdirAll.
		if err := fs.c.mkcol(ctx, cur); err != nil && !errors.Is(err, iofs.ErrExist) {
			return err
		}
	}
	return nil
}

func (fs *davFS) Rename(ctx context.Context, oldpath, newpath string) error {
	return fs.c.move(ctx, oldpath, newpath)
}

// Truncate has no WebDAV equivalent except the degenerate case of truncating
// to zero, which is a PUT of an empty body.
func (fs *davFS) Truncate(ctx context.Context, name string, size int64) error {
	if size != 0 {
		return fmt.Errorf("%w: truncate to a non-zero size", ErrNotSupported)
	}
	return fs.c.Create(ctx, name, strings.NewReader(""), 0)
}

func (fs *davFS) Chmod(ctx context.Context, name string, mode xrdfs.OpenMode) error {
	return fmt.Errorf("%w: chmod", ErrNotSupported)
}

func (fs *davFS) VirtualStat(ctx context.Context, name string) (xrdfs.VirtualFSStat, error) {
	return xrdfs.VirtualFSStat{}, fmt.Errorf("%w: virtual filesystem stat", ErrNotSupported)
}

func (fs *davFS) Open(ctx context.Context, name string, mode xrdfs.OpenMode, options xrdfs.OpenOptions) (xrdfs.File, error) {
	f := &davFile{fs: fs, name: name}

	write := options&(xrdfs.OpenOptionsOpenUpdate|xrdfs.OpenOptionsOpenAppend|xrdfs.OpenOptionsNew|xrdfs.OpenOptionsDelete) != 0
	if !write {
		st, err := fs.Stat(ctx, name)
		if err != nil {
			return nil, err
		}
		f.info = &st
		return f, nil
	}

	f.write = true
	// An update or append starts from what is already there; a new or
	// delete-and-create starts empty.
	if options&(xrdfs.OpenOptionsOpenUpdate|xrdfs.OpenOptionsOpenAppend) != 0 &&
		options&xrdfs.OpenOptionsDelete == 0 {
		raw, err := fs.c.ReadAll(ctx, name)
		if err == nil {
			f.buf = raw
		}
	}
	return f, nil
}

// davFile is an xrdfs.File over HTTP. Reads are Range GETs issued directly;
// writes accumulate in memory and are PUT as a whole, because HTTP has no
// random-access write.
type davFile struct {
	fs   *davFS
	name string

	mu     sync.Mutex
	buf    []byte
	write  bool
	dirty  bool
	info   *xrdfs.EntryStat
	closed bool
}

var _ xrdfs.File = (*davFile)(nil)

func (f *davFile) Compression() *xrdfs.FileCompression { return nil }
func (f *davFile) Info() *xrdfs.EntryStat              { return f.info }

// Handle returns a zero handle: HTTP is stateless and has no server-side file
// handle to report.
func (f *davFile) Handle() xrdfs.FileHandle { return xrdfs.FileHandle{} }

func (f *davFile) ReadAtContext(ctx context.Context, p []byte, off int64) (int, error) {
	f.mu.Lock()
	if f.write {
		defer f.mu.Unlock()
		if off >= int64(len(f.buf)) {
			return 0, io.EOF
		}
		n := copy(p, f.buf[off:])
		if n < len(p) {
			return n, io.EOF
		}
		return n, nil
	}
	f.mu.Unlock()
	return f.fs.c.ReadAt(ctx, p, f.name, off)
}

func (f *davFile) ReadAt(p []byte, off int64) (int, error) {
	return f.ReadAtContext(context.Background(), p, off)
}

func (f *davFile) WriteAtContext(ctx context.Context, p []byte, off int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.write {
		return fmt.Errorf("xrdhttp: %q is not open for writing", f.name)
	}
	if need := off + int64(len(p)); int64(len(f.buf)) < need {
		f.buf = append(f.buf, make([]byte, need-int64(len(f.buf)))...)
	}
	copy(f.buf[off:], p)
	f.dirty = true
	return nil
}

func (f *davFile) WriteAt(p []byte, off int64) (int, error) {
	if err := f.WriteAtContext(context.Background(), p, off); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Sync uploads the buffered content. It is a full PUT, not an incremental
// flush: HTTP offers nothing finer.
func (f *davFile) Sync(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.flush(ctx)
}

func (f *davFile) flush(ctx context.Context) error {
	if !f.write || !f.dirty {
		return nil
	}
	if err := f.fs.c.Create(ctx, f.name, strings.NewReader(string(f.buf)), int64(len(f.buf))); err != nil {
		return err
	}
	f.dirty = false
	return nil
}

func (f *davFile) Close(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	return f.flush(ctx)
}

// CloseVerify closes the file and checks the server reports the given size.
// A zero size suppresses the check.
func (f *davFile) CloseVerify(ctx context.Context, size int64) error {
	if err := f.Close(ctx); err != nil {
		return err
	}
	if size == 0 {
		return nil
	}
	fi, err := f.fs.c.Stat(ctx, f.name)
	if err != nil {
		return err
	}
	if fi.Size != size {
		return fmt.Errorf("xrdhttp: %q holds %d bytes after close, want %d", f.name, fi.Size, size)
	}
	return nil
}

func (f *davFile) Truncate(ctx context.Context, size int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.write {
		return fmt.Errorf("xrdhttp: %q is not open for writing", f.name)
	}
	switch {
	case size < int64(len(f.buf)):
		f.buf = f.buf[:size]
	default:
		f.buf = append(f.buf, make([]byte, size-int64(len(f.buf)))...)
	}
	f.dirty = true
	return nil
}

func (f *davFile) Stat(ctx context.Context) (xrdfs.EntryStat, error) {
	es, err := f.fs.Stat(ctx, f.name)
	if err != nil {
		return xrdfs.EntryStat{}, err
	}
	f.mu.Lock()
	f.info = &es
	f.mu.Unlock()
	return es, nil
}

func (f *davFile) StatVirtualFS(ctx context.Context) (xrdfs.VirtualFSStat, error) {
	return xrdfs.VirtualFSStat{}, fmt.Errorf("%w: virtual filesystem stat", ErrNotSupported)
}

func (f *davFile) VerifyWriteAt(ctx context.Context, p []byte, off int64) error {
	return fmt.Errorf("%w: checksum-verified write", ErrNotSupported)
}

// ---- WebDAV verbs used only by the filesystem view ----

func (c *Client) mkcol(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, "MKCOL", c.urlFor(name), nil)
	if err != nil {
		return err
	}
	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("xrdhttp: MKCOL %q: %w", name, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	switch {
	case resp.StatusCode/100 == 2:
		return nil
	default:
		// 405 is "it already exists" and 409 is "a parent does not",
		// which StatusError tells apart; both are ordinary outcomes of
		// creating a directory rather than transport failures.
		return statusError("MKCOL", name, resp)
	}
}

func (c *Client) move(ctx context.Context, oldpath, newpath string) error {
	req, err := http.NewRequestWithContext(ctx, "MOVE", c.urlFor(oldpath), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Destination", c.urlFor(newpath))
	req.Header.Set("Overwrite", "T")
	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("xrdhttp: MOVE %q: %w", oldpath, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("xrdhttp: MOVE to %q: %w", newpath, statusError("MOVE", oldpath, resp))
	}
	return nil
}
