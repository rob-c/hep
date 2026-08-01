// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdhttp_test // import "go-hep.org/x/hep/xrootd/xrdhttp"

import (
	"context"
	"fmt"
	"log"
	"time"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdhttp"
)

// Listing a remote collection over WebDAV.
//
// A listing is a PROPFIND with Depth: 1, and the collection itself is one of
// the members the server answers with; it is removed here, so what comes back
// is what a listing means everywhere else. Servers disagree about whether an
// href is an absolute URL or a bare path, and about how much of it is escaped —
// the name is what is left after that has been undone.
func Example_listingOverWebDAV() {
	ctx := context.Background()

	cli, err := xrdhttp.Dial("davs://webdav.example.com:2880/atlas/rucio",
		xrdhttp.WithDiscoveredBearerToken(),
	)
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}

	entries, err := cli.Dirlist(ctx, "user/analysis")
	if err != nil {
		log.Fatalf("could not list the collection: %v", err)
	}
	for _, e := range entries {
		kind := "file"
		if e.IsDir {
			kind = "dir "
		}
		fmt.Printf("%s %10d %s %s\n",
			kind, e.Size, e.ModTime.UTC().Format(time.RFC3339), e.Name)
	}
}

// The same listing through the protocol-neutral interface, which is what makes
// a walk or a glob work over WebDAV as it does over the native protocol: they
// are written against xrdfs.FileSystem, and this client provides one.
//
// A program that takes a URL from its caller can therefore have one code path
// for root:// and davs:// alike, and choose the transport at the point it
// dials.
func Example_listingRecursiveOverWebDAV() {
	ctx := context.Background()

	cli, err := xrdhttp.Dial("davs://webdav.example.com:2880/atlas/rucio",
		xrdhttp.WithDiscoveredBearerToken(),
		xrdhttp.Hardened(),
	)
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}

	fsys := cli.FS()

	roots, err := xrdfs.Glob(ctx, fsys, "/mc23/**/*.root")
	if err != nil {
		log.Fatalf("could not glob: %v", err)
	}
	fmt.Printf("%d matching files\n", len(roots))

	var total int64
	err = xrdfs.Walk(ctx, fsys, "/mc23", func(p string, e xrdfs.EntryStat, err error) error {
		if err != nil {
			// A collection the token does not cover is not a failed walk.
			log.Printf("skipping %s: %v", p, err)
			return nil
		}
		if !e.IsDir() {
			total += e.EntrySize
		}
		return nil
	})
	if err != nil {
		log.Fatalf("could not walk the namespace: %v", err)
	}
	fmt.Printf("%d bytes below /mc23\n", total)
}
