// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hplot

import (
	"go-hep.org/x/hep/hbook"
	"gonum.org/v1/plot/palette"
)

// H3DPlane names the plane a 3-dim histogram is drawn on.
//
// A plot is a flat thing and a 3-dim histogram is not, so drawing one means
// choosing what to do with the third dimension: sum over it, which is what
// the projections below do, or take one plane of bins out of it, which is
// what NewH3DSlice does.
type H3DPlane int

const (
	// H3DXY draws the x-y plane, summing over z.
	H3DXY H3DPlane = iota
	// H3DXZ draws the x-z plane, summing over y.
	H3DXZ
	// H3DYZ draws the y-z plane, summing over x.
	H3DYZ
)

func (p H3DPlane) String() string {
	switch p {
	case H3DXY:
		return "x-y"
	case H3DXZ:
		return "x-z"
	case H3DYZ:
		return "y-z"
	}
	return "unknown"
}

// NewH3D returns a plotter drawing a 3-dim histogram as a heat map of its
// projection onto the given plane.
//
// A nil palette asks for hplot's default one.
func NewH3D(h *hbook.H3D, plane H3DPlane, p palette.Palette) *H2D {
	var proj *hbook.H2D
	switch plane {
	case H3DXZ:
		proj = h.ProjectionXZ()
	case H3DYZ:
		proj = h.ProjectionYZ()
	default:
		proj = h.ProjectionXY()
	}
	return NewH2D(proj, p)
}

// NewH3DSlice returns a plotter drawing the x-y plane of a 3-dim histogram at
// the iz-th bin along z, counting from zero.
//
// NewH3DSlice returns an error if iz names no bin.
func NewH3DSlice(h *hbook.H3D, iz int, p palette.Palette) (*H2D, error) {
	slice, err := h.SliceXY(iz)
	if err != nil {
		return nil, err
	}
	return NewH2D(slice, p), nil
}

// NewH3DProfile returns a plotter drawing the 1-dim histogram a 3-dim one
// becomes when summed over two of its axes.
//
// axis picks the one that survives: 0 for x, 1 for y, 2 for z.
func NewH3DProfile(h *hbook.H3D, axis int, opts ...Options) *H1D {
	var proj *hbook.H1D
	switch axis {
	case 1:
		proj = h.ProjectionY()
	case 2:
		proj = h.ProjectionZ()
	default:
		proj = h.ProjectionX()
	}
	return NewH1D(proj, opts...)
}
