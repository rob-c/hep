// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package minuit

import (
	"fmt"
	"math"
)

// simplex minimises the function without derivatives, as the SIMPLEX command
// does, by the method of Nelder and Mead: keep n+1 points, and repeatedly
// replace the worst of them by reflecting it through the centre of the rest,
// stretching the step when that helps and shrinking it when it does not.
//
// It is slower than MIGRAD near a minimum and much steadier far from one,
// which is what MINIMIZE falls back on it for. It leaves no covariance
// behind: it never forms one.
func (m *Minuit) simplex(maxcalls int, tol float64) error {
	free := m.free()
	if len(free) == 0 {
		return m.noFreeParameters()
	}
	if tol <= 0 {
		tol = m.tol
	}
	if maxcalls <= 0 {
		maxcalls = 1000 + 100*len(free)
	}
	budget := m.ncalls + maxcalls

	m.callFCN(IFlagInit)

	n := len(free)

	// the starting simplex: the current point, and one step along each axis.
	pts := make([][]float64, n+1)
	fs := make([]float64, n+1)

	pts[0] = m.internal()
	fs[0] = m.evalInt(pts[0])

	for k := range n {
		p := make([]float64, n)
		copy(p, pts[0])

		s := m.pars[free[k]].err
		if d := m.pars[free[k]].dExtdInt(pts[0][k]); d != 0 {
			s /= math.Abs(d)
		}
		if s == 0 || math.IsNaN(s) || math.IsInf(s, 0) {
			s = 0.1
		}

		p[k] += s
		pts[k+1] = p
		fs[k+1] = m.evalInt(p)
	}

	goal := 1e-3 * tol * m.up

	m.status = CallLimit
	for m.ncalls < budget {
		hi, lo, nhi := orderSimplex(fs)

		// the spread of the function over the simplex stands in for the
		// distance to the minimum, there being no gradient to ask.
		if math.Abs(fs[hi]-fs[lo]) < goal {
			m.status = Converged
			break
		}

		if m.print >= 1 {
			fmt.Fprintf(m.out, "simplex: fcn=%v spread=%v ncalls=%d\n", fs[lo], fs[hi]-fs[lo], m.ncalls)
		}

		// the centre of every point but the worst.
		cen := make([]float64, n)
		for i := range pts {
			if i == hi {
				continue
			}
			for k := range n {
				cen[k] += pts[i][k] / float64(n)
			}
		}

		refl, frefl := m.simplexTry(cen, pts[hi], 1.0)

		switch {
		case frefl < fs[lo]:
			// better than anything yet: try going further.
			exp, fexp := m.simplexTry(cen, pts[hi], 2.0)
			if fexp < frefl {
				pts[hi], fs[hi] = exp, fexp
			} else {
				pts[hi], fs[hi] = refl, frefl
			}

		case frefl < fs[nhi]:
			pts[hi], fs[hi] = refl, frefl

		default:
			// no better: pull in towards the centre.
			con, fcon := m.simplexTry(cen, pts[hi], -0.5)
			if fcon < fs[hi] {
				pts[hi], fs[hi] = con, fcon
				break
			}
			// still no better: shrink the whole simplex onto the best point.
			for i := range pts {
				if i == lo {
					continue
				}
				for k := range n {
					pts[i][k] = 0.5 * (pts[i][k] + pts[lo][k])
				}
				fs[i] = m.evalInt(pts[i])
			}
		}
	}

	_, lo, _ := orderSimplex(fs)
	m.store(pts[lo])
	m.fmin = fs[lo]
	m.edm = math.Abs(maxOf(fs) - fs[lo])
	m.invalidateCov()

	m.callFCN(IFlagEnd)

	if m.print >= 0 {
		m.Print()
	}
	return nil
}

// simplexTry moves the worst point through the centre by the given factor.
func (m *Minuit) simplexTry(cen, worst []float64, fac float64) ([]float64, float64) {
	p := make([]float64, len(cen))
	for k := range p {
		p[k] = cen[k] + fac*(cen[k]-worst[k])
	}
	return p, m.evalInt(p)
}

// orderSimplex returns the indices of the worst, the best and the second
// worst points.
func orderSimplex(fs []float64) (hi, lo, nhi int) {
	hi, lo = 0, 0
	for i, f := range fs {
		if f > fs[hi] {
			hi = i
		}
		if f < fs[lo] {
			lo = i
		}
	}
	nhi = lo
	for i, f := range fs {
		if i != hi && f > fs[nhi] {
			nhi = i
		}
	}
	return hi, lo, nhi
}

func maxOf(vs []float64) float64 {
	o := vs[0]
	for _, v := range vs[1:] {
		o = math.Max(o, v)
	}
	return o
}

// invalidateCov drops the covariance without touching the fit result.
func (m *Minuit) invalidateCov() {
	m.cov = nil
	m.hasHesse = false
}
