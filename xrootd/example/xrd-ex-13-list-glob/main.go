// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-13-list-glob finds files by pattern.
//
//   - and ?  stay inside one path component
//     **         crosses them
//     [abc]      a character class, [a-z] a range, [!abc] its negation
//
// Only the directories the pattern can actually reach are listed: the literal
// prefix bounds the walk, so "/store/mc/**/*.root" never asks about
// "/store/data". Over a link with real latency that is the whole cost.
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

	cli, err := xrootd.NewClient(ctx, "eospublic.cern.ch:1094", "gopher")
	if err != nil {
		log.Fatalf("could not connect: %+v", err)
	}
	defer cli.Close()

	fsys := cli.FS()

	// An absolute pattern.
	paths, err := xrdfs.Glob(ctx, fsys, "/eos/opendata/cms/Run2012B/**/AOD*.root")
	if err != nil {
		log.Fatalf("could not glob: %+v", err)
	}
	for _, p := range paths {
		fmt.Println(p)
	}

	// RGlob is the common case spelled directly: this pattern at any depth
	// below that root.
	more, err := xrdfs.RGlob(ctx, fsys, "/eos/opendata/cms", "*.root")
	if err != nil {
		log.Fatalf("could not rglob: %+v", err)
	}
	fmt.Printf("%d .root files below /eos/opendata/cms\n", len(more))

	// Match is the same matcher without the network, for filtering a list you
	// already have.
	fmt.Println(xrdfs.Match("/store/mc/**/*.root", "/store/mc/run1/x/AOD.root")) // true
}
