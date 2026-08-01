// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-03-root-write creates a file on a remote server and writes to it.
//
// OpenOptionsNew refuses to overwrite; use OpenOptionsDelete to replace.
// OpenOptionsMkPath creates the parent directories on the way.
//
// Note CloseVerify rather than Close: it tells the server how many bytes it
// should be holding, and fails if it is holding a different number. A Close
// that returns nil having stored a truncated file is a real failure mode over
// a link that drops, and it is the one nobody checks for.
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

	const name = "/store/user/gopher/out.dat"

	f, err := cli.FS().Open(ctx, name,
		xrdfs.OpenModeOwnerRead|xrdfs.OpenModeOwnerWrite,
		xrdfs.OpenOptionsNew|xrdfs.OpenOptionsMkPath,
	)
	if err != nil {
		log.Fatalf("could not create %s: %+v", name, err)
	}

	data := []byte("hello from go-hep\n")
	if err := f.WriteAtContext(ctx, data, 0); err != nil {
		f.Close(ctx)
		log.Fatalf("could not write: %+v", err)
	}

	// Commit, then close and check the length the server ended up with.
	if err := f.Sync(ctx); err != nil {
		f.Close(ctx)
		log.Fatalf("could not sync: %+v", err)
	}
	if err := f.CloseVerify(ctx, int64(len(data))); err != nil {
		log.Fatalf("close did not verify: %+v", err)
	}
	fmt.Printf("wrote %d bytes to %s\n", len(data), name)
}
