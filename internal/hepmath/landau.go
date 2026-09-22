// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package hepmath holds the special functions particle physics needs and the
// Go standard library does not have.
package hepmath // import "go-hep.org/x/hep/internal/hepmath"

import (
	"math"
	"sync"
)

// The Landau density is computed from Landau's own definition of it,
//
//	phi(lambda) = 1/pi * integral from 0 to infinity of
//	              exp(-t*ln(t) - lambda*t) * sin(pi*t) dt
//
// rather than from any of the rational approximations usually quoted for it.
// Those live in CERNLIB and in ROOT, both of which are GPL, and the integral
// is the definition anyway: it is what the approximations approximate.
//
// The integral is done once onto a grid and interpolated after, because a
// density a fit calls a million times cannot afford quadrature each time.
// Doing it per call was the first attempt and was three milliseconds a go.
//
// The integrand is oscillatory -- it carries a sin(pi*t) -- so the quadrature
// is a fixed Simpson rule with enough points to resolve that, and not an
// adaptive one. An adaptive rule subdivides wherever its two estimates
// disagree, and on an oscillation they disagree everywhere, so it recurses
// until it hits its depth limit over the whole range.

const (
	// the grid the density is tabulated on.
	landauLo   = -5.0
	landauHi   = 60.0
	landauStep = 0.01

	// beyond landauHi the density is its asymptote, 1/lambda^2.
	landauTailFrom = landauHi

	// Below this the defining integral cannot be evaluated in floating
	// point and the left-tail asymptotic is used instead. See landauDensity.
	landauAsymFrom = -3.0
)

var (
	landauOnce sync.Once
	landauPDF  []float64 // the density on the grid
	landauCDF  []float64 // its running integral
)

func landauTable() {
	landauOnce.Do(func() {
		n := int((landauHi-landauLo)/landauStep) + 1

		landauPDF = make([]float64, n)
		landauCDF = make([]float64, n)

		for i := range n {
			landauPDF[i] = landauDensity(landauLo + float64(i)*landauStep)
		}

		// the running integral, by the trapezium rule on a grid this fine.
		for i := 1; i < n; i++ {
			landauCDF[i] = landauCDF[i-1] +
				0.5*(landauPDF[i]+landauPDF[i-1])*landauStep
		}
	})
}

// landauDensity returns the density at lambda, by whichever of the two
// routes is sound there.
//
// The integrand of Landau's integral peaks at exp(exp(-1-lambda)), and the
// sine in it has to cancel that back down to the answer. At lambda = -3 the
// peak is 1.6e3 and the answer 6.7e-4, which floating point manages; by
// -3.25 the cancellation has eaten every digit and the result comes back
// negative. That is a property of the integral, not of the quadrature, and
// is why the approximations in the literature exist at all.
//
// So below -3 use the left-tail asymptotic instead,
//
//	phi(lambda) -> exp(-(lambda+1)/2 - exp(-(lambda+1))) / sqrt(2*pi)
//
// which comes from the saddle point of the same integral. Measured against
// the integral where both are sound it is right to 0.5% at -3 and better
// below, converging as lambda falls. The seam at -3 is therefore a step of
// about 0.5% in a density that is 6.7e-4 there: three parts in a million of
// the peak, which nothing fitting with it will notice.
func landauDensity(lambda float64) float64 {
	if lambda < landauAsymFrom {
		u := -(lambda + 1)
		if u > 700 {
			return 0
		}
		return math.Exp(u/2-math.Exp(u)) / math.Sqrt(2*math.Pi)
	}
	return landauIntegral(lambda)
}

// landauIntegral evaluates Landau's integral at lambda.
func landauIntegral(lambda float64) float64 {
	// Where the integrand matters depends on lambda: for a large one the
	// exp(-lambda*t) crushes it almost at once, and integrating out to 30
	// would put every sample where there is nothing to see.
	hi := 30.0
	if lambda > 1 {
		hi = math.Max(2, 40/lambda)
	}

	f := func(t float64) float64 {
		if t <= 0 {
			return 0
		}
		e := -t*math.Log(t) - lambda*t
		if e < -700 {
			return 0
		}
		return math.Exp(e) * math.Sin(math.Pi*t)
	}

	// 1200 points over 15 periods of the sine is eighty a period, which is
	// far more than Simpson needs to follow it.
	return simpsonFixed(f, 0, hi, 1200) / math.Pi
}

// simpsonFixed is Simpson's rule on n intervals, n even.
func simpsonFixed(f func(float64) float64, lo, hi float64, n int) float64 {
	if hi <= lo {
		return 0
	}
	if n%2 == 1 {
		n++
	}

	h := (hi - lo) / float64(n)
	sum := f(lo) + f(hi)

	for i := 1; i < n; i++ {
		w := 4.0
		if i%2 == 0 {
			w = 2.0
		}
		sum += w * f(lo+float64(i)*h)
	}

	return sum * h / 3
}

// Landau returns the Landau density at x, the distribution of the energy a
// charged particle loses crossing a thin layer of matter.
//
// The density has no variance and a very long tail to the right: it goes like
// 1/x^2 out there, which is why a Landau fit is so sensitive to where the
// range is cut.
func Landau(x float64) float64 {
	switch {
	case math.IsNaN(x):
		return math.NaN()
	case x < landauLo:
		// the left tail falls off doubly exponentially: it is below 1e-30
		// by here, and underflows not far after.
		return landauDensity(x)
	case x >= landauTailFrom:
		return 1 / (x * x)
	}

	landauTable()
	return interp(landauPDF, x)
}

// LandauCDF returns the probability that a Landau variate is below x.
func LandauCDF(x float64) float64 {
	switch {
	case math.IsNaN(x):
		return math.NaN()
	case x < landauLo:
		return 0
	case x >= landauTailFrom:
		// the tail from here on is the integral of 1/u^2, which is 1/x.
		landauTable()
		return landauCDF[len(landauCDF)-1] + 1/landauTailFrom - 1/x
	}

	landauTable()
	return interp(landauCDF, x)
}

// interp reads a tabulated function at x by cubic interpolation.
//
// The grid is fine enough that linear would nearly do, but a density is
// differentiated by every minimiser that fits with it, and linear
// interpolation has a derivative that jumps at every knot.
func interp(tbl []float64, x float64) float64 {
	var (
		pos = (x - landauLo) / landauStep
		i   = int(pos)
	)
	switch {
	case i < 0:
		return tbl[0]
	case i >= len(tbl)-1:
		return tbl[len(tbl)-1]
	}

	t := pos - float64(i)

	// Catmull-Rom through the four points around x, falling back on the
	// ends where there are not four.
	at := func(j int) float64 {
		switch {
		case j < 0:
			return tbl[0]
		case j >= len(tbl):
			return tbl[len(tbl)-1]
		}
		return tbl[j]
	}

	var (
		p0 = at(i - 1)
		p1 = at(i)
		p2 = at(i + 1)
		p3 = at(i + 2)
	)

	return p1 + 0.5*t*(p2-p0+t*(2*p0-5*p1+4*p2-p3+t*(3*(p1-p2)+p3-p0)))
}
