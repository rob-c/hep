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
	var (
		err  error
		out  strings.Builder
		last int
	)

	for _, m := range fshapes.FindAllStringSubmatchIndex(expr, -1) {
		if err != nil {
			break
		}

		var (
			whole = expr[m[0]:m[1]]
			name  = expr[m[2]:m[3]]
			off   = 0
		)

		// The offset group, when there is one: "gaus(3)" starts at
		// parameter three.
		hasOffset := m[10] >= 0
		if hasOffset {
			off, _ = strconv.Atoi(expr[m[10]:m[11]])
		}

		// "landau" is both a shape and a function, as it is in ROOT: the
		// shape is the bare word and the function is a call. A name
		// followed by an open bracket that was not the offset is a call,
		// and is left alone for the parser to deal with.
		if !hasOffset && strings.HasPrefix(strings.TrimLeft(expr[m[1]:], " \t"), "(") {
			continue
		}

		out.WriteString(expr[last:m[0]])
		last = m[1]

		// the degree of a polN or a chebN, when the match was one.
		degree := ""
		switch {
		case m[4] >= 0:
			degree = expr[m[4]:m[5]]
		case m[6] >= 0:
			degree = expr[m[6]:m[7]]
		}

		out.WriteString(expandOne(name, off, degree, &err, whole))
	}

	if err != nil {
		return expr, err
	}

	out.WriteString(expr[last:])
	return out.String(), nil
}

// expandOne returns what one shape shorthand stands for.
func expandOne(name string, off int, degree string, err *error, whole string) string {
	{

		p := func(i int) string { return "[" + strconv.Itoa(off+i) + "]" }

		switch {
		case name == "gaus":
			return fmt.Sprintf("(%s*exp(-0.5*((x-%s)/%s)^2))", p(0), p(1), p(2))
		case name == "gausn":
			return fmt.Sprintf("(%s/(sqrt(2*pi())*%s)*exp(-0.5*((x-%s)/%s)^2))", p(0), p(2), p(1), p(2))
		case name == "expo":
			return fmt.Sprintf("(exp(%s+%s*x))", p(0), p(1))
		case name == "landau":
			return fmt.Sprintf("(%s*landau(x,%s,%s))", p(0), p(1), p(2))
		case name == "landaun":
			// the normalised one: TMath::Landau already divides by the
			// scale, so the two differ only in what the height means.
			return fmt.Sprintf("(%s*landau(x,%s,%s))", p(0), p(1), p(2))
		case strings.HasPrefix(name, "pol"):
			n, _ := strconv.Atoi(degree)
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

		// Chebyshev needs ROOT's basis and the range it is defined over,
		// which a formula string does not carry. Refusing it is better than
		// guessing: a formula that quietly evaluates to the wrong number is
		// worse than one that says it cannot be evaluated.
		*err = fmt.Errorf("rhist: formula shape %q is not supported", name)
		return whole
	}
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
