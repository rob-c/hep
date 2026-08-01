// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-26-clone-checkpoint assembles a file out of ranges of others, on the server, atomically.
//
// kXR_clone is the copy that does not cross the network. Taking the same event
// selection out of a run's worth of files otherwise means reading every byte
// to the client and writing it straight back -- twice the network cost, with
// your uplink as the bottleneck. Here the ranges are named and the server
// moves them itself.
//
// Clone is not atomic on its own: a failure part-way leaves the ranges already
// copied in place. A checkpoint makes it all-or-nothing, which over a link
// that drops is the difference between a retry and a corrupt output file.
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

	// Source and destination must be open on the SAME connection.
	src, err := fsys.Open(ctx, "/store/user/gopher/run1.root",
		xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
	if err != nil {
		log.Fatalf("could not open the source: %+v", err)
	}
	defer src.Close(ctx)

	dst, err := fsys.Open(ctx, "/store/user/gopher/merged.root",
		xrdfs.OpenModeOwnerRead|xrdfs.OpenModeOwnerWrite,
		xrdfs.OpenOptionsNew|xrdfs.OpenOptionsMkPath)
	if err != nil {
		log.Fatalf("could not open the destination: %+v", err)
	}
	defer dst.Close(ctx)

	cloner, ok := dst.(xrdfs.Cloner)
	if !ok {
		log.Fatal("this server does not support kXR_clone")
	}
	ckp, ok := dst.(xrdfs.Checkpointer)
	if !ok {
		log.Fatal("this server does not support checkpoints")
	}

	// How much the server is prepared to undo.
	limits, err := ckp.CheckpointQuery(ctx)
	if err != nil {
		log.Fatalf("could not query the checkpoint limits: %+v", err)
	}
	fmt.Printf("checkpoint capacity: %d bytes (%d used)\n", limits.Capacity, limits.Used)

	if err := ckp.CheckpointBegin(ctx); err != nil {
		log.Fatalf("could not open a checkpoint: %+v", err)
	}

	// At most 1024 ranges per call; a longer list is refused outright.
	ranges := []xrdfs.CloneRange{
		{Src: src, SrcOffset: 0, Length: 4096, DstOffset: 0},
		{Src: src, SrcOffset: 1 << 20, Length: 8192, DstOffset: 4096},
	}
	if err := cloner.Clone(ctx, ranges); err != nil {
		if rerr := ckp.CheckpointRollback(ctx); rerr != nil {
			log.Printf("rollback also failed: %v", rerr)
		}
		log.Fatalf("clone failed and was rolled back: %+v", err)
	}

	if err := ckp.CheckpointCommit(ctx); err != nil {
		log.Fatalf("could not commit: %+v", err)
	}
	fmt.Println("merged")
}
