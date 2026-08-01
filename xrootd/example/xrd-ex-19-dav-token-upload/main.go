// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-19-dav-token-upload uploads over WebDAV with a token the program already holds.
//
// WithBearerToken takes the token itself -- no "Bearer " prefix. The package
// never writes it to an error message, to the terminal, or into the URL that
// appears in a transport error.
package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"go-hep.org/x/hep/xrootd/xrdhttp"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	tok := os.Getenv("MY_APP_TOKEN")
	if tok == "" {
		log.Fatal("set MY_APP_TOKEN")
	}

	cli, err := xrdhttp.Dial("https://webdav.example.com:2880/atlas/rucio",
		xrdhttp.WithBearerToken(tok),
	)
	if err != nil {
		log.Fatalf("could not connect: %+v", err)
	}

	body := []byte("hello from go-hep\n")

	// The size is declared, so the PUT carries a Content-Length and the
	// endpoint knows what it is committing to. Pass -1 for an unknown length
	// and it goes out chunked.
	//
	// A *bytes.Reader is rewindable, which is what makes this upload eligible
	// to be retried after a transient failure -- see 29.
	if err := cli.Create(ctx, "user/analysis/note.txt", bytes.NewReader(body), int64(len(body))); err != nil {
		log.Fatalf("could not upload: %+v", err)
	}
	fmt.Printf("uploaded %d bytes\n", len(body))

	// Read it back to be sure it is really there and really that long.
	fi, err := cli.Stat(ctx, "user/analysis/note.txt")
	if err != nil {
		log.Fatalf("could not stat: %+v", err)
	}
	if fi.Size != int64(len(body)) {
		log.Fatalf("server holds %d bytes, wrote %d", fi.Size, len(body))
	}

	if err := cli.Remove(ctx, "user/analysis/note.txt"); err != nil {
		log.Fatalf("could not remove: %+v", err)
	}
	fmt.Println("round trip verified")
}
