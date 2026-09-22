// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package pdf fits probability densities to data by maximum likelihood, in
// the manner of RooFit.
//
//	model := pdf.Add(
//		[]pdf.PDF{pdf.Gaussian(), pdf.Exponential()},
//		[]string{"nsig", "nbkg"},
//	)
//
//	res, err := pdf.FitUnbinned(data, model, 0, 10, []minuit.Par{...})
//
// What separates this from least squares is normalisation: a probability
// density has to integrate to one over the range it is fitted in, and it is
// that which lets the fit decide how much of a sample belongs to each
// component rather than only what shape it has.
//
// # Normalisation
//
// Each density here knows its own integral in closed form where one exists,
// which is exact and cheap. A density that does not falls back on adaptive
// Simpson quadrature. Composing densities composes their integrals, so a sum
// of a gaussian and an exponential is still normalised exactly.
//
// # The likelihood
//
// An unbinned fit minimises the negative log-likelihood
//
//	-ln L = - sum over the data of ln f(x)
//
// and an extended one adds the Poisson term that ties the total yield to the
// number of events seen:
//
//	-ln L = nu - N ln nu - sum over the data of ln f(x)
//
// The minimiser is told UP = 0.5, which is what makes a rise of half a unit
// in -ln L one standard deviation, so the uncertainties mean what they do
// everywhere else.
package pdf // import "go-hep.org/x/hep/fit/pdf"

import (
	"fmt"
	"math"
)

// PDF is a probability density over one observable.
//
// Shape is the density before normalisation and Integral is its integral, so
// that Eval can divide one by the other. Splitting them that way is what lets
// a density give its integral in closed form.
type PDF interface {
	// Name says what kind of density this is.
	Name() string

	// ParNames names the parameters, in the order Shape expects them.
	ParNames() []string

	// NPar returns how many parameters Shape takes.
	NPar() int

	// Shape returns the unnormalised density at x.
	Shape(x float64, par []float64) float64

	// Integral returns the integral of Shape over [lo, hi].
	Integral(lo, hi float64, par []float64) float64
}

// Eval returns the density at x, normalised over [lo, hi].
func Eval(p PDF, x, lo, hi float64, par []float64) float64 {
	norm := p.Integral(lo, hi, par)
	if norm <= 0 || math.IsNaN(norm) || math.IsInf(norm, 0) {
		return 0
	}
	return p.Shape(x, par) / norm
}

// Func returns the normalised density as a function of x alone, ready to be
// drawn beside the data it was fitted to.
//
// scale multiplies the result, which is what turns a density into something
// that sits on top of a histogram: pass the number of entries times the bin
// width.
func Func(p PDF, lo, hi float64, par []float64, scale float64) func(float64) float64 {
	norm := p.Integral(lo, hi, par)
	if norm <= 0 {
		return func(float64) float64 { return 0 }
	}
	return func(x float64) float64 { return scale * p.Shape(x, par) / norm }
}

// --- the densities ---

type gaussian struct{}

// Gaussian returns the normal density, with parameters mean and sigma.
func Gaussian() PDF { return gaussian{} }

func (gaussian) Name() string       { return "gaussian" }
func (gaussian) ParNames() []string { return []string{"mean", "sigma"} }
func (gaussian) NPar() int          { return 2 }

func (gaussian) Shape(x float64, par []float64) float64 {
	sigma := par[1]
	if sigma <= 0 {
		return 0
	}
	d := (x - par[0]) / sigma
	return math.Exp(-0.5 * d * d)
}

// Integral is exact: the integral of a gaussian is an error function.
func (gaussian) Integral(lo, hi float64, par []float64) float64 {
	var (
		mean  = par[0]
		sigma = par[1]
	)
	if sigma <= 0 {
		return 0
	}
	const sqrt2 = math.Sqrt2
	a := (lo - mean) / (sigma * sqrt2)
	b := (hi - mean) / (sigma * sqrt2)
	return sigma * math.Sqrt(math.Pi/2) * (math.Erf(b) - math.Erf(a))
}

type exponential struct{}

// Exponential returns exp(slope*x), with the one parameter slope.
//
// A negative slope is the falling background every mass spectrum has.
func Exponential() PDF { return exponential{} }

func (exponential) Name() string       { return "exponential" }
func (exponential) ParNames() []string { return []string{"slope"} }
func (exponential) NPar() int          { return 1 }

func (exponential) Shape(x float64, par []float64) float64 {
	return math.Exp(par[0] * x)
}

// Integral is exact, and handles the flat case a slope of zero gives.
func (exponential) Integral(lo, hi float64, par []float64) float64 {
	k := par[0]
	if k == 0 {
		return hi - lo
	}
	return (math.Exp(k*hi) - math.Exp(k*lo)) / k
}

type uniform struct{}

// Uniform returns a flat density, with no parameters at all.
func Uniform() PDF { return uniform{} }

func (uniform) Name() string                     { return "uniform" }
func (uniform) ParNames() []string               { return nil }
func (uniform) NPar() int                        { return 0 }
func (uniform) Shape(float64, []float64) float64 { return 1 }
func (uniform) Integral(lo, hi float64, _ []float64) float64 {
	return hi - lo
}

type polynomial struct{ n int }

// Polynomial returns a polynomial of degree n, with par[i] the coefficient
// of x^i.
//
// It is not constrained to stay positive: a fit that wanders somewhere it
// goes negative will find the likelihood undefined there and be pushed back.
func Polynomial(n int) PDF { return polynomial{n: n} }

func (p polynomial) Name() string { return fmt.Sprintf("pol%d", p.n) }

func (p polynomial) ParNames() []string {
	o := make([]string, p.n+1)
	for i := range o {
		o[i] = fmt.Sprintf("a%d", i)
	}
	return o
}

func (p polynomial) NPar() int { return p.n + 1 }

func (p polynomial) Shape(x float64, par []float64) float64 {
	var (
		o float64
		v = 1.0
	)
	for i := range p.n + 1 {
		o += par[i] * v
		v *= x
	}
	return o
}

// Integral is exact: a polynomial integrates term by term.
func (p polynomial) Integral(lo, hi float64, par []float64) float64 {
	var o float64
	for i := range p.n + 1 {
		n := float64(i + 1)
		o += par[i] * (math.Pow(hi, n) - math.Pow(lo, n)) / n
	}
	return o
}

type crystalBall struct{}

// CrystalBall returns the Crystal Ball density: a gaussian core with a power
// law tail below it, which is what a calorimeter does to a peak.
//
// Its parameters are mean, sigma, alpha and n: alpha is where the tail takes
// over, in units of sigma below the mean, and n is how steeply it falls.
func CrystalBall() PDF { return crystalBall{} }

func (crystalBall) Name() string       { return "crystal-ball" }
func (crystalBall) ParNames() []string { return []string{"mean", "sigma", "alpha", "n"} }
func (crystalBall) NPar() int          { return 4 }

func (crystalBall) Shape(x float64, par []float64) float64 {
	var (
		mean  = par[0]
		sigma = par[1]
		alpha = math.Abs(par[2])
		n     = par[3]
	)
	if sigma <= 0 || alpha <= 0 || n <= 1 {
		return 0
	}

	t := (x - mean) / sigma
	if t > -alpha {
		return math.Exp(-0.5 * t * t)
	}

	a := math.Pow(n/alpha, n) * math.Exp(-0.5*alpha*alpha)
	b := n/alpha - alpha
	return a / math.Pow(b-t, n)
}

// Integral falls back on quadrature: the closed form exists but is delicate
// where alpha or n approach the values that make it degenerate, and a fit
// walks through exactly those.
func (c crystalBall) Integral(lo, hi float64, par []float64) float64 {
	return simpson(func(x float64) float64 { return c.Shape(x, par) }, lo, hi)
}

// --- composition ---

// Sum is several densities added together.
type Sum struct {
	pdfs     []PDF
	names    []string
	extended bool
}

// Add returns the sum of several densities.
//
// names gives each component's coefficient a name. With one name per
// component the coefficients are yields and the fit is extended: it fits how
// many events belong to each. With one fewer name than components they are
// fractions, the last being whatever is left over, and the fit says only what
// the mixture is.
func Add(pdfs []PDF, names []string) *Sum {
	switch {
	case len(names) == len(pdfs):
		return &Sum{pdfs: pdfs, names: names, extended: true}
	case len(names) == len(pdfs)-1:
		return &Sum{pdfs: pdfs, names: names, extended: false}
	}
	panic(fmt.Errorf(
		"pdf: %d densities need %d or %d coefficient names, got %d",
		len(pdfs), len(pdfs), len(pdfs)-1, len(names),
	))
}

func (s *Sum) Name() string { return "sum" }

// Extended says whether the coefficients of this sum are yields rather than
// fractions.
func (s *Sum) Extended() bool { return s.extended }

// ParNames lists the coefficients first, then each component's own
// parameters, which is the order Shape expects them in.
func (s *Sum) ParNames() []string {
	o := append([]string(nil), s.names...)
	for _, p := range s.pdfs {
		o = append(o, p.ParNames()...)
	}
	return o
}

func (s *Sum) NPar() int {
	n := len(s.names)
	for _, p := range s.pdfs {
		n += p.NPar()
	}
	return n
}

// coeffs returns the coefficient of each component, the last being whatever
// is left over when they are fractions.
func (s *Sum) coeffs(par []float64) []float64 {
	o := make([]float64, len(s.pdfs))
	copy(o, par[:len(s.names)])

	if !s.extended {
		var sum float64
		for _, v := range o[:len(s.names)] {
			sum += v
		}
		o[len(o)-1] = 1 - sum
	}
	return o
}

// split hands each component its own parameters.
func (s *Sum) split(par []float64) [][]float64 {
	o := make([][]float64, len(s.pdfs))
	at := len(s.names)
	for i, p := range s.pdfs {
		o[i] = par[at : at+p.NPar()]
		at += p.NPar()
	}
	return o
}

func (s *Sum) Shape(x float64, par []float64) float64 {
	var (
		cs = s.coeffs(par)
		ps = s.split(par)
		o  float64
	)
	for i, p := range s.pdfs {
		o += cs[i] * p.Shape(x, ps[i])
	}
	return o
}

// Integral adds the components' integrals, each weighted by its coefficient,
// so a sum of densities with closed forms still has one.
func (s *Sum) Integral(lo, hi float64, par []float64) float64 {
	var (
		cs = s.coeffs(par)
		ps = s.split(par)
		o  float64
	)
	for i, p := range s.pdfs {
		o += cs[i] * p.Integral(lo, hi, ps[i])
	}
	return o
}

// NormShape returns the density with each component normalised on its own
// before being added, which is what makes a coefficient mean "this many of
// the events" rather than "this much of the unnormalised height".
func (s *Sum) NormShape(x, lo, hi float64, par []float64) float64 {
	var (
		cs = s.coeffs(par)
		ps = s.split(par)
		o  float64
	)
	for i, p := range s.pdfs {
		n := p.Integral(lo, hi, ps[i])
		if n <= 0 {
			continue
		}
		o += cs[i] * p.Shape(x, ps[i]) / n
	}
	return o
}

// evalAt returns the normalised density of the sum at x.
//
// A coefficient multiplies a component that has been normalised first,
// whether it is a yield or a fraction, so that it counts events either way.
// Shape cannot do this: normalising a component needs the range, and Shape
// is not given one.
func (s *Sum) evalAt(x, lo, hi float64, par []float64) float64 {
	v := s.NormShape(x, lo, hi, par)
	if !s.extended {
		return v
	}

	nu := s.Yield(par)
	if nu <= 0 {
		return 0
	}
	return v / nu
}

// Yield returns the total number of events a sum expects, which is the sum
// of its coefficients when they are yields and meaningless when they are
// fractions.
func (s *Sum) Yield(par []float64) float64 {
	var o float64
	for _, v := range s.coeffs(par) {
		o += v
	}
	return o
}

// simpson integrates f over [lo,hi] by adaptive Simpson quadrature.
//
// The densities that need it are peaked, so a fixed rule would either waste
// its evaluations on the flat parts or miss the peak: splitting where the
// estimate disagrees with itself puts them where the function is doing
// something.
func simpson(f func(float64) float64, lo, hi float64) float64 {
	const (
		tol   = 1e-10
		depth = 50
	)

	simp := func(a, b float64, fa, fm, fb float64) float64 {
		return (b - a) / 6 * (fa + 4*fm + fb)
	}

	var rec func(a, b, fa, fm, fb, whole float64, n int) float64
	rec = func(a, b, fa, fm, fb, whole float64, n int) float64 {
		m := 0.5 * (a + b)
		lm := 0.5 * (a + m)
		rm := 0.5 * (m + b)

		flm := f(lm)
		frm := f(rm)

		left := simp(a, m, fa, flm, fm)
		right := simp(m, b, fm, frm, fb)

		if n <= 0 || math.Abs(left+right-whole) <= 15*tol {
			return left + right + (left+right-whole)/15
		}
		return rec(a, m, fa, flm, fm, left, n-1) + rec(m, b, fm, frm, fb, right, n-1)
	}

	m := 0.5 * (lo + hi)
	fa, fm, fb := f(lo), f(m), f(hi)
	return rec(lo, hi, fa, fm, fb, simp(lo, hi, fa, fm, fb), depth)
}
