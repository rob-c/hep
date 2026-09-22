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

// TestNormalisation checks each density integrates to one once divided by
// its own integral, which is the property that makes it a density at all.
func TestNormalisation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		p      pdf.PDF
		par    []float64
		lo, hi float64
	}{
		{"gaussian", pdf.Gaussian(), []float64{0, 1}, -10, 10},
		{"gaussian off-centre", pdf.Gaussian(), []float64{3, 0.5}, -10, 10},
		{"exponential", pdf.Exponential(), []float64{-0.5}, 0, 20},
		{"exponential flat", pdf.Exponential(), []float64{0}, 0, 5},
		{"uniform", pdf.Uniform(), nil, -2, 3},
		{"pol2", pdf.Polynomial(2), []float64{1, 0.1, 0.01}, 0, 10},
		{"crystal ball", pdf.CrystalBall(), []float64{0, 1, 1.5, 3}, -10, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// integrate the normalised density by hand, finely.
			const n = 20000
			var (
				sum float64
				dx  = (tc.hi - tc.lo) / n
			)
			for i := range n {
				x := tc.lo + (float64(i)+0.5)*dx
				sum += pdf.Eval(tc.p, x, tc.lo, tc.hi, tc.par) * dx
			}

			if math.Abs(sum-1) > 1e-4 {
				t.Errorf("integral of the normalised density: got=%v, want=1", sum)
			}
		})
	}
}

// TestGaussianIntegralIsExact checks the closed form against quadrature,
// since the whole point of having one is that it is right.
func TestGaussianIntegralIsExact(t *testing.T) {
	g := pdf.Gaussian()
	par := []float64{1.5, 0.8}

	const n = 200000
	var (
		lo, hi = -6.0, 9.0
		dx     = (hi - lo) / n
		sum    float64
	)
	for i := range n {
		x := lo + (float64(i)+0.5)*dx
		sum += g.Shape(x, par) * dx
	}

	got := g.Integral(lo, hi, par)
	if math.Abs(got-sum)/sum > 1e-6 {
		t.Errorf("closed form=%v, quadrature=%v", got, sum)
	}
}

// TestFitGaussian checks an unbinned fit recovers parameters it was not told.
func TestFitGaussian(t *testing.T) {
	const (
		mean  = 2.0
		sigma = 0.5
		n     = 20000
	)

	rnd := rand.New(rand.NewPCG(1, 2))
	data := make([]float64, 0, n)
	for len(data) < n {
		x := mean + sigma*rnd.NormFloat64()
		if x < 0 || x > 5 {
			continue
		}
		data = append(data, x)
	}

	res, err := pdf.FitUnbinned(data, pdf.Gaussian(), 0, 5, []minuit.Par{
		{Name: "mean", Value: 1.0, Step: 0.1},
		{Name: "sigma", Value: 1.0, Step: 0.1, Min: 1e-3, Max: 10},
	})
	if err != nil {
		t.Fatalf("could not fit: %+v", err)
	}

	gotMean, meanErr := res.Value(0)
	gotSigma, sigErr := res.Value(1)

	if math.Abs(gotMean-mean) > 5*meanErr {
		t.Errorf("mean: got=%v +/- %v, want=%v", gotMean, meanErr, mean)
	}
	if math.Abs(gotSigma-sigma) > 5*sigErr {
		t.Errorf("sigma: got=%v +/- %v, want=%v", gotSigma, sigErr, sigma)
	}

	// the uncertainty on the mean of n events is sigma/sqrt(n).
	if want := sigma / math.Sqrt(n); math.Abs(meanErr-want) > 0.2*want {
		t.Errorf("uncertainty on the mean: got=%v, want~%v", meanErr, want)
	}
}

// TestExtendedFit checks an extended fit counts the events in each component
// rather than only telling them apart by shape.
func TestExtendedFit(t *testing.T) {
	const (
		nsig = 2000
		nbkg = 8000
		lo   = 0.0
		hi   = 10.0
	)

	rnd := rand.New(rand.NewPCG(3, 4))

	var data []float64
	for len(data) < nsig {
		x := 5 + 0.4*rnd.NormFloat64()
		if x < lo || x > hi {
			continue
		}
		data = append(data, x)
	}
	// an exponential background, by inversion.
	const slope = -0.3
	for range nbkg {
		u := rnd.Float64()
		x := math.Log(1+u*(math.Exp(slope*hi)-1)) / slope
		data = append(data, x)
	}

	model := pdf.Add(
		[]pdf.PDF{pdf.Gaussian(), pdf.Exponential()},
		[]string{"nsig", "nbkg"},
	)

	res, err := pdf.FitUnbinned(data, model, lo, hi, []minuit.Par{
		{Name: "nsig", Value: 1000, Step: 100, Min: 0, Max: 50000},
		{Name: "nbkg", Value: 5000, Step: 100, Min: 0, Max: 50000},
		{Name: "mean", Value: 4.5, Step: 0.1, Min: lo, Max: hi},
		{Name: "sigma", Value: 0.5, Step: 0.05, Min: 1e-3, Max: 5},
		{Name: "slope", Value: -0.2, Step: 0.05, Min: -5, Max: 5},
	})
	if err != nil {
		t.Fatalf("could not fit: %+v", err)
	}

	gotSig, sigErr := res.Value(0)
	gotBkg, bkgErr := res.Value(1)
	gotMean, _ := res.Value(2)

	if math.Abs(gotSig-nsig) > 5*sigErr {
		t.Errorf("nsig: got=%v +/- %v, want=%v", gotSig, sigErr, nsig)
	}
	if math.Abs(gotBkg-nbkg) > 5*bkgErr {
		t.Errorf("nbkg: got=%v +/- %v, want=%v", gotBkg, bkgErr, nbkg)
	}
	if math.Abs(gotMean-5) > 0.05 {
		t.Errorf("mean: got=%v, want=5", gotMean)
	}

	// the yields must add up to the events there are.
	if got, want := gotSig+gotBkg, float64(len(data)); math.Abs(got-want) > 0.02*want {
		t.Errorf("total yield: got=%v, want~%v", got, want)
	}

	// and a signal yield of 2000 should be known to about its square root.
	if want := math.Sqrt(nsig); math.Abs(sigErr-want) > 0.5*want {
		t.Errorf("uncertainty on nsig: got=%v, want~%v", sigErr, want)
	}
}

// TestComponents checks each piece of a fitted sum can be drawn on its own,
// and that the pieces add up to the whole.
func TestComponents(t *testing.T) {
	rnd := rand.New(rand.NewPCG(5, 6))

	var data []float64
	for range 3000 {
		if rnd.Float64() < 0.3 {
			data = append(data, 5+0.5*rnd.NormFloat64())
			continue
		}
		data = append(data, rnd.Float64()*10)
	}

	model := pdf.Add(
		[]pdf.PDF{pdf.Gaussian(), pdf.Uniform()},
		[]string{"nsig", "nbkg"},
	)

	res, err := pdf.FitUnbinned(data, model, 0, 10, []minuit.Par{
		{Name: "nsig", Value: 500, Step: 50, Min: 0, Max: 10000},
		{Name: "nbkg", Value: 2000, Step: 50, Min: 0, Max: 10000},
		{Name: "mean", Value: 4.5, Step: 0.1, Min: 0, Max: 10},
		{Name: "sigma", Value: 0.6, Step: 0.05, Min: 1e-3, Max: 5},
	})
	if err != nil {
		t.Fatalf("could not fit: %+v", err)
	}

	total := res.Func(1)
	sig, err := res.Component(0, 1)
	if err != nil {
		t.Fatalf("could not take the signal component: %+v", err)
	}
	bkg, err := res.Component(1, 1)
	if err != nil {
		t.Fatalf("could not take the background component: %+v", err)
	}

	for _, x := range []float64{1, 3, 5, 7, 9} {
		got := sig(x) + bkg(x)
		want := total(x)
		if math.Abs(got-want) > 1e-9*math.Max(1, want) {
			t.Errorf("at x=%v the components sum to %v, the whole is %v", x, got, want)
		}
	}

	if _, err := res.Component(9, 1); err == nil {
		t.Error("asking for a component that is not there was allowed")
	}
}

// TestBinnedFit checks a fit to a histogram lands in the same place as one to
// the events that filled it.
func TestBinnedFit(t *testing.T) {
	const (
		mean  = 3.0
		sigma = 0.7
	)

	rnd := rand.New(rand.NewPCG(7, 8))
	h := newH1D(0, 10, 100)
	var data []float64
	for len(data) < 20000 {
		x := mean + sigma*rnd.NormFloat64()
		if x < 0 || x > 10 {
			continue
		}
		data = append(data, x)
		h.Fill(x, 1)
	}

	pars := []minuit.Par{
		{Name: "mean", Value: 2.0, Step: 0.1, Min: 0, Max: 10},
		{Name: "sigma", Value: 1.0, Step: 0.1, Min: 1e-3, Max: 5},
	}

	binned, err := pdf.FitBinned(h, pdf.Gaussian(), 0, 10, pars)
	if err != nil {
		t.Fatalf("could not fit the histogram: %+v", err)
	}
	unbinned, err := pdf.FitUnbinned(data, pdf.Gaussian(), 0, 10, pars)
	if err != nil {
		t.Fatalf("could not fit the data: %+v", err)
	}

	for i, name := range []string{"mean", "sigma"} {
		b, berr := binned.Value(i)
		u, _ := unbinned.Value(i)
		if math.Abs(b-u) > 3*berr {
			t.Errorf("%s: binned=%v +/- %v, unbinned=%v", name, b, berr, u)
		}
	}
}

func TestErrors(t *testing.T) {
	t.Run("no data", func(t *testing.T) {
		_, err := pdf.FitUnbinned(nil, pdf.Gaussian(), 0, 1, nil)
		if err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("wrong parameter count", func(t *testing.T) {
		_, err := pdf.FitUnbinned([]float64{1, 2, 3}, pdf.Gaussian(), 0, 10, []minuit.Par{
			{Name: "mean", Value: 1, Step: 0.1},
		})
		if err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("bad coefficient count", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected a panic")
			}
		}()
		pdf.Add([]pdf.PDF{pdf.Gaussian(), pdf.Uniform()}, []string{"a", "b", "c"})
	})
}
