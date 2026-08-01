// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-04-root-download streams a remote file to local disk.
//
// xrdio.File is an io.Reader, so the copy is the one line you would write for
// a local file. There is a ready-made command for this too: xrootd/cmd/xrd-cp.
package main

import (
	"fmt"
	"io"
	"log"
	"os"

	"go-hep.org/x/hep/xrootd/xrdio"
)

func main() {
	src, err := xrdio.Open("root://eospublic.cern.ch:1094//eos/opendata/cms/file.root")
	if err != nil {
		log.Fatalf("could not open the remote file: %+v", err)
	}
	defer src.Close()

	dst, err := os.Create("file.root")
	if err != nil {
		log.Fatalf("could not create the local file: %+v", err)
	}
	defer dst.Close()

	n, err := io.Copy(dst, src)
	if err != nil {
		// A partial copy has left a partial file. Say so, and take it away:
		// a truncated file left on disk is read by the next job as a real one.
		os.Remove("file.root")
		log.Fatalf("copy failed after %d bytes: %+v", n, err)
	}
	if err := dst.Close(); err != nil {
		log.Fatalf("could not flush the local file: %+v", err)
	}
	fmt.Printf("copied %d bytes\n", n)
}
