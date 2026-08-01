// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-07-root-pgread reads with a per-page CRC-32C (kXR_pgread).
//
// This one is on-theme for a bad network. An ordinary read trusts the bytes
// that arrive; a paged read carries a CRC for every 4 KiB page, so silent
// corruption introduced anywhere between the disk and this process is caught
// here rather than three weeks later in a physics plot.
//
// Not every server implements it, hence the interface check.
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cli, err := xrootd.NewClient(ctx, "eospublic.cern.ch:1094", "gopher")
	if err != nil {
		log.Fatalf("could not connect: %+v", err)
	}
	defer cli.Close()

	f, err := cli.FS().Open(ctx, "/eos/opendata/cms/file.root",
		xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
	if err != nil {
		log.Fatalf("could not open: %+v", err)
	}
	defer f.Close(ctx)

	pr, ok := f.(xrdfs.PgReader)
	if !ok {
		log.Fatal("this server does not offer paged reads")
	}

	buf := make([]byte, 64<<10)
	n, err := pr.PgReadAt(ctx, buf, 0)
	if err != nil {
		// A CRC mismatch arrives here, named as such, rather than as data.
		log.Fatalf("paged read failed: %+v", err)
	}
	fmt.Printf("read and verified %d bytes\n", n)
}
