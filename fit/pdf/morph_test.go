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

// tmpl builds a histogram from the given bin contents.
func tmpl(vals ...float64) *hbook.H1D {
	h := hbook.NewH1D(len(vals), 0, float64(len(vals)))
	for i, v := range vals {
		if v == 0 {
			continue
		}
		h.Fill(float64(i)+0.5, v)
	}
	return h
}

// TestMorphAtTheVariations checks the interpolation passes through the
// templates it was given: nominal at zero, up at +1, down at -1. Every code
// has to do that, whatever it does between them.
func TestMorphAtTheVariations(t *testing.T) {
	var (
		nom  = tmpl(10, 20, 30)
		up   = tmpl(12, 26, 27)
		down = tmpl(9, 15, 34)
	)

	for _, code := range []pdf.Interp{pdf.InterpLinear, pdf.InterpExp, pdf.InterpPolyExp} {
		t.Run(code.String(), func(t *testing.T) {
			m, err := pdf.NewMorph(nom, []*hbook.H1D{up}, []*hbook.H1D{down}, code)
			if err != nil {
				t.Fatalf("could not build the morph: %+v", err)
			}

			for _, tc := range []struct {
				alpha float64
				want  []float64
			}{
				{0, []float64{10, 20, 30}},
				{+1, []float64{12, 26, 27}},
				{-1, []float64{9, 15, 34}},
			} {
				for i, want := range tc.want {
					got := m.Bin(i, []float64{tc.alpha})
					if math.Abs(got-want) > 1e-9 {
						t.Errorf("alpha=%v bin %d: got=%v, want=%v", tc.alpha, i, got, want)
					}
				}
			}
		})
	}
}

// TestMorphPolyExpIsSmooth checks the polynomial-exponential interpolation
// joins up smoothly, which is the whole reason to prefer it.
//
// The linear and exponential codes both have a kink at alpha=0 -- their two
// halves meet with different slopes -- and this one should not.
func TestMorphPolyExpIsSmooth(t *testing.T) {
	var (
		nom  = tmpl(100)
		up   = tmpl(130)
		down = tmpl(80)
	)

	slope := func(m *pdf.Morph, a, h float64) float64 {
		return (m.Bin(0, []float64{a + h}) - m.Bin(0, []float64{a - h})) / (2 * h)
	}

	const h = 1e-5

	t.Run("exponential has a kink at zero", func(t *testing.T) {
		m, err := pdf.NewMorph(nom, []*hbook.H1D{up}, []*hbook.H1D{down}, pdf.InterpExp)
		if err != nil {
			t.Fatal(err)
		}
		var (
			left  = slope(m, -2*h, h)
			right = slope(m, +2*h, h)
		)
		if math.Abs(left-right) < 1 {
			t.Errorf("expected a kink: slopes %v and %v", left, right)
		}
	})

	t.Run("polynomial-exponential does not", func(t *testing.T) {
		m, err := pdf.NewMorph(nom, []*hbook.H1D{up}, []*hbook.H1D{down}, pdf.InterpPolyExp)
		if err != nil {
			t.Fatal(err)
		}

		// smooth at zero, and also where the polynomial meets the
		// exponentials at plus and minus one.
		for _, a := range []float64{0, +1, -1} {
			var (
				left  = slope(m, a-2*h, h)
				right = slope(m, a+2*h, h)
			)
			if math.Abs(left-right) > 1e-2*math.Max(1, math.Abs(left)) {
				t.Errorf("at alpha=%v the slope jumps from %v to %v", a, left, right)
			}
		}
	})
}

// TestMorphStaysPositive checks the multiplicative codes never send a bin
// negative, which a yield cannot be, and that the linear one is clamped.
func TestMorphStaysPositive(t *testing.T) {
	var (
		nom  = tmpl(10)
		up   = tmpl(18)
		down = tmpl(3)
	)

	for _, code := range []pdf.Interp{pdf.InterpLinear, pdf.InterpExp, pdf.InterpPolyExp} {
		m, err := pdf.NewMorph(nom, []*hbook.H1D{up}, []*hbook.H1D{down}, code)
		if err != nil {
			t.Fatal(err)
		}
		for a := -6.0; a <= 6.0; a += 0.25 {
			if got := m.Bin(0, []float64{a}); got < 0 {
				t.Fatalf("%v at alpha=%v: got %v", code, a, got)
			}
		}
	}
}

// TestMorphSeveralSystematics checks systematics compose, each one scaling
// what the ones before it left.
func TestMorphSeveralSystematics(t *testing.T) {
	var (
		nom = tmpl(100)
		up1 = tmpl(110) // +10%
		dn1 = tmpl(90)
		up2 = tmpl(120) // +20%
		dn2 = tmpl(80)
	)

	m, err := pdf.NewMorph(nom,
		[]*hbook.H1D{up1, up2},
		[]*hbook.H1D{dn1, dn2},
		pdf.InterpExp,
	)
	if err != nil {
		t.Fatalf("could not build the morph: %+v", err)
	}

	if got, want := m.NPar(), 2; got != want {
		t.Fatalf("npar: got=%d, want=%d", got, want)
	}

	// both up by one: 100 * 1.1 * 1.2.
	if got, want := m.Bin(0, []float64{1, 1}), 132.0; math.Abs(got-want) > 1e-9 {
		t.Errorf("both up: got=%v, want=%v", got, want)
	}
	// one up, one down: 100 * 1.1 * 0.8.
	if got, want := m.Bin(0, []float64{1, -1}), 88.0; math.Abs(got-want) > 1e-9 {
		t.Errorf("one each way: got=%v, want=%v", got, want)
	}
}

func TestMorphErrors(t *testing.T) {
	nom := tmpl(1, 2, 3)

	for _, tc := range []struct {
		name       string
		nom        *hbook.H1D
		ups, downs []*hbook.H1D
	}{
		{"no nominal", nil, nil, nil},
		{"mismatched counts", nom, []*hbook.H1D{tmpl(1, 2, 3)}, nil},
		{"missing variation", nom, []*hbook.H1D{nil}, []*hbook.H1D{tmpl(1, 2, 3)}},
		{"wrong binning", nom, []*hbook.H1D{tmpl(1, 2)}, []*hbook.H1D{tmpl(1, 2)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pdf.NewMorph(tc.nom, tc.ups, tc.downs, pdf.InterpExp); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

// TestBinnedModel fits a signal strength on top of a background with a
// systematic, which is the shape nearly every collider measurement has.
func TestBinnedModel(t *testing.T) {
	// eight bins: signal peaks in the middle, background falls.
	var (
		sigNom = tmpl(1, 4, 12, 30, 30, 12, 4, 1)
		bkgNom = tmpl(200, 170, 145, 123, 105, 89, 76, 64)

		// the background systematic tilts it: more at one end, less at
		// the other.
		bkgUp = tmpl(180, 158, 138, 120, 107, 94, 83, 73)
		bkgDn = tmpl(220, 182, 152, 126, 103, 84, 69, 55)
	)

	sig, err := pdf.NewMorph(sigNom, nil, nil, pdf.InterpPolyExp)
	if err != nil {
		t.Fatalf("could not build the signal template: %+v", err)
	}
	bkg, err := pdf.NewMorph(bkgNom,
		[]*hbook.H1D{bkgUp}, []*hbook.H1D{bkgDn}, pdf.InterpPolyExp,
	)
	if err != nil {
		t.Fatalf("could not build the background template: %+v", err)
	}

	// parameter 0 is the signal strength, 1 the background systematic.
	model, err := pdf.NewBinned(2, []pdf.Sample{
		{Name: "signal", Template: sig, Norms: []int{0}},
		{Name: "background", Template: bkg, Alphas: []int{1}},
	}, nil)
	if err != nil {
		t.Fatalf("could not build the model: %+v", err)
	}

	// the systematic should have been given a unit gaussian constraint.
	cons := model.Constraints()
	if len(cons) != 1 || cons[0].Par != 1 || cons[0].Sigma != 1 {
		t.Fatalf("constraints: got=%v, want one unit gaussian on parameter 1", cons)
	}

	// data: the model at a signal strength of 1.5 and the systematic at
	// half a sigma, thrown as Poisson counts.
	truth := []float64{1.5, 0.5}
	rnd := rand.New(rand.NewPCG(1, 2))

	data := hbook.NewH1D(8, 0, 8)
	for i := range 8 {
		mu := model.Expected(i, truth)
		data.Fill(float64(i)+0.5, float64(poisson(rnd, mu)))
	}

	res, err := pdf.FitBinnedModel(model, data, []minuit.Par{
		{Name: "mu", Value: 1.0, Step: 0.2, Min: 0, Max: 10},
		{Name: "alpha", Value: 0.0, Step: 0.3, Min: -5, Max: 5},
	})
	if err != nil {
		t.Fatalf("could not fit: %+v", err)
	}

	mu, muErr := res.Value(0)
	if math.Abs(mu-1.5) > 4*muErr {
		t.Errorf("signal strength: got=%v +/- %v, want=1.5", mu, muErr)
	}
	if muErr <= 0 {
		t.Errorf("signal strength has no uncertainty: %v", muErr)
	}

	// the nuisance should stay within a couple of sigma of where it is held.
	alpha, _ := res.Value(1)
	if math.Abs(alpha) > 2.5 {
		t.Errorf("the nuisance ran to %v, which the constraint should not allow", alpha)
	}
}

// TestBinnedSystematicWidensTheError checks a systematic costs precision:
// the same data fitted with the nuisance free should know the signal less
// well than with it held.
func TestBinnedSystematicWidensTheError(t *testing.T) {
	var (
		sigNom = tmpl(2, 8, 20, 20, 8, 2)
		bkgNom = tmpl(100, 90, 80, 72, 65, 58)
		bkgUp  = tmpl(120, 104, 88, 76, 65, 55)
		bkgDn  = tmpl(80, 76, 72, 68, 65, 61)
	)

	sig, err := pdf.NewMorph(sigNom, nil, nil, pdf.InterpPolyExp)
	if err != nil {
		t.Fatal(err)
	}
	bkg, err := pdf.NewMorph(bkgNom, []*hbook.H1D{bkgUp}, []*hbook.H1D{bkgDn}, pdf.InterpPolyExp)
	if err != nil {
		t.Fatal(err)
	}

	model, err := pdf.NewBinned(2, []pdf.Sample{
		{Name: "signal", Template: sig, Norms: []int{0}},
		{Name: "background", Template: bkg, Alphas: []int{1}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	rnd := rand.New(rand.NewPCG(3, 4))
	data := hbook.NewH1D(6, 0, 6)
	for i := range 6 {
		data.Fill(float64(i)+0.5, float64(poisson(rnd, model.Expected(i, []float64{1, 0}))))
	}

	free, err := pdf.FitBinnedModel(model, data, []minuit.Par{
		{Name: "mu", Value: 1, Step: 0.2, Min: 0, Max: 10},
		{Name: "alpha", Value: 0, Step: 0.3, Min: -5, Max: 5},
	})
	if err != nil {
		t.Fatalf("could not fit with the nuisance free: %+v", err)
	}

	held, err := pdf.FitBinnedModel(model, data, []minuit.Par{
		{Name: "mu", Value: 1, Step: 0.2, Min: 0, Max: 10},
		{Name: "alpha", Value: 0, Step: 0, Min: -5, Max: 5}, // a step of zero fixes it
	})
	if err != nil {
		t.Fatalf("could not fit with the nuisance held: %+v", err)
	}

	_, freeErr := free.Value(0)
	_, heldErr := held.Value(0)

	if !(freeErr > heldErr) {
		t.Errorf(
			"a free systematic should cost precision: free=%v, held=%v",
			freeErr, heldErr,
		)
	}
}

func TestBinnedErrors(t *testing.T) {
	good, err := pdf.NewMorph(tmpl(1, 2), nil, nil, pdf.InterpExp)
	if err != nil {
		t.Fatal(err)
	}
	withSyst, err := pdf.NewMorph(tmpl(1, 2), []*hbook.H1D{tmpl(2, 3)}, []*hbook.H1D{tmpl(1, 1)}, pdf.InterpExp)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		npar    int
		samples []pdf.Sample
		cons    []pdf.Constraint
	}{
		{"no samples", 1, nil, nil},
		{"no template", 1, []pdf.Sample{{Name: "a"}}, nil},
		{"wrong alpha count", 1, []pdf.Sample{{Name: "a", Template: withSyst}}, nil},
		{"parameter out of range", 1, []pdf.Sample{{Name: "a", Template: good, Norms: []int{9}}}, nil},
		{"bad kappa", 1, []pdf.Sample{{Name: "a", Template: good, LogNorms: []pdf.LogNorm{{Par: 0, Kappa: 0}}}}, nil},
		{"bad constraint width", 1, []pdf.Sample{{Name: "a", Template: good}}, []pdf.Constraint{{Par: 0, Sigma: 0}}},
		{"mismatched bins", 1, []pdf.Sample{
			{Name: "a", Template: good},
			{Name: "b", Template: mustMorph(t, tmpl(1, 2, 3))},
		}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pdf.NewBinned(tc.npar, tc.samples, tc.cons); err == nil {
				t.Fatal("expected an error")
			}
		})
	}

	t.Run("data binned differently", func(t *testing.T) {
		m, err := pdf.NewBinned(1, []pdf.Sample{{Name: "a", Template: good, Norms: []int{0}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.NLL(hbook.NewH1D(5, 0, 5)); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func mustMorph(t *testing.T, h *hbook.H1D) *pdf.Morph {
	t.Helper()
	m, err := pdf.NewMorph(h, nil, nil, pdf.InterpExp)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// poisson draws a Poisson count, by Knuth's method for a small mean and a
// gaussian for a large one where the product would underflow.
func poisson(rnd *rand.Rand, mu float64) int {
	if mu <= 0 {
		return 0
	}
	if mu > 500 {
		v := mu + math.Sqrt(mu)*rnd.NormFloat64()
		if v < 0 {
			return 0
		}
		return int(math.Round(v))
	}

	var (
		l = math.Exp(-mu)
		k = 0
		p = 1.0
	)
	for {
		p *= rnd.Float64()
		if p <= l {
			return k
		}
		k++
	}
}
