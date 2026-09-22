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

// TestWeightedIsUnweightedWhenWeightsAreOne checks the weighted fit reduces
// to the ordinary one when every weight is one, which it must.
func TestWeightedIsUnweightedWhenWeightsAreOne(t *testing.T) {
	rnd := rand.New(rand.NewPCG(1, 2))

	const lo, hi = 0.0, 10.0
	data, err := pdf.Generate(rnd, pdf.Gaussian(), lo, hi, []float64{5, 1}, 5000)
	if err != nil {
		t.Fatalf("could not generate: %+v", err)
	}

	ones := make([]float64, len(data))
	for i := range ones {
		ones[i] = 1
	}

	pars := []minuit.Par{
		{Name: "mean", Value: 4.5, Step: 0.2, Min: lo, Max: hi},
		{Name: "sigma", Value: 1.2, Step: 0.1, Min: 0.01, Max: 5},
	}

	plain, err := pdf.FitUnbinned(data, pdf.Gaussian(), lo, hi, pars)
	if err != nil {
		t.Fatalf("could not fit: %+v", err)
	}
	wtd, err := pdf.FitWeighted(data, ones, pdf.Gaussian(), lo, hi, pars)
	if err != nil {
		t.Fatalf("could not fit weighted: %+v", err)
	}

	for i, name := range []string{"mean", "sigma"} {
		pv, pe := plain.Value(i)
		wv, we := wtd.Value(i)
		if math.Abs(pv-wv) > 1e-4 {
			t.Errorf("%s: plain=%v, weighted=%v", name, pv, wv)
		}
		// with unit weights the correction is the identity, so the
		// uncertainties should agree too.
		if math.Abs(pe-we)/pe > 0.02 {
			t.Errorf("%s: uncertainty plain=%v, weighted=%v", name, pe, we)
		}
	}
}

// TestWeightedCovarianceIsCorrected checks the correction does what it is
// for.
//
// Take a sample and give every event a weight of two. That is not twice the
// data -- it is the same events counted twice -- so the uncertainty must not
// fall by root two. An uncorrected weighted fit says it does; the correction
// puts it back.
func TestWeightedCovarianceIsCorrected(t *testing.T) {
	rnd := rand.New(rand.NewPCG(3, 4))

	const lo, hi = 0.0, 10.0
	data, err := pdf.Generate(rnd, pdf.Gaussian(), lo, hi, []float64{5, 1}, 4000)
	if err != nil {
		t.Fatalf("could not generate: %+v", err)
	}

	twos := make([]float64, len(data))
	ones := make([]float64, len(data))
	for i := range twos {
		twos[i] = 2
		ones[i] = 1
	}

	pars := []minuit.Par{
		{Name: "mean", Value: 4.5, Step: 0.2, Min: lo, Max: hi},
		{Name: "sigma", Value: 1.2, Step: 0.1, Min: 0.01, Max: 5},
	}

	unit, err := pdf.FitWeighted(data, ones, pdf.Gaussian(), lo, hi, pars)
	if err != nil {
		t.Fatalf("could not fit with unit weights: %+v", err)
	}
	double, err := pdf.FitWeighted(data, twos, pdf.Gaussian(), lo, hi, pars)
	if err != nil {
		t.Fatalf("could not fit with weights of two: %+v", err)
	}

	for i, name := range []string{"mean", "sigma"} {
		_, ue := unit.Value(i)
		_, de := double.Value(i)

		// the corrected uncertainty must be the same: the information in
		// the sample did not change.
		if math.Abs(de-ue)/ue > 0.03 {
			t.Errorf("%s: corrected uncertainty went from %v to %v when every weight doubled", name, ue, de)
		}

		// and the minimiser's own, uncorrected, should have fallen by
		// about root two -- which is the mistake being corrected.
		raw := math.Sqrt(double.Minuit.Covariance().At(i, i))
		if want := ue / math.Sqrt2; math.Abs(raw-want)/want > 0.05 {
			t.Errorf("%s: the uncorrected uncertainty is %v, expected about %v", name, raw, want)
		}
	}
}

// TestEffectiveEntries checks the count of how much a weighted sample is
// worth.
func TestEffectiveEntries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		weights []float64
		want    float64
	}{
		{"unit weights", []float64{1, 1, 1, 1}, 4},
		{"all doubled", []float64{2, 2, 2, 2}, 4},
		{"one big one", []float64{10, 1, 1, 1}, 169.0 / 103.0},
		{"nothing", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := pdf.EffectiveEntries(tc.weights); math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("got=%v, want=%v", got, tc.want)
			}
		})
	}
}

// TestFitSWeighted takes the output of an sPlot and fits it, which is the
// reason weighted fits are here at all.
func TestFitSWeighted(t *testing.T) {
	const (
		lo, hi = 0.0, 10.0
		nsig   = 4000
		nbkg   = 6000
	)

	rnd := rand.New(rand.NewPCG(5, 6))

	// signal peaks in the mass and is gaussian in the control variable;
	// background is flat in the mass and elsewhere in the control.
	type event struct{ mass, ctrl float64 }
	var events []event

	for range nsig {
		m, err := pdf.Generate(rnd, pdf.Gaussian(), lo, hi, []float64{5, 0.4}, 1)
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, event{mass: m[0], ctrl: 3 + rnd.NormFloat64()*0.8})
	}
	for range nbkg {
		events = append(events, event{mass: rnd.Float64() * 10, ctrl: 7 + rnd.NormFloat64()*0.8})
	}

	mass := make([]float64, len(events))
	ctrl := make([]float64, len(events))
	for i, e := range events {
		mass[i] = e.mass
		ctrl[i] = e.ctrl
	}

	model := pdf.Add([]pdf.PDF{pdf.Gaussian(), pdf.Uniform()}, []string{"nsig", "nbkg"})

	res, err := pdf.FitUnbinned(mass, model, lo, hi, []minuit.Par{
		{Name: "nsig", Value: 3000, Step: 100, Min: 0, Max: 50000},
		{Name: "nbkg", Value: 5000, Step: 100, Min: 0, Max: 50000},
		{Name: "mean", Value: 4.8, Step: 0.1, Min: lo, Max: hi},
		{Name: "sigma", Value: 0.5, Step: 0.05, Min: 0.01, Max: 3},
	})
	if err != nil {
		t.Fatalf("could not fit the mass: %+v", err)
	}

	sw, err := pdf.SPlot(mass, model, lo, hi, res.Values())
	if err != nil {
		t.Fatalf("could not compute sWeights: %+v", err)
	}

	// the signal weights, and a fit of the control variable with them.
	wsig := make([]float64, len(events))
	for i := range events {
		wsig[i] = sw.W[i][0]
	}

	fit, err := pdf.FitWeighted(ctrl, wsig, pdf.Gaussian(), 0, 12, []minuit.Par{
		{Name: "mean", Value: 5, Step: 0.3, Min: 0, Max: 12},
		{Name: "sigma", Value: 1.5, Step: 0.2, Min: 0.05, Max: 5},
	})
	if err != nil {
		t.Fatalf("could not fit the sWeighted control variable: %+v", err)
	}

	gotMean, meanErr := fit.Value(0)
	gotSigma, _ := fit.Value(1)

	// it should find the signal's control distribution, not the mixture's.
	if math.Abs(gotMean-3) > 6*meanErr {
		t.Errorf("mean: got=%v +/- %v, want=3", gotMean, meanErr)
	}
	if math.Abs(gotSigma-0.8) > 0.15 {
		t.Errorf("sigma: got=%v, want=0.8", gotSigma)
	}

	if !fit.Weighted {
		t.Error("the fit does not know it was weighted")
	}

	// sWeights are negative for some events, so the sample is worth rather
	// fewer than the events in it.
	if eff := pdf.EffectiveEntries(wsig); eff >= float64(len(events)) {
		t.Logf("effective entries %v of %d events", eff, len(events))
	}
}

func TestWeightedErrors(t *testing.T) {
	g := pdf.Gaussian()
	pars := []minuit.Par{
		{Name: "mean", Value: 5, Step: 0.2},
		{Name: "sigma", Value: 1, Step: 0.1},
	}

	if _, err := pdf.FitWeighted(nil, nil, g, 0, 10, pars); err == nil {
		t.Error("an empty dataset was accepted")
	}
	if _, err := pdf.FitWeighted([]float64{1, 2}, []float64{1}, g, 0, 10, pars); err == nil {
		t.Error("mismatched weights were accepted")
	}
	if _, err := pdf.FitWeighted([]float64{1, 2}, []float64{1, 1}, g, 0, 10, pars[:1]); err == nil {
		t.Error("the wrong number of parameters was accepted")
	}
	if _, err := pdf.NLLWeighted([]float64{1}, []float64{1, 1}, g, 0, 10); err == nil {
		t.Error("mismatched weights were accepted by NLLWeighted")
	}
}

// TestBernsteinIsPositive checks the basis does what it is chosen for: with
// non-negative coefficients the density never goes below zero, where a plain
// polynomial with the same freedom does.
func TestBernsteinIsPositive(t *testing.T) {
	const lo, hi = 0.0, 10.0

	b := pdf.Bernstein(3, lo, hi)
	if got, want := b.NPar(), 4; got != want {
		t.Fatalf("npar: got=%d, want=%d", got, want)
	}

	par := []float64{1, 0.2, 0.05, 0.8}
	for x := lo; x <= hi; x += 0.05 {
		if got := b.Shape(x, par); got < 0 {
			t.Fatalf("at x=%v: got=%v, want it non-negative", x, got)
		}
	}

	// the integral over the whole range is exact: the sum of the
	// coefficients times (hi-lo)/(n+1).
	var sum float64
	for _, c := range par {
		sum += c
	}
	if got, want := b.Integral(lo, hi, par), sum*(hi-lo)/4; math.Abs(got-want) > 1e-12 {
		t.Errorf("integral: got=%v, want=%v", got, want)
	}

	// and it agrees with quadrature.
	const n = 200000
	var (
		dx  = (hi - lo) / n
		num float64
	)
	for i := range n {
		num += b.Shape(lo+(float64(i)+0.5)*dx, par) * dx
	}
	if got := b.Integral(lo, hi, par); math.Abs(got-num)/num > 1e-5 {
		t.Errorf("closed form=%v, quadrature=%v", got, num)
	}
}

func TestBernsteinPanics(t *testing.T) {
	for _, tc := range []struct {
		n      int
		lo, hi float64
	}{
		{-1, 0, 1},
		{2, 1, 1},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Bernstein(%d, %v, %v) was allowed", tc.n, tc.lo, tc.hi)
				}
			}()
			pdf.Bernstein(tc.n, tc.lo, tc.hi)
		}()
	}
}

// TestKeys checks a kernel density estimate follows the sample it was built
// from, and normalises.
func TestKeys(t *testing.T) {
	const lo, hi = 0.0, 10.0

	rnd := rand.New(rand.NewPCG(7, 8))
	data, err := pdf.Generate(rnd, pdf.Gaussian(), lo, hi, []float64{5, 1}, 20000)
	if err != nil {
		t.Fatalf("could not generate: %+v", err)
	}

	k, err := pdf.Keys(data, lo, hi, 1)
	if err != nil {
		t.Fatalf("could not build the estimate: %+v", err)
	}

	if got, want := k.NPar(), 0; got != want {
		t.Fatalf("npar: got=%d, want=%d", got, want)
	}

	// it should be close to the gaussian it came from.
	g := pdf.Gaussian()
	for _, x := range []float64{3.5, 4.5, 5, 5.5, 6.5} {
		var (
			got  = pdf.Eval(k, x, lo, hi, nil)
			want = pdf.Eval(g, x, lo, hi, []float64{5, 1})
		)
		if math.Abs(got-want)/want > 0.1 {
			t.Errorf("at x=%v: estimate=%v, gaussian=%v", x, got, want)
		}
	}

	// and it is a density.
	if got := k.Integral(lo, hi, nil); math.Abs(got-1) > 1e-2 {
		t.Errorf("the estimate integrates to %v, want 1", got)
	}

	// nothing outside the range.
	if got := k.Shape(-1, nil); got != 0 {
		t.Errorf("below the range: got=%v, want=0", got)
	}
}

// TestKeysHoldsUpAtTheEdge checks the reflection at the ends: a flat sample
// should give a flat estimate right up to the boundary, where an unreflected
// one sags to half.
func TestKeysHoldsUpAtTheEdge(t *testing.T) {
	const lo, hi = 0.0, 10.0

	rnd := rand.New(rand.NewPCG(9, 10))
	data := make([]float64, 20000)
	for i := range data {
		data[i] = rnd.Float64() * 10
	}

	k, err := pdf.Keys(data, lo, hi, 1)
	if err != nil {
		t.Fatalf("could not build the estimate: %+v", err)
	}

	mid := k.Shape(5, nil)
	for _, x := range []float64{0.05, 0.3, 9.7, 9.95} {
		got := k.Shape(x, nil)
		if math.Abs(got-mid)/mid > 0.15 {
			t.Errorf("at x=%v the estimate is %v, the middle is %v", x, got, mid)
		}
	}
}

func TestKeysErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		data   []float64
		lo, hi float64
		scale  float64
	}{
		{"too few events", []float64{1}, 0, 10, 1},
		{"no range", []float64{1, 2}, 10, 0, 1},
		{"bad scale", []float64{1, 2}, 0, 10, 0},
		{"no spread", []float64{5, 5, 5}, 0, 10, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pdf.Keys(tc.data, tc.lo, tc.hi, tc.scale); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
