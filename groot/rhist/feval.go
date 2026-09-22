// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rhist

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"go-hep.org/x/hep/groot/rbase"
)

// fshapes matches the shorthands ROOT predefines for the shapes people fit,
// optionally followed by the parameter offset they start at: "gaus", "expo",
// "pol3", "gaus(0)", "pol1(3)".
var fshapes = regexp.MustCompile(`\b(gausn|gaus|expo|landaun|landau|pol(\d+)|cheb(\d+))\b(\s*\(\s*(\d+)\s*\))?`)

// expandShapes rewrites ROOT's predefined shape names into the expressions
// they stand for, so that the parser only ever sees arithmetic.
//
// The number in parentheses, where ROOT allows one, is the index of the first
// parameter the shape uses: "gaus(3)" is a gaussian over [3], [4] and [5].
func expandShapes(expr string) (string, error) {
	var err error
	out := fshapes.ReplaceAllStringFunc(expr, func(m string) string {
		if err != nil {
			return m
		}
		sub := fshapes.FindStringSubmatch(m)
		var (
			name = sub[1]
			off  = 0
		)
		if sub[5] != "" {
			off, _ = strconv.Atoi(sub[5])
		}

		p := func(i int) string { return "[" + strconv.Itoa(off+i) + "]" }

		switch {
		case name == "gaus":
			return fmt.Sprintf("(%s*exp(-0.5*((x-%s)/%s)^2))", p(0), p(1), p(2))
		case name == "gausn":
			return fmt.Sprintf("(%s/(sqrt(2*pi())*%s)*exp(-0.5*((x-%s)/%s)^2))", p(0), p(2), p(1), p(2))
		case name == "expo":
			return fmt.Sprintf("(exp(%s+%s*x))", p(0), p(1))
		case strings.HasPrefix(name, "pol"):
			n, _ := strconv.Atoi(sub[2])
			terms := make([]string, 0, n+1)
			for i := range n + 1 {
				switch i {
				case 0:
					terms = append(terms, p(0))
				case 1:
					terms = append(terms, p(1)+"*x")
				default:
					terms = append(terms, p(i)+"*x^"+strconv.Itoa(i))
				}
			}
			return "(" + strings.Join(terms, "+") + ")"
		}

		// landau needs TMath::Landau, and chebyshev needs ROOT's basis: both
		// are shapes groot would have to approximate, and a formula that
		// quietly evaluates to the wrong number is worse than one that says
		// it cannot be evaluated.
		err = fmt.Errorf("rhist: formula shape %q is not supported", name)
		return m
	})
	return out, err
}

// compileFormula parses expr into a tree, resolving parameter names through
// params.
//
// When alloc is true a name params does not hold is given the next free index
// and added to it, which is how a formula written from scratch names its own
// parameters. When it is false an unknown name is an error, since a formula
// read from a file carries the names it was written with.
func compileFormula(expr string, params map[string]int32, alloc bool) (fnode, int, error) {
	expr, err := expandShapes(expr)
	if err != nil {
		return nil, 0, err
	}

	lx := &flex{src: expr}
	err = lx.lex()
	if err != nil {
		return nil, 0, err
	}

	p := &fparser{
		lx:   lx,
		toks: lx.toks,
		params: func(name string) (int, error) {
			if i, err := strconv.Atoi(name); err == nil {
				if i < 0 {
					return 0, fmt.Errorf("rhist: negative parameter index %d", i)
				}
				return i, nil
			}
			if i, ok := params[name]; ok {
				return int(i), nil
			}
			if !alloc {
				return 0, fmt.Errorf("rhist: unknown parameter %q in formula %q", name, expr)
			}
			i := int32(len(params))
			params[name] = i
			return int(i), nil
		},
	}

	n, err := p.parse()
	if err != nil {
		return nil, 0, err
	}
	return n, p.npar, nil
}

// Expr returns the expression this formula evaluates.
func (f *Formula) Expr() string {
	return f.formula
}

// Params returns the parameter values of this formula.
func (f *Formula) Params() []float64 {
	return f.clingParams
}

// SetParams sets the parameter values of this formula.
func (f *Formula) SetParams(ps []float64) {
	f.clingParams = make([]float64, len(ps))
	copy(f.clingParams, ps)
	f.allParamsSet = true
}

// ParamNames returns the names this formula gives its parameters, indexed the
// way the formula indexes them. A parameter the formula only ever refers to
// by number has an empty name.
func (f *Formula) ParamNames() []string {
	if len(f.params) == 0 {
		return nil
	}
	n := 0
	for _, i := range f.params {
		if int(i)+1 > n {
			n = int(i) + 1
		}
	}
	o := make([]string, n)
	for name, i := range f.params {
		o[i] = name
	}
	return o
}

// Func compiles this formula and returns it as a Go function of the
// variables x, y, z and t, in that order. Missing variables read as zero.
//
// Func returns an error if the formula uses something groot cannot evaluate.
func (f *Formula) Func() (func(vars ...float64) float64, error) {
	node, npar, err := compileFormula(f.formula, f.params, false)
	if err != nil {
		return nil, err
	}

	params := make([]float64, max(npar, len(f.clingParams)))
	copy(params, f.clingParams)

	return func(vars ...float64) float64 {
		env := fenv{vars: vars, params: params}
		return node.eval(&env)
	}, nil
}

// Eval evaluates this formula at the given values of x, y, z and t.
//
// Eval compiles the formula on every call: use Func to evaluate a formula
// more than once.
func (f *Formula) Eval(vars ...float64) (float64, error) {
	fct, err := f.Func()
	if err != nil {
		return 0, err
	}
	return fct(vars...), nil
}

// NewFormula creates a ROOT TFormula from an expression, the way
// TFormula(name, expr) does in C++.
//
// NewFormula returns an error if the expression uses something groot cannot
// evaluate, so that a formula in hand is always one that can be evaluated.
func NewFormula(name, expr string) (*Formula, error) {
	f := newFormula()
	f.named = *rbase.NewNamed(name, expr)
	f.formula = expr
	f.params = make(map[string]int32)
	f.ndim = 1

	_, npar, err := compileFormula(expr, f.params, true)
	if err != nil {
		return nil, err
	}

	// a formula's parameters all start at zero, as they do in ROOT.
	f.clingParams = make([]float64, max(npar, len(f.params)))
	f.allParamsSet = true

	if len(f.params) == 0 {
		f.params = nil
	}

	return f, nil
}
