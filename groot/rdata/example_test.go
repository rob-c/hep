// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rdata_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"go-hep.org/x/hep/groot/rdata"
)

// Opening a file and reading a column out of it, which is the whole of what
// most jobs need.
func ExampleOpen() {
	t, err := rdata.Open("../testdata/simple.root")
	if err != nil {
		log.Fatal(err)
	}
	defer t.Close()

	one, err := t.Floats("one")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("tree:    %q\n", t.Name())
	fmt.Printf("events:  %d\n", t.Rows())
	fmt.Printf("columns: %v\n", t.Columns())
	fmt.Printf("one:     %v\n", one)

	// Output:
	// tree:    "tree"
	// events:  4
	// columns: [one two three]
	// one:     [1 2 3 4]
}

// Several files read end to end, as though they had been one all along. The
// names may be a pattern instead — "../testdata/chain.flat.*.root" — and may
// be URLs on grid storage.
func ExampleOpenAll() {
	t, err := rdata.OpenAll([]string{
		"../testdata/chain.flat.1.root",
		"../testdata/chain.flat.2.root",
	})
	if err != nil {
		log.Fatal(err)
	}
	defer t.Close()

	fmt.Printf("events: %d\n", t.Rows())

	// Output:
	// events: 10
}

// What is in a file nobody wrote down.
func ExampleTrees() {
	names, err := rdata.Trees("../testdata/simple.root")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("trees: %v\n", names)

	// Output:
	// trees: [tree]
}

// A column holding several numbers per event: the length of each is how many
// there were in that event.
func ExampleTable_Arrays() {
	t, err := rdata.Open("../testdata/small-flat-tree.root")
	if err != nil {
		log.Fatal(err)
	}
	defer t.Close()

	jets, err := t.Arrays("SliceFloat64")
	if err != nil {
		log.Fatal(err)
	}

	for _, evt := range jets[:4] {
		fmt.Printf("%d: %v\n", len(evt), evt)
	}

	// Output:
	// 0: []
	// 1: [1]
	// 2: [2 2]
	// 3: [3 3 3]
}

// Walking the table one event at a time, which holds nothing and so does not
// care how large the file is.
func ExampleTable_Each() {
	t, err := rdata.Open("../testdata/simple.root")
	if err != nil {
		log.Fatal(err)
	}
	defer t.Close()

	var sum float64
	err = t.Each([]string{"one"}, func(r rdata.Row) error {
		sum += r.Float("one")
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("sum: %v\n", sum)

	// Output:
	// sum: 10
}

// Writing the answer out, in a form ROOT itself can open.
func ExampleSave() {
	dir, err := os.MkdirTemp("", "rdata-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	name := filepath.Join(dir, "out.root")

	err = rdata.Save(name, "results",
		rdata.Column{Name: "mass", Data: []float64{91.2, 125.1, 172.5}},
		rdata.Column{Name: "tag", Data: []string{"z", "h", "t"}},
	)
	if err != nil {
		log.Fatal(err)
	}

	t, err := rdata.Open(name)
	if err != nil {
		log.Fatal(err)
	}
	defer t.Close()

	mass, err := t.Floats("mass")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("events: %d\n", t.Rows())
	fmt.Printf("mass:   %v\n", mass)

	// Output:
	// events: 3
	// mass:   [91.2 125.1 172.5]
}
