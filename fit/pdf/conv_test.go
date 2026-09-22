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

// TestConvAgreesWithQuadrature checks the transform gives the same answer as
// integrating, which is the only thing that makes it worth being faster.
func TestConvAgreesWithQuadrature(t *testing.T) {
	var (
		lo, hi = -20.0, 20.0

		fft = mustConv(t, lo, hi, 8192)
		num = pdf.Voigtian()

		// mean, width, sigma -- the same three, in the same order, since
		// the convolution is a Breit-Wigner against a zero-mean gaussian.
		par = []float64{0, 2, 1}
	)

	for _, x := range []float64{-6, -3, -1, 0, 1, 3, 6} {
		var (
			got  = pdf.Eval(fft, x, lo, hi, par)
			want = pdf.Eval(num, x, lo, hi, par)
		)
		if math.Abs(got-want)/want > 2e-2 {
			t.Errorf("at x=%v: fft=%v, quadrature=%v", x, got, want)
		}
	}
}

// TestConvNormalises checks the convolution is a density once normalised.
func TestConvNormalises(t *testing.T) {
	var (
		lo, hi = -20.0, 20.0
		c      = mustConv(t, lo, hi, 4096)
		par    = []float64{0, 1.5, 0.8}
	)

	const n = 4000
	var (
		dx  = (hi - lo) / n
		sum float64
	)
	for i := range n {
		sum += pdf.Eval(c, lo+(float64(i)+0.5)*dx, lo, hi, par) * dx
	}

	if math.Abs(sum-1) > 5e-3 {
		t.Errorf("the normalised convolution integrates to %v, want 1", sum)
	}
}

// TestConvLimits checks the convolution collapses onto each of the shapes it
// is made of when the other is taken away.
func TestConvLimits(t *testing.T) {
	var (
		lo, hi = -20.0, 20.0
		c      = mustConv(t, lo, hi, 8192)
	)

	t.Run("no resolution", func(t *testing.T) {
		// a resolution far narrower than the grid step is a delta: the
		// answer should be the Breit-Wigner.
		var (
			bw   = pdf.BreitWigner()
			par  = []float64{0, 2, 1e-3}
			bpar = []float64{0, 2}
		)
		for _, x := range []float64{-4, -1, 0, 1, 4} {
			var (
				got  = pdf.Eval(c, x, lo, hi, par)
				want = pdf.Eval(bw, x, lo, hi, bpar)
			)
			if math.Abs(got-want)/want > 5e-2 {
				t.Errorf("at x=%v: convolution=%v, breit-wigner=%v", x, got, want)
			}
		}
	})

	t.Run("symmetric", func(t *testing.T) {
		// both shapes are symmetric about the mean, so the convolution is
		// too.
		par := []float64{0, 1.5, 1}
		for _, d := range []float64{0.5, 2, 5} {
			var (
				l = pdf.Eval(c, -d, lo, hi, par)
				r = pdf.Eval(c, +d, lo, hi, par)
			)
			if math.Abs(l-r)/math.Max(l, r) > 1e-6 {
				t.Errorf("at +/-%v: %v and %v, want them equal", d, l, r)
			}
		}
	})
}

// TestConvFits checks a convolution can be fitted with, which is what the
// transform is for: the quadrature one is too slow to be.
func TestConvFits(t *testing.T) {
	const (
		lo, hi = -15.0, 15.0
		mean   = 1.0
		width  = 2.0
		sigma  = 1.0
	)

	c := mustConv(t, lo, hi, 4096)

	rnd := rand.New(rand.NewPCG(1, 2))
	data, err := pdf.Generate(rnd, c, lo, hi, []float64{mean, width, sigma}, 10000)
	if err != nil {
		t.Fatalf("could not generate: %+v", err)
	}

	res, err := pdf.FitUnbinned(data, c, lo, hi, []minuit.Par{
		{Name: "mean", Value: 0, Step: 0.2, Min: lo, Max: hi},
		{Name: "width", Value: 1.5, Step: 0.2, Min: 0.05, Max: 10},
		{Name: "sigma", Value: 1.5, Step: 0.2, Min: 0.05, Max: 10},
	}, pdf.MaxCalls(20000))
	if err != nil {
		t.Fatalf("could not fit: %+v", err)
	}

	// The width and the resolution are famously correlated -- both make the
	// peak wider -- so this checks the mean tightly and the other two
	// loosely.
	gotMean, meanErr := res.Value(0)
	if math.Abs(gotMean-mean) > 5*meanErr {
		t.Errorf("mean: got=%v +/- %v, want=%v", gotMean, meanErr, mean)
	}

	gotWidth, _ := res.Value(1)
	gotSigma, _ := res.Value(2)
	if gotWidth <= 0 || gotSigma <= 0 {
		t.Errorf("width=%v sigma=%v, want both positive", gotWidth, gotSigma)
	}

	// what the two of them together make of the peak is much better known
	// than either alone: the total width should come out right.
	total := math.Hypot(gotSigma, 0.5*gotWidth)
	want := math.Hypot(sigma, 0.5*width)
	if math.Abs(total-want)/want > 0.15 {
		t.Errorf("the peak's total width is %v, want about %v", total, want)
	}
}

// TestConvIsFaster is the claim the package makes for the transform, checked
// rather than asserted.
func TestConvIsFaster(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}

	var (
		lo, hi = -20.0, 20.0
		par    = []float64{0, 2, 1}
		fft    = mustConv(t, lo, hi, 4096)
		num    = pdf.Voigtian()
		xs     = make([]float64, 2000)
	)
	for i := range xs {
		xs[i] = lo + (hi-lo)*float64(i)/float64(len(xs))
	}

	// prime the grid, which is the cost the transform pays once.
	_ = fft.Integral(lo, hi, par)

	tf := timeIt(func() {
		for _, x := range xs {
			_ = fft.Shape(x, par)
		}
	})
	tn := timeIt(func() {
		for _, x := range xs {
			_ = num.Shape(x, par)
		}
	})

	t.Logf("%d points: fft=%v quadrature=%v", len(xs), tf, tn)
	if tf >= tn {
		t.Errorf("the transform (%v) was not faster than the quadrature (%v)", tf, tn)
	}
}

func TestConvErrors(t *testing.T) {
	if _, err := pdf.Convolve(nil, pdf.Resolution(), -1, 1, 1024); err == nil {
		t.Error("a nil density was accepted")
	}
	if _, err := pdf.Convolve(pdf.BreitWigner(), nil, -1, 1, 1024); err == nil {
		t.Error("a nil resolution was accepted")
	}
	if _, err := pdf.Convolve(pdf.BreitWigner(), pdf.Resolution(), -1, 1, 4); err == nil {
		t.Error("a grid of four points was accepted")
	}
	if _, err := pdf.Convolve(pdf.BreitWigner(), pdf.Resolution(), 1, -1, 1024); err == nil {
		t.Error("an inverted range was accepted")
	}
}

func mustConv(t *testing.T, lo, hi float64, n int) *pdf.Conv {
	t.Helper()
	c, err := pdf.VoigtianFFT(lo, hi, n)
	if err != nil {
		t.Fatalf("could not build the convolution: %+v", err)
	}
	return c
}
