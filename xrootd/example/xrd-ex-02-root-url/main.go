// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-02-root-url opens a root:// URL in one call.
//
// xrdio.Open takes the whole URL, picks the transport from the scheme, and
// hands back something that satisfies io.Reader, io.ReaderAt and io.Seeker --
// so a remote file drops straight into code written for os.File.
package main

import (
	"fmt"
	"io"
	"log"

	"go-hep.org/x/hep/xrootd/xrdio"
)

func main() {
	f, err := xrdio.Open("root://eospublic.cern.ch:1094//eos/opendata/cms/file.root")
	if err != nil {
		log.Fatalf("could not open: %+v", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		log.Fatalf("could not stat: %+v", err)
	}
	fmt.Printf("%s: %d bytes\n", f.Name(), fi.Size())

	// The first four bytes of a ROOT file are the magic "root".
	magic := make([]byte, 4)
	if _, err := io.ReadFull(f, magic); err != nil {
		log.Fatalf("could not read the magic: %+v", err)
	}
	fmt.Printf("magic: %q\n", magic)

	// Seek works too, so the trailer is one call away.
	if _, err := f.Seek(-64, io.SeekEnd); err != nil {
		log.Fatalf("could not seek: %+v", err)
	}
	tail := make([]byte, 64)
	if _, err := io.ReadFull(f, tail); err != nil {
		log.Fatalf("could not read the tail: %+v", err)
	}
	fmt.Printf("read %d trailing bytes\n", len(tail))
}
