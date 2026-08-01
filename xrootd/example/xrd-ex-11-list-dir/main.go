// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-11-list-dir lists one directory.
//
// A listing is a single request, and the stat information for every entry
// comes back with it: asking whether each name is a directory costs no round
// trip per name. Whether the server sent it is worth checking -- an old server
// answers with names alone, which is what HasStatInfo reports.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"go-hep.org/x/hep/xrootd"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cli, err := xrootd.NewClient(ctx, "eospublic.cern.ch:1094", "gopher")
	if err != nil {
		log.Fatalf("could not connect: %+v", err)
	}
	defer cli.Close()

	entries, err := cli.FS().Dirlist(ctx, "/eos/opendata/cms")
	if err != nil {
		log.Fatalf("could not list: %+v", err)
	}
	for _, e := range entries {
		if !e.HasStatInfo {
			fmt.Println(e.EntryName)
			continue
		}
		kind := "f"
		switch {
		case e.IsDir():
			kind = "d"
		case e.IsOffline():
			kind = "t" // on tape
		}
		fmt.Printf("%s %12d %s %s\n", kind, e.EntrySize,
			time.Unix(e.Mtime, 0).UTC().Format("2006-01-02 15:04"), e.EntryName)
	}
}
