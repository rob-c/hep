// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for what a caller gets without asking for anything.
//
// A library's defaults are the configuration almost every program ships with,
// because the caller who would change them is the caller who already knows what
// they do. That makes an unbounded default a real choice and not a neutral one:
// a wide-area path that stops forwarding without closing leaves a read blocked
// until TCP gives up, and a beginner's first program against grid storage then
// hangs for the better part of an hour with nothing in the output to say why.
//
// So the bounds are on unless they are turned off, and the three layers below
// have to stay in this order — defaults, then the environment, then the
// program — or a caller that configured its client explicitly would behave
// differently depending on the shell it was started from.

package xrootd

import (
	"context"
	"net"
	"testing"
	"time"
)

// bootstrapOnly is a server that answers the handshake, kXR_protocol and
// kXR_login and then says nothing. It is enough for NewClient to return, which
// is all these tests need: what is under test is the configuration the client
// arrived at, not anything it goes on to do.
func bootstrapOnly(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				if _, ok := redirBootstrap(conn); !ok {
					return
				}
				// Park until the client hangs up.
				_, _ = readFull(conn, make([]byte, 1))
			}()
		}
	}()

	return ln.Addr().String()
}

// safeClient dials the bootstrap server with opts and returns the client, so a
// test reads the configuration NewClient actually assembled rather than the
// configuration a helper re-derived.
func safeClient(t *testing.T, addr string, opts ...Option) *Client {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)

	cli, err := NewClient(ctx, addr, "gopher", opts...)
	if err != nil {
		t.Fatalf("could not connect: %+v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	return cli
}

// TestConformance_ANewClientIsBoundedWithoutBeingAsked is the property the
// whole file exists for. A caller who writes the three arguments NewClient
// requires and nothing else gets every bound, because that caller is the one
// least able to recognise what an unbounded wait looks like from the outside.
func TestConformance_ANewClientIsBoundedWithoutBeingAsked(t *testing.T) {
	cli := safeClient(t, bootstrapOnly(t))

	if got, want := cli.streamTimeout, hardenedStreamTimeout; got != want {
		t.Errorf("got stream timeout %v, want %v", got, want)
	}
	if got, want := cli.dialTimeout, hardenedConnectionWindow; got != want {
		t.Errorf("got connection window %v, want %v", got, want)
	}
	if got, want := cli.connRetry, hardenedConnectionRetry; got != want {
		t.Errorf("got connection retry %d, want %d", got, want)
	}
	if !cli.keepAlive.Enable {
		t.Error("keepalives are off")
	}
	if got, want := cli.keepAlive.Idle, hardenedKeepAliveIdle; got != want {
		t.Errorf("got keepalive idle %v, want %v", got, want)
	}
	if got, want := cli.keepAlive.Interval, hardenedKeepAliveInterval; got != want {
		t.Errorf("got keepalive interval %v, want %v", got, want)
	}
	if got, want := cli.keepAlive.Count, hardenedKeepAliveProbes; got != want {
		t.Errorf("got keepalive probes %d, want %d", got, want)
	}
}

// TestConformance_APassedHardenedChangesNothing pins that the option is still
// meaningful to write. It is exported so that a program can say out loud what
// it relies on, and a reader who finds it in someone else's code should not
// have to wonder whether it does something a client without it does not.
func TestConformance_APassedHardenedChangesNothing(t *testing.T) {
	addr := bootstrapOnly(t)

	implicit := safeClient(t, addr)
	explicit := safeClient(t, addr, Hardened())

	if got, want := explicit.streamTimeout, implicit.streamTimeout; got != want {
		t.Errorf("got stream timeout %v, want %v", got, want)
	}
	if got, want := explicit.connRetry, implicit.connRetry; got != want {
		t.Errorf("got connection retry %d, want %d", got, want)
	}
	if got, want := explicit.keepAlive, implicit.keepAlive; got != want {
		t.Errorf("got keepalive %+v, want %+v", got, want)
	}
}

// TestConformance_ACallersOptionsWinOverTheSafeDefaults checks the layer that
// makes the defaults safe rather than imposed. A site that stages from tape
// needs a stream timeout of minutes, and a default it could not move would be a
// client that cut off every one of its reads.
func TestConformance_ACallersOptionsWinOverTheSafeDefaults(t *testing.T) {
	cli := safeClient(t, bootstrapOnly(t),
		WithStreamTimeout(5*time.Minute),
		WithConnectionRetry(1),
	)

	if got, want := cli.streamTimeout, 5*time.Minute; got != want {
		t.Errorf("got stream timeout %v, want %v", got, want)
	}
	if got, want := cli.connRetry, 1; got != want {
		t.Errorf("got connection retry %d, want %d", got, want)
	}
	// The bounds the caller did not name are still the safe ones: naming one
	// option is not a request to drop the rest.
	if got, want := cli.dialTimeout, hardenedConnectionWindow; got != want {
		t.Errorf("got connection window %v, want %v", got, want)
	}
}

// TestConformance_TheEnvironmentWinsOverTheSafeDefaults checks the middle
// layer. XRD_* is how a batch system tunes a program it did not write, and a
// default that outranked it would make the variables look supported while
// having no effect — the worst of the three possible behaviours, because
// nothing reports it.
func TestConformance_TheEnvironmentWinsOverTheSafeDefaults(t *testing.T) {
	t.Setenv(EnvStreamTimeout, "7")
	t.Setenv(EnvConnectionRetry, "2")

	cli := safeClient(t, bootstrapOnly(t))

	if got, want := cli.streamTimeout, 7*time.Second; got != want {
		t.Errorf("got stream timeout %v, want %v", got, want)
	}
	if got, want := cli.connRetry, 2; got != want {
		t.Errorf("got connection retry %d, want %d", got, want)
	}
}

// TestConformance_ACallersOptionsWinOverTheEnvironment keeps the order of the
// upper two layers. A program that configures its client explicitly has to
// behave the same whatever shell it was started from, or a variable left over
// in someone's profile becomes an unreproducible bug report.
func TestConformance_ACallersOptionsWinOverTheEnvironment(t *testing.T) {
	t.Setenv(EnvStreamTimeout, "7")

	cli := safeClient(t, bootstrapOnly(t), WithStreamTimeout(90*time.Second))

	if got, want := cli.streamTimeout, 90*time.Second; got != want {
		t.Errorf("got stream timeout %v, want %v", got, want)
	}
}

// TestConformance_UnboundedRemovesEveryBound is the way out, and it has to
// remove all four: a caller that asked for no bounds and got three of them
// would find the client giving up on a read it meant to wait for.
func TestConformance_UnboundedRemovesEveryBound(t *testing.T) {
	cli := safeClient(t, bootstrapOnly(t), Unbounded())

	if got := cli.streamTimeout; got != 0 {
		t.Errorf("got stream timeout %v, want none", got)
	}
	if got := cli.dialTimeout; got != 0 {
		t.Errorf("got connection window %v, want none", got)
	}
	if got := cli.connRetry; got != 0 {
		t.Errorf("got connection retry %d, want none", got)
	}
	if cli.keepAlive.Enable {
		t.Error("keepalives are still on")
	}
}

// TestConformance_UnboundedAlsoOutranksTheEnvironment. Unbounded is a program
// saying it wants to wait; a variable in the environment that quietly put a
// bound back would defeat the one caller who was explicit about it.
func TestConformance_UnboundedAlsoOutranksTheEnvironment(t *testing.T) {
	t.Setenv(EnvStreamTimeout, "7")
	t.Setenv(EnvKeepAlive, "1")

	cli := safeClient(t, bootstrapOnly(t), Unbounded())

	if got := cli.streamTimeout; got != 0 {
		t.Errorf("got stream timeout %v, want none", got)
	}
	if cli.keepAlive.Enable {
		t.Error("keepalives are still on")
	}
}

// TestConformance_ABoundCanBePutBackAfterUnbounded. Options are applied in
// order, so dropping the set and keeping one is a matter of saying so — which
// is the shape a caller reaches for when they want their own context to be the
// only deadline but still want the kernel to probe an idle flow.
func TestConformance_ABoundCanBePutBackAfterUnbounded(t *testing.T) {
	cli := safeClient(t, bootstrapOnly(t),
		Unbounded(),
		WithKeepAlive(30*time.Second, 10*time.Second, 3),
	)

	if got := cli.streamTimeout; got != 0 {
		t.Errorf("got stream timeout %v, want none", got)
	}
	if !cli.keepAlive.Enable {
		t.Fatal("keepalives are off")
	}
	if got, want := cli.keepAlive.Count, 3; got != want {
		t.Errorf("got keepalive probes %d, want %d", got, want)
	}
}
