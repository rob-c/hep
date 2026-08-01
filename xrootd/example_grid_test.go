// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd_test // import "go-hep.org/x/hep/xrootd"

import (
	"context"
	"fmt"
	"log"
	"time"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdio"
)

// Reading a file from a storage element over the native protocol: connect,
// list, open, read.
//
// The address is a host and a port; the path is separate and absolute. In a
// root:// URL the two are spelled together with a double slash between them —
// root://host:1094//store/data/file.root — which is why the path below still
// starts with a single one.
func Example_rootAccess() {
	ctx := context.Background()

	cli, err := xrootd.NewClient(ctx, "server.example.com:1094", "cms001")
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}
	defer cli.Close()

	fs := cli.FS()

	entries, err := fs.Dirlist(ctx, "/store/data")
	if err != nil {
		log.Fatalf("could not list the directory: %v", err)
	}
	for _, e := range entries {
		fmt.Printf("%s %d\n", e.EntryName, e.EntrySize)
	}

	f, err := fs.Open(ctx, "/store/data/file.root", xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
	if err != nil {
		log.Fatalf("could not open the file: %v", err)
	}
	defer f.Close(ctx)

	// Reads are positional: there is no cursor on the server, and every read
	// names the offset it wants. That is what lets several of them be in
	// flight at once on one connection.
	buf := make([]byte, 4096)
	n, err := f.ReadAtContext(ctx, buf, 0)
	if err != nil {
		log.Fatalf("could not read the file: %v", err)
	}
	fmt.Printf("read %d bytes\n", n)
}

// The same thing in one call, when a URL is what the program has: xrdio.Open
// takes the whole URL, picks the transport from its scheme, and gives back
// something that satisfies io.ReaderAt and io.Seeker.
func Example_rootAccessByURL() {
	f, err := xrdio.Open("root://server.example.com:1094//store/data/file.root")
	if err != nil {
		log.Fatalf("could not open the file: %v", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		log.Fatalf("could not stat the file: %v", err)
	}
	fmt.Printf("%s is %d bytes\n", f.Name(), fi.Size())

	buf := make([]byte, 4096)
	if _, err := f.ReadAt(buf, 0); err != nil {
		log.Fatalf("could not read the file: %v", err)
	}
}

// Reading over a wide-area network that will not always fail loudly.
//
// The failure worth planning for is not a refused connection — that one is
// reported immediately — but a path that stops forwarding while both ends still
// believe the connection is up. Left to TCP, a read on such a connection can
// block for the better part of an hour. xrootd.Hardened bounds it: the client
// gives up on a silent server after a minute, keeps the connection warm through
// stateful firewalls, and retries the connection itself, which is the one thing
// that can be repeated without risking a request being carried out twice.
func Example_hardenedAgainstBadNetworks() {
	// The caller's context is still the outer bound. Everything below decides
	// how quickly a hung connection is noticed inside it.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	cli, err := xrootd.NewClient(ctx, "server.example.com:1094", "cms001",
		xrootd.Hardened(),

		// Each part is separately adjustable. A site that stages from tape can
		// legitimately say nothing for longer than the default minute.
		xrootd.WithStreamTimeout(5*time.Minute),

		// Failures that reach nobody — a response for a caller that has gone, a
		// data connection the server would not bind — are dropped unless they
		// are asked for. On a bad network they are the first sign of trouble.
		xrootd.WithErrorHandler(func(err error) { log.Printf("xrootd: %v", err) }),
	)
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}
	defer cli.Close()

	if _, err := cli.FS().Stat(ctx, "/store/data/file.root"); err != nil {
		log.Fatalf("could not stat the file: %v", err)
	}
}
