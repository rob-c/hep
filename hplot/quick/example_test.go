// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package quick_test

import (
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"path/filepath"

	"go-hep.org/x/hep/hplot/quick"
)

// sample is a column of numbers standing in for one read out of a ROOT file.
func sample(n int, mu, sigma float64) []float64 {
	rnd := rand.New(rand.NewPCG(1, 2))
	out := make([]float64, n)
	for i := range out {
		out[i] = mu + sigma*rnd.NormFloat64()
	}
	return out
}

func ExampleHist() {
	dir, err := os.MkdirTemp("", "quick-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	px := sample(10000, 0, 20)

	err = quick.Hist(filepath.Join(dir, "px.png"), px,
		quick.Title("px of every event"),
		quick.XLabel("px [GeV]"),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("px.png written")

	// Output:
	// px.png written
}

func ExampleHists() {
	dir, err := os.MkdirTemp("", "quick-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	err = quick.Hists(filepath.Join(dir, "mass.png"), []quick.Series{
		{Name: "data", Data: sample(5000, 91, 3)},
		{Name: "simulation", Data: sample(5000, 90, 4)},
	},
		quick.Title("dimuon mass"),
		quick.XLabel("m [GeV]"),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("mass.png written")

	// Output:
	// mass.png written
}

func ExampleScatter() {
	dir, err := os.MkdirTemp("", "quick-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	var (
		x = sample(2000, 0, 1)
		y = make([]float64, len(x))
	)
	for i, v := range x {
		y[i] = v * v
	}

	err = quick.Scatter(filepath.Join(dir, "xy.png"), x, y,
		quick.XLabel("x"), quick.YLabel("x²"),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("xy.png written")

	// Output:
	// xy.png written
}

func ExampleBin() {
	h, err := quick.Bin(sample(10000, 0, 20))
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("entries: %d\n", h.Entries())
	fmt.Printf("bins:    %d\n", len(h.Binning.Bins))
	fmt.Printf("range:   [%v, %v]\n", h.Binning.XRange.Min, h.Binning.XRange.Max)
	fmt.Printf("mean:    %.3f\n", h.XMean())

	// Output:
	// entries: 10000
	// bins:    71
	// range:   [-95, 82.5]
	// mean:    0.170
}
