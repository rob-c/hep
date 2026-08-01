// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-01-root-read reads a range of a file over the native XRootD protocol.
//
// The address is a host and a port; the path is separate and absolute. In a
// root:// URL the two are spelled together with a DOUBLE slash between them --
// root://host:1094//eos/... -- which is why the path here starts with one.
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

	const name = "/eos/opendata/cms/Run2012B/DoubleMuParked/file.root"

	fi, err := cli.FS().Stat(ctx, name)
	if err != nil {
		log.Fatalf("could not stat %s: %+v", name, err)
	}
	fmt.Printf("%s: %d bytes\n", name, fi.EntrySize)

	f, err := cli.FS().Open(ctx, name, xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
	if err != nil {
		log.Fatalf("could not open %s: %+v", name, err)
	}
	defer f.Close(ctx)

	// Reads are positional: there is no cursor on the server, and every read
	// names the offset it wants. That is what lets several be in flight at
	// once on one connection -- see 06.
	buf := make([]byte, 4096)
	n, err := f.ReadAtContext(ctx, buf, 0)
	if err != nil {
		log.Fatalf("could not read %s: %+v", name, err)
	}
	fmt.Printf("read %d bytes\n", n)
}
