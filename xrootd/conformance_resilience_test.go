// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for a connection that goes away.
//
// A transfer that runs for hours across a wide-area link loses its connection
// sooner or later, and the failure modes that matter are not the ones a server
// reports: a request whose reply never comes, a reply that stops halfway, a
// socket that closes while several requests are in flight. None of those is an
// error the protocol has a status code for, so every one of them has to be
// turned into an error by the client itself — the alternative is a caller that
// blocks forever holding a lock, which is worse than a failed copy.
//
// These need a real socket to close, so they run against real TCP servers
// rather than the net.Pipe harness: the bootstrap and the client helpers are
// the ones in conformance_redirect_test.go.

package xrootd

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/open"
	"go-hep.org/x/hep/xrootd/xrdproto/stat"
)

// flakyServer answers requests until it is told to stop answering, at which
// point it does to the connection whatever the test asked for: closes it,
// stops replying, or hangs up mid-reply.
type flakyServer struct {
	addr string
	// done is closed when the test ends, so a connection parked in
	// flakySilence has something to leave on.
	done chan struct{}

	mu sync.Mutex
	// after is the number of requests to answer before the fault. -1 means
	// "answer everything".
	after int
	// fault is what the server does with the request that trips it.
	fault flakyFault
	// served counts the requests answered normally.
	served int
	// conns holds the accepted connections, so a test can sever them from
	// the outside — a server process that is killed, rather than one that
	// decides to hang up.
	conns []net.Conn
}

type flakyFault int

const (
	// flakyClose closes the socket instead of replying.
	flakyClose flakyFault = iota
	// flakySilence keeps the socket open and never replies.
	flakySilence
	// flakyHalfReply writes a header promising a body, then hangs up.
	flakyHalfReply
)

func newFlakyServer(t *testing.T, after int, fault flakyFault) *flakyServer {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	srv := &flakyServer{addr: ln.Addr().String(), done: make(chan struct{}), after: after, fault: fault}
	t.Cleanup(func() { close(srv.done) })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			srv.mu.Lock()
			srv.conns = append(srv.conns, conn)
			srv.mu.Unlock()
			go srv.serve(conn)
		}
	}()
	return srv
}

func (srv *flakyServer) serve(conn net.Conn) {
	defer conn.Close()

	if _, ok := redirBootstrap(conn); !ok {
		return
	}

	for {
		hdr, body := readBootstrapRequest(conn)
		if body == nil {
			return
		}

		srv.mu.Lock()
		trip := srv.after >= 0 && srv.served >= srv.after
		if !trip {
			srv.served++
		}
		fault := srv.fault
		srv.mu.Unlock()

		if trip {
			switch fault {
			case flakyClose:
				return
			case flakySilence:
				// Hold the socket open and answer nothing. The
				// request is now the caller's context to bound.
				<-srv.done
				return
			case flakyHalfReply:
				// The header promises 64 bytes; 4 arrive and the
				// socket closes. A client that decodes what it
				// got rather than what it was promised reads a
				// stat line out of four bytes of nothing.
				_, _ = conn.Write(append(confRespHdr(hdr.StreamID, uint16(xrdproto.Ok), 64), 0, 0, 0, 0))
				return
			}
		}

		switch hdr.RequestID {
		case stat.RequestID:
			var req stat.Request
			_ = req.UnmarshalXrd(xrdenc.NewRBuffer(body))
			writeBootstrapResponse(conn, hdr.StreamID, &stat.DefaultResponse{
				EntryStat: xrdfs.EntryStat{HasStatInfo: true, EntrySize: 42},
			})
		case open.RequestID:
			writeBootstrapResponse(conn, hdr.StreamID, &open.Response{
				FileHandle: xrdfs.FileHandle{1, 0, 0, 0},
			})
		default:
			writeBootstrapResponse(conn, hdr.StreamID, rawBody(nil))
		}
	}
}

// severAll closes every connection the server accepted, from the server side
// and without a reply. This is the death of a server process, not a hang-up it
// chose to perform.
func (srv *flakyServer) severAll() {
	srv.mu.Lock()
	conns := append([]net.Conn(nil), srv.conns...)
	srv.mu.Unlock()

	for _, conn := range conns {
		_ = conn.Close()
	}
}

// TestConformance_TransportLossIsReported is the base case: the reply to a
// request in flight never comes because the socket closed. There is no status
// code for that, so silence has to become an error rather than a wait.
func TestConformance_TransportLossIsReported(t *testing.T) {
	srv := newFlakyServer(t, 0, flakyClose)
	cli, ctx := redirClient(t, srv.addr)

	done := make(chan error, 1)
	go func() {
		_, err := cli.FS().Stat(ctx, "/f")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a request whose connection closed reported success")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a request whose connection closed never returned")
	}
}

// TestConformance_TransportLossFailsEveryRequestInFlight: a connection carries
// many requests at once, and closing it fails all of them. A client that
// resolves only the request it happens to be reading for leaves the rest of the
// callers blocked forever on a socket that no longer exists.
func TestConformance_TransportLossFailsEveryRequestInFlight(t *testing.T) {
	const inFlight = 8

	// The server answers the warm-up and then goes quiet, so the batch is
	// genuinely in flight — all of it — when the connection is severed.
	srv := newFlakyServer(t, 1, flakySilence)
	cli, ctx := redirClient(t, srv.addr)

	// One request first, so the session is fully established before the
	// batch goes out and the failure cannot be blamed on the bring-up.
	if _, err := cli.FS().Stat(ctx, "/warmup"); err != nil {
		t.Fatalf("warm-up Stat: %v", err)
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs int
	)
	start := make(chan struct{})
	for range inFlight {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := cli.FS().Stat(ctx, "/f"); err != nil {
				mu.Lock()
				errs++
				mu.Unlock()
			}
		}()
	}

	close(start)
	// Give the requests a moment to reach the wire, then kill the server
	// under them.
	time.Sleep(20 * time.Millisecond)
	srv.severAll()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("requests were left waiting on a connection that no longer exists")
	}

	mu.Lock()
	defer mu.Unlock()
	if errs != inFlight {
		t.Errorf("%d of %d requests through a severed connection reported success",
			inFlight-errs, inFlight)
	}
}

// TestConformance_RequestsAfterTransportLossAreRefused: once the connection is
// gone the session is unusable, and saying so is the only honest answer. The
// failure mode this rules out is a client that queues the request against a
// dead socket and waits for a reply that cannot come.
func TestConformance_RequestsAfterTransportLossAreRefused(t *testing.T) {
	srv := newFlakyServer(t, -1, flakyClose)
	cli, ctx := redirClient(t, srv.addr)

	if _, err := cli.FS().Stat(ctx, "/f"); err != nil {
		t.Fatalf("the first Stat failed: %v", err)
	}

	srv.severAll()

	// The client may not notice the close until it tries to use the socket,
	// so the first request after the sever is allowed to be the one that
	// discovers it. What is not allowed is for any of them to hang.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 3 {
			if _, err := cli.FS().Stat(ctx, "/f"); err != nil {
				return
			}
		}
		t.Error("requests kept succeeding against a server that is gone")
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("a request against a dead connection never returned")
	}
}

// TestConformance_HalfWrittenReplyIsReported: the header promised 64 bytes and
// 4 arrived. Everything about that reply looks well-formed until the body runs
// out, which is exactly when a client that trusts the length it was given
// starts decoding whatever memory it allocated for it.
func TestConformance_HalfWrittenReplyIsReported(t *testing.T) {
	srv := newFlakyServer(t, 0, flakyHalfReply)
	cli, ctx := redirClient(t, srv.addr)

	done := make(chan error, 1)
	go func() {
		_, err := cli.FS().Stat(ctx, "/f")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a reply that stopped halfway reported success")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a reply that stopped halfway never returned")
	}
}

// TestConformance_ASilentServerIsBoundedByTheContext: a socket that stays open
// and answers nothing is indistinguishable from a slow server, so it is not the
// client's job to decide it has failed — but it is the client's job to let the
// caller decide. The deadline the caller set has to be the one that ends it.
func TestConformance_ASilentServerIsBoundedByTheContext(t *testing.T) {
	srv := newFlakyServer(t, 0, flakySilence)
	cli, _ := redirClient(t, srv.addr)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := cli.FS().Stat(ctx, "/f")
		done <- err
	}()

	select {
	case err := <-done:
		switch {
		case err == nil:
			t.Fatal("a request to a server that never answered reported success")
		case !strings.Contains(err.Error(), "context"):
			t.Fatalf("the request failed with %v, want the context deadline", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a request outlived the context that bounded it")
	}
}

// TestConformance_ClosingTheClientEndsRequestsInFlight: Close is what a caller
// reaches for to abandon work, and a Close that waits for replies that will
// never come is not a way out of anything.
func TestConformance_ClosingTheClientEndsRequestsInFlight(t *testing.T) {
	srv := newFlakyServer(t, 0, flakySilence)

	ctx := context.Background()
	cli, err := NewClient(ctx, srv.addr, "gopher")
	if err != nil {
		t.Fatalf("could not create the client: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := cli.FS().Stat(ctx, "/f")
		done <- err
	}()

	time.Sleep(20 * time.Millisecond)
	if err := cli.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a request abandoned by Close reported success")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close left a request waiting for a reply that cannot come")
	}
}

// TestConformance_AnOpenFileFailsWithItsConnection: a file handle is a piece of
// state on the far side of the socket, so it dies with the socket. Every method
// on it has to say so rather than block, including the Close a deferred cleanup
// will call.
func TestConformance_AnOpenFileFailsWithItsConnection(t *testing.T) {
	srv := newFlakyServer(t, -1, flakyClose)
	cli, ctx := redirClient(t, srv.addr)

	f, err := cli.FS().Open(ctx, "/f", xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	srv.severAll()

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 8)
		for range 3 {
			if _, err := f.ReadAt(buf, 0); err != nil {
				break
			}
		}
		// Close of a file whose connection is gone must return rather
		// than wait, whatever it reports.
		_ = f.Close(ctx)
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("a file outlived its connection and blocked on it")
	}
}
