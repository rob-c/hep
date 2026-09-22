// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rdraw_test

import (
	"math"
	"testing"

	"go-hep.org/x/hep/groot/rtree/rdraw"
)

// TestProfile checks a profile of the tree whose i-th entry holds i: with
// ten bins over a hundred entries, the k-th bin takes the entries 10k to
// 10k+9 and so has a mean of 10k+4.5.
func TestProfile(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	p, err := rdraw.P1D(tree, "Float64:Float64", rdraw.Bins(10, 0, 100))
	if err != nil {
		t.Fatalf("could not profile: %+v", err)
	}

	if got, want := p.Entries(), int64(nEntries); got != want {
		t.Fatalf("entries: got=%d, want=%d", got, want)
	}

	bins := p.Binning().Bins()
	if got, want := len(bins), 10; got != want {
		t.Fatalf("bins: got=%d, want=%d", got, want)
	}

	for k := range bins {
		b := &bins[k]
		if got, want := b.Entries(), int64(10); got != want {
			t.Errorf("bin %d: got %d entries, want %d", k, got, want)
		}
		if got, want := b.YMean(), float64(10*k)+4.5; math.Abs(got-want) > 1e-9 {
			t.Errorf("bin %d: mean y got=%v, want=%v", k, got, want)
		}

		// the ten values in a bin are consecutive whole numbers, whose
		// spread about their mean is the same in every bin. hbook
		// carries Bessel's correction, as it does everywhere else, so
		// the sum of squares is over one fewer than the count.
		var want float64
		for i := range 10 {
			d := float64(i) - 4.5
			want += d * d
		}
		want = math.Sqrt(want / 9)
		if got := b.YStdDev(); math.Abs(got-want) > 1e-9 {
			t.Errorf("bin %d: spread got=%v, want=%v", k, got, want)
		}
		// and the error on the mean is that over the root of the count.
		if got, want := b.YStdErr(), want/math.Sqrt(10); math.Abs(got-want) > 1e-9 {
			t.Errorf("bin %d: error on the mean got=%v, want=%v", k, got, want)
		}
	}
}

// TestProfileOverCollection checks a profile built from collection
// branches, which is the shape a per-jet or per-track profile has.
func TestProfileOverCollection(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	// every element of entry i holds i, in both branches.
	p, err := rdraw.P1D(tree, "ArrayFloat32:ArrayFloat64", rdraw.Bins(10, 0, 100))
	if err != nil {
		t.Fatalf("could not profile: %+v", err)
	}

	if got, want := p.Entries(), int64(nEntries*arrayLen); got != want {
		t.Errorf("entries: got=%d, want=%d", got, want)
	}

	bins := p.Binning().Bins()
	for k := range bins {
		if got, want := bins[k].YMean(), float64(10*k)+4.5; math.Abs(got-want) > 1e-5 {
			t.Errorf("bin %d: mean y got=%v, want=%v", k, got, want)
		}
	}
}

// TestProfileAutoBins checks that a profile given no binning works out the
// range of the axis it bins.
func TestProfileAutoBins(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	p, err := rdraw.P1D(tree, "Float64:Float64")
	if err != nil {
		t.Fatalf("could not profile: %+v", err)
	}
	if got, want := p.Entries(), int64(nEntries); got != want {
		t.Errorf("entries: got=%d, want=%d", got, want)
	}
	if lo, hi := p.XMin(), p.XMax(); lo > 0 || hi < 99 {
		t.Errorf("range [%v, %v) should cover 0 to 99", lo, hi)
	}
}

// TestProfileCutAndWeight checks that a profile takes the same cut and
// weight the histograms do.
func TestProfileCutAndWeight(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	p, err := rdraw.P1D(tree, "Float64:Float64",
		rdraw.Cut("Float64 >= 50"),
		rdraw.Bins(10, 0, 100),
	)
	if err != nil {
		t.Fatalf("could not profile: %+v", err)
	}

	if got, want := p.Entries(), int64(50); got != want {
		t.Errorf("entries: got=%d, want=%d", got, want)
	}
	// the first five bins are empty, the rest hold ten each.
	bins := p.Binning().Bins()
	for k := range bins {
		want := int64(10)
		if k < 5 {
			want = 0
		}
		if got := bins[k].Entries(); got != want {
			t.Errorf("bin %d: got %d entries, want %d", k, got, want)
		}
	}
}

// TestProfileWrongRank checks that a profile asked for with the wrong
// number of expressions says so.
func TestProfileWrongRank(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	for _, expr := range []string{"Float64", "Float64:Float64:Float64"} {
		if _, err := rdraw.P1D(tree, expr, rdraw.Bins(10, 0, 100)); err == nil {
			t.Errorf("%q was accepted as a profile", expr)
		}
	}
}
