// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package groot_test

import (
	"math"
	"path/filepath"
	"testing"

	"go-hep.org/x/hep/groot"
	"go-hep.org/x/hep/groot/rhist"
	"go-hep.org/x/hep/groot/root"
	"go-hep.org/x/hep/groot/rtree"
	"go-hep.org/x/hep/hbook"
)

// TestPureGoROOTFile writes a ROOT file holding a TTree, the three ranks of
// histogram, a function and a 2-dim graph, then reads every one of them back.
//
// Nothing here goes near a C++ ROOT installation: it is the whole of what
// groot can put in a file and take out of it again, in Go alone.
func TestPureGoROOTFile(t *testing.T) {
	fname := filepath.Join(t.TempDir(), "purego.root")

	type Event struct {
		N   int32
		X   float64
		Str string
		Sli []float64
	}

	const nevts = 100

	// what goes in.
	var (
		h1 = hbook.NewH1D(20, -4, 4)
		h2 = hbook.NewH2D(20, -4, 4, 20, -4, 4)
		h3 = hbook.NewH3D(10, -4, 4, 10, -4, 4, 10, -4, 4)
	)
	for i := range nevts {
		v := float64(i)/nevts*8 - 4
		h1.Fill(v, 1)
		h2.Fill(v, -v, 1)
		h3.Fill(v, -v, v/2, 1)
	}

	f1, err := rhist.NewF1("f1", "[0]*exp(-0.5*((x-[1])/[2])^2)", -4, 4)
	if err != nil {
		t.Fatalf("could not build TF1: %+v", err)
	}
	if err := f1.SetParams([]float64{2, 0, 1}); err != nil {
		t.Fatalf("could not set TF1 parameters: %+v", err)
	}

	g2d, err := rhist.NewGraph2DFrom(
		[]float64{1, 2, 3},
		[]float64{4, 5, 6},
		[]float64{7, 8, 9},
	)
	if err != nil {
		t.Fatalf("could not build TGraph2D: %+v", err)
	}

	func() {
		f, err := groot.Create(fname)
		if err != nil {
			t.Fatalf("could not create ROOT file: %+v", err)
		}
		defer f.Close()

		var evt Event
		tree, err := rtree.NewWriter(f, "tree", []rtree.WriteVar{
			{Name: "N", Value: &evt.N},
			{Name: "X", Value: &evt.X},
			{Name: "Str", Value: &evt.Str},
			{Name: "Sli", Value: &evt.Sli, Count: "N"},
		})
		if err != nil {
			t.Fatalf("could not create tree writer: %+v", err)
		}

		for i := range nevts {
			evt.N = int32(i % 3)
			evt.X = float64(i)
			evt.Str = "evt"
			evt.Sli = make([]float64, evt.N)
			for j := range evt.Sli {
				evt.Sli[j] = float64(i * j)
			}
			if _, err := tree.Write(); err != nil {
				t.Fatalf("could not write event %d: %+v", i, err)
			}
		}
		if err := tree.Close(); err != nil {
			t.Fatalf("could not close tree writer: %+v", err)
		}

		for k, v := range map[string]root.Object{
			"h1":  rhist.NewH1DFrom(h1),
			"h2":  rhist.NewH2DFrom(h2),
			"h3":  rhist.NewH3DFrom(h3),
			"f1":  f1,
			"g2d": g2d,
		} {
			if err := f.Put(k, v); err != nil {
				t.Fatalf("could not write %q: %+v", k, err)
			}
		}

		if err := f.Close(); err != nil {
			t.Fatalf("could not close ROOT file: %+v", err)
		}
	}()

	// and what comes out.
	r, err := groot.Open(fname)
	if err != nil {
		t.Fatalf("could not open ROOT file: %+v", err)
	}
	defer r.Close()

	t.Run("tree", func(t *testing.T) {
		obj, err := r.Get("tree")
		if err != nil {
			t.Fatalf("could not read tree: %+v", err)
		}
		tree := obj.(rtree.Tree)
		if got, want := tree.Entries(), int64(nevts); got != want {
			t.Fatalf("entries: got=%d, want=%d", got, want)
		}

		var evt Event
		rv := []rtree.ReadVar{
			{Name: "N", Value: &evt.N},
			{Name: "X", Value: &evt.X},
			{Name: "Str", Value: &evt.Str},
			{Name: "Sli", Value: &evt.Sli},
		}
		rr, err := rtree.NewReader(tree, rv)
		if err != nil {
			t.Fatalf("could not create tree reader: %+v", err)
		}
		defer rr.Close()

		n := 0
		err = rr.Read(func(rctx rtree.RCtx) error {
			i := int(rctx.Entry)
			if got, want := evt.X, float64(i); got != want {
				t.Fatalf("event %d: X got=%v, want=%v", i, got, want)
			}
			if got, want := evt.N, int32(i%3); got != want {
				t.Fatalf("event %d: N got=%v, want=%v", i, got, want)
			}
			if got, want := len(evt.Sli), int(evt.N); got != want {
				t.Fatalf("event %d: len(Sli) got=%d, want=%d", i, got, want)
			}
			if got, want := evt.Str, "evt"; got != want {
				t.Fatalf("event %d: Str got=%q, want=%q", i, got, want)
			}
			n++
			return nil
		})
		if err != nil {
			t.Fatalf("could not read tree: %+v", err)
		}
		if got, want := n, nevts; got != want {
			t.Fatalf("read %d events, want %d", got, want)
		}
	})

	t.Run("histos", func(t *testing.T) {
		for _, tc := range []struct {
			key   string
			class string
			sumw  float64
		}{
			{"h1", "TH1D", h1.SumW()},
			{"h2", "TH2D", h2.SumW()},
			{"h3", "TH3D", h3.SumW()},
		} {
			obj, err := r.Get(tc.key)
			if err != nil {
				t.Fatalf("could not read %q: %+v", tc.key, err)
			}
			if got, want := obj.Class(), tc.class; got != want {
				t.Fatalf("%q: class got=%q, want=%q", tc.key, got, want)
			}

			var sumw float64
			switch h := obj.(type) {
			case rhist.H1:
				sumw = h.SumW()
			case rhist.H2:
				sumw = h.SumW()
			case rhist.H3:
				sumw = h.SumW()
			default:
				t.Fatalf("%q: %T is no histogram", tc.key, obj)
			}
			if math.Abs(sumw-tc.sumw) > 1e-9 {
				t.Fatalf("%q: sumw got=%v, want=%v", tc.key, sumw, tc.sumw)
			}
		}
	})

	t.Run("func", func(t *testing.T) {
		obj, err := r.Get("f1")
		if err != nil {
			t.Fatalf("could not read f1: %+v", err)
		}
		fct, ok := obj.(*rhist.F1)
		if !ok {
			t.Fatalf("f1 is a %T, want a *rhist.F1", obj)
		}

		// it came back out of the file evaluable, parameters and all.
		eval, err := fct.Func()
		if err != nil {
			t.Fatalf("could not compile f1: %+v", err)
		}
		if got, want := eval(0), 2.0; math.Abs(got-want) > 1e-12 {
			t.Fatalf("f1(0): got=%v, want=%v", got, want)
		}
		if got, want := eval(1), 2*math.Exp(-0.5); math.Abs(got-want) > 1e-12 {
			t.Fatalf("f1(1): got=%v, want=%v", got, want)
		}
	})

	t.Run("graph2d", func(t *testing.T) {
		obj, err := r.Get("g2d")
		if err != nil {
			t.Fatalf("could not read g2d: %+v", err)
		}
		g, ok := obj.(*rhist.Graph2D)
		if !ok {
			t.Fatalf("g2d is a %T, want a *rhist.Graph2D", obj)
		}
		if got, want := g.Len(), 3; got != want {
			t.Fatalf("len: got=%d, want=%d", got, want)
		}
		x, y, z := g.XYZ(1)
		if x != 2 || y != 5 || z != 8 {
			t.Fatalf("point 1: got=(%v,%v,%v), want=(2,5,8)", x, y, z)
		}
	})
}

// TestWriteROOTWrittenTF1 writes back out a TF1 that C++ ROOT wrote.
//
// This used to fail at the point of closing the file: a TFormula holds its
// parameter names in a map<TString,int,TFormulaParamOrder>, and groot walked
// every template argument of a container looking for streamers, comparator
// included — and there is no streamer for a comparator.
func TestWriteROOTWrittenTF1(t *testing.T) {
	src, err := groot.Open("testdata/tformula.root")
	if err != nil {
		t.Fatalf("could not open source file: %+v", err)
	}
	defer src.Close()

	obj, err := src.Get("func1")
	if err != nil {
		t.Fatalf("could not read func1: %+v", err)
	}

	fname := filepath.Join(t.TempDir(), "f1.root")
	func() {
		f, err := groot.Create(fname)
		if err != nil {
			t.Fatalf("could not create ROOT file: %+v", err)
		}
		defer f.Close()

		err = f.Put("func1", obj)
		if err != nil {
			t.Fatalf("could not write func1: %+v", err)
		}

		err = f.Close()
		if err != nil {
			t.Fatalf("could not close ROOT file: %+v", err)
		}
	}()

	r, err := groot.Open(fname)
	if err != nil {
		t.Fatalf("could not reopen ROOT file: %+v", err)
	}
	defer r.Close()

	got, err := r.Get("func1")
	if err != nil {
		t.Fatalf("could not read back func1: %+v", err)
	}

	f1, ok := got.(*rhist.F1)
	if !ok {
		t.Fatalf("func1 is a %T, want a *rhist.F1", got)
	}

	// it is still the function ROOT wrote: f(x) = 10 + 20x.
	eval, err := f1.Func()
	if err != nil {
		t.Fatalf("could not compile func1: %+v", err)
	}
	for _, tc := range []struct{ x, want float64 }{{0, 10}, {1, 30}, {2, 50}} {
		if got := eval(tc.x); got != tc.want {
			t.Errorf("func1(%v): got=%v, want=%v", tc.x, got, tc.want)
		}
	}
}
