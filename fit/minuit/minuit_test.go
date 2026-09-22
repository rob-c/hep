// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package minuit_test

import (
	"math"
	"math/rand/v2"
	"testing"

	"go-hep.org/x/hep/fit/minuit"
)

const tol = 1e-4

// TestQuadratic minimises a function whose minimum, uncertainties and
// correlation are all known on paper.
//
// For f = sum over i of ((x_i - c_i)/s_i)^2 the minimum is at c, and since f
// rises by exactly 1 when x_i moves by s_i, the uncertainty on x_i is s_i
// with UP = 1. The parameters do not mix, so the correlation is zero.
func TestQuadratic(t *testing.T) {
	var (
		want = []float64{1.5, -2.5, 0.25}
		sig  = []float64{0.5, 2.0, 0.1}
	)

	m := minuit.New(len(want))
	m.SetFCN(minuit.FuncOf(func(par []float64) float64 {
		var f float64
		for i := range want {
			d := (par[i] - want[i]) / sig[i]
			f += d * d
		}
		return f
	}))

	for i := range want {
		err := m.Parameter(i, "p", 0, 1, 0, 0)
		if err != nil {
			t.Fatalf("could not set parameter %d: %+v", i, err)
		}
	}

	err := m.Command("MIGRAD", 1000, 0.1)
	if err != nil {
		t.Fatalf("could not run MIGRAD: %+v", err)
	}

	if got := m.Status(); got != minuit.Converged {
		t.Fatalf("status: got=%v, want=%v", got, minuit.Converged)
	}
	if got := m.FMin(); math.Abs(got) > tol {
		t.Errorf("fmin: got=%v, want=0", got)
	}

	for i := range want {
		val, _, free := m.Value(i)
		if !free {
			t.Errorf("parameter %d is not free", i)
		}
		if math.Abs(val-want[i]) > 1e-3 {
			t.Errorf("parameter %d: got=%v, want=%v", i, val, want[i])
		}
	}

	// HESSE computes the uncertainties where the fit ended, rather than
	// taking what MIGRAD's metric happened to accumulate.
	err = m.Command("HESSE")
	if err != nil {
		t.Fatalf("could not run HESSE: %+v", err)
	}

	for i := range want {
		_, e, _ := m.Value(i)
		if math.Abs(e-sig[i]) > 1e-2*sig[i] {
			t.Errorf("parameter %d: error got=%v, want=%v", i, e, sig[i])
		}
	}

	cor := m.Correlation()
	if cor == nil {
		t.Fatal("no correlation matrix")
	}
	for i := range want {
		for j := range want {
			got := cor.At(i, j)
			w := 0.0
			if i == j {
				w = 1
			}
			if math.Abs(got-w) > 1e-2 {
				t.Errorf("correlation (%d,%d): got=%v, want=%v", i, j, got, w)
			}
		}
	}
}

// TestErrorDef checks UP does what it says: with UP = 4 the uncertainty is
// the distance over which the function rises by 4, which for a parabola is
// twice the one-sigma distance.
func TestErrorDef(t *testing.T) {
	fcn := minuit.FuncOf(func(par []float64) float64 {
		d := par[0] - 3
		return d * d // rises by 1 at a distance of 1
	})

	for _, tc := range []struct {
		up   float64
		want float64
	}{
		{1, 1},
		{4, 2},
		{0.25, 0.5},
	} {
		m := minuit.New(1)
		m.SetFCN(fcn)
		if err := m.Command("SET ERR", tc.up); err != nil {
			t.Fatalf("could not set the error definition: %+v", err)
		}
		if err := m.Parameter(0, "x", 0, 0.5, 0, 0); err != nil {
			t.Fatalf("could not set the parameter: %+v", err)
		}
		if err := m.Command("MIGRAD"); err != nil {
			t.Fatalf("could not run MIGRAD: %+v", err)
		}
		if err := m.Command("HESSE"); err != nil {
			t.Fatalf("could not run HESSE: %+v", err)
		}

		val, e, _ := m.Value(0)
		if math.Abs(val-3) > tol {
			t.Errorf("up=%v: value got=%v, want=3", tc.up, val)
		}
		if math.Abs(e-tc.want) > 1e-2 {
			t.Errorf("up=%v: error got=%v, want=%v", tc.up, e, tc.want)
		}
	}
}

// TestRosenbrock minimises the classic awkward function, whose minimum sits
// at (1,1) along a curved valley that defeats a naive descent.
func TestRosenbrock(t *testing.T) {
	m := minuit.New(2)
	m.SetFCN(minuit.FuncOf(func(par []float64) float64 {
		x, y := par[0], par[1]
		return 100*(y-x*x)*(y-x*x) + (1-x)*(1-x)
	}))

	if err := m.Parameter(0, "x", -1.2, 0.1, 0, 0); err != nil {
		t.Fatalf("could not set x: %+v", err)
	}
	if err := m.Parameter(1, "y", 1.0, 0.1, 0, 0); err != nil {
		t.Fatalf("could not set y: %+v", err)
	}

	if err := m.Command("MIGRAD", 5000, 0.1); err != nil {
		t.Fatalf("could not run MIGRAD: %+v", err)
	}

	x, _, _ := m.Value(0)
	y, _, _ := m.Value(1)
	if math.Abs(x-1) > 1e-2 || math.Abs(y-1) > 1e-2 {
		t.Fatalf("got=(%v,%v), want=(1,1) [fmin=%v, status=%v]", x, y, m.FMin(), m.Status())
	}
	if got := m.FMin(); got > 1e-4 {
		t.Errorf("fmin: got=%v, want~0", got)
	}
}

// TestLimits checks a parameter with limits converges to a minimum inside
// them, and is held inside them when the minimum is outside.
func TestLimits(t *testing.T) {
	t.Run("inside", func(t *testing.T) {
		m := minuit.New(1)
		m.SetFCN(minuit.FuncOf(func(par []float64) float64 {
			d := par[0] - 2
			return d * d
		}))
		if err := m.Parameter(0, "x", 1, 0.5, -10, 10); err != nil {
			t.Fatalf("could not set the parameter: %+v", err)
		}
		if err := m.Command("MIGRAD"); err != nil {
			t.Fatalf("could not run MIGRAD: %+v", err)
		}
		if val, _, _ := m.Value(0); math.Abs(val-2) > 1e-3 {
			t.Fatalf("got=%v, want=2", val)
		}
	})

	t.Run("outside", func(t *testing.T) {
		// the minimum is at 20, the parameter may not go past 5.
		m := minuit.New(1)
		m.SetFCN(minuit.FuncOf(func(par []float64) float64 {
			if par[0] < -5 || par[0] > 5 {
				t.Errorf("the function was called at %v, outside the limits", par[0])
			}
			d := par[0] - 20
			return d * d
		}))
		if err := m.Parameter(0, "x", 0, 0.5, -5, 5); err != nil {
			t.Fatalf("could not set the parameter: %+v", err)
		}
		if err := m.Command("MIGRAD", 2000, 0.1); err != nil {
			t.Fatalf("could not run MIGRAD: %+v", err)
		}
		if val, _, _ := m.Value(0); val < 4.5 || val > 5 {
			t.Fatalf("got=%v, want it pressed up against the limit at 5", val)
		}
	})
}

// TestFixRelease checks a fixed parameter stays put and a released one moves.
func TestFixRelease(t *testing.T) {
	fcn := minuit.FuncOf(func(par []float64) float64 {
		return (par[0]-1)*(par[0]-1) + (par[1]-2)*(par[1]-2)
	})

	m := minuit.New(2)
	m.SetFCN(fcn)
	if err := m.Parameter(0, "a", 0, 0.1, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := m.Parameter(1, "b", 0, 0.1, 0, 0); err != nil {
		t.Fatal(err)
	}

	if err := m.Command("FIX", 1); err != nil { // MINUIT counts from one
		t.Fatalf("could not fix: %+v", err)
	}
	if got, want := m.NFree(), 1; got != want {
		t.Fatalf("nfree: got=%d, want=%d", got, want)
	}
	if err := m.Command("MIGRAD"); err != nil {
		t.Fatalf("could not run MIGRAD: %+v", err)
	}

	if a, _, free := m.Value(0); a != 0 || free {
		t.Errorf("fixed parameter moved to %v (free=%v)", a, free)
	}
	if b, _, _ := m.Value(1); math.Abs(b-2) > 1e-3 {
		t.Errorf("free parameter: got=%v, want=2", b)
	}

	if err := m.Command("RELEASE", 1); err != nil {
		t.Fatalf("could not release: %+v", err)
	}
	if err := m.Command("MIGRAD"); err != nil {
		t.Fatalf("could not run MIGRAD: %+v", err)
	}
	if a, _, _ := m.Value(0); math.Abs(a-1) > 1e-3 {
		t.Errorf("released parameter: got=%v, want=1", a)
	}
}

// TestMinosSymmetric checks that for a parabola, where the two answers must
// agree, MINOS reproduces the parabolic uncertainty.
func TestMinosSymmetric(t *testing.T) {
	m := minuit.New(2)
	m.SetFCN(minuit.FuncOf(func(par []float64) float64 {
		a := (par[0] - 1) / 0.5
		b := (par[1] + 3) / 2.0
		return a*a + b*b
	}))

	if err := m.Parameter(0, "a", 0, 0.3, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := m.Parameter(1, "b", 0, 0.3, 0, 0); err != nil {
		t.Fatal(err)
	}

	if err := m.Command("MIGRAD"); err != nil {
		t.Fatalf("could not run MIGRAD: %+v", err)
	}
	if err := m.Command("HESSE"); err != nil {
		t.Fatalf("could not run HESSE: %+v", err)
	}
	if err := m.Command("MINOS"); err != nil {
		t.Fatalf("could not run MINOS: %+v", err)
	}

	for i, want := range []float64{0.5, 2.0} {
		eplus, eminus, eparab, _ := m.Errors(i)
		if math.Abs(eplus-want) > 2e-2*want {
			t.Errorf("parameter %d: e+ got=%v, want=%v", i, eplus, want)
		}
		if math.Abs(-eminus-want) > 2e-2*want {
			t.Errorf("parameter %d: e- got=%v, want=%v", i, eminus, -want)
		}
		if math.Abs(eparab-want) > 2e-2*want {
			t.Errorf("parameter %d: parabolic got=%v, want=%v", i, eparab, want)
		}
	}
}

// TestMinosAsymmetric checks MINOS finds the two sides of a function that is
// genuinely lopsided, where the parabolic uncertainty cannot tell them apart.
//
// f(x) = (exp(x) - 1)^2 rises much faster to the right of its minimum at 0
// than to the left, so the distance out to a rise of 1 is shorter on the
// right: exp(x)-1 = +1 gives x = ln2 = 0.693, and exp(x)-1 = -1 gives
// x -> -inf, so the left side runs to the largest x the search will reach.
func TestMinosAsymmetric(t *testing.T) {
	m := minuit.New(1)
	m.SetFCN(minuit.FuncOf(func(par []float64) float64 {
		d := math.Exp(par[0]) - 1
		return d * d
	}))

	if err := m.Parameter(0, "x", 0.1, 0.3, -5, 5); err != nil {
		t.Fatal(err)
	}
	if err := m.Command("MIGRAD"); err != nil {
		t.Fatalf("could not run MIGRAD: %+v", err)
	}
	if err := m.Command("MINOS"); err != nil {
		t.Fatalf("could not run MINOS: %+v", err)
	}

	eplus, eminus, _, _ := m.Errors(0)

	if math.Abs(eplus-math.Ln2) > 1e-2 {
		t.Errorf("e+: got=%v, want=%v", eplus, math.Ln2)
	}
	// the function never reaches a rise of 1 going left, so the search runs
	// out at the limit rather than finding a crossing.
	if -eminus <= eplus {
		t.Errorf("e-: got=%v, expected it to reach further than e+=%v", eminus, eplus)
	}
}

// TestSimplex checks the derivative-free minimiser gets to the same place.
func TestSimplex(t *testing.T) {
	m := minuit.New(2)
	m.SetFCN(minuit.FuncOf(func(par []float64) float64 {
		return (par[0]-3)*(par[0]-3) + (par[1]+1)*(par[1]+1) + 7
	}))

	if err := m.Parameter(0, "a", 0, 0.5, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := m.Parameter(1, "b", 0, 0.5, 0, 0); err != nil {
		t.Fatal(err)
	}

	if err := m.Command("SIMPLEX", 2000, 0.1); err != nil {
		t.Fatalf("could not run SIMPLEX: %+v", err)
	}

	a, _, _ := m.Value(0)
	b, _, _ := m.Value(1)
	if math.Abs(a-3) > 1e-2 || math.Abs(b+1) > 1e-2 {
		t.Fatalf("got=(%v,%v), want=(3,-1)", a, b)
	}
	if got := m.FMin(); math.Abs(got-7) > 1e-3 {
		t.Errorf("fmin: got=%v, want=7", got)
	}
}

// TestCorrelatedFit fits a straight line to noisy data and checks the fitted
// parameters, their uncertainties and the correlation between them are the
// ones least squares gives on paper.
func TestCorrelatedFit(t *testing.T) {
	const (
		slope  = 2.0
		offset = -1.0
		sigma  = 0.5
		n      = 200
	)

	rnd := rand.New(rand.NewPCG(42, 1234))
	xs := make([]float64, n)
	ys := make([]float64, n)
	for i := range n {
		xs[i] = float64(i) / n * 10
		ys[i] = offset + slope*xs[i] + rnd.NormFloat64()*sigma
	}

	m := minuit.New(2)
	m.SetFCN(minuit.FuncOf(func(par []float64) float64 {
		var chi2 float64
		for i := range xs {
			d := (ys[i] - (par[0] + par[1]*xs[i])) / sigma
			chi2 += d * d
		}
		return chi2
	}))

	if err := m.Parameter(0, "offset", 0, 0.5, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := m.Parameter(1, "slope", 0, 0.5, 0, 0); err != nil {
		t.Fatal(err)
	}

	if err := m.Command("MIGRAD", 2000, 0.1); err != nil {
		t.Fatalf("could not run MIGRAD: %+v", err)
	}
	if err := m.Command("HESSE"); err != nil {
		t.Fatalf("could not run HESSE: %+v", err)
	}

	off, offErr, _ := m.Value(0)
	slp, slpErr, _ := m.Value(1)

	// the fit should land within a few of its own uncertainties of the truth.
	if math.Abs(off-offset) > 5*offErr {
		t.Errorf("offset: got=%v +/- %v, want=%v", off, offErr, offset)
	}
	if math.Abs(slp-slope) > 5*slpErr {
		t.Errorf("slope: got=%v +/- %v, want=%v", slp, slpErr, slope)
	}

	// chi-square per degree of freedom should be about one.
	if ndf := float64(n - 2); math.Abs(m.FMin()/ndf-1) > 0.3 {
		t.Errorf("chi2/ndf: got=%v, want~1", m.FMin()/ndf)
	}

	// fitting an offset and a slope over x in [0,10] leaves them strongly
	// anti-correlated: raising the offset must lower the slope.
	cor := m.Correlation()
	if got := cor.At(0, 1); got > -0.5 {
		t.Errorf("correlation: got=%v, want it strongly negative", got)
	}
}

// TestUnknownCommand checks a command minuit does not know is refused rather
// than quietly doing nothing.
func TestUnknownCommand(t *testing.T) {
	m := minuit.New(1)
	m.SetFCN(minuit.FuncOf(func(par []float64) float64 { return par[0] * par[0] }))
	if err := m.Parameter(0, "x", 1, 0.1, 0, 0); err != nil {
		t.Fatal(err)
	}

	err := m.Command("SCAN")
	if err == nil {
		t.Fatal("expected an error for a command minuit does not know")
	}
}

// TestNoFCN checks a command before SetFCN is refused.
func TestNoFCN(t *testing.T) {
	m := minuit.New(1)
	if err := m.Command("MIGRAD"); err == nil {
		t.Fatal("expected an error when there is no function to minimise")
	}
}

// TestIFlag checks the function is told where in the fit each call is.
func TestIFlag(t *testing.T) {
	seen := make(map[int]int)
	m := minuit.New(1)
	m.SetFCN(func(npar int, grad []float64, par []float64, iflag int) float64 {
		seen[iflag]++
		return par[0] * par[0]
	})
	if err := m.Parameter(0, "x", 1, 0.1, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := m.Command("MIGRAD"); err != nil {
		t.Fatalf("could not run MIGRAD: %+v", err)
	}

	if seen[minuit.IFlagInit] != 1 {
		t.Errorf("init calls: got=%d, want=1", seen[minuit.IFlagInit])
	}
	if seen[minuit.IFlagEnd] != 1 {
		t.Errorf("end calls: got=%d, want=1", seen[minuit.IFlagEnd])
	}
	if seen[minuit.IFlagVal] == 0 {
		t.Error("the function was never called to be evaluated")
	}
}

// TestInfiniteLimits checks a one-sided limit is refused rather than being
// turned into an infinity inside the sine that varies a limited parameter.
func TestInfiniteLimits(t *testing.T) {
	m := minuit.New(1)
	m.SetFCN(minuit.FuncOf(func(par []float64) float64 { return par[0] * par[0] }))

	for _, tc := range []struct{ lo, hi float64 }{
		{0, math.Inf(+1)},
		{math.Inf(-1), 0},
		{math.NaN(), 1},
	} {
		if err := m.Parameter(0, "x", 1, 0.1, tc.lo, tc.hi); err == nil {
			t.Errorf("limits [%v, %v] were accepted", tc.lo, tc.hi)
		}
	}

	// no limits at all is fine, and is what lo == hi means.
	if err := m.Parameter(0, "x", 1, 0.1, 0, 0); err != nil {
		t.Errorf("unbounded parameter refused: %+v", err)
	}
}

// TestParameterOutsideLimits checks a starting value outside the limits is
// refused, rather than being silently moved.
func TestParameterOutsideLimits(t *testing.T) {
	m := minuit.New(1)
	m.SetFCN(minuit.FuncOf(func(par []float64) float64 { return par[0] }))
	if err := m.Parameter(0, "x", 10, 0.1, -1, 1); err == nil {
		t.Fatal("a starting value outside the limits was accepted")
	}
}
