// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hbook

import (
	"fmt"
	"reflect"
	"testing"
)

// fills chosen to land in bins, in every in-range x/y stripe of the eight
// outflow corners, and to carry non-trivial weights, so the sum exercises
// the cross-term and the whole outflow array.
var addH2DFills = [][][3]float64{
	{ // h1
		{0.5, 0.5, 1},
		{1.5, 2.5, 2},
		{2.5, 1.5, 0.5},
		{-1, 0.5, 1},   // W
		{5, 0.5, 1},    // E
		{0.5, -1, 1},   // S
		{0.5, 5, 1},    // N
		{-1, -1, 0.25}, // SW
	},
	{ // h2
		{0.5, 0.5, 0.7},
		{1.5, 2.5, 1.2},
		{2.2, 0.7, 0.8},
		{-1, 5, 1}, // NW
		{5, 5, 1},  // NE
		{5, -1, 1}, // SE
		{1.5, 1.5, 2},
	},
}

func newAddH2DHist(fills ...[][3]float64) *H2D {
	h := NewH2D(3, 0, 3, 3, 0, 3)
	for _, fs := range fills {
		for _, f := range fs {
			h.Fill(f[0], f[1], f[2])
		}
	}
	return h
}

func TestAddH2D(t *testing.T) {
	// adding uncorrelated histograms is exactly equivalent to filling one
	// histogram with both fill sequences: every moment, every bin, every
	// outflow corner and the x*y cross-term must agree.
	var (
		h1   = newAddH2DHist(addH2DFills[0])
		h2   = newAddH2DHist(addH2DFills[1])
		got  = AddH2D(h1, h2)
		want = newAddH2DHist(addH2DFills...)
	)
	// the sums accumulate in a different order, so compare up to
	// floating-point round-off, not bit-for-bit
	if got, want := len(got.Binning.Bins), len(want.Binning.Bins); got != want {
		t.Fatalf("got %d bins, want %d", got, want)
	}
	for i := range got.Binning.Bins {
		if d := cmpDist2D(got.Binning.Bins[i].Dist, want.Binning.Bins[i].Dist); d != "" {
			t.Errorf("bin %d: %s", i, d)
		}
	}
	for i := range got.Binning.Outflows {
		if d := cmpDist2D(got.Binning.Outflows[i], want.Binning.Outflows[i]); d != "" {
			t.Errorf("outflow %d: %s", i, d)
		}
	}
	if d := cmpDist2D(got.Binning.Dist, want.Binning.Dist); d != "" {
		t.Errorf("global dist: %s", d)
	}
}

func cmpDist2D(a, b Dist2D) string {
	for _, v := range []struct {
		name   string
		av, bv float64
	}{
		{"n", float64(a.Entries()), float64(b.Entries())},
		{"sumw", a.SumW(), b.SumW()},
		{"sumw2", a.SumW2(), b.SumW2()},
		{"sumwx", a.SumWX(), b.SumWX()},
		{"sumwx2", a.SumWX2(), b.SumWX2()},
		{"sumwy", a.SumWY(), b.SumWY()},
		{"sumwy2", a.SumWY2(), b.SumWY2()},
		{"sumwxy", a.SumWXY(), b.SumWXY()},
	} {
		if !fuzzyEq(v.av, v.bv) {
			return fmt.Sprintf("%s differs: got %v, want %v", v.name, v.av, v.bv)
		}
	}
	return ""
}

func TestSubH2D(t *testing.T) {
	// h - h zeroes every weight moment (the entry counts add)
	h := newAddH2DHist(addH2DFills[0])
	diff := SubH2D(h, h)
	if got := diff.SumW(); got != 0 {
		t.Errorf("total sumw: got %v, want 0", got)
	}
	for i, bin := range diff.Binning.Bins {
		if bin.SumW() != 0 || bin.SumW2() < 0 {
			t.Errorf("bin %d: got sumw=%v sumw2=%v, want 0 and >=0", i, bin.SumW(), bin.SumW2())
		}
	}
	for i, out := range diff.Binning.Outflows {
		if got := out.SumW(); got != 0 {
			t.Errorf("outflow %d: got sumw=%v, want 0", i, got)
		}
	}
	if got, want := diff.Entries(), 2*h.Entries(); got != want {
		t.Errorf("entries: got %v, want %v", got, want)
	}
}

func TestAddScaledH2D(t *testing.T) {
	var (
		h    = newAddH2DHist(addH2DFills[0])
		got  = AddScaledH2D(h, 2, h)
		bin  = got.Binning.Bins[0] // holds the (0.5, 0.5, w=1) fill
		ref  = h.Binning.Bins[0]
		w    = ref.SumW()
		w2   = ref.SumW2()
		sumw = bin.SumW()
	)
	if want := 3 * w; sumw != want {
		t.Errorf("bin sumw: got %v, want %v", sumw, want)
	}
	if got, want := bin.SumW2(), 5*w2; got != want {
		t.Errorf("bin sumw2: got %v, want %v", got, want)
	}
	if got, want := got.Binning.Dist.Stats.SumWXY, 3*h.Binning.Dist.Stats.SumWXY; got != want {
		t.Errorf("sumwxy: got %v, want %v", got, want)
	}
}

func TestAddH2DPanics(t *testing.T) {
	for _, tc := range []struct {
		h1, h2 *H2D
		panics error
	}{
		{
			h1:     NewH2D(10, 0, 10, 10, 0, 10),
			h2:     NewH2D(5, 0, 10, 10, 0, 10),
			panics: fmt.Errorf("hbook: h1 and h2 have different number of bins"),
		},
		{
			h1:     NewH2D(10, 0, 10, 10, 0, 10),
			h2:     NewH2D(10, 1, 10, 10, 0, 10),
			panics: fmt.Errorf("hbook: h1 and h2 have different range"),
		},
		{
			h1:     NewH2D(10, 0, 10, 10, 0, 10),
			h2:     NewH2D(10, 0, 10, 10, 0, 11),
			panics: fmt.Errorf("hbook: h1 and h2 have different range"),
		},
	} {
		t.Run("", func(t *testing.T) {
			defer func() {
				err := recover()
				if err == nil {
					t.Fatalf("expected a panic")
				}
				if got, want := err.(error).Error(), tc.panics.Error(); got != want {
					t.Fatalf("invalid panic message.\ngot= %v\nwant=%v", got, want)
				}
			}()
			_ = AddH2D(tc.h1, tc.h2)
		})
	}
}

func TestH2DClone(t *testing.T) {
	h := newAddH2DHist(addH2DFills[0])
	h.Ann["name"] = "orig"

	c := h.Clone()
	if !reflect.DeepEqual(c.Binning, h.Binning) {
		t.Fatal("clone differs from the original")
	}

	// the clone must not share state with the original
	h.Fill(1.5, 1.5, 3)
	h.Ann["name"] = "changed"
	if reflect.DeepEqual(c.Binning, h.Binning) {
		t.Error("filling the original reached the clone")
	}
	if got, want := c.Ann["name"], "orig"; got != want {
		t.Errorf("clone annotation: got %q, want %q", got, want)
	}
}
