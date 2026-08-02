// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rhist

import (
	"testing"

	"go-hep.org/x/hep/groot/rbase"
	"go-hep.org/x/hep/hbook"
)

func TestMultiGraphROOTMerge(t *testing.T) {
	mk := func(pts ...float64) *tmultigraph {
		mg := newMultiGraph()
		s2 := hbook.NewS2D()
		for _, x := range pts {
			s2.Fill(hbook.Point2D{X: x, Y: 2 * x})
		}
		mg.graphs.Append(NewGraphFrom(s2))
		return mg
	}

	mg1 := mk(1, 2)
	mg2 := mk(3, 4, 5)

	if err := mg1.ROOTMerge(mg2); err != nil {
		t.Fatalf("could not merge: %+v", err)
	}
	if got, want := mg1.Len(), 2; got != want {
		t.Fatalf("got %d graphs, want %d", got, want)
	}
	gs := mg1.Graphs()
	if got, want := gs[0].Len(), 2; got != want {
		t.Errorf("graph 0: got %d points, want %d", got, want)
	}
	if got, want := gs[1].Len(), 3; got != want {
		t.Errorf("graph 1: got %d points, want %d", got, want)
	}
	if x, y := gs[1].XY(0); x != 3 || y != 6 {
		t.Errorf("graph 1 point 0: got (%v, %v), want (3, 6)", x, y)
	}

	if err := mg1.ROOTMerge(rbase.NewNamed("n", "t")); err == nil {
		t.Fatal("merging a TNamed into a TMultiGraph reported success")
	}
}
