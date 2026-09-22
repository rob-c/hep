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
	"go-hep.org/x/hep/hbook"
)

// TestShapesNormalise checks every density integrates to one once divided by
// its own integral, which is what makes it a density at all.
//
// It is the same check the first batch of shapes gets, because it is the one
// that catches a wrong closed-form integral, and a wrong integral is the way
// a density fails that a plot will not show you.
func TestShapesNormalise(t *testing.T) {
	cheb := pdf.Chebychev(3, 0, 10)

	for _, tc := range []struct {
		name   string
		p      pdf.PDF
		par    []float64
		lo, hi float64
	}{
		{"breit-wigner", pdf.BreitWigner(), []float64{5, 1}, -50, 60},
		{"breit-wigner narrow", pdf.BreitWigner(), []float64{0, 0.1}, -20, 20},
		{"bifurcated", pdf.BifurGauss(), []float64{2, 0.5, 1.5}, -10, 15},
		{"bifurcated equal", pdf.BifurGauss(), []float64{0, 1, 1}, -10, 10},
		{"argus", pdf.Argus(), []float64{5.29, -20, 0.5}, 5.0, 5.29},
		{"landau", pdf.Landau(), []float64{0, 1}, -5, 200},
		{"landau scaled", pdf.Landau(), []float64{10, 2}, 0, 400},
		{"chebychev", cheb, []float64{0.2, -0.1, 0.05}, 0, 10},
		{"voigtian", pdf.Voigtian(), []float64{0, 1, 0.5}, -30, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const n = 4000
			var (
				sum float64
				dx  = (tc.hi - tc.lo) / n
			)
			for i := range n {
				x := tc.lo + (float64(i)+0.5)*dx
				sum += pdf.Eval(tc.p, x, tc.lo, tc.hi, tc.par) * dx
			}

			if math.Abs(sum-1) > 2e-3 {
				t.Errorf("the normalised density integrates to %v, want 1", sum)
			}
		})
	}
}

// TestClosedFormIntegrals checks the densities that claim an exact integral
// against quadrature. The whole point of a closed form is that it is right.
func TestClosedFormIntegrals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		p      pdf.PDF
		par    []float64
		lo, hi float64
	}{
		{"breit-wigner", pdf.BreitWigner(), []float64{1.5, 0.8}, -9, 12},
		{"bifurcated", pdf.BifurGauss(), []float64{2, 0.5, 1.5}, -6, 9},
		{"landau", pdf.Landau(), []float64{1, 2}, -3, 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const n = 200000
			var (
				dx  = (tc.hi - tc.lo) / n
				sum float64
			)
			for i := range n {
				x := tc.lo + (float64(i)+0.5)*dx
				sum += tc.p.Shape(x, tc.par) * dx
			}

			got := tc.p.Integral(tc.lo, tc.hi, tc.par)
			if math.Abs(got-sum)/sum > 1e-4 {
				t.Errorf("closed form=%v, quadrature=%v", got, sum)
			}
		})
	}
}

// TestBreitWignerShape checks the width really is the full width at half the
// maximum, which is what the parameter is supposed to mean.
func TestBreitWignerShape(t *testing.T) {
	var (
		bw   = pdf.BreitWigner()
		par  = []float64{3, 2.0}
		peak = bw.Shape(3, par)
	)

	for _, d := range []float64{-1, +1} { // half the width either side
		if got, want := bw.Shape(3+d, par), 0.5*peak; math.Abs(got-want)/want > 1e-12 {
			t.Errorf("at %v from the peak: got=%v, want=%v (half maximum)", d, got, want)
		}
	}
}

// TestBifurGaussSides checks each side really uses its own width.
func TestBifurGaussSides(t *testing.T) {
	var (
		bg  = pdf.BifurGauss()
		par = []float64{0, 1, 3} // narrow below, wide above
	)

	// one sigma out on each side gives the same height.
	if got, want := bg.Shape(-1, par), bg.Shape(+3, par); math.Abs(got-want) > 1e-12 {
		t.Errorf("one sigma either side: got=%v and %v, want them equal", got, want)
	}
	// and the wide side is higher at equal distance.
	if bg.Shape(+1, par) <= bg.Shape(-1, par) {
		t.Error("the wide side should fall off more slowly")
	}
}

// TestArgusStopsAtTheEndpoint checks the shape is zero at and beyond m0,
// which is the whole reason for using it.
func TestArgusStopsAtTheEndpoint(t *testing.T) {
	var (
		a   = pdf.Argus()
		par = []float64{5.29, -20, 0.5}
	)

	for _, x := range []float64{5.29, 5.3, 6, 10} {
		if got := a.Shape(x, par); got != 0 {
			t.Errorf("Shape(%v): got=%v, want=0", x, got)
		}
	}
	if got := a.Shape(5.2, par); got <= 0 {
		t.Errorf("Shape(5.2): got=%v, want it positive", got)
	}
}

// TestPoissonSumsToOne checks the probabilities of all the counts add up.
func TestPoissonSumsToOne(t *testing.T) {
	p := pdf.Poisson()

	for _, mu := range []float64{0.5, 3, 20} {
		if got := p.Integral(0, 200, []float64{mu}); math.Abs(got-1) > 1e-9 {
			t.Errorf("mean=%v: the probabilities sum to %v, want 1", mu, got)
		}
	}

	// and the mean really is the mean.
	const mu = 4.0
	var mean float64
	for k := range 100 {
		mean += float64(k) * p.Shape(float64(k), []float64{mu})
	}
	if math.Abs(mean-mu) > 1e-9 {
		t.Errorf("mean: got=%v, want=%v", mean, mu)
	}
}

// TestVoigtianBetweenItsLimits checks the convolution collapses onto each of
// the two shapes it is made of when the other is taken away.
func TestVoigtianBetweenItsLimits(t *testing.T) {
	v := pdf.Voigtian()

	t.Run("no resolution", func(t *testing.T) {
		// with sigma tiny it should be the Breit-Wigner.
		var (
			bw   = pdf.BreitWigner()
			par  = []float64{0, 2, 1e-4}
			bpar = []float64{0, 2}
		)
		for _, x := range []float64{-2, -0.5, 0, 0.5, 2} {
			var (
				got  = pdf.Eval(v, x, -20, 20, par)
				want = pdf.Eval(bw, x, -20, 20, bpar)
			)
			if math.Abs(got-want)/want > 1e-2 {
				t.Errorf("at x=%v: voigtian=%v, breit-wigner=%v", x, got, want)
			}
		}
	})

	t.Run("no width", func(t *testing.T) {
		// with the width tiny it should be the gaussian.
		var (
			g    = pdf.Gaussian()
			par  = []float64{0, 1e-6, 1.5}
			gpar = []float64{0, 1.5}
		)
		for _, x := range []float64{-2, -0.5, 0, 0.5, 2} {
			var (
				got  = pdf.Eval(v, x, -20, 20, par)
				want = pdf.Eval(g, x, -20, 20, gpar)
			)
			if math.Abs(got-want)/want > 1e-2 {
				t.Errorf("at x=%v: voigtian=%v, gaussian=%v", x, got, want)
			}
		}
	})
}

// TestFormula checks a density written as an expression behaves like the one
// it spells out.
func TestFormula(t *testing.T) {
	f, err := pdf.Formula("exp(slope*x)", []string{"slope"})
	if err != nil {
		t.Fatalf("could not build the formula: %+v", err)
	}

	if got, want := f.NPar(), 1; got != want {
		t.Fatalf("npar: got=%d, want=%d", got, want)
	}

	// it is an exponential, so it should agree with one.
	var (
		e   = pdf.Exponential()
		par = []float64{-0.3}
	)
	for _, x := range []float64{0, 1, 5, 9} {
		var (
			got  = pdf.Eval(f, x, 0, 10, par)
			want = pdf.Eval(e, x, 0, 10, par)
		)
		if math.Abs(got-want)/want > 1e-6 {
			t.Errorf("at x=%v: formula=%v, exponential=%v", x, got, want)
		}
	}
}

func TestFormulaErrors(t *testing.T) {
	for _, tc := range []struct {
		expr string
		pars []string
	}{
		{"x +", nil},
		{"nosuchfunc(x)", nil},
		{"x * gain", nil},            // reads a name that is not a parameter
		{"x * gain", []string{"mu"}}, // nor one of the ones given
	} {
		if _, err := pdf.Formula(tc.expr, tc.pars); err == nil {
			t.Errorf("%q with %v was accepted", tc.expr, tc.pars)
		}
	}
}

// TestHistPDF checks a template taken from a histogram keeps its shape, and
// does not change when the same data is binned more finely.
func TestHistPDF(t *testing.T) {
	fill := func(n int) *hbook.H1D {
		h := hbook.NewH1D(n, 0, 10)
		rnd := rand.New(rand.NewPCG(1, 2))
		for range 100000 {
			h.Fill(rnd.Float64()*10, 1)
		}
		return h
	}

	coarse := pdf.Hist(fill(10))
	fine := pdf.Hist(fill(100))

	// the data is flat, so both templates should be flat and equal.
	for _, x := range []float64{0.5, 3.3, 7.7, 9.5} {
		var (
			a = pdf.Eval(coarse, x, 0, 10, nil)
			b = pdf.Eval(fine, x, 0, 10, nil)
		)
		if math.Abs(a-0.1) > 0.02 {
			t.Errorf("coarse at %v: got=%v, want~0.1", x, a)
		}
		if math.Abs(a-b) > 0.03 {
			t.Errorf("at %v the two binnings disagree: %v and %v", x, a, b)
		}
	}

	// and nothing outside the histogram.
	if got := coarse.Shape(-1, nil); got != 0 {
		t.Errorf("below the range: got=%v, want=0", got)
	}
	if got := coarse.Shape(11, nil); got != 0 {
		t.Errorf("above the range: got=%v, want=0", got)
	}
}

// TestFitLandau checks a Landau can actually be fitted, which is the thing
// the earlier refusal to approximate it was blocking.
func TestFitLandau(t *testing.T) {
	const (
		loc   = 10.0
		scale = 2.0
		lo    = -5.0
		hi    = 120.0
	)

	// sample a Landau by inverting its cumulative on a grid.
	rnd := rand.New(rand.NewPCG(11, 12))
	data := make([]float64, 0, 20000)
	for len(data) < 20000 {
		x := lo + rnd.Float64()*(hi-lo)
		p := pdf.Eval(pdf.Landau(), x, lo, hi, []float64{loc, scale})
		if rnd.Float64()*0.1 < p {
			data = append(data, x)
		}
	}

	res, err := pdf.FitUnbinned(data, pdf.Landau(), lo, hi, []minuit.Par{
		{Name: "location", Value: 8, Step: 0.5, Min: 0, Max: 30},
		{Name: "scale", Value: 1, Step: 0.2, Min: 0.1, Max: 10},
	})
	if err != nil {
		t.Fatalf("could not fit: %+v", err)
	}

	gotLoc, locErr := res.Value(0)
	gotScale, scaleErr := res.Value(1)

	if math.Abs(gotLoc-loc) > 5*locErr {
		t.Errorf("location: got=%v +/- %v, want=%v", gotLoc, locErr, loc)
	}
	if math.Abs(gotScale-scale) > 5*scaleErr {
		t.Errorf("scale: got=%v +/- %v, want=%v", gotScale, scaleErr, scale)
	}
}

func TestChebychevPanics(t *testing.T) {
	for _, tc := range []struct {
		n      int
		lo, hi float64
	}{
		{0, 0, 1},
		{2, 1, 1},
		{2, 5, 1},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Chebychev(%d, %v, %v) was allowed", tc.n, tc.lo, tc.hi)
				}
			}()
			pdf.Chebychev(tc.n, tc.lo, tc.hi)
		}()
	}
}
