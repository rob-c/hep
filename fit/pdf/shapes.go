// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"fmt"
	"math"

	"go-hep.org/x/hep/hbook"
	"go-hep.org/x/hep/internal/hepmath"
	"go-hep.org/x/hep/internal/rexpr"
)

// --- Breit-Wigner ---

type breitWigner struct{}

// BreitWigner returns the non-relativistic Breit-Wigner density, the shape of
// a resonance, with parameters mean and width.
//
// The width is the full width at half maximum. Like the Cauchy distribution
// it is, it has no mean and no variance: the tails go like 1/x^2, so what a
// fit gets out of it depends on where the range was cut.
func BreitWigner() PDF { return breitWigner{} }

func (breitWigner) Name() string       { return "breit-wigner" }
func (breitWigner) ParNames() []string { return []string{"mean", "width"} }
func (breitWigner) NPar() int          { return 2 }

func (breitWigner) Shape(x float64, par []float64) float64 {
	g := par[1]
	if g <= 0 {
		return 0
	}
	d := x - par[0]
	return 1 / (d*d + 0.25*g*g)
}

// Integral is exact: the integral of a Breit-Wigner is an arctangent.
func (breitWigner) Integral(lo, hi float64, par []float64) float64 {
	var (
		m = par[0]
		g = par[1]
	)
	if g <= 0 {
		return 0
	}
	h := 0.5 * g
	return (math.Atan((hi-m)/h) - math.Atan((lo-m)/h)) / h
}

// --- bifurcated gaussian ---

type bifurGauss struct{}

// BifurGauss returns a gaussian with a different width either side of its
// peak, with parameters mean, sigmaL and sigmaR.
//
// It is the cheapest way to describe a peak that is lopsided, and unlike a
// Crystal Ball it says nothing about why.
func BifurGauss() PDF { return bifurGauss{} }

func (bifurGauss) Name() string       { return "bifurcated-gaussian" }
func (bifurGauss) ParNames() []string { return []string{"mean", "sigmaL", "sigmaR"} }
func (bifurGauss) NPar() int          { return 3 }

func (bifurGauss) Shape(x float64, par []float64) float64 {
	var (
		mean = par[0]
		sig  = par[1]
	)
	if x >= mean {
		sig = par[2]
	}
	if sig <= 0 {
		return 0
	}
	d := (x - mean) / sig
	return math.Exp(-0.5 * d * d)
}

// Integral is exact: each side is half an error function.
func (bifurGauss) Integral(lo, hi float64, par []float64) float64 {
	var (
		mean = par[0]
		sl   = par[1]
		sr   = par[2]
	)
	if sl <= 0 || sr <= 0 {
		return 0
	}

	// the integral of exp(-((x-m)/s)^2/2) from a to b.
	half := func(a, b, s float64) float64 {
		if b <= a {
			return 0
		}
		const sqrt2 = math.Sqrt2
		return s * math.Sqrt(math.Pi/2) *
			(math.Erf((b-mean)/(s*sqrt2)) - math.Erf((a-mean)/(s*sqrt2)))
	}

	return half(lo, math.Min(hi, mean), sl) + half(math.Max(lo, mean), hi, sr)
}

// --- ARGUS ---

type argus struct{}

// Argus returns the ARGUS background shape, with parameters m0, c and p.
//
// It describes the combinatorial background under a peak near a kinematic
// limit: it rises from zero, turns over, and stops dead at m0.
func Argus() PDF { return argus{} }

func (argus) Name() string       { return "argus" }
func (argus) ParNames() []string { return []string{"m0", "c", "p"} }
func (argus) NPar() int          { return 3 }

func (argus) Shape(x float64, par []float64) float64 {
	var (
		m0 = par[0]
		c  = par[1]
		p  = par[2]
	)
	if m0 <= 0 || x <= 0 || x >= m0 {
		return 0
	}

	t := x / m0
	u := 1 - t*t
	if u <= 0 {
		return 0
	}

	return x * math.Pow(u, p) * math.Exp(c*u)
}

// Integral is by quadrature: the closed form exists only for particular
// powers, and a fit moves the power.
func (a argus) Integral(lo, hi float64, par []float64) float64 {
	hi = math.Min(hi, par[0])
	if hi <= lo {
		return 0
	}
	return simpson(func(x float64) float64 { return a.Shape(x, par) }, lo, hi)
}

// --- Landau ---

type landau struct{}

// Landau returns the Landau density, with parameters location and scale.
//
// It is the energy a charged particle loses crossing a thin layer of matter,
// and the reason a calorimeter's response has the tail it does. The location
// is not the mean: a Landau has no mean, and no variance either.
func Landau() PDF { return landau{} }

func (landau) Name() string       { return "landau" }
func (landau) ParNames() []string { return []string{"location", "scale"} }
func (landau) NPar() int          { return 2 }

func (landau) Shape(x float64, par []float64) float64 {
	s := par[1]
	if s <= 0 {
		return 0
	}
	return hepmath.Landau((x - par[0]) / s)
}

// Integral comes from the cumulative, so it is as exact as the density is.
func (landau) Integral(lo, hi float64, par []float64) float64 {
	s := par[1]
	if s <= 0 {
		return 0
	}
	m := par[0]
	return s * (hepmath.LandauCDF((hi-m)/s) - hepmath.LandauCDF((lo-m)/s))
}

// --- Chebychev ---

type chebychev struct {
	n      int
	lo, hi float64
}

// Chebychev returns a Chebychev polynomial of the first kind of degree n over
// [lo, hi], with the n coefficients of T_1 to T_n as its parameters.
//
// The constant term is fixed at one, as in RooChebychev, since a density is
// normalised anyway and leaving it free would only duplicate the yield.
//
// Chebychev needs its range up front because that is what the polynomial is
// defined over: the coefficients mean nothing without it. It is a better
// behaved background than a plain polynomial of the same degree, its
// coefficients being far less correlated.
func Chebychev(n int, lo, hi float64) PDF {
	if n < 1 {
		panic(fmt.Errorf("pdf: a Chebychev needs a degree of at least 1, got %d", n))
	}
	if hi <= lo {
		panic(fmt.Errorf("pdf: a Chebychev needs lo < hi, got [%v, %v]", lo, hi))
	}
	return chebychev{n: n, lo: lo, hi: hi}
}

func (c chebychev) Name() string { return fmt.Sprintf("chebychev%d", c.n) }

func (c chebychev) ParNames() []string {
	o := make([]string, c.n)
	for i := range o {
		o[i] = fmt.Sprintf("c%d", i+1)
	}
	return o
}

func (c chebychev) NPar() int { return c.n }

func (c chebychev) Shape(x float64, par []float64) float64 {
	// onto [-1, 1], which is where the polynomials live.
	y := 2*(x-c.lo)/(c.hi-c.lo) - 1

	// T_0 = 1, T_1 = y, T_{k+1} = 2y*T_k - T_{k-1}.
	var (
		o   = 1.0
		tm1 = 1.0
		t   = y
	)
	for i := range c.n {
		o += par[i] * t
		t, tm1 = 2*y*t-tm1, t
	}
	return o
}

func (c chebychev) Integral(lo, hi float64, par []float64) float64 {
	return simpson(func(x float64) float64 { return c.Shape(x, par) }, lo, hi)
}

// --- Poisson ---

type poisson struct{}

// Poisson returns the Poisson probability of x counts given a mean, with the
// one parameter mean.
//
// x is rounded to the nearest whole number, counts being what they are.
func Poisson() PDF { return poisson{} }

func (poisson) Name() string       { return "poisson" }
func (poisson) ParNames() []string { return []string{"mean"} }
func (poisson) NPar() int          { return 1 }

func (poisson) Shape(x float64, par []float64) float64 {
	mu := par[0]
	k := math.Round(x)
	if mu <= 0 || k < 0 {
		return 0
	}

	// through the log, so that a large mean does not overflow the factorial
	// on the way to a perfectly ordinary probability.
	lg, _ := math.Lgamma(k + 1)
	return math.Exp(k*math.Log(mu) - mu - lg)
}

// Integral sums the probabilities of the whole numbers in the range, a
// Poisson being a distribution over counts rather than a density.
func (p poisson) Integral(lo, hi float64, par []float64) float64 {
	var (
		k0  = math.Max(0, math.Ceil(lo))
		sum float64
	)
	for k := k0; k <= hi; k++ {
		sum += p.Shape(k, par)
		if k > 1e6 {
			break
		}
	}
	return sum
}

// --- a density written as a formula ---

type formula struct {
	src  string
	expr *rexpr.Expr
	pars []string
}

// Formula returns a density written as an expression in x and the named
// parameters, as RooGenericPdf is.
//
// The expression is the language rdraw and rdf take: arithmetic, the usual
// comparisons, and maths functions under their ROOT names or their Go ones.
//
//	pdf.Formula("exp(-x/tau) * x*x", []string{"tau"})
//
// Formula returns an error for an expression that cannot be parsed, or that
// reads a name which is neither x nor one of the parameters.
func Formula(expr string, pars []string) (PDF, error) {
	e, err := rexpr.New(expr)
	if err != nil {
		return nil, fmt.Errorf("pdf: could not parse %q: %w", expr, err)
	}

	known := map[string]bool{"x": true}
	for _, p := range pars {
		known[p] = true
	}
	for _, id := range e.Idents() {
		if !known[id] {
			return nil, fmt.Errorf(
				"pdf: %q reads %q, which is neither x nor one of the parameters %v",
				expr, id, pars,
			)
		}
	}

	return &formula{src: expr, expr: e, pars: pars}, nil
}

func (f *formula) Name() string       { return "formula" }
func (f *formula) ParNames() []string { return f.pars }
func (f *formula) NPar() int          { return len(f.pars) }

func (f *formula) Shape(x float64, par []float64) float64 {
	vals := make(map[string]float64, len(f.pars)+1)
	vals["x"] = x
	for i, name := range f.pars {
		vals[name] = par[i]
	}

	v, err := f.expr.Eval(vals)
	if err != nil {
		return 0
	}
	return v
}

// Integral is by quadrature: nothing is known about the expression.
func (f *formula) Integral(lo, hi float64, par []float64) float64 {
	return simpson(func(x float64) float64 { return f.Shape(x, par) }, lo, hi)
}

// --- a density taken from a histogram ---

type histPDF struct {
	h *hbook.H1D
}

// Hist returns a density taken from a histogram, as RooHistPdf is: the
// template shapes people get out of simulation and fit to data.
//
// It has no parameters. The density is the bin content, held flat across each
// bin, and zero outside the histogram.
func Hist(h *hbook.H1D) PDF { return &histPDF{h: h} }

func (*histPDF) Name() string       { return "hist" }
func (*histPDF) ParNames() []string { return nil }
func (*histPDF) NPar() int          { return 0 }

func (p *histPDF) Shape(x float64, _ []float64) float64 {
	bin := p.h.Bin(x)
	if bin == nil {
		return 0
	}
	w := bin.XWidth()
	if w <= 0 {
		return 0
	}
	// divided by the width, so that the shape is a density and rebinning
	// the template does not change it.
	return bin.SumW() / w
}

func (p *histPDF) Integral(lo, hi float64, _ []float64) float64 {
	var sum float64
	for i := range p.h.Binning.Bins {
		b := &p.h.Binning.Bins[i]

		// the part of this bin inside [lo, hi].
		a := math.Max(lo, b.XMin())
		z := math.Min(hi, b.XMax())
		if z <= a {
			continue
		}

		w := b.XWidth()
		if w <= 0 {
			continue
		}
		sum += b.SumW() / w * (z - a)
	}
	return sum
}

// --- Voigtian ---

type voigtian struct{}

// Voigtian returns a Breit-Wigner convolved with a gaussian, with parameters
// mean, width and sigma.
//
// It is the shape of a resonance seen through a detector: the width is the
// resonance's own and the sigma is the resolution smearing it. Fitting the
// two separately is the point of it, and is what a plain Breit-Wigner or a
// plain gaussian cannot do.
//
// The convolution is done by quadrature at each call, so this is the most
// expensive density here by some way.
func Voigtian() PDF { return voigtian{} }

func (voigtian) Name() string       { return "voigtian" }
func (voigtian) ParNames() []string { return []string{"mean", "width", "sigma"} }
func (voigtian) NPar() int          { return 3 }

func (voigtian) Shape(x float64, par []float64) float64 {
	var (
		mean  = par[0]
		width = par[1]
		sigma = par[2]
	)
	// When one of the two is far narrower than the other it contributes
	// nothing a quadrature could resolve, and trying anyway is how this
	// goes wrong: a Lorentzian a millionth as wide as the grid spacing
	// falls between the nodes, and the answer is whatever it happened to
	// miss. Below a thousandth the convolution is the wider shape to
	// better than a part in a thousand, so say so outright.
	const degenerate = 1e-3

	switch {
	case sigma <= 0 || sigma < degenerate*width:
		return breitWigner{}.Shape(x, []float64{mean, width})
	case width <= 0 || width < degenerate*sigma:
		d := (x - mean) / sigma
		return math.Exp(-0.5*d*d) / (sigma * math.Sqrt(2*math.Pi))
	}

	// V(x) = integral of G(t; sigma) * BW(x - t; width) dt.
	//
	// The gaussian truncates the integral, so eight sigma either side holds
	// everything of it that matters even though the Breit-Wigner does not
	// fall off.
	var (
		g  = 0.5 * width
		nf = 1 / (sigma * math.Sqrt(2*math.Pi)) * g / math.Pi
	)

	f := func(t float64) float64 {
		var (
			u = t / sigma
			d = x - mean - t
		)
		return math.Exp(-0.5*u*u) / (d*d + g*g)
	}

	// enough nodes that the narrower of the two shapes still gets twenty
	// across it, within reason: this is the expensive density and the
	// bound is what stops it becoming the ruinous one.
	n := int(16 * sigma / (0.05 * math.Min(sigma, g)))
	n = min(max(n, 200), 4000)

	return nf * simpsonFixed(f, -8*sigma, 8*sigma, n)
}

func (v voigtian) Integral(lo, hi float64, par []float64) float64 {
	return simpson(func(x float64) float64 { return v.Shape(x, par) }, lo, hi)
}

// simpsonFixed is Simpson's rule on n intervals, n even.
//
// The convolution above wants a rule that costs the same every time it is
// called, an adaptive one being both slower and, on a shape that a fit is
// moving around, unpredictably so.
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
