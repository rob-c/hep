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

// --- Bernstein ---

type bernstein struct {
	n      int
	lo, hi float64
	binom  []float64
}

// Bernstein returns a Bernstein polynomial of degree n over [lo, hi], with
// the n+1 coefficients as its parameters.
//
// Every basis function of a Bernstein polynomial is positive on the interval,
// so a set of non-negative coefficients gives a density that is positive
// everywhere — which a plain polynomial of the same degree does not, and
// which is why this is what a smooth background is usually written as.
//
// Its integral is trivially exact: each basis function integrates to the same
// (hi-lo)/(n+1), so the integral is that times the sum of the coefficients.
func Bernstein(n int, lo, hi float64) PDF {
	if n < 0 {
		panic(fmt.Errorf("pdf: a Bernstein polynomial needs a degree of at least 0, got %d", n))
	}
	if hi <= lo {
		panic(fmt.Errorf("pdf: a Bernstein polynomial needs lo < hi, got [%v, %v]", lo, hi))
	}

	// the binomial coefficients, by Pascal's rule rather than by factorials,
	// which overflow long before the degree gets interesting.
	binom := make([]float64, n+1)
	binom[0] = 1
	for i := 1; i <= n; i++ {
		for j := i; j > 0; j-- {
			binom[j] += binom[j-1]
		}
	}

	return bernstein{n: n, lo: lo, hi: hi, binom: binom}
}

func (b bernstein) Name() string { return fmt.Sprintf("bernstein%d", b.n) }

func (b bernstein) ParNames() []string {
	o := make([]string, b.n+1)
	for i := range o {
		o[i] = fmt.Sprintf("b%d", i)
	}
	return o
}

func (b bernstein) NPar() int { return b.n + 1 }

func (b bernstein) Shape(x float64, par []float64) float64 {
	t := (x - b.lo) / (b.hi - b.lo)
	if t < 0 || t > 1 {
		return 0
	}

	var (
		o float64
		u = 1.0 // t^i
	)
	for i := range b.n + 1 {
		o += par[i] * b.binom[i] * u * math.Pow(1-t, float64(b.n-i))
		u *= t
	}
	if o < 0 {
		return 0
	}
	return o
}

// Integral is exact over the whole interval and by quadrature over part of
// it, a partial Bernstein integral having no such tidy form.
func (b bernstein) Integral(lo, hi float64, par []float64) float64 {
	if lo <= b.lo && hi >= b.hi {
		var sum float64
		for i := range b.n + 1 {
			sum += par[i]
		}
		return sum * (b.hi - b.lo) / float64(b.n+1)
	}
	return simpson(func(x float64) float64 { return b.Shape(x, par) }, lo, hi)
}

// --- kernel density estimate ---

type keys struct {
	grid   []float64
	lo, hi float64
	step   float64
}

// Keys returns a kernel density estimate of a sample, as RooKeysPdf does: a
// smooth density built from events without binning them.
//
// It is how a template is made from a simulation too small to bin finely: a
// gaussian is laid over each event and the sum of them is the density.
//
// The bandwidth is Silverman's rule, 1.06 * sigma * n^(-1/5), times the scale
// given — one for the rule as it stands, more to smooth further, less to
// follow the sample more closely. The estimate is built onto a grid once and
// read off it after, since summing over every event at every call would make
// it useless to fit with.
//
// A kernel density estimate leaks across the ends of the range, where there
// is no sample to balance it. Keys reflects the sample in both ends, which
// holds the density up where it would otherwise sag.
func Keys(data []float64, lo, hi float64, scale float64) (PDF, error) {
	switch {
	case len(data) < 2:
		return nil, fmt.Errorf("pdf: a kernel density estimate needs at least two events, got %d", len(data))
	case hi <= lo:
		return nil, fmt.Errorf("pdf: a kernel density estimate over [%v, %v] has no range", lo, hi)
	case scale <= 0:
		return nil, fmt.Errorf("pdf: the bandwidth scale must be positive, got %v", scale)
	}

	// the sample's own width, for Silverman's rule.
	var mean, m2 float64
	for _, x := range data {
		mean += x
	}
	mean /= float64(len(data))
	for _, x := range data {
		d := x - mean
		m2 += d * d
	}

	sigma := math.Sqrt(m2 / float64(len(data)-1))
	if sigma <= 0 {
		return nil, fmt.Errorf("pdf: every event is at %v: there is nothing to smooth", mean)
	}

	h := scale * 1.06 * sigma * math.Pow(float64(len(data)), -0.2)

	const n = 2048
	k := &keys{
		grid: make([]float64, n+1),
		lo:   lo,
		hi:   hi,
		step: (hi - lo) / n,
	}

	// Each event contributes a gaussian, and so do its two reflections in
	// the ends of the range: without them the estimate falls away at the
	// edges, where a real density need not.
	add := func(x float64) {
		// only the grid within six bandwidths feels it.
		var (
			i0 = max(0, int((x-6*h-lo)/k.step))
			i1 = min(n, int((x+6*h-lo)/k.step)+1)
		)
		for i := i0; i <= i1; i++ {
			d := (lo + float64(i)*k.step - x) / h
			k.grid[i] += math.Exp(-0.5 * d * d)
		}
	}

	for _, x := range data {
		add(x)
		add(2*lo - x)
		add(2*hi - x)
	}

	norm := float64(len(data)) * h * math.Sqrt(2*math.Pi)
	for i := range k.grid {
		k.grid[i] /= norm
	}

	return k, nil
}

func (*keys) Name() string       { return "keys" }
func (*keys) ParNames() []string { return nil }
func (*keys) NPar() int          { return 0 }

func (k *keys) Shape(x float64, _ []float64) float64 {
	if x < k.lo || x > k.hi {
		return 0
	}

	var (
		pos = (x - k.lo) / k.step
		i   = int(pos)
	)
	if i >= len(k.grid)-1 {
		return k.grid[len(k.grid)-1]
	}

	t := pos - float64(i)
	return k.grid[i]*(1-t) + k.grid[i+1]*t
}

func (k *keys) Integral(lo, hi float64, _ []float64) float64 {
	var sum float64
	for i := range len(k.grid) - 1 {
		var (
			a = k.lo + float64(i)*k.step
			b = a + k.step
		)
		a = math.Max(a, lo)
		b = math.Min(b, hi)
		if b <= a {
			continue
		}
		// the trapezium over the part of this cell that is inside.
		sum += 0.5 * (k.Shape(a, nil) + k.Shape(b, nil)) * (b - a)
	}
	return sum
}
