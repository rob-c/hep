// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rdf_test

import (
	"math"
	"strings"
	"testing"

	"go-hep.org/x/hep/groot"
	"go-hep.org/x/hep/groot/rtree"
	"go-hep.org/x/hep/groot/rtree/rdf"
)

// The tree these tests read holds, for its i-th of a hundred entries,
// Int32 = i, N = i%10, a slice of N copies of i and an array of ten copies
// of i.
const (
	nEntries = 100
	arrayLen = 10
)

func openTree(t *testing.T) (rtree.Tree, func()) {
	t.Helper()

	f, err := groot.Open("../../testdata/small-flat-tree.root")
	if err != nil {
		t.Fatalf("could not open file: %+v", err)
	}
	o, err := f.Get("tree")
	if err != nil {
		f.Close()
		t.Fatalf("could not get tree: %+v", err)
	}
	return o.(rtree.Tree), func() { f.Close() }
}

// wantSlice returns how many elements the slice branch holds across the
// tree and what they add up to.
func wantSlice() (n int, sum float64) {
	for i := range nEntries {
		k := i % 10
		n += k
		sum += float64(k * i)
	}
	return n, sum
}

// TestSumOverCollection checks that an action over a collection runs once
// per element.
func TestSumOverCollection(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	df := rdf.New(tree)
	var (
		sumArr   = df.Sum("ArrayFloat64")
		sumSlice = df.Sum("SliceFloat64")
		count    = df.Count()
	)

	if err := df.Run(); err != nil {
		t.Fatalf("could not run: %+v", err)
	}

	var wantArr float64
	for i := range nEntries {
		wantArr += float64(arrayLen * i)
	}
	if got := sumArr.Value(); math.Abs(got-wantArr) > 1e-9 {
		t.Errorf("sum over the array: got=%v, want=%v", got, wantArr)
	}

	_, wantSl := wantSlice()
	if got := sumSlice.Value(); math.Abs(got-wantSl) > 1e-9 {
		t.Errorf("sum over the slice: got=%v, want=%v", got, wantSl)
	}

	// Count is about entries, whatever else the frame is looping over.
	if got, want := count.Value(), int64(nEntries); got != want {
		t.Errorf("count: got=%d, want=%d", got, want)
	}
}

// TestHistoOverCollection checks that a histogram over a collection takes
// one fill per element.
func TestHistoOverCollection(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	df := rdf.New(tree)
	h := df.Histo1D("SliceFloat64", rdf.Bins(100, -1, 100))

	if err := df.Run(); err != nil {
		t.Fatalf("could not run: %+v", err)
	}

	n, sum := wantSlice()
	if got, want := h.Value().Entries(), int64(n); got != want {
		t.Errorf("entries: got=%d, want=%d", got, want)
	}
	if got, want := h.Value().XMean(), sum/float64(n); math.Abs(got-want) > 1e-9 {
		t.Errorf("mean: got=%v, want=%v", got, want)
	}
}

// TestDefineCollection checks that a defined column may be a collection,
// and behaves like a branch holding one.
func TestDefineCollection(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	df := rdf.New(tree).Define("twice", "ArrayFloat64 * 2")
	var (
		sum = df.Sum("twice")
		h   = df.Histo1D("twice", rdf.Bins(100, -1, 200))
	)

	if err := df.Run(); err != nil {
		t.Fatalf("could not run: %+v", err)
	}

	var want float64
	for i := range nEntries {
		want += float64(2 * arrayLen * i)
	}
	if got := sum.Value(); math.Abs(got-want) > 1e-9 {
		t.Errorf("sum: got=%v, want=%v", got, want)
	}
	if got, want := h.Value().Entries(), int64(nEntries*arrayLen); got != want {
		t.Errorf("entries: got=%d, want=%d", got, want)
	}
}

// TestDefineReducedFromCollection checks that a column defined by reducing
// a collection is a single value per entry.
func TestDefineReducedFromCollection(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	df := rdf.New(tree).Define("n", "Length$(SliceFloat64)")
	var (
		sum   = df.Sum("n")
		count = df.Count()
	)

	if err := df.Run(); err != nil {
		t.Fatalf("could not run: %+v", err)
	}

	n, _ := wantSlice()
	if got, want := sum.Value(), float64(n); math.Abs(got-want) > 1e-9 {
		t.Errorf("sum of lengths: got=%v, want=%v", got, want)
	}
	if got, want := count.Value(), int64(nEntries); got != want {
		t.Errorf("count: got=%d, want=%d", got, want)
	}
}

// TestFilterOnTheEntry checks that a filter reducing a collection selects
// entries, and that the cut flow counts entries.
func TestFilterOnTheEntry(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	df := rdf.New(tree).Filter("Length$(SliceFloat64) >= 5", "at least five")
	var (
		count = df.Count()
		sum   = df.Sum("SliceFloat64")
		rep   = df.Report()
	)

	if err := df.Run(); err != nil {
		t.Fatalf("could not run: %+v", err)
	}

	var (
		wantEntries int64
		wantSum     float64
	)
	for i := range nEntries {
		if k := i % 10; k >= 5 {
			wantEntries++
			wantSum += float64(k * i)
		}
	}

	if got := count.Value(); got != wantEntries {
		t.Errorf("count: got=%d, want=%d", got, wantEntries)
	}
	if got := sum.Value(); math.Abs(got-wantSum) > 1e-9 {
		t.Errorf("sum: got=%v, want=%v", got, wantSum)
	}

	info := rep.Value()
	if len(info) != 1 {
		t.Fatalf("got %d cut(s), want 1", len(info))
	}
	if got, want := info[0].All, int64(nEntries); got != want {
		t.Errorf("cut saw %d entries, want %d", got, want)
	}
	if got := info[0].Pass; got != wantEntries {
		t.Errorf("cut passed %d entries, want %d", got, wantEntries)
	}
}

// TestFilterOverCollectionIsRefused checks that a filter which does not
// answer once for the entry is reported, with a pointer at what to do
// instead, rather than quietly keeping or dropping the wrong thing.
func TestFilterOverCollectionIsRefused(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	df := rdf.New(tree).Filter("SliceFloat64 > 5")
	_ = df.Count()

	err := df.Run()
	if err == nil {
		t.Fatal("a filter over a collection was accepted")
	}
	if got := err.Error(); !strings.Contains(got, "Length$") {
		t.Errorf("the error should say what to do instead, got: %v", got)
	}
}

// TestMeanAndMinMaxOverCollection checks the other reductions run per
// element too.
func TestMeanAndMinMaxOverCollection(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	df := rdf.New(tree)
	var (
		mean = df.Mean("ArrayFloat64")
		rng  = df.MinMax("ArrayFloat64")
	)

	if err := df.Run(); err != nil {
		t.Fatalf("could not run: %+v", err)
	}

	// every element of entry i is i, so the mean over all elements is the
	// mean of i and the range is 0 to 99.
	var sum float64
	for i := range nEntries {
		sum += float64(arrayLen * i)
	}
	if got, want := mean.Value(), sum/float64(nEntries*arrayLen); math.Abs(got-want) > 1e-9 {
		t.Errorf("mean: got=%v, want=%v", got, want)
	}
	if got, want := rng.Value(), [2]float64{0, 99}; got != want {
		t.Errorf("range: got=%v, want=%v", got, want)
	}
}

// TestScalarAndCollectionTogether checks a scalar branch goes with every
// element of a collection.
func TestScalarAndCollectionTogether(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	df := rdf.New(tree)
	sum := df.Sum("ArrayFloat64 - Float64")

	if err := df.Run(); err != nil {
		t.Fatalf("could not run: %+v", err)
	}
	if got, want := sum.Value(), 0.0; math.Abs(got-want) > 1e-9 {
		t.Errorf("sum: got=%v, want=%v", got, want)
	}
}

// TestMismatchedCollections checks that collections which do not line up
// are reported.
func TestMismatchedCollections(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	df := rdf.New(tree)
	_ = df.Sum("SliceFloat64 + ArrayFloat64")

	if err := df.Run(); err == nil {
		t.Fatal("collections of different lengths were added")
	}
}
