// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package rexpr evaluates small arithmetic expressions over named values.
//
// It is the language a cut or a column definition is written in: arithmetic
// over names, the usual comparisons, && and ||, and a library of maths
// functions that answer to their ROOT names as well as their Go ones.
//
// Parsing is go/parser's. Such an expression is arithmetic, and Go and C++
// spell arithmetic the same way, so the only translation needed is "::" to
// "." for the namespace ROOT's maths library sits in, and the "$" that ends
// ROOT's handful of special names, which Go will not have in an identifier.
//
// # Collections
//
// A name may stand for a single number or for a collection of them. An
// expression over collections is evaluated once per element, which is what
// ROOT's Draw does with an array or a vector branch:
//
//	"pt > 20"          one value per element of pt
//	"pt[0] > 20"       one value, from the leading element
//	"Sum$(pt) > 200"   one value, from the whole collection
//
// Every collection an expression iterates over must have the same number of
// elements for a given entry, since they are read element by element and
// there is no one right answer when they do not line up. Names under Sum$
// and its neighbours are exempt: those reduce a collection to a number and
// so do not take part in the loop around them.
//
// # ROOT's special names
//
//	Length$(x)       how many elements x has
//	Sum$(x)          the sum over x
//	Min$(x), Max$(x) the extremes of x
//	MinIf$(x, c)     the extremes of the elements where c holds
//	MaxIf$(x, c)
//	Alt$(x, y)       x where it has an element, y elsewhere
//	Entry$           the entry number
//	Entries$         how many entries there are
//	Iteration$       which element of the collection is being looked at
package rexpr // import "go-hep.org/x/hep/internal/rexpr"

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"strconv"
	"strings"
)

// Value is what a name stands for while an expression is evaluated: a
// single number, or a collection the expression is evaluated over one
// element at a time.
type Value struct {
	vs     []float64
	scalar bool
}

// Num returns a Value holding one number.
func Num(v float64) Value { return Value{vs: []float64{v}, scalar: true} }

// Slice returns a Value holding a collection of numbers, which an
// expression naming it is evaluated over element by element.
func Slice(vs []float64) Value { return Value{vs: vs} }

// Len returns how many elements the value has. A single number has one.
func (v Value) Len() int { return len(v.vs) }

// Scalar reports whether the value is a single number rather than a
// collection, which a collection of one element is not.
func (v Value) Scalar() bool { return v.scalar }

// at returns the i-th element, or the number itself if the value is one.
func (v Value) at(i int) (float64, bool) {
	if v.scalar {
		return v.vs[0], true
	}
	if i < 0 || i >= len(v.vs) {
		return 0, false
	}
	return v.vs[i], true
}

// Ctx is what an expression is evaluated against: the values its names
// stand for, and where in the tree the evaluation is happening.
type Ctx struct {
	Vals    map[string]Value
	Entry   int64 // the entry being read, for Entry$
	Entries int64 // how many entries there are, for Entries$
}

func (ctx *Ctx) lookup(name string) (Value, bool) {
	if ctx == nil || ctx.Vals == nil {
		return Value{}, false
	}
	v, ok := ctx.Vals[name]
	return v, ok
}

// Expr is a compiled expression.
type Expr struct {
	src    string
	node   ast.Expr
	idents []string // the branch names it reads, in the order first seen
}

// New compiles an expression over named values.
//
// The expression is parsed by go/parser, which accepts the arithmetic,
// comparisons and calls that a Draw expression is made of, and which spares
// this package a lexer of its own. What it does not accept is C++ that is not
// also Go — "&&" and "||" are the same in both, but a cast written "(int)x"
// is not, and neither is "x ? a : b".
func New(src string) (*Expr, error) {
	// C++ says TMath::Abs where Go says TMath.Abs, and the parser below
	// only knows the second. Nothing else in an expression uses "::".
	// ROOT's special names end in "$", which go/parser will not accept in
	// an identifier either, so they are spelled out for it here and spelled
	// back whenever one is shown to the caller.
	node, err := parser.ParseExpr(mangle.Replace(strings.ReplaceAll(src, "::", ".")))
	if err != nil {
		return nil, fmt.Errorf("rexpr: could not parse %q: %w", src, err)
	}

	e := &Expr{src: src, node: node}
	err = e.scan(node)
	if err != nil {
		return nil, err
	}
	return e, nil
}

// scan walks the tree, collecting the names it reads and refusing what this
// package cannot evaluate, so that a bad expression is caught once here
// rather than once per entry.
func (e *Expr) scan(node ast.Expr) error {
	switch n := node.(type) {
	case *ast.BasicLit:
		switch n.Kind {
		case token.INT, token.FLOAT:
			return nil
		}
		return fmt.Errorf("rexpr: %q is not a number", n.Value)

	case *ast.Ident:
		if _, ok := consts[n.Name]; ok {
			return nil
		}
		if _, ok := wheres[n.Name]; ok {
			return nil
		}
		e.addIdent(n.Name)
		return nil

	case *ast.IndexExpr:
		// "pt[0]" picks one element out of a collection, which makes the
		// expression around it a single value again.
		if err := e.scan(n.X); err != nil {
			return err
		}
		return e.scan(n.Index)

	case *ast.ParenExpr:
		return e.scan(n.X)

	case *ast.UnaryExpr:
		switch n.Op {
		case token.SUB, token.ADD, token.NOT:
			return e.scan(n.X)
		}
		return fmt.Errorf("rexpr: unsupported unary operator %q", n.Op)

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
		return fmt.Errorf("rexpr: unsupported operator %q", n.Op)

	case *ast.CallExpr:
		name, err := callName(n)
		if err != nil {
			return err
		}
		arity, ok := reducers[name]
		if !ok {
			fct, known := funcs[name]
			if !known {
				return fmt.Errorf("rexpr: unknown function %q", unmangle(name))
			}
			arity = fct.arity
		}
		if arity >= 0 && len(n.Args) != arity {
			return fmt.Errorf(
				"rexpr: %q takes %d argument(s), got %d",
				unmangle(name), arity, len(n.Args),
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
			return fmt.Errorf("rexpr: unsupported expression %q", exprString(n))
		}
		e.addIdent(name)
		return nil
	}

	return fmt.Errorf("rexpr: unsupported expression %q", exprString(node))
}

func (e *Expr) addIdent(name string) {
	for _, id := range e.idents {
		if id == name {
			return
		}
	}
	e.idents = append(e.idents, name)
}

// Idents returns the names the expression reads, in the order first seen.
func (e *Expr) Idents() []string { return e.idents }

// String returns the expression as it was written.
func (e *Expr) String() string { return e.src }

// Eval computes the expression over single values, looking each name up in
// vals. It is N and At for the case where nothing is a collection.
func (e *Expr) Eval(vals map[string]float64) (float64, error) {
	ctx := Ctx{Vals: make(map[string]Value, len(vals))}
	for k, v := range vals {
		ctx.Vals[k] = Num(v)
	}
	return e.At(&ctx, 0)
}

// N returns how many values the expression yields for the entry ctx holds:
// one when nothing in it is a collection, and otherwise one per element of
// the collections it iterates over.
//
// It is an error for two collections the expression iterates over to have
// different numbers of elements, since they are read element by element and
// there is no one right answer when they do not line up.
func (e *Expr) N(ctx *Ctx) (int, error) {
	n, _, err := e.Span(ctx)
	return n, err
}

// Span returns how many values the expression yields and whether that came
// from a collection it loops over or from a single value, which goes with
// however many elements it is put next to.
//
// The two differ for a collection that happens to hold one element: it is
// still a collection, and putting it beside a collection of three is the
// same mistake whatever its length.
func (e *Expr) Span(ctx *Ctx) (n int, collection bool, err error) {
	sp, err := spanOf(e.node, ctx)
	if err != nil {
		return 0, false, err
	}
	return sp.count(), sp.iter, nil
}

// At computes the expression for its i-th value.
func (e *Expr) At(ctx *Ctx, i int) (float64, error) {
	return evalNode(e.node, ctx, i)
}

// span is how many values a subtree yields, and whether that came from a
// collection it loops over or from a single value, which goes with however
// many elements the rest of the expression has.
//
// The two are kept apart on purpose: a collection that happens to hold one
// element for this entry is still a collection, and pairing it with a
// collection of three is the same mistake whether it holds one element or
// two.
type span struct {
	n    int
	iter bool
}

var one = span{n: 1}

func (s span) count() int {
	if s.iter {
		return s.n
	}
	return 1
}

// spanOf returns the span of a subtree.
//
// A name standing for a collection yields one value per element; a single
// value, an indexed element and anything Sum$ or its neighbours have
// reduced yield one that goes with any number of them.
func spanOf(node ast.Expr, ctx *Ctx) (span, error) {
	switch n := node.(type) {
	case *ast.BasicLit:
		return one, nil

	case *ast.Ident:
		if _, ok := consts[n.Name]; ok {
			return one, nil
		}
		if _, ok := wheres[n.Name]; ok {
			return one, nil
		}
		return spanOfName(ctx, n.Name)

	case *ast.SelectorExpr:
		name, _ := selectorName(n)
		return spanOfName(ctx, name)

	case *ast.IndexExpr:
		return one, nil

	case *ast.ParenExpr:
		return spanOf(n.X, ctx)

	case *ast.UnaryExpr:
		return spanOf(n.X, ctx)

	case *ast.BinaryExpr:
		x, err := spanOf(n.X, ctx)
		if err != nil {
			return span{}, err
		}
		y, err := spanOf(n.Y, ctx)
		if err != nil {
			return span{}, err
		}
		return combine(x, y, node)

	case *ast.CallExpr:
		name, err := callName(n)
		if err != nil {
			return span{}, err
		}
		if _, ok := reducers[name]; ok {
			// these turn a collection into one value, so whatever is
			// under them does not drive the loop around them.
			return one, nil
		}
		out := one
		for _, arg := range n.Args {
			k, err := spanOf(arg, ctx)
			if err != nil {
				return span{}, err
			}
			out, err = combine(out, k, node)
			if err != nil {
				return span{}, err
			}
		}
		return out, nil
	}

	return span{}, fmt.Errorf("rexpr: unsupported expression %q", unmangle(exprString(node)))
}

func spanOfName(ctx *Ctx, name string) (span, error) {
	v, ok := ctx.lookup(name)
	if !ok {
		return span{}, fmt.Errorf("rexpr: no branch named %q", name)
	}
	if v.scalar {
		return one, nil
	}
	return span{n: len(v.vs), iter: true}, nil
}

// combine folds the spans of two subtrees into the span of the whole. A
// single value goes with anything; two collections have to agree.
func combine(x, y span, node ast.Expr) (span, error) {
	switch {
	case !x.iter:
		return y, nil
	case !y.iter:
		return x, nil
	case x.n == y.n:
		return x, nil
	}
	return span{}, fmt.Errorf(
		"rexpr: %q puts a collection of %d element(s) together with one of %d",
		unmangle(exprString(node)), x.n, y.n,
	)
}

// evalAll computes every value a subtree yields, which is what the reducers
// are handed.
func evalAll(node ast.Expr, ctx *Ctx) ([]float64, error) {
	sp, err := spanOf(node, ctx)
	if err != nil {
		return nil, err
	}
	n := sp.count()
	out := make([]float64, n)
	for i := range n {
		v, err := evalNode(node, ctx, i)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func evalNode(node ast.Expr, ctx *Ctx, iter int) (float64, error) {
	switch n := node.(type) {
	case *ast.BasicLit:
		v, err := strconv.ParseFloat(n.Value, 64)
		if err != nil {
			return 0, fmt.Errorf("rexpr: could not read the number %q: %w", n.Value, err)
		}
		return v, nil

	case *ast.Ident:
		if v, ok := consts[n.Name]; ok {
			return v, nil
		}
		if where, ok := wheres[n.Name]; ok {
			return where(ctx, iter), nil
		}
		return element(ctx, n.Name, iter)

	case *ast.SelectorExpr:
		name, _ := selectorName(n)
		return element(ctx, name, iter)

	case *ast.IndexExpr:
		name, ok := nameOf(n.X)
		if !ok {
			return 0, fmt.Errorf("rexpr: %q is not a branch that can be indexed", exprString(n.X))
		}
		v, ok := ctx.lookup(name)
		if !ok {
			return 0, fmt.Errorf("rexpr: no branch named %q", name)
		}
		k, err := evalNode(n.Index, ctx, iter)
		if err != nil {
			return 0, err
		}
		if v.scalar {
			return 0, fmt.Errorf("rexpr: branch %q is a single value, not a collection to index", name)
		}
		i := int(k)
		if float64(i) != k {
			return 0, fmt.Errorf("rexpr: %q is not a whole number to index %q by", exprString(n.Index), name)
		}
		out, ok := v.at(i)
		if !ok {
			return 0, fmt.Errorf(
				"rexpr: element %d is past the %d element(s) of %q", i, len(v.vs), name,
			)
		}
		return out, nil

	case *ast.ParenExpr:
		return evalNode(n.X, ctx, iter)

	case *ast.UnaryExpr:
		v, err := evalNode(n.X, ctx, iter)
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
			x, err := evalNode(n.X, ctx, iter)
			if err != nil {
				return 0, err
			}
			if x == 0 {
				return 0, nil
			}
			y, err := evalNode(n.Y, ctx, iter)
			if err != nil {
				return 0, err
			}
			return b2f(y != 0), nil

		case token.LOR:
			x, err := evalNode(n.X, ctx, iter)
			if err != nil {
				return 0, err
			}
			if x != 0 {
				return 1, nil
			}
			y, err := evalNode(n.Y, ctx, iter)
			if err != nil {
				return 0, err
			}
			return b2f(y != 0), nil
		}

		x, err := evalNode(n.X, ctx, iter)
		if err != nil {
			return 0, err
		}
		y, err := evalNode(n.Y, ctx, iter)
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
		if _, ok := reducers[name]; ok {
			return evalReducer(name, n, ctx, iter)
		}
		args := make([]float64, len(n.Args))
		for i, arg := range n.Args {
			v, err := evalNode(arg, ctx, iter)
			if err != nil {
				return 0, err
			}
			args[i] = v
		}
		return funcs[name].fct(args), nil
	}

	return 0, fmt.Errorf("rexpr: unsupported expression %q", unmangle(exprString(node)))
}

// element returns the value a name stands for at the current element.
func element(ctx *Ctx, name string, iter int) (float64, error) {
	v, ok := ctx.lookup(name)
	if !ok {
		return 0, fmt.Errorf("rexpr: no branch named %q", name)
	}
	out, ok := v.at(iter)
	if !ok {
		return 0, fmt.Errorf(
			"rexpr: element %d is past the %d element(s) of %q", iter, len(v.vs), name,
		)
	}
	return out, nil
}

// nameOf returns the branch name a node stands for, if it is one.
func nameOf(node ast.Expr) (string, bool) {
	switch n := node.(type) {
	case *ast.Ident:
		return n.Name, true
	case *ast.SelectorExpr:
		return selectorName(n)
	case *ast.ParenExpr:
		return nameOf(n.X)
	}
	return "", false
}

// evalReducer computes the ROOT names that turn a collection into one value
// or say where in the tree the evaluation has got to.
func evalReducer(name string, n *ast.CallExpr, ctx *Ctx, iter int) (float64, error) {
	switch name {
	case "Length" + dollar:
		sp, err := spanOf(n.Args[0], ctx)
		if err != nil {
			return 0, err
		}
		return float64(sp.count()), nil

	case "Sum" + dollar:
		vs, err := evalAll(n.Args[0], ctx)
		if err != nil {
			return 0, err
		}
		var sum float64
		for _, v := range vs {
			sum += v
		}
		return sum, nil

	case "Min" + dollar, "Max" + dollar:
		vs, err := evalAll(n.Args[0], ctx)
		if err != nil {
			return 0, err
		}
		return extreme(vs, name == "Min"+dollar), nil

	case "MinIf" + dollar, "MaxIf" + dollar:
		vs, err := evalAll(n.Args[0], ctx)
		if err != nil {
			return 0, err
		}
		cond, err := evalAll(n.Args[1], ctx)
		if err != nil {
			return 0, err
		}
		var kept []float64
		for i, v := range vs {
			// a condition of one value speaks for every element.
			c := cond[0]
			if len(cond) > 1 {
				if i >= len(cond) {
					break
				}
				c = cond[i]
			}
			if c != 0 {
				kept = append(kept, v)
			}
		}
		return extreme(kept, name == "MinIf"+dollar), nil

	case "Alt" + dollar:
		// the first argument where it has an element, the second where it
		// has run out, which is how a short collection is padded.
		sp, err := spanOf(n.Args[0], ctx)
		if err != nil {
			return 0, err
		}
		if iter < sp.count() {
			return evalNode(n.Args[0], ctx, iter)
		}
		return evalNode(n.Args[1], ctx, 0)
	}

	return 0, fmt.Errorf("rexpr: unknown function %q", unmangle(name))
}

// extreme returns the smallest or largest of vs. ROOT answers zero for an
// empty collection rather than refusing, and so does this.
func extreme(vs []float64, least bool) float64 {
	if len(vs) == 0 {
		return 0
	}
	out := vs[0]
	for _, v := range vs[1:] {
		switch {
		case least && v < out:
			out = v
		case !least && v > out:
			out = v
		}
	}
	return out
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
	return "", fmt.Errorf("rexpr: unsupported call %q", exprString(n.Fun))
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

// dollar stands in for the "$" that ends ROOT's special names, which
// go/parser will not accept in an identifier. It is spelled back out
// whenever an expression or a name is shown to the caller.
const dollar = "_rexprDollar"

var dollarNames = []string{
	"Length", "Sum", "Min", "Max", "MinIf", "MaxIf", "Alt",
	"Entry", "Entries", "Iteration",
}

var mangle = func() *strings.Replacer {
	pairs := make([]string, 0, 2*len(dollarNames))
	for _, name := range dollarNames {
		pairs = append(pairs, name+"$", name+dollar)
	}
	return strings.NewReplacer(pairs...)
}()

// unmangle puts the "$" back, for error messages.
func unmangle(s string) string { return strings.ReplaceAll(s, dollar, "$") }

// reducers are the names that take a collection and give back one value, by
// arity. They are handled as they are written rather than as evaluated
// arguments, since what is under them is a whole collection.
var reducers = map[string]int{
	"Length" + dollar: 1,
	"Sum" + dollar:    1,
	"Min" + dollar:    1,
	"Max" + dollar:    1,
	"MinIf" + dollar:  2,
	"MaxIf" + dollar:  2,
	"Alt" + dollar:    2,
}

// wheres are the names that report where in the tree the evaluation is.
var wheres = map[string]func(ctx *Ctx, iter int) float64{
	"Entry" + dollar: func(ctx *Ctx, _ int) float64 {
		if ctx == nil {
			return 0
		}
		return float64(ctx.Entry)
	},
	"Entries" + dollar: func(ctx *Ctx, _ int) float64 {
		if ctx == nil {
			return 0
		}
		return float64(ctx.Entries)
	},
	"Iteration" + dollar: func(_ *Ctx, iter int) float64 { return float64(iter) },
}
