// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrd_test

import (
	"fmt"
	"log"

	"go-hep.org/x/hep/groot"
	_ "go-hep.org/x/hep/groot/riofs/plugin/xrootd" // teaches groot the root:// schemes
	"go-hep.org/x/hep/xrootd/xrd"
)

// The shortest useful program: read a small file off grid storage.
func Example() {
	data, err := xrd.ReadFile("root://storage.example.org//store/user/gopher/runs.txt")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s", data)
}

// The same functions take a local path, so a program can be written and
// debugged against a file on your laptop and then pointed at the grid.
func ExampleReadFile_local() {
	data, err := xrd.ReadFile("runs.txt")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d bytes\n", len(data))
}

// Finding the files of a dataset, and looking at what they weigh.
func ExampleGlob() {
	files, err := xrd.Glob("root://storage.example.org//store/user/gopher/run*/AOD*.root")
	if err != nil {
		log.Fatal(err)
	}

	var total int64
	for _, name := range files {
		fi, err := xrd.Stat(name)
		if err != nil {
			log.Fatal(err)
		}
		total += fi.Size()
	}
	fmt.Printf("%d files, %.1f GB\n", len(files), float64(total)/(1<<30))
}

// Everything under a directory, however deep, without writing the recursion.
func ExampleFind() {
	files, err := xrd.Find("root://storage.example.org//store/user/gopher/mc")
	if err != nil {
		log.Fatal(err)
	}
	for _, name := range files {
		fmt.Println(name)
	}
}

// Copying a file down to work on it locally. The transfer resumes if it is
// interrupted, and is checked against the server's checksum.
func ExampleDownload() {
	err := xrd.Download(
		"root://storage.example.org//store/user/gopher/run42.root",
		"run42.root",
	)
	if err != nil {
		log.Fatal(err)
	}
}

// Writing results back. The directory is created if it is not there.
func ExampleWriteFile() {
	err := xrd.WriteFile(
		"root://storage.example.org//store/user/gopher/results/summary.txt",
		[]byte("42 events passed\n"),
	)
	if err != nil {
		log.Fatal(err)
	}
}

// ROOT files are groot's job, and groot takes the same URLs once the plugin is
// imported. Use xrd to decide which files, and groot to read them.
func Example_rootFiles() {
	files, err := xrd.Glob("root://storage.example.org//store/user/gopher/*.root")
	if err != nil {
		log.Fatal(err)
	}

	for _, name := range files {
		f, err := groot.Open(name)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s: %d keys\n", name, len(f.Keys()))
		f.Close()
	}
}

// Before a long job, ask whether it can work at all. This is the cheapest way
// to find an expired proxy: now, rather than in an hour.
func ExampleCheck() {
	if err := xrd.Check("root://storage.example.org//store/user/gopher"); err != nil {
		log.Fatal(err)
	}
}

// Bringing a whole dataset down, a few files at a time, into a directory that
// does not have to exist yet.
func ExampleDownloadAll() {
	files, err := xrd.Glob("root://storage.example.org//store/user/gopher/*.root")
	if err != nil {
		log.Fatal(err)
	}

	local, err := xrd.DownloadAll(files, "data")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d files under data/\n", len(local))
}

// How big is it? One call, whether it is a file or a tree of them.
func ExampleSize() {
	n, err := xrd.Size("root://storage.example.org//store/user/gopher/mc")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%.1f GB\n", float64(n)/(1<<30))
}

// A list of runs, one per line, is how such a list usually arrives.
func ExampleLines() {
	runs, err := xrd.Lines("root://storage.example.org//store/user/gopher/runs.txt")
	if err != nil {
		log.Fatal(err)
	}
	for _, run := range runs {
		fmt.Println(run)
	}
}
