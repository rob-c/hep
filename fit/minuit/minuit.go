// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package minuit minimises a function of several parameters and estimates
// the uncertainties on the parameters at the minimum.
//
// It offers the commands and the call sequence of ROOT's TMinuit — MIGRAD,
// HESSE, MINOS, SIMPLEX, parameter limits, the error definition UP — so that
// a fit written against TMinuit reads the same way here:
//
//	m := minuit.New(3)
//	m.SetFCN(fcn)
//	m.Command("SET ERR", 1)
//	m.Parameter(0, "mean", 0, 0.1, 0, 0)
//	m.Command("MIGRAD", 500, 0.1)
//	val, err, _ := m.Value(0)
//
// # Provenance
//
// This is a fresh implementation in Go of the methods MINUIT is built on,
// written from their published descriptions:
//
//   - F. James and M. Roos, "Minuit — a system for function minimization and
//     analysis of the parameter errors and correlations", Computer Physics
//     Communications 10 (1975) 343-367.
//   - F. James, "MINUIT Function Minimization and Error Analysis, Reference
//     Manual", CERN Program Library Long Writeup D506.
//
// No code was taken from MINUIT, from its Fortran original or from ROOT's
// C++ transliteration of it: both are covered by the GNU (Lesser) General
// Public License and go-hep is BSD licensed. What is shared with them is the
// mathematics, which is not anyone's to license, and the interface, so that
// the two can be used the same way.
//
// # What the numbers mean
//
// UP, set by "SET ERR", is the amount by which the function rises from its
// minimum at one standard deviation: 1 for a chi-square, 0.5 for a negative
// log-likelihood. Every uncertainty reported here is in those terms.
//
// EDM is the estimated distance to the minimum: half the gradient contracted
// twice with the inverse of the second-derivative matrix, which for a
// quadratic function is exactly how far the function value still has to fall.
// MIGRAD stops when it drops below 1e-3 * tolerance * UP.
package minuit // import "go-hep.org/x/hep/fit/minuit"

import (
	"fmt"
	"io"
	"math"
	"strings"

	"gonum.org/v1/gonum/mat"
)

// FCN is a function to be minimised, in the shape TMinuit calls it.
//
// par holds the current values of every parameter, fixed ones included.
// grad is where a function that computes its own gradient writes it, and is
// nil for one that does not. iflag says what the call is for: [IFlagInit]
// once at the start, [IFlagGrad] when a gradient is wanted, [IFlagEnd] once
// at the end, and [IFlagVal] for an ordinary evaluation.
//
// FuncOf turns a plain function of the parameters into an FCN.
type FCN func(npar int, grad []float64, par []float64, iflag int) float64

// The values iflag takes, matching TMinuit's.
const (
	IFlagInit = 1 // first call: initialise whatever the function needs
	IFlagGrad = 2 // compute the gradient into grad
	IFlagEnd  = 3 // last call: the fit is over
	IFlagVal  = 4 // ordinary call: just return the value
)

// FuncOf turns a plain function of the parameters into an FCN, for the common
// case of a function that neither computes its own gradient nor cares to be
// told where in the fit it is.
func FuncOf(f func(par []float64) float64) FCN {
	return func(npar int, grad []float64, par []float64, iflag int) float64 {
		return f(par)
	}
}

// Status describes how a minimisation ended.
type Status int

const (
	// NotCalculated means no minimisation has been run yet.
	NotCalculated Status = iota
	// Converged means the minimiser reached the accuracy asked of it.
	Converged
	// CallLimit means it ran out of function calls first.
	CallLimit
	// Failed means it could not make progress.
	Failed
)

func (s Status) String() string {
	switch s {
	case NotCalculated:
		return "not calculated"
	case Converged:
		return "converged"
	case CallLimit:
		return "call limit reached"
	case Failed:
		return "failed"
	}
	return fmt.Sprintf("Status(%d)", int(s))
}

// Minuit minimises a function of several parameters.
//
// The zero value is not usable: make one with New.
type Minuit struct {
	fcn  FCN
	pars []param

	up       float64 // the rise in the function that defines one sigma
	tol      float64 // MIGRAD tolerance
	maxCalls int
	strategy int
	print    int
	out      io.Writer

	fmin   float64
	edm    float64
	ncalls int
	status Status

	// cov is the covariance of the free parameters, in external units,
	// in the order the free parameters appear in pars.
	cov *mat.SymDense
	// hasHesse says whether cov came from HESSE rather than from the
	// matrix MIGRAD built up on its way to the minimum.
	hasHesse bool
}

// New returns a Minuit ready to take up to maxpar parameters.
//
// The defaults are TMinuit's: UP is 1, so the function is taken to be a
// chi-square, the MIGRAD tolerance is 0.1 and the strategy is 1.
func New(maxpar int) *Minuit {
	return &Minuit{
		pars:     make([]param, 0, maxpar),
		up:       1,
		tol:      0.1,
		maxCalls: 0, // decided per command
		strategy: 1,
		print:    0,
		out:      io.Discard,
	}
}

// SetFCN sets the function to minimise. It corresponds to TMinuit::SetFCN.
func (m *Minuit) SetFCN(fcn FCN) {
	m.fcn = fcn
}

// SetOutput sends what the print level asks for to w.
func (m *Minuit) SetOutput(w io.Writer) {
	if w == nil {
		w = io.Discard
	}
	m.out = w
}

// SetPrintLevel sets how much the minimiser says for itself: -1 for nothing,
// 0 for the result, 1 or more for each step. It corresponds to
// TMinuit::SetPrintLevel.
func (m *Minuit) SetPrintLevel(lvl int) {
	m.print = lvl
}

// Parameter defines the i-th parameter, as TMinuit::mnparm does.
//
// start is where the fit begins and step is the expected uncertainty, which
// sets the scale the minimiser works at; a step of zero fixes the parameter.
// Giving lo == hi leaves the parameter unbounded.
func (m *Minuit) Parameter(i int, name string, start, step, lo, hi float64) error {
	switch {
	case i < 0:
		return fmt.Errorf("minuit: negative parameter index %d", i)
	case lo > hi:
		return fmt.Errorf(
			"minuit: parameter %d (%q) has its lower limit above its upper one (%v > %v)",
			i, name, lo, hi,
		)
	case lo != hi && (math.IsInf(lo, 0) || math.IsInf(hi, 0) || math.IsNaN(lo) || math.IsNaN(hi)):
		// a limited parameter is varied through a sine of the internal one,
		// which needs both ends to be finite. A parameter bounded on one
		// side only is not something MINUIT does: leave it unbounded, or
		// give it a far-away bound on the other side.
		return fmt.Errorf(
			"minuit: parameter %d (%q) needs two finite limits, got [%v, %v]",
			i, name, lo, hi,
		)
	}

	for len(m.pars) <= i {
		m.pars = append(m.pars, param{name: fmt.Sprintf("p%d", len(m.pars))})
	}

	p := param{
		name:  name,
		val:   start,
		err:   math.Abs(step),
		lo:    lo,
		hi:    hi,
		lim:   lo != hi,
		fixed: step == 0,
	}

	if p.lim && (start < lo || start > hi) {
		return fmt.Errorf(
			"minuit: parameter %d (%q) starts at %v, outside its limits [%v, %v]",
			i, name, start, lo, hi,
		)
	}

	m.pars[i] = p
	m.invalidate()
	return nil
}

// NPar returns the number of parameters that have been defined.
func (m *Minuit) NPar() int { return len(m.pars) }

// NFree returns the number of parameters that are free to vary.
func (m *Minuit) NFree() int {
	n := 0
	for i := range m.pars {
		if !m.pars[i].fixed {
			n++
		}
	}
	return n
}

// Fix stops the i-th parameter from varying, as the FIX command does.
func (m *Minuit) Fix(i int) error {
	if i < 0 || i >= len(m.pars) {
		return fmt.Errorf("minuit: no parameter %d", i)
	}
	m.pars[i].fixed = true
	m.invalidate()
	return nil
}

// Release lets the i-th parameter vary again, as the RELEASE command does.
func (m *Minuit) Release(i int) error {
	if i < 0 || i >= len(m.pars) {
		return fmt.Errorf("minuit: no parameter %d", i)
	}
	if m.pars[i].err == 0 {
		// a parameter fixed by a zero step has no scale to vary on.
		m.pars[i].err = 0.1 * math.Max(1, math.Abs(m.pars[i].val))
	}
	m.pars[i].fixed = false
	m.invalidate()
	return nil
}

// SetErrorDef sets UP, the rise in the function that defines one standard
// deviation: 1 for a chi-square, 0.5 for a negative log-likelihood. It
// corresponds to TMinuit::SetErrorDef and to the "SET ERR" command.
func (m *Minuit) SetErrorDef(up float64) error {
	if up <= 0 {
		return fmt.Errorf("minuit: the error definition must be positive, got %v", up)
	}
	m.up = up
	return nil
}

// ErrorDef returns UP.
func (m *Minuit) ErrorDef() float64 { return m.up }

// SetStrategy sets how much work the minimiser puts into the accuracy of its
// derivatives: 0 for the least, 1 for the default, 2 for the most.
func (m *Minuit) SetStrategy(n int) error {
	if n < 0 || n > 2 {
		return fmt.Errorf("minuit: strategy must be 0, 1 or 2, got %d", n)
	}
	m.strategy = n
	return nil
}

// Value returns the value of the i-th parameter, its parabolic uncertainty
// and whether it is free to vary. It is TMinuit::mnpout, without the
// parameter name and limits, which Parameters gives.
func (m *Minuit) Value(i int) (val, err float64, free bool) {
	if i < 0 || i >= len(m.pars) {
		return 0, 0, false
	}
	p := &m.pars[i]
	return p.val, p.err, !p.fixed
}

// Parameters returns every parameter, in the order they were defined.
func (m *Minuit) Parameters() []Parameter {
	o := make([]Parameter, len(m.pars))
	for i := range m.pars {
		p := &m.pars[i]
		o[i] = Parameter{
			Name:   p.name,
			Value:  p.val,
			Error:  p.err,
			Min:    p.lo,
			Max:    p.hi,
			Limits: p.lim,
			Fixed:  p.fixed,
			EPlus:  p.eplus,
			EMinus: p.eminus,
		}
	}
	return o
}

// Parameter describes one parameter at the end of a fit.
type Parameter struct {
	Name  string
	Value float64
	Error float64 // the parabolic uncertainty

	Min, Max float64 // the limits, when Limits is true
	Limits   bool
	Fixed    bool

	// EPlus and EMinus are the asymmetric uncertainties MINOS found, and
	// are zero until MINOS has run.
	EPlus, EMinus float64
}

// Errors returns the uncertainties on the i-th parameter: the one MINOS found
// above and below the minimum, the parabolic one, and the global correlation
// coefficient. It corresponds to TMinuit::mnerrs.
//
// The MINOS uncertainties are zero until MINOS has run.
func (m *Minuit) Errors(i int) (eplus, eminus, eparab, gcc float64) {
	if i < 0 || i >= len(m.pars) {
		return 0, 0, 0, 0
	}
	p := &m.pars[i]
	return p.eplus, p.eminus, p.err, m.globalCC(i)
}

// Stats returns the state of the fit: the function value at the minimum, the
// estimated distance to the minimum, UP, and how many parameters are free and
// defined. It corresponds to TMinuit::mnstat.
func (m *Minuit) Stats() (fmin, edm, up float64, nfree, npar int, status Status) {
	return m.fmin, m.edm, m.up, m.NFree(), len(m.pars), m.status
}

// FMin returns the function value at the minimum.
func (m *Minuit) FMin() float64 { return m.fmin }

// EDM returns the estimated distance to the minimum.
func (m *Minuit) EDM() float64 { return m.edm }

// NCalls returns how many times the function has been called.
func (m *Minuit) NCalls() int { return m.ncalls }

// Status returns how the last minimisation ended.
func (m *Minuit) Status() Status { return m.status }

// Covariance returns the covariance of the free parameters, in the order they
// were defined. It corresponds to TMinuit::mnemat.
//
// Covariance returns nil before a minimisation has produced one.
func (m *Minuit) Covariance() *mat.SymDense {
	if m.cov == nil {
		return nil
	}
	n := m.cov.SymmetricDim()
	o := mat.NewSymDense(n, nil)
	o.CopySym(m.cov)
	return o
}

// Correlation returns the correlation of the free parameters, in the order
// they were defined, or nil before there is a covariance to derive it from.
func (m *Minuit) Correlation() *mat.SymDense {
	if m.cov == nil {
		return nil
	}
	n := m.cov.SymmetricDim()
	o := mat.NewSymDense(n, nil)
	for i := range n {
		for j := i; j < n; j++ {
			d := math.Sqrt(m.cov.At(i, i) * m.cov.At(j, j))
			if d == 0 {
				continue
			}
			o.SetSym(i, j, m.cov.At(i, j)/d)
		}
	}
	return o
}

// globalCC returns the global correlation coefficient of the i-th parameter:
// how correlated it is with the best linear combination of all the others.
func (m *Minuit) globalCC(i int) float64 {
	if m.cov == nil || m.pars[i].fixed {
		return 0
	}

	k := m.freeIndex(i)
	if k < 0 {
		return 0
	}

	var inv mat.SymDense
	err := invertSym(&inv, m.cov)
	if err != nil {
		return 0
	}

	d := m.cov.At(k, k) * inv.At(k, k)
	if d <= 0 {
		return 0
	}
	gcc := 1 - 1/d
	if gcc < 0 {
		return 0
	}
	return math.Sqrt(gcc)
}

// freeIndex returns the position of parameter i among the free ones, or -1.
func (m *Minuit) freeIndex(i int) int {
	k := 0
	for j := range m.pars {
		if m.pars[j].fixed {
			continue
		}
		if j == i {
			return k
		}
		k++
	}
	return -1
}

// free returns the indices of the parameters that are free to vary.
func (m *Minuit) free() []int {
	o := make([]int, 0, len(m.pars))
	for i := range m.pars {
		if !m.pars[i].fixed {
			o = append(o, i)
		}
	}
	return o
}

// invalidate drops results that the change just made no longer supports.
func (m *Minuit) invalidate() {
	m.cov = nil
	m.hasHesse = false
	m.edm = 0
	m.status = NotCalculated
}

// eval calls the function with the given external parameter values.
func (m *Minuit) eval(par []float64) float64 {
	m.ncalls++
	return m.fcn(m.NFree(), nil, par, IFlagVal)
}

// evalInt calls the function with internal values for the free parameters,
// leaving the fixed ones where they are.
func (m *Minuit) evalInt(x []float64) float64 {
	return m.eval(m.external(x))
}

// external builds the full parameter vector from the internal values of the
// free parameters.
func (m *Minuit) external(x []float64) []float64 {
	par := make([]float64, len(m.pars))
	for i := range m.pars {
		par[i] = m.pars[i].val
	}
	for k, i := range m.free() {
		par[i] = m.pars[i].int2ext(x[k])
	}
	return par
}

// internal returns the internal values of the free parameters.
func (m *Minuit) internal() []float64 {
	free := m.free()
	x := make([]float64, len(free))
	for k, i := range free {
		x[k] = m.pars[i].ext2int(m.pars[i].val)
	}
	return x
}

// store writes internal values back onto the parameters.
func (m *Minuit) store(x []float64) {
	for k, i := range m.free() {
		m.pars[i].val = m.pars[i].int2ext(x[k])
	}
}

// Command runs a MINUIT command, as TMinuit::mnexcm does.
//
// The commands are:
//
//	MIGRAD [maxcalls [tolerance]]   minimise
//	MINIMIZE [maxcalls [tolerance]] minimise, falling back on SIMPLEX
//	SIMPLEX [maxcalls [tolerance]]  minimise without derivatives
//	HESSE [maxcalls]                second derivatives, and the covariance
//	MINOS [maxcalls [par...]]       asymmetric uncertainties
//	SET ERR up                      the rise that defines one sigma
//	SET LIMITS par lo hi            put limits on a parameter
//	SET PRINT lvl                   how much to say
//	SET STRATEGY n                  how hard to work at derivatives
//	FIX par...                      stop parameters varying
//	RELEASE par...                  let them vary again
//	CALL FCN iflag                  call the function once
//	CLEAR                           forget every parameter
//
// Command returns an error for a command it does not know, rather than
// ignoring it.
func (m *Minuit) Command(cmd string, args ...float64) error {
	if m.fcn == nil {
		return fmt.Errorf("minuit: no function to minimise: call SetFCN first")
	}

	key := strings.ToUpper(strings.Join(strings.Fields(cmd), " "))

	arg := func(i int, def float64) float64 {
		if i < len(args) {
			return args[i]
		}
		return def
	}

	switch {
	case key == "MIGRAD":
		return m.migrad(int(arg(0, 0)), arg(1, m.tol))

	case key == "MINIMIZE" || key == "MINI":
		err := m.migrad(int(arg(0, 0)), arg(1, m.tol))
		if err == nil && m.status == Converged {
			return nil
		}
		// MINIMIZE falls back on SIMPLEX and tries once more.
		if err := m.simplex(int(arg(0, 0)), arg(1, m.tol)); err != nil {
			return err
		}
		return m.migrad(int(arg(0, 0)), arg(1, m.tol))

	case key == "SIMPLEX" || key == "SIM":
		return m.simplex(int(arg(0, 0)), arg(1, m.tol))

	case key == "HESSE" || key == "HES":
		return m.hesse(int(arg(0, 0)))

	case key == "MINOS" || key == "MIN":
		var which []int
		for _, v := range args[min(1, len(args)):] {
			which = append(which, int(v)-1) // MINUIT counts from one
		}
		return m.minos(int(arg(0, 0)), which)

	case key == "SET ERR" || key == "SET ERRORDEF" || key == "SET ERRDEF":
		if len(args) < 1 {
			return fmt.Errorf("minuit: %q needs the error definition", cmd)
		}
		return m.SetErrorDef(args[0])

	case key == "SET PRINT" || key == "SET PRI":
		if len(args) < 1 {
			return fmt.Errorf("minuit: %q needs a print level", cmd)
		}
		m.SetPrintLevel(int(args[0]))
		return nil

	case key == "SET STRATEGY" || key == "SET STR":
		if len(args) < 1 {
			return fmt.Errorf("minuit: %q needs a strategy", cmd)
		}
		return m.SetStrategy(int(args[0]))

	case key == "SET LIMITS" || key == "SET LIM":
		if len(args) < 3 {
			return fmt.Errorf("minuit: %q needs a parameter and two limits", cmd)
		}
		return m.setLimits(int(args[0])-1, args[1], args[2])

	case key == "FIX":
		if len(args) < 1 {
			return fmt.Errorf("minuit: %q needs a parameter", cmd)
		}
		for _, v := range args {
			if err := m.Fix(int(v) - 1); err != nil {
				return err
			}
		}
		return nil

	case key == "RELEASE" || key == "REL":
		if len(args) < 1 {
			return fmt.Errorf("minuit: %q needs a parameter", cmd)
		}
		for _, v := range args {
			if err := m.Release(int(v) - 1); err != nil {
				return err
			}
		}
		return nil

	case key == "CALL FCN" || key == "CALL":
		par := make([]float64, len(m.pars))
		for i := range m.pars {
			par[i] = m.pars[i].val
		}
		m.ncalls++
		m.fmin = m.fcn(m.NFree(), nil, par, int(arg(0, IFlagVal)))
		return nil

	case key == "CLEAR" || key == "CLE":
		m.pars = m.pars[:0]
		m.invalidate()
		m.fmin = 0
		m.ncalls = 0
		return nil
	}

	return fmt.Errorf("minuit: unknown command %q", cmd)
}

func (m *Minuit) setLimits(i int, lo, hi float64) error {
	if i < 0 || i >= len(m.pars) {
		return fmt.Errorf("minuit: no parameter %d", i+1)
	}
	if lo > hi {
		return fmt.Errorf("minuit: lower limit %v is above upper limit %v", lo, hi)
	}
	p := &m.pars[i]
	p.lo, p.hi, p.lim = lo, hi, lo != hi
	if p.lim {
		p.val = math.Min(math.Max(p.val, lo), hi)
	}
	m.invalidate()
	return nil
}

// Migrad minimises the function, as the MIGRAD command does.
func (m *Minuit) Migrad() error { return m.Command("MIGRAD") }

// Hesse computes the second derivatives and the covariance from them, as the
// HESSE command does.
func (m *Minuit) Hesse() error { return m.Command("HESSE") }

// Minos finds the asymmetric uncertainties, as the MINOS command does.
func (m *Minuit) Minos() error { return m.Command("MINOS") }

// Print writes the state of the fit to the writer set by SetOutput.
func (m *Minuit) Print() {
	fmt.Fprintf(m.out, "fmin=%v edm=%v ncalls=%d status=%v\n", m.fmin, m.edm, m.ncalls, m.status)
	for _, p := range m.Parameters() {
		switch {
		case p.Fixed:
			fmt.Fprintf(m.out, "  %-12s %12.6g  (fixed)\n", p.Name, p.Value)
		case p.EPlus != 0 || p.EMinus != 0:
			fmt.Fprintf(m.out, "  %-12s %12.6g +/- %-12.6g  +%-12.6g %-12.6g\n",
				p.Name, p.Value, p.Error, p.EPlus, p.EMinus,
			)
		default:
			fmt.Fprintf(m.out, "  %-12s %12.6g +/- %-12.6g\n", p.Name, p.Value, p.Error)
		}
	}
}

// invertSym inverts a symmetric matrix, falling back on a pseudo-inverse
// built from its eigen-decomposition when it is singular — which happens when
// a parameter has no effect on the function at all, and should give an
// infinite uncertainty rather than an error.
func invertSym(dst *mat.SymDense, src *mat.SymDense) error {
	n := src.SymmetricDim()
	var chol mat.Cholesky
	if ok := chol.Factorize(src); ok {
		var inv mat.SymDense
		err := chol.InverseTo(&inv)
		if err == nil {
			// size dst first: CopySym copies the smaller of the two
			// matrices, and would quietly copy nothing into an empty one.
			dst.Reset()
			dst.ReuseAsSym(n)
			dst.CopySym(&inv)
			return nil
		}
	}

	var eig mat.EigenSym
	if ok := eig.Factorize(src, true); !ok {
		return fmt.Errorf("minuit: could not invert the second-derivative matrix")
	}

	var vec mat.Dense
	eig.VectorsTo(&vec)
	vals := eig.Values(nil)

	// the largest eigenvalue sets the scale below which one counts as zero.
	var maxv float64
	for _, v := range vals {
		maxv = math.Max(maxv, math.Abs(v))
	}
	tol := 1e-12 * maxv

	dst.Reset()
	dst.ReuseAsSym(n)
	for i := range n {
		for j := i; j < n; j++ {
			var sum float64
			for k := range n {
				if math.Abs(vals[k]) <= tol {
					continue
				}
				sum += vec.At(i, k) * vec.At(j, k) / vals[k]
			}
			dst.SetSym(i, j, sum)
		}
	}
	return nil
}
