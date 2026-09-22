// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rdraw_test

import (
	"math"
	"path/filepath"
	"testing"

	"go-hep.org/x/hep/groot"
	"go-hep.org/x/hep/groot/rtree"
	"go-hep.org/x/hep/groot/rtree/rdraw"
)

// mktree writes a tree to fit the tests around: x runs 0..99, y is -x, n is
// the entry number as an int32, and ok says whether the entry is even.
func mktree(t *testing.T) string {
	t.Helper()

	fname := filepath.Join(t.TempDir(), "tree.root")
	f, err := groot.Create(fname)
	if err != nil {
		t.Fatalf("could not create ROOT file: %+v", err)
	}

	var (
		x  float64
		y  float64
		n  int32
		ok bool
	)
	w, err := rtree.NewWriter(f, "tree", []rtree.WriteVar{
		{Name: "x", Value: &x},
		{Name: "y", Value: &y},
		{Name: "n", Value: &n},
		{Name: "ok", Value: &ok},
	})
	if err != nil {
		t.Fatalf("could not create tree writer: %+v", err)
	}

	for i := range 100 {
		x = float64(i)
		y = -x
		n = int32(i)
		ok = i%2 == 0
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

	return fname
}

func open(t *testing.T, fname string) (rtree.Tree, func()) {
	t.Helper()

	f, err := groot.Open(fname)
	if err != nil {
		t.Fatalf("could not open ROOT file: %+v", err)
	}
	obj, err := f.Get("tree")
	if err != nil {
		t.Fatalf("could not get the tree: %+v", err)
	}
	return obj.(rtree.Tree), func() { f.Close() }
}

func TestH1D(t *testing.T) {
	tree, done := open(t, mktree(t))
	defer done()

	for _, tc := range []struct {
		name    string
		expr    string
		opts    []rdraw.Option
		entries int64
		sumw    float64
		mean    float64
	}{
		{
			name: "plain", expr: "x",
			opts:    []rdraw.Option{rdraw.Bins(100, 0, 100)},
			entries: 100, sumw: 100, mean: 49.5,
		},
		{
			name: "arithmetic", expr: "2*x",
			opts:    []rdraw.Option{rdraw.Bins(100, 0, 200)},
			entries: 100, sumw: 100, mean: 99,
		},
		{
			name: "cut", expr: "x",
			opts:    []rdraw.Option{rdraw.Bins(100, 0, 100), rdraw.Cut("x >= 50")},
			entries: 50, sumw: 50, mean: 74.5,
		},
		{
			name: "compound cut", expr: "x",
			opts:    []rdraw.Option{rdraw.Bins(100, 0, 100), rdraw.Cut("x >= 20 && x < 30")},
			entries: 10, sumw: 10, mean: 24.5,
		},
		{
			name: "bool branch", expr: "x",
			opts:    []rdraw.Option{rdraw.Bins(100, 0, 100), rdraw.Cut("ok")},
			entries: 50, sumw: 50, mean: 49,
		},
		{
			name: "int branch", expr: "n",
			opts:    []rdraw.Option{rdraw.Bins(100, 0, 100)},
			entries: 100, sumw: 100, mean: 49.5,
		},
		{
			name: "function", expr: "abs(y)",
			opts:    []rdraw.Option{rdraw.Bins(100, 0, 100)},
			entries: 100, sumw: 100, mean: 49.5,
		},
		{
			name: "ROOT spelling", expr: "TMath::Abs(y)",
			opts:    []rdraw.Option{rdraw.Bins(100, 0, 100)},
			entries: 100, sumw: 100, mean: 49.5,
		},
		{
			name: "weight", expr: "x",
			opts:    []rdraw.Option{rdraw.Bins(100, 0, 100), rdraw.Weight("2")},
			entries: 100, sumw: 200, mean: 49.5,
		},
		{
			name: "range", expr: "x",
			opts:    []rdraw.Option{rdraw.Bins(100, 0, 100), rdraw.Range(0, 10)},
			entries: 10, sumw: 10, mean: 4.5,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, err := rdraw.H1D(tree, tc.expr, tc.opts...)
			if err != nil {
				t.Fatalf("could not draw %q: %+v", tc.expr, err)
			}
			if got := h.Entries(); got != tc.entries {
				t.Errorf("entries: got=%d, want=%d", got, tc.entries)
			}
			if got := h.SumW(); math.Abs(got-tc.sumw) > 1e-9 {
				t.Errorf("sumw: got=%v, want=%v", got, tc.sumw)
			}
			if got := h.XMean(); math.Abs(got-tc.mean) > 1e-9 {
				t.Errorf("mean: got=%v, want=%v", got, tc.mean)
			}
		})
	}
}

// TestAxisOrder checks "y:x" puts y on the vertical axis, as ROOT does.
func TestAxisOrder(t *testing.T) {
	tree, done := open(t, mktree(t))
	defer done()

	// y is -x, so if the axes are the right way round the x axis runs 0..99
	// and the y axis runs -99..0.
	h, err := rdraw.H2D(tree, "y:x", rdraw.Bins(10, 0, 100), rdraw.BinsY(10, -100, 0))
	if err != nil {
		t.Fatalf("could not draw: %+v", err)
	}

	if got, want := h.Entries(), int64(100); got != want {
		t.Fatalf("entries: got=%d, want=%d", got, want)
	}
	if got, want := h.XMean(), 49.5; math.Abs(got-want) > 1e-9 {
		t.Errorf("x mean: got=%v, want=%v", got, want)
	}
	if got, want := h.YMean(), -49.5; math.Abs(got-want) > 1e-9 {
		t.Errorf("y mean: got=%v, want=%v", got, want)
	}
}

func TestH3D(t *testing.T) {
	tree, done := open(t, mktree(t))
	defer done()

	h, err := rdraw.H3D(tree, "n:y:x",
		rdraw.Bins(10, 0, 100), rdraw.BinsY(10, -100, 0), rdraw.BinsZ(10, 0, 100),
	)
	if err != nil {
		t.Fatalf("could not draw: %+v", err)
	}
	if got, want := h.Entries(), int64(100); got != want {
		t.Fatalf("entries: got=%d, want=%d", got, want)
	}
	if got, want := h.ZMean(), 49.5; math.Abs(got-want) > 1e-9 {
		t.Errorf("z mean: got=%v, want=%v", got, want)
	}
}

// TestAutoBinning checks a Draw given no binning finds one that holds
// everything it drew.
func TestAutoBinning(t *testing.T) {
	tree, done := open(t, mktree(t))
	defer done()

	h, err := rdraw.H1D(tree, "x")
	if err != nil {
		t.Fatalf("could not draw: %+v", err)
	}

	if got, want := h.Entries(), int64(100); got != want {
		t.Fatalf("entries: got=%d, want=%d", got, want)
	}
	// nothing may have fallen outside the range it chose.
	if got, want := h.SumW(), 100.0; got != want {
		t.Errorf("sumw: got=%v, want=%v", got, want)
	}
	if h.XMin() > 0 || h.XMax() < 99 {
		t.Errorf("range [%v, %v] does not hold the data [0, 99]", h.XMin(), h.XMax())
	}
}

// TestDrawRank checks Draw picks its rank from the expression.
func TestDrawRank(t *testing.T) {
	tree, done := open(t, mktree(t))
	defer done()

	for _, tc := range []struct {
		expr string
		want string
	}{
		{"x", "*hbook.H1D"},
		{"y:x", "*hbook.H2D"},
		{"n:y:x", "*hbook.H3D"},
	} {
		h, err := rdraw.Draw(tree, tc.expr)
		if err != nil {
			t.Fatalf("could not draw %q: %+v", tc.expr, err)
		}
		if got := typeName(h); got != tc.want {
			t.Errorf("%q: got=%s, want=%s", tc.expr, got, tc.want)
		}
	}
}

func typeName(v any) string {
	return fmtType(v)
}

func TestErrors(t *testing.T) {
	tree, done := open(t, mktree(t))
	defer done()

	for _, tc := range []struct {
		name string
		expr string
		opts []rdraw.Option
		want string
	}{
		{"no such branch", "nosuch", nil, `has no branch "nosuch"`},
		{"unknown function", "nosuchfct(x)", nil, `unknown function "nosuchfct"`},
		{"bad syntax", "x +", nil, "could not parse"},
		{"bad cut", "x", []rdraw.Option{rdraw.Cut("x +")}, "bad cut"},
		{"too many axes", "x:y:n:x", nil, "want 1, 2 or 3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := rdraw.Draw(tree, tc.expr, tc.opts...)
			if err == nil {
				t.Fatalf("expected an error for %q", tc.expr)
			}
			if !contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}
