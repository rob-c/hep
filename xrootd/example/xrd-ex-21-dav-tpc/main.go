// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-21-dav-tpc copies a file between two storage elements without the bytes passing through the client.
// bytes passing through this process.
//
// Two credentials are in play and they are not the same one. The client's own
// token authenticates THIS process to the active endpoint; TPCOptions.
// RemoteToken is what the active endpoint replays against its peer. Reusing
// one for both is the usual reason a TPC that "should work" returns 401.
//
// A COPY is never retried by the hardening layer, however badly the network is
// behaving: the first attempt may still be running, and two servers writing
// one file is worse than a failure.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"go-hep.org/x/hep/xrootd/xrdhttp"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()

	cli, err := xrdhttp.Dial("davs://src.example.org:2880/atlas/rucio",
		xrdhttp.WithBearerToken(os.Getenv("SRC_TOKEN")),
	)
	if err != nil {
		log.Fatalf("could not connect: %+v", err)
	}

	opts := xrdhttp.TPCOptions{
		RemoteToken:                 os.Getenv("DST_TOKEN"),
		Overwrite:                   true,
		RequireChecksumVerification: true,

		// The endpoints emit performance markers while the copy runs. A
		// transfer that has stopped moving looks exactly like one that is
		// simply large, unless somebody is watching these.
		Progress: func(m xrdhttp.TPCMarker) {
			fmt.Printf("marker: %+v\n", m)
		},
	}

	// Push: this endpoint sends to the destination.
	err = cli.Push(ctx, "user/analysis/data.root",
		"davs://dst.example.org:2880/atlas/rucio/user/analysis/data.root", opts)
	if err != nil {
		log.Fatalf("push failed: %+v", err)
	}
	fmt.Println("pushed")

	// Pull: this endpoint fetches from the source. Which one to use is a
	// question of which side is allowed to open the connection.
	err = cli.Pull(ctx, "user/analysis/copy.root",
		"davs://dst.example.org:2880/atlas/rucio/user/analysis/data.root", opts)
	if err != nil {
		log.Fatalf("pull failed: %+v", err)
	}
	fmt.Println("pulled")
}
