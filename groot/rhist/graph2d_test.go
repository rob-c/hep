// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rhist

import (
	"reflect"
	"testing"

	"go-hep.org/x/hep/groot/internal/rtests"
	"go-hep.org/x/hep/groot/rbytes"
	"go-hep.org/x/hep/groot/rtypes"
)

func TestGraph2D(t *testing.T) {
	xs := []float64{1, 2, 3, 4}
	ys := []float64{10, 20, 30, 40}
	zs := []float64{100, 200, 300, 400}

	g, err := NewGraph2DFrom(xs, ys, zs)
	if err != nil {
		t.Fatalf("could not build TGraph2D: %+v", err)
	}
	g.SetName("g2d")
	g.SetTitle("a 2-dim graph")

	if got, want := g.Len(), 4; got != want {
		t.Fatalf("len: got=%d, want=%d", got, want)
	}
	for i := range xs {
		x, y, z := g.XYZ(i)
		if x != xs[i] || y != ys[i] || z != zs[i] {
			t.Fatalf("point %d: got=(%v,%v,%v), want=(%v,%v,%v)", i, x, y, z, xs[i], ys[i], zs[i])
		}
	}

	if _, err := NewGraph2DFrom(xs, ys, zs[:2]); err == nil {
		t.Fatal("expected an error for mismatched lengths")
	}
}

func TestGraph2DRoundTrip(t *testing.T) {
	g2d, err := NewGraph2DFrom(
		[]float64{1, 2, 3},
		[]float64{4, 5, 6},
		[]float64{7, 8, 9},
	)
	if err != nil {
		t.Fatalf("could not build TGraph2D: %+v", err)
	}
	g2d.SetName("g2d")
	g2d.SetTitle("a 2-dim graph")

	gerr := NewGraph2DErrors(3)
	gerr.SetName("g2de")
	for i := range 3 {
		gerr.SetXYZ(i, float64(i), float64(2*i), float64(3*i))
		gerr.SetXYZError(i, 0.1, 0.2, 0.3)
	}

	for _, tc := range []struct {
		name string
		want rtests.ROOTer
	}{
		{name: "TGraph2D", want: g2d},
		{name: "TGraph2DErrors", want: gerr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, want := tc.want.Class(), tc.name; got != want {
				t.Fatalf("class: got=%q, want=%q", got, want)
			}

			wbuf := rbytes.NewWBuffer(nil, nil, 0, nil)
			_, err := tc.want.MarshalROOT(wbuf)
			if err != nil {
				t.Fatalf("could not marshal: %+v", err)
			}

			obj := rtypes.Factory.Get(tc.name)().Interface().(rtests.ROOTer)
			rbuf := rbytes.NewRBuffer(wbuf.Bytes(), nil, 0, nil)
			err = obj.UnmarshalROOT(rbuf)
			if err != nil {
				t.Fatalf("could not unmarshal: %+v", err)
			}

			rw := rbytes.NewWBuffer(nil, nil, 0, nil)
			_, err = obj.MarshalROOT(rw)
			if err != nil {
				t.Fatalf("could not re-marshal: %+v", err)
			}
			if !reflect.DeepEqual(rw.Bytes(), wbuf.Bytes()) {
				t.Fatalf("round-trip lost bytes")
			}

			// and the values really came back.
			switch got := obj.(type) {
			case *Graph2D:
				if got.Len() != 3 {
					t.Fatalf("len: got=%d, want=3", got.Len())
				}
				x, y, z := got.XYZ(2)
				if x != 3 || y != 6 || z != 9 {
					t.Fatalf("point 2: got=(%v,%v,%v), want=(3,6,9)", x, y, z)
				}
				if got, want := got.Name(), "g2d"; got != want {
					t.Fatalf("name: got=%q, want=%q", got, want)
				}
			case *Graph2DErrors:
				if lo, hi := got.ZError(1); lo != 0.3 || hi != 0.3 {
					t.Fatalf("z-error: got=(%v,%v), want=(0.3,0.3)", lo, hi)
				}
			}
		})
	}
}

func TestProfile3DRoundTrip(t *testing.T) {
	p3d := newProfile3D()
	p3d.h3d = *NewH3DFrom(h3dFixture())
	p3d.binEntries.Data = []float64{1, 2, 3}
	p3d.binSumw2.Data = []float64{4, 5, 6}
	p3d.errMode = 1
	p3d.tmin = -1
	p3d.tmax = +1
	p3d.sumwt = 12
	p3d.sumwt2 = 34

	if got, want := p3d.Class(), "TProfile3D"; got != want {
		t.Fatalf("class: got=%q, want=%q", got, want)
	}

	wbuf := rbytes.NewWBuffer(nil, nil, 0, nil)
	_, err := p3d.MarshalROOT(wbuf)
	if err != nil {
		t.Fatalf("could not marshal: %+v", err)
	}

	obj := rtypes.Factory.Get("TProfile3D")().Interface().(rtests.ROOTer)
	rbuf := rbytes.NewRBuffer(wbuf.Bytes(), nil, 0, nil)
	err = obj.UnmarshalROOT(rbuf)
	if err != nil {
		t.Fatalf("could not unmarshal: %+v", err)
	}

	got := obj.(*Profile3D)
	if !reflect.DeepEqual(got.BinEntries(), p3d.BinEntries()) {
		t.Errorf("bin entries: got=%v, want=%v", got.BinEntries(), p3d.BinEntries())
	}
	if got.TMin() != p3d.TMin() || got.TMax() != p3d.TMax() {
		t.Errorf("t-range: got=(%v,%v), want=(%v,%v)", got.TMin(), got.TMax(), p3d.TMin(), p3d.TMax())
	}
	if got.sumwt != p3d.sumwt || got.sumwt2 != p3d.sumwt2 {
		t.Errorf("sumwt: got=(%v,%v), want=(%v,%v)", got.sumwt, got.sumwt2, p3d.sumwt, p3d.sumwt2)
	}

	rw := rbytes.NewWBuffer(nil, nil, 0, nil)
	_, err = got.MarshalROOT(rw)
	if err != nil {
		t.Fatalf("could not re-marshal: %+v", err)
	}
	if !reflect.DeepEqual(rw.Bytes(), wbuf.Bytes()) {
		t.Fatalf("round-trip lost bytes")
	}
}
