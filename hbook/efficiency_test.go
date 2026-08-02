// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hbook_test

import (
	"math"
	"testing"

	"go-hep.org/x/hep/hbook"
)

func TestBinomialInterval(t *testing.T) {
	t.Parallel()

	const (
		cl68 = hbook.DefaultEffLevel
		cl95 = 0.95
	)

	for _, tc := range []struct {
		name   string
		kind   hbook.IntervalKind
		k, n   float64
		level  float64
		lo, hi float64
	}{
		// the textbook Clopper-Pearson values
		{
			name: "clopper-pearson/1-of-10", kind: hbook.ClopperPearson,
			k: 1, n: 10, level: cl95, lo: 0.0025285785, hi: 0.4450161170,
		},
		{
			name: "clopper-pearson/80-of-100", kind: hbook.ClopperPearson,
			k: 80, n: 100, level: cl95, lo: 0.7081573109, hi: 0.8733444479,
		},
		{
			// with no successes the lower limit is 0 and the upper one is
			// 1 - (alpha/2)^(1/n), which is 0.16815 here
			name: "clopper-pearson/none", kind: hbook.ClopperPearson,
			k: 0, n: 10, level: cl68, lo: 0, hi: 0.1681491861,
		},
		{
			// and the mirror image when everything passes
			name: "clopper-pearson/all", kind: hbook.ClopperPearson,
			k: 10, n: 10, level: cl68, lo: 0.8318508139, hi: 1,
		},
		{
			name: "clopper-pearson/half", kind: hbook.ClopperPearson,
			k: 5, n: 10, level: cl68, lo: 0.3048178830, hi: 0.6951821170,
		},
		{
			name: "clopper-pearson/3-of-3", kind: hbook.ClopperPearson,
			k: 3, n: 3, level: cl95, lo: 0.2924017738, hi: 1,
		},

		// the textbook Wilson values
		{
			name: "wilson/1-of-10", kind: hbook.Wilson,
			k: 1, n: 10, level: cl95, lo: 0.0178762131, hi: 0.4041500268,
		},
		{
			name: "wilson/80-of-100", kind: hbook.Wilson,
			k: 80, n: 100, level: cl95, lo: 0.7111708344, hi: 0.8666330667,
		},
		{
			name: "wilson/none", kind: hbook.Wilson,
			k: 0, n: 10, level: cl68, lo: 0, hi: 0.0909090909,
		},
		{
			name: "wilson/all", kind: hbook.Wilson,
			k: 10, n: 10, level: cl68, lo: 0.9090909091, hi: 1,
		},

		{
			name: "agresti-coull/1-of-10", kind: hbook.AgrestiCoull,
			k: 1, n: 10, level: cl95, lo: 0, hi: 0.4259677374,
		},
		{
			name: "agresti-coull/80-of-100", kind: hbook.AgrestiCoull,
			k: 80, n: 100, level: cl95, lo: 0.7104115897, hi: 0.8673923114,
		},

		{
			name: "normal/half", kind: hbook.Normal,
			k: 5, n: 10, level: cl68, lo: 0.3418861170, hi: 0.6581138830,
		},
		{
			// the reason the normal interval is not the default: with every
			// event passing it claims to know the efficiency exactly
			name: "normal/all", kind: hbook.Normal,
			k: 3, n: 3, level: cl95, lo: 1, hi: 1,
		},
		{
			name: "normal/none", kind: hbook.Normal,
			k: 0, n: 10, level: cl68, lo: 0, hi: 0,
		},

		// degenerate inputs
		{name: "empty", kind: hbook.ClopperPearson, k: 0, n: 0, level: cl68, lo: 0, hi: 0},
		{name: "negative-n", kind: hbook.Wilson, k: 1, n: -1, level: cl68, lo: 0, hi: 0},
		{
			// a numerator nudged past its denominator by rounding is clamped
			// rather than turned into a nonsensical interval
			name: "over-full", kind: hbook.ClopperPearson,
			k: 11, n: 10, level: cl68, lo: 0.8318508139, hi: 1,
		},
		{
			name: "under-empty", kind: hbook.ClopperPearson,
			k: -1, n: 10, level: cl68, lo: 0, hi: 0.1681491861,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lo, hi := hbook.BinomialInterval(tc.kind, tc.k, tc.n, tc.level)
			if math.Abs(lo-tc.lo) > 1e-8 || math.Abs(hi-tc.hi) > 1e-8 {
				t.Fatalf("got [%v, %v], want [%v, %v]", lo, hi, tc.lo, tc.hi)
			}
			if lo < 0 || hi > 1 || lo > hi {
				t.Fatalf("[%v, %v] is not an interval inside [0, 1]", lo, hi)
			}
		})
	}
}

// TestBinomialIntervalOrdering checks the properties the intervals are chosen
// for, over a range of inputs rather than at a handful of points.
func TestBinomialIntervalOrdering(t *testing.T) {
	t.Parallel()

	kinds := []hbook.IntervalKind{hbook.ClopperPearson, hbook.Wilson, hbook.AgrestiCoull, hbook.Normal}
	for _, n := range []float64{1, 2, 5, 10, 100, 1000} {
		for k := 0.0; k <= n; k++ {
			for _, kind := range kinds {
				lo, hi := hbook.BinomialInterval(kind, k, n, hbook.DefaultEffLevel)
				eff := k / n
				switch {
				case lo < 0 || hi > 1:
					t.Fatalf("%v: %v/%v gives [%v, %v], outside [0, 1]", kind, k, n, lo, hi)
				case lo > eff+1e-12 || hi < eff-1e-12:
					t.Fatalf("%v: %v/%v gives [%v, %v], which does not contain %v", kind, k, n, lo, hi, eff)
				}
			}

			// Clopper-Pearson is the conservative one: it never reports a
			// narrower interval than the approximations do.
			var (
				cpLo, cpHi = hbook.BinomialInterval(hbook.ClopperPearson, k, n, hbook.DefaultEffLevel)
				wLo, wHi   = hbook.BinomialInterval(hbook.Wilson, k, n, hbook.DefaultEffLevel)
			)
			if cpHi-cpLo < wHi-wLo-1e-12 {
				t.Fatalf("%v/%v: clopper-pearson [%v, %v] is narrower than wilson [%v, %v]",
					k, n, cpLo, cpHi, wLo, wHi,
				)
			}

			// and a larger sample says more, whatever the recipe
			for _, kind := range kinds {
				lo1, hi1 := hbook.BinomialInterval(kind, k, n, hbook.DefaultEffLevel)
				lo2, hi2 := hbook.BinomialInterval(kind, 10*k, 10*n, hbook.DefaultEffLevel)
				if hi2-lo2 > hi1-lo1+1e-12 {
					t.Fatalf("%v: %v/%v gives [%v, %v] but ten times the sample gives the wider [%v, %v]",
						kind, k, n, lo1, hi1, lo2, hi2,
					)
				}
			}
		}
	}
}

func TestBinomialIntervalPanics(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		kind  hbook.IntervalKind
		level float64
	}{
		{name: "level-too-low", kind: hbook.Wilson, level: 0},
		{name: "level-too-high", kind: hbook.Wilson, level: 1},
		{name: "invalid-kind", kind: hbook.IntervalKind(42), level: 0.95},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				if recover() == nil {
					t.Fatalf("expected a panic")
				}
			}()
			_, _ = hbook.BinomialInterval(tc.kind, 1, 10, tc.level)
		})
	}
}

func TestIntervalKindString(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		kind hbook.IntervalKind
		want string
	}{
		{hbook.ClopperPearson, "clopper-pearson"},
		{hbook.Wilson, "wilson"},
		{hbook.AgrestiCoull, "agresti-coull"},
		{hbook.Normal, "normal"},
	} {
		if got := tc.kind.String(); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Errorf("expected a panic for an out-of-range IntervalKind")
			}
		}()
		_ = hbook.IntervalKind(42).String()
	}()
}

// effHistos builds a pass/total pair holding the counts given, one bin per
// entry, over [0, len(counts)).
func effHistos(t *testing.T, pass, total []float64) (*hbook.H1D, *hbook.H1D) {
	t.Helper()

	n := len(total)
	hpass := hbook.NewH1D(n, 0, float64(n))
	htot := hbook.NewH1D(n, 0, float64(n))
	hpass.Annotation()["name"] = "pass"
	htot.Annotation()["name"] = "total"

	for i := range n {
		x := float64(i) + 0.5
		for range int(pass[i]) {
			hpass.Fill(x, 1)
		}
		for range int(total[i]) {
			htot.Fill(x, 1)
		}
	}
	return hpass, htot
}

func TestNewEfficiency(t *testing.T) {
	t.Parallel()

	var (
		pass  = []float64{0, 5, 10, 0}
		total = []float64{10, 10, 10, 0}
	)
	hpass, htot := effHistos(t, pass, total)

	eff, err := hbook.NewEfficiency(hpass, htot)
	if err != nil {
		t.Fatalf("could not build the efficiency: %+v", err)
	}

	if got, want := eff.Len(), len(total); got != want {
		t.Fatalf("got %d bins, want %d", got, want)
	}
	if got, want := eff.IntervalKind(), hbook.ClopperPearson; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, want := eff.Level(), hbook.DefaultEffLevel; got != want {
		t.Errorf("got level %v, want %v", got, want)
	}
	if got, want := eff.Rank(), 1; got != want {
		t.Errorf("got rank %d, want %d", got, want)
	}

	for _, tc := range []struct {
		bin          int
		eff          float64
		errLo, errHi float64
	}{
		// the counts are the ones the interval test checks against the
		// textbook, so the efficiency has to reproduce them bin by bin
		{bin: 0, eff: 0.0, errLo: 0, errHi: 0.1681491861},
		{bin: 1, eff: 0.5, errLo: 0.5 - 0.3048178830, errHi: 0.6951821170 - 0.5},
		{bin: 2, eff: 1.0, errLo: 1 - 0.8318508139, errHi: 0},
		// an empty bin has no efficiency to speak of, and says so with a
		// point at zero rather than a NaN
		{bin: 3, eff: 0, errLo: 0, errHi: 0},
	} {
		b := eff.Bin(tc.bin)
		switch {
		case math.Abs(b.Eff-tc.eff) > 1e-12:
			t.Errorf("bin %d: got eff=%v, want %v", tc.bin, b.Eff, tc.eff)
		case math.Abs(b.ErrLo-tc.errLo) > 1e-8:
			t.Errorf("bin %d: got errlo=%v, want %v", tc.bin, b.ErrLo, tc.errLo)
		case math.Abs(b.ErrHi-tc.errHi) > 1e-8:
			t.Errorf("bin %d: got errhi=%v, want %v", tc.bin, b.ErrHi, tc.errHi)
		}

		if got, want := b.XRange, (hbook.Range{Min: float64(tc.bin), Max: float64(tc.bin + 1)}); got != want {
			t.Errorf("bin %d: got x-range %v, want %v", tc.bin, got, want)
		}
		if lo, hi := b.EffLo(), b.EffHi(); lo < 0 || hi > 1 {
			t.Errorf("bin %d: the interval [%v, %v] leaves [0, 1]", tc.bin, lo, hi)
		}
	}

	// this is what quadrature error propagation gets wrong: a bin where
	// everything passed has no room to go up, and one where nothing did has
	// none to go down.
	if got := eff.Bin(2).ErrHi; got != 0 {
		t.Errorf("an efficiency of 1 has an upper error of %v, want 0", got)
	}
	if got := eff.Bin(0).ErrLo; got != 0 {
		t.Errorf("an efficiency of 0 has a lower error of %v, want 0", got)
	}
}

func TestEfficiencyOptions(t *testing.T) {
	t.Parallel()

	hpass, htot := effHistos(t, []float64{1}, []float64{10})

	for _, tc := range []struct {
		name   string
		opts   []hbook.EffOption
		lo, hi float64
	}{
		{
			name: "wilson-95",
			opts: []hbook.EffOption{hbook.WithEffInterval(hbook.Wilson), hbook.WithEffLevel(0.95)},
			lo:   0.0178762131,
			hi:   0.4041500268,
		},
		{
			name: "clopper-pearson-95",
			opts: []hbook.EffOption{hbook.WithEffLevel(0.95)},
			lo:   0.0025285785,
			hi:   0.4450161170,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			eff, err := hbook.NewEfficiency(hpass, htot, tc.opts...)
			if err != nil {
				t.Fatalf("could not build the efficiency: %+v", err)
			}
			b := eff.Bin(0)
			if got, want := b.EffLo(), tc.lo; math.Abs(got-want) > 1e-8 {
				t.Errorf("got lower limit %v, want %v", got, want)
			}
			if got, want := b.EffHi(), tc.hi; math.Abs(got-want) > 1e-8 {
				t.Errorf("got upper limit %v, want %v", got, want)
			}
		})
	}
}

// TestEfficiencyWeighted checks that a sample filled with a constant weight
// says exactly what the same sample filled with unit weights says: the
// weights cancel out of the efficiency, and out of the effective entries the
// interval is computed from.
func TestEfficiencyWeighted(t *testing.T) {
	t.Parallel()

	hpass, htot := effHistos(t, []float64{3}, []float64{10})

	wpass := hbook.NewH1D(1, 0, 1)
	wtot := hbook.NewH1D(1, 0, 1)
	for range 3 {
		wpass.Fill(0.5, 2)
	}
	for range 10 {
		wtot.Fill(0.5, 2)
	}

	ref, err := hbook.NewEfficiency(hpass, htot)
	if err != nil {
		t.Fatalf("could not build the efficiency: %+v", err)
	}
	got, err := hbook.NewEfficiency(wpass, wtot)
	if err != nil {
		t.Fatalf("could not build the weighted efficiency: %+v", err)
	}

	if got.Bin(0) != ref.Bin(0) {
		t.Fatalf("weighting every event by 2 changed the answer:\ngot  %+v\nwant %+v", got.Bin(0), ref.Bin(0))
	}

	// whereas events that carry a spread of weights are worth less than their
	// number: the interval widens.
	spread := hbook.NewH1D(1, 0, 1)
	for _, w := range []float64{1, 1, 1, 1, 1, 1, 1, 1, 1, 11} {
		spread.Fill(0.5, w)
	}
	sub := hbook.NewH1D(1, 0, 1)
	for range 3 {
		sub.Fill(0.5, 2)
	}
	wide, err := hbook.NewEfficiency(sub, spread)
	if err != nil {
		t.Fatalf("could not build the efficiency: %+v", err)
	}
	if b := wide.Bin(0); b.Total >= 10 {
		t.Fatalf("got %v effective entries out of 10 events of unequal weight, want fewer", b.Total)
	}
}

func TestEfficiencyErrors(t *testing.T) {
	t.Parallel()

	hpass, htot := effHistos(t, []float64{1, 2}, []float64{10, 10})

	other := hbook.NewH1D(3, 0, 3)
	other.Annotation()["name"] = "other"

	shifted := hbook.NewH1D(2, 1, 3)
	shifted.Annotation()["name"] = "shifted"

	tooMany := hbook.NewH1D(2, 0, 2)
	tooMany.Annotation()["name"] = "toomany"
	for range 11 {
		tooMany.Fill(0.5, 1)
	}

	negative := hbook.NewH1D(2, 0, 2)
	negative.Annotation()["name"] = "negative"
	negative.Fill(0.5, -1)

	for _, tc := range []struct {
		name        string
		pass, total *hbook.H1D
		opts        []hbook.EffOption
		want        string
	}{
		{
			name: "nil-numerator", pass: nil, total: htot,
			want: "hbook: nil histogram",
		},
		{
			name: "nil-denominator", pass: hpass, total: nil,
			want: "hbook: nil histogram",
		},
		{
			name: "bin-count", pass: hpass, total: other,
			want: `hbook: "pass" has 2 bins, "other" has 3: the two have to share a binning`,
		},
		{
			name: "bin-edges", pass: hpass, total: shifted,
			want: "hbook: x binnings are not equivalent in pass / shifted",
		},
		{
			name: "not-a-subset", pass: tooMany, total: htot,
			want: "hbook: bin 0 of toomany / total passes more than it counts (11 > 10): the numerator has to be a subset of the denominator",
		},
		{
			name: "negative-weights", pass: negative, total: htot,
			want: "hbook: bin 0 of negative / total holds a negative sum of weights (-1 / 10): it is not a count of anything",
		},
		{
			name: "level-too-low", pass: hpass, total: htot,
			opts: []hbook.EffOption{hbook.WithEffLevel(0)},
			want: "hbook: confidence level 0 out of range (0, 1)",
		},
		{
			name: "level-too-high", pass: hpass, total: htot,
			opts: []hbook.EffOption{hbook.WithEffLevel(1)},
			want: "hbook: confidence level 1 out of range (0, 1)",
		},
		{
			name: "invalid-kind", pass: hpass, total: htot,
			opts: []hbook.EffOption{hbook.WithEffInterval(hbook.IntervalKind(42))},
			want: "hbook: invalid IntervalKind (42)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := hbook.NewEfficiency(tc.pass, tc.total, tc.opts...)
			switch {
			case err == nil:
				t.Fatalf("expected an error")
			case err.Error() != tc.want:
				t.Fatalf("got  %q\nwant %q", err.Error(), tc.want)
			}
		})
	}
}

func TestEfficiencyPlotting(t *testing.T) {
	t.Parallel()

	hpass, htot := effHistos(t, []float64{0, 5, 10}, []float64{10, 10, 10})
	eff, err := hbook.NewEfficiency(hpass, htot)
	if err != nil {
		t.Fatalf("could not build the efficiency: %+v", err)
	}
	eff.Annotation()["name"] = "trigger"

	if got, want := eff.Name(), "trigger"; got != want {
		t.Errorf("got name %q, want %q", got, want)
	}

	for i := range eff.Len() {
		b := eff.Bin(i)
		if x, y := eff.XY(i); x != b.XMid() || y != b.Eff {
			t.Errorf("bin %d: got XY=(%v, %v), want (%v, %v)", i, x, y, b.XMid(), b.Eff)
		}
		if got, want := eff.Value(i), b.Eff; got != want {
			t.Errorf("bin %d: got value %v, want %v", i, got, want)
		}
		if lo, hi := eff.XError(i); lo != 0.5 || hi != 0.5 {
			t.Errorf("bin %d: got x-error (%v, %v), want (0.5, 0.5)", i, lo, hi)
		}
		if lo, hi := eff.YError(i); lo != b.ErrLo || hi != b.ErrHi {
			t.Errorf("bin %d: got y-error (%v, %v), want (%v, %v)", i, lo, hi, b.ErrLo, b.ErrHi)
		}
	}

	if got, want := len(eff.Bins()), eff.Len(); got != want {
		t.Errorf("got %d bins, want %d", got, want)
	}

	xmin, xmax, ymin, ymax := eff.DataRange()
	switch {
	case xmin != 0 || xmax != 3:
		t.Errorf("got x-range [%v, %v], want [0, 3]", xmin, xmax)
	case ymin != 0:
		t.Errorf("got y-min %v, want 0: the first bin has an efficiency of 0", ymin)
	case ymax != 1:
		t.Errorf("got y-max %v, want 1: the last bin has an efficiency of 1", ymax)
	}

	s2d := eff.S2D()
	if got, want := s2d.Len(), eff.Len(); got != want {
		t.Fatalf("got %d points, want %d", got, want)
	}
	if got, want := s2d.Name(), "trigger"; got != want {
		t.Errorf("got name %q, want %q", got, want)
	}
	for i := range s2d.Len() {
		var (
			b       = eff.Bin(i)
			x, y    = s2d.XY(i)
			lo, hi  = s2d.YError(i)
			xlo, xh = s2d.XError(i)
		)
		switch {
		case x != b.XMid() || y != b.Eff:
			t.Errorf("point %d: got (%v, %v), want (%v, %v)", i, x, y, b.XMid(), b.Eff)
		case lo != b.ErrLo || hi != b.ErrHi:
			t.Errorf("point %d: got y-error (%v, %v), want (%v, %v)", i, lo, hi, b.ErrLo, b.ErrHi)
		case xlo != 0.5 || xh != 0.5:
			t.Errorf("point %d: got x-error (%v, %v), want (0.5, 0.5)", i, xlo, xh)
		}
	}
}

// TestEfficiencyVsDivideH1D pins down the difference this type exists for.
func TestEfficiencyVsDivideH1D(t *testing.T) {
	t.Parallel()

	hpass, htot := effHistos(t, []float64{10}, []float64{10})

	eff, err := hbook.NewEfficiency(hpass, htot)
	if err != nil {
		t.Fatalf("could not build the efficiency: %+v", err)
	}
	ratio, err := hbook.DivideH1D(hpass, htot)
	if err != nil {
		t.Fatalf("could not divide: %+v", err)
	}

	// with every event passing, quadrature propagation still claims an
	// uncertainty, and one that reaches past 1
	lo, hi := ratio.YError(0)
	if hi <= 0 {
		t.Fatalf("DivideH1D reports no upper error at an efficiency of 1: nothing left to demonstrate")
	}
	if _, y := ratio.XY(0); y+hi <= 1 {
		t.Fatalf("DivideH1D keeps the ratio below 1: nothing left to demonstrate")
	}

	b := eff.Bin(0)
	switch {
	case b.ErrHi != 0:
		t.Errorf("got an upper error of %v at an efficiency of 1, want 0", b.ErrHi)
	case b.ErrLo <= 0:
		t.Errorf("got a lower error of %v at an efficiency of 1, want a positive one", b.ErrLo)
	case b.ErrLo >= lo+hi:
		t.Errorf("got a lower error of %v, want less than the %v quadrature gives either side", b.ErrLo, lo)
	}
}
