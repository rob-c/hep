// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-29-hardened-http spells out what the HTTP transport retries, and what it will not.
//
// The HTTP path crosses more machinery than the native one -- proxies,
// redirectors, load balancers, gateways in front of the storage -- and each
// has its own way of saying "not now". None of them mean the file is
// unavailable, and all of them are worth a second attempt.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"go-hep.org/x/hep/xrootd/xrdhttp"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	cli, err := xrdhttp.Dial("davs://webdav.example.org:2880/atlas",
		xrdhttp.WithDiscoveredBearerToken(),

		// Dial already applies both of these -- that is what Hardened() is,
		// and it is the default. They are written out here so the rules are
		// next to the setting they belong to.
		//
		// Attempts, not retries: 5 means the first try plus four more. What
		// is resent is (a) a transport failure that happened BEFORE any
		// response was read, and (b) 429, 500, 502, 503, 504. A 404 or a 403
		// is an answer, and comes back as one.
		//
		// Never resent, whatever this is set to:
		//   - COPY. It starts a third-party transfer whose first attempt may
		//     still be running, and two servers writing one file is worse
		//     than a failure. POST, PATCH and MKCOL are out for the same
		//     reason.
		//   - any request whose body cannot be produced a second time. A body
		//     handed over as a stream is gone once read, and resending would
		//     upload whatever is left of it. net/http records GetBody for the
		//     bodies it can rewind itself -- *bytes.Reader, *strings.Reader,
		//     *bytes.Buffer -- and that is the test used.
		//
		// Retry-After is preferred to the backoff when the server sends one,
		// in both spellings RFC 9110 allows, capped at a minute: the header
		// is advice, and a gateway asking for an hour is asking for longer
		// than any caller meant to wait.
		xrdhttp.WithRetry(5),

		// Per ATTEMPT, not for the whole operation, and sized to cover a
		// ranged read of a large file rather than a single round trip.
		xrdhttp.WithTimeout(5*time.Minute),
	)
	if err != nil {
		log.Fatalf("could not connect: %+v", err)
	}

	// This upload is retryable: bytes.Reader can be rewound.
	body := []byte("hello from go-hep\n")
	name := "user/gopher/note.txt"
	if err := cli.Create(ctx, name, bytes.NewReader(body), int64(len(body))); err != nil {
		log.Fatalf("could not upload: %+v", err)
	}

	// This one would not be: os.File is a stream as far as net/http is
	// concerned, so a failure part-way through is reported rather than
	// silently resuming from wherever the reader stopped.
	f, err := os.Open("/dev/null")
	if err != nil {
		log.Fatalf("could not open: %+v", err)
	}
	defer f.Close()
	_ = f

	// Errors carry no credential. net/http records the request URL verbatim
	// in the *url.Error it builds for a transport failure -- query string and
	// all, which is exactly where WebDAV and XRootD endpoints put ?authz=.
	// Every transport error here is scrubbed of its query, its fragment and
	// any userinfo password before it is returned, so it is safe to log.
	_, err = cli.Stat(ctx, "user/gopher/missing.root")
	if err != nil {
		var se *xrdhttp.StatusError
		switch {
		case errors.As(err, &se):
			fmt.Printf("server said %d: %s\n", se.Code, se.Status)
		default:
			log.Printf("transport failure (safe to log): %v", err)
		}
	}
}
