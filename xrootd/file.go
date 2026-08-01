// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"context"
	"fmt"
	"math"
	"strings"
	rsync "sync"

	"go-hep.org/x/hep/xrootd/internal/pgbuf"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/open"
	"go-hep.org/x/hep/xrootd/xrdproto/pgread"
	"go-hep.org/x/hep/xrootd/xrdproto/pgwrite"
	"go-hep.org/x/hep/xrootd/xrdproto/query"
	"go-hep.org/x/hep/xrootd/xrdproto/read"
	"go-hep.org/x/hep/xrootd/xrdproto/readv"
	"go-hep.org/x/hep/xrootd/xrdproto/stat"
	"go-hep.org/x/hep/xrootd/xrdproto/sync"
	"go-hep.org/x/hep/xrootd/xrdproto/truncate"
	"go-hep.org/x/hep/xrootd/xrdproto/verifyw"
	"go-hep.org/x/hep/xrootd/xrdproto/write"
	"go-hep.org/x/hep/xrootd/xrdproto/writev"
	"go-hep.org/x/hep/xrootd/xrdproto/xrdclose"
)

// File implements access to a content and meta information of file over XRootD.
type file struct {
	fs          *fileSystem
	handle      xrdfs.FileHandle
	compression *xrdfs.FileCompression

	// path, mode and options are what the file was opened with. They are kept
	// because a request to an open file can be redirected, and the file then
	// has to be opened again at the server it was sent to: a handle means
	// nothing anywhere but where it was issued.
	path    string
	mode    xrdfs.OpenMode
	options xrdfs.OpenOptions

	mu        rsync.RWMutex
	info      *xrdfs.EntryStat
	sessionID string
}

// send issues req on the session sid on behalf of this file, re-opening the
// file wherever the request is redirected to.
func (f *file) send(ctx context.Context, sid string, resp xrdproto.Response, req xrdproto.Request) (string, error) {
	return f.fs.c.sendSessionFile(ctx, sid, resp, req, f)
}

// reopen implements reopener: it opens this file again at the server a request
// to it was redirected to, and reports the handle that server gave out along
// with the session the file is now open on.
//
// The options are the ones the file was opened with, with one change: a file
// opened with kXR_delete has already been truncated once, at the server the
// first open went to, and asking a second server to delete it would throw away
// whatever was written in between. It is opened for update instead, which is
// what the reference client does.
func (f *file) reopen(ctx context.Context, sessionID, opaque string) (xrdfs.FileHandle, string, error) {
	f.mu.RLock()
	path, mode, options := f.path, f.mode, f.options
	f.mu.RUnlock()

	if path == "" {
		return xrdfs.FileHandle{}, "", fmt.Errorf("xrootd: a request to an open file was redirected to %q, but the file was not opened by this client", sessionID)
	}
	if options&xrdfs.OpenOptionsDelete != 0 {
		options &^= xrdfs.OpenOptionsDelete
		options |= xrdfs.OpenOptionsOpenUpdate
	}

	req := open.NewRequest(path, mode, options)
	addOpaque(req, opaque)

	var resp open.Response
	id, err := f.fs.c.sendSession(ctx, sessionID, &resp, req)
	if err != nil {
		return xrdfs.FileHandle{}, "", fmt.Errorf("xrootd: could not re-open %q at %q after a redirect: %w", path, sessionID, err)
	}

	f.mu.Lock()
	f.handle = resp.FileHandle
	if resp.Compression != nil {
		f.compression = resp.Compression
	}
	if resp.Stat != nil {
		f.info = resp.Stat
	}
	f.sessionID = id
	f.mu.Unlock()

	return resp.FileHandle, id, nil
}

// Compression returns the compression info.
func (f *file) Compression() *xrdfs.FileCompression {
	return f.compression
}

// Info returns the cached stat info.
// Note that it may return nil if info was not yet fetched and info may be not up-to-date.
func (f *file) Info() *xrdfs.EntryStat {
	return f.info
}

// Handle returns the file handle.
func (f *file) Handle() xrdfs.FileHandle {
	return f.handle
}

// Close closes the file.
func (f *file) Close(ctx context.Context) error {
	return f.do(ctx, func(ctx context.Context, sid string) (string, error) {
		return f.send(ctx, sid, nil, &xrdclose.Request{Handle: f.handle})
	})
}

// CloseVerify closes the file and checks whether the file has the provided size.
// A zero size suppresses the verification.
func (f *file) CloseVerify(ctx context.Context, size int64) error {
	return f.do(ctx, func(ctx context.Context, sid string) (string, error) {
		return f.send(ctx, sid, nil, &xrdclose.Request{Handle: f.handle, Size: size})
	})
}

// Sync commits all pending writes to an open file.
func (f *file) Sync(ctx context.Context) error {
	return f.do(ctx, func(ctx context.Context, sid string) (string, error) {
		return f.send(ctx, sid, nil, &sync.Request{Handle: f.handle})
	})
}

// ReadAtContext reads len(p) bytes into p starting at offset off.
func (f *file) ReadAtContext(ctx context.Context, p []byte, off int64) (n int, err error) {
	resp := read.Response{Data: p}
	req := &read.Request{Handle: f.handle, Offset: off, Length: int32(len(p))}
	err = f.do(ctx, func(ctx context.Context, sid string) (string, error) {
		return f.send(ctx, sid, &resp, req)
	})
	if err != nil {
		return 0, err
	}
	return len(resp.Data), nil
}

// ReadAt reads len(p) bytes into p starting at offset off.
func (f *file) ReadAt(p []byte, off int64) (n int, err error) {
	return f.ReadAtContext(context.Background(), p, off)
}

// WriteAtContext writes len(p) bytes from p to the file at offset off.
func (f *file) WriteAtContext(ctx context.Context, p []byte, off int64) error {
	return f.do(ctx, func(ctx context.Context, sid string) (string, error) {
		return f.send(ctx, sid, nil, &write.Request{Handle: f.handle, Offset: off, Data: p})
	})
}

// WriteAt writes len(p) bytes from p to the file at offset off.
func (f *file) WriteAt(p []byte, off int64) (n int, err error) {
	err = f.WriteAtContext(context.Background(), p, off)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

// Truncate changes the size of the named file.
func (f *file) Truncate(ctx context.Context, size int64) error {
	return f.do(ctx, func(ctx context.Context, sid string) (string, error) {
		return f.send(ctx, sid, nil, &truncate.Request{Handle: f.handle, Size: size})
	})
}

// StatVirtualFS fetches the virtual fs stat info from the XRootD server.
//
// The request names this open file, and whether a kXR_stat carrying kXR_vfs may
// name a handle at all is unsettled: the stock server answers it as an ordinary
// stat of the file. See https://github.com/xrootd/xrootd/issues/728. Servers
// that do answer it, this package's own among them, report the storage holding
// the file. Use FileSystem.VirtualStat to ask about a path instead.
func (f *file) StatVirtualFS(ctx context.Context) (xrdfs.VirtualFSStat, error) {
	var resp stat.VirtualFSResponse
	err := f.do(ctx, func(ctx context.Context, sid string) (string, error) {
		return f.send(ctx, sid, &resp, &stat.Request{FileHandle: f.handle, Options: stat.OptionsVFS})
	})
	if err != nil {
		return xrdfs.VirtualFSStat{}, err
	}
	return resp.VirtualFSStat, nil
}

// Stat fetches the stat info of this file from the XRootD server.
// Note that Stat re-fetches value returned by the Info, so after the call to Stat
// calls to Info may return different value than before.
func (f *file) Stat(ctx context.Context) (xrdfs.EntryStat, error) {
	f.mu.RLock()
	sid := f.sessionID
	f.mu.RUnlock()

	var resp stat.DefaultResponse
	sid, err := f.send(ctx, sid, &resp, &stat.Request{FileHandle: f.handle})
	if err != nil {
		return xrdfs.EntryStat{}, err
	}

	f.mu.Lock()
	f.sessionID = sid
	f.info = &resp.EntryStat
	f.mu.Unlock()

	return resp.EntryStat, nil
}

// VerifyWriteAt writes len(p) bytes from p to the file at offset off using crc32 verification.
//
// The stock XRootD server does not implement kXR_verifyw and answers it as an
// unknown request; see https://github.com/xrootd/xrootd/issues/738. A caller
// that wants the write to land whatever the server supports should use WriteAt,
// which asks for no verification and is accepted everywhere.
func (f *file) VerifyWriteAt(ctx context.Context, p []byte, off int64) error {
	return f.do(ctx, func(ctx context.Context, sid string) (string, error) {
		return f.send(ctx, sid, nil, verifyw.NewRequestCRC32(f.handle, off, p))
	})
}

func (f *file) do(ctx context.Context, fct func(ctx context.Context, sid string) (string, error)) error {
	f.mu.RLock()
	sid := f.sessionID
	f.mu.RUnlock()

	id, err := fct(ctx, sid)
	if err != nil {
		return err
	}

	f.mu.Lock()
	f.sessionID = id
	f.mu.Unlock()

	return nil
}

// PgReadAt implements xrdfs.PgReader: it reads up to len(p) bytes into p
// starting at offset off using kXR_pgread, verifying the per-page CRC-32C
// of every page received.
func (f *file) PgReadAt(ctx context.Context, p []byte, off int64) (int, error) {
	var resp pgread.Response
	req := &pgread.Request{Handle: f.handle, Offset: off, ReadLength: int32(len(p))}
	err := f.do(ctx, func(ctx context.Context, sid string) (string, error) {
		return f.send(ctx, sid, &resp, req)
	})
	if err != nil {
		return 0, err
	}
	return copy(p, resp.Data), nil
}

// maxPgRetries is the number of times a single page is retransmitted before
// a pgwrite gives up. Corruption that survives this many independent attempts
// is not a transient wire error, and retrying forever would turn a broken
// link into a hang.
const maxPgRetries = 3

// PgWriteAt implements xrdfs.PgWriter: it writes p at offset off using
// kXR_pgwrite, attaching a CRC-32C to every page sent.
//
// The server verifies each page as it arrives and stores it regardless,
// reporting any page whose CRC-32C did not match. Those pages hold corrupt
// data on the server until they are retransmitted, so PgWriteAt resends each
// one with the kXR_pgRetry flag and fails if any stays corrupt. A successful
// return therefore means every page is intact on the server, not merely that
// the request was delivered.
func (f *file) PgWriteAt(ctx context.Context, p []byte, off int64) error {
	corrupt, err := f.pgWriteOnce(ctx, p, off, 0)
	if err != nil {
		return err
	}
	for _, pgoff := range corrupt {
		if err := f.pgRetryPage(ctx, p, off, pgoff); err != nil {
			return err
		}
	}
	return nil
}

// pgWriteOnce sends a single kXR_pgwrite and returns the file offsets of the
// pages the server reports as corrupt.
func (f *file) pgWriteOnce(ctx context.Context, p []byte, off int64, flags uint8) ([]int64, error) {
	var resp pgwrite.Response
	req := &pgwrite.Request{Handle: f.handle, Offset: off, Data: p, Flags: flags}
	err := f.do(ctx, func(ctx context.Context, sid string) (string, error) {
		return f.send(ctx, sid, &resp, req)
	})
	if err != nil {
		return nil, err
	}
	return resp.Corrupt, nil
}

// pgRetryPage retransmits the single page at file offset pgoff, sliced out of
// data (which starts at file offset base), until the server accepts it or the
// retry budget runs out.
func (f *file) pgRetryPage(ctx context.Context, data []byte, base, pgoff int64) error {
	doff := pgoff - base
	if doff < 0 || doff >= int64(len(data)) {
		return fmt.Errorf("xrootd: pgwrite reported a corrupt page at offset %d, outside the %d-byte request at offset %d", pgoff, len(data), base)
	}
	page := data[doff : doff+int64(pgbuf.PageSpan(pgoff, len(data)-int(doff)))]

	for range maxPgRetries {
		corrupt, err := f.pgWriteOnce(ctx, page, pgoff, pgwrite.Retry)
		if err != nil {
			return err
		}
		if len(corrupt) == 0 {
			return nil
		}
	}
	return fmt.Errorf("xrootd: pgwrite: page at offset %d still corrupt after %d retries", pgoff, maxPgRetries)
}

// ReadVAt implements xrdfs.VectorReader: it reads all of segs in a single
// kXR_readv round trip and returns their data in the order requested.
//
// The reply interleaves an echo header with each segment's bytes, and the
// length in that header is what was actually read. A server that stops early
// simply sends fewer segments, so a reply that does not account for every
// requested segment — with exactly the bytes asked for, at the offset asked
// for — is an error rather than a short read: the caller has no way to tell
// which of its ranges the returned data belongs to otherwise.
func (f *file) ReadVAt(ctx context.Context, segs []xrdfs.ReadVSegment) ([][]byte, error) {
	req := &readv.Request{Segments: make([]readv.Segment, len(segs))}
	for i, seg := range segs {
		if seg.Length < 0 || int64(seg.Length) > math.MaxInt32 {
			return nil, fmt.Errorf("xrootd: readv segment %d has an invalid length of %d", i, seg.Length)
		}
		req.Segments[i] = readv.Segment{
			Handle: f.handle,
			Length: int32(seg.Length),
			Offset: seg.Offset,
		}
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	var resp readv.Response
	err := f.do(ctx, func(ctx context.Context, sid string) (string, error) {
		return f.send(ctx, sid, &resp, req)
	})
	if err != nil {
		return nil, err
	}

	if len(resp.Chunks) != len(segs) {
		return nil, fmt.Errorf("xrootd: readv asked for %d segments and got %d back", len(segs), len(resp.Chunks))
	}
	out := make([][]byte, len(segs))
	for i, c := range resp.Chunks {
		switch {
		case c.Handle != f.handle:
			return nil, fmt.Errorf("xrootd: readv segment %d came back for handle %v, not %v", i, c.Handle, f.handle)
		case c.Offset != segs[i].Offset:
			return nil, fmt.Errorf("xrootd: readv segment %d came back for offset %d, not %d", i, c.Offset, segs[i].Offset)
		case len(c.Data) != segs[i].Length:
			return nil, fmt.Errorf("xrootd: readv segment %d came back with %d bytes, not the %d asked for", i, len(c.Data), segs[i].Length)
		}
		out[i] = c.Data
	}
	return out, nil
}

// WriteVAt implements xrdfs.VectorWriter: it writes all of segs in a single
// kXR_writev round trip. The server applies every segment or none of them.
func (f *file) WriteVAt(ctx context.Context, segs []xrdfs.WriteVSegment) error {
	req := &writev.Request{Segments: make([]writev.Segment, len(segs))}
	for i, seg := range segs {
		if int64(len(seg.Data)) > math.MaxInt32 {
			return fmt.Errorf("xrootd: writev segment %d of %d bytes is too large for one segment", i, len(seg.Data))
		}
		req.Segments[i] = writev.Segment{
			Handle: f.handle,
			Offset: seg.Offset,
			Data:   seg.Data,
		}
	}
	if err := req.Validate(); err != nil {
		return err
	}

	return f.do(ctx, func(ctx context.Context, sid string) (string, error) {
		return f.send(ctx, sid, nil, req)
	})
}

// Visa returns the visa attributes of the open file (query kXR_Qvisa).
//
// A visa is whatever the storage system wants to say about this handle that
// the protocol has no field for — which pool the file came from, how it was
// staged, what an experiment's own plugin recorded against it. It is asked of
// the handle rather than the path, so it describes the file this client has
// open and not whatever a later lookup of the name would find. The answer is
// site-defined text, so it is returned unparsed.
func (f *file) Visa(ctx context.Context) (string, error) {
	var resp query.Response
	req := &query.Request{Query: query.Visa, Handle: f.handle}
	err := f.do(ctx, func(ctx context.Context, sid string) (string, error) {
		return f.send(ctx, sid, &resp, req)
	})
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(resp.Data), "\x00"), nil
}

var (
	_ xrdfs.File         = (*file)(nil)
	_ xrdfs.PgReader     = (*file)(nil)
	_ xrdfs.PgWriter     = (*file)(nil)
	_ xrdfs.VectorReader = (*file)(nil)
	_ xrdfs.VectorWriter = (*file)(nil)
	_ xrdfs.VisaFile     = (*file)(nil)
)
