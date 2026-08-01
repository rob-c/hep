// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-20-dav-list lists a remote collection over WebDAV.
//
// A listing is a PROPFIND with Depth: 1, and the collection itself is one of
// the members the server answers with; it is removed here, so a listing means
// the same thing it does everywhere else. Servers disagree about whether an
// href is an absolute URL or a bare path, and about how much of it is escaped
// -- the name is what is left after that has been undone.
//
// cli.FS() is the SAME xrdfs.FileSystem the native client returns, so the walk
// and glob from 12 and 13 run unchanged here. A program that takes a URL from
// its caller can have one code path for root:// and davs:// alike.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"go-hep.org/x/hep/xrootd/xrdfs"
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

	// The WebDAV-native listing.
	entries, err := cli.Dirlist(ctx, "user/analysis")
	if err != nil {
		log.Fatalf("could not list: %+v", err)
	}
	for _, e := range entries {
		kind := "f"
		if e.IsDir {
			kind = "d"
		}
		fmt.Printf("%s %12d %s %s\n", kind, e.Size,
			e.ModTime.UTC().Format("2006-01-02 15:04"), e.Name)
	}

	// The protocol-neutral one, and everything built on it.
	fsys := cli.FS()

	paths, err := xrdfs.Glob(ctx, fsys, "/mc23/**/*.root")
	if err != nil {
		log.Fatalf("could not glob: %+v", err)
	}
	fmt.Printf("\n%d matching files\n", len(paths))

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
		log.Fatalf("could not walk: %+v", err)
	}
	fmt.Printf("%d bytes below /mc23\n", total)
}
