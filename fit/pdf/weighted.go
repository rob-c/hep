// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"fmt"
	"math"

	"go-hep.org/x/hep/fit/minuit"
	"gonum.org/v1/gonum/mat"
)

// NLLWeighted returns the negative log-likelihood of a density against data
// where each event carries a weight:
//
//	-ln L = - sum over the data of w * ln f(x)
//
// Weighted events are what comes out of an sPlot, or of a simulation
// reweighted to match data, and fitting them is ordinary. Believing the
// uncertainties that come back is not: see FitWeighted.
func NLLWeighted(data, weights []float64, p PDF, lo, hi float64) (minuit.FCN, error) {
	if len(weights) != len(data) {
		return nil, fmt.Errorf(
			"pdf: %d events and %d weights", len(data), len(weights),
		)
	}

	sum, isSum := p.(*Sum)
	extended := isSum && sum.Extended()

	return minuit.FuncOf(func(par []float64) float64 {
		var (
			nll float64
			sw  float64
		)

		for i, x := range data {
			if x < lo || x > hi {
				continue
			}
			w := weights[i]
			sw += w

			var f float64
			switch {
			case extended:
				f = sum.NormShape(x, lo, hi, par)
			case isSum:
				f = sum.evalAt(x, lo, hi, par)
			default:
				f = Eval(p, x, lo, hi, par)
			}

			if f <= 0 || math.IsNaN(f) {
				return math.Inf(+1)
			}
			nll -= w * math.Log(f)
		}

		if extended {
			nu := sum.Yield(par)
			if nu <= 0 {
				return math.Inf(+1)
			}
			nll += nu
		}

		return nll
	}), nil
}

// FitWeighted fits a density to weighted data by maximum likelihood, and
// corrects the uncertainties for the weights.
//
// # Why the correction is needed
//
// A likelihood built from weights is not a likelihood: doubling every weight
// doubles it, where doubling the data would not, so the curvature at the
// minimum no longer says what the uncertainty is. Taken at face value the
// uncertainties can be wrong by any factor the weights care to supply, and
// for sWeights -- which are negative for some events -- badly so.
//
// What is asymptotically right is the sandwich
//
//	C = V(w) * V(w^2)^-1 * V(w)
//
// where V(w) is the covariance the weighted fit reports and V(w^2) the one it
// would report with every weight squared. That is what RooFit's
// SumW2Error does, and it is what Result.Cov holds when a fit comes back from
// here. Minuit.Covariance still holds the uncorrected one, for comparison.
func FitWeighted(data, weights []float64, p PDF, lo, hi float64, pars []minuit.Par, opts ...FitOption) (*Result, error) {
	switch {
	case len(data) == 0:
		return nil, fmt.Errorf("pdf: no data to fit")
	case len(weights) != len(data):
		return nil, fmt.Errorf("pdf: %d events and %d weights", len(data), len(weights))
	case len(pars) != p.NPar():
		return nil, fmt.Errorf(
			"pdf: %s takes %d parameters, got %d", p.Name(), p.NPar(), len(pars),
		)
	}

	nll, err := NLLWeighted(data, weights, p, lo, hi)
	if err != nil {
		return nil, err
	}

	res, err := fitFCN(nll, p, lo, hi, pars, opts...)
	if err != nil {
		return res, err
	}

	// the same fit again with the weights squared, held at the minimum: only
	// its curvature is wanted, not its minimum, which is the same one.
	sq := make([]float64, len(weights))
	for i, w := range weights {
		sq[i] = w * w
	}

	nll2, err := NLLWeighted(data, sq, p, lo, hi)
	if err != nil {
		return res, err
	}

	cov, err := sandwich(res, nll2, pars)
	if err != nil {
		return res, fmt.Errorf("pdf: could not correct the covariance for the weights: %w", err)
	}

	res.Cov = cov
	res.Weighted = true

	return res, nil
}

// sandwich returns V(w) * V(w^2)^-1 * V(w).
func sandwich(res *Result, nll2 minuit.FCN, pars []minuit.Par) (*mat.SymDense, error) {
	vw := res.Minuit.Covariance()
	if vw == nil {
		return nil, fmt.Errorf("the weighted fit produced no covariance")
	}

	// a second minimiser, put at the minimum the first one found, asked only
	// for second derivatives.
	m := minuit.New(len(pars))
	m.SetPrintLevel(-1)
	m.SetFCN(nll2)
	if err := m.SetErrorDef(errorDef); err != nil {
		return nil, err
	}

	vals := res.Minuit.Values()
	for i, p := range pars {
		step := p.Step
		if step == 0 && p.Min == p.Max {
			step = 0.1 * math.Max(1, math.Abs(vals[i]))
		}
		if err := m.Parameter(i, p.Name, vals[i], step, p.Min, p.Max); err != nil {
			return nil, err
		}
	}

	if err := m.Command("HESSE"); err != nil {
		return nil, err
	}

	vw2 := m.Covariance()
	if vw2 == nil {
		return nil, fmt.Errorf("the squared-weight fit produced no covariance")
	}

	var inv mat.SymDense
	if err := invertSPlot(&inv, vw2); err != nil {
		return nil, err
	}

	// V * inv * V, which is symmetric because V and inv are.
	var (
		n   = vw.SymmetricDim()
		tmp mat.Dense
		out mat.Dense
	)
	tmp.Mul(vw, &inv)
	out.Mul(&tmp, vw)

	o := mat.NewSymDense(n, nil)
	for i := range n {
		for j := i; j < n; j++ {
			// the product of symmetric matrices is not exactly symmetric in
			// floating point: average the two halves rather than pick one.
			o.SetSym(i, j, 0.5*(out.At(i, j)+out.At(j, i)))
		}
	}

	return o, nil
}

// EffectiveEntries returns the number of unweighted events that would carry
// as much information as these weighted ones:
//
//	(sum of w)^2 / sum of w^2
//
// It is what the uncertainty on a weighted sample scales with, and comparing
// it with the number of events says how much the weighting cost.
func EffectiveEntries(weights []float64) float64 {
	var sw, sw2 float64
	for _, w := range weights {
		sw += w
		sw2 += w * w
	}
	if sw2 <= 0 {
		return 0
	}
	return sw * sw / sw2
}
