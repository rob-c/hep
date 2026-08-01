// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-25-any-url serves root://, davs:// and https:// through one code path.
//
// xrootd.Dial picks the transport from the scheme and hands back a Backend,
// whose FS() is the same xrdfs.FileSystem in every case. A tool that takes a
// URL from its user does not need to know which protocol it got -- and the
// walk, the glob and the batch stat from 12-14 work over all of them.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdfs"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s <url>", os.Args[0])
	}
	raw := os.Args[1]

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// A scheme with no transport behind it is refused by name here, rather
	// than dialled as if it were something else.
	be, err := xrootd.Dial(ctx, raw, "gopher")
	if err != nil {
		log.Fatalf("could not dial %s: %+v", raw, err)
	}
	defer be.Close()

	// The URL parser is worth knowing about on its own: root://host//store/f
	// carries a DOUBLE slash, and a parser that treats it as an ordinary URL
	// opens a path one slash short and is told kXR_NotFound for a file that is
	// plainly there.
	u, err := xrootd.ParseURL(raw)
	if err != nil {
		log.Fatalf("could not parse %s: %+v", raw, err)
	}
	fmt.Printf("scheme=%s host=%s path=%s\n", u.Scheme, u.Addr, u.Path)

	entries, err := be.FS().Dirlist(ctx, u.Path)
	if err != nil {
		log.Fatalf("could not list: %+v", err)
	}
	for _, e := range entries {
		fmt.Printf("%-40s %12d\n", e.EntryName, e.EntrySize)
	}

	// Native-only features are reached by asking, not by knowing the scheme.
	if cks, ok := be.FS().(xrdfs.ChecksumFS); ok {
		algo, sum, err := cks.Checksum(ctx, u.Path)
		if err == nil {
			fmt.Printf("checksum: %s:%s\n", algo, sum)
		}
	}
}
