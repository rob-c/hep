// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package spectrum_test

import (
	"fmt"
	"log"
	"math"

	"go-hep.org/x/hep/hbook"
	"go-hep.org/x/hep/hbook/spectrum"
)

func ExampleSearch() {
	// a spectrum: two lines on a continuum that falls away.
	h := hbook.NewH1D(1000, 0, 1000)
	for _, b := range h.Binning.Bins {
		x := b.XMid()
		y := 400 * math.Exp(-x/400)
		for _, mean := range []float64{350, 700} {
			d := (x - mean) / 10
			y += 500 * math.Exp(-0.5*d*d)
		}
		h.Fill(x, y)
	}

	peaks, err := spectrum.Search(h, spectrum.Sigma(5), spectrum.Iterations(40))
	if err != nil {
		log.Fatalf("could not search: %+v", err)
	}

	for _, p := range peaks {
		fmt.Printf("peak at %.0f, standing %.0f above the background, %.0f wide\n",
			p.X, p.Y, p.Width)
	}

	// Output:
	// peak at 350, standing 498 above the background, 24 wide
	// peak at 700, standing 497 above the background, 24 wide
}

func ExampleBackground() {
	// a peak on a sloping continuum.
	ys := make([]float64, 200)
	for i := range ys {
		x := float64(i)
		d := (x - 100) / 5
		ys[i] = 50 + 0.5*x + 300*math.Exp(-0.5*d*d)
	}

	bkg := spectrum.Background(ys, spectrum.Iterations(20))

	for _, i := range []int{20, 100, 180} {
		fmt.Printf("channel %3d: spectrum %6.1f, background %6.1f\n", i, ys[i], bkg[i])
	}

	// The background follows the continuum where there is no peak, and
	// stays on it under the peak rather than climbing into it. It sits a
	// little below: clipping only ever lowers a channel, so what comes
	// back is the continuum less a shade.
	//
	// Output:
	// channel  20: spectrum   60.0, background   58.0
	// channel 100: spectrum  400.0, background   97.3
	// channel 180: spectrum  140.0, background  138.9
}
