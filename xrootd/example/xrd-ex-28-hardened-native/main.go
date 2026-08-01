// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-28-hardened-native spells out every bound the native client applies by default.
//
// NewClient already applies all four of the options below -- that is what
// Hardened() is, and it is the default. Nothing here needs to be written out
// to get it. This program writes it out anyway, so that each bound is visible
// and the reason for it is next to it, and so that a caller who needs to move
// one knows which one to move.
//
// The failure to plan for on a wide-area link is not a refused connection --
// that is reported at once -- but a path that stops forwarding while both ends
// still believe the connection is up: a firewall that dropped the flow from
// its table, a load balancer that failed over, a route that got black-holed.
// Nothing is closed and nothing is refused, so a read blocks until TCP gives
// up, which on Linux is the better part of an hour.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"go-hep.org/x/hep/xrootd"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	cli, err := xrootd.NewClient(ctx, "storage.example.org:1094", "gopher",
		// How long the connection may say nothing WHILE A REQUEST IS
		// OUTSTANDING before it is declared dead. An idle connection with
		// nothing pending is left alone -- telling those two apart is the
		// whole point, since the socket reports both as a read deadline.
		//
		// Raise it at a site that stages from tape, where a single open can
		// legitimately take many minutes.
		xrootd.WithStreamTimeout(60*time.Second), // XRD_STREAMTIMEOUT

		// A separate bound on the dial AND on the bootstrap that follows it:
		// handshake, kXR_protocol, TLS upgrade. Those run before the read
		// loop that carries the stream timeout exists, so without this an
		// address that completes the TCP handshake and then goes quiet hangs
		// NewClient for good.
		xrootd.WithConnectionWindow(30*time.Second), // XRD_CONNECTIONWINDOW

		// How many times to redial. Only the CONNECTION is retried: nothing
		// has been sent when a dial fails, so repeating it cannot make
		// anything happen twice. A request is a different matter -- the
		// server may have carried it out and lost the answer -- and is never
		// replayed here. Backoff is jittered so a thousand jobs hitting one
		// recovering redirector do not come back in step.
		xrootd.WithConnectionRetry(5), // XRD_CONNECTIONRETRY

		// Probes on the socket itself, so a connection that is idle between
		// reads still finds out the peer is gone. Interval, then probe
		// spacing, then probe count: 30s + 3x10s means a dead peer is noticed
		// in about a minute rather than in two hours.
		xrootd.WithKeepAlive(30*time.Second, 10*time.Second, 3),
		// XRD_TCPKEEPALIVE, _TIME, _INTVL, _PROBES
	)
	if err != nil {
		log.Fatalf("could not connect: %+v", err)
	}
	defer cli.Close()

	// In practice you write none of it. The defaults are already the above,
	// so the usual shape is to name only the one bound your site needs moved:
	//
	//	xrootd.NewClient(ctx, addr, user,
	//		xrootd.WithStreamTimeout(5*time.Minute), // a site staging from tape
	//	)
	//
	// And xrootd.Unbounded() removes all four, for a caller whose own context
	// is the only deadline it wants.

	st, err := cli.FS().Stat(ctx, "/store/user/gopher/data.root")
	if err != nil {
		log.Fatalf("could not stat: %+v", err)
	}
	fmt.Printf("%d bytes\n", st.EntrySize)
}
