// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hbook

import (
	"math"
	"reflect"
	"testing"
)

func TestH3DFill(t *testing.T) {
	h := NewH3D(2, 0, 2, 2, 0, 2, 2, 0, 2)
	h.Fill(0.5, 0.5, 0.5, 1)
	h.Fill(1.5, 0.5, 0.5, 2)
	h.Fill(1.5, 1.5, 1.5, 3)

	if got, want := h.Entries(), int64(3); got != want {
		t.Fatalf("entries: got=%d, want=%d", got, want)
	}
	if got, want := h.SumW(), 6.0; got != want {
		t.Fatalf("sumw: got=%v, want=%v", got, want)
	}
	if got, want := h.SumW2(), 1.0+4+9; got != want {
		t.Fatalf("sumw2: got=%v, want=%v", got, want)
	}

	for _, tc := range []struct {
		x, y, z float64
		want    float64
	}{
		{0.5, 0.5, 0.5, 1},
		{1.5, 0.5, 0.5, 2},
		{1.5, 1.5, 1.5, 3},
		{0.5, 1.5, 0.5, 0},
	} {
		bin := h.Bin(tc.x, tc.y, tc.z)
		if bin == nil {
			t.Fatalf("no bin at (%v,%v,%v)", tc.x, tc.y, tc.z)
		}
		if got := bin.SumW(); got != tc.want {
			t.Fatalf("bin (%v,%v,%v): got=%v, want=%v", tc.x, tc.y, tc.z, got, tc.want)
		}
	}

	// cross-terms: 1*(0.5*0.5) + 2*(1.5*0.5) + 3*(1.5*1.5)
	if got, want := h.SumWXY(), 0.25+1.5+6.75; got != want {
		t.Fatalf("sumwxy: got=%v, want=%v", got, want)
	}
}

// TestH3DOutflows checks that a point missing the binning lands in the one
// outflow that describes where it went, for all 26 of them.
func TestH3DOutflows(t *testing.T) {
	// a point on each axis: below the range, inside it, above it.
	pos := map[int]float64{-1: -10, 0: 0.5, +1: +10}

	n := 0
	for sx := -1; sx <= +1; sx++ {
		for sy := -1; sy <= +1; sy++ {
			for sz := -1; sz <= +1; sz++ {
				if sx == 0 && sy == 0 && sz == 0 {
					continue // that is the binning, not an outflow
				}
				n++

				h := NewH3D(1, 0, 1, 1, 0, 1, 1, 0, 1)
				h.Fill(pos[sx], pos[sy], pos[sz], 1)

				want := outflow3D(sx, sy, sz)
				for i := range h.Binning.Outflows {
					got := h.Binning.Outflows[i].SumW()
					switch i {
					case want:
						if got != 1 {
							t.Fatalf("(%+d,%+d,%+d): outflow %d holds %v, want 1", sx, sy, sz, i, got)
						}
					default:
						if got != 0 {
							t.Fatalf("(%+d,%+d,%+d): outflow %d holds %v, want 0", sx, sy, sz, i, got)
						}
					}
				}

				// an outflow is not a bin.
				if bin := h.Bin(pos[sx], pos[sy], pos[sz]); bin != nil {
					t.Fatalf("(%+d,%+d,%+d): got a bin for an outflow point", sx, sy, sz)
				}
				// the overall distribution counts it all the same.
				if got, want := h.SumW(), 1.0; got != want {
					t.Fatalf("(%+d,%+d,%+d): sumw got=%v, want=%v", sx, sy, sz, got, want)
				}
			}
		}
	}

	if n != NumOutflows3D {
		t.Fatalf("covered %d outflows, want %d", n, NumOutflows3D)
	}
}

// TestOutflow3DIsABijection checks the 26 sign-triples map onto the 26
// outflow slots one for one.
func TestOutflow3DIsABijection(t *testing.T) {
	seen := make(map[int]struct{}, NumOutflows3D)
	for sx := -1; sx <= +1; sx++ {
		for sy := -1; sy <= +1; sy++ {
			for sz := -1; sz <= +1; sz++ {
				if sx == 0 && sy == 0 && sz == 0 {
					continue
				}
				i := outflow3D(sx, sy, sz)
				if i < 0 || i >= NumOutflows3D {
					t.Fatalf("(%+d,%+d,%+d): index %d out of range", sx, sy, sz, i)
				}
				if _, dup := seen[i]; dup {
					t.Fatalf("(%+d,%+d,%+d): index %d already used", sx, sy, sz, i)
				}
				seen[i] = struct{}{}
			}
		}
	}
	if len(seen) != NumOutflows3D {
		t.Fatalf("covered %d slots, want %d", len(seen), NumOutflows3D)
	}
}

func TestH3DFromEdges(t *testing.T) {
	h := NewH3DFromEdges(
		[]float64{0, 1, 3},
		[]float64{0, 2, 4},
		[]float64{0, 5},
	)
	if got, want := len(h.Binning.Bins), 2*2*1; got != want {
		t.Fatalf("nbins: got=%d, want=%d", got, want)
	}
	h.Fill(2, 3, 1, 1)
	bin := h.Bin(2, 3, 1)
	if bin == nil {
		t.Fatal("no bin")
	}
	if got, want := bin.SumW(), 1.0; got != want {
		t.Fatalf("sumw: got=%v, want=%v", got, want)
	}
	if got, want := [2]float64{bin.XMin(), bin.XMax()}, [2]float64{1, 3}; got != want {
		t.Fatalf("x-range: got=%v, want=%v", got, want)
	}
	if got, want := [2]float64{bin.YMin(), bin.YMax()}, [2]float64{2, 4}; got != want {
		t.Fatalf("y-range: got=%v, want=%v", got, want)
	}
	if got, want := [2]float64{bin.ZMin(), bin.ZMax()}, [2]float64{0, 5}; got != want {
		t.Fatalf("z-range: got=%v, want=%v", got, want)
	}
}

func TestH3DFillN(t *testing.T) {
	h1 := NewH3D(2, 0, 2, 2, 0, 2, 2, 0, 2)
	h1.FillN([]float64{0.5, 1.5}, []float64{0.5, 1.5}, []float64{0.5, 1.5}, []float64{2, 3})

	h2 := NewH3D(2, 0, 2, 2, 0, 2, 2, 0, 2)
	h2.Fill(0.5, 0.5, 0.5, 2)
	h2.Fill(1.5, 1.5, 1.5, 3)

	if !reflect.DeepEqual(h1.Binning, h2.Binning) {
		t.Fatal("FillN and Fill disagree")
	}

	// weights default to 1.
	h3 := NewH3D(1, 0, 1, 1, 0, 1, 1, 0, 1)
	h3.FillN([]float64{0.5, 0.5}, []float64{0.5, 0.5}, []float64{0.5, 0.5}, nil)
	if got, want := h3.SumW(), 2.0; got != want {
		t.Fatalf("sumw: got=%v, want=%v", got, want)
	}
}

func TestH3DClone(t *testing.T) {
	h := NewH3D(2, 0, 2, 2, 0, 2, 2, 0, 2)
	h.Ann["name"] = "h3"
	h.Fill(0.5, 0.5, 0.5, 1)

	o := h.Clone()
	o.Fill(1.5, 1.5, 1.5, 1)
	o.Ann["name"] = "clone"

	if got, want := h.Name(), "h3"; got != want {
		t.Fatalf("clone shares annotations: got=%q, want=%q", got, want)
	}
	if got, want := h.Entries(), int64(1); got != want {
		t.Fatalf("clone shares bins: got=%d, want=%d", got, want)
	}
	if got, want := o.Entries(), int64(2); got != want {
		t.Fatalf("clone: got=%d, want=%d", got, want)
	}
}

func TestH3DBrioRoundTrip(t *testing.T) {
	h := NewH3D(2, 0, 2, 3, 0, 3, 2, -1, 1)
	h.Ann["name"] = "h3d"
	h.Ann["title"] = "a 3-dim histogram"
	h.Fill(0.5, 0.5, -0.5, 1)
	h.Fill(1.5, 2.5, +0.5, 2)
	h.Fill(-1, -1, -1, 3) // an outflow
	h.Fill(+9, +9, +9, 4) // another one

	raw, err := h.MarshalBinary()
	if err != nil {
		t.Fatalf("could not marshal: %+v", err)
	}

	var o H3D
	err = o.UnmarshalBinary(raw)
	if err != nil {
		t.Fatalf("could not unmarshal: %+v", err)
	}

	if got, want := o.Entries(), h.Entries(); got != want {
		t.Fatalf("entries: got=%d, want=%d", got, want)
	}
	if got, want := o.SumW(), h.SumW(); got != want {
		t.Fatalf("sumw: got=%v, want=%v", got, want)
	}
	if !reflect.DeepEqual(o.Binning, h.Binning) {
		t.Fatal("binning did not survive the round-trip")
	}
	if got, want := o.Name(), h.Name(); got != want {
		t.Fatalf("name: got=%q, want=%q", got, want)
	}
}

func TestH3DMoments(t *testing.T) {
	h := NewH3D(4, 0, 4, 4, 0, 4, 4, 0, 4)
	for _, p := range [][3]float64{{0.5, 1.5, 2.5}, {1.5, 2.5, 3.5}, {2.5, 3.5, 0.5}} {
		h.Fill(p[0], p[1], p[2], 1)
	}

	for _, tc := range []struct {
		name string
		got  float64
		want float64
	}{
		{"xmean", h.XMean(), (0.5 + 1.5 + 2.5) / 3},
		{"ymean", h.YMean(), (1.5 + 2.5 + 3.5) / 3},
		{"zmean", h.ZMean(), (2.5 + 3.5 + 0.5) / 3},
	} {
		if math.Abs(tc.got-tc.want) > 1e-12 {
			t.Fatalf("%s: got=%v, want=%v", tc.name, tc.got, tc.want)
		}
	}

	if got, want := h.Rank(), 3; got != want {
		t.Fatalf("rank: got=%d, want=%d", got, want)
	}
	if got, want := h.Integral(), 3.0; got != want {
		t.Fatalf("integral: got=%v, want=%v", got, want)
	}
}
