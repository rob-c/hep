// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package minuit_test

import (
	"fmt"
	"log"
	"math"

	"go-hep.org/x/hep/fit/minuit"
)

// ExampleMinuit shows the call sequence of a TMinuit fit, line for line.
//
// In C++ it reads:
//
//	TMinuit *gMinuit = new TMinuit(2);
//	gMinuit->SetFCN(fcn);
//	Double_t arglist[10];
//	Int_t ierflg = 0;
//	arglist[0] = 1;
//	gMinuit->mnexcm("SET ERR", arglist, 1, ierflg);
//	gMinuit->mnparm(0, "mean",  0.0, 0.1, 0, 0, ierflg);
//	gMinuit->mnparm(1, "sigma", 1.0, 0.1, 0, 0, ierflg);
//	arglist[0] = 500;
//	arglist[1] = 1.0;
//	gMinuit->mnexcm("MIGRAD", arglist, 2, ierflg);
func ExampleMinuit() {
	// the function to minimise, in the shape TMinuit calls it.
	fcn := func(npar int, grad []float64, par []float64, iflag int) float64 {
		var (
			mean  = par[0]
			sigma = par[1]
			chi2  float64
		)
		// a chi-square with its minimum at mean = 2 and sigma = 0.5.
		chi2 += ((mean - 2) / 0.1) * ((mean - 2) / 0.1)
		chi2 += ((sigma - 0.5) / 0.05) * ((sigma - 0.5) / 0.05)
		return chi2
	}

	m := minuit.New(2)
	m.SetFCN(fcn)

	err := m.Command("SET ERR", 1) // a chi-square: one sigma is a rise of 1
	if err != nil {
		log.Fatalf("%+v", err)
	}

	err = m.Parameter(0, "mean", 0.0, 0.1, 0, 0)
	if err != nil {
		log.Fatalf("%+v", err)
	}
	err = m.Parameter(1, "sigma", 1.0, 0.1, 0, 0)
	if err != nil {
		log.Fatalf("%+v", err)
	}

	err = m.Command("MIGRAD", 500, 1.0)
	if err != nil {
		log.Fatalf("%+v", err)
	}
	err = m.Command("HESSE")
	if err != nil {
		log.Fatalf("%+v", err)
	}

	for i := range m.NPar() {
		val, e, _ := m.Value(i)
		fmt.Printf("%-6s = %.4f +/- %.4f\n", m.Parameters()[i].Name, val, e)
	}
	fmt.Printf("status  = %v\n", m.Status())

	// Output:
	// mean   = 2.0000 +/- 0.1000
	// sigma  = 0.5000 +/- 0.0500
	// status  = converged
}

// ExampleFitH1D fits a gaussian to a histogram, which is what h->Fit("gaus")
// does in ROOT.
func ExampleFitH1D() {
	// a histogram filled from a gaussian, without the randomness, so that
	// the answer is the same every time.
	h := newGausHist(2.0, 0.75)

	// GausPars takes the starting values off the histogram, which is what
	// lets h->Fit("gaus") work without being told where to start.
	fit, err := minuit.FitH1D(h, minuit.Gaussian, minuit.GausPars(h))
	if err != nil {
		log.Fatalf("%+v", err)
	}

	mean, meanErr, _ := fit.Value(1)
	sigma, sigmaErr, _ := fit.Value(2)

	fmt.Printf("mean  = %.3f +/- %.3f\n", mean, meanErr)
	fmt.Printf("sigma = %.3f +/- %.3f\n", sigma, sigmaErr)

	// The uncertainties are the ones 20000 entries buy: sigma/sqrt(N) on the
	// mean and sigma/sqrt(2N) on the width.

	// Output:
	// mean  = 2.000 +/- 0.005
	// sigma = 0.750 +/- 0.004
}

// ExampleMinuit_minos shows the asymmetric uncertainties MINOS finds on a
// function that is not parabolic, where the two sides genuinely differ.
func ExampleMinuit_minos() {
	// f rises steeply above its minimum and gently below it.
	m := minuit.New(1)
	m.SetFCN(minuit.FuncOf(func(par []float64) float64 {
		d := math.Exp(par[0]) - 1
		return d * d
	}))

	err := m.Parameter(0, "x", 0.1, 0.3, -2, 2)
	if err != nil {
		log.Fatalf("%+v", err)
	}
	if err := m.Command("MIGRAD"); err != nil {
		log.Fatalf("%+v", err)
	}
	if err := m.Command("MINOS"); err != nil {
		log.Fatalf("%+v", err)
	}

	eplus, eminus, eparab, _ := m.Errors(0)
	fmt.Printf("parabolic: +/- %.2f\n", eparab)
	fmt.Printf("minos:     +%.2f %.2f\n", eplus, eminus)

	// The parabolic uncertainty has to be one number and splits the
	// difference. MINOS finds the two sides: above the minimum the function
	// reaches a rise of 1 at ln(2), and below it never does at all, so the
	// search runs out at the limit the parameter was given.

	// Output:
	// parabolic: +/- 0.99
	// minos:     +0.69 -2.00
}
