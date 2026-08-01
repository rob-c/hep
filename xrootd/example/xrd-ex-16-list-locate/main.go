// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-16-list-locate finds out where the replicas of a path actually live.
//
// A plain locate against the top of a federation answers with the managers one
// tier down, not with anything holding a byte. DeepLocate walks that tree for
// you and returns only data servers -- which is what you want when picking the
// nearest replica, or when working out why one site is slow.
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

	cli, err := xrootd.NewClient(ctx, "cms-xrd-global.cern.ch:1094", "gopher")
	if err != nil {
		log.Fatalf("could not connect: %+v", err)
	}
	defer cli.Close()

	lfs, ok := cli.FS().(xrdfs.LocateFS)
	if !ok {
		log.Fatal("this server does not answer locate requests")
	}

	const name = "/store/mc/RunIISummer20/AOD.root"

	// LocateRefresh skips the cache; LocateNoWait takes whatever is known now
	// rather than waiting for the best answer.
	locs, err := lfs.Locate(ctx, name, xrdfs.LocateRefresh)
	if err != nil {
		log.Fatalf("could not locate: %+v", err)
	}
	for _, l := range locs {
		fmt.Printf("%-40s kind=%c writable=%v\n", l.Addr, l.Kind, l.CanWrite())
	}

	// The replicas that really hold the bytes.
	servers, err := lfs.DeepLocate(ctx, name, 0)
	if err != nil {
		log.Fatalf("could not deep-locate: %+v", err)
	}
	fmt.Printf("\n%d data servers hold %s:\n", len(servers), name)
	for _, l := range servers {
		fmt.Printf("  %s\n", l.Addr)
	}
}
