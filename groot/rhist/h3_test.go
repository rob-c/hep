// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rhist

import (
	"math"
	"testing"

	"go-hep.org/x/hep/hbook"
)

// h3dFixture builds an hbook 3-dim histogram with a distinct weight in every
// bin and in every one of the 26 outflows, so nothing can be confused with
// anything else on the way to ROOT and back.
func h3dFixture() *hbook.H3D {
	h := hbook.NewH3D(3, 0, 3, 2, 0, 2, 2, 0, 2)
	h.Ann["name"] = "h3"
	h.Ann["title"] = "a 3-dim histogram"

	w := 1.0
	for ix := range 3 {
		for iy := range 2 {
			for iz := range 2 {
				h.Fill(float64(ix)+0.5, float64(iy)+0.5, float64(iz)+0.5, w)
				w++
			}
		}
	}

	// and one entry in each outflow, each with its own weight.
	pos := map[int]float64{-1: -10, 0: 0.5, +1: +10}
	for sx := -1; sx <= +1; sx++ {
		for sy := -1; sy <= +1; sy++ {
			for sz := -1; sz <= +1; sz++ {
				if sx == 0 && sy == 0 && sz == 0 {
					continue
				}
				h.Fill(pos[sx], pos[sy], pos[sz], w)
				w++
			}
		}
	}
	return h
}

// TestH3DBridge takes an hbook 3-dim histogram through ROOT's TH3D and back,
// and checks every bin, every outflow and every moment came back unchanged.
func TestH3DBridge(t *testing.T) {
	want := h3dFixture()
	got := NewH3DFrom(want).AsH3D()

	if got, want := got.Name(), want.Name(); got != want {
		t.Errorf("name: got=%q, want=%q", got, want)
	}

	for i := range want.Binning.Bins {
		var (
			w = want.Binning.Bins[i]
			g = got.Binning.Bins[i]
		)
		if g.SumW() != w.SumW() {
			t.Errorf("bin %d: sumw got=%v, want=%v", i, g.SumW(), w.SumW())
		}
		if g.SumW2() != w.SumW2() {
			t.Errorf("bin %d: sumw2 got=%v, want=%v", i, g.SumW2(), w.SumW2())
		}
		if g.XRange != w.XRange || g.YRange != w.YRange || g.ZRange != w.ZRange {
			t.Errorf("bin %d: ranges got=%v/%v/%v, want=%v/%v/%v",
				i, g.XRange, g.YRange, g.ZRange, w.XRange, w.YRange, w.ZRange,
			)
		}
	}

	for i := range want.Binning.Outflows {
		var (
			w = want.Binning.Outflows[i]
			g = got.Binning.Outflows[i]
		)
		if g.SumW() != w.SumW() {
			t.Errorf("outflow %d: sumw got=%v, want=%v", i, g.SumW(), w.SumW())
		}
		if g.SumW2() != w.SumW2() {
			t.Errorf("outflow %d: sumw2 got=%v, want=%v", i, g.SumW2(), w.SumW2())
		}
	}

	for _, tc := range []struct {
		name     string
		got, wnt float64
	}{
		{"sumw", got.SumW(), want.SumW()},
		{"sumw2", got.SumW2(), want.SumW2()},
		{"sumwx", got.SumWX(), want.SumWX()},
		{"sumwx2", got.SumWX2(), want.SumWX2()},
		{"sumwy", got.SumWY(), want.SumWY()},
		{"sumwy2", got.SumWY2(), want.SumWY2()},
		{"sumwz", got.SumWZ(), want.SumWZ()},
		{"sumwz2", got.SumWZ2(), want.SumWZ2()},
		{"sumwxy", got.SumWXY(), want.SumWXY()},
		{"sumwxz", got.SumWXZ(), want.SumWXZ()},
		{"sumwyz", got.SumWYZ(), want.SumWYZ()},
	} {
		if math.Abs(tc.got-tc.wnt) > 1e-9 {
			t.Errorf("%s: got=%v, want=%v", tc.name, tc.got, tc.wnt)
		}
	}
}

// TestH3Cells checks the ROOT cell a bin index triple maps to, which is what
// decides whether a TH3 written by groot can be read by anything else.
func TestH3Cells(t *testing.T) {
	h := NewH3DFrom(hbook.NewH3D(3, 0, 3, 4, 0, 4, 5, 0, 5))

	// ROOT lays a TH3 out as ix + (nx+2)*(iy + (ny+2)*iz), counting the
	// under- and overflow cell on each axis.
	for _, tc := range []struct {
		ix, iy, iz int
		want       int
	}{
		{0, 0, 0, 0},
		{1, 0, 0, 1},
		{0, 1, 0, 5},
		{0, 0, 1, 30},
		{1, 1, 1, 36},
		// clamped to the overflow cell on each axis: 4 + 5*(5 + 6*6).
		{4, 5, 6, 209},
		{9, 9, 9, 209},
	} {
		if got := h.bin(tc.ix, tc.iy, tc.iz); got != tc.want {
			t.Errorf("bin(%d,%d,%d): got=%d, want=%d", tc.ix, tc.iy, tc.iz, got, tc.want)
		}
	}

	if got, want := len(h.arr.Data), 5*6*7; got != want {
		t.Errorf("ncells: got=%d, want=%d", got, want)
	}
	if got, want := h.th1.ncells, 5*6*7; got != want {
		t.Errorf("fNcells: got=%d, want=%d", got, want)
	}
}

// TestH3Merge checks two TH3s add up the way their hbook counterparts do.
func TestH3Merge(t *testing.T) {
	h1 := NewH3DFrom(h3dFixture())
	h2 := NewH3DFrom(h3dFixture())

	err := h1.ROOTMerge(h2)
	if err != nil {
		t.Fatalf("could not merge: %+v", err)
	}

	if got, want := h1.SumW(), 2*NewH3DFrom(h3dFixture()).SumW(); math.Abs(got-want) > 1e-9 {
		t.Fatalf("sumw after merge: got=%v, want=%v", got, want)
	}
}
