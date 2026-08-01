// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-10-root-xattr reads and writes extended attributes (kXR_fattr).
//
// Worth knowing: xattr failures arrive inside a SUCCESSFUL response. The
// server answers kXR_ok and puts each attribute's outcome in the body as a
// per-attribute code, so a client that reads only the status reports a get of
// a missing attribute as empty bytes and a rejected set as done. This library
// turns the per-attribute code into an error, which is why the checks below
// mean anything.
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

	xfs, ok := cli.FS().(xrdfs.XAttrFS)
	if !ok {
		log.Fatal("this server does not support extended attributes")
	}

	const name = "/store/user/gopher/data.root"

	if err := xfs.SetXAttr(ctx, name, "user.dataset", []byte("mc23_13p6TeV")); err != nil {
		log.Fatalf("could not set the attribute: %+v", err)
	}

	names, err := xfs.ListXAttr(ctx, name)
	if err != nil {
		log.Fatalf("could not list attributes: %+v", err)
	}
	fmt.Printf("attributes: %v\n", names)

	v, err := xfs.GetXAttr(ctx, name, "user.dataset")
	if err != nil {
		log.Fatalf("could not get the attribute: %+v", err)
	}
	fmt.Printf("user.dataset = %s\n", v)

	if err := xfs.DelXAttr(ctx, name, "user.dataset"); err != nil {
		log.Fatalf("could not delete the attribute: %+v", err)
	}
}
