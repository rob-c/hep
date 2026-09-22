// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hbook

import "fmt"

// H3D is a 3-dim histogram with weighted entries.
type H3D struct {
	Binning Binning3D
	Ann     Annotation
}

// NewH3D creates a new 3-dim histogram.
func NewH3D(nx int, xlow, xhigh float64, ny int, ylow, yhigh float64, nz int, zlow, zhigh float64) *H3D {
	return &H3D{
		Binning: newBinning3D(nx, xlow, xhigh, ny, ylow, yhigh, nz, zlow, zhigh),
		Ann:     make(Annotation),
	}
}

// NewH3DFromEdges creates a new 3-dim histogram from slices
// of edges in x, y and z.
// The number of bins in each dimension is thus len(edges)-1.
// It panics if the length of edges is <=1 (in any dimension.)
// It panics if the edges are not sorted (in any dimension.)
// It panics if there are duplicate edge values (in any dimension.)
func NewH3DFromEdges(xedges, yedges, zedges []float64) *H3D {
	return &H3D{
		Binning: newBinning3DFromEdges(xedges, yedges, zedges),
		Ann:     make(Annotation),
	}
}

// Clone returns a deep copy of this 3-dim histogram.
func (h *H3D) Clone() *H3D {
	return &H3D{
		Binning: h.Binning.clone(),
		Ann:     h.Ann.clone(),
	}
}

// Name returns the name of this histogram, if any
func (h *H3D) Name() string {
	v, ok := h.Ann["name"]
	if !ok {
		return ""
	}
	n, ok := v.(string)
	if !ok {
		return ""
	}
	return n
}

// Annotation returns the annotations attached to this histogram
func (h *H3D) Annotation() Annotation {
	return h.Ann
}

// Rank returns the number of dimensions for this histogram
func (h *H3D) Rank() int {
	return 3
}

// Entries returns the number of entries in this histogram
func (h *H3D) Entries() int64 {
	return h.Binning.entries()
}

// EffEntries returns the number of effective entries in this histogram.
func (h *H3D) EffEntries() float64 {
	return h.Binning.effEntries()
}

// SumW returns the sum of weights in this histogram.
// Overflows are included in the computation.
func (h *H3D) SumW() float64 {
	return h.Binning.Dist.SumW()
}

// SumW2 returns the sum of squared weights in this histogram.
// Overflows are included in the computation.
func (h *H3D) SumW2() float64 {
	return h.Binning.Dist.SumW2()
}

// SumWX returns the 1st order weighted x moment
// Overflows are included in the computation.
func (h *H3D) SumWX() float64 {
	return h.Binning.Dist.SumWX()
}

// SumWX2 returns the 2nd order weighted x moment
// Overflows are included in the computation.
func (h *H3D) SumWX2() float64 {
	return h.Binning.Dist.SumWX2()
}

// SumWY returns the 1st order weighted y moment
// Overflows are included in the computation.
func (h *H3D) SumWY() float64 {
	return h.Binning.Dist.SumWY()
}

// SumWY2 returns the 2nd order weighted y moment
// Overflows are included in the computation.
func (h *H3D) SumWY2() float64 {
	return h.Binning.Dist.SumWY2()
}

// SumWZ returns the 1st order weighted z moment
// Overflows are included in the computation.
func (h *H3D) SumWZ() float64 {
	return h.Binning.Dist.SumWZ()
}

// SumWZ2 returns the 2nd order weighted z moment
// Overflows are included in the computation.
func (h *H3D) SumWZ2() float64 {
	return h.Binning.Dist.SumWZ2()
}

// SumWXY returns the 2nd-order x-y cross-term.
// Overflows are included in the computation.
func (h *H3D) SumWXY() float64 {
	return h.Binning.Dist.SumWXY()
}

// SumWXZ returns the 2nd-order x-z cross-term.
// Overflows are included in the computation.
func (h *H3D) SumWXZ() float64 {
	return h.Binning.Dist.SumWXZ()
}

// SumWYZ returns the 2nd-order y-z cross-term.
// Overflows are included in the computation.
func (h *H3D) SumWYZ() float64 {
	return h.Binning.Dist.SumWYZ()
}

// XMean returns the mean X.
// Overflows are included in the computation.
func (h *H3D) XMean() float64 {
	return h.Binning.Dist.xMean()
}

// YMean returns the mean Y.
// Overflows are included in the computation.
func (h *H3D) YMean() float64 {
	return h.Binning.Dist.yMean()
}

// ZMean returns the mean Z.
// Overflows are included in the computation.
func (h *H3D) ZMean() float64 {
	return h.Binning.Dist.zMean()
}

// XVariance returns the variance in X.
// Overflows are included in the computation.
func (h *H3D) XVariance() float64 {
	return h.Binning.Dist.xVariance()
}

// YVariance returns the variance in Y.
// Overflows are included in the computation.
func (h *H3D) YVariance() float64 {
	return h.Binning.Dist.yVariance()
}

// ZVariance returns the variance in Z.
// Overflows are included in the computation.
func (h *H3D) ZVariance() float64 {
	return h.Binning.Dist.zVariance()
}

// XStdDev returns the standard deviation in X.
// Overflows are included in the computation.
func (h *H3D) XStdDev() float64 {
	return h.Binning.Dist.xStdDev()
}

// YStdDev returns the standard deviation in Y.
// Overflows are included in the computation.
func (h *H3D) YStdDev() float64 {
	return h.Binning.Dist.yStdDev()
}

// ZStdDev returns the standard deviation in Z.
// Overflows are included in the computation.
func (h *H3D) ZStdDev() float64 {
	return h.Binning.Dist.zStdDev()
}

// XStdErr returns the standard error in X.
// Overflows are included in the computation.
func (h *H3D) XStdErr() float64 {
	return h.Binning.Dist.xStdErr()
}

// YStdErr returns the standard error in Y.
// Overflows are included in the computation.
func (h *H3D) YStdErr() float64 {
	return h.Binning.Dist.yStdErr()
}

// ZStdErr returns the standard error in Z.
// Overflows are included in the computation.
func (h *H3D) ZStdErr() float64 {
	return h.Binning.Dist.zStdErr()
}

// XRMS returns the RMS in X.
// Overflows are included in the computation.
func (h *H3D) XRMS() float64 {
	return h.Binning.Dist.xRMS()
}

// YRMS returns the RMS in Y.
// Overflows are included in the computation.
func (h *H3D) YRMS() float64 {
	return h.Binning.Dist.yRMS()
}

// ZRMS returns the RMS in Z.
// Overflows are included in the computation.
func (h *H3D) ZRMS() float64 {
	return h.Binning.Dist.zRMS()
}

// Fill fills this histogram with (x,y,z) and weight w.
func (h *H3D) Fill(x, y, z, w float64) {
	h.Binning.fill(x, y, z, w)
}

// FillN fills this histogram with the provided slices (xs,ys,zs) and weights ws.
// if ws is nil, the histogram will be filled with entries of weight 1.
// Otherwise, FillN panics if the slices lengths differ.
func (h *H3D) FillN(xs, ys, zs, ws []float64) {
	if len(xs) != len(ys) || len(xs) != len(zs) {
		panic(fmt.Errorf("hbook: lengths mismatch"))
	}
	if ws != nil && len(xs) != len(ws) {
		panic(fmt.Errorf("hbook: lengths mismatch"))
	}

	for i := range xs {
		w := 1.0
		if ws != nil {
			w = ws[i]
		}
		h.Binning.fill(xs[i], ys[i], zs[i], w)
	}
}

// Bin returns the bin at coordinates (x,y,z) for this 3-dim histogram.
// Bin returns nil for under/over flow bins or for a point falling in a gap
// of the binning.
func (h *H3D) Bin(x, y, z float64) *Bin3D {
	idx := h.Binning.coordToIndex(x, y, z)
	if idx < 0 || idx >= len(h.Binning.Bins) {
		return nil
	}
	return &h.Binning.Bins[idx]
}

// XMin returns the low edge of the X-axis of this histogram.
func (h *H3D) XMin() float64 { return h.Binning.xMin() }

// XMax returns the high edge of the X-axis of this histogram.
func (h *H3D) XMax() float64 { return h.Binning.xMax() }

// YMin returns the low edge of the Y-axis of this histogram.
func (h *H3D) YMin() float64 { return h.Binning.yMin() }

// YMax returns the high edge of the Y-axis of this histogram.
func (h *H3D) YMax() float64 { return h.Binning.yMax() }

// ZMin returns the low edge of the Z-axis of this histogram.
func (h *H3D) ZMin() float64 { return h.Binning.zMin() }

// ZMax returns the high edge of the Z-axis of this histogram.
func (h *H3D) ZMax() float64 { return h.Binning.zMax() }

// Integral computes the integral of the histogram.
//
// Overflows are included in the computation.
func (h *H3D) Integral() float64 {
	return h.SumW()
}

// check various interfaces
var (
	_ Object    = (*H3D)(nil)
	_ Histogram = (*H3D)(nil)
)
