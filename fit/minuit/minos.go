// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package minuit

import (
	"fmt"
	"math"
)

// minos finds the uncertainty on each parameter the way the MINOS command
// does: by asking how far the parameter can move before the function rises by
// UP, with every other parameter re-minimised at each step.
//
// That is a different question from the one the covariance answers. The
// parabolic uncertainty is the width of the parabola fitted at the minimum;
// the MINOS uncertainty is where the function really reaches UP, following
// the valley rather than assuming it is straight. The two agree for a
// function that is quadratic in its parameters and part company for one that
// is not, which is the case worth having MINOS for.
//
// which names the parameters to do, by index; an empty which does them all.
func (m *Minuit) minos(maxcalls int, which []int) error {
	free := m.free()
	if len(free) == 0 {
		return m.noFreeParameters()
	}

	// MINOS needs a minimum, and the uncertainties to start its search from.
	if m.cov == nil {
		if err := m.migrad(0, m.tol); err != nil {
			return err
		}
	}

	if len(which) == 0 {
		which = free
	}
	if maxcalls <= 0 {
		maxcalls = defaultCalls(len(free))
	}

	var (
		fmin = m.fmin
		best = make([]float64, len(m.pars))
	)
	for i := range m.pars {
		best[i] = m.pars[i].val
	}

	for _, i := range which {
		if i < 0 || i >= len(m.pars) {
			return fmt.Errorf("minuit: no parameter %d", i+1)
		}
		if m.pars[i].fixed {
			continue
		}

		budget := m.ncalls + maxcalls

		up, err := m.minosSide(i, +1, fmin, best, budget)
		if err != nil {
			return err
		}
		lo, err := m.minosSide(i, -1, fmin, best, budget)
		if err != nil {
			return err
		}

		m.pars[i].eplus = up
		m.pars[i].eminus = lo

		// put everything back where the fit left it before doing the next.
		for j := range m.pars {
			m.pars[j].val = best[j]
		}
		m.fmin = fmin
	}

	if m.print >= 0 {
		m.Print()
	}
	return nil
}

// minosSide walks parameter i away from the minimum in the given direction
// until the function has risen by UP, re-minimising the others at each stop,
// and returns the signed distance it got to.
//
// It brackets the crossing first, then bisects it. The re-minimisation is
// what makes this expensive and what makes it worth doing.
func (m *Minuit) minosSide(i, sign int, fmin float64, best []float64, budget int) (float64, error) {
	var (
		step = m.pars[i].err
		lim  = math.Inf(sign)
	)
	if step == 0 {
		return 0, nil
	}
	if m.pars[i].lim {
		switch sign {
		case +1:
			lim = m.pars[i].hi
		default:
			lim = m.pars[i].lo
		}
	}

	// f(d) is the function minimised over every other parameter with this
	// one held d away from its best value, less the rise we are looking for.
	f := func(d float64) float64 {
		v := best[i] + float64(sign)*d
		if m.pars[i].lim {
			v = math.Min(math.Max(v, m.pars[i].lo), m.pars[i].hi)
		}
		return m.profile(i, v, best) - fmin - m.up
	}

	// bracket: the rise is about UP at one parabolic sigma, so start there
	// and grow until the function has gone past.
	var (
		alo, ahi float64
		flo, fhi float64
		bracket  bool
	)
	alo, flo = 0, -m.up
	for a, k := step, 0; k < 20; a, k = a*1.6, k+1 {
		if m.ncalls >= budget {
			break
		}
		if m.pars[i].lim && math.Abs(float64(sign)*a) > math.Abs(lim-best[i]) {
			// the limit is reached before the rise is: report the distance
			// to the limit, which is all the parameter can do.
			return float64(sign) * math.Abs(lim-best[i]), nil
		}

		fa := f(a)
		if fa >= 0 {
			ahi, fhi = a, fa
			bracket = true
			break
		}
		alo, flo = a, fa
	}

	if !bracket {
		// the function never rose by UP: the uncertainty is unbounded on
		// this side, which a zero says better than a made-up number.
		return 0, nil
	}

	// Now close in on the crossing. The straight line through the two ends
	// of the bracket says where to look next, which converges much faster
	// than halving; but on a curved function it keeps landing on the same
	// side and the far end never moves, so on a repeat the retained end has
	// its value halved to drag it in. That is the Illinois variant of false
	// position, and it is what stops the search stalling.
	var (
		root = 0.5 * (alo + ahi)
		side int
	)
	for range 60 {
		if m.ncalls >= budget {
			break
		}

		a := 0.5 * (alo + ahi)
		if d := fhi - flo; d != 0 {
			lin := alo - flo*(ahi-alo)/d
			if lin > alo && lin < ahi {
				a = lin
			}
		}
		root = a

		fa := f(a)

		// close enough in the function value, or in the bracket width.
		if math.Abs(fa) < 1e-6*m.up || ahi-alo < 1e-8*math.Max(1, math.Abs(ahi)) {
			break
		}

		switch {
		case fa < 0:
			alo, flo = a, fa
			if side == -1 {
				fhi *= 0.5
			}
			side = -1
		default:
			ahi, fhi = a, fa
			if side == +1 {
				flo *= 0.5
			}
			side = +1
		}
	}

	return float64(sign) * root, nil
}

// profile minimises the function over every parameter but the i-th, which is
// held at v, and returns the value it reaches.
func (m *Minuit) profile(i int, v float64, start []float64) float64 {
	// remember what to put back.
	var (
		saveVals  = make([]float64, len(m.pars))
		saveFixed = make([]bool, len(m.pars))
		saveErrs  = make([]float64, len(m.pars))
	)
	for j := range m.pars {
		saveVals[j] = m.pars[j].val
		saveFixed[j] = m.pars[j].fixed
		saveErrs[j] = m.pars[j].err
	}

	for j := range m.pars {
		m.pars[j].val = start[j]
	}
	m.pars[i].val = v
	m.pars[i].fixed = true

	var f float64
	switch m.NFree() {
	case 0:
		par := make([]float64, len(m.pars))
		for j := range m.pars {
			par[j] = m.pars[j].val
		}
		f = m.eval(par)
	default:
		saveStatus, saveEDM, saveFMin := m.status, m.edm, m.fmin
		saveCov, saveHesse := m.cov, m.hasHesse

		_ = m.migradQuiet()
		f = m.fmin

		m.status, m.edm, m.fmin = saveStatus, saveEDM, saveFMin
		m.cov, m.hasHesse = saveCov, saveHesse
	}

	for j := range m.pars {
		m.pars[j].val = saveVals[j]
		m.pars[j].fixed = saveFixed[j]
		m.pars[j].err = saveErrs[j]
	}

	return f
}

// migradQuiet runs MIGRAD without saying anything, for the re-minimisations
// MINOS does inside its search.
func (m *Minuit) migradQuiet() error {
	save := m.print
	m.print = -1
	err := m.migrad(0, m.tol)
	m.print = save
	return err
}
