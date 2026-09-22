// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"fmt"
	"math"

	"gonum.org/v1/gonum/mat"
)

// SWeights are per-event weights that unfold a mixture into its components.
//
// The idea behind sPlot: having fitted a sum in a discriminating variable --
// a mass, say, where signal peaks and background does not -- each event is
// given one weight per component. Histogramming any *other* variable with
// those weights gives that component's distribution of it, background
// subtracted, without ever cutting on the mass.
//
// It works only when the control variable is independent of the
// discriminating one. That assumption is the whole method and nothing here
// can check it: correlated variables give weighted distributions that are
// confidently wrong.
type SWeights struct {
	// W is one row per event and one column per component, in the order the
	// components were given.
	W [][]float64

	// Cov is the covariance of the yields that the weights imply, which is
	// what the uncertainty on a weighted histogram comes from.
	Cov *mat.SymDense

	names []string
}

// Names returns the component names, in the order the columns are in.
func (s *SWeights) Names() []string { return append([]string(nil), s.names...) }

// Sum returns the total weight of a component over all events, which comes
// back as that component's fitted yield: the weights are built to.
func (s *SWeights) Sum(comp int) float64 {
	var o float64
	for _, w := range s.W {
		o += w[comp]
	}
	return o
}

// SPlot computes sWeights from an extended fit to a sum of densities.
//
// data are the events in the discriminating variable, over [lo, hi], and the
// fit must be an extended one: the weights are built out of the yields, and
// fractions do not say how many events there are.
//
// The weights come from inverting
//
//	V^-1[n][j] = sum over events of f_n(x) f_j(x) / (sum_k N_k f_k(x))^2
//
// and then
//
//	w_n(x) = sum_j V[n][j] f_j(x) / (sum_k N_k f_k(x))
//
// with f the components normalised over the range and N their yields. That
// is what makes the weights sum to the yields and the weighted distributions
// come out right.
func SPlot(data []float64, sum *Sum, lo, hi float64, par []float64) (*SWeights, error) {
	if sum == nil {
		return nil, fmt.Errorf("pdf: sPlot needs a sum of densities")
	}
	if !sum.Extended() {
		return nil, fmt.Errorf(
			"pdf: sPlot needs an extended fit: the weights are built out of the yields",
		)
	}
	if len(par) != sum.NPar() {
		return nil, fmt.Errorf(
			"pdf: the sum takes %d parameters, got %d", sum.NPar(), len(par),
		)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("pdf: no events to weight")
	}

	var (
		n      = len(sum.pdfs)
		yields = sum.coeffs(par)
		ps     = sum.split(par)
	)

	// each component, normalised over the range, at each event.
	f := make([][]float64, len(data))
	for e, x := range data {
		f[e] = make([]float64, n)
		for i, p := range sum.pdfs {
			norm := p.Integral(lo, hi, ps[i])
			if norm <= 0 {
				continue
			}
			f[e][i] = p.Shape(x, ps[i]) / norm
		}
	}

	// the inverse covariance of the yields.
	inv := mat.NewSymDense(n, nil)
	for e := range data {
		var den float64
		for k := range n {
			den += yields[k] * f[e][k]
		}
		if den <= 0 {
			continue
		}
		den *= den

		for i := range n {
			for j := i; j < n; j++ {
				inv.SetSym(i, j, inv.At(i, j)+f[e][i]*f[e][j]/den)
			}
		}
	}

	var cov mat.SymDense
	if err := invertSPlot(&cov, inv); err != nil {
		return nil, fmt.Errorf("pdf: could not invert the sPlot matrix: %w", err)
	}

	// and the weights themselves.
	w := make([][]float64, len(data))
	for e := range data {
		var den float64
		for k := range n {
			den += yields[k] * f[e][k]
		}

		w[e] = make([]float64, n)
		if den <= 0 {
			continue
		}
		for i := range n {
			var s float64
			for j := range n {
				s += cov.At(i, j) * f[e][j]
			}
			w[e][i] = s / den
		}
	}

	names := make([]string, n)
	for i := range names {
		switch {
		case i < len(sum.names):
			names[i] = sum.names[i]
		default:
			names[i] = sum.pdfs[i].Name()
		}
	}

	return &SWeights{W: w, Cov: &cov, names: names}, nil
}

func invertSPlot(dst *mat.SymDense, src *mat.SymDense) error {
	n := src.SymmetricDim()

	var chol mat.Cholesky
	if ok := chol.Factorize(src); ok {
		var inv mat.SymDense
		if err := chol.InverseTo(&inv); err == nil {
			dst.Reset()
			dst.ReuseAsSym(n)
			dst.CopySym(&inv)
			return nil
		}
	}

	return fmt.Errorf(
		"the matrix is singular: two components the data cannot tell apart",
	)
}

// --- goodness of fit and significance ---

// Chi2 returns the chi-square of a density against binned counts, and the
// number of degrees of freedom, which is the bins used less the parameters
// fitted.
//
// Bins holding too few events to trust a gaussian uncertainty are skipped:
// min is how few is too few, and five is the usual answer.
func Chi2(xs, counts []float64, widths []float64, p PDF, lo, hi float64, par []float64, npar int, min float64) (chi2 float64, ndf int) {
	var total float64
	for _, c := range counts {
		total += c
	}

	for i, x := range xs {
		if counts[i] < min {
			continue
		}
		mu := total * Eval(p, x, lo, hi, par) * widths[i]
		if mu <= 0 {
			continue
		}
		d := counts[i] - mu
		chi2 += d * d / mu
		ndf++
	}

	return chi2, ndf - npar
}

// Significance turns a difference in log-likelihood into a number of standard
// deviations, by Wilks' theorem:
//
//	Z = sqrt(2 * (NLL(null) - NLL(best)))
//
// The theorem holds asymptotically, for nested models, and away from the edge
// of the parameter space. A signal yield pressed against zero is exactly the
// case where it does not, and where this number flatters the result.
func Significance(nllNull, nllBest float64) float64 {
	d := nllNull - nllBest
	if d <= 0 {
		return 0
	}
	return math.Sqrt(2 * d)
}
