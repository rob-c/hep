// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for parallel data connections (kXR_bind sub-streams).
//
// Bulk data does not have to travel on the connection the requests travel on.
// A client binds a second connection to the same session, the server hands it a
// path id, and a write that names that id puts its header on the request
// connection and its bytes on the bound one. That is what keeps a large write
// from blocking every other request to the same server behind it, and it is a
// two-socket protocol exchange that nothing else in the suite exercises: the
// header and the payload of one request are read from two different sockets,
// and a client that gets the split wrong writes a file full of nothing.

package xrootd

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/bind"
	"go-hep.org/x/hep/xrootd/xrdproto/login"
	"go-hep.org/x/hep/xrootd/xrdproto/open"
	"go-hep.org/x/hep/xrootd/xrdproto/protocol"
	"go-hep.org/x/hep/xrootd/xrdproto/write"
)

// substreamServer accepts as many connections as the client cares to make: the
// first is the request connection, and every later one arrives with a kXR_bind
// asking to become a data path for the same session.
type substreamServer struct {
	addr   string
	handle xrdfs.FileHandle
	// refuseBind makes every kXR_bind fail, as a server that has run out of
	// connections does.
	refuseBind bool

	mu      sync.Mutex
	binds   int
	subs    map[xrdproto.PathID]net.Conn
	pathIDs []xrdproto.PathID // the path id each write named
	written []byte            // the bytes the writes delivered, in order
}

func newSubstreamServer(t *testing.T) *substreamServer {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	srv := &substreamServer{
		addr:   ln.Addr().String(),
		handle: xrdfs.FileHandle{0x11, 0x22, 0x33, 0x44},
		subs:   make(map[xrdproto.PathID]net.Conn),
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.serve(t, conn)
		}
	}()
	return srv
}

// substreamFrame reads one request frame: the 4-byte header, the 16 parameter
// bytes and the declared length. The payload is deliberately left on the socket
// — for a write it is not on this socket at all.
func substreamFrame(conn net.Conn) (hdr xrdproto.RequestHeader, params []byte, dlen int, err error) {
	buf := make([]byte, xrdproto.RequestHeaderLength+16+4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return hdr, nil, 0, err
	}
	if err := hdr.UnmarshalXrd(xrdenc.NewRBuffer(buf[:xrdproto.RequestHeaderLength])); err != nil {
		return hdr, nil, 0, err
	}
	params = buf[xrdproto.RequestHeaderLength : len(buf)-4]
	dlen = int(binary.BigEndian.Uint32(buf[len(buf)-4:]))
	return hdr, params, dlen, nil
}

func (srv *substreamServer) serve(t *testing.T, conn net.Conn) {
	// A connection that becomes a data path is left open and unread: what
	// arrives on it next is the payload of a write, and the handler for the
	// request connection is what reads it.
	bound := false
	defer func() {
		if !bound {
			_ = conn.Close()
		}
	}()

	bootHandshake(t, conn)

	for {
		hdr, params, dlen, err := substreamFrame(conn)
		if err != nil {
			return
		}
		payload := make([]byte, dlen)

		switch hdr.RequestID {
		case protocol.RequestID:
			if _, err := io.ReadFull(conn, payload); err != nil {
				return
			}
			writeBootstrapResponse(conn, hdr.StreamID, protocol.Response{
				BinaryProtocolVersion: 0x310,
				Flags:                 protocol.IsServer,
			})

		case login.RequestID:
			if _, err := io.ReadFull(conn, payload); err != nil {
				return
			}
			writeBootstrapResponse(conn, hdr.StreamID, login.Response{})

		case bind.RequestID:
			if _, err := io.ReadFull(conn, payload); err != nil {
				return
			}
			srv.mu.Lock()
			srv.binds++
			refuse := srv.refuseBind
			pathID := xrdproto.PathID(srv.binds)
			if !refuse {
				srv.subs[pathID] = conn
			}
			srv.mu.Unlock()
			if refuse {
				bootErrorFrame(conn, hdr.StreamID, "no more connections")
				continue
			}
			writeBootstrapResponse(conn, hdr.StreamID, bind.Response{PathID: pathID})
			bound = true
			return

		case open.RequestID:
			if _, err := io.ReadFull(conn, payload); err != nil {
				return
			}
			writeBootstrapResponse(conn, hdr.StreamID, open.Response{FileHandle: srv.handle})

		case write.RequestID:
			// The path id says which socket the bytes are on: this one when it
			// is zero, and the connection that was bound to it otherwise.
			pathID := xrdproto.PathID(params[12])
			from := conn
			if pathID != 0 {
				srv.mu.Lock()
				from = srv.subs[pathID]
				srv.mu.Unlock()
				if from == nil {
					t.Errorf("a write named path %d, which was never bound", pathID)
					return
				}
			}
			if _, err := io.ReadFull(from, payload); err != nil {
				t.Errorf("could not read the %d bytes the write declared: %v", dlen, err)
				return
			}
			srv.mu.Lock()
			srv.pathIDs = append(srv.pathIDs, pathID)
			srv.written = append(srv.written, payload...)
			srv.mu.Unlock()
			writeBootstrapResponse(conn, hdr.StreamID, rawBody(nil))

		default:
			if _, err := io.ReadFull(conn, payload); err != nil {
				return
			}
			writeBootstrapResponse(conn, hdr.StreamID, rawBody(nil))
		}
	}
}

func (srv *substreamServer) seen() (binds int, pathIDs []xrdproto.PathID, written []byte) {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	return srv.binds, append([]xrdproto.PathID(nil), srv.pathIDs...), append([]byte(nil), srv.written...)
}

// substreamClient connects to srv and opens a file for writing.
func substreamClient(t *testing.T, srv *substreamServer, opts ...Option) (xrdfs.File, context.Context) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)

	client, err := NewClient(ctx, srv.addr, "gopher", opts...)
	if err != nil {
		t.Fatalf("could not connect: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	f, err := client.FS().Open(ctx, "/data/out.root", xrdfs.OpenModeOwnerWrite, xrdfs.OpenOptionsOpenUpdate)
	if err != nil {
		t.Fatalf("could not open the file: %v", err)
	}
	return f, ctx
}

func TestConformance_AWriteTravelsOverABoundDataConnection(t *testing.T) {
	// The split is the whole point of kXR_bind: the request goes one way and
	// the bytes go another, so a multi-gigabyte write does not sit in front of
	// every stat and close queued behind it on the same socket.
	srv := newSubstreamServer(t)
	f, ctx := substreamClient(t, srv)

	data := bytes.Repeat([]byte("payload!"), 64)
	if err := f.WriteAtContext(ctx, data, 0); err != nil {
		t.Fatalf("could not write: %v", err)
	}

	binds, pathIDs, written := srv.seen()
	if binds != 1 {
		t.Fatalf("the client bound %d data connections, want one", binds)
	}
	if len(pathIDs) != 1 || pathIDs[0] == 0 {
		t.Fatalf("the write named path ids %v, want one that is not the request connection", pathIDs)
	}
	if !bytes.Equal(written, data) {
		t.Fatalf("the server received %d bytes, want the %d that were written", len(written), len(data))
	}
}

func TestConformance_ABoundDataConnectionIsReused(t *testing.T) {
	// A data connection costs a socket and a bind round trip. Opening one per
	// write would make a loop of small writes slower than not binding at all,
	// so a path that has been released goes back to the pool.
	srv := newSubstreamServer(t)
	f, ctx := substreamClient(t, srv)

	for i := range 3 {
		if err := f.WriteAtContext(ctx, []byte("some bytes"), int64(i*10)); err != nil {
			t.Fatalf("could not write: %v", err)
		}
	}

	binds, pathIDs, written := srv.seen()
	if binds != 1 {
		t.Fatalf("three sequential writes bound %d data connections, want one", binds)
	}
	if len(pathIDs) != 3 {
		t.Fatalf("the server saw %d writes, want 3", len(pathIDs))
	}
	for _, id := range pathIDs {
		if id != pathIDs[0] {
			t.Fatalf("the writes were spread over paths %v, want the one that was already bound", pathIDs)
		}
	}
	if got, want := string(written), "some bytessome bytessome bytes"; got != want {
		t.Fatalf("the server received %q, want %q", got, want)
	}
}

func TestConformance_WithoutSubStreamsTheDataSharesTheRequestConnection(t *testing.T) {
	// A caller who wants one connection per server gets exactly that: no bind
	// is attempted and the write carries its own bytes, which is what the C++
	// client does out of the box. A client that bound anyway would double the
	// connection count of every job at a site that configured against it.
	srv := newSubstreamServer(t)
	f, ctx := substreamClient(t, srv, WithSubStreams(0))

	data := []byte("all on one socket")
	if err := f.WriteAtContext(ctx, data, 0); err != nil {
		t.Fatalf("could not write: %v", err)
	}

	binds, pathIDs, written := srv.seen()
	if binds != 0 {
		t.Fatalf("a client told to open no data connections bound %d", binds)
	}
	if len(pathIDs) != 1 || pathIDs[0] != 0 {
		t.Fatalf("the write named path ids %v, want the request connection (0)", pathIDs)
	}
	if !bytes.Equal(written, data) {
		t.Fatalf("the server received %q, want %q", written, data)
	}
}

func TestConformance_AServerThatWillNotBindStillGetsTheWrite(t *testing.T) {
	// A data connection is an optimisation. A server that has no spare
	// connection to give — or has kXR_bind turned off entirely — must not cost
	// the caller their write: the bytes go inline on the request connection,
	// which is where they would have gone had the client never asked.
	//
	// It is still worth reporting, because a site whose binds are all refused
	// looks from the outside like a link that will not go fast.
	srv := newSubstreamServer(t)
	srv.refuseBind = true

	reported := make(chan error, 8)
	f, ctx := substreamClient(t, srv, WithErrorHandler(func(err error) {
		select {
		case reported <- err:
		default:
		}
	}))

	data := []byte("inline after all")
	if err := f.WriteAtContext(ctx, data, 0); err != nil {
		t.Fatalf("a write failed because the server would not bind a data connection: %v", err)
	}

	_, pathIDs, written := srv.seen()
	if len(pathIDs) != 1 || pathIDs[0] != 0 {
		t.Fatalf("the write named path ids %v, want the request connection (0)", pathIDs)
	}
	if !bytes.Equal(written, data) {
		t.Fatalf("the server received %q, want %q", written, data)
	}

	select {
	case err := <-reported:
		if !strings.Contains(err.Error(), "data connection") {
			t.Fatalf("the report does not say what was refused: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a refused data connection was not reported")
	}
}

func TestConformance_WithoutSubStreamsNothingIsReported(t *testing.T) {
	// Asking for no data connections is a decision, not a fault. A client that
	// reported one per write would fill a log with a refusal nobody made.
	srv := newSubstreamServer(t)

	reported := make(chan error, 8)
	f, ctx := substreamClient(t, srv,
		WithSubStreams(0),
		WithErrorHandler(func(err error) {
			select {
			case reported <- err:
			default:
			}
		}),
	)

	for i := range 3 {
		if err := f.WriteAtContext(ctx, []byte("bytes"), int64(i*5)); err != nil {
			t.Fatalf("could not write: %v", err)
		}
	}

	select {
	case err := <-reported:
		t.Fatalf("a client configured for no data connections reported: %v", err)
	default:
	}
}
