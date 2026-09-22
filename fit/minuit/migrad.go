// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package minuit

import (
	"fmt"
	"math"

	"gonum.org/v1/gonum/mat"
)

// migrad minimises the function by the variable-metric method.
//
// The method, following James and Roos, keeps an approximation V to the
// inverse of the second-derivative matrix, steps along -V*g, and improves V
// from the step taken and the change in the gradient it produced. It stops
// when the estimated distance to the minimum,
//
//	EDM = 1/2 * g' * V * g
//
// which for a quadratic function is exactly how far the function value still
// has to fall, drops below 1e-3 * tolerance * UP.
func (m *Minuit) migrad(maxcalls int, tol float64) error {
	free := m.free()
	if len(free) == 0 {
		return m.noFreeParameters()
	}
	if tol <= 0 {
		tol = m.tol
	}
	if maxcalls <= 0 {
		maxcalls = 200 + 100*len(free) + 5*len(free)*len(free)
	}
	budget := m.ncalls + maxcalls

	m.callFCN(IFlagInit)

	var (
		n = len(free)
		x = m.internal()
		f = m.evalInt(x)
	)

	v := m.initMetric(x, f, free)
	g := m.gradient(x, f)

	// the convergence criterion, as MINUIT states it.
	goal := 1e-3 * tol * m.up

	m.status = CallLimit
	for iter := 0; ; iter++ {
		edm := edmOf(g, v)
		if edm < goal && edm >= 0 {
			m.status = Converged
			m.edm = edm
			break
		}
		if m.ncalls >= budget {
			m.edm = math.Max(edm, 0)
			break
		}

		if m.print >= 1 {
			fmt.Fprintf(m.out, "migrad: iter=%d fcn=%v edm=%v ncalls=%d\n", iter, f, edm, m.ncalls)
		}

		// the step the metric suggests.
		step := make([]float64, n)
		for i := range n {
			var sum float64
			for j := range n {
				sum += v.At(i, j) * g[j]
			}
			step[i] = -sum
		}

		// a direction that does not go downhill means the metric has gone
		// bad: start it again from the steepest descent.
		if dot(step, g) >= 0 {
			v.Reset()
			v.ReuseAsSym(n)
			for i := range n {
				v.SetSym(i, i, 1)
			}
			for i := range n {
				step[i] = -g[i]
			}
		}

		xnew, fnew, ok := m.lineSearch(x, f, step, g, budget)
		if !ok {
			m.edm = math.Max(edmOf(g, v), 0)
			if m.ncalls >= budget {
				m.status = CallLimit
			} else {
				m.status = Failed
			}
			break
		}

		gnew := m.gradient(xnew, fnew)

		// improve the metric from the step just taken. The update is the
		// Davidon-Fletcher-Powell one MINUIT uses:
		//
		//	V' = V + d d'/(d'.y) - (V y)(V y)'/(y' V y)
		//
		// with d the change in the parameters and y the change in the
		// gradient. It is skipped when either denominator is too small to
		// divide by, which leaves the metric as it was rather than ruining it.
		d := sub(xnew, x)
		y := sub(gnew, g)
		updateMetric(v, d, y)

		x, f, g = xnew, fnew, gnew
	}

	m.store(x)
	m.fmin = f

	// the metric the minimiser arrived at is an estimate of the inverse
	// second-derivative matrix, so it gives a covariance without any more
	// function calls. HESSE replaces it with a computed one.
	m.setCovFromMetric(v, x, free)
	m.hasHesse = false

	m.callFCN(IFlagEnd)

	if m.print >= 0 {
		m.Print()
	}
	return nil
}

// initMetric builds the first approximation to the inverse of the
// second-derivative matrix.
//
// The caller's step sizes alone make a poor one: they say how far a parameter
// might move, not how sharply the function curves, and a first step built
// from them can be wrong by orders of magnitude. So take the diagonal second
// derivatives at the starting point and use those, which is what makes the
// first step the right size — and for a function that is quadratic and
// separable, the right step outright. Where a second derivative comes out
// non-positive, the function is not curving upwards along that parameter and
// there is nothing to learn from it, so fall back on the caller's step.
func (m *Minuit) initMetric(x []float64, f float64, free []int) *mat.SymDense {
	n := len(free)
	v := mat.NewSymDense(n, nil)

	for k, i := range free {
		s := m.pars[i].err
		if d := m.pars[i].dExtdInt(x[k]); d != 0 {
			s /= math.Abs(d)
		}
		if s == 0 || math.IsNaN(s) || math.IsInf(s, 0) {
			s = 0.1
		}

		vkk := s * s / (2 * m.up)

		h := m.hesseStep(k, i, x)
		xh := make([]float64, n)
		copy(xh, x)

		xh[k] = x[k] + h
		fp := m.evalInt(xh)
		xh[k] = x[k] - h
		fm := m.evalInt(xh)

		if d2 := (fp - 2*f + fm) / (h * h); d2 > 0 && !math.IsInf(d2, 0) {
			vkk = 1 / d2
		}

		v.SetSym(k, k, vkk)
	}

	return v
}

func (m *Minuit) noFreeParameters() error {
	par := make([]float64, len(m.pars))
	for i := range m.pars {
		par[i] = m.pars[i].val
	}
	m.fmin = m.eval(par)
	m.edm = 0
	m.status = Converged
	m.cov = mat.NewSymDense(0, nil)
	return nil
}

// callFCN tells the function where in the fit it is, for the functions that
// take an interest.
func (m *Minuit) callFCN(iflag int) {
	par := make([]float64, len(m.pars))
	for i := range m.pars {
		par[i] = m.pars[i].val
	}
	m.ncalls++
	m.fcn(m.NFree(), nil, par, iflag)
}

// edmOf returns the estimated distance to the minimum.
func edmOf(g []float64, v *mat.SymDense) float64 {
	var sum float64
	for i := range g {
		for j := range g {
			sum += g[i] * v.At(i, j) * g[j]
		}
	}
	return 0.5 * sum
}

// updateMetric applies the Davidon-Fletcher-Powell correction to v.
func updateMetric(v *mat.SymDense, d, y []float64) {
	n := len(d)

	dy := dot(d, y)
	if math.Abs(dy) < 1e-30 {
		return
	}

	vy := make([]float64, n)
	for i := range n {
		var sum float64
		for j := range n {
			sum += v.At(i, j) * y[j]
		}
		vy[i] = sum
	}

	yvy := dot(y, vy)
	if math.Abs(yvy) < 1e-30 {
		return
	}

	for i := range n {
		for j := i; j < n; j++ {
			upd := v.At(i, j) + d[i]*d[j]/dy - vy[i]*vy[j]/yvy
			if math.IsNaN(upd) || math.IsInf(upd, 0) {
				return
			}
			v.SetSym(i, j, upd)
		}
	}
}

// lineSearch looks along step for a point that lowers the function.
//
// It tries the full step first, since near the minimum the metric makes that
// the right one, then fits a parabola through what it has seen, and failing
// that falls back on halving. It is content with any decrease rather than
// insisting on a strong one: the metric update is what buys the convergence,
// and a cheap step leaves more of the call budget for it.
func (m *Minuit) lineSearch(x []float64, f float64, step, g []float64, budget int) ([]float64, float64, bool) {
	slope := dot(step, g)
	if slope >= 0 {
		return nil, 0, false
	}

	try := func(a float64) ([]float64, float64) {
		xa := make([]float64, len(x))
		for i := range x {
			xa[i] = x[i] + a*step[i]
		}
		return xa, m.evalInt(xa)
	}

	var (
		alpha        = 1.0
		xbest        []float64
		fbest        = f
		found        bool
		prevA, prevF float64
	)

	// a first step from an imperfect metric can overshoot badly, so keep
	// halving until either the function comes down or the step is too small
	// to matter.
	for range 30 {
		if m.ncalls >= budget {
			break
		}

		xa, fa := try(alpha)
		if fa < fbest && !math.IsNaN(fa) {
			xbest, fbest, found = xa, fa, true

			// a parabola through the start, this point and the last one
			// suggests where the bottom along this line is.
			if prevA != 0 && m.ncalls < budget {
				if a, ok := parabolicMin(0, f, prevA, prevF, alpha, fa); ok {
					xp, fp := try(a)
					if fp < fbest && !math.IsNaN(fp) {
						xbest, fbest = xp, fp
					}
				}
			}
			break
		}

		prevA, prevF = alpha, fa
		alpha *= 0.5
		if alpha < 1e-12 {
			break
		}
	}

	if !found {
		return nil, 0, false
	}
	return xbest, fbest, true
}

// parabolicMin returns where the parabola through three points is lowest.
func parabolicMin(a0, f0, a1, f1, a2, f2 float64) (float64, bool) {
	d1 := (a1 - a0) * (f2 - f0)
	d2 := (a2 - a0) * (f1 - f0)
	den := 2 * (d2 - d1)
	if math.Abs(den) < 1e-30 {
		return 0, false
	}

	num := (a2-a0)*d2 - (a1-a0)*d1
	a := a0 + num/den
	if math.IsNaN(a) || math.IsInf(a, 0) {
		return 0, false
	}

	// only trust it inside the range already explored.
	lo := math.Min(a0, math.Min(a1, a2))
	hi := math.Max(a0, math.Max(a1, a2))
	if a <= lo || a >= hi {
		return 0, false
	}
	return a, true
}

// gradient returns the derivative of the function with respect to each free
// internal parameter, by central differences.
//
// The step for each parameter is taken from the uncertainty the caller gave
// it, which is the scale over which the function is expected to change by
// about UP, floored so that it stays well clear of the rounding noise in the
// function value.
func (m *Minuit) gradient(x []float64, f float64) []float64 {
	free := m.free()
	g := make([]float64, len(x))

	for k, i := range free {
		h := m.gradStep(k, i, x)

		xh := make([]float64, len(x))
		copy(xh, x)

		xh[k] = x[k] + h
		fp := m.evalInt(xh)

		xh[k] = x[k] - h
		fm := m.evalInt(xh)

		switch {
		case math.IsNaN(fp) && math.IsNaN(fm):
			g[k] = 0
		case math.IsNaN(fp):
			g[k] = (f - fm) / h
		case math.IsNaN(fm):
			g[k] = (fp - f) / h
		default:
			g[k] = (fp - fm) / (2 * h)
		}
	}
	return g
}

// gradStep returns the step to differentiate the k-th free parameter over.
func (m *Minuit) gradStep(k, i int, x []float64) float64 {
	p := &m.pars[i]

	h := p.err
	if d := p.dExtdInt(x[k]); d != 0 {
		h /= math.Abs(d)
	}
	if h == 0 || math.IsNaN(h) || math.IsInf(h, 0) {
		h = 0.1
	}

	// a strategy of 0 settles for a coarser derivative, one of 2 for a finer.
	switch m.strategy {
	case 0:
		h *= 1e-2
	case 2:
		h *= 1e-4
	default:
		h *= 1e-3
	}

	// never so small that the difference is lost in the noise of the values.
	if e := 1e-8 * (math.Abs(x[k]) + 1); h < e {
		h = e
	}
	return h
}

// setCovFromMetric turns the internal metric into the external covariance.
//
// The metric approximates the inverse of the second-derivative matrix in
// internal units. The covariance is that scaled by 2*UP — which is what makes
// the square root of a diagonal element the distance over which the function
// rises by UP — and carried into external units through dPext/dPint.
func (m *Minuit) setCovFromMetric(v *mat.SymDense, x []float64, free []int) {
	n := len(free)
	cov := mat.NewSymDense(n, nil)

	for a, i := range free {
		da := m.pars[i].dExtdInt(x[a])
		for b := a; b < n; b++ {
			db := m.pars[free[b]].dExtdInt(x[b])
			cov.SetSym(a, b, 2*m.up*v.At(a, b)*da*db)
		}
	}

	m.cov = cov
	m.setErrorsFromCov()
}

// setErrorsFromCov takes the parabolic uncertainties off the diagonal.
func (m *Minuit) setErrorsFromCov() {
	if m.cov == nil {
		return
	}
	for k, i := range m.free() {
		d := m.cov.At(k, k)
		switch {
		case d > 0:
			m.pars[i].err = math.Sqrt(d)
		default:
			m.pars[i].err = 0
		}
	}
}

func dot(a, b []float64) float64 {
	var sum float64
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

func sub(a, b []float64) []float64 {
	o := make([]float64, len(a))
	for i := range a {
		o[i] = a[i] - b[i]
	}
	return o
}
