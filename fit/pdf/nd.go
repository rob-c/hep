// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"fmt"
	"math"

	"go-hep.org/x/hep/fit/minuit"
)

// PDFND is a probability density over several observables.
//
// It is the other half of this package: PDF describes one observable and is
// what most fits need, and PDFND describes several, which is what a fit needs
// once it wants to use more than one thing it measured about each event.
//
// A PDF becomes a PDFND through Factorise, which is also the cheapest way to
// build one: a density that factorises has an integral that factorises too,
// and that integral is what a normalisation costs.
type PDFND interface {
	// Name says what kind of density this is.
	Name() string

	// NDim returns how many observables it is over.
	NDim() int

	// ParNames names the parameters, in the order Shape expects them.
	ParNames() []string

	// NPar returns how many parameters Shape takes.
	NPar() int

	// Shape returns the unnormalised density at x, which has NDim values.
	Shape(x []float64, par []float64) float64

	// Integral returns the integral of Shape over the box [lo, hi].
	Integral(lo, hi []float64, par []float64) float64
}

// EvalND returns the density at x, normalised over the box [lo, hi].
func EvalND(p PDFND, x, lo, hi []float64, par []float64) float64 {
	norm := p.Integral(lo, hi, par)
	if norm <= 0 || math.IsNaN(norm) || math.IsInf(norm, 0) {
		return 0
	}
	return p.Shape(x, par) / norm
}

// --- factorised products ---

type factorised struct {
	pdfs []PDF
}

// Factorise returns the density f0(x0) * f1(x1) * ..., one factor for each
// observable, as RooProdPdf does when its factors depend on different
// observables.
//
// This is the multi-dimensional model worth having. Its integral over a box
// is the product of the factors' integrals, each of which the factor knows in
// closed form or can do cheaply in one dimension, so normalising it costs
// nothing like what integrating a general density over a box would.
//
// Two observables that are not independent cannot be written this way, which
// is the assumption being made and the one worth checking before relying on
// the answer.
func Factorise(pdfs ...PDF) PDFND {
	if len(pdfs) == 0 {
		panic("pdf: a factorised density needs at least one factor")
	}
	return &factorised{pdfs: pdfs}
}

func (p *factorised) Name() string { return "factorised" }
func (p *factorised) NDim() int    { return len(p.pdfs) }

func (p *factorised) ParNames() []string {
	var o []string
	for i, f := range p.pdfs {
		for _, name := range f.ParNames() {
			o = append(o, fmt.Sprintf("%s%d", name, i))
		}
	}
	return o
}

func (p *factorised) NPar() int {
	n := 0
	for _, f := range p.pdfs {
		n += f.NPar()
	}
	return n
}

func (p *factorised) split(par []float64) [][]float64 {
	o := make([][]float64, len(p.pdfs))
	at := 0
	for i, f := range p.pdfs {
		o[i] = par[at : at+f.NPar()]
		at += f.NPar()
	}
	return o
}

func (p *factorised) Shape(x []float64, par []float64) float64 {
	if len(x) != len(p.pdfs) {
		return 0
	}

	var (
		ps = p.split(par)
		o  = 1.0
	)
	for i, f := range p.pdfs {
		o *= f.Shape(x[i], ps[i])
	}
	return o
}

// Integral is the product of the factors' integrals, which is exact.
func (p *factorised) Integral(lo, hi []float64, par []float64) float64 {
	if len(lo) != len(p.pdfs) || len(hi) != len(p.pdfs) {
		return 0
	}

	var (
		ps = p.split(par)
		o  = 1.0
	)
	for i, f := range p.pdfs {
		o *= f.Integral(lo[i], hi[i], ps[i])
	}
	return o
}

// Factors returns the one-dimensional densities this was built from.
func (p *factorised) Factors() []PDF { return p.pdfs }

// --- sums in several dimensions ---

// SumND is several multi-dimensional densities added together.
type SumND struct {
	pdfs     []PDFND
	names    []string
	extended bool
	ndim     int
}

// AddND returns the sum of several multi-dimensional densities, as Add does
// in one.
//
// With one name per component the coefficients are yields and the fit is
// extended; with one fewer they are fractions.
func AddND(pdfs []PDFND, names []string) *SumND {
	if len(pdfs) == 0 {
		panic("pdf: a sum needs at least one density")
	}

	ndim := pdfs[0].NDim()
	for _, p := range pdfs {
		if p.NDim() != ndim {
			panic(fmt.Errorf(
				"pdf: cannot add a density over %d observables to one over %d",
				p.NDim(), ndim,
			))
		}
	}

	switch {
	case len(names) == len(pdfs):
		return &SumND{pdfs: pdfs, names: names, extended: true, ndim: ndim}
	case len(names) == len(pdfs)-1:
		return &SumND{pdfs: pdfs, names: names, extended: false, ndim: ndim}
	}
	panic(fmt.Errorf(
		"pdf: %d densities need %d or %d coefficient names, got %d",
		len(pdfs), len(pdfs), len(pdfs)-1, len(names),
	))
}

func (s *SumND) Name() string { return "sum" }
func (s *SumND) NDim() int    { return s.ndim }

// Extended says whether the coefficients are yields rather than fractions.
func (s *SumND) Extended() bool { return s.extended }

func (s *SumND) ParNames() []string {
	o := append([]string(nil), s.names...)
	for _, p := range s.pdfs {
		o = append(o, p.ParNames()...)
	}
	return o
}

func (s *SumND) NPar() int {
	n := len(s.names)
	for _, p := range s.pdfs {
		n += p.NPar()
	}
	return n
}

func (s *SumND) coeffs(par []float64) []float64 {
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

func (s *SumND) split(par []float64) [][]float64 {
	o := make([][]float64, len(s.pdfs))
	at := len(s.names)
	for i, p := range s.pdfs {
		o[i] = par[at : at+p.NPar()]
		at += p.NPar()
	}
	return o
}

func (s *SumND) Shape(x []float64, par []float64) float64 {
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

func (s *SumND) Integral(lo, hi []float64, par []float64) float64 {
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
// before being added, so that a coefficient counts events.
func (s *SumND) NormShape(x, lo, hi []float64, par []float64) float64 {
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

// evalAt returns the normalised density of the sum at x, its coefficients
// multiplying components that have been normalised first.
func (s *SumND) evalAt(x, lo, hi []float64, par []float64) float64 {
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

// Yield returns the total number of events the coefficients ask for.
func (s *SumND) Yield(par []float64) float64 {
	var o float64
	for _, v := range s.coeffs(par) {
		o += v
	}
	return o
}

// Components returns the densities this sum was built from.
func (s *SumND) Components() []PDFND { return s.pdfs }

// --- fitting ---

// NLLND returns the negative log-likelihood of a multi-dimensional density
// against unbinned data over the box [lo, hi].
//
// Each event is one slice of NDim values.
func NLLND(data [][]float64, p PDFND, lo, hi []float64) minuit.FCN {
	sum, isSum := p.(*SumND)
	extended := isSum && sum.Extended()

	inside := func(x []float64) bool {
		for i, v := range x {
			if v < lo[i] || v > hi[i] {
				return false
			}
		}
		return true
	}

	return minuit.FuncOf(func(par []float64) float64 {
		var (
			nll float64
			n   int
		)

		for _, x := range data {
			if len(x) != p.NDim() || !inside(x) {
				continue
			}
			n++

			var f float64
			switch {
			case extended:
				f = sum.NormShape(x, lo, hi, par)
			case isSum:
				f = sum.evalAt(x, lo, hi, par)
			default:
				f = EvalND(p, x, lo, hi, par)
			}

			if f <= 0 || math.IsNaN(f) {
				return math.Inf(+1)
			}
			nll -= math.Log(f)
		}

		if extended {
			nu := sum.Yield(par)
			if nu <= 0 {
				return math.Inf(+1)
			}
			nll += nu
		}

		return nll
	})
}

// ResultND is what a multi-dimensional fit came to.
type ResultND struct {
	// Minuit is the minimiser that did the work.
	Minuit *minuit.Minuit

	// PDF is the density that was fitted, and Lo and Hi the box.
	PDF    PDFND
	Lo, Hi []float64

	// NLL is the negative log-likelihood at the minimum.
	NLL float64
}

// Values returns the fitted parameters.
func (r *ResultND) Values() []float64 { return r.Minuit.Values() }

// Value returns the fitted value of the i-th parameter and its uncertainty.
func (r *ResultND) Value(i int) (val, err float64) {
	v, e, _ := r.Minuit.Value(i)
	return v, e
}

// Projection returns the fitted density projected onto one observable, as a
// function of that observable alone: the other observables are integrated
// over their whole range.
//
// This is what a multi-dimensional fit is drawn as, there being no way to
// draw the thing itself.
func (r *ResultND) Projection(obs int, scale float64) (func(float64) float64, error) {
	if obs < 0 || obs >= r.PDF.NDim() {
		return nil, fmt.Errorf("pdf: no observable %d to project onto", obs)
	}

	par := r.Values()

	// A factorised density projects onto exactly its own factor, the rest
	// integrating away to a constant. That covers the models worth building
	// and costs nothing.
	if f, ok := r.PDF.(*factorised); ok {
		var (
			ps  = f.split(par)
			one = f.pdfs[obs]
			sub = ps[obs]
		)
		return Func(one, r.Lo[obs], r.Hi[obs], sub, scale), nil
	}

	// Otherwise integrate the others out numerically, one observable at a
	// time. It is slow, and it is the price of a density that does not
	// factorise.
	return func(x float64) float64 {
		v := r.project(obs, x, par)
		n := r.project(obs, math.NaN(), par) // the normalisation
		if n <= 0 {
			return 0
		}
		return scale * v / n
	}, nil
}

// project integrates the density over every observable but obs, holding that
// one at x. A NaN x asks for the integral over everything, which is the
// normalisation.
func (r *ResultND) project(obs int, x float64, par []float64) float64 {
	const n = 64

	var (
		nd = r.PDF.NDim()
		pt = make([]float64, nd)
	)

	var walk func(d int) float64
	walk = func(d int) float64 {
		if d == nd {
			return r.PDF.Shape(pt, par)
		}
		if d == obs && !math.IsNaN(x) {
			pt[d] = x
			return walk(d + 1)
		}

		var (
			lo  = r.Lo[d]
			hi  = r.Hi[d]
			dx  = (hi - lo) / n
			sum float64
		)
		for i := range n {
			pt[d] = lo + (float64(i)+0.5)*dx
			sum += walk(d+1) * dx
		}
		return sum
	}

	return walk(0)
}

// FitUnbinnedND fits a multi-dimensional density to unbinned data over the
// box [lo, hi] by maximum likelihood.
func FitUnbinnedND(data [][]float64, p PDFND, lo, hi []float64, pars []minuit.Par, opts ...FitOption) (*ResultND, error) {
	switch {
	case len(data) == 0:
		return nil, fmt.Errorf("pdf: no data to fit")
	case len(lo) != p.NDim() || len(hi) != p.NDim():
		return nil, fmt.Errorf(
			"pdf: %s is over %d observables and the box gives %d and %d",
			p.Name(), p.NDim(), len(lo), len(hi),
		)
	case len(pars) != p.NPar():
		return nil, fmt.Errorf(
			"pdf: %s takes %d parameters, got %d",
			p.Name(), p.NPar(), len(pars),
		)
	}

	for i := range lo {
		if hi[i] <= lo[i] {
			return nil, fmt.Errorf(
				"pdf: observable %d has the range [%v, %v]", i, lo[i], hi[i],
			)
		}
	}

	cfg := newFitCfg(opts)

	m := minuit.New(len(pars))
	m.SetFCN(NLLND(data, p, lo, hi))
	args := cfg.apply(m)

	err := m.SetErrorDef(errorDef)
	if err != nil {
		return nil, err
	}

	names := p.ParNames()
	for i, par := range pars {
		name := par.Name
		if name == "" && i < len(names) {
			name = names[i]
		}
		step := par.Step
		if step == 0 && par.Min == par.Max {
			step = 0.1 * math.Max(1, math.Abs(par.Value))
		}
		if err := m.Parameter(i, name, par.Value, step, par.Min, par.Max); err != nil {
			return nil, fmt.Errorf("pdf: could not set parameter %d: %w", i, err)
		}
	}

	res := &ResultND{Minuit: m, PDF: p, Lo: lo, Hi: hi}

	if err := m.Command("MIGRAD", args...); err != nil {
		return res, fmt.Errorf("pdf: MIGRAD failed: %w", err)
	}
	if m.Status() != minuit.Converged {
		return res, fmt.Errorf("pdf: the fit did not converge: %v (edm=%v)", m.Status(), m.EDM())
	}
	if err := m.Command("HESSE"); err != nil {
		return res, fmt.Errorf("pdf: HESSE failed: %w", err)
	}

	res.NLL = m.FMin()
	return res, nil
}

// GenerateND draws n events from a multi-dimensional density over the box
// [lo, hi], by accept-reject.
func GenerateND(rnd interface{ Float64() float64 }, p PDFND, lo, hi []float64, par []float64, n int) ([][]float64, error) {
	nd := p.NDim()
	switch {
	case len(lo) != nd || len(hi) != nd:
		return nil, fmt.Errorf(
			"pdf: %s is over %d observables and the box gives %d and %d",
			p.Name(), nd, len(lo), len(hi),
		)
	case n < 0:
		return nil, fmt.Errorf("pdf: cannot generate %d events", n)
	}
	for i := range lo {
		if hi[i] <= lo[i] {
			return nil, fmt.Errorf("pdf: observable %d has the range [%v, %v]", i, lo[i], hi[i])
		}
	}

	// What to sample: a sum's coefficients multiply normalised components,
	// so its raw Shape would draw the wrong mixture.
	shape := func(x []float64) float64 { return p.Shape(x, par) }
	if sum, ok := p.(*SumND); ok {
		shape = func(x []float64) float64 { return sum.evalAt(x, lo, hi, par) }
	}

	// how high the density gets, from a scatter of points: a grid would
	// cost the number of points to the power of the dimension.
	var (
		peak float64
		x    = make([]float64, nd)
	)
	for range 200000 {
		for i := range x {
			x[i] = lo[i] + rnd.Float64()*(hi[i]-lo[i])
		}
		if v := shape(x); v > peak {
			peak = v
		}
	}

	if peak <= 0 {
		return nil, fmt.Errorf(
			"pdf: %s is not positive anywhere in the box: nothing to generate",
			p.Name(),
		)
	}
	peak *= 1.2 // headroom, the scatter having probably missed the very top

	o := make([][]float64, 0, n)
	for len(o) < n {
		pt := make([]float64, nd)
		for i := range pt {
			pt[i] = lo[i] + rnd.Float64()*(hi[i]-lo[i])
		}
		if rnd.Float64()*peak < shape(pt) {
			o = append(o, pt)
		}
	}

	return o, nil
}
