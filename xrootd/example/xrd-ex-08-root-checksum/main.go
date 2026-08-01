// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-08-root-checksum asks the server for a file's checksum and verifies the bytes against it.
//
// The server computes it from its own copy, so comparing it against a digest
// of what you received is an end-to-end check on the transfer -- the one thing
// that tells a truncated download from a complete one when the length happens
// to match.
package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
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

	const name = "/eos/opendata/cms/file.root"

	cks, ok := cli.FS().(xrdfs.ChecksumFS)
	if !ok {
		log.Fatal("this server does not report checksums")
	}
	algo, want, err := cks.Checksum(ctx, name)
	if err != nil {
		log.Fatalf("could not get the checksum: %+v", err)
	}
	fmt.Printf("server says %s:%s\n", algo, want)

	// Verify it against what actually arrived.
	data, err := readAll(ctx, cli.FS(), name)
	if err != nil {
		log.Fatalf("could not read: %+v", err)
	}
	if algo == "md5" {
		sum := md5.Sum(data)
		if got := hex.EncodeToString(sum[:]); got != want {
			log.Fatalf("checksum mismatch: got %s, server says %s", got, want)
		}
		fmt.Println("checksum verified")
	}
}

func readAll(ctx context.Context, fs xrdfs.FileSystem, name string) ([]byte, error) {
	f, err := fs.Open(ctx, name, xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
	if err != nil {
		return nil, err
	}
	defer f.Close(ctx)

	fi, err := f.Stat(ctx)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, fi.EntrySize)
	if _, err := f.ReadAtContext(ctx, buf, 0); err != nil {
		return nil, err
	}
	return buf, nil
}
