// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdhttp_test // import "go-hep.org/x/hep/xrootd/xrdhttp"

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"go-hep.org/x/hep/xrootd/xrdhttp"
)

// Reading from grid storage over WebDAV with a bearer token — a WLCG token, a
// SciToken, or a macaroon.
//
// The token is found the way every WLCG client finds it: $BEARER_TOKEN, then
// $BEARER_TOKEN_FILE, then the well-known files under $XDG_RUNTIME_DIR and
// /tmp. Discovery is asked for rather than assumed, because an HTTP request
// presents its credential unprompted: unlike the native protocol, the server
// never says what it accepts first, so sending an ambient token to whatever
// host was dialled has to be the caller's decision.
//
// davs:// and https:// name the same thing here. Either is accepted; a
// cleartext http:// endpoint is refused outright, since a bearer token is a
// credential anyone who sees it can replay.
func Example_bearerToken() {
	ctx := context.Background()

	cli, err := xrdhttp.Dial("davs://webdav.example.com:2880/atlas/rucio",
		xrdhttp.WithDiscoveredBearerToken(),
	)
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}

	// Paths are resolved against the base path of the URL, so a relative name
	// stays inside the area the token was issued for.
	entries, err := cli.Dirlist(ctx, "user/analysis")
	if err != nil {
		log.Fatalf("could not list the collection: %v", err)
	}
	for _, e := range entries {
		fmt.Printf("%s %d %v\n", e.Name, e.Size, e.IsDir)
	}

	fi, err := cli.Stat(ctx, "user/analysis/data.root")
	if err != nil {
		log.Fatalf("could not stat the file: %v", err)
	}
	fmt.Printf("%s is %d bytes\n", fi.Name, fi.Size)

	// A ranged read: one HTTP Range request, not a download of the whole
	// object. This is what reading a TTree basket out of a multi-gigabyte file
	// costs over HTTP.
	buf := make([]byte, 64<<10)
	n, err := cli.ReadAt(ctx, buf, "user/analysis/data.root", 0)
	if err != nil {
		log.Fatalf("could not read the file: %v", err)
	}
	fmt.Printf("read %d bytes\n", n)
}

// A token the program already holds, rather than one on disk. It is passed as
// the token itself — no "Bearer " prefix — and the package never writes it to
// an error message or to the terminal.
func Example_explicitBearerToken() {
	tok := os.Getenv("MY_APP_TOKEN")

	cli, err := xrdhttp.Dial("https://webdav.example.com:2880",
		xrdhttp.WithBearerToken(tok),
	)
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}

	// The same client also exposes the protocol-neutral filesystem interface,
	// so code written against the native protocol runs unchanged over WebDAV.
	entries, err := cli.FS().Dirlist(context.Background(), "/atlas/rucio")
	if err != nil {
		log.Fatalf("could not list the collection: %v", err)
	}
	fmt.Printf("%d entries\n", len(entries))
}

// Token access over a wide-area network that will not always fail loudly.
//
// The HTTP path crosses more machinery than the native one — proxies,
// redirectors, gateways in front of the storage — and each has its own way of
// saying "not now": a reset before the response, a 503 while a backend drains,
// a 429 from a rate limiter. None of them mean the file is missing.
// xrdhttp.Hardened bounds each attempt and repeats the ones that can be
// repeated safely, honouring a Retry-After when the server names its own delay.
//
// A COPY — a third-party transfer — is never repeated: the first one may still
// be running, and two servers writing the same file is worse than a failure.
func Example_hardenedAgainstBadNetworks() {
	cli, err := xrdhttp.Dial("davs://webdav.example.com:2880/atlas/rucio",
		xrdhttp.WithDiscoveredBearerToken(),
		xrdhttp.Hardened(),

		// Each part is separately adjustable; anything after Hardened wins.
		xrdhttp.WithTimeout(30*time.Minute), // whole-file reads of large objects
		xrdhttp.WithRetry(8),
	)
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}

	// The caller's context still bounds the whole operation, retries included.
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	if _, err := cli.ReadAll(ctx, "user/analysis/data.root"); err != nil {
		log.Fatalf("could not read the file: %v", err)
	}
}
