// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rdraw

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"strconv"
	"strings"
)

// expr is a compiled expression over the branches of a tree.
type expr struct {
	src    string
	node   ast.Expr
	idents []string // the branch names it reads, in the order first seen
}

// newExpr compiles an expression over branch names.
//
// The expression is parsed by go/parser, which accepts the arithmetic,
// comparisons and calls that a Draw expression is made of, and which spares
// this package a lexer of its own. What it does not accept is C++ that is not
// also Go — "&&" and "||" are the same in both, but a cast written "(int)x"
// is not, and neither is "x ? a : b".
func newExpr(src string) (*expr, error) {
	// C++ says TMath::Abs where Go says TMath.Abs, and the parser below
	// only knows the second. Nothing else in an expression uses "::".
	node, err := parser.ParseExpr(strings.ReplaceAll(src, "::", "."))
	if err != nil {
		return nil, fmt.Errorf("rdraw: could not parse %q: %w", src, err)
	}

	e := &expr{src: src, node: node}
	err = e.scan(node)
	if err != nil {
		return nil, err
	}
	return e, nil
}

// scan walks the tree, collecting the names it reads and refusing what this
// package cannot evaluate, so that a bad expression is caught once here
// rather than once per entry.
func (e *expr) scan(node ast.Expr) error {
	switch n := node.(type) {
	case *ast.BasicLit:
		switch n.Kind {
		case token.INT, token.FLOAT:
			return nil
		}
		return fmt.Errorf("rdraw: %q is not a number", n.Value)

	case *ast.Ident:
		if _, ok := consts[n.Name]; ok {
			return nil
		}
		e.addIdent(n.Name)
		return nil

	case *ast.ParenExpr:
		return e.scan(n.X)

	case *ast.UnaryExpr:
		switch n.Op {
		case token.SUB, token.ADD, token.NOT:
			return e.scan(n.X)
		}
		return fmt.Errorf("rdraw: unsupported unary operator %q", n.Op)

	case *ast.BinaryExpr:
		switch n.Op {
		case token.ADD, token.SUB, token.MUL, token.QUO, token.REM,
			token.LSS, token.LEQ, token.GTR, token.GEQ, token.EQL, token.NEQ,
			token.LAND, token.LOR:
			if err := e.scan(n.X); err != nil {
				return err
			}
			return e.scan(n.Y)
		}
		return fmt.Errorf("rdraw: unsupported operator %q", n.Op)

	case *ast.CallExpr:
		name, err := callName(n)
		if err != nil {
			return err
		}
		fct, ok := funcs[name]
		if !ok {
			return fmt.Errorf("rdraw: unknown function %q", name)
		}
		if fct.arity >= 0 && len(n.Args) != fct.arity {
			return fmt.Errorf(
				"rdraw: %q takes %d argument(s), got %d",
				name, fct.arity, len(n.Args),
			)
		}
		for _, arg := range n.Args {
			if err := e.scan(arg); err != nil {
				return err
			}
		}
		return nil

	case *ast.SelectorExpr:
		// a name with a dot in it is a branch such as "mu.pt", not a method
		// call: the whole thing names one leaf.
		name, ok := selectorName(n)
		if !ok {
			return fmt.Errorf("rdraw: unsupported expression %q", exprString(n))
		}
		e.addIdent(name)
		return nil
	}

	return fmt.Errorf("rdraw: unsupported expression %q", exprString(node))
}

func (e *expr) addIdent(name string) {
	for _, id := range e.idents {
		if id == name {
			return
		}
	}
	e.idents = append(e.idents, name)
}

// eval computes the expression, reading each branch through vals.
func (e *expr) eval(vals map[string]float64) (float64, error) {
	return evalNode(e.node, vals)
}

func evalNode(node ast.Expr, vals map[string]float64) (float64, error) {
	switch n := node.(type) {
	case *ast.BasicLit:
		v, err := strconv.ParseFloat(n.Value, 64)
		if err != nil {
			return 0, fmt.Errorf("rdraw: could not read the number %q: %w", n.Value, err)
		}
		return v, nil

	case *ast.Ident:
		if v, ok := consts[n.Name]; ok {
			return v, nil
		}
		v, ok := vals[n.Name]
		if !ok {
			return 0, fmt.Errorf("rdraw: no branch named %q", n.Name)
		}
		return v, nil

	case *ast.SelectorExpr:
		name, _ := selectorName(n)
		v, ok := vals[name]
		if !ok {
			return 0, fmt.Errorf("rdraw: no branch named %q", name)
		}
		return v, nil

	case *ast.ParenExpr:
		return evalNode(n.X, vals)

	case *ast.UnaryExpr:
		v, err := evalNode(n.X, vals)
		if err != nil {
			return 0, err
		}
		switch n.Op {
		case token.SUB:
			return -v, nil
		case token.ADD:
			return v, nil
		case token.NOT:
			return b2f(v == 0), nil
		}

	case *ast.BinaryExpr:
		// && and || short-circuit, as they do in the C++ this imitates.
		switch n.Op {
		case token.LAND:
			x, err := evalNode(n.X, vals)
			if err != nil {
				return 0, err
			}
			if x == 0 {
				return 0, nil
			}
			y, err := evalNode(n.Y, vals)
			if err != nil {
				return 0, err
			}
			return b2f(y != 0), nil

		case token.LOR:
			x, err := evalNode(n.X, vals)
			if err != nil {
				return 0, err
			}
			if x != 0 {
				return 1, nil
			}
			y, err := evalNode(n.Y, vals)
			if err != nil {
				return 0, err
			}
			return b2f(y != 0), nil
		}

		x, err := evalNode(n.X, vals)
		if err != nil {
			return 0, err
		}
		y, err := evalNode(n.Y, vals)
		if err != nil {
			return 0, err
		}

		switch n.Op {
		case token.ADD:
			return x + y, nil
		case token.SUB:
			return x - y, nil
		case token.MUL:
			return x * y, nil
		case token.QUO:
			return x / y, nil
		case token.REM:
			return math.Mod(x, y), nil
		case token.LSS:
			return b2f(x < y), nil
		case token.LEQ:
			return b2f(x <= y), nil
		case token.GTR:
			return b2f(x > y), nil
		case token.GEQ:
			return b2f(x >= y), nil
		case token.EQL:
			return b2f(x == y), nil
		case token.NEQ:
			return b2f(x != y), nil
		}

	case *ast.CallExpr:
		name, err := callName(n)
		if err != nil {
			return 0, err
		}
		args := make([]float64, len(n.Args))
		for i, arg := range n.Args {
			v, err := evalNode(arg, vals)
			if err != nil {
				return 0, err
			}
			args[i] = v
		}
		return funcs[name].fct(args), nil
	}

	return 0, fmt.Errorf("rdraw: unsupported expression %q", exprString(node))
}

// callName returns the name a call expression calls, with any "TMath::"
// stripped off it: ROOT spells its maths library both ways and so may a
// Draw expression.
func callName(n *ast.CallExpr) (string, error) {
	switch fun := n.Fun.(type) {
	case *ast.Ident:
		return fun.Name, nil
	case *ast.SelectorExpr:
		if pkg, ok := fun.X.(*ast.Ident); ok {
			switch pkg.Name {
			case "TMath", "math":
				return fun.Sel.Name, nil
			}
		}
	}
	return "", fmt.Errorf("rdraw: unsupported call %q", exprString(n.Fun))
}

// selectorName flattens a dotted name such as "mu.pt" into one string, and
// says whether it was a plain dotted name at all.
func selectorName(n *ast.SelectorExpr) (string, bool) {
	switch x := n.X.(type) {
	case *ast.Ident:
		return x.Name + "." + n.Sel.Name, true
	case *ast.SelectorExpr:
		name, ok := selectorName(x)
		if !ok {
			return "", false
		}
		return name + "." + n.Sel.Name, true
	}
	return "", false
}

func exprString(node ast.Expr) string {
	switch n := node.(type) {
	case *ast.Ident:
		return n.Name
	case *ast.BasicLit:
		return n.Value
	}
	return fmt.Sprintf("%T", node)
}

func b2f(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

// consts are the names an expression may use without them being branches.
var consts = map[string]float64{
	"pi":    math.Pi,
	"Pi":    math.Pi,
	"e":     math.E,
	"twopi": 2 * math.Pi,
}

// funcs are the functions an expression may call.
var funcs = map[string]struct {
	arity int // -1: any
	fct   func(args []float64) float64
}{
	"abs":   {1, func(a []float64) float64 { return math.Abs(a[0]) }},
	"Abs":   {1, func(a []float64) float64 { return math.Abs(a[0]) }},
	"sqrt":  {1, func(a []float64) float64 { return math.Sqrt(a[0]) }},
	"Sqrt":  {1, func(a []float64) float64 { return math.Sqrt(a[0]) }},
	"exp":   {1, func(a []float64) float64 { return math.Exp(a[0]) }},
	"Exp":   {1, func(a []float64) float64 { return math.Exp(a[0]) }},
	"log":   {1, func(a []float64) float64 { return math.Log(a[0]) }},
	"Log":   {1, func(a []float64) float64 { return math.Log(a[0]) }},
	"log10": {1, func(a []float64) float64 { return math.Log10(a[0]) }},
	"Log10": {1, func(a []float64) float64 { return math.Log10(a[0]) }},
	"sin":   {1, func(a []float64) float64 { return math.Sin(a[0]) }},
	"Sin":   {1, func(a []float64) float64 { return math.Sin(a[0]) }},
	"cos":   {1, func(a []float64) float64 { return math.Cos(a[0]) }},
	"Cos":   {1, func(a []float64) float64 { return math.Cos(a[0]) }},
	"tan":   {1, func(a []float64) float64 { return math.Tan(a[0]) }},
	"Tan":   {1, func(a []float64) float64 { return math.Tan(a[0]) }},
	"asin":  {1, func(a []float64) float64 { return math.Asin(a[0]) }},
	"acos":  {1, func(a []float64) float64 { return math.Acos(a[0]) }},
	"atan":  {1, func(a []float64) float64 { return math.Atan(a[0]) }},
	"sinh":  {1, func(a []float64) float64 { return math.Sinh(a[0]) }},
	"cosh":  {1, func(a []float64) float64 { return math.Cosh(a[0]) }},
	"tanh":  {1, func(a []float64) float64 { return math.Tanh(a[0]) }},
	"floor": {1, func(a []float64) float64 { return math.Floor(a[0]) }},
	"ceil":  {1, func(a []float64) float64 { return math.Ceil(a[0]) }},
	"int":   {1, func(a []float64) float64 { return math.Trunc(a[0]) }},

	"pow":   {2, func(a []float64) float64 { return math.Pow(a[0], a[1]) }},
	"Power": {2, func(a []float64) float64 { return math.Pow(a[0], a[1]) }},
	"atan2": {2, func(a []float64) float64 { return math.Atan2(a[0], a[1]) }},
	"ATan2": {2, func(a []float64) float64 { return math.Atan2(a[0], a[1]) }},
	"hypot": {2, func(a []float64) float64 { return math.Hypot(a[0], a[1]) }},
	"fmod":  {2, func(a []float64) float64 { return math.Mod(a[0], a[1]) }},

	"min": {-1, func(a []float64) float64 { return reduce(a, math.Min) }},
	"Min": {-1, func(a []float64) float64 { return reduce(a, math.Min) }},
	"max": {-1, func(a []float64) float64 { return reduce(a, math.Max) }},
	"Max": {-1, func(a []float64) float64 { return reduce(a, math.Max) }},
}

func reduce(vs []float64, fct func(a, b float64) float64) float64 {
	if len(vs) == 0 {
		return math.NaN()
	}
	o := vs[0]
	for _, v := range vs[1:] {
		o = fct(o, v)
	}
	return o
}
