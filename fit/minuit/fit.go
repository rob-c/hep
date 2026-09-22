// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package minuit

import (
	"fmt"
	"math"

	"go-hep.org/x/hep/hbook"
)

// Model is a function of one variable and a set of parameters, the thing a
// fit adjusts the parameters of.
type Model func(x float64, par []float64) float64

// Par describes one parameter of a fit: where it starts, the scale it varies
// on, and the range it is allowed.
//
// A Step of zero fixes the parameter. Min equal to Max leaves it unbounded.
type Par struct {
	Name  string
	Value float64
	Step  float64
	Min   float64
	Max   float64
}

// Chi2 returns the function that is the chi-square of model against the given
// points:
//
//	sum over i of ( (y_i - model(x_i, par)) / err_i )^2
//
// A point with an uncertainty of zero or less is skipped, having nothing to
// say about how well the model does.
func Chi2(model Model, xs, ys, errs []float64) FCN {
	return FuncOf(func(par []float64) float64 {
		var chi2 float64
		for i := range xs {
			if errs[i] <= 0 {
				continue
			}
			d := (ys[i] - model(xs[i], par)) / errs[i]
			chi2 += d * d
		}
		return chi2
	})
}

// Chi2H1D returns the chi-square of model against a histogram, taking each
// bin's content as the measurement and the square root of its sum of squared
// weights as the uncertainty on it.
//
// Empty bins are skipped: with no entries they have no uncertainty either,
// and counting them would be claiming a measurement that was never made.
func Chi2H1D(h *hbook.H1D, model Model) FCN {
	xs, ys, errs := h1dPoints(h)
	return Chi2(model, xs, ys, errs)
}

// h1dPoints pulls the fittable points out of a histogram.
func h1dPoints(h *hbook.H1D) (xs, ys, errs []float64) {
	bins := h.Binning.Bins
	xs = make([]float64, 0, len(bins))
	ys = make([]float64, 0, len(bins))
	errs = make([]float64, 0, len(bins))

	for i := range bins {
		bin := &bins[i]
		if bin.Entries() <= 0 {
			continue
		}
		xs = append(xs, bin.XMid())
		ys = append(ys, bin.SumW())
		errs = append(errs, math.Sqrt(bin.SumW2()))
	}
	return xs, ys, errs
}

// FitH1D fits model to a histogram by least squares and returns the Minuit
// that did it, so that the parameters, their uncertainties and the covariance
// can be read off it.
//
// It is the equivalent of ROOT's h->Fit(f): MIGRAD to find the minimum, then
// HESSE to compute the uncertainties where it ended up. Ask for MINOS after
// if the asymmetric ones are wanted.
func FitH1D(h *hbook.H1D, model Model, pars []Par) (*Minuit, error) {
	xs, ys, errs := h1dPoints(h)
	if len(xs) < len(pars) {
		return nil, fmt.Errorf(
			"minuit: cannot fit %d parameters to %d filled bins",
			len(pars), len(xs),
		)
	}
	return Fit(Chi2(model, xs, ys, errs), pars)
}

// FitXY fits model to a set of points by least squares, and is FitH1D for
// data that did not come from a histogram.
func FitXY(xs, ys, errs []float64, model Model, pars []Par) (*Minuit, error) {
	switch {
	case len(xs) != len(ys):
		return nil, fmt.Errorf("minuit: got %d x values and %d y values", len(xs), len(ys))
	case len(errs) != len(xs):
		return nil, fmt.Errorf("minuit: got %d x values and %d uncertainties", len(xs), len(errs))
	case len(xs) < len(pars):
		return nil, fmt.Errorf(
			"minuit: cannot fit %d parameters to %d points",
			len(pars), len(xs),
		)
	}
	return Fit(Chi2(model, xs, ys, errs), pars)
}

// Fit minimises fcn over the given parameters, running MIGRAD and then HESSE,
// and returns the Minuit that did it.
func Fit(fcn FCN, pars []Par) (*Minuit, error) {
	m := New(len(pars))
	m.SetPrintLevel(-1)
	m.SetFCN(fcn)

	for i, p := range pars {
		name := p.Name
		if name == "" {
			name = fmt.Sprintf("p%d", i)
		}
		step := p.Step
		if step == 0 && p.Min == p.Max {
			// no scale given and no range to take one from: guess one from
			// the starting value, as a fixed parameter would be a surprise.
			step = 0.1 * math.Max(1, math.Abs(p.Value))
		}
		err := m.Parameter(i, name, p.Value, step, p.Min, p.Max)
		if err != nil {
			return nil, fmt.Errorf("minuit: could not set parameter %d: %w", i, err)
		}
	}

	err := m.Command("MIGRAD")
	if err != nil {
		return nil, fmt.Errorf("minuit: MIGRAD failed: %w", err)
	}
	if m.Status() != Converged {
		return m, fmt.Errorf("minuit: the fit did not converge: %v (edm=%v)", m.Status(), m.EDM())
	}

	err = m.Command("HESSE")
	if err != nil {
		return m, fmt.Errorf("minuit: HESSE failed: %w", err)
	}

	return m, nil
}

// Values returns the fitted value of every parameter, in the order they were
// defined, which is the form a model wants them in.
func (m *Minuit) Values() []float64 {
	o := make([]float64, len(m.pars))
	for i := range m.pars {
		o[i] = m.pars[i].val
	}
	return o
}

// Func returns the fitted model as a function of x alone, ready to be drawn.
func (m *Minuit) Func(model Model) func(x float64) float64 {
	par := m.Values()
	return func(x float64) float64 { return model(x, par) }
}

// Chi2NDF returns the chi-square per degree of freedom of a least-squares fit
// to n points, which is the usual first thing to look at to see whether the
// model fits at all.
func (m *Minuit) Chi2NDF(n int) float64 {
	ndf := n - m.NFree()
	if ndf <= 0 {
		return math.NaN()
	}
	return m.FMin() / float64(ndf)
}

// Gaussian is the model ROOT calls "gaus": par[0] is the height, par[1] the
// mean and par[2] the width.
func Gaussian(x float64, par []float64) float64 {
	if par[2] == 0 {
		return 0
	}
	d := (x - par[1]) / par[2]
	return par[0] * math.Exp(-0.5*d*d)
}

// Exponential is the model ROOT calls "expo": exp(par[0] + par[1]*x).
func Exponential(x float64, par []float64) float64 {
	return math.Exp(par[0] + par[1]*x)
}

// Polynomial returns the model ROOT calls "polN": a polynomial of degree n,
// with par[i] the coefficient of x^i.
func Polynomial(n int) Model {
	return func(x float64, par []float64) float64 {
		var (
			o float64
			p = 1.0
		)
		for i := range n + 1 {
			o += par[i] * p
			p *= x
		}
		return o
	}
}

// GausPars returns starting parameters for a gaussian fit to h, taken from
// the histogram itself: the tallest bin for the height, and the histogram's
// own mean and standard deviation for the other two.
//
// It is what lets h->Fit("gaus") work in ROOT without being told where to
// start, and it is worth having for the same reason: a gaussian fit started
// somewhere arbitrary can spend its whole budget getting back to the data.
//
// The width is kept positive, since a gaussian of negative width is the same
// one and the fit only has to find it twice.
func GausPars(h *hbook.H1D) []Par {
	var height float64
	for i := range h.Binning.Bins {
		height = math.Max(height, h.Binning.Bins[i].SumW())
	}
	if height == 0 {
		height = 1
	}

	mean := h.XMean()
	sigma := h.XStdDev()
	if sigma <= 0 || math.IsNaN(sigma) {
		sigma = 0.1 * math.Max(1, math.Abs(h.XMax()-h.XMin()))
	}

	// the width is held positive, since a gaussian of negative width is the
	// same one and the fit only has to find it twice. The upper bound is far
	// enough away not to constrain anything: both bounds have to be finite,
	// because a limited parameter is varied through a sine.
	wide := math.Abs(h.XMax() - h.XMin())
	if wide <= 0 {
		wide = math.Max(1, 10*sigma)
	}

	return []Par{
		{Name: "height", Value: height, Step: 0.1 * height},
		{Name: "mean", Value: mean, Step: 0.1 * sigma},
		{Name: "sigma", Value: sigma, Step: 0.1 * sigma, Min: 1e-12, Max: wide},
	}
}
