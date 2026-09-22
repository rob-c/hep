// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf_test

import (
	"fmt"
	"log"
	"math/rand/v2"

	"go-hep.org/x/hep/fit/minuit"
	"go-hep.org/x/hep/fit/pdf"
)

// Example fits a peak on a falling background and says how many events are
// in each, which is the thing RooFit is usually reached for.
func Example() {
	const (
		lo = 0.0
		hi = 10.0
	)

	// a model: a gaussian peak plus an exponential background, with the
	// coefficients as yields rather than fractions, so the fit counts
	// events.
	model := pdf.Add(
		[]pdf.PDF{pdf.Gaussian(), pdf.Exponential()},
		[]string{"nsig", "nbkg"},
	)

	// some data to fit: 1500 in the peak and 8500 underneath it.
	rnd := rand.New(rand.NewPCG(20260922, 1))

	sig, err := pdf.Generate(rnd, pdf.Gaussian(), lo, hi, []float64{5, 0.4}, 1500)
	if err != nil {
		log.Fatalf("%+v", err)
	}
	bkg, err := pdf.Generate(rnd, pdf.Exponential(), lo, hi, []float64{-0.3}, 8500)
	if err != nil {
		log.Fatalf("%+v", err)
	}
	data := append(sig, bkg...)

	res, err := pdf.FitUnbinned(data, model, lo, hi, []minuit.Par{
		{Name: "nsig", Value: 1000, Step: 100, Min: 0, Max: 20000},
		{Name: "nbkg", Value: 5000, Step: 100, Min: 0, Max: 20000},
		{Name: "mean", Value: 4.5, Step: 0.1, Min: lo, Max: hi},
		{Name: "sigma", Value: 0.5, Step: 0.05, Min: 0.01, Max: 5},
		{Name: "slope", Value: -0.2, Step: 0.05, Min: -5, Max: 5},
	})
	if err != nil {
		log.Fatalf("%+v", err)
	}

	nsig, nsigErr := res.Value(0)
	nbkg, _ := res.Value(1)
	mean, _ := res.Value(2)

	fmt.Printf("events    = %d\n", len(data))
	fmt.Printf("signal    = %.0f +/- %.0f\n", nsig, nsigErr)
	fmt.Printf("background= %.0f\n", nbkg)
	fmt.Printf("peak at   = %.2f\n", mean)

	// The fit recovers the 1500 and 8500 that went in, to within the
	// uncertainty it quotes on them.

	// Output:
	// events    = 10000
	// signal    = 1513 +/- 59
	// background= 8487
	// peak at   = 5.01
}

// ExampleResult_Scan profiles the likelihood over the signal yield, which is
// where an interval or a limit is read off.
func ExampleResult_Scan() {
	const (
		lo = 0.0
		hi = 10.0
	)

	rnd := rand.New(rand.NewPCG(7, 7))
	data, err := pdf.Generate(rnd, pdf.Gaussian(), lo, hi, []float64{5, 1}, 2000)
	if err != nil {
		log.Fatalf("%+v", err)
	}

	res, err := pdf.FitUnbinned(data, pdf.Gaussian(), lo, hi, []minuit.Par{
		{Name: "mean", Value: 4.5, Step: 0.1, Min: lo, Max: hi},
		{Name: "sigma", Value: 1.2, Step: 0.1, Min: 0.01, Max: 5},
	})
	if err != nil {
		log.Fatalf("%+v", err)
	}

	mean, meanErr := res.Value(0)

	// profile the mean, refitting the width at every point.
	scan, err := res.Scan(0, mean-4*meanErr, mean+4*meanErr, 41)
	if err != nil {
		log.Fatalf("%+v", err)
	}

	// where the likelihood has risen by UP is one standard deviation.
	lo68, hi68, ok := pdf.Interval(scan, res.Minuit.ErrorDef())
	if !ok {
		log.Fatal("the scan did not reach one sigma")
	}

	fmt.Printf("parabolic: %.3f +/- %.3f\n", mean, meanErr)
	fmt.Printf("profiled : %.3f to %.3f\n", lo68, hi68)

	// A gaussian likelihood is parabolic, so the profiled interval comes
	// out the parabolic uncertainty either side. A shape that is not would
	// give two sides that differ, which is what profiling is for.

	// Output:
	// parabolic: 4.995 +/- 0.022
	// profiled : 4.973 to 5.017
}
