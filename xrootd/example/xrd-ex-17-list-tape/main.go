// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-17-list-tape finds the files that are on tape and asks for them back.
//
// A file flagged offline is not missing -- it is on tape, and opening it
// blocks the job for however long the tape robot takes. The right move is to
// stage the whole input list up front, in one request, and come back later.
//
// Evict is the reverse, and CancelPrepare withdraws a request whose job has
// already given up: a prepare nobody cancels keeps a tape system busy on
// behalf of nothing.
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

	fsys := cli.FS()
	inputs := []string{
		"/store/data/run1/AOD_001.root",
		"/store/data/run1/AOD_002.root",
		"/store/data/run1/AOD_003.root",
	}

	// Which of them are not on disk?
	flags, err := fsys.Statx(ctx, inputs)
	if err != nil {
		log.Fatalf("could not statx: %+v", err)
	}
	var offline []string
	for i, fl := range flags {
		if fl&xrdfs.StatIsOffline != 0 {
			offline = append(offline, inputs[i])
		}
	}
	if len(offline) == 0 {
		fmt.Println("everything is on disk")
		return
	}

	pfs, ok := fsys.(xrdfs.PrepareFS)
	if !ok {
		log.Fatal("this server does not support prepare")
	}

	// Priority 1 of 0..3. The handle is what a later cancellation names.
	handle, err := pfs.Stage(ctx, offline, 1)
	if err != nil {
		log.Fatalf("could not stage: %+v", err)
	}
	fmt.Printf("staging %d files, handle %q\n", len(offline), handle)

	// If the job is abandoned before the tape system gets there:
	//	if err := pfs.CancelPrepare(ctx, handle); err != nil { ... }
	//
	// And when the disk copies are no longer wanted:
	//	if err := pfs.Evict(ctx, offline); err != nil { ... }
	_ = handle
}
