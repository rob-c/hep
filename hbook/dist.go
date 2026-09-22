// Copyright ©2016 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hbook

import "math"

// Dist0D is a 0-dim distribution.
type Dist0D struct {
	N     int64   // number of entries
	SumW  float64 // sum of weights
	SumW2 float64 // sum of squared weights
}

func (d Dist0D) clone() Dist0D {
	return d
}

// Rank returns the number of dimensions of the distribution.
func (*Dist0D) Rank() int {
	return 1
}

// Entries returns the number of entries in the distribution.
func (d *Dist0D) Entries() int64 {
	return d.N
}

// EffEntries returns the number of weighted entries, such as:
//
//	(\sum w)^2 / \sum w^2
func (d *Dist0D) EffEntries() float64 {
	if d.SumW2 == 0 {
		return 0
	}
	return d.SumW * d.SumW / d.SumW2
}

// errW returns the absolute error on sumW()
func (d *Dist0D) errW() float64 {
	return math.Sqrt(d.SumW2)
}

// // relErrW returns the relative error on sumW()
// func (d *Dist0D) relErrW() float64 {
// 	// FIXME(sbinet) check for low stats ?
// 	return d.errW() / d.SumW
// }

func (d *Dist0D) fill(w float64) {
	d.N++
	d.SumW += w
	d.SumW2 += w * w
}

func (d *Dist0D) addScaled(a, a2 float64, o Dist0D) {
	d.N += o.N
	d.SumW += a * o.SumW
	d.SumW2 += a2 * o.SumW2
}

func (d *Dist0D) scaleW(f float64) {
	d.SumW *= f
	d.SumW2 *= f * f
}

// Dist1D is a 1-dim distribution.
type Dist1D struct {
	Dist  Dist0D // weight moments
	Stats struct {
		SumWX  float64 // 1st order weighted x moment
		SumWX2 float64 // 2nd order weighted x moment
	}
}

func (d Dist1D) clone() Dist1D {
	return Dist1D{
		Dist:  d.Dist.clone(),
		Stats: d.Stats,
	}
}

// Rank returns the number of dimensions of the distribution.
func (*Dist1D) Rank() int {
	return 1
}

// Entries returns the number of entries in the distribution.
func (d *Dist1D) Entries() int64 {
	return d.Dist.Entries()
}

// EffEntries returns the effective number of entries in the distribution.
func (d *Dist1D) EffEntries() float64 {
	return d.Dist.EffEntries()
}

// SumW returns the sum of weights of the distribution.
func (d *Dist1D) SumW() float64 {
	return d.Dist.SumW
}

// SumW2 returns the sum of squared weights of the distribution.
func (d *Dist1D) SumW2() float64 {
	return d.Dist.SumW2
}

// SumWX returns the 1st order weighted x moment.
func (d *Dist1D) SumWX() float64 {
	return d.Stats.SumWX
}

// SumWX2 returns the 2nd order weighted x moment.
func (d *Dist1D) SumWX2() float64 {
	return d.Stats.SumWX2
}

// errW returns the absolute error on sumW()
func (d *Dist1D) errW() float64 {
	return d.Dist.errW()
}

// // relErrW returns the relative error on sumW()
// func (d *Dist1D) relErrW() float64 {
// 	return d.Dist.relErrW()
// }

// mean returns the weighted mean of the distribution
func (d *Dist1D) mean() float64 {
	// FIXME(sbinet): check for low stats?
	return d.SumWX() / d.SumW()
}

// variance returns the weighted variance of the distribution, defined as:
//
//	sig2 = ( \sum(wx^2) * \sum(w) - \sum(wx)^2 ) / ( \sum(w)^2 - \sum(w^2) )
//
// see: https://en.wikipedia.org/wiki/Weighted_arithmetic_mean
func (d *Dist1D) variance() float64 {
	// FIXME(sbinet): check for low stats?
	sumw := d.SumW()
	num := d.SumWX2()*sumw - d.SumWX()*d.SumWX()
	den := sumw*sumw - d.SumW2()
	v := num / den
	return math.Abs(v)
}

// stdDev returns the weighted standard deviation of the distribution
func (d *Dist1D) stdDev() float64 {
	return math.Sqrt(d.variance())
}

// stdErr returns the weighted standard error of the distribution
func (d *Dist1D) stdErr() float64 {
	// FIXME(sbinet): check for low stats?
	// TODO(sbinet): unbiased should check that Neff>1 and divide by N-1?
	return math.Sqrt(d.variance() / d.EffEntries())
}

// rms returns the weighted RMS of the distribution, defined as:
//
//	rms = \sqrt{\sum{w . x^2} / \sum{w}}
func (d *Dist1D) rms() float64 {
	// FIXME(sbinet): check for low stats?
	meansq := d.SumWX2() / d.SumW()
	return math.Sqrt(meansq)
}

func (d *Dist1D) fill(x, w float64) {
	d.Dist.fill(w)
	d.Stats.SumWX += w * x
	d.Stats.SumWX2 += w * x * x
}

func (d *Dist1D) addScaled(a, a2 float64, o Dist1D) {
	d.Dist.addScaled(a, a2, o.Dist)
	d.Stats.SumWX += a * o.Stats.SumWX
	d.Stats.SumWX2 += a * o.Stats.SumWX2
}

func (d *Dist1D) scaleW(f float64) {
	d.Dist.scaleW(f)
	d.Stats.SumWX *= f
	d.Stats.SumWX2 *= f
}

// Dist2D is a 2-dim distribution.
type Dist2D struct {
	X     Dist1D // x moments
	Y     Dist1D // y moments
	Stats struct {
		SumWXY float64 // 2nd-order cross-term
	}
}

// Rank returns the number of dimensions of the distribution.
func (*Dist2D) Rank() int {
	return 2
}

// Entries returns the number of entries in the distribution.
func (d *Dist2D) Entries() int64 {
	return d.X.Entries()
}

// EffEntries returns the effective number of entries in the distribution.
func (d *Dist2D) EffEntries() float64 {
	return d.X.EffEntries()
}

// SumW returns the sum of weights of the distribution.
func (d *Dist2D) SumW() float64 {
	return d.X.SumW()
}

// SumW2 returns the sum of squared weights of the distribution.
func (d *Dist2D) SumW2() float64 {
	return d.X.SumW2()
}

// SumWX returns the 1st order weighted x moment
func (d *Dist2D) SumWX() float64 {
	return d.X.SumWX()
}

// SumWX2 returns the 2nd order weighted x moment
func (d *Dist2D) SumWX2() float64 {
	return d.X.SumWX2()
}

// SumWY returns the 1st order weighted y moment
func (d *Dist2D) SumWY() float64 {
	return d.Y.SumWX()
}

// SumWY2 returns the 2nd order weighted y moment
func (d *Dist2D) SumWY2() float64 {
	return d.Y.SumWX2()
}

// SumWXY returns the 2nd-order cross-term.
func (d *Dist2D) SumWXY() float64 {
	return d.Stats.SumWXY
}

// // errW returns the absolute error on sumW()
// func (d *Dist2D) errW() float64 {
// 	return d.X.errW()
// }
//
// // relErrW returns the relative error on sumW()
// func (d *Dist2D) relErrW() float64 {
// 	return d.X.relErrW()
// }

// xMean returns the weighted mean of the distribution
func (d *Dist2D) xMean() float64 {
	return d.X.mean()
}

// yMean returns the weighted mean of the distribution
func (d *Dist2D) yMean() float64 {
	return d.Y.mean()
}

// xVariance returns the weighted variance of the distribution
func (d *Dist2D) xVariance() float64 {
	return d.X.variance()
}

// yVariance returns the weighted variance of the distribution
func (d *Dist2D) yVariance() float64 {
	return d.Y.variance()
}

// xStdDev returns the weighted standard deviation of the distribution
func (d *Dist2D) xStdDev() float64 {
	return d.X.stdDev()
}

// yStdDev returns the weighted standard deviation of the distribution
func (d *Dist2D) yStdDev() float64 {
	return d.Y.stdDev()
}

// xStdErr returns the weighted standard error of the distribution
func (d *Dist2D) xStdErr() float64 {
	return d.X.stdErr()
}

// yStdErr returns the weighted standard error of the distribution
func (d *Dist2D) yStdErr() float64 {
	return d.Y.stdErr()
}

// xRMS returns the weighted RMS of the distribution
func (d *Dist2D) xRMS() float64 {
	return d.X.rms()
}

// yRMS returns the weighted RMS of the distribution
func (d *Dist2D) yRMS() float64 {
	return d.Y.rms()
}

func (d *Dist2D) fill(x, y, w float64) {
	d.X.fill(x, w)
	d.Y.fill(y, w)
	d.Stats.SumWXY += w * x * y
}

func (d *Dist2D) addScaled(a, a2 float64, o Dist2D) {
	d.X.addScaled(a, a2, o.X)
	d.Y.addScaled(a, a2, o.Y)
	d.Stats.SumWXY += a * o.Stats.SumWXY
}

func (d *Dist2D) scaleW(f float64) {
	d.X.scaleW(f)
	d.Y.scaleW(f)
	d.Stats.SumWXY *= f
}

// Dist3D is a 3-dim distribution.
type Dist3D struct {
	X     Dist1D // x moments
	Y     Dist1D // y moments
	Z     Dist1D // z moments
	Stats struct {
		SumWXY float64 // 2nd-order x-y cross-term
		SumWXZ float64 // 2nd-order x-z cross-term
		SumWYZ float64 // 2nd-order y-z cross-term
	}
}

// Rank returns the number of dimensions of the distribution.
func (*Dist3D) Rank() int {
	return 3
}

// Entries returns the number of entries in the distribution.
func (d *Dist3D) Entries() int64 {
	return d.X.Entries()
}

// EffEntries returns the effective number of entries in the distribution.
func (d *Dist3D) EffEntries() float64 {
	return d.X.EffEntries()
}

// SumW returns the sum of weights of the distribution.
func (d *Dist3D) SumW() float64 {
	return d.X.SumW()
}

// SumW2 returns the sum of squared weights of the distribution.
func (d *Dist3D) SumW2() float64 {
	return d.X.SumW2()
}

// SumWX returns the 1st order weighted x moment
func (d *Dist3D) SumWX() float64 {
	return d.X.SumWX()
}

// SumWX2 returns the 2nd order weighted x moment
func (d *Dist3D) SumWX2() float64 {
	return d.X.SumWX2()
}

// SumWY returns the 1st order weighted y moment
func (d *Dist3D) SumWY() float64 {
	return d.Y.SumWX()
}

// SumWY2 returns the 2nd order weighted y moment
func (d *Dist3D) SumWY2() float64 {
	return d.Y.SumWX2()
}

// SumWZ returns the 1st order weighted z moment
func (d *Dist3D) SumWZ() float64 {
	return d.Z.SumWX()
}

// SumWZ2 returns the 2nd order weighted z moment
func (d *Dist3D) SumWZ2() float64 {
	return d.Z.SumWX2()
}

// SumWXY returns the 2nd-order x-y cross-term.
func (d *Dist3D) SumWXY() float64 {
	return d.Stats.SumWXY
}

// SumWXZ returns the 2nd-order x-z cross-term.
func (d *Dist3D) SumWXZ() float64 {
	return d.Stats.SumWXZ
}

// SumWYZ returns the 2nd-order y-z cross-term.
func (d *Dist3D) SumWYZ() float64 {
	return d.Stats.SumWYZ
}

// xMean returns the weighted mean of the distribution
func (d *Dist3D) xMean() float64 {
	return d.X.mean()
}

// yMean returns the weighted mean of the distribution
func (d *Dist3D) yMean() float64 {
	return d.Y.mean()
}

// zMean returns the weighted mean of the distribution
func (d *Dist3D) zMean() float64 {
	return d.Z.mean()
}

// xVariance returns the weighted variance of the distribution
func (d *Dist3D) xVariance() float64 {
	return d.X.variance()
}

// yVariance returns the weighted variance of the distribution
func (d *Dist3D) yVariance() float64 {
	return d.Y.variance()
}

// zVariance returns the weighted variance of the distribution
func (d *Dist3D) zVariance() float64 {
	return d.Z.variance()
}

// xStdDev returns the weighted standard deviation of the distribution
func (d *Dist3D) xStdDev() float64 {
	return d.X.stdDev()
}

// yStdDev returns the weighted standard deviation of the distribution
func (d *Dist3D) yStdDev() float64 {
	return d.Y.stdDev()
}

// zStdDev returns the weighted standard deviation of the distribution
func (d *Dist3D) zStdDev() float64 {
	return d.Z.stdDev()
}

// xStdErr returns the weighted standard error of the distribution
func (d *Dist3D) xStdErr() float64 {
	return d.X.stdErr()
}

// yStdErr returns the weighted standard error of the distribution
func (d *Dist3D) yStdErr() float64 {
	return d.Y.stdErr()
}

// zStdErr returns the weighted standard error of the distribution
func (d *Dist3D) zStdErr() float64 {
	return d.Z.stdErr()
}

// xRMS returns the weighted RMS of the distribution
func (d *Dist3D) xRMS() float64 {
	return d.X.rms()
}

// yRMS returns the weighted RMS of the distribution
func (d *Dist3D) yRMS() float64 {
	return d.Y.rms()
}

// zRMS returns the weighted RMS of the distribution
func (d *Dist3D) zRMS() float64 {
	return d.Z.rms()
}

func (d *Dist3D) fill(x, y, z, w float64) {
	d.X.fill(x, w)
	d.Y.fill(y, w)
	d.Z.fill(z, w)
	d.Stats.SumWXY += w * x * y
	d.Stats.SumWXZ += w * x * z
	d.Stats.SumWYZ += w * y * z
}

func (d *Dist3D) addScaled(a, a2 float64, o Dist3D) {
	d.X.addScaled(a, a2, o.X)
	d.Y.addScaled(a, a2, o.Y)
	d.Z.addScaled(a, a2, o.Z)
	d.Stats.SumWXY += a * o.Stats.SumWXY
	d.Stats.SumWXZ += a * o.Stats.SumWXZ
	d.Stats.SumWYZ += a * o.Stats.SumWYZ
}

func (d *Dist3D) scaleW(f float64) {
	d.X.scaleW(f)
	d.Y.scaleW(f)
	d.Z.scaleW(f)
	d.Stats.SumWXY *= f
	d.Stats.SumWXZ *= f
	d.Stats.SumWYZ *= f
}
