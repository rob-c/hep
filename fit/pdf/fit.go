// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"fmt"
	"math"

	"go-hep.org/x/hep/fit/minuit"
	"go-hep.org/x/hep/hbook"
)

// errorDef is the rise in -ln L that makes one standard deviation. Half a
// unit is what a likelihood gives, where a chi-square gives one.
const errorDef = 0.5

// NLL returns the negative log-likelihood of a density against unbinned data
// over [lo, hi].
//
// For a plain density it is
//
//	-ln L = - sum over the data of ln f(x)
//
// and for an extended sum, whose coefficients are yields, it is
//
//	-ln L = nu - N ln nu - sum over the data of ln f(x)
//
// with nu the total yield the parameters ask for and N the number of events
// there actually are. That extra term is what lets a fit say how many events
// each component has rather than only what fraction.
func NLL(data []float64, p PDF, lo, hi float64) minuit.FCN {
	sum, isSum := p.(*Sum)
	extended := isSum && sum.Extended()

	return minuit.FuncOf(func(par []float64) float64 {
		var nll float64

		for _, x := range data {
			if x < lo || x > hi {
				continue
			}

			var f float64
			switch {
			case extended:
				// each component normalised on its own, so that a yield
				// counts events.
				f = sum.NormShape(x, lo, hi, par)
			default:
				f = Eval(p, x, lo, hi, par)
			}

			if f <= 0 || math.IsNaN(f) {
				// the parameters have taken the density somewhere it is not
				// a density at all: make it costly rather than undefined.
				return math.Inf(+1)
			}
			nll -= math.Log(f)
		}

		if extended {
			// The per-event term above already used the sum scaled by the
			// yields, whose integral is nu, so ln(nu) is in it N times
			// over. All that is left to add is nu itself: adding
			// -N*ln(nu) on top would count it twice, and a likelihood of
			// nu - 2N*ln(nu) is smallest at twice the yield there is.
			nu := sum.Yield(par)
			if nu <= 0 {
				return math.Inf(+1)
			}
			nll += nu
		}

		return nll
	})
}

// BinnedNLL returns the negative log-likelihood of a density against a
// histogram, treating each bin as a Poisson count of what the density
// predicts for it.
//
// This is the right likelihood for binned data, and is what a chi-square
// approximates badly once the bins hold few events.
func BinnedNLL(h *hbook.H1D, p PDF, lo, hi float64) minuit.FCN {
	sum, isSum := p.(*Sum)
	extended := isSum && sum.Extended()

	type bin struct {
		x, w, n float64
	}

	var (
		bins  []bin
		total float64
	)
	for i := range h.Binning.Bins {
		b := &h.Binning.Bins[i]
		if b.XMid() < lo || b.XMid() > hi {
			continue
		}
		bins = append(bins, bin{x: b.XMid(), w: b.XWidth(), n: b.SumW()})
		total += b.SumW()
	}

	return minuit.FuncOf(func(par []float64) float64 {
		var nll float64

		for _, b := range bins {
			var f float64
			switch {
			case extended:
				f = sum.NormShape(b.x, lo, hi, par)
			default:
				f = Eval(p, b.x, lo, hi, par)
			}
			if f < 0 || math.IsNaN(f) {
				return math.Inf(+1)
			}

			// what the density says this bin should hold.
			mu := f * b.w
			if !extended {
				mu *= total
			}

			switch {
			case mu <= 0:
				if b.n > 0 {
					return math.Inf(+1)
				}
			default:
				nll += mu - b.n*math.Log(mu)
			}
		}

		return nll
	})
}

// Result is what a fit came to.
type Result struct {
	// Minuit is the minimiser that did the work, for the covariance, MINOS,
	// or anything else it knows.
	Minuit *minuit.Minuit

	// PDF is the density that was fitted, and Lo and Hi the range.
	PDF    PDF
	Lo, Hi float64

	// NLL is the negative log-likelihood at the minimum.
	NLL float64

	// Channels are the datasets a simultaneous fit was made of, and are nil
	// for a fit to one.
	Channels []Channel

	// what the fit was, kept so that it can be done again: a profile scan
	// is a fit per point and has to build its own minimisers.
	fcn  minuit.FCN
	pars []minuit.Par
}

// Values returns the fitted parameters.
func (r *Result) Values() []float64 { return r.Minuit.Values() }

// Value returns the fitted value of the i-th parameter and its uncertainty.
func (r *Result) Value(i int) (val, err float64) {
	v, e, _ := r.Minuit.Value(i)
	return v, e
}

// Func returns the fitted density as a function of x alone, scaled so that
// it can be drawn on top of a histogram of n entries binned at width w.
func (r *Result) Func(scale float64) func(float64) float64 {
	if r.PDF == nil {
		return func(float64) float64 { return 0 }
	}
	par := r.Values()

	if sum, ok := r.PDF.(*Sum); ok && sum.Extended() {
		return func(x float64) float64 {
			return scale * sum.NormShape(x, r.Lo, r.Hi, par)
		}
	}
	return Func(r.PDF, r.Lo, r.Hi, par, scale)
}

// Component returns the i-th component of a fitted sum as a function of x
// alone, for drawing each piece of a model separately.
func (r *Result) Component(i int, scale float64) (func(float64) float64, error) {
	sum, ok := r.PDF.(*Sum)
	if !ok {
		return nil, fmt.Errorf("pdf: the fitted density is not a sum")
	}
	if i < 0 || i >= len(sum.pdfs) {
		return nil, fmt.Errorf("pdf: no component %d", i)
	}

	var (
		par = r.Values()
		cs  = sum.coeffs(par)
		ps  = sum.split(par)
		p   = sum.pdfs[i]
		n   = p.Integral(r.Lo, r.Hi, ps[i])
	)
	if n <= 0 {
		return func(float64) float64 { return 0 }, nil
	}

	return func(x float64) float64 {
		return scale * cs[i] * p.Shape(x, ps[i]) / n
	}, nil
}

// FitUnbinned fits a density to unbinned data over [lo, hi] by maximum
// likelihood.
func FitUnbinned(data []float64, p PDF, lo, hi float64, pars []minuit.Par) (*Result, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("pdf: no data to fit")
	}
	return fit(NLL(data, p, lo, hi), p, lo, hi, pars)
}

// FitBinned fits a density to a histogram over [lo, hi] by maximum
// likelihood, treating each bin as a Poisson count.
func FitBinned(h *hbook.H1D, p PDF, lo, hi float64, pars []minuit.Par) (*Result, error) {
	if h.Entries() == 0 {
		return nil, fmt.Errorf("pdf: the histogram is empty")
	}
	return fit(BinnedNLL(h, p, lo, hi), p, lo, hi, pars)
}

// Fit minimises a likelihood that was built by hand, which is what a
// constrained one is: Constrain returns a likelihood and not a density, so
// there is no FitUnbinned to hand it to.
//
// The result knows no density, so Func and Component have nothing to return,
// but the parameters, the covariance and a profile scan all work.
func Fit(fcn minuit.FCN, pars []minuit.Par) (*Result, error) {
	return fitFCN(fcn, nil, 0, 0, pars)
}

func fit(fcn minuit.FCN, p PDF, lo, hi float64, pars []minuit.Par) (*Result, error) {
	if len(pars) != p.NPar() {
		return nil, fmt.Errorf(
			"pdf: %s takes %d parameters, got %d",
			p.Name(), p.NPar(), len(pars),
		)
	}
	return fitFCN(fcn, p, lo, hi, pars)
}

// fitFCN minimises a likelihood over the given parameters.
//
// It is fit without the check that the parameters match a density's, since a
// simultaneous fit has parameters spread across several densities and
// matching none of them one for one.
func fitFCN(fcn minuit.FCN, p PDF, lo, hi float64, pars []minuit.Par) (*Result, error) {
	m := minuit.New(len(pars))
	m.SetPrintLevel(-1)
	m.SetFCN(fcn)

	// a likelihood, not a chi-square: one sigma is a rise of half a unit.
	err := m.SetErrorDef(errorDef)
	if err != nil {
		return nil, err
	}

	var names []string
	if p != nil {
		names = p.ParNames()
	}
	for i, par := range pars {
		name := par.Name
		if name == "" && i < len(names) {
			name = names[i]
		}
		step := par.Step
		if step == 0 && par.Min == par.Max {
			step = 0.1 * math.Max(1, math.Abs(par.Value))
		}
		err := m.Parameter(i, name, par.Value, step, par.Min, par.Max)
		if err != nil {
			return nil, fmt.Errorf("pdf: could not set parameter %d: %w", i, err)
		}
	}

	res := &Result{Minuit: m, PDF: p, Lo: lo, Hi: hi, fcn: fcn, pars: pars}

	err = m.Command("MIGRAD")
	if err != nil {
		return res, fmt.Errorf("pdf: MIGRAD failed: %w", err)
	}
	if m.Status() != minuit.Converged {
		return res, fmt.Errorf("pdf: the fit did not converge: %v (edm=%v)", m.Status(), m.EDM())
	}

	err = m.Command("HESSE")
	if err != nil {
		return res, fmt.Errorf("pdf: HESSE failed: %w", err)
	}

	res.NLL = m.FMin()
	return res, nil
}
