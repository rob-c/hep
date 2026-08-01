// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd_test // import "go-hep.org/x/hep/xrootd"

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"time"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdfs"
)

// Listing one directory on a remote server.
//
// A listing is a single request, and the stat information for every entry comes
// back with it: asking whether each name is a directory does not cost a round
// trip per name. Whether the server sent that information is worth checking,
// because an old server may answer with names alone.
func Example_listing() {
	ctx := context.Background()

	cli, err := xrootd.NewClient(ctx, "server.example.com:1094", "cms001")
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}
	defer cli.Close()

	entries, err := cli.FS().Dirlist(ctx, "/store/data/Run2024")
	if err != nil {
		log.Fatalf("could not list the directory: %v", err)
	}

	for _, e := range entries {
		if !e.HasStatInfo {
			fmt.Println(e.EntryName)
			continue
		}
		kind := "file"
		if e.IsDir() {
			kind = "dir "
		}
		fmt.Printf("%s %10d %s %s\n",
			kind, e.EntrySize,
			time.Unix(e.Mtime, 0).UTC().Format(time.RFC3339),
			e.EntryName,
		)
	}
}

// Listing a whole subtree.
//
// Walk is depth first and lexical, like filepath.WalkDir, and reports a
// directory it could not list a second time — with the error, and without
// descending — rather than abandoning the walk. A namespace with mixed
// permissions is the normal case on the grid, and only the caller knows whether
// one unreadable directory makes the answer wrong. Returning nil from that call
// carries on; returning the error stops the walk and hands it back.
//
// Opaque data on the root — the "?authz=..." an authorized request travels
// with — is carried into every listing below it, so a walk of a
// token-authorized namespace stays authorized.
func Example_listingRecursive() {
	ctx := context.Background()

	cli, err := xrootd.NewClient(ctx, "server.example.com:1094", "cms001")
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}
	defer cli.Close()

	var (
		files int
		bytes int64
	)
	err = xrdfs.Walk(ctx, cli.FS(), "/store/data/Run2024", func(p string, e xrdfs.EntryStat, err error) error {
		switch {
		case err != nil:
			// Unreadable, and not fatal: note it and keep going.
			log.Printf("skipping %s: %v", p, err)
			return nil
		case e.IsDir():
			// Nothing here is worth descending into.
			if e.EntryName == ".snapshot" {
				return fs.SkipDir
			}
			return nil
		}
		files++
		bytes += e.EntrySize
		return nil
	})
	if err != nil {
		log.Fatalf("could not walk the namespace: %v", err)
	}
	fmt.Printf("%d files, %d bytes\n", files, bytes)
}

// Finding files by pattern, without listing the whole namespace.
//
// "*" and "?" stay inside one path component and "**" crosses them, which is
// what somebody writing "/store/mc/**/*.root" means. Only the directories the
// pattern can reach are listed, so the leading literal part of it — here
// /store/mc — is what decides the cost.
func Example_listingGlob() {
	ctx := context.Background()

	cli, err := xrootd.NewClient(ctx, "server.example.com:1094", "cms001")
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}
	defer cli.Close()

	paths, err := xrdfs.Glob(ctx, cli.FS(), "/store/mc/**/AOD*.root")
	if err != nil {
		log.Fatalf("could not glob: %v", err)
	}
	for _, p := range paths {
		fmt.Println(p)
	}
}

// Asking about many paths at once.
//
// Statx answers a whole batch in one request, and answers per path: a path the
// server could not resolve comes back flagged offline rather than failing the
// batch and losing the answers for the paths that did work. This is how a job
// checks that its input list is present without one round trip per file.
func Example_listingBatchStat() {
	ctx := context.Background()

	cli, err := xrootd.NewClient(ctx, "server.example.com:1094", "cms001")
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}
	defer cli.Close()

	paths := []string{
		"/store/data/Run2024/a.root",
		"/store/data/Run2024/b.root",
		"/store/data/Run2024/missing.root",
	}
	flags, err := cli.FS().Statx(ctx, paths)
	if err != nil {
		log.Fatalf("could not stat the paths: %v", err)
	}
	for i, fl := range flags {
		switch {
		case fl&xrdfs.StatIsOffline != 0:
			fmt.Printf("%s: on tape, staging needed\n", paths[i])
		case fl&xrdfs.StatIsDir != 0:
			fmt.Printf("%s: directory\n", paths[i])
		default:
			fmt.Printf("%s: available\n", paths[i])
		}
	}
}
