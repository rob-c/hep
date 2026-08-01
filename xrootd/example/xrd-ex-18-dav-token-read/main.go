// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-18-dav-token-read reads over WebDAV with a bearer token.
// macaroon.
//
// The token is found the way every WLCG client finds it: $BEARER_TOKEN, then
// $BEARER_TOKEN_FILE, then the well-known files under $XDG_RUNTIME_DIR and
// /tmp. Discovery is asked for rather than assumed, because an HTTP request
// presents its credential unprompted: unlike the native protocol the server
// never says what it accepts first, so sending an ambient token to whatever
// host was dialled has to be the caller's decision.
//
// davs:// and https:// name the same thing here. A cleartext http:// endpoint
// is refused outright -- a bearer token is a credential anyone who sees it can
// replay.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"go-hep.org/x/hep/xrootd/xrdhttp"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	cli, err := xrdhttp.Dial("davs://webdav.example.com:2880/atlas/rucio",
		xrdhttp.WithDiscoveredBearerToken(),
	)
	if err != nil {
		log.Fatalf("could not connect: %+v", err)
	}

	// Paths resolve against the base path of the URL, so a relative name stays
	// inside the area the token was issued for.
	fi, err := cli.Stat(ctx, "user/analysis/data.root")
	if err != nil {
		log.Fatalf("could not stat: %+v", err)
	}
	fmt.Printf("%s: %d bytes, exists=%v\n", fi.Name, fi.Size, fi.Exists)

	// A ranged read: ONE HTTP Range request, not a download of the whole
	// object. This is what reading a TTree basket out of a multi-gigabyte file
	// costs over HTTP.
	buf := make([]byte, 64<<10)
	n, err := cli.ReadAt(ctx, buf, "user/analysis/data.root", 0)
	if err != nil {
		log.Fatalf("could not read: %+v", err)
	}
	fmt.Printf("read %d bytes\n", n)

	// The whole object, when it is small enough to want in memory.
	all, err := cli.ReadAll(ctx, "user/analysis/summary.json")
	if err != nil {
		log.Fatalf("could not read: %+v", err)
	}
	fmt.Printf("summary is %d bytes\n", len(all))
}
