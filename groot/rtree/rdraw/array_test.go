// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rdraw_test

import (
	"math"
	"testing"

	"go-hep.org/x/hep/groot"
	"go-hep.org/x/hep/groot/rtree"
	"go-hep.org/x/hep/groot/rtree/rdraw"
)

// The tree these tests draw from holds, for its i-th of a hundred entries,
// Int32 = i, N = i%10, a slice of N copies of i and an array of ten copies
// of i. That is enough to say exactly what every draw below should give.
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

// wantSlice returns what the slice branch holds across the whole tree: for
// entry i, i%10 copies of i.
func wantSlice() (n int, sum float64) {
	for i := range nEntries {
		k := i % 10
		n += k
		sum += float64(k * i)
	}
	return n, sum
}

// TestDrawArray checks that a fixed-size array branch is looped over: ten
// fills per entry rather than one.
func TestDrawArray(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	h, err := rdraw.H1D(tree, "ArrayFloat64", rdraw.Bins(100, -1, 100))
	if err != nil {
		t.Fatalf("could not draw: %+v", err)
	}

	if got, want := h.Entries(), int64(nEntries*arrayLen); got != want {
		t.Errorf("entries: got=%d, want=%d", got, want)
	}

	// every element of entry i holds i, so the mean is the mean of i.
	var sum float64
	for i := range nEntries {
		sum += float64(arrayLen * i)
	}
	if got, want := h.SumW(), float64(nEntries*arrayLen); got != want {
		t.Errorf("sum of weights: got=%v, want=%v", got, want)
	}
	if got, want := h.XMean(), sum/float64(nEntries*arrayLen); math.Abs(got-want) > 1e-9 {
		t.Errorf("mean: got=%v, want=%v", got, want)
	}
}

// TestDrawSlice checks a variable-length branch, whose number of elements
// changes from entry to entry.
func TestDrawSlice(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	h, err := rdraw.H1D(tree, "SliceFloat64", rdraw.Bins(100, -1, 100))
	if err != nil {
		t.Fatalf("could not draw: %+v", err)
	}

	n, sum := wantSlice()
	if got, want := h.Entries(), int64(n); got != want {
		t.Errorf("entries: got=%d, want=%d", got, want)
	}
	if got, want := h.XMean(), sum/float64(n); math.Abs(got-want) > 1e-9 {
		t.Errorf("mean: got=%v, want=%v", got, want)
	}
}

// TestDrawIndexed checks that indexing an array picks one element and
// leaves one fill per entry.
func TestDrawIndexed(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	h, err := rdraw.H1D(tree, "ArrayFloat64[0]", rdraw.Bins(100, -1, 100))
	if err != nil {
		t.Fatalf("could not draw: %+v", err)
	}

	if got, want := h.Entries(), int64(nEntries); got != want {
		t.Errorf("entries: got=%d, want=%d", got, want)
	}

	// element 0 of entry i is i, the same as the scalar branch.
	ref, err := rdraw.H1D(tree, "Float64", rdraw.Bins(100, -1, 100))
	if err != nil {
		t.Fatalf("could not draw the reference: %+v", err)
	}
	if got, want := h.XMean(), ref.XMean(); math.Abs(got-want) > 1e-9 {
		t.Errorf("mean: got=%v, want=%v (the scalar branch)", got, want)
	}
}

// TestDrawReducers checks the ROOT names that turn a collection into one
// value, which is how a draw goes back to one fill per entry.
func TestDrawReducers(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	n, sum := wantSlice()

	t.Run("Length$", func(t *testing.T) {
		h, err := rdraw.H1D(tree, "Length$(SliceFloat64)", rdraw.Bins(20, -1, 11))
		if err != nil {
			t.Fatalf("could not draw: %+v", err)
		}
		if got, want := h.Entries(), int64(nEntries); got != want {
			t.Errorf("entries: got=%d, want=%d", got, want)
		}
		// the lengths add up to the number of elements in the tree.
		if got, want := h.XMean()*float64(nEntries), float64(n); math.Abs(got-want) > 1e-9 {
			t.Errorf("lengths sum to %v, want %v", got, want)
		}
	})

	t.Run("Sum$", func(t *testing.T) {
		h, err := rdraw.H1D(tree, "Sum$(SliceFloat64)", rdraw.Bins(100, -1, 1000))
		if err != nil {
			t.Fatalf("could not draw: %+v", err)
		}
		if got, want := h.Entries(), int64(nEntries); got != want {
			t.Errorf("entries: got=%d, want=%d", got, want)
		}
		if got, want := h.XMean()*float64(nEntries), sum; math.Abs(got-want) > 1e-6 {
			t.Errorf("sums add to %v, want %v", got, want)
		}
	})

	t.Run("Max$", func(t *testing.T) {
		// every element of an entry is the same number, so the largest
		// is that number, and empty entries answer zero.
		h, err := rdraw.H1D(tree, "Max$(SliceFloat64)", rdraw.Bins(100, -1, 100))
		if err != nil {
			t.Fatalf("could not draw: %+v", err)
		}
		var want float64
		for i := range nEntries {
			if i%10 != 0 {
				want += float64(i)
			}
		}
		if got := h.XMean() * float64(nEntries); math.Abs(got-want) > 1e-6 {
			t.Errorf("maxima add to %v, want %v", got, want)
		}
	})
}

// TestDrawCutOnElements checks that a cut over a collection is applied
// element by element, keeping elements rather than whole entries.
func TestDrawCutOnElements(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	h, err := rdraw.H1D(tree, "ArrayFloat64",
		rdraw.Cut("ArrayFloat64 > 50"),
		rdraw.Bins(100, -1, 100),
	)
	if err != nil {
		t.Fatalf("could not draw: %+v", err)
	}

	// entry i holds ten copies of i, so every element of the entries past
	// 50 is kept and none of the rest.
	var want int64
	for i := range nEntries {
		if float64(i) > 50 {
			want += arrayLen
		}
	}
	if got := h.Entries(); got != want {
		t.Errorf("entries: got=%d, want=%d", got, want)
	}
}

// TestDrawCutOnTheEntry checks that a cut written over the whole collection
// selects entries, which is the other thing a ROOT user means by a cut.
func TestDrawCutOnTheEntry(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	// keep the entries holding at least five elements, and fill with all
	// of them.
	h, err := rdraw.H1D(tree, "SliceFloat64",
		rdraw.Cut("Length$(SliceFloat64) >= 5"),
		rdraw.Bins(100, -1, 100),
	)
	if err != nil {
		t.Fatalf("could not draw: %+v", err)
	}

	var want int64
	for i := range nEntries {
		if k := i % 10; k >= 5 {
			want += int64(k)
		}
	}
	if got := h.Entries(); got != want {
		t.Errorf("entries: got=%d, want=%d", got, want)
	}
}

// TestDrawScalarWithCollection checks that a scalar branch goes with every
// element of a collection in the same expression.
func TestDrawScalarWithCollection(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	// every element of entry i is i, and the scalar is i too.
	h, err := rdraw.H1D(tree, "ArrayFloat64 - Float64", rdraw.Bins(21, -10.5, 10.5))
	if err != nil {
		t.Fatalf("could not draw: %+v", err)
	}

	if got, want := h.Entries(), int64(nEntries*arrayLen); got != want {
		t.Errorf("entries: got=%d, want=%d", got, want)
	}
	if got, want := h.XMean(), 0.0; math.Abs(got-want) > 1e-9 {
		t.Errorf("mean: got=%v, want=%v", got, want)
	}
}

// TestDraw2DOverCollections checks that two axes over the same collection
// are read element by element together.
func TestDraw2DOverCollections(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	h, err := rdraw.H2D(tree, "ArrayFloat32:ArrayFloat64",
		rdraw.Bins(20, -1, 100), rdraw.BinsY(20, -1, 100),
	)
	if err != nil {
		t.Fatalf("could not draw: %+v", err)
	}

	if got, want := h.Entries(), int64(nEntries*arrayLen); got != want {
		t.Errorf("entries: got=%d, want=%d", got, want)
	}
	// the two hold the same number, so everything sits on the diagonal.
	if got, want := h.XMean(), h.YMean(); math.Abs(got-want) > 1e-6 {
		t.Errorf("x mean %v and y mean %v should agree", got, want)
	}
}

// TestDrawMismatchedCollections checks that collections which do not line
// up are reported rather than paired off in some order nobody asked for.
func TestDrawMismatchedCollections(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	_, err := rdraw.H1D(tree, "SliceFloat64 + ArrayFloat64", rdraw.Bins(10, 0, 1))
	if err == nil {
		t.Fatal("a slice and an array of different lengths were drawn together")
	}
}

// TestDrawEmptyCollection checks that an entry whose collection is empty
// contributes nothing, rather than a zero.
func TestDrawEmptyCollection(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	// only the entries whose length is zero, of which there are ten.
	h, err := rdraw.H1D(tree, "SliceFloat64",
		rdraw.Cut("Length$(SliceFloat64) == 0"),
		rdraw.Bins(10, -1, 100),
	)
	if err != nil {
		t.Fatalf("could not draw: %+v", err)
	}
	if got, want := h.Entries(), int64(0); got != want {
		t.Errorf("entries: got=%d, want=%d", got, want)
	}
}

// TestDrawEntryDollar checks the names that say where in the tree the draw
// has got to.
func TestDrawEntryDollar(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	// Entry$ is the entry number, which for this tree is what Int32 holds.
	h, err := rdraw.H1D(tree, "Entry$ - Int32", rdraw.Bins(3, -1.5, 1.5))
	if err != nil {
		t.Fatalf("could not draw: %+v", err)
	}
	if got, want := h.Entries(), int64(nEntries); got != want {
		t.Errorf("entries: got=%d, want=%d", got, want)
	}
	if got, want := h.XMean(), 0.0; math.Abs(got-want) > 1e-12 {
		t.Errorf("Entry$ should equal Int32: mean of the difference is %v, want %v", got, want)
	}

	// Iteration$ counts the elements within an entry.
	h, err = rdraw.H1D(tree, "Iteration$", rdraw.Cut("ArrayFloat64 > -1"), rdraw.Bins(10, -0.5, 9.5))
	if err != nil {
		t.Fatalf("could not draw: %+v", err)
	}
	if got, want := h.Entries(), int64(nEntries*arrayLen); got != want {
		t.Errorf("entries: got=%d, want=%d", got, want)
	}
	if got, want := h.XMean(), 4.5; math.Abs(got-want) > 1e-9 {
		t.Errorf("mean of Iteration$: got=%v, want=%v", got, want)
	}
}

// TestDrawWeightOverElements checks that a weight is evaluated per element
// too.
func TestDrawWeightOverElements(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	h, err := rdraw.H1D(tree, "ArrayFloat64",
		rdraw.Weight("2"),
		rdraw.Bins(100, -1, 100),
	)
	if err != nil {
		t.Fatalf("could not draw: %+v", err)
	}

	if got, want := h.Entries(), int64(nEntries*arrayLen); got != want {
		t.Errorf("entries: got=%d, want=%d", got, want)
	}
	if got, want := h.SumW(), float64(2*nEntries*arrayLen); math.Abs(got-want) > 1e-9 {
		t.Errorf("sum of weights: got=%v, want=%v", got, want)
	}
}

// TestDrawAutoBinsOverCollections checks that a draw given no binning finds
// the range over the elements, not over entries it never looks at.
func TestDrawAutoBinsOverCollections(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	h, err := rdraw.H1D(tree, "ArrayFloat64")
	if err != nil {
		t.Fatalf("could not draw: %+v", err)
	}
	if got, want := h.Entries(), int64(nEntries*arrayLen); got != want {
		t.Errorf("entries: got=%d, want=%d", got, want)
	}
	if lo, hi := h.XMin(), h.XMax(); lo > 0 || hi < 99 {
		t.Errorf("range [%v, %v) should cover the elements 0 to 99", lo, hi)
	}
}
