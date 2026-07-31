// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the client's behaviour against a server that does not follow
// the protocol.
//
// The unit tests in xrdproto exercise the decoders directly; these drive the
// whole client against a peer that answers with truncated, over-long and
// garbage bodies, so they also cover the framing, the multiplexer and the code
// that turns a reply into a result. A client that survives a decoder in
// isolation still has to survive one reached through Send.

package xrootd

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

// rawBody is a response body written verbatim, which is how a hostile server
// is expressed: the bodies below are not valid encodings of anything.
type rawBody []byte

func (b rawBody) MarshalXrd(w *xrdenc.WBuffer) error {
	w.WriteBytes(b)
	return nil
}

// hostileServer answers every request it can read with the same body and
// status, until the connection goes away.
func hostileServer(status xrdproto.ResponseStatus, body []byte) func(cancel func(), conn net.Conn) {
	return func(cancel func(), conn net.Conn) {
		for {
			hdr, data := readBootstrapRequest(conn)
			if data == nil {
				return
			}
			if err := xrdproto.WriteResponse(conn, hdr.StreamID, status, rawBody(body)); err != nil {
				return
			}
		}
	}
}

// hostileOps are the client calls that decode a reply. Each one must come back
// with an answer or an error; none may panic or hang, whatever the reply says.
func hostileOps() []struct {
	name string
	call func(context.Context, *Client) error
} {
	return []struct {
		name string
		call func(context.Context, *Client) error
	}{
		{"protocol", func(ctx context.Context, c *Client) error {
			_, err := c.sessions[c.initialSessionID].Protocol(ctx)
			return err
		}},
		{"login", func(ctx context.Context, c *Client) error {
			_, err := c.sessions[c.initialSessionID].Login(ctx, "gopher", "")
			return err
		}},
		{"ping", func(ctx context.Context, c *Client) error {
			return c.sessions[c.initialSessionID].Ping(ctx)
		}},
		{"dirlist", func(ctx context.Context, c *Client) error {
			_, err := c.FS().Dirlist(ctx, "/tmp")
			return err
		}},
		{"stat", func(ctx context.Context, c *Client) error {
			_, err := c.FS().Stat(ctx, "/tmp/file")
			return err
		}},
		{"virtual-stat", func(ctx context.Context, c *Client) error {
			_, err := c.FS().VirtualStat(ctx, "/tmp")
			return err
		}},
		{"statx", func(ctx context.Context, c *Client) error {
			_, err := c.FS().Statx(ctx, []string{"/tmp/file"})
			return err
		}},
		{"open", func(ctx context.Context, c *Client) error {
			f, err := c.FS().Open(ctx, "/tmp/file", xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
			if err == nil {
				_ = f.Close(ctx)
			}
			return err
		}},
		{"mkdir", func(ctx context.Context, c *Client) error {
			return c.FS().Mkdir(ctx, "/tmp/dir", xrdfs.OpenModeOwnerRead)
		}},
		{"remove-file", func(ctx context.Context, c *Client) error {
			return c.FS().RemoveFile(ctx, "/tmp/file")
		}},
		{"remove-dir", func(ctx context.Context, c *Client) error {
			return c.FS().RemoveDir(ctx, "/tmp/dir")
		}},
		{"rename", func(ctx context.Context, c *Client) error {
			return c.FS().Rename(ctx, "/tmp/a", "/tmp/b")
		}},
		{"chmod", func(ctx context.Context, c *Client) error {
			return c.FS().Chmod(ctx, "/tmp/file", xrdfs.OpenModeOwnerRead)
		}},
		{"truncate", func(ctx context.Context, c *Client) error {
			return c.FS().Truncate(ctx, "/tmp/file", 0)
		}},
	}
}

// hostileBodies are the replies a broken or malicious server can produce. The
// lengths straddle every fixed-size record in the protocol: a file handle is
// 4 bytes, a session id 16, a stat record is text of no fixed length.
func hostileBodies() [][]byte {
	var bodies [][]byte
	for _, n := range []int{0, 1, 2, 3, 4, 5, 7, 8, 15, 16, 17, 31, 32, 33} {
		for _, fill := range []byte{0x00, 0xff, 'S', '\n'} {
			body := make([]byte, n)
			for i := range body {
				body[i] = fill
			}
			bodies = append(bodies, body)
		}
	}
	// A length prefix that promises far more data than follows, which is
	// what turns a decoder that allocates first into a memory switch.
	bodies = append(bodies,
		[]byte{0x7f, 0xff, 0xff, 0xff},
		[]byte{0xff, 0xff, 0xff, 0xff, 'x'},
		append([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, 0x7f, 0xff, 0xff, 0xff),
	)
	return bodies
}

// TestConformance_MalformedRepliesAreRejected drives every client call against
// every malformed reply. The client has no recover() between the decoder and
// main, so a panic here is a remote crash of any program using the package.
//
// The statuses here are the ones that hand a body to a decoder and then finish
// the request. kXR_wait and kXR_redirect send the client off to do something
// else with the body and are covered on their own below.
func TestConformance_MalformedRepliesAreRejected(t *testing.T) {
	for _, status := range []struct {
		name string
		code xrdproto.ResponseStatus
	}{
		{"ok", xrdproto.Ok},
		{"error", xrdproto.Error},
		{"authmore", xrdproto.AuthMore},
	} {
		for i, body := range hostileBodies() {
			for _, op := range hostileOps() {
				t.Run(fmt.Sprintf("%s/%s/body-%d", status.name, op.name, i), func(t *testing.T) {
					hostileRun(t, status.code, body, op.call)
				})
			}
		}
	}
}

// TestConformance_MalformedRedirectsAreRejected covers the redirect body, which
// is parsed on the connection-reading goroutine before anything about the peer
// has been authenticated. Only bodies too short to hold the 4-byte port are
// used: those must be refused outright, whereas a longer garbage body is a
// syntactically valid redirect and would send the client dialing.
func TestConformance_MalformedRedirectsAreRejected(t *testing.T) {
	for n := range 4 {
		for _, fill := range []byte{0x00, 0xff} {
			body := make([]byte, n)
			for i := range body {
				body[i] = fill
			}
			t.Run(fmt.Sprintf("%d-bytes/%#x", n, fill), func(t *testing.T) {
				var err error
				hostileRun(t, xrdproto.Redirect, body, func(ctx context.Context, c *Client) error {
					err = c.FS().RemoveFile(ctx, "/tmp/file")
					return err
				})
				if err == nil {
					t.Fatalf("a %d-byte redirect body was accepted", n)
				}
			})
		}
	}
}

// TestConformance_WaitIsBounded: the delay in a kXR_wait comes from the server
// and the field holds up to 68 years. The client caps it, so a request parked
// by a server still comes back to its caller when the caller gives up, and the
// goroutine holding it does not outlive the session.
func TestConformance_WaitIsBounded(t *testing.T) {
	var w xrdenc.WBuffer
	if err := (xrdproto.WaitResponse{Duration: 1 << 31 * time.Second}).MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal the wait response: %v", err)
	}

	before := runtime.NumGoroutine()
	done := make(chan error, 1)
	testClientWithMockServer(hostileServer(xrdproto.Wait, w.Bytes()), func(cancel func(), client *Client) {
		ctx, stop := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer stop()
		done <- client.FS().RemoveFile(ctx, "/tmp/file")
	})

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a request parked by a kXR_wait reported success")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("a request parked by a kXR_wait never came back")
	}

	// The sleeping goroutine is released by the session shutting down, not
	// by the wait expiring: it was told to wait for 68 years.
	deadline := time.Now().Add(10 * time.Second)
	for runtime.NumGoroutine() > before+2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := runtime.NumGoroutine(); got > before+2 {
		t.Fatalf("%d goroutines are still running, started from %d", got, before)
	}
}

// hostileRun is one call against one reply, with a deadline: a client that
// waits forever for a reply it already has is as broken as one that crashes.
func hostileRun(t *testing.T, status xrdproto.ResponseStatus, body []byte, call func(context.Context, *Client) error) {
	t.Helper()

	done := make(chan struct{})
	testClientWithMockServer(hostileServer(status, body), func(cancel func(), client *Client) {
		defer close(done)
		ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		// The result is deliberately ignored: an error is the expected
		// outcome, and a reply that happens to decode is fine too. What is
		// under test is that the call returns at all, without panicking.
		_ = call(ctx, client)
	})

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the call never came back from a malformed reply")
	}
}
