// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package minuit

import "math"

// param is one parameter of the function being minimised.
//
// A parameter has two values: the external one, which is what the function is
// given and what a caller asks about, and the internal one, which is what the
// minimiser varies. For a parameter with no limits the two are the same. For
// one with limits they are related by the sine transformation below, which
// lets the minimiser work on an unbounded variable and still never offer the
// function a value outside the limits.
type param struct {
	name  string
	val   float64 // external value
	err   float64 // step size, and then the parabolic uncertainty
	lo    float64
	hi    float64
	lim   bool
	fixed bool

	// the uncertainties MINOS found either side of the minimum.
	eplus  float64
	eminus float64
}

// int2ext maps an internal value onto the external one.
//
// For a parameter limited to [a, b] the map is
//
//	Pext = a + (b-a)/2 * (sin(Pint) + 1)
//
// which is the transformation described in the MINUIT manual. It sends the
// whole real line into the open interval, so the function is never called
// outside the limits, and it is smooth, so the minimiser can still use
// derivatives.
func (p *param) int2ext(v float64) float64 {
	if !p.lim {
		return v
	}
	return p.lo + 0.5*(p.hi-p.lo)*(math.Sin(v)+1)
}

// ext2int maps an external value onto the internal one, inverting int2ext:
//
//	Pint = arcsin( 2*(Pext-a)/(b-a) - 1 )
//
// A value at or beyond a limit has no finite internal value, so it is held
// just inside, where the derivative is still non-zero and the minimiser can
// move away from the edge.
func (p *param) ext2int(v float64) float64 {
	if !p.lim {
		return v
	}

	const edge = 1 - 1e-12

	d := p.hi - p.lo
	if d == 0 {
		return 0
	}

	s := 2*(v-p.lo)/d - 1
	s = math.Min(math.Max(s, -edge), +edge)
	return math.Asin(s)
}

// dExtdInt returns the derivative of the external value with respect to the
// internal one, which is what turns a gradient in internal units into one in
// external units and back:
//
//	dPext/dPint = (b-a)/2 * cos(Pint)
func (p *param) dExtdInt(v float64) float64 {
	if !p.lim {
		return 1
	}
	return 0.5 * (p.hi - p.lo) * math.Cos(v)
}
