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

// TestFactoriseIsTheProduct checks a factorised density is the product of its
// factors and that its integral is the product of theirs -- which is the
// whole reason to build one this way.
func TestFactoriseIsTheProduct(t *testing.T) {
	var (
		g   = pdf.Gaussian()
		e   = pdf.Exponential()
		p   = pdf.Factorise(g, e)
		par = []float64{5, 1, -0.3}
	)

	if got, want := p.NDim(), 2; got != want {
		t.Fatalf("ndim: got=%d, want=%d", got, want)
	}
	if got, want := p.NPar(), 3; got != want {
		t.Fatalf("npar: got=%d, want=%d", got, want)
	}

	for _, x := range [][]float64{{5, 1}, {3, 4}, {7, 0.5}} {
		var (
			got  = p.Shape(x, par)
			want = g.Shape(x[0], par[:2]) * e.Shape(x[1], par[2:])
		)
		if math.Abs(got-want) > 1e-12 {
			t.Errorf("at %v: got=%v, want=%v", x, got, want)
		}
	}

	var (
		lo   = []float64{0, 0}
		hi   = []float64{10, 10}
		got  = p.Integral(lo, hi, par)
		want = g.Integral(0, 10, par[:2]) * e.Integral(0, 10, par[2:])
	)
	if math.Abs(got-want)/want > 1e-12 {
		t.Errorf("integral: got=%v, want=%v", got, want)
	}
}

// TestFactoriseNormalises checks the density integrates to one over the box.
func TestFactoriseNormalises(t *testing.T) {
	var (
		p   = pdf.Factorise(pdf.Gaussian(), pdf.Exponential())
		par = []float64{5, 1, -0.3}
		lo  = []float64{0, 0}
		hi  = []float64{10, 10}
		n   = 300
		dx  = (hi[0] - lo[0]) / float64(n)
		dy  = (hi[1] - lo[1]) / float64(n)
		sum float64
	)

	for i := range n {
		for j := range n {
			x := []float64{
				lo[0] + (float64(i)+0.5)*dx,
				lo[1] + (float64(j)+0.5)*dy,
			}
			sum += pdf.EvalND(p, x, lo, hi, par) * dx * dy
		}
	}

	if math.Abs(sum-1) > 1e-3 {
		t.Errorf("the normalised density integrates to %v, want 1", sum)
	}
}

// TestFitND fits a two-dimensional model and gets back what generated it.
func TestFitND(t *testing.T) {
	rnd := rand.New(rand.NewPCG(1, 2))

	var (
		lo   = []float64{0, 0}
		hi   = []float64{10, 10}
		p    = pdf.Factorise(pdf.Gaussian(), pdf.Exponential())
		want = []float64{6.0, 0.8, -0.4}
	)

	data, err := pdf.GenerateND(rnd, p, lo, hi, want, 20000)
	if err != nil {
		t.Fatalf("could not generate: %+v", err)
	}
	if got := len(data); got != 20000 {
		t.Fatalf("generated %d events, want 20000", got)
	}
	for _, x := range data {
		if len(x) != 2 {
			t.Fatalf("an event has %d observables, want 2", len(x))
		}
	}

	res, err := pdf.FitUnbinnedND(data, p, lo, hi, []minuit.Par{
		{Name: "mean", Value: 5, Step: 0.2, Min: 0, Max: 10},
		{Name: "sigma", Value: 1, Step: 0.1, Min: 1e-3, Max: 5},
		{Name: "slope", Value: -0.2, Step: 0.05, Min: -5, Max: 5},
	})
	if err != nil {
		t.Fatalf("could not fit: %+v", err)
	}

	for i, name := range []string{"mean", "sigma", "slope"} {
		got, err := res.Value(i)
		if math.Abs(got-want[i]) > 5*err {
			t.Errorf("%s: got=%v +/- %v, want=%v", name, got, err, want[i])
		}
	}
}

// TestProjection checks the projection of a factorised fit onto an observable
// is the factor for that observable.
func TestProjection(t *testing.T) {
	rnd := rand.New(rand.NewPCG(3, 4))

	var (
		lo  = []float64{0, 0}
		hi  = []float64{10, 10}
		p   = pdf.Factorise(pdf.Gaussian(), pdf.Exponential())
		par = []float64{5, 1, -0.3}
	)

	data, err := pdf.GenerateND(rnd, p, lo, hi, par, 2000)
	if err != nil {
		t.Fatalf("could not generate: %+v", err)
	}

	res, err := pdf.FitUnbinnedND(data, p, lo, hi, []minuit.Par{
		{Name: "mean", Value: 5, Step: 0.2, Min: 0, Max: 10},
		{Name: "sigma", Value: 1, Step: 0.1, Min: 1e-3, Max: 5},
		{Name: "slope", Value: -0.3, Step: 0.05, Min: -5, Max: 5},
	})
	if err != nil {
		t.Fatalf("could not fit: %+v", err)
	}

	proj, err := res.Projection(0, 1)
	if err != nil {
		t.Fatalf("could not project: %+v", err)
	}

	// it should be the fitted gaussian, normalised over its own range.
	fitted := res.Values()
	for _, x := range []float64{3, 5, 7} {
		var (
			got  = proj(x)
			want = pdf.Eval(pdf.Gaussian(), x, 0, 10, fitted[:2])
		)
		if math.Abs(got-want)/want > 1e-9 {
			t.Errorf("at x=%v: projection=%v, gaussian=%v", x, got, want)
		}
	}

	if _, err := res.Projection(9, 1); err == nil {
		t.Error("projecting onto an observable that is not there was allowed")
	}
}

// TestAddND checks a sum in two dimensions counts its components.
func TestAddND(t *testing.T) {
	rnd := rand.New(rand.NewPCG(5, 6))

	var (
		lo = []float64{0, 0}
		hi = []float64{10, 10}

		// signal peaks in both observables; background is flat in one and
		// falls in the other.
		sig = pdf.Factorise(pdf.Gaussian(), pdf.Gaussian())
		bkg = pdf.Factorise(pdf.Uniform(), pdf.Exponential())
	)

	model := pdf.AddND([]pdf.PDFND{sig, bkg}, []string{"nsig", "nbkg"})

	if got, want := model.NDim(), 2; got != want {
		t.Fatalf("ndim: got=%d, want=%d", got, want)
	}
	// 2 yields + 4 signal + 1 background
	if got, want := model.NPar(), 7; got != want {
		t.Fatalf("npar: got=%d, want=%d", got, want)
	}

	truth := []float64{2000, 8000, 5, 0.5, 5, 0.5, -0.3}
	data, err := pdf.GenerateND(rnd, model, lo, hi, truth, 10000)
	if err != nil {
		t.Fatalf("could not generate: %+v", err)
	}

	res, err := pdf.FitUnbinnedND(data, model, lo, hi, []minuit.Par{
		{Name: "nsig", Value: 1500, Step: 100, Min: 0, Max: 50000},
		{Name: "nbkg", Value: 7000, Step: 100, Min: 0, Max: 50000},
		{Name: "mx", Value: 4.5, Step: 0.1, Min: 0, Max: 10},
		{Name: "sx", Value: 0.6, Step: 0.05, Min: 1e-2, Max: 3},
		{Name: "my", Value: 4.5, Step: 0.1, Min: 0, Max: 10},
		{Name: "sy", Value: 0.6, Step: 0.05, Min: 1e-2, Max: 3},
		{Name: "slope", Value: -0.2, Step: 0.05, Min: -5, Max: 5},
	}, pdf.MaxCalls(50000))
	if err != nil {
		t.Fatalf("could not fit: %+v", err)
	}

	nsig, nsigErr := res.Value(0)
	nbkg, _ := res.Value(1)

	// the yields must add up to the events generated, and be in the right
	// proportion.
	if got, want := nsig+nbkg, float64(len(data)); math.Abs(got-want) > 0.03*want {
		t.Errorf("total yield: got=%v, want~%v", got, want)
	}
	if want := 2000.0 * float64(len(data)) / 10000; math.Abs(nsig-want) > 6*nsigErr {
		t.Errorf("nsig: got=%v +/- %v, want~%v", nsig, nsigErr, want)
	}
}

func TestNDErrors(t *testing.T) {
	p := pdf.Factorise(pdf.Gaussian(), pdf.Exponential())
	pars := []minuit.Par{
		{Name: "mean", Value: 5, Step: 0.2},
		{Name: "sigma", Value: 1, Step: 0.1},
		{Name: "slope", Value: -0.2, Step: 0.05},
	}

	for _, tc := range []struct {
		name   string
		data   [][]float64
		lo, hi []float64
		pars   []minuit.Par
	}{
		{"no data", nil, []float64{0, 0}, []float64{1, 1}, pars},
		{"wrong box", [][]float64{{1, 1}}, []float64{0}, []float64{1}, pars},
		{"inverted range", [][]float64{{1, 1}}, []float64{0, 5}, []float64{1, 1}, pars},
		{"wrong parameters", [][]float64{{1, 1}}, []float64{0, 0}, []float64{1, 1}, pars[:1]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pdf.FitUnbinnedND(tc.data, p, tc.lo, tc.hi, tc.pars); err == nil {
				t.Fatal("expected an error")
			}
		})
	}

	t.Run("mismatched dimensions", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected a panic")
			}
		}()
		pdf.AddND([]pdf.PDFND{
			pdf.Factorise(pdf.Gaussian()),
			pdf.Factorise(pdf.Gaussian(), pdf.Gaussian()),
		}, []string{"a", "b"})
	})
}
