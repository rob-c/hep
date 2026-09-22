// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rdf_test

import (
	"math"
	"path/filepath"
	"testing"

	"go-hep.org/x/hep/groot"
	"go-hep.org/x/hep/groot/rtree"
	"go-hep.org/x/hep/groot/rtree/rdf"
)

// mktree writes a tree where x runs 0..99, y is -x and n is the entry number.
func mktree(t *testing.T) rtree.Tree {
	t.Helper()

	fname := filepath.Join(t.TempDir(), "tree.root")
	f, err := groot.Create(fname)
	if err != nil {
		t.Fatalf("could not create ROOT file: %+v", err)
	}

	var (
		x float64
		y float64
		n int32
	)
	w, err := rtree.NewWriter(f, "tree", []rtree.WriteVar{
		{Name: "x", Value: &x},
		{Name: "y", Value: &y},
		{Name: "n", Value: &n},
	})
	if err != nil {
		t.Fatalf("could not create tree writer: %+v", err)
	}
	for i := range 100 {
		x, y, n = float64(i), -float64(i), int32(i)
		if _, err := w.Write(); err != nil {
			t.Fatalf("could not write entry %d: %+v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("could not close tree writer: %+v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("could not close ROOT file: %+v", err)
	}

	r, err := groot.Open(fname)
	if err != nil {
		t.Fatalf("could not open ROOT file: %+v", err)
	}
	t.Cleanup(func() { r.Close() })

	obj, err := r.Get("tree")
	if err != nil {
		t.Fatalf("could not get the tree: %+v", err)
	}
	return obj.(rtree.Tree)
}

func TestCountAndFilter(t *testing.T) {
	tree := mktree(t)

	df := rdf.New(tree)
	all := df.Count()
	half := df.Filter("x >= 50").Count()
	few := df.Filter("x >= 50").Filter("x < 60").Count()

	if err := df.Run(); err != nil {
		t.Fatalf("could not run: %+v", err)
	}

	for _, tc := range []struct {
		name string
		got  int64
		want int64
	}{
		{"all", all.Value(), 100},
		{"half", half.Value(), 50},
		{"few", few.Value(), 10},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: got=%d, want=%d", tc.name, tc.got, tc.want)
		}
	}
}

// TestOnePass checks every result comes out of a single reading of the tree,
// which is the point of describing the work rather than looping.
func TestOnePass(t *testing.T) {
	tree := mktree(t)

	df := rdf.New(tree)
	var counts []*rdf.Result[int64]
	for range 10 {
		counts = append(counts, df.Count())
	}

	if err := df.Run(); err != nil {
		t.Fatalf("could not run: %+v", err)
	}
	for i, c := range counts {
		if got, want := c.Value(), int64(100); got != want {
			t.Errorf("count %d: got=%d, want=%d", i, got, want)
		}
	}

	// running again must not read anything twice.
	if err := df.Run(); err != nil {
		t.Fatalf("could not run again: %+v", err)
	}
	if got, want := counts[0].Value(), int64(100); got != want {
		t.Errorf("after a second Run: got=%d, want=%d", got, want)
	}
}

func TestDefine(t *testing.T) {
	tree := mktree(t)

	df := rdf.New(tree).Define("z", "2*x + 1")
	sum := df.Sum("z")
	mean := df.Mean("z")

	// a Define may build on an earlier one.
	df2 := df.Define("w", "z - 1")
	sumw := df2.Sum("w")

	if err := df.Run(); err != nil {
		t.Fatalf("could not run: %+v", err)
	}

	// sum over i of (2i+1) for i in 0..99 is 2*4950 + 100 = 10000.
	if got, want := sum.Value(), 10000.0; math.Abs(got-want) > 1e-9 {
		t.Errorf("sum: got=%v, want=%v", got, want)
	}
	if got, want := mean.Value(), 100.0; math.Abs(got-want) > 1e-9 {
		t.Errorf("mean: got=%v, want=%v", got, want)
	}
	if got, want := sumw.Value(), 9900.0; math.Abs(got-want) > 1e-9 {
		t.Errorf("sum of w: got=%v, want=%v", got, want)
	}
}

// TestDefineAfterFilter checks a column defined after a cut is computed only
// for the entries that passed it.
func TestDefineAfterFilter(t *testing.T) {
	tree := mktree(t)

	df := rdf.New(tree).Filter("x >= 90").Define("z", "x*x")
	sum := df.Sum("z")
	n := df.Count()

	if err := df.Run(); err != nil {
		t.Fatalf("could not run: %+v", err)
	}

	var want float64
	for i := 90; i < 100; i++ {
		want += float64(i * i)
	}
	if got := sum.Value(); math.Abs(got-want) > 1e-9 {
		t.Errorf("sum: got=%v, want=%v", got, want)
	}
	if got, want := n.Value(), int64(10); got != want {
		t.Errorf("count: got=%d, want=%d", got, want)
	}
}

func TestHisto(t *testing.T) {
	tree := mktree(t)

	df := rdf.New(tree)
	h1 := df.Histo1D("x", rdf.Bins(100, 0, 100))
	h2 := df.Histo2D("y:x", rdf.Bins(10, 0, 100), rdf.BinsY(10, -100, 0))
	hw := df.Histo1D("x", rdf.Bins(100, 0, 100), rdf.HWeight("2"))

	if err := df.Run(); err != nil {
		t.Fatalf("could not run: %+v", err)
	}

	if got, want := h1.Value().Entries(), int64(100); got != want {
		t.Errorf("h1 entries: got=%d, want=%d", got, want)
	}
	if got, want := h1.Value().XMean(), 49.5; math.Abs(got-want) > 1e-9 {
		t.Errorf("h1 mean: got=%v, want=%v", got, want)
	}

	// "y:x" is y against x, so the x axis holds x and the y axis holds -x.
	if got, want := h2.Value().XMean(), 49.5; math.Abs(got-want) > 1e-9 {
		t.Errorf("h2 x mean: got=%v, want=%v", got, want)
	}
	if got, want := h2.Value().YMean(), -49.5; math.Abs(got-want) > 1e-9 {
		t.Errorf("h2 y mean: got=%v, want=%v", got, want)
	}

	if got, want := hw.Value().SumW(), 200.0; math.Abs(got-want) > 1e-9 {
		t.Errorf("weighted sumw: got=%v, want=%v", got, want)
	}
}

// TestReport checks the cut flow says what each filter was given and kept.
func TestReport(t *testing.T) {
	tree := mktree(t)

	df := rdf.New(tree).
		Filter("x >= 50", "half").
		Filter("x < 60", "narrow")
	rep := df.Report()
	n := df.Count()

	if err := df.Run(); err != nil {
		t.Fatalf("could not run: %+v", err)
	}

	info := rep.Value()
	if got, want := len(info), 2; got != want {
		t.Fatalf("got %d cuts, want %d", got, want)
	}

	if got, want := info[0].Name, "half"; got != want {
		t.Errorf("cut 0 name: got=%q, want=%q", got, want)
	}
	if got, want := info[0].All, int64(100); got != want {
		t.Errorf("cut 0 saw %d entries, want %d", got, want)
	}
	if got, want := info[0].Pass, int64(50); got != want {
		t.Errorf("cut 0 passed %d entries, want %d", got, want)
	}
	if got, want := info[0].Eff(), 0.5; math.Abs(got-want) > 1e-9 {
		t.Errorf("cut 0 efficiency: got=%v, want=%v", got, want)
	}

	// the second cut only ever sees what the first let through.
	if got, want := info[1].All, int64(50); got != want {
		t.Errorf("cut 1 saw %d entries, want %d", got, want)
	}
	if got, want := info[1].Pass, int64(10); got != want {
		t.Errorf("cut 1 passed %d entries, want %d", got, want)
	}

	if got, want := n.Value(), int64(10); got != want {
		t.Errorf("count: got=%d, want=%d", got, want)
	}
}

// TestFramesAreImmutable checks deriving a frame leaves the one it came from
// alone, so that one tree can feed several analyses in one pass.
func TestFramesAreImmutable(t *testing.T) {
	tree := mktree(t)

	base := rdf.New(tree)
	all := base.Count()

	cut := base.Filter("x < 10")
	some := cut.Count()

	other := base.Filter("x >= 90")
	rest := other.Count()

	if err := base.Run(); err != nil {
		t.Fatalf("could not run: %+v", err)
	}

	if got, want := all.Value(), int64(100); got != want {
		t.Errorf("base: got=%d, want=%d", got, want)
	}
	if got, want := some.Value(), int64(10); got != want {
		t.Errorf("first branch: got=%d, want=%d", got, want)
	}
	if got, want := rest.Value(), int64(10); got != want {
		t.Errorf("second branch: got=%d, want=%d", got, want)
	}
}

func TestMinMax(t *testing.T) {
	tree := mktree(t)

	df := rdf.New(tree)
	mm := df.MinMax("x")

	if err := df.Run(); err != nil {
		t.Fatalf("could not run: %+v", err)
	}
	if got, want := mm.Value(), [2]float64{0, 99}; got != want {
		t.Errorf("got=%v, want=%v", got, want)
	}
}

// TestLazy checks a result can be taken without calling Run first.
func TestLazy(t *testing.T) {
	tree := mktree(t)

	n := rdf.New(tree).Filter("x < 25").Count()
	if got, want := n.Value(), int64(25); got != want {
		t.Errorf("got=%d, want=%d", got, want)
	}
}

func TestRange(t *testing.T) {
	tree := mktree(t)

	df := rdf.NewRange(tree, 10, 20)
	n := df.Count()
	mm := df.MinMax("x")

	if err := df.Run(); err != nil {
		t.Fatalf("could not run: %+v", err)
	}
	if got, want := n.Value(), int64(20); got != want {
		t.Errorf("count: got=%d, want=%d", got, want)
	}
	if got, want := mm.Value(), [2]float64{10, 29}; got != want {
		t.Errorf("range: got=%v, want=%v", got, want)
	}
}

func TestErrors(t *testing.T) {
	tree := mktree(t)

	t.Run("no such branch", func(t *testing.T) {
		df := rdf.New(tree).Filter("nosuch > 0")
		_ = df.Count()
		err := df.Run()
		if err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("bad expression", func(t *testing.T) {
		df := rdf.New(tree).Define("z", "x +")
		_ = df.Count()
		if err := df.Run(); err == nil {
			t.Fatal("expected an error")
		}
	})
}
