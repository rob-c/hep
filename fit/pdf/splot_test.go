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

// TestGenerateMixture checks a sum generates the proportions its coefficients
// ask for.
//
// A coefficient multiplies a component that has been normalised first, so
// sampling the raw shape draws the wrong mixture: a component with a large
// integral comes out over-represented by exactly that integral. Nothing about
// the fitted parameters looks wrong when that happens, which is why this is
// tested on its own.
func TestGenerateMixture(t *testing.T) {
	const (
		lo = 0.0
		hi = 10.0
	)

	// a narrow peak and a flat background. Their unnormalised shapes have
	// very different integrals -- the gaussian's is about 1.25 and the
	// uniform's is 10 -- so getting this wrong is not subtle.
	model := pdf.Add(
		[]pdf.PDF{pdf.Gaussian(), pdf.Uniform()},
		[]string{"nsig", "nbkg"},
	)

	rnd := rand.New(rand.NewPCG(1, 2))
	data, err := pdf.Generate(rnd, model, lo, hi, []float64{3000, 7000, 5, 0.5}, 10000)
	if err != nil {
		t.Fatalf("could not generate: %+v", err)
	}

	// count what landed within three sigma of the peak, where the signal
	// is and the background is only 3/10 of itself.
	var near int
	for _, x := range data {
		if math.Abs(x-5) < 1.5 {
			near++
		}
	}

	// 3000 signal (essentially all of it) plus 7000*3/10 = 2100 background.
	if want := 3000 + 2100; math.Abs(float64(near)-float64(want)) > 0.1*float64(want) {
		t.Errorf("%d events near the peak, want about %d", near, want)
	}
}

// TestSPlot checks sWeights unfold a mixture: a control variable histogrammed
// with the signal weights should give the signal's distribution of it, with
// the background subtracted away.
func TestSPlot(t *testing.T) {
	const (
		lo = 0.0
		hi = 10.0

		nsig = 3000
		nbkg = 7000
	)

	rnd := rand.New(rand.NewPCG(3, 4))

	// The discriminating variable: signal peaks, background is flat. The
	// control variable is independent of it, and is what the weights are
	// meant to unfold: signal sits at 2, background at 8.
	type event struct{ mass, ctrl float64 }
	var events []event

	for range nsig {
		m, err := pdf.Generate(rnd, pdf.Gaussian(), lo, hi, []float64{5, 0.4}, 1)
		if err != nil {
			t.Fatalf("could not generate: %+v", err)
		}
		events = append(events, event{mass: m[0], ctrl: 2 + rnd.NormFloat64()*0.5})
	}
	for range nbkg {
		events = append(events, event{mass: rnd.Float64() * 10, ctrl: 8 + rnd.NormFloat64()*0.5})
	}

	mass := make([]float64, len(events))
	for i, e := range events {
		mass[i] = e.mass
	}

	model := pdf.Add(
		[]pdf.PDF{pdf.Gaussian(), pdf.Uniform()},
		[]string{"nsig", "nbkg"},
	)

	res, err := pdf.FitUnbinned(mass, model, lo, hi, []minuit.Par{
		{Name: "nsig", Value: 2000, Step: 100, Min: 0, Max: 50000},
		{Name: "nbkg", Value: 6000, Step: 100, Min: 0, Max: 50000},
		{Name: "mean", Value: 4.8, Step: 0.1, Min: lo, Max: hi},
		{Name: "sigma", Value: 0.5, Step: 0.05, Min: 0.01, Max: 3},
	})
	if err != nil {
		t.Fatalf("could not fit: %+v", err)
	}

	sw, err := pdf.SPlot(mass, model, lo, hi, res.Values())
	if err != nil {
		t.Fatalf("could not compute sWeights: %+v", err)
	}

	if got, want := len(sw.W), len(events); got != want {
		t.Fatalf("weights: got %d rows, want %d", got, want)
	}
	if got, want := sw.Names(), []string{"nsig", "nbkg"}; len(got) != len(want) || got[0] != want[0] {
		t.Errorf("names: got=%v, want=%v", got, want)
	}

	// the weights sum to the fitted yields, which is what they are built to.
	fitSig, _ := res.Value(0)
	fitBkg, _ := res.Value(1)
	if got := sw.Sum(0); math.Abs(got-fitSig) > 1e-6*math.Abs(fitSig) {
		t.Errorf("signal weights sum to %v, the fit said %v", got, fitSig)
	}
	if got := sw.Sum(1); math.Abs(got-fitBkg) > 1e-6*math.Abs(fitBkg) {
		t.Errorf("background weights sum to %v, the fit said %v", got, fitBkg)
	}

	// and now the point of it: the weighted mean of the control variable
	// should come out where the signal is, not where the mixture is.
	var (
		wsum  float64
		wmean float64
	)
	for i, e := range events {
		wsum += sw.W[i][0]
		wmean += sw.W[i][0] * e.ctrl
	}
	wmean /= wsum

	if math.Abs(wmean-2) > 0.15 {
		t.Errorf("the signal-weighted control mean is %v, want about 2", wmean)
	}

	// the unweighted mean is nowhere near it, which is what makes this
	// worth doing.
	var plain float64
	for _, e := range events {
		plain += e.ctrl
	}
	plain /= float64(len(events))
	if math.Abs(plain-2) < 1 {
		t.Errorf("the unweighted mean is %v, too close to the signal's for this to prove anything", plain)
	}
}

func TestSPlotErrors(t *testing.T) {
	data := []float64{1, 2, 3}

	t.Run("fractions", func(t *testing.T) {
		// a sum of fractions has no yields to build weights from.
		sum := pdf.Add([]pdf.PDF{pdf.Gaussian(), pdf.Uniform()}, []string{"f"})
		if _, err := pdf.SPlot(data, sum, 0, 10, []float64{0.3, 5, 1}); err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("no data", func(t *testing.T) {
		sum := pdf.Add([]pdf.PDF{pdf.Gaussian(), pdf.Uniform()}, []string{"a", "b"})
		if _, err := pdf.SPlot(nil, sum, 0, 10, []float64{1, 1, 5, 1}); err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("wrong parameters", func(t *testing.T) {
		sum := pdf.Add([]pdf.PDF{pdf.Gaussian(), pdf.Uniform()}, []string{"a", "b"})
		if _, err := pdf.SPlot(data, sum, 0, 10, []float64{1}); err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("nil sum", func(t *testing.T) {
		if _, err := pdf.SPlot(data, nil, 0, 10, nil); err == nil {
			t.Fatal("expected an error")
		}
	})
}

// TestStudy checks a study finds a fit unbiased with honest uncertainties,
// which is the thing a study is run to find out.
func TestStudy(t *testing.T) {
	rnd := rand.New(rand.NewPCG(5, 6))

	s := &pdf.Study{
		PDF:  pdf.Gaussian(),
		True: []float64{5, 1},
		N:    500,
		Lo:   0,
		Hi:   10,
		Start: []minuit.Par{
			// deliberately not the true values: a fit that only works when
			// started at the answer is not working.
			{Name: "mean", Value: 4.0, Step: 0.2, Min: 0, Max: 10},
			{Name: "sigma", Value: 1.5, Step: 0.1, Min: 0.01, Max: 5},
		},
	}

	res, err := s.Run(rnd, 200)
	if err != nil {
		t.Fatalf("could not run the study: %+v", err)
	}

	if res.Fits < 190 {
		t.Errorf("only %d of 200 toys converged", res.Fits)
	}

	for i, name := range []string{"mean", "sigma"} {
		// the pull of an unbiased fit with honest errors is a unit
		// gaussian. With 200 toys the mean is known to about 1/sqrt(200),
		// which is 0.07, so three of those is the tolerance.
		if got := res.PullMean(i); math.Abs(got) > 0.25 {
			t.Errorf("%s: pull mean is %v, want about 0", name, got)
		}
		if got := res.PullWidth(i); math.Abs(got-1) > 0.2 {
			t.Errorf("%s: pull width is %v, want about 1", name, got)
		}
		if got := res.Bias(i, s.True[i]); math.Abs(got) > 0.1 {
			t.Errorf("%s: bias is %v, want about 0", name, got)
		}
	}
}

func TestStudyErrors(t *testing.T) {
	rnd := rand.New(rand.NewPCG(7, 8))

	for _, tc := range []struct {
		name string
		s    *pdf.Study
		n    int
	}{
		{"no density", &pdf.Study{Lo: 0, Hi: 1}, 10},
		{"wrong true count", &pdf.Study{PDF: pdf.Gaussian(), True: []float64{1}, Start: make([]minuit.Par, 2), Lo: 0, Hi: 1}, 10},
		{"wrong start count", &pdf.Study{PDF: pdf.Gaussian(), True: []float64{1, 1}, Start: make([]minuit.Par, 1), Lo: 0, Hi: 1}, 10},
		{"bad range", &pdf.Study{PDF: pdf.Gaussian(), True: []float64{1, 1}, Start: make([]minuit.Par, 2), Lo: 1, Hi: 0}, 10},
		{"no toys", &pdf.Study{PDF: pdf.Gaussian(), True: []float64{1, 1}, Start: make([]minuit.Par, 2), Lo: 0, Hi: 1}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.s.Run(rnd, tc.n); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

// TestSignificance checks the conversion from a likelihood difference to
// standard deviations, and that it refuses to report a negative one.
func TestSignificance(t *testing.T) {
	for _, tc := range []struct {
		null, best float64
		want       float64
	}{
		{0.5, 0, 1},  // half a unit is one sigma
		{2, 0, 2},    // two units is two sigma
		{12.5, 0, 5}, // the discovery threshold
		{0, 0, 0},    // no difference, no significance
		{0, 5, 0},    // the null fits better: not a negative sigma
	} {
		if got := pdf.Significance(tc.null, tc.best); math.Abs(got-tc.want) > 1e-12 {
			t.Errorf("Significance(%v, %v): got=%v, want=%v", tc.null, tc.best, got, tc.want)
		}
	}
}
