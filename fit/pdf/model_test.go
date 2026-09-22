// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf_test

import (
	"math"
	"math/rand/v2"
	"testing"

	"go-hep.org/x/hep/fit/minuit"
	"go-hep.org/x/hep/fit/pdf"
)

// TestGenerate checks the toys really come from the density they were asked
// for, by fitting them and getting back what went in.
func TestGenerate(t *testing.T) {
	rnd := rand.New(rand.NewPCG(1, 2))

	const (
		mean  = 3.0
		sigma = 0.8
		lo    = 0.0
		hi    = 6.0
	)

	data, err := pdf.Generate(rnd, pdf.Gaussian(), lo, hi, []float64{mean, sigma}, 20000)
	if err != nil {
		t.Fatalf("could not generate: %+v", err)
	}
	if got, want := len(data), 20000; got != want {
		t.Fatalf("generated %d values, want %d", got, want)
	}

	for _, x := range data {
		if x < lo || x > hi {
			t.Fatalf("generated %v, outside [%v, %v]", x, lo, hi)
		}
	}

	res, err := pdf.FitUnbinned(data, pdf.Gaussian(), lo, hi, []minuit.Par{
		{Name: "mean", Value: 2, Step: 0.2, Min: lo, Max: hi},
		{Name: "sigma", Value: 1, Step: 0.1, Min: 1e-3, Max: 5},
	})
	if err != nil {
		t.Fatalf("could not fit the toys: %+v", err)
	}

	gotMean, meanErr := res.Value(0)
	gotSigma, sigErr := res.Value(1)

	if math.Abs(gotMean-mean) > 5*meanErr {
		t.Errorf("mean: got=%v +/- %v, want=%v", gotMean, meanErr, mean)
	}
	if math.Abs(gotSigma-sigma) > 5*sigErr {
		t.Errorf("sigma: got=%v +/- %v, want=%v", gotSigma, sigErr, sigma)
	}
}

func TestGenerateErrors(t *testing.T) {
	rnd := rand.New(rand.NewPCG(1, 2))

	if _, err := pdf.Generate(rnd, pdf.Gaussian(), 5, 1, []float64{0, 1}, 10); err == nil {
		t.Error("an inverted range was accepted")
	}
	// a gaussian of zero width is zero everywhere: there is nothing to draw.
	if _, err := pdf.Generate(rnd, pdf.Gaussian(), 0, 1, []float64{0, 0}, 10); err == nil {
		t.Error("a density that is zero everywhere was accepted")
	}
}

// TestProduct checks a product is the product, and that it normalises as a
// whole rather than component by component.
func TestProduct(t *testing.T) {
	// a gaussian times a linear efficiency.
	eff, err := pdf.Formula("slope*x + 1", []string{"slope"})
	if err != nil {
		t.Fatalf("could not build the efficiency: %+v", err)
	}

	p := pdf.Mul(pdf.Gaussian(), eff)

	if got, want := p.NPar(), 3; got != want {
		t.Fatalf("npar: got=%d, want=%d", got, want)
	}
	if got, want := p.ParNames(), []string{"mean", "sigma", "slope"}; len(got) != len(want) {
		t.Fatalf("par names: got=%v, want=%v", got, want)
	}

	par := []float64{5.0, 1.0, 0.1}
	for _, x := range []float64{3, 5, 7} {
		var (
			got  = p.Shape(x, par)
			want = pdf.Gaussian().Shape(x, par[:2]) * eff.Shape(x, par[2:])
		)
		if math.Abs(got-want) > 1e-12 {
			t.Errorf("at x=%v: got=%v, want=%v", x, got, want)
		}
	}

	// and it is a density once normalised.
	const n = 4000
	var (
		lo, hi = 0.0, 10.0
		dx     = (hi - lo) / n
		sum    float64
	)
	for i := range n {
		sum += pdf.Eval(p, lo+(float64(i)+0.5)*dx, lo, hi, par) * dx
	}
	if math.Abs(sum-1) > 2e-3 {
		t.Errorf("the product integrates to %v, want 1", sum)
	}
}

// TestConstraint checks a gaussian penalty pulls a parameter towards where it
// is held, and tightens the uncertainty that comes out.
func TestConstraint(t *testing.T) {
	rnd := rand.New(rand.NewPCG(3, 4))

	const (
		lo = 0.0
		hi = 10.0
	)
	data, err := pdf.Generate(rnd, pdf.Gaussian(), lo, hi, []float64{5, 1}, 500)
	if err != nil {
		t.Fatalf("could not generate: %+v", err)
	}

	pars := []minuit.Par{
		{Name: "mean", Value: 5, Step: 0.2, Min: lo, Max: hi},
		{Name: "sigma", Value: 1, Step: 0.1, Min: 1e-3, Max: 5},
	}

	free, err := pdf.FitUnbinned(data, pdf.Gaussian(), lo, hi, pars)
	if err != nil {
		t.Fatalf("could not fit: %+v", err)
	}
	freeMean, freeErr := free.Value(0)

	// now hold the mean at 6, tightly, and see it pulled that way.
	nll := pdf.Constrain(pdf.NLL(data, pdf.Gaussian(), lo, hi), 0, 6.0, 0.01)
	held, err := pdf.Fit(nll, pars)
	if err != nil {
		t.Fatalf("could not fit with the constraint: %+v", err)
	}
	heldMean, heldErr := held.Value(0)

	if !(heldMean > freeMean) {
		t.Errorf("the constraint did not pull the mean up: free=%v, held=%v", freeMean, heldMean)
	}
	if math.Abs(heldMean-6) > 0.1 {
		t.Errorf("a tight constraint at 6 left the mean at %v", heldMean)
	}
	if heldErr >= freeErr {
		t.Errorf("the constraint did not tighten the uncertainty: free=%v, held=%v", freeErr, heldErr)
	}
}

// TestSimultaneous fits two datasets that share a parameter and checks the
// shared one is measured by both.
func TestSimultaneous(t *testing.T) {
	rnd := rand.New(rand.NewPCG(5, 6))

	const (
		mean = 4.0 // shared between the channels
		lo   = 0.0
		hi   = 8.0
	)

	// two channels with the same peak position and different widths.
	a, err := pdf.Generate(rnd, pdf.Gaussian(), lo, hi, []float64{mean, 0.5}, 8000)
	if err != nil {
		t.Fatalf("could not generate channel a: %+v", err)
	}
	b, err := pdf.Generate(rnd, pdf.Gaussian(), lo, hi, []float64{mean, 1.5}, 8000)
	if err != nil {
		t.Fatalf("could not generate channel b: %+v", err)
	}

	// parameters: 0 is the shared mean, 1 and 2 the two widths.
	res, err := pdf.FitSimultaneous([]pdf.Channel{
		{Name: "a", Data: a, PDF: pdf.Gaussian(), Lo: lo, Hi: hi, Pars: []int{0, 1}},
		{Name: "b", Data: b, PDF: pdf.Gaussian(), Lo: lo, Hi: hi, Pars: []int{0, 2}},
	}, []minuit.Par{
		{Name: "mean", Value: 3, Step: 0.2, Min: lo, Max: hi},
		{Name: "sigmaA", Value: 1, Step: 0.1, Min: 1e-3, Max: 5},
		{Name: "sigmaB", Value: 1, Step: 0.1, Min: 1e-3, Max: 5},
	})
	if err != nil {
		t.Fatalf("could not fit: %+v", err)
	}

	gotMean, meanErr := res.Value(0)
	sigA, _ := res.Value(1)
	sigB, _ := res.Value(2)

	if math.Abs(gotMean-mean) > 5*meanErr {
		t.Errorf("shared mean: got=%v +/- %v, want=%v", gotMean, meanErr, mean)
	}
	if math.Abs(sigA-0.5) > 0.05 {
		t.Errorf("sigmaA: got=%v, want=0.5", sigA)
	}
	if math.Abs(sigB-1.5) > 0.1 {
		t.Errorf("sigmaB: got=%v, want=1.5", sigB)
	}

	// the shared mean is measured by both channels, so it must be known
	// better than the narrow channel alone could know it.
	alone, err := pdf.FitUnbinned(a, pdf.Gaussian(), lo, hi, []minuit.Par{
		{Name: "mean", Value: 3, Step: 0.2, Min: lo, Max: hi},
		{Name: "sigma", Value: 1, Step: 0.1, Min: 1e-3, Max: 5},
	})
	if err != nil {
		t.Fatalf("could not fit channel a alone: %+v", err)
	}
	_, aloneErr := alone.Value(0)

	if meanErr >= aloneErr {
		t.Errorf("two channels (%v) did no better than one (%v)", meanErr, aloneErr)
	}

	if got, want := len(res.Channels), 2; got != want {
		t.Errorf("channels: got=%d, want=%d", got, want)
	}
}

func TestSimultaneousErrors(t *testing.T) {
	g := pdf.Gaussian()
	pars := []minuit.Par{{Name: "m", Value: 0, Step: 0.1}}

	for _, tc := range []struct {
		name  string
		chans []pdf.Channel
	}{
		{"none", nil},
		{"no density", []pdf.Channel{{Name: "a", Lo: 0, Hi: 1}}},
		{"wrong parameter count", []pdf.Channel{{Name: "a", PDF: g, Lo: 0, Hi: 1, Pars: []int{0}}}},
		{"bad range", []pdf.Channel{{Name: "a", PDF: g, Lo: 1, Hi: 0, Pars: []int{0, 0}}}},
		{"parameter out of range", []pdf.Channel{{Name: "a", PDF: g, Lo: 0, Hi: 1, Pars: []int{0, 9}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pdf.FitSimultaneous(tc.chans, pars); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

// TestScan checks a profile scan is lowest at the fitted value and that the
// interval it gives back matches the parabolic uncertainty for a fit that is
// parabolic.
func TestScan(t *testing.T) {
	rnd := rand.New(rand.NewPCG(7, 8))

	const (
		mean = 5.0
		lo   = 0.0
		hi   = 10.0
	)
	data, err := pdf.Generate(rnd, pdf.Gaussian(), lo, hi, []float64{mean, 1}, 5000)
	if err != nil {
		t.Fatalf("could not generate: %+v", err)
	}

	res, err := pdf.FitUnbinned(data, pdf.Gaussian(), lo, hi, []minuit.Par{
		{Name: "mean", Value: 4.5, Step: 0.1, Min: lo, Max: hi},
		{Name: "sigma", Value: 1.2, Step: 0.1, Min: 1e-3, Max: 5},
	})
	if err != nil {
		t.Fatalf("could not fit: %+v", err)
	}

	fitMean, fitErr := res.Value(0)

	scan, err := res.Scan(0, fitMean-4*fitErr, fitMean+4*fitErr, 17)
	if err != nil {
		t.Fatalf("could not scan: %+v", err)
	}
	if got, want := len(scan), 17; got != want {
		t.Fatalf("scan points: got=%d, want=%d", got, want)
	}

	// the profile is lowest where the fit put the parameter.
	best := 0
	for i := range scan {
		if scan[i].Delta < scan[best].Delta {
			best = i
		}
	}
	if math.Abs(scan[best].Value-fitMean) > 0.6*fitErr {
		t.Errorf("the profile is lowest at %v, the fit said %v", scan[best].Value, fitMean)
	}
	if math.Abs(scan[best].Delta) > 1e-3 {
		t.Errorf("the profile bottoms out at Delta=%v, want~0", scan[best].Delta)
	}

	// a gaussian fit is parabolic, so the interval where Delta crosses UP
	// should be the parabolic uncertainty either side.
	ilo, ihi, ok := pdf.Interval(scan, 0.5)
	if !ok {
		t.Fatalf("no interval found in the scan")
	}
	for _, tc := range []struct {
		name     string
		got, wnt float64
	}{
		{"lower", fitMean - ilo, fitErr},
		{"upper", ihi - fitMean, fitErr},
	} {
		if math.Abs(tc.got-tc.wnt) > 0.15*tc.wnt {
			t.Errorf("%s side: got=%v, want~%v", tc.name, tc.got, tc.wnt)
		}
	}
}

func TestScanErrors(t *testing.T) {
	rnd := rand.New(rand.NewPCG(9, 10))
	data, err := pdf.Generate(rnd, pdf.Gaussian(), 0, 10, []float64{5, 1}, 500)
	if err != nil {
		t.Fatalf("could not generate: %+v", err)
	}

	res, err := pdf.FitUnbinned(data, pdf.Gaussian(), 0, 10, []minuit.Par{
		{Name: "mean", Value: 5, Step: 0.2, Min: 0, Max: 10},
		{Name: "sigma", Value: 1, Step: 0.1, Min: 1e-3, Max: 5},
	})
	if err != nil {
		t.Fatalf("could not fit: %+v", err)
	}

	for _, tc := range []struct {
		name   string
		i, n   int
		lo, hi float64
	}{
		{"no such parameter", 9, 5, 4, 6},
		{"too few points", 0, 1, 4, 6},
		{"inverted range", 0, 5, 6, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := res.Scan(tc.i, tc.lo, tc.hi, tc.n); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

// TestIntervalNotFound checks a scan that never rises far enough says so
// rather than inventing a bound.
func TestIntervalNotFound(t *testing.T) {
	flat := []pdf.ScanPoint{
		{Value: 0, Delta: 0},
		{Value: 1, Delta: 0.01},
		{Value: 2, Delta: 0.02},
	}
	if _, _, ok := pdf.Interval(flat, 0.5); ok {
		t.Error("an interval was found where the likelihood never rose to it")
	}
	if _, _, ok := pdf.Interval(nil, 0.5); ok {
		t.Error("an interval was found in an empty scan")
	}
}
