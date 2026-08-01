// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-12-list-walk walks a whole subtree.
//
// Walk is depth first and lexical, like filepath.WalkDir. A directory it could
// not list is reported a SECOND time -- with the error, and without descending
// -- rather than abandoning the walk: a namespace with mixed permissions is
// the normal case on the grid, and only the caller knows whether one
// unreadable directory makes the answer wrong.
//
// This matters over a bad link. A walk of a large namespace is hundreds of
// requests over minutes, long enough to lose a connection in the middle. The
// client re-establishes it, so the directories after the loss are still
// listed; a walk that aborted there would report a namespace far smaller than
// it is with nothing in the result to say so.
//
//	return nil        -> carry on
//	return err        -> end the walk, err comes back from Walk
//	return fs.SkipDir -> skip this subtree without listing it
//	return fs.SkipAll -> end the walk, Walk returns nil
package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"strings"
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

	var (
		files, dirs, skipped int
		bytes                int64
	)
	err = xrdfs.Walk(ctx, cli.FS(), "/eos/opendata/cms/Run2012B", func(p string, e xrdfs.EntryStat, err error) error {
		switch {
		case err != nil:
			// Unreadable, and not fatal.
			skipped++
			log.Printf("skipping %s: %v", p, err)
			return nil
		case e.IsDir():
			dirs++
			if strings.HasPrefix(e.EntryName, ".") {
				return fs.SkipDir
			}
		default:
			files++
			bytes += e.EntrySize
		}
		return nil
	})
	if err != nil {
		log.Fatalf("could not walk: %+v", err)
	}
	fmt.Printf("%d dirs, %d files, %d bytes, %d unreadable\n", dirs, files, bytes, skipped)
}
