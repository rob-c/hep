// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-32-simple-api is the whole of grid storage in one screen, using the
// xrd package: no connections, no contexts, no options.
//
// This is the program to copy if you have not written Go before. Change the
// two constants at the top and run it. Every name here can be a local path
// instead of a URL, which is the easiest way to try the program out before
// pointing it at a server.
//
// The other examples in this directory use the packages underneath, which give
// you control over every one of those things — start here, go there when you
// need to.
package main

import (
	"fmt"
	"log"

	"go-hep.org/x/hep/xrootd/xrd"
)

const (
	server = "root://storage.example.org:1094"
	base   = "/store/user/gopher"
)

func main() {
	// What is in my directory?
	entries, err := xrd.List(server + "/" + base)
	if err != nil {
		log.Fatal(err)
	}
	for _, e := range entries {
		kind := "file"
		if e.IsDir() {
			kind = "dir "
		}
		fmt.Printf("%s %10d  %s\n", kind, e.Size(), e.Name())
	}

	// Which of them are the ROOT files of this dataset, wherever they sit?
	files, err := xrd.Glob(server + "/" + base + "/run*/AOD*.root")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\n%d files match\n", len(files))

	// Add up what they weigh. The sizes came with the listing, so this costs
	// one request per directory rather than one per file.
	var total int64
	for _, name := range files {
		fi, err := xrd.Stat(name)
		if err != nil {
			log.Fatal(err)
		}
		total += fi.Size()
	}
	fmt.Printf("%.2f GB in total\n", float64(total)/(1<<30))

	// Write a result back. The directory is created if it is not there.
	out := server + "/" + base + "/results/summary.txt"
	err = xrd.WriteFile(out, []byte(fmt.Sprintf("%d files, %d bytes\n", len(files), total)))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote %s\n", out)

	// And read it back, to be sure it is really there.
	data, err := xrd.ReadFile(out)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("it says: %s", data)

	// Bring the first file down to work on locally. An interrupted transfer
	// resumes where it stopped, and is checked against the server's checksum.
	if len(files) > 0 {
		if err := xrd.Download(files[0], "run.root"); err != nil {
			log.Fatal(err)
		}
		fmt.Println("downloaded run.root")
	}
}
