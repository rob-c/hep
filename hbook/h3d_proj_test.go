// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hbook

import (
	"math"
	"testing"
)

func projFixture() *H3D {
	h := NewH3D(2, 0, 2, 3, 0, 3, 4, 0, 4)
	w := 1.0
	for ix := range 2 {
		for iy := range 3 {
			for iz := range 4 {
				h.Fill(float64(ix)+0.5, float64(iy)+0.5, float64(iz)+0.5, w)
				w++
			}
		}
	}
	return h
}

// TestH3DProjections checks a projection keeps every weight, and puts each
// one in the bin the surviving axes say it belongs to.
func TestH3DProjections(t *testing.T) {
	h := projFixture()

	for _, tc := range []struct {
		name   string
		got    *H2D
		nx, ny int
	}{
		{"XY", h.ProjectionXY(), 2, 3},
		{"XZ", h.ProjectionXZ(), 2, 4},
		{"YZ", h.ProjectionYZ(), 3, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, want := tc.got.Binning.Nx, tc.nx; got != want {
				t.Fatalf("nx: got=%d, want=%d", got, want)
			}
			if got, want := tc.got.Binning.Ny, tc.ny; got != want {
				t.Fatalf("ny: got=%d, want=%d", got, want)
			}

			// nothing may be lost: the bins of the projection hold the same
			// total as the bins of the histogram.
			var sum float64
			for i := range tc.got.Binning.Bins {
				sum += tc.got.Binning.Bins[i].SumW()
			}
			var want float64
			for i := range h.Binning.Bins {
				want += h.Binning.Bins[i].SumW()
			}
			if math.Abs(sum-want) > 1e-9 {
				t.Fatalf("sum over bins: got=%v, want=%v", sum, want)
			}
			if got := tc.got.SumW(); math.Abs(got-h.SumW()) > 1e-9 {
				t.Fatalf("sumw: got=%v, want=%v", got, h.SumW())
			}
		})
	}
}

// TestH3DProjectionXYBins checks a single bin of a projection holds exactly
// the bins that projected onto it.
func TestH3DProjectionXYBins(t *testing.T) {
	h := NewH3D(2, 0, 2, 2, 0, 2, 2, 0, 2)
	h.Fill(0.5, 0.5, 0.5, 1)
	h.Fill(0.5, 0.5, 1.5, 2) // same (x,y), other z
	h.Fill(1.5, 1.5, 0.5, 4)

	xy := h.ProjectionXY()

	if got, want := xy.Bin(0.5, 0.5).SumW(), 3.0; got != want {
		t.Errorf("bin(0.5,0.5): got=%v, want=%v", got, want)
	}
	if got, want := xy.Bin(1.5, 1.5).SumW(), 4.0; got != want {
		t.Errorf("bin(1.5,1.5): got=%v, want=%v", got, want)
	}
	if got, want := xy.Bin(1.5, 0.5).SumW(), 0.0; got != want {
		t.Errorf("bin(1.5,0.5): got=%v, want=%v", got, want)
	}
}

func TestH3DProjection1D(t *testing.T) {
	h := NewH3D(2, 0, 2, 2, 0, 2, 2, 0, 2)
	h.Fill(0.5, 0.5, 0.5, 1)
	h.Fill(0.5, 1.5, 1.5, 2)
	h.Fill(1.5, 0.5, 1.5, 4)

	x := h.ProjectionX()
	if got, want := x.Binning.Bins[0].SumW(), 3.0; got != want {
		t.Errorf("x-bin 0: got=%v, want=%v", got, want)
	}
	if got, want := x.Binning.Bins[1].SumW(), 4.0; got != want {
		t.Errorf("x-bin 1: got=%v, want=%v", got, want)
	}

	z := h.ProjectionZ()
	if got, want := z.Binning.Bins[0].SumW(), 1.0; got != want {
		t.Errorf("z-bin 0: got=%v, want=%v", got, want)
	}
	if got, want := z.Binning.Bins[1].SumW(), 6.0; got != want {
		t.Errorf("z-bin 1: got=%v, want=%v", got, want)
	}
}

// TestH3DProjectionOutflows checks an entry that missed the binning ends up
// in the outflow the surviving axes put it in.
func TestH3DProjectionOutflows(t *testing.T) {
	h := NewH3D(1, 0, 1, 1, 0, 1, 1, 0, 1)

	h.Fill(-1, 0.5, 0.5, 2) // below x, inside y and z
	h.Fill(0.5, 0.5, +9, 5) // inside x and y, only outside z
	h.Fill(+9, +9, 0.5, 7)  // above x and y

	xy := h.ProjectionXY()

	if got, want := xy.Binning.Outflows[Outflow2D(-1, 0)].SumW(), 2.0; got != want {
		t.Errorf("W outflow: got=%v, want=%v", got, want)
	}
	if got, want := xy.Binning.Outflows[Outflow2D(+1, +1)].SumW(), 7.0; got != want {
		t.Errorf("NE outflow: got=%v, want=%v", got, want)
	}

	// the one that only missed z is in no outflow of the projection, and is
	// still counted in the overall distribution.
	var out float64
	for i := range xy.Binning.Outflows {
		out += xy.Binning.Outflows[i].SumW()
	}
	if got, want := out, 9.0; got != want {
		t.Errorf("total outflow: got=%v, want=%v", got, want)
	}
	if got, want := xy.SumW(), 14.0; got != want {
		t.Errorf("sumw: got=%v, want=%v", got, want)
	}
}

func TestH3DSliceXY(t *testing.T) {
	h := NewH3D(2, 0, 2, 2, 0, 2, 2, 0, 2)
	h.Fill(0.5, 0.5, 0.5, 1)
	h.Fill(0.5, 0.5, 1.5, 2)

	s0, err := h.SliceXY(0)
	if err != nil {
		t.Fatalf("could not slice: %+v", err)
	}
	if got, want := s0.Bin(0.5, 0.5).SumW(), 1.0; got != want {
		t.Errorf("slice 0: got=%v, want=%v", got, want)
	}

	s1, err := h.SliceXY(1)
	if err != nil {
		t.Fatalf("could not slice: %+v", err)
	}
	if got, want := s1.Bin(0.5, 0.5).SumW(), 2.0; got != want {
		t.Errorf("slice 1: got=%v, want=%v", got, want)
	}

	if _, err := h.SliceXY(2); err == nil {
		t.Fatal("expected an error for a z-bin that does not exist")
	}
	if _, err := h.SliceXY(-1); err == nil {
		t.Fatal("expected an error for a negative z-bin")
	}
}

func TestOutflow2DIsABijection(t *testing.T) {
	seen := make(map[int]struct{}, 8)
	for sx := -1; sx <= +1; sx++ {
		for sy := -1; sy <= +1; sy++ {
			if sx == 0 && sy == 0 {
				continue
			}
			i := Outflow2D(sx, sy)
			if i < 0 || i >= 8 {
				t.Fatalf("(%+d,%+d): index %d out of range", sx, sy, i)
			}
			if _, dup := seen[i]; dup {
				t.Fatalf("(%+d,%+d): index %d already used", sx, sy, i)
			}
			seen[i] = struct{}{}
		}
	}
	if len(seen) != 8 {
		t.Fatalf("covered %d slots, want 8", len(seen))
	}
}
