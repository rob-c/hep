// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rdf_test

import (
	"math"
	"testing"

	"go-hep.org/x/hep/groot/rtree/rdf"
)

// TestProfile1D checks the frame's profile against the same tree the draw
// tests use: with ten bins over a hundred entries the k-th bin takes the
// entries 10k to 10k+9, whose mean is 10k+4.5.
func TestProfile1D(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	df := rdf.New(tree)
	p := df.Profile1D("Float64:Float64", rdf.Bins(10, 0, 100))

	if err := df.Run(); err != nil {
		t.Fatalf("could not run: %+v", err)
	}

	got := p.Value()
	if got, want := got.Entries(), int64(nEntries); got != want {
		t.Fatalf("entries: got=%d, want=%d", got, want)
	}

	bins := got.Binning().Bins()
	for k := range bins {
		if got, want := bins[k].YMean(), float64(10*k)+4.5; math.Abs(got-want) > 1e-9 {
			t.Errorf("bin %d: mean y got=%v, want=%v", k, got, want)
		}
	}
}

// TestProfile1DAfterFilter checks a profile takes the filters above it.
func TestProfile1DAfterFilter(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	df := rdf.New(tree).Filter("Float64 >= 50")
	p := df.Profile1D("Float64:Float64", rdf.Bins(10, 0, 100))

	if err := df.Run(); err != nil {
		t.Fatalf("could not run: %+v", err)
	}
	if got, want := p.Value().Entries(), int64(50); got != want {
		t.Errorf("entries: got=%d, want=%d", got, want)
	}
}

// TestProfile1DOverCollection checks a profile built from collection
// branches runs per element.
func TestProfile1DOverCollection(t *testing.T) {
	tree, close := openTree(t)
	defer close()

	df := rdf.New(tree)
	p := df.Profile1D("ArrayFloat32:ArrayFloat64", rdf.Bins(10, 0, 100))

	if err := df.Run(); err != nil {
		t.Fatalf("could not run: %+v", err)
	}
	if got, want := p.Value().Entries(), int64(nEntries*arrayLen); got != want {
		t.Errorf("entries: got=%d, want=%d", got, want)
	}
}
