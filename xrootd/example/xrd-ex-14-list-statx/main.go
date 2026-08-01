// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-14-list-statx asks about many paths in one request.
//
// Statx answers a whole batch in one round trip, and answers PER PATH: a path
// the server could not resolve comes back flagged rather than failing the
// batch and losing the answers for the paths that did work. This is how a job
// checks its input list without one round trip per file -- for a thousand
// inputs across the Atlantic, the difference is minutes.
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

	inputs := []string{
		"/eos/opendata/cms/a.root",
		"/eos/opendata/cms/b.root",
		"/eos/opendata/cms/Run2012B",
		"/eos/opendata/cms/never-existed.root",
	}

	flags, err := cli.FS().Statx(ctx, inputs)
	if err != nil {
		log.Fatalf("could not statx: %+v", err)
	}

	var missing, staging []string
	for i, fl := range flags {
		switch {
		case fl&xrdfs.StatIsDir != 0:
			fmt.Printf("%-45s directory\n", inputs[i])
		case fl&xrdfs.StatIsOffline != 0:
			staging = append(staging, inputs[i])
			fmt.Printf("%-45s on tape\n", inputs[i])
		case fl == 0:
			missing = append(missing, inputs[i])
			fmt.Printf("%-45s absent\n", inputs[i])
		default:
			fmt.Printf("%-45s available\n", inputs[i])
		}
	}
	fmt.Printf("\n%d absent, %d need staging (see 17)\n", len(missing), len(staging))
}
