// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-05-root-vector-read reads a scattered set of ranges in one round trip (kXR_readv).
//
// This is what makes reading a handful of TTree branches out of a remote file
// affordable. Ten separate ReadAt calls across the Atlantic is ten times the
// latency; one vector read is one.
//
// A vector read is all-or-nothing: a server that stops short of the last
// segment is an error, not a short result, so there is no way to mistake a
// truncated answer for the data.
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

	rv, ok := f.(xrdfs.VectorReader)
	if !ok {
		log.Fatal("this server does not offer vector reads")
	}

	segs := []xrdfs.ReadVSegment{
		{Offset: 0, Length: 512},
		{Offset: 1 << 20, Length: 4096},
		{Offset: 8 << 20, Length: 4096},
	}
	chunks, err := rv.ReadVAt(ctx, segs)
	if err != nil {
		log.Fatalf("vector read failed: %+v", err)
	}
	for i, c := range chunks {
		fmt.Printf("segment %d @%d: %d bytes\n", i, segs[i].Offset, len(c))
	}
}
