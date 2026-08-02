// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hbook_test

import (
	"fmt"
	"log"

	"go-hep.org/x/hep/hbook"
)

// ExampleNewEfficiency measures the efficiency of a selection as a function of
// transverse momentum, the shape of nearly every trigger and reconstruction
// performance plot.
func ExampleNewEfficiency() {
	var (
		// the two histograms have to share a binning: one counts the events
		// that were looked at, the other the subset that passed
		total = hbook.NewH1D(4, 0, 40)
		pass  = hbook.NewH1D(4, 0, 40)
	)
	total.Annotation()["name"] = "total"
	pass.Annotation()["name"] = "pass"

	// 40 events per bin, of which 0, 8, 34 and 40 passed: a turn-on rising
	// from nothing to a plateau, which is where the error bars matter most.
	for i, n := range []int{0, 8, 34, 40} {
		x := 10*float64(i) + 5
		for j := range 40 {
			total.Fill(x, 1)
			if j < n {
				pass.Fill(x, 1)
			}
		}
	}

	eff, err := hbook.NewEfficiency(pass, total)
	if err != nil {
		log.Fatalf("could not compute the efficiency: %+v", err)
	}

	fmt.Printf("%-12s %-8s %s\n", "pt [GeV]", "eff", "68% interval")
	for i := range eff.Len() {
		b := eff.Bin(i)
		fmt.Printf("[%2.0f, %2.0f)     %-8.3f [%.3f, %.3f]\n",
			b.XRange.Min, b.XRange.Max, b.Eff, b.EffLo(), b.EffHi(),
		)
	}

	// the interval never leaves [0, 1], and it closes up on the side an
	// efficiency of 0 or 1 cannot move towards. Propagating the errors of the
	// two histograms in quadrature, as DivideH1D does, gets both wrong.
	//
	// eff.S2D() hands the same numbers to the plotting packages, and
	// *Efficiency itself satisfies the gonum/plot XYer, XErrorer and YErrorer
	// interfaces, so hplot.NewS2D can take it directly.

	// Output:
	// pt [GeV]     eff      68% interval
	// [ 0, 10)     0.000    [0.000, 0.045]
	// [10, 20)     0.200    [0.134, 0.284]
	// [20, 30)     0.850    [0.771, 0.908]
	// [30, 40)     1.000    [0.955, 1.000]
}

// ExampleBinomialInterval puts an uncertainty on a single cut efficiency,
// without a histogram in sight.
func ExampleBinomialInterval() {
	const (
		pass  = 17.0
		total = 20.0
	)

	for _, kind := range []hbook.IntervalKind{
		hbook.ClopperPearson,
		hbook.Wilson,
		hbook.AgrestiCoull,
		hbook.Normal,
	} {
		lo, hi := hbook.BinomialInterval(kind, pass, total, 0.95)
		fmt.Printf("%-16v %d/%d = %.3f, 95%% CL [%.3f, %.3f]\n",
			kind, int(pass), int(total), pass/total, lo, hi,
		)
	}

	// Output:
	// clopper-pearson  17/20 = 0.850, 95% CL [0.621, 0.968]
	// wilson           17/20 = 0.850, 95% CL [0.640, 0.948]
	// agresti-coull    17/20 = 0.850, 95% CL [0.631, 0.956]
	// normal           17/20 = 0.850, 95% CL [0.694, 1.000]
}
