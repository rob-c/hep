// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"fmt"
	"math"

	"gonum.org/v1/gonum/dsp/fourier"
)

// Conv is one density convolved with another, as RooFFTConvPdf is.
//
//	(f * g)(x) = integral of f(t) g(x-t) dt
//
// which is what a detector does to a distribution: f is what was there and g
// is the resolution smearing it.
//
// The convolution is done on a grid by Fourier transform, which turns it into
// a multiplication. That costs one pass over the grid per change of
// parameters rather than an integral per point, and is the difference between
// a convolution that can be fitted with and one that cannot: the quadrature
// the Voigtian does costs four hundred evaluations for every single value.
//
// # The range
//
// A convolution on a grid needs to know what range the grid covers, so Conv
// is given one when it is made rather than inferring one later. That is what
// lets Shape answer on its own, as every other density here does.
//
// The grid is periodic: what leaves one end comes back in at the other. Conv
// pads the range by a third either side and throws the padding away, which
// keeps the wrap-around out of the answer as long as the resolution is narrow
// next to the range. A resolution comparable to the whole range is not
// something this can do.
type Conv struct {
	f, g PDF
	n    int

	// the grid the last transform was done on, and the parameters it was
	// done for, so that a fit asking for many points of one shape pays for
	// the transform once.
	cache    []float64
	cachePar []float64
	lo, hi   float64
}

// Convolve returns the convolution of two densities over [lo, hi], evaluated
// on a grid of n points.
//
// n is rounded up to a power of two, which is what the transform wants. A
// few thousand is usually plenty: the grid has to resolve the narrower of the
// two shapes, and nothing finer buys anything.
func Convolve(f, g PDF, lo, hi float64, n int) (*Conv, error) {
	switch {
	case f == nil || g == nil:
		return nil, fmt.Errorf("pdf: a convolution needs two densities")
	case hi <= lo:
		return nil, fmt.Errorf("pdf: a convolution over [%v, %v] has no range", lo, hi)
	case n < 16:
		return nil, fmt.Errorf("pdf: a convolution needs at least 16 points, got %d", n)
	}

	// up to a power of two.
	size := 16
	for size < n {
		size *= 2
	}

	return &Conv{f: f, g: g, n: size, lo: lo, hi: hi}, nil
}

func (c *Conv) Name() string { return "convolution" }

func (c *Conv) ParNames() []string {
	o := append([]string(nil), c.f.ParNames()...)
	return append(o, c.g.ParNames()...)
}

func (c *Conv) NPar() int { return c.f.NPar() + c.g.NPar() }

func (c *Conv) split(par []float64) (fp, gp []float64) {
	n := c.f.NPar()
	return par[:n], par[n:]
}

// padding is how much of the grid either side is there only to absorb the
// wrap-around, as a fraction of the range asked for.
const padding = 1.0 / 3.0

// build computes the convolution on the grid, if it is not already there for
// these parameters.
func (c *Conv) build(par []float64) {
	if c.cache != nil && sameFloats(c.cachePar, par) {
		return
	}

	var (
		pad  = (c.hi - c.lo) * padding
		glo  = c.lo - pad
		ghi  = c.hi + pad
		step = (ghi - glo) / float64(c.n)

		fp, gp = c.split(par)

		fs = make([]float64, c.n)
		gs = make([]float64, c.n)
	)

	// f on the grid, and g centred on zero and wrapped, which is what makes
	// the product of the transforms come out aligned.
	for i := range c.n {
		x := glo + float64(i)*step
		fs[i] = c.f.Shape(x, fp)

		// the offset from the middle, wrapped into the grid.
		d := float64(i) * step
		if i > c.n/2 {
			d = float64(i-c.n) * step
		}
		gs[i] = c.g.Shape(d, gp)
	}

	fft := fourier.NewFFT(c.n)

	var (
		fc = fft.Coefficients(nil, fs)
		gc = fft.Coefficients(nil, gs)
	)
	for i := range fc {
		fc[i] *= gc[i]
	}

	out := fft.Sequence(nil, fc)
	for i := range out {
		// the transform here is unnormalised, and the sum has to become an
		// integral, so the step comes back in.
		out[i] *= step / float64(c.n)
		if out[i] < 0 {
			out[i] = 0
		}
	}

	c.cache = out
	c.cachePar = append(c.cachePar[:0], par...)
}

// at reads the convolution off the grid, interpolating between points.
func (c *Conv) at(x float64) float64 {
	var (
		pad  = (c.hi - c.lo) * padding
		glo  = c.lo - pad
		ghi  = c.hi + pad
		step = (ghi - glo) / float64(c.n)
		pos  = (x - glo) / step
		i    = int(pos)
	)

	switch {
	case i < 0 || i >= len(c.cache)-1:
		return 0
	}

	t := pos - float64(i)
	return c.cache[i]*(1-t) + c.cache[i+1]*t
}

// Shape returns the convolution at x.
func (c *Conv) Shape(x float64, par []float64) float64 {
	c.build(par)
	return c.at(x)
}

// Integral returns the integral of the convolution over [lo, hi], which may
// be narrower than the range the convolution was built over but is read off
// the same grid.
func (c *Conv) Integral(lo, hi float64, par []float64) float64 {
	c.build(par)

	var (
		pad  = (c.hi - c.lo) * padding
		glo  = c.lo - pad
		ghi  = c.hi + pad
		step = (ghi - glo) / float64(c.n)
		sum  float64
	)

	for i := range c.n {
		x := glo + float64(i)*step
		if x < lo || x > hi {
			continue
		}
		sum += c.cache[i] * step
	}

	return sum
}

func sameFloats(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// VoigtianFFT returns a Breit-Wigner convolved with a gaussian, computed by
// transform rather than by quadrature.
//
// It is the same shape Voigtian gives and is far cheaper to fit with, at the
// cost of a grid: the answer is interpolated off n points rather than
// integrated at each one.
func VoigtianFFT(lo, hi float64, n int) (*Conv, error) {
	return Convolve(BreitWigner(), zeroMeanGauss{}, lo, hi, n)
}

// zeroMeanGauss is a gaussian centred on zero, with only a width: a
// resolution has no mean of its own, the shape it smears having one already.
type zeroMeanGauss struct{}

func (zeroMeanGauss) Name() string       { return "resolution" }
func (zeroMeanGauss) ParNames() []string { return []string{"sigma"} }
func (zeroMeanGauss) NPar() int          { return 1 }

func (zeroMeanGauss) Shape(x float64, par []float64) float64 {
	s := par[0]
	if s <= 0 {
		return 0
	}
	d := x / s
	return math.Exp(-0.5*d*d) / (s * math.Sqrt(2*math.Pi))
}

func (zeroMeanGauss) Integral(lo, hi float64, par []float64) float64 {
	s := par[0]
	if s <= 0 {
		return 0
	}
	const sqrt2 = math.Sqrt2
	return 0.5 * (math.Erf(hi/(s*sqrt2)) - math.Erf(lo/(s*sqrt2)))
}

// Resolution returns a gaussian centred on zero, for use as the second
// argument to Convolve.
func Resolution() PDF { return zeroMeanGauss{} }
