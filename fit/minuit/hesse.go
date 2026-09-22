// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package minuit

import (
	"fmt"
	"math"

	"gonum.org/v1/gonum/mat"
)

// hesse computes the matrix of second derivatives at the current point and
// inverts it to get the covariance, as the HESSE command does.
//
// Where MIGRAD's covariance is whatever its metric happened to accumulate on
// the way down, this one is computed where the fit ended. The diagonal comes
// from
//
//	d2f/dxi2 = ( f(x+h) - 2f(x) + f(x-h) ) / h^2
//
// and the off-diagonal terms from the four-point formula, and the covariance
// is 2*UP times the inverse: the factor that makes the square root of a
// diagonal element the distance over which the function rises by UP.
func (m *Minuit) hesse(maxcalls int) error {
	free := m.free()
	if len(free) == 0 {
		return m.noFreeParameters()
	}
	if maxcalls <= 0 {
		maxcalls = defaultCalls(len(free))
	}
	budget := m.ncalls + maxcalls

	var (
		n = len(free)
		x = m.internal()
		f = m.evalInt(x)
	)

	h := make([]float64, n)
	for k, i := range free {
		h[k] = m.hesseStep(k, i, x)
	}

	hess := mat.NewSymDense(n, nil)

	// the diagonal first: it also tells us whether the steps are sensible.
	for k := range n {
		if m.ncalls >= budget {
			return fmt.Errorf("minuit: HESSE ran out of calls after %d", m.ncalls)
		}

		xh := make([]float64, n)
		copy(xh, x)

		xh[k] = x[k] + h[k]
		fp := m.evalInt(xh)
		xh[k] = x[k] - h[k]
		fm := m.evalInt(xh)

		d := (fp - 2*f + fm) / (h[k] * h[k])
		hess.SetSym(k, k, d)
	}

	for a := range n {
		for b := a + 1; b < n; b++ {
			if m.ncalls >= budget {
				return fmt.Errorf("minuit: HESSE ran out of calls after %d", m.ncalls)
			}

			xh := make([]float64, n)
			copy(xh, x)

			xh[a], xh[b] = x[a]+h[a], x[b]+h[b]
			fpp := m.evalInt(xh)

			xh[a], xh[b] = x[a]+h[a], x[b]-h[b]
			fpm := m.evalInt(xh)

			xh[a], xh[b] = x[a]-h[a], x[b]+h[b]
			fmp := m.evalInt(xh)

			xh[a], xh[b] = x[a]-h[a], x[b]-h[b]
			fmm := m.evalInt(xh)

			d := (fpp - fpm - fmp + fmm) / (4 * h[a] * h[b])
			hess.SetSym(a, b, d)
		}
	}

	var inv mat.SymDense
	err := invertSym(&inv, hess)
	if err != nil {
		return fmt.Errorf("minuit: HESSE could not invert the second-derivative matrix: %w", err)
	}

	// into external units, and scaled so that one sigma is a rise of UP.
	cov := mat.NewSymDense(n, nil)
	for a, i := range free {
		da := m.pars[i].dExtdInt(x[a])
		for b := a; b < n; b++ {
			db := m.pars[free[b]].dExtdInt(x[b])
			cov.SetSym(a, b, 2*m.up*inv.At(a, b)*da*db)
		}
	}

	m.cov = cov
	m.hasHesse = true
	m.setErrorsFromCov()
	m.fmin = f

	// with a computed matrix in hand the distance to the minimum is worth
	// restating.
	g := m.gradient(x, f)
	m.edm = math.Max(edmOf(g, &inv), 0)

	if m.print >= 0 {
		m.Print()
	}
	return nil
}

// hesseStep returns the step to take the second derivative of the k-th free
// parameter over.
//
// A second derivative from differences loses roughly twice as many digits as
// a first one, so the step is a good deal larger than the gradient's.
func (m *Minuit) hesseStep(k, i int, x []float64) float64 {
	p := &m.pars[i]

	h := p.err
	if d := p.dExtdInt(x[k]); d != 0 {
		h /= math.Abs(d)
	}
	if h == 0 || math.IsNaN(h) || math.IsInf(h, 0) {
		h = 0.1
	}

	switch m.strategy {
	case 0:
		h *= 0.1
	case 2:
		h *= 0.01
	default:
		h *= 0.05
	}

	if e := 1e-6 * (math.Abs(x[k]) + 1); h < e {
		h = e
	}
	return h
}
