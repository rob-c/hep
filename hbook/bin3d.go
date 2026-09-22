// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hbook

// Bin3D models a bin in a 3-dim space.
type Bin3D struct {
	XRange Range
	YRange Range
	ZRange Range
	Dist   Dist3D
}

// Rank returns the number of dimensions for this bin.
func (Bin3D) Rank() int { return 3 }

func (b *Bin3D) fill(x, y, z, w float64) {
	b.Dist.fill(x, y, z, w)
}

func (b *Bin3D) addScaled(a, a2 float64, o Bin3D) {
	b.Dist.addScaled(a, a2, o.Dist)
}

// Entries returns the number of entries in this bin.
func (b *Bin3D) Entries() int64 {
	return b.Dist.Entries()
}

// EffEntries returns the effective number of entries \f$ = (\sum w)^2 / \sum w^2 \f$
func (b *Bin3D) EffEntries() float64 {
	return b.Dist.EffEntries()
}

// SumW returns the sum of weights in this bin.
func (b *Bin3D) SumW() float64 {
	return b.Dist.SumW()
}

// SumW2 returns the sum of squared weights in this bin.
func (b *Bin3D) SumW2() float64 {
	return b.Dist.SumW2()
}

// XEdges returns the [low,high] edges of this bin.
func (b *Bin3D) XEdges() Range {
	return b.XRange
}

// YEdges returns the [low,high] edges of this bin.
func (b *Bin3D) YEdges() Range {
	return b.YRange
}

// ZEdges returns the [low,high] edges of this bin.
func (b *Bin3D) ZEdges() Range {
	return b.ZRange
}

// XMin returns the lower limit of the bin (inclusive).
func (b *Bin3D) XMin() float64 {
	return b.XRange.Min
}

// YMin returns the lower limit of the bin (inclusive).
func (b *Bin3D) YMin() float64 {
	return b.YRange.Min
}

// ZMin returns the lower limit of the bin (inclusive).
func (b *Bin3D) ZMin() float64 {
	return b.ZRange.Min
}

// XMax returns the upper limit of the bin (exclusive).
func (b *Bin3D) XMax() float64 {
	return b.XRange.Max
}

// YMax returns the upper limit of the bin (exclusive).
func (b *Bin3D) YMax() float64 {
	return b.YRange.Max
}

// ZMax returns the upper limit of the bin (exclusive).
func (b *Bin3D) ZMax() float64 {
	return b.ZRange.Max
}

// XMid returns the geometric center of the bin.
// i.e.: 0.5*(high+low)
func (b *Bin3D) XMid() float64 {
	return 0.5 * (b.XRange.Min + b.XRange.Max)
}

// YMid returns the geometric center of the bin.
// i.e.: 0.5*(high+low)
func (b *Bin3D) YMid() float64 {
	return 0.5 * (b.YRange.Min + b.YRange.Max)
}

// ZMid returns the geometric center of the bin.
// i.e.: 0.5*(high+low)
func (b *Bin3D) ZMid() float64 {
	return 0.5 * (b.ZRange.Min + b.ZRange.Max)
}

// XYZMid returns the (x,y,z) coordinates of the geometric center of the bin.
// i.e.: 0.5*(high+low)
func (b *Bin3D) XYZMid() (float64, float64, float64) {
	return b.XMid(), b.YMid(), b.ZMid()
}

// XWidth returns the (signed) width of the bin
func (b *Bin3D) XWidth() float64 {
	return b.XRange.Max - b.XRange.Min
}

// YWidth returns the (signed) width of the bin
func (b *Bin3D) YWidth() float64 {
	return b.YRange.Max - b.YRange.Min
}

// ZWidth returns the (signed) width of the bin
func (b *Bin3D) ZWidth() float64 {
	return b.ZRange.Max - b.ZRange.Min
}

// XYZWidth returns the (signed) (x,y,z) widths of the bin
func (b *Bin3D) XYZWidth() (float64, float64, float64) {
	return b.XWidth(), b.YWidth(), b.ZWidth()
}

// XFocus returns the mean position in the bin, or the midpoint (if the
// sum of weights for this bin is 0).
func (b *Bin3D) XFocus() float64 {
	if b.SumW() == 0 {
		return b.XMid()
	}
	return b.XMean()
}

// YFocus returns the mean position in the bin, or the midpoint (if the
// sum of weights for this bin is 0).
func (b *Bin3D) YFocus() float64 {
	if b.SumW() == 0 {
		return b.YMid()
	}
	return b.YMean()
}

// ZFocus returns the mean position in the bin, or the midpoint (if the
// sum of weights for this bin is 0).
func (b *Bin3D) ZFocus() float64 {
	if b.SumW() == 0 {
		return b.ZMid()
	}
	return b.ZMean()
}

// XYZFocus returns the mean position in the bin, or the midpoint (if the
// sum of weights for this bin is 0).
func (b *Bin3D) XYZFocus() (float64, float64, float64) {
	if b.SumW() == 0 {
		return b.XMid(), b.YMid(), b.ZMid()
	}
	return b.XMean(), b.YMean(), b.ZMean()
}

// XMean returns the mean X.
func (b *Bin3D) XMean() float64 {
	return b.Dist.xMean()
}

// YMean returns the mean Y.
func (b *Bin3D) YMean() float64 {
	return b.Dist.yMean()
}

// ZMean returns the mean Z.
func (b *Bin3D) ZMean() float64 {
	return b.Dist.zMean()
}

// XVariance returns the variance in X.
func (b *Bin3D) XVariance() float64 {
	return b.Dist.xVariance()
}

// YVariance returns the variance in Y.
func (b *Bin3D) YVariance() float64 {
	return b.Dist.yVariance()
}

// ZVariance returns the variance in Z.
func (b *Bin3D) ZVariance() float64 {
	return b.Dist.zVariance()
}

// XStdDev returns the standard deviation in X.
func (b *Bin3D) XStdDev() float64 {
	return b.Dist.xStdDev()
}

// YStdDev returns the standard deviation in Y.
func (b *Bin3D) YStdDev() float64 {
	return b.Dist.yStdDev()
}

// ZStdDev returns the standard deviation in Z.
func (b *Bin3D) ZStdDev() float64 {
	return b.Dist.zStdDev()
}

// XStdErr returns the standard error in X.
func (b *Bin3D) XStdErr() float64 {
	return b.Dist.xStdErr()
}

// YStdErr returns the standard error in Y.
func (b *Bin3D) YStdErr() float64 {
	return b.Dist.yStdErr()
}

// ZStdErr returns the standard error in Z.
func (b *Bin3D) ZStdErr() float64 {
	return b.Dist.zStdErr()
}

// XRMS returns the RMS in X.
func (b *Bin3D) XRMS() float64 {
	return b.Dist.xRMS()
}

// YRMS returns the RMS in Y.
func (b *Bin3D) YRMS() float64 {
	return b.Dist.yRMS()
}

// ZRMS returns the RMS in Z.
func (b *Bin3D) ZRMS() float64 {
	return b.Dist.zRMS()
}

// check Bin3D implements interfaces
var _ Bin = (*Bin3D)(nil)
