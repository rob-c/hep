// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-09-root-namespace-ops exercises the namespace operations: mkdir, touch, rename, truncate, remove.
//
// RemoveAll is a walk rather than a single request: it stops at what it cannot
// remove instead of returning nil having silently skipped a subtree.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
	const dir = "/store/user/gopher/work"

	// MkdirAll is happy with a directory that is already there.
	if err := fsys.MkdirAll(ctx, dir, xrdfs.OpenModeOwnerRead|xrdfs.OpenModeOwnerWrite|xrdfs.OpenModeOwnerExecute); err != nil {
		log.Fatalf("could not create %s: %+v", dir, err)
	}

	// Touch creates an empty file if it is not there, and does nothing if it
	// is. There is no request that moves a modification time, so unlike
	// touch(1) it cannot refresh one -- and does not pretend to.
	if err := xrdfs.Touch(ctx, fsys, dir+"/marker", xrdfs.OpenModeOwnerRead|xrdfs.OpenModeOwnerWrite); err != nil {
		log.Fatalf("could not touch: %+v", err)
	}

	if err := fsys.Rename(ctx, dir+"/marker", dir+"/marker.old"); err != nil {
		log.Fatalf("could not rename: %+v", err)
	}

	if err := fsys.Truncate(ctx, dir+"/marker.old", 0); err != nil {
		log.Fatalf("could not truncate: %+v", err)
	}

	// A missing path is fs.ErrNotExist, whatever the transport, and the
	// underlying kXR_ error is still there under errors.As if you want it.
	if _, err := fsys.Stat(ctx, dir+"/marker"); !errors.Is(err, fs.ErrNotExist) {
		log.Fatalf("expected the renamed file to be gone, got %+v", err)
	}

	if err := fsys.RemoveAll(ctx, dir); err != nil {
		log.Fatalf("could not remove %s: %+v", dir, err)
	}
	fmt.Println("cleaned up")
}
