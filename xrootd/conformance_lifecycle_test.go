// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the server's connection lifecycle, over a real socket.
//
// A storage server is judged on what it does when things go wrong, because a
// client that is misbehaving is indistinguishable from a client that has
// crashed, and both arrive constantly. The properties below are the ones that
// keep a server up: a connection that fails its handshake is dropped rather
// than fed into the request loop, a client that vanishes is not an error, a
// request the server cannot handle is still *answered* so the caller is not
// left waiting on a stream that will never be written, and shutting down
// releases both the listener and everything still connected to it.

package xrootd_test

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/handshake"
	"go-hep.org/x/hep/xrootd/xrdproto/ping"
)

// errorSink collects what a Server reports to its ErrorHandler. A server
// handles each connection in its own goroutine, so the collection has to be
// safe to read from the test's goroutine.
type errorSink struct {
	mu   sync.Mutex
	errs []error
	gotc chan struct{}
}

func newErrorSink() *errorSink {
	return &errorSink{gotc: make(chan struct{}, 16)}
}

func (s *errorSink) handle(err error) {
	s.mu.Lock()
	s.errs = append(s.errs, err)
	s.mu.Unlock()
	select {
	case s.gotc <- struct{}{}:
	default:
	}
}

// wait blocks until the server has reported at least one error.
func (s *errorSink) wait(t *testing.T) error {
	t.Helper()

	select {
	case <-s.gotc:
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not report anything")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.errs[len(s.errs)-1]
}

// serveTCP starts a server on a loopback port and returns its address. The
// listener is a real socket rather than a pipe: Shutdown has to close the thing
// that is accepting, and a pipe has nothing to close.
func serveTCP(t *testing.T, errorHandler xrootd.ErrorHandler) (*xrootd.Server, string) {
	t.Helper()

	return serveHandler(t, xrootd.NewFSHandler(t.TempDir()), errorHandler)
}

// serveHandler is serveTCP with the request handler chosen by the caller.
func serveHandler(t *testing.T, handler xrootd.Handler, errorHandler xrootd.ErrorHandler) (*xrootd.Server, string) {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}

	srv := xrootd.NewServer(handler, errorHandler)
	done := make(chan error, 1)
	go func() { done <- srv.Serve(l) }()

	t.Cleanup(func() {
		_ = srv.Shutdown(context.Background())
		if err := <-done; err != nil && !errors.Is(err, xrootd.ErrServerClosed) {
			t.Errorf("the server stopped with %v", err)
		}
	})

	return srv, l.Addr().String()
}

// dialServer opens a connection to addr and closes it when the test ends.
func dialServer(t *testing.T, addr string) *net.TCPConn {
	t.Helper()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("could not dial %q: %v", addr, err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("could not set the deadline: %v", err)
	}
	return conn.(*net.TCPConn)
}

// clientHandshake performs the initial exchange every other request depends on.
func clientHandshake(t *testing.T, conn net.Conn) {
	t.Helper()

	var w xrdenc.WBuffer
	if err := handshake.NewRequest().MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal the handshake: %v", err)
	}
	if _, err := conn.Write(w.Bytes()); err != nil {
		t.Fatalf("could not send the handshake: %v", err)
	}
	if _, _, err := xrdproto.ReadResponse(conn); err != nil {
		t.Fatalf("could not read the handshake response: %v", err)
	}
}

// sendRequest writes a request header and body under the given stream ID.
func sendRequest(t *testing.T, conn net.Conn, id xrdproto.StreamID, requestID uint16, req xrdproto.Marshaler) {
	t.Helper()

	var w xrdenc.WBuffer
	hdr := xrdproto.RequestHeader{StreamID: id, RequestID: requestID}
	if err := hdr.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal the request header: %v", err)
	}
	if err := req.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal the request: %v", err)
	}
	if _, err := conn.Write(w.Bytes()); err != nil {
		t.Fatalf("could not send the request: %v", err)
	}
}

func TestConformance_AMalformedHandshakeAbortsTheConnection(t *testing.T) {
	// The handshake is the only thing standing between the request loop and
	// whatever else is scanning the port. A server that fell through to the loop
	// would be parsing arbitrary bytes as requests; one that hung on to the
	// connection would leak a goroutine and a descriptor per probe.
	for _, tc := range []struct {
		name string
		send []byte
	}{
		{"the right length, the wrong content", make([]byte, handshake.RequestLength)},
		{"a handshake that stops half way", make([]byte, handshake.RequestLength/2)},
		{"a single byte", []byte{0x00}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sink := newErrorSink()
			_, addr := serveTCP(t, sink.handle)
			conn := dialServer(t, addr)

			if _, err := conn.Write(tc.send); err != nil {
				t.Fatalf("could not send: %v", err)
			}
			// A partial handshake only completes once the peer is gone, so the
			// write half has to be closed for the server to see the EOF.
			if err := conn.CloseWrite(); err != nil {
				t.Fatalf("could not half-close: %v", err)
			}

			if err := sink.wait(t); err == nil {
				t.Fatal("the server accepted a malformed handshake")
			}

			if _, err := io.ReadAll(conn); err != nil {
				t.Fatalf("could not drain the connection: %v", err)
			}
			if _, err := conn.Read(make([]byte, 1)); err != io.EOF {
				t.Fatalf("the server left the connection open, reading it gave %v", err)
			}
		})
	}
}

func TestConformance_AServerWithNoErrorHandlerStillRefusesABadHandshake(t *testing.T) {
	// NewServer documents that a nil ErrorHandler means "discard". An embedder
	// that takes it at its word must not get a nil dereference on the first
	// port scan.
	_, addr := serveTCP(t, nil)
	conn := dialServer(t, addr)

	if _, err := conn.Write(make([]byte, handshake.RequestLength)); err != nil {
		t.Fatalf("could not send: %v", err)
	}
	if _, err := io.ReadAll(conn); err != nil {
		t.Fatalf("could not drain the connection: %v", err)
	}
	if _, err := conn.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("the server left the connection open, reading it gave %v", err)
	}
}

func TestConformance_AClientThatVanishesIsNotAnError(t *testing.T) {
	// Clients disappear all the time: a job is killed, a worker node reboots, a
	// network partition heals the wrong way. Logging each one as a server error
	// buries the failures that matter, so a clean EOF has to be silent — and the
	// server has to keep serving.
	sink := newErrorSink()
	_, addr := serveTCP(t, sink.handle)

	first := dialServer(t, addr)
	clientHandshake(t, first)
	if err := first.Close(); err != nil {
		t.Fatalf("could not close the connection: %v", err)
	}

	// The next client is the proof the server is still there.
	second := dialServer(t, addr)
	clientHandshake(t, second)
	sendRequest(t, second, xrdproto.StreamID{0, 1}, ping.RequestID, ping.Request{})

	hdr, _, err := xrdproto.ReadResponse(second)
	if err != nil {
		t.Fatalf("could not read the ping response: %v", err)
	}
	if hdr.Status != xrdproto.Ok {
		t.Fatalf("the ping answered %v", hdr.Status)
	}

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.errs) != 0 {
		t.Fatalf("a client that hung up was reported as %v", sink.errs)
	}
}

func TestConformance_ARequestTheServerCannotHandleIsStillAnswered(t *testing.T) {
	// Every request is answered on the stream it arrived on, and a client blocks
	// on that stream until something comes back. Dropping the response to a
	// request the server does not understand does not fail the request — it
	// hangs the client, until whatever timeout it has, if it has one.
	_, addr := serveTCP(t, func(error) {})
	conn := dialServer(t, addr)
	clientHandshake(t, conn)

	const unknown = 0xffff
	id := xrdproto.StreamID{0x1a, 0x2b}
	sendRequest(t, conn, id, unknown, ping.Request{})

	hdr, data, err := xrdproto.ReadResponse(conn)
	if err != nil {
		t.Fatalf("could not read the response: %v", err)
	}
	if hdr.StreamID != id {
		t.Fatalf("the response came back on stream %v, want %v", hdr.StreamID, id)
	}
	if hdr.Status != xrdproto.Error {
		t.Fatalf("an unknown request answered %v", hdr.Status)
	}

	var srvErr xrdproto.ServerError
	if err := srvErr.UnmarshalXrd(xrdenc.NewRBuffer(data)); err != nil {
		t.Fatalf("could not unmarshal the failure: %v", err)
	}
	if srvErr.Code != xrdproto.InvalidRequest {
		t.Fatalf("the failure is coded %v, want %v", srvErr.Code, xrdproto.InvalidRequest)
	}
}

func TestConformance_EveryResponseCarriesTheStreamIDOfItsRequest(t *testing.T) {
	// The server answers requests concurrently, so responses need not arrive in
	// the order the requests were sent. The stream ID is the only thing tying an
	// answer to its question; if the server reused one or invented one, a client
	// would hand a stat response to a read.
	_, addr := serveTCP(t, func(error) {})
	conn := dialServer(t, addr)
	clientHandshake(t, conn)

	want := map[xrdproto.StreamID]bool{
		{0, 1}:       true,
		{0x7f, 0x10}: true,
		{0xff, 0xfe}: true,
	}
	for id := range want {
		sendRequest(t, conn, id, ping.RequestID, ping.Request{})
	}

	for range want {
		hdr, _, err := xrdproto.ReadResponse(conn)
		if err != nil {
			t.Fatalf("could not read a response: %v", err)
		}
		if hdr.Status != xrdproto.Ok {
			t.Fatalf("a ping on stream %v answered %v", hdr.StreamID, hdr.Status)
		}
		if !want[hdr.StreamID] {
			t.Fatalf("a response came back on stream %v, which was never asked", hdr.StreamID)
		}
		delete(want, hdr.StreamID)
	}
}

func TestConformance_ShutdownClosesTheListenerAndWhatIsConnectedToIt(t *testing.T) {
	// Shutdown is what a server does when it is being replaced, and the point is
	// to release the port. A listener left open holds the port against the
	// incoming process; a connection left open holds a client that will never be
	// answered again.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}
	addr := l.Addr().String()

	srv := xrootd.NewServer(xrootd.NewFSHandler(t.TempDir()), func(error) {})
	done := make(chan error, 1)
	go func() { done <- srv.Serve(l) }()

	conn := dialServer(t, addr)
	clientHandshake(t, conn)

	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("could not shut the server down: %v", err)
	}

	// Serve reports the shutdown as such, so an embedder can tell it apart from
	// the listener failing on its own.
	if err := <-done; !errors.Is(err, xrootd.ErrServerClosed) {
		t.Fatalf("Serve returned %v, want %v", err, xrootd.ErrServerClosed)
	}

	// A reset is as good as an EOF here: either way the connection is gone.
	if _, err := io.ReadAll(conn); err != nil {
		t.Logf("draining the connection gave %v", err)
	}

	// The port is free, so nothing is listening on it any more.
	next, err := net.DialTimeout("tcp", addr, time.Second)
	if err == nil {
		next.Close()
		t.Fatal("the server is still accepting connections after Shutdown")
	}
}

func TestConformance_AListenerThatFailsOnItsOwnIsReported(t *testing.T) {
	// The other way a Serve loop ends. An embedder restarts on this and exits on
	// ErrServerClosed, so conflating the two either leaks a dead server or
	// resurrects one that was deliberately stopped.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}

	srv := xrootd.NewServer(xrootd.NewFSHandler(t.TempDir()), func(error) {})
	done := make(chan error, 1)
	go func() { done <- srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	// Closing the listener behind the server's back is an accept failing for a
	// reason that is not a shutdown.
	if err := l.Close(); err != nil {
		t.Fatalf("could not close the listener: %v", err)
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Serve returned no error after its listener failed")
		}
		if errors.Is(err, xrootd.ErrServerClosed) {
			t.Fatal("a listener failure was reported as a clean shutdown")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after its listener failed")
	}
}
