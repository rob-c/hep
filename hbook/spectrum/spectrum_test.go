// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package spectrum_test

import (
	"math"
	"math/rand/v2"
	"testing"

	"go-hep.org/x/hep/hbook"
	"go-hep.org/x/hep/hbook/spectrum"
)

// build makes a histogram of a function of the bin centre, which is how
// every spectrum below is put together: the peaks are placed on purpose, so
// what a search should find is known before it runs.
func build(n int, lo, hi float64, fct func(x float64) float64) *hbook.H1D {
	h := hbook.NewH1D(n, lo, hi)
	for i, b := range h.Binning.Bins {
		_ = i
		h.Fill(b.XMid(), fct(b.XMid()))
	}
	return h
}

func gauss(x, mean, sigma, height float64) float64 {
	d := (x - mean) / sigma
	return height * math.Exp(-0.5*d*d)
}

// TestSearchFindsPlacedPeaks checks that peaks put at known places are found
// there, on a flat background.
func TestSearchFindsPlacedPeaks(t *testing.T) {
	want := []float64{200, 500, 750}

	h := build(1000, 0, 1000, func(x float64) float64 {
		y := 50.0 // a flat continuum
		for _, mean := range want {
			y += gauss(x, mean, 8, 400)
		}
		return y
	})

	peaks, err := spectrum.Search(h, spectrum.Sigma(4), spectrum.Iterations(30))
	if err != nil {
		t.Fatalf("could not search: %+v", err)
	}
	if got, want := len(peaks), len(want); got != want {
		t.Fatalf("found %d peak(s), want %d: %+v", got, want, peaks)
	}

	// they come back tallest first, and here they are the same height, so
	// compare them by position.
	got := make([]float64, len(peaks))
	for i, p := range peaks {
		got[i] = p.X
	}
	for _, mean := range want {
		if !near(got, mean, 2) {
			t.Errorf("no peak within 2 of %v: got %v", mean, got)
		}
	}
}

func near(xs []float64, want, tol float64) bool {
	for _, x := range xs {
		if math.Abs(x-want) <= tol {
			return true
		}
	}
	return false
}

// TestSearchOnASlopingBackground checks the part that earns the package: a
// continuum that wanders should not be mistaken for peaks, and peaks on top
// of it should still be found.
func TestSearchOnASlopingBackground(t *testing.T) {
	want := []float64{300, 600}

	h := build(1000, 0, 1000, func(x float64) float64 {
		// a continuum falling by an order of magnitude across the range
		y := 500 * math.Exp(-x/300)
		for _, mean := range want {
			y += gauss(x, mean, 10, 300)
		}
		return y
	})

	peaks, err := spectrum.Search(h, spectrum.Sigma(5), spectrum.Iterations(40))
	if err != nil {
		t.Fatalf("could not search: %+v", err)
	}
	if got, want := len(peaks), len(want); got != want {
		t.Fatalf("found %d peak(s), want %d: %+v", got, want, peaks)
	}
	got := make([]float64, len(peaks))
	for i, p := range peaks {
		got[i] = p.X
	}
	for _, mean := range want {
		if !near(got, mean, 3) {
			t.Errorf("no peak within 3 of %v: got %v", mean, got)
		}
	}
}

// TestBackgroundFollowsTheContinuum checks that the background estimate sits
// on the continuum where there is no peak, and under the peak where there
// is one.
func TestBackgroundFollowsTheContinuum(t *testing.T) {
	const (
		n    = 500
		mean = 250.0
	)
	cont := func(x float64) float64 { return 100 + 0.2*x }

	h := build(n, 0, 500, func(x float64) float64 {
		return cont(x) + gauss(x, mean, 6, 500)
	})

	var ys []float64
	for _, b := range h.Binning.Bins {
		ys = append(ys, b.SumW())
	}

	bkg := spectrum.Background(ys, spectrum.Iterations(30))
	if got, want := len(bkg), n; got != want {
		t.Fatalf("background has %d channels, want %d", got, want)
	}

	for i, b := range h.Binning.Bins {
		x := b.XMid()

		// never above what it was given, and never negative.
		if bkg[i] > ys[i]+1e-9 {
			t.Fatalf("channel %d: background %v is above the spectrum %v", i, bkg[i], ys[i])
		}
		if bkg[i] < 0 {
			t.Fatalf("channel %d: background is negative: %v", i, bkg[i])
		}

		// well away from the peak it should be the continuum itself.
		if math.Abs(x-mean) > 60 && i > 20 && i < n-20 {
			if got, want := bkg[i], cont(x); math.Abs(got-want) > 0.1*want {
				t.Errorf("channel %d (x=%v): background %v is not the continuum %v", i, x, got, want)
			}
		}
	}

	// and under the peak it should stay near the continuum rather than
	// climbing into it, which is the whole point.
	top := int(mean / 500 * n)
	if got, want := bkg[top], cont(mean); got > 1.3*want {
		t.Errorf("the background climbed into the peak: %v against a continuum of %v", got, want)
	}
}

// TestBackgroundLeavesAFlatSpectrumAlone checks the case with no peaks at
// all: the background is the spectrum.
func TestBackgroundLeavesAFlatSpectrumAlone(t *testing.T) {
	ys := make([]float64, 200)
	for i := range ys {
		ys[i] = 42
	}

	bkg := spectrum.Background(ys)
	for i, v := range bkg {
		if math.Abs(v-42) > 0.5 {
			t.Fatalf("channel %d: got=%v, want=42", i, v)
		}
	}
}

// TestSearchIgnoresWhatIsTooSmall checks the threshold.
func TestSearchIgnoresWhatIsTooSmall(t *testing.T) {
	h := build(1000, 0, 1000, func(x float64) float64 {
		return 20 + gauss(x, 300, 8, 500) + gauss(x, 700, 8, 25)
	})

	// the small peak is a twentieth of the big one, so a threshold above
	// that should leave it out and one below should take it in.
	for _, tc := range []struct {
		threshold float64
		want      int
	}{
		{0.5, 1},
		{0.01, 2},
	} {
		peaks, err := spectrum.Search(h,
			spectrum.Sigma(4), spectrum.Iterations(30),
			spectrum.Threshold(tc.threshold),
		)
		if err != nil {
			t.Fatalf("could not search: %+v", err)
		}
		if got := len(peaks); got != tc.want {
			t.Errorf("threshold %v: found %d peak(s), want %d: %+v",
				tc.threshold, got, tc.want, peaks)
		}
	}
}

// TestSearchSeparatesCloseAndMergesCloser checks that two peaks are two
// peaks when they are further apart than the width being looked for, and
// one when they are not.
func TestSearchSeparatesCloseAndMergesCloser(t *testing.T) {
	for _, tc := range []struct {
		gap  float64
		want int
	}{
		{60, 2},
		{2, 1},
	} {
		h := build(1000, 0, 1000, func(x float64) float64 {
			return 10 + gauss(x, 500-tc.gap/2, 6, 300) + gauss(x, 500+tc.gap/2, 6, 300)
		})

		peaks, err := spectrum.Search(h, spectrum.Sigma(10), spectrum.Iterations(30))
		if err != nil {
			t.Fatalf("could not search: %+v", err)
		}
		if got := len(peaks); got != tc.want {
			t.Errorf("a gap of %v: found %d peak(s), want %d: %+v", tc.gap, got, tc.want, peaks)
		}
	}
}

// TestSearchWithNoise checks that a search still works on counts that
// fluctuate the way real ones do.
func TestSearchWithNoise(t *testing.T) {
	rnd := rand.New(rand.NewPCG(42, 43))
	want := []float64{250, 600}

	h := hbook.NewH1D(500, 0, 1000)
	for _, b := range h.Binning.Bins {
		x := b.XMid()
		mean := 100 + 0.1*x
		for _, m := range want {
			mean += gauss(x, m, 15, 600)
		}
		// counts of that mean, spread as counts are.
		h.Fill(x, mean+math.Sqrt(mean)*rnd.NormFloat64())
	}

	// Counts that fluctuate by their own square root leave bumps of a few
	// percent of the tallest peak, and the threshold is what decides
	// whether those count. The default of a twentieth, which is ROOT's,
	// takes them in; a fifth does not. That is the knob, and the test
	// turns it rather than pretending the noise is not there.
	loose, err := spectrum.Search(h, spectrum.Sigma(8), spectrum.Iterations(40), spectrum.Smoothing(5))
	if err != nil {
		t.Fatalf("could not search: %+v", err)
	}
	if len(loose) <= len(want) {
		t.Errorf("a threshold of a twentieth should have taken in some noise, found %d peak(s)", len(loose))
	}

	peaks, err := spectrum.Search(h,
		spectrum.Sigma(8), spectrum.Iterations(40),
		spectrum.Smoothing(5), spectrum.Threshold(0.2),
	)
	if err != nil {
		t.Fatalf("could not search: %+v", err)
	}
	if got, want := len(peaks), len(want); got != want {
		t.Fatalf("found %d peak(s), want %d: %+v", got, want, peaks)
	}
	got := make([]float64, len(peaks))
	for i, p := range peaks {
		got[i] = p.X
	}
	for _, m := range want {
		if !near(got, m, 10) {
			t.Errorf("no peak within 10 of %v: got %v", m, got)
		}
	}

	// and whatever the threshold, the two real peaks are the two tallest.
	for i, m := range []float64{600, 250} {
		if !near([]float64{loose[i].X}, m, 10) {
			t.Errorf("the %d tallest peak is at %v, want near %v", i, loose[i].X, m)
		}
	}
}

// TestPeakWidth checks that the width reported for a peak is about the one
// it was given.
func TestPeakWidth(t *testing.T) {
	const sigma = 12.0

	h := build(1000, 0, 1000, func(x float64) float64 {
		return 10 + gauss(x, 500, sigma, 400)
	})

	peaks, err := spectrum.Search(h, spectrum.Sigma(6), spectrum.Iterations(40), spectrum.Smoothing(0))
	if err != nil {
		t.Fatalf("could not search: %+v", err)
	}
	if len(peaks) != 1 {
		t.Fatalf("found %d peak(s), want 1: %+v", len(peaks), peaks)
	}

	// the full width at half the height of a gaussian is about 2.355 sigma.
	want := 2 * math.Sqrt(2*math.Ln2) * sigma
	if got := peaks[0].Width; math.Abs(got-want) > 0.25*want {
		t.Errorf("width: got=%v, want about %v", got, want)
	}
}

// TestSmooth checks that smoothing keeps the area and flattens the wiggles.
func TestSmooth(t *testing.T) {
	ys := make([]float64, 200)
	for i := range ys {
		ys[i] = 100
		if i%2 == 0 {
			ys[i] = 120
		}
	}

	out := spectrum.Smooth(ys, 4)

	// the middle should have settled near the average of the two values.
	for i := 20; i < 180; i++ {
		if math.Abs(out[i]-110) > 4 {
			t.Fatalf("channel %d: got=%v, want about 110", i, out[i])
		}
	}

	// and a window of zero should change nothing.
	same := spectrum.Smooth(ys, 0)
	for i := range ys {
		if same[i] != ys[i] {
			t.Fatalf("channel %d: smoothing over nothing changed %v to %v", i, ys[i], same[i])
		}
	}
}

// TestSearchErrors checks what a search refuses.
func TestSearchErrors(t *testing.T) {
	if _, err := spectrum.Search(nil); err == nil {
		t.Error("searching nothing was accepted")
	}
	if _, err := spectrum.Search(hbook.NewH1D(2, 0, 1)); err == nil {
		t.Error("searching a histogram of two bins was accepted")
	}

	// an empty histogram has no peaks and is not an error.
	peaks, err := spectrum.Search(hbook.NewH1D(100, 0, 1))
	if err != nil {
		t.Errorf("searching an empty histogram: %+v", err)
	}
	if len(peaks) != 0 {
		t.Errorf("found %d peak(s) in an empty histogram", len(peaks))
	}
}
