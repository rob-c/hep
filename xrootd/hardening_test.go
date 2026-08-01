// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/internal/mux"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/dirlist"
	"go-hep.org/x/hep/xrootd/xrdproto/signing"
)

func TestConnectBackoffGrowsAndIsCapped(t *testing.T) {
	// The jitter makes each pause a range rather than a value, so the bounds
	// are what can be asserted: never negative, never past the cap, and
	// growing until it reaches it.
	const jitterMax = 1 + connectBackoffJitter

	for n := range 12 {
		got := connectBackoff(n)
		if got < 0 {
			t.Fatalf("backoff %d is %v, want a non-negative pause", n, got)
		}
		if max := time.Duration(jitterMax * float64(connectBackoffMax)); got > max {
			t.Fatalf("backoff %d is %v, want no more than %v", n, got, max)
		}
	}

	// The first pause cannot reach the cap, the tenth cannot be below it: a
	// backoff that did not grow would hammer a host that is refusing
	// connections, and one that grew without bound would turn a generous retry
	// count into a wait measured in minutes.
	if got, max := connectBackoff(0), time.Duration(jitterMax*float64(connectBackoffBase)); got > max {
		t.Fatalf("the first backoff is %v, want no more than %v", got, max)
	}
	if got, min := connectBackoff(10), time.Duration((1-connectBackoffJitter)*float64(connectBackoffMax)); got < min {
		t.Fatalf("the eleventh backoff is %v, want at least %v", got, min)
	}
}

func TestHardenedSetsEveryBound(t *testing.T) {
	var client Client
	if err := Hardened()(&client); err != nil {
		t.Fatalf("could not apply the option: %v", err)
	}

	if client.streamTimeout <= 0 {
		t.Fatal("a hardened client tolerates any silence")
	}
	if client.dialTimeout <= 0 {
		t.Fatal("a hardened client waits indefinitely for a connection")
	}
	if client.connRetry <= 0 {
		t.Fatal("a hardened client does not retry a failed connection")
	}
	if !client.keepAlive.Enable {
		t.Fatal("a hardened client sends no keepalive probes")
	}

	// An option applied afterwards wins: Hardened is a starting point, not a
	// policy a caller cannot get out from under.
	if err := WithStreamTimeout(5 * time.Minute)(&client); err != nil {
		t.Fatalf("could not apply the option: %v", err)
	}
	if got, want := client.streamTimeout, 5*time.Minute; got != want {
		t.Fatalf("the stream timeout is %v, want %v", got, want)
	}
}

func TestHardeningOptionsRefuseNonsense(t *testing.T) {
	for _, tc := range []struct {
		name string
		opt  Option
		ok   bool
	}{
		{"a negative retry count", WithConnectionRetry(-1), false},
		{"no retries at all", WithConnectionRetry(0), true},
		{"a negative probe count", WithKeepAlive(time.Second, time.Second, -1), false},
		{"keepalives turned off", WithKeepAlive(0, time.Second, 3), true},
		{"a negative stream timeout", WithStreamTimeout(-time.Second), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var client Client
			err := tc.opt(&client)
			if got := err == nil; got != tc.ok {
				t.Fatalf("the option returned %v, want ok=%v", err, tc.ok)
			}
		})
	}

	// A negative stream timeout is not an error, it is "no bound" — and it has
	// to be stored as such, or every read would be given a deadline in the past.
	var client Client
	if err := WithStreamTimeout(-time.Second)(&client); err != nil {
		t.Fatalf("could not apply the option: %v", err)
	}
	if client.streamTimeout != 0 {
		t.Fatalf("a negative stream timeout was stored as %v, want none", client.streamTimeout)
	}

	// Keepalives turned off must leave the schedule empty, not a disabled
	// schedule with an idle time nobody asked for.
	if err := WithKeepAlive(0, time.Second, 3)(&client); err != nil {
		t.Fatalf("could not apply the option: %v", err)
	}
	if client.keepAlive != (net.KeepAliveConfig{}) {
		t.Fatalf("keepalives are %+v, want the system's own settings", client.keepAlive)
	}
}

func TestHardeningEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name  string
		env   map[string]string
		ok    bool
		check func(*testing.T, *Client)
	}{
		{
			name: "a stream timeout in seconds",
			env:  map[string]string{EnvStreamTimeout: "45"},
			ok:   true,
			check: func(t *testing.T, c *Client) {
				if got, want := c.streamTimeout, 45*time.Second; got != want {
					t.Fatalf("the stream timeout is %v, want %v", got, want)
				}
			},
		},
		{
			name: "a retry count",
			env:  map[string]string{EnvConnectionRetry: "3"},
			ok:   true,
			check: func(t *testing.T, c *Client) {
				if got, want := c.connRetry, 3; got != want {
					t.Fatalf("the retry count is %d, want %d", got, want)
				}
			},
		},
		{
			name: "keepalives with a schedule",
			env: map[string]string{
				EnvKeepAlive:         "1",
				EnvKeepAliveTime:     "20",
				EnvKeepAliveInterval: "5",
				EnvKeepAliveProbes:   "9",
			},
			ok: true,
			check: func(t *testing.T, c *Client) {
				want := net.KeepAliveConfig{Enable: true, Idle: 20 * time.Second, Interval: 5 * time.Second, Count: 9}
				if c.keepAlive != want {
					t.Fatalf("keepalives are %+v, want %+v", c.keepAlive, want)
				}
			},
		},
		{
			// A schedule with nothing to schedule. The C++ client reads it the
			// same way, and quietly turning keepalives on because a tuning was
			// left in the environment would be a surprise.
			name: "a schedule without the switch",
			env:  map[string]string{EnvKeepAliveTime: "20"},
			ok:   true,
			check: func(t *testing.T, c *Client) {
				if c.keepAlive.Enable {
					t.Fatalf("keepalives are on: %+v", c.keepAlive)
				}
			},
		},
		{
			// A setting the user believes is in force has to be one that is,
			// so an unreadable value is an error rather than a silent default.
			name: "an unreadable stream timeout",
			env:  map[string]string{EnvStreamTimeout: "45s"},
		},
		{
			name: "an unreadable retry count",
			env:  map[string]string{EnvConnectionRetry: "lots"},
		},
		{
			name: "an unreadable keepalive time",
			env:  map[string]string{EnvKeepAlive: "yes", EnvKeepAliveTime: "20s"},
		},
		{
			name: "an unreadable keepalive interval",
			env:  map[string]string{EnvKeepAlive: "yes", EnvKeepAliveInterval: "5s"},
		},
		{
			name: "an unreadable probe count",
			env:  map[string]string{EnvKeepAlive: "yes", EnvKeepAliveProbes: "nine"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			var (
				client Client
				err    error
			)
			for _, opt := range hardeningEnvOptions() {
				if opt == nil {
					continue
				}
				if err = opt(&client); err != nil {
					break
				}
			}
			if got := err == nil; got != tc.ok {
				t.Fatalf("the environment was read as %v, want ok=%v", err, tc.ok)
			}
			if tc.check != nil {
				tc.check(t, &client)
			}
		})
	}
}

// deadAddr returns an address nothing is listening on: a listener is opened to
// have one assigned and then closed, so the port is one the kernel handed out
// rather than one this test hopes is free.
func deadAddr(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func TestDialRetriesABoundedNumberOfTimes(t *testing.T) {
	addr := deadAddr(t)
	client := &Client{connRetry: 2}

	var d net.Dialer
	start := time.Now()
	_, err := dial(context.Background(), &d, addr, client)
	if err == nil {
		t.Fatal("a connection to an address nothing is listening on succeeded")
	}
	if !strings.Contains(err.Error(), "after 3 attempts") {
		t.Fatalf("the failure says %q, want it to say how many attempts were made", err)
	}

	// Two pauses were taken, and each is at least three quarters of its
	// nominal length. Without the backoff the three attempts would be
	// instantaneous, which is the bug this guards: a retry loop with no pause
	// is three connection refusals in a row and no more likely to succeed.
	min := time.Duration(0.75 * float64(connectBackoffBase+2*connectBackoffBase))
	if got := time.Since(start); got < min {
		t.Fatalf("three attempts took %v, want at least %v", got, min)
	}
}

func TestDialWithoutRetriesReportsTheDiallerError(t *testing.T) {
	// The default is one attempt and the dialler's own error, so a caller
	// matching on *net.OpError, or simply reading "connection refused", finds
	// what it has always found.
	addr := deadAddr(t)

	var d net.Dialer
	_, err := dial(context.Background(), &d, addr, &Client{})
	if err == nil {
		t.Fatal("a connection to an address nothing is listening on succeeded")
	}
	if strings.Contains(err.Error(), "attempts") {
		t.Fatalf("the failure says %q, want the dialler's own error", err)
	}
	var opErr *net.OpError
	if !errors.As(err, &opErr) {
		t.Fatalf("the failure is a %T, want a *net.OpError", err)
	}
}

func TestDialStopsRetryingWhenTheCallerGivesUp(t *testing.T) {
	// A caller whose deadline has passed is not waiting for the answer any
	// more, and a retry loop that ignored that would keep dialling long after
	// the request it was for had been abandoned.
	addr := deadAddr(t)
	client := &Client{connRetry: 100}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	var d net.Dialer
	start := time.Now()
	_, err := dial(ctx, &d, addr, client)
	if err == nil {
		t.Fatal("a connection to an address nothing is listening on succeeded")
	}
	// A hundred attempts at the capped backoff would be minutes.
	if got := time.Since(start); got > 10*time.Second {
		t.Fatalf("the attempts took %v after the caller gave up", got)
	}
}

func TestDialAppliesTheKeepAliveSchedule(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}
	defer l.Close()
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		<-time.After(50 * time.Millisecond)
	}()

	want := net.KeepAliveConfig{Enable: true, Idle: 7 * time.Second, Interval: 3 * time.Second, Count: 2}
	client := &Client{keepAlive: want}

	var d net.Dialer
	conn, err := dial(context.Background(), &d, l.Addr().String(), client)
	if err != nil {
		t.Fatalf("could not connect: %v", err)
	}
	defer conn.Close()

	if d.KeepAliveConfig != want {
		t.Fatalf("the dialler was given %+v, want %+v", d.KeepAliveConfig, want)
	}
}

// testSilentSession builds a session over an in-memory pipe with the given
// stream timeout, hands back the server end, and starts the read loop. It is
// the mock-server harness with a configured client, which is what these tests
// need and what testClientWithMockServer does not offer.
func testSilentSession(t *testing.T, timeout time.Duration) (*Client, net.Conn) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	server, conn := net.Pipe()

	client := &Client{
		cancel:          cancel,
		sessions:        make(map[string]*cliSession),
		maxRedirections: 8,
		streamTimeout:   timeout,
	}
	sess := &cliSession{
		cancel:           cancel,
		ctx:              ctx,
		conn:             conn,
		mux:              mux.New(),
		requests:         make(map[xrdproto.StreamID]pendingRequest),
		client:           client,
		signRequirements: signing.Default(),
		sessionID:        "test.org:1234",
		addr:             "test.org:1234",
	}
	client.initialSessionID = sess.sessionID
	client.sessions[sess.sessionID] = sess

	t.Cleanup(func() {
		client.Close()
		server.Close()
		cancel()
	})

	go sess.consume()

	return client, server
}

func TestStreamTimeoutFailsARequestOnASilentServer(t *testing.T) {
	// The failure this is for: the connection is up, the request went out, and
	// nothing comes back — a path that stopped forwarding, a server that was
	// swapped out mid-transfer. Left to TCP the read blocks for the better part
	// of an hour, and the caller has no way to tell it from a slow server.
	const timeout = 200 * time.Millisecond
	client, server := testSilentSession(t, timeout)

	go func() {
		// Read the request and say nothing, which is exactly what a
		// black-holed path looks like from this end.
		_, _ = xrdproto.ReadRequest(server)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	_, err := client.FS().Dirlist(ctx, "/tmp")
	if err == nil {
		t.Fatal("a request to a server that never answered succeeded")
	}
	if !strings.Contains(err.Error(), "sent nothing for") {
		t.Fatalf("the failure says %q, want it to say the server went silent", err)
	}
	if got := time.Since(start); got > 10*time.Second {
		t.Fatalf("the request took %v to fail, want about %v", got, timeout)
	}
}

func TestStreamTimeoutKeepsAnIdleConnection(t *testing.T) {
	// Silence with nothing outstanding is what a connection between transfers
	// looks like. Dropping it would mean a reconnection — a TCP handshake, a
	// login, an authentication — before the next read, which on a wide-area
	// link costs more than the connection was saving.
	const timeout = 100 * time.Millisecond
	client, server := testSilentSession(t, timeout)

	want := []xrdfs.EntryStat{{EntryName: "testfile", EntrySize: 20, Mtime: 10, HasStatInfo: true}}
	answered := make(chan error, 1)
	go func() {
		data, err := xrdproto.ReadRequest(server)
		if err != nil {
			answered <- err
			return
		}
		var req dirlist.Request
		header, err := unmarshalRequest(data, &req)
		if err != nil {
			answered <- err
			return
		}
		answered <- xrdproto.WriteResponse(server, header.StreamID, xrdproto.Ok, dirlist.Response{Entries: want, WithStatInfo: true})
	}()

	// Several timeouts' worth of silence, with nothing outstanding.
	time.Sleep(5 * timeout)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	got, err := client.FS().Dirlist(ctx, "/tmp")
	if err != nil {
		t.Fatalf("the connection did not survive being idle: %v", err)
	}
	if err := <-answered; err != nil {
		t.Fatalf("the server could not answer: %v", err)
	}
	if len(got) != 1 || got[0].EntryName != want[0].EntryName {
		t.Fatalf("the listing is %+v, want %+v", got, want)
	}
}

func TestStreamTimeoutBoundsTheHandshake(t *testing.T) {
	// A host that completes the TCP connection and then says nothing — a load
	// balancer in front of a dead backend is the usual way to meet one. The
	// handshake reads the socket directly, ahead of the loop that applies the
	// stream timeout, so without a bound of its own NewClient would wait here
	// for as long as the caller's context allows, which for a context.Background
	// is forever.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}
	defer l.Close()

	done := make(chan struct{})
	defer close(done)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		<-done
	}()

	const timeout = 200 * time.Millisecond
	start := time.Now()
	cli, err := NewClient(context.Background(), l.Addr().String(), "gopher", WithStreamTimeout(timeout))
	if err == nil {
		cli.Close()
		t.Fatal("a client was built against a host that never answered")
	}
	if got := time.Since(start); got > 10*time.Second {
		t.Fatalf("the handshake took %v to give up, want about %v", got, timeout)
	}
}
