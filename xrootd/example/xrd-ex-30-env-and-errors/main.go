// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-30-env-and-errors configures the client from the environment and tells failures apart.
//
// Every bound in example 28 also reads an XRD_* variable, with the same names
// the C++ client uses, so a job wrapper can tune a program it did not write.
// An option passed in code wins over the environment.
//
//	export XRD_STREAMTIMEOUT=120
//	export XRD_CONNECTIONWINDOW=45
//	export XRD_CONNECTIONRETRY=8
//	export XRD_TCPKEEPALIVE=1
//	export XRD_TCPKEEPALIVETIME=30
//	export XRD_TCPKEEPALIVEINTERVAL=10
//	export XRD_TCPKEEPALIVEPROBES=3
//
// The second half is the part that decides whether a job retries or gives up:
// a missing file, a credential that is not good enough, and a network that
// went quiet all arrive as an error, and treating them alike either wastes an
// hour retrying a 404 or throws away a run over one dropped packet.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"time"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	cli, err := xrootd.NewClient(ctx, "storage.example.org:1094", "gopher")
	if err != nil {
		log.Fatalf("could not connect: %+v", err)
	}
	defer cli.Close()

	for _, name := range os.Args[1:] {
		st, err := cli.FS().Stat(ctx, name)
		switch {
		case err == nil:
			fmt.Printf("ok      %-50s %d\n", name, st.EntrySize)

		// The portable classification, and the one to reach for first: the
		// protocol's kXR_NotFound and kXR_NotAuthorized are mapped onto the
		// io/fs sentinels, so this same switch works over WebDAV too.
		case errors.Is(err, fs.ErrNotExist):
			fmt.Printf("missing %-50s\n", name)
		case errors.Is(err, fs.ErrPermission):
			fmt.Printf("denied  %-50s (check the token or the proxy)\n", name)

		// The protocol error itself, when the code matters. ServerError
		// carries the server's own code and message.
		default:
			var se xrdproto.ServerError
			if errors.As(err, &se) {
				fmt.Printf("server  %-50s %v: %s\n", name, se.Code, se.Message)
				continue
			}
			// Anything left is the network: a timeout, a reset, a link that
			// went quiet and was declared dead by the stream timeout. THIS is
			// the class worth retrying at the job level.
			fmt.Printf("network %-50s %v -- retry later\n", name, err)
		}
	}

	// Deadlines belong to the caller, not to the library. A context bounds
	// the whole operation; the options in example 28 bound each step within
	// it.
	short, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := cli.FS().Dirlist(short, "/store"); errors.Is(err, context.DeadlineExceeded) {
		fmt.Println("listing gave up on the caller's deadline")
	}
}
