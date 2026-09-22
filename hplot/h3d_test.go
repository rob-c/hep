// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hplot_test

import (
	"math/rand/v2"
	"path/filepath"
	"testing"

	"go-hep.org/x/hep/hbook"
	"go-hep.org/x/hep/hplot"
	"gonum.org/v1/plot/vg"
)

func h3dSample() *hbook.H3D {
	h := hbook.NewH3D(20, -4, 4, 20, -4, 4, 10, -4, 4)
	rnd := rand.New(rand.NewPCG(1234, 5678))
	for range 20000 {
		h.Fill(rnd.NormFloat64(), rnd.NormFloat64(), rnd.NormFloat64(), 1)
	}
	h.Ann["name"] = "h3"
	return h
}

// TestH3DPlot draws a 3-dim histogram on each of the three planes, and as a
// single slice, and checks each one renders.
func TestH3DPlot(t *testing.T) {
	h := h3dSample()
	tmp := t.TempDir()

	for _, tc := range []struct {
		name  string
		plane hplot.H3DPlane
	}{
		{"xy", hplot.H3DXY},
		{"xz", hplot.H3DXZ},
		{"yz", hplot.H3DYZ},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := hplot.New()
			p.Title.Text = "3-dim histogram, " + tc.plane.String() + " plane"
			p.Add(hplot.NewH3D(h, tc.plane, nil))

			err := p.Save(10*vg.Centimeter, 10*vg.Centimeter, filepath.Join(tmp, tc.name+".png"))
			if err != nil {
				t.Fatalf("could not save plot: %+v", err)
			}
		})
	}

	t.Run("slice", func(t *testing.T) {
		pl, err := hplot.NewH3DSlice(h, 5, nil)
		if err != nil {
			t.Fatalf("could not slice: %+v", err)
		}
		p := hplot.New()
		p.Add(pl)
		err = p.Save(10*vg.Centimeter, 10*vg.Centimeter, filepath.Join(tmp, "slice.png"))
		if err != nil {
			t.Fatalf("could not save plot: %+v", err)
		}

		if _, err := hplot.NewH3DSlice(h, 999, nil); err == nil {
			t.Fatal("expected an error for a z-bin that does not exist")
		}
	})

	t.Run("profile", func(t *testing.T) {
		for _, axis := range []int{0, 1, 2} {
			p := hplot.New()
			p.Add(hplot.NewH3DProfile(h, axis))
			err := p.Save(10*vg.Centimeter, 10*vg.Centimeter, filepath.Join(tmp, "prof.png"))
			if err != nil {
				t.Fatalf("axis %d: could not save plot: %+v", axis, err)
			}
		}
	})
}

// TestH3DPlaneString guards the names the planes go by, which end up in plot
// titles.
func TestH3DPlaneString(t *testing.T) {
	for _, tc := range []struct {
		plane hplot.H3DPlane
		want  string
	}{
		{hplot.H3DXY, "x-y"},
		{hplot.H3DXZ, "x-z"},
		{hplot.H3DYZ, "y-z"},
		{hplot.H3DPlane(42), "unknown"},
	} {
		if got := tc.plane.String(); got != tc.want {
			t.Errorf("got=%q, want=%q", got, tc.want)
		}
	}
}
