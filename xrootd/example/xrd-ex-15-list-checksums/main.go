// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-15-list-checksums lists a directory with a checksum next to every entry (kXR_dcksm).
//
// One round trip instead of one per file -- but the server reads every file in
// the directory before it answers any of it, so this is a different cost as
// well as a different request. For a single file's digest, ask ChecksumFS
// (see 08) rather than listing its parent.
//
// Entries that cannot have a digest -- subdirectories, files the server could
// not read -- come back present in the listing with an empty ChecksumValue,
// which is how "the file is there but its digest is not" is told apart from
// "this listing carried no checksums at all".
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdfs"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	cli, err := xrootd.NewClient(ctx, "storage.example.org:1094", "gopher")
	if err != nil {
		log.Fatalf("could not connect: %+v", err)
	}
	defer cli.Close()

	cfs, ok := cli.FS().(xrdfs.ChecksumDirFS)
	if !ok {
		log.Fatal("this server cannot checksum a whole directory")
	}

	// An empty algo takes the server's default.
	entries, err := cfs.DirlistChecksum(ctx, "/store/user/gopher/data", "adler32")
	if err != nil {
		log.Fatalf("could not list with checksums: %+v", err)
	}
	for _, e := range entries {
		digest := e.ChecksumValue()
		if digest == "" {
			digest = "-"
		}
		fmt.Printf("%-40s %-8s %s\n", e.EntryName, e.ChecksumAlgo(), digest)
	}
}
