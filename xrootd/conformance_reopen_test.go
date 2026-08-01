// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"encoding/binary"
	"net"
	"strconv"
	"sync"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/open"
	"go-hep.org/x/hep/xrootd/xrdproto/read"
	"go-hep.org/x/hep/xrootd/xrdproto/stat"
	"go-hep.org/x/hep/xrootd/xrdproto/truncate"
)

// redirectBody builds the body of a kXR_redirect: a 4-byte port, then the host,
// then the opaque data the client is to carry with the re-issued request.
func redirectBody(t *testing.T, addr, opaque string) []byte {
	t.Helper()

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("could not split %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("could not parse the port in %q: %v", addr, err)
	}

	body := binary.BigEndian.AppendUint32(nil, uint32(port))
	url := host
	if opaque != "" {
		url += "?" + opaque
	}
	return append(body, url...)
}

// openHost is a server that hands out one file handle and then answers, or
// redirects, whatever is asked of that handle. It records every open it was
// asked for so a test can say what the client did on its way back.
type openHost struct {
	addr string

	// handle is what an open on this host returns.
	handle xrdfs.FileHandle
	// redirect, when set, is the body of the kXR_redirect every request other
	// than an open is answered with.
	redirect []byte
	// data is what a read on this host answers with, when it answers.
	data []byte

	mu      sync.Mutex
	opens   []open.Request
	handles []xrdfs.FileHandle // the handles requests arrived with
	paths   []string           // the paths requests named instead of a handle
}

func newOpenHost(t *testing.T, handle xrdfs.FileHandle) *openHost {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	h := &openHost{addr: ln.Addr().String(), handle: handle}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go h.serve(conn)
		}
	}()
	return h
}

func (h *openHost) serve(conn net.Conn) {
	defer conn.Close()

	if _, ok := redirBootstrap(conn); !ok {
		return
	}
	for {
		hdr, body := readBootstrapRequest(conn)
		if body == nil {
			return
		}

		// An open is always answered: it is how a file comes to be on this
		// host at all. Everything else is redirected when the host is set up
		// to redirect.
		if hdr.RequestID == open.RequestID {
			var req open.Request
			_ = req.UnmarshalXrd(xrdenc.NewRBuffer(body))
			h.mu.Lock()
			h.opens = append(h.opens, req)
			h.mu.Unlock()
			writeBootstrapResponse(conn, hdr.StreamID, open.Response{FileHandle: h.handle})
			continue
		}

		switch hdr.RequestID {
		case read.RequestID:
			var req read.Request
			_ = req.UnmarshalXrd(xrdenc.NewRBuffer(body))
			h.record(req.Handle, "")
		case truncate.RequestID:
			var req truncate.Request
			_ = req.UnmarshalXrd(xrdenc.NewRBuffer(body))
			h.record(req.Handle, req.Path)
		case stat.RequestID:
			var req stat.Request
			_ = req.UnmarshalXrd(xrdenc.NewRBuffer(body))
			h.record(req.FileHandle, req.Path)
		}

		if h.redirect != nil {
			_ = xrdproto.WriteResponse(conn, hdr.StreamID, xrdproto.Redirect, rawBody(h.redirect))
			continue
		}

		switch hdr.RequestID {
		case read.RequestID:
			writeBootstrapResponse(conn, hdr.StreamID, read.Response{Data: h.data})
		case stat.RequestID:
			writeBootstrapResponse(conn, hdr.StreamID, &stat.DefaultResponse{
				EntryStat: xrdfs.EntryStat{HasStatInfo: true, EntrySize: 42},
			})
		default:
			writeBootstrapResponse(conn, hdr.StreamID, rawBody(nil))
		}
	}
}

func (h *openHost) record(handle xrdfs.FileHandle, path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handles = append(h.handles, handle)
	if path != "" {
		h.paths = append(h.paths, path)
	}
}

func (h *openHost) seen() (opens []open.Request, handles []xrdfs.FileHandle) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]open.Request(nil), h.opens...), append([]xrdfs.FileHandle(nil), h.handles...)
}

func TestConformance_ARedirectedFileIsOpenedWhereItWasSent(t *testing.T) {
	// A file handle is a name for state on the server that issued it, and the
	// server a request is redirected to has none of that state. Re-sending the
	// request with the old handle would name a file the new server never
	// opened — at best an error, at worst somebody else's file.
	var (
		first  = xrdfs.FileHandle{0x01, 0x02, 0x03, 0x04}
		second = xrdfs.FileHandle{0xaa, 0xbb, 0xcc, 0xdd}
	)

	tgt := newOpenHost(t, second)
	tgt.data = []byte("the bytes the second server holds")

	origin := newOpenHost(t, first)
	origin.redirect = redirectBody(t, tgt.addr, "auth=abc")

	cli, ctx := redirClient(t, origin.addr)

	f, err := cli.FS().Open(ctx, "/data/run42.root", xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
	if err != nil {
		t.Fatalf("could not open the file: %v", err)
	}

	p := make([]byte, len(tgt.data))
	n, err := f.ReadAtContext(ctx, p, 0)
	if err != nil {
		t.Fatalf("could not read the file: %v", err)
	}
	if got := string(p[:n]); got != string(tgt.data) {
		t.Fatalf("read %q, want %q", got, tgt.data)
	}

	opens, handles := tgt.seen()
	if len(opens) != 1 {
		t.Fatalf("the file was opened %d times at the server the read was sent to, want once", len(opens))
	}
	if got, want := opens[0].Path, "/data/run42.root?auth=abc"; got != want {
		// The opaque data a redirect carries is the authorization to open the
		// file there; an open without it is refused by a real server.
		t.Fatalf("the file was re-opened as %q, want %q", got, want)
	}
	if got, want := opens[0].Options, xrdfs.OpenOptionsOpenRead; got != want {
		t.Fatalf("the file was re-opened with options %#04x, want the ones it was opened with (%#04x)", got, want)
	}
	if len(handles) != 1 || handles[0] != second {
		t.Fatalf("the read arrived with handles %v, want the one this server issued (%v)", handles, second)
	}

	// The file now lives at the second server, and what it reports as its
	// handle has to be the one that server knows it by.
	if got := f.Handle(); got != second {
		t.Fatalf("the file holds handle %v, want %v", got, second)
	}
}

func TestConformance_ARedirectedFileIsNotDeletedTwice(t *testing.T) {
	// kXR_delete truncates the file as it opens it. The first open has already
	// done that, and whatever was written since is the caller's data: asking a
	// second server to delete it again would throw it away. The reference
	// client opens for update instead, and so does this one.
	tgt := newOpenHost(t, xrdfs.FileHandle{0x09, 0x09, 0x09, 0x09})

	origin := newOpenHost(t, xrdfs.FileHandle{0x01, 0x01, 0x01, 0x01})
	origin.redirect = redirectBody(t, tgt.addr, "")

	cli, ctx := redirClient(t, origin.addr)

	f, err := cli.FS().Open(ctx, "/data/out.root", xrdfs.OpenModeOwnerWrite, xrdfs.OpenOptionsDelete)
	if err != nil {
		t.Fatalf("could not open the file: %v", err)
	}
	if err := f.Truncate(ctx, 0); err != nil {
		t.Fatalf("could not truncate the file: %v", err)
	}

	opens, _ := tgt.seen()
	if len(opens) != 1 {
		t.Fatalf("the file was opened %d times at the second server, want once", len(opens))
	}
	if opens[0].Options&xrdfs.OpenOptionsDelete != 0 {
		t.Fatal("the file was re-opened for deletion, discarding what the first server was given")
	}
	if opens[0].Options&xrdfs.OpenOptionsOpenUpdate == 0 {
		t.Fatalf("the file was re-opened with options %#04x, which cannot be written to", opens[0].Options)
	}
}

func TestConformance_ARedirectedPathIsNotReopened(t *testing.T) {
	// A kXR_stat names either an open file or a path, and one of those has
	// nothing to re-open. Opening a file to answer a request that named a path
	// would leave a file open on the server that nobody ever closes, and would
	// fail outright where the caller has no right to open it.
	tgt := newOpenHost(t, xrdfs.FileHandle{0x07, 0x07, 0x07, 0x07})

	origin := newOpenHost(t, xrdfs.FileHandle{0x01, 0x01, 0x01, 0x01})
	origin.redirect = redirectBody(t, tgt.addr, "")

	cli, ctx := redirClient(t, origin.addr)
	if _, err := cli.FS().Stat(ctx, "/data/run42.root"); err != nil {
		t.Fatalf("could not stat the file: %v", err)
	}

	opens, _ := tgt.seen()
	if len(opens) != 0 {
		t.Fatalf("a request that named a path opened %d files on the server it was redirected to", len(opens))
	}

	tgt.mu.Lock()
	paths := append([]string(nil), tgt.paths...)
	tgt.mu.Unlock()
	if len(paths) != 1 || paths[0] != "/data/run42.root" {
		t.Fatalf("the redirected stat named %v, want the path the caller asked about", paths)
	}
}
