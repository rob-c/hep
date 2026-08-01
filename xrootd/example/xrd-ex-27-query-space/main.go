// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-27-query-space asks the server about itself: free space, configuration, identity.
//
// VirtualStat answers with the space behind a path PREFIX rather than an
// existing object, so it can be asked before writing anything: it filters out
// the servers and partitions that could not hold a path starting that way.
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

	cli, err := xrootd.NewClient(ctx, "storage.example.org:1094", "gopher")
	if err != nil {
		log.Fatalf("could not connect: %+v", err)
	}
	defer cli.Close()

	fsys := cli.FS()

	vfs, err := fsys.VirtualStat(ctx, "/store/user/gopher")
	if err != nil {
		log.Fatalf("could not stat the vfs: %+v", err)
	}
	// Sizes are in MEGABYTES, and FreeRW is the largest CONTIGUOUS area
	// rather than the total: a partition with a terabyte free in scattered
	// pieces will not take a terabyte file, and this is the number that says
	// so.
	fmt.Printf("read/write: %d nodes, largest free area %d MB, %d%% used\n",
		vfs.NumberRW, vfs.FreeRW, vfs.UtilizationRW)
	fmt.Printf("staging:    %d nodes, largest free area %d MB, %d%% used\n",
		vfs.NumberStaging, vfs.FreeStaging, vfs.UtilizationStaging)

	qfs, ok := fsys.(xrdfs.QueryFS)
	if !ok {
		log.Fatal("this server does not answer queries")
	}

	// Logical space statistics for a path.
	space, err := qfs.Query(ctx, xrdfs.QuerySpace, "/store/user/gopher")
	if err != nil {
		log.Fatalf("could not query space: %+v", err)
	}
	fmt.Printf("space: %s\n", space)

	// Configuration values, one per name asked. A name the server has no
	// value for is simply absent from the map.
	cfg, err := qfs.QueryConfig(ctx, "version", "role", "sitename", "tpc")
	if err != nil {
		log.Fatalf("could not query the config: %+v", err)
	}
	for k, v := range cfg {
		fmt.Printf("%-10s %s\n", k, v)
	}

	// Label this connection in the server's monitoring stream, so an operator
	// looking at the server can tell whose traffic it is.
	if pfs, ok := fsys.(xrdfs.PropertyFS); ok {
		if err := pfs.SetAppID(ctx, "analysis-42"); err != nil {
			log.Printf("could not set the app id: %v", err)
		}
	}
}
