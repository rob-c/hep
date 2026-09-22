// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rhist

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"go-hep.org/x/hep/internal/hepmath"
)

// A ROOT formula is a C++ expression ROOT hands to its interpreter. groot has
// no interpreter, so what follows is a parser and evaluator for the subset
// that formulas are actually written in: arithmetic over the variables x, y,
// z and t, the parameters [0], [1], ... and the usual library of maths
// functions, plus the shorthands ROOT predefines for the shapes people fit.
//
// Anything outside that subset is refused by name rather than guessed at. A
// formula that silently evaluates to the wrong number is worse than one that
// says it cannot be evaluated.

// fnode is a node of a parsed formula.
type fnode interface {
	eval(env *fenv) float64
}

// fenv holds what a formula is evaluated against.
type fenv struct {
	vars   []float64 // x, y, z, t
	params []float64
}

func (env *fenv) variable(i int) float64 {
	if i < 0 || i >= len(env.vars) {
		return 0
	}
	return env.vars[i]
}

func (env *fenv) param(i int) float64 {
	if i < 0 || i >= len(env.params) {
		return 0
	}
	return env.params[i]
}

type fconst struct{ v float64 }

func (n fconst) eval(*fenv) float64 { return n.v }

type fvar struct{ i int }

func (n fvar) eval(env *fenv) float64 { return env.variable(n.i) }

type fparam struct{ i int }

func (n fparam) eval(env *fenv) float64 { return env.param(n.i) }

type funary struct {
	op string
	x  fnode
}

func (n funary) eval(env *fenv) float64 {
	v := n.x.eval(env)
	switch n.op {
	case "-":
		return -v
	case "+":
		return v
	case "!":
		return b2f(v == 0)
	}
	panic(fmt.Errorf("rhist: unknown unary operator %q", n.op))
}

type fbinary struct {
	op   string
	x, y fnode
}

func (n fbinary) eval(env *fenv) float64 {
	// && and || short-circuit, like the C++ they are transcribed from.
	switch n.op {
	case "&&":
		if n.x.eval(env) == 0 {
			return 0
		}
		return b2f(n.y.eval(env) != 0)
	case "||":
		if n.x.eval(env) != 0 {
			return 1
		}
		return b2f(n.y.eval(env) != 0)
	}

	x := n.x.eval(env)
	y := n.y.eval(env)
	switch n.op {
	case "+":
		return x + y
	case "-":
		return x - y
	case "*":
		return x * y
	case "/":
		return x / y
	case "%":
		return math.Mod(x, y)
	case "^":
		return math.Pow(x, y)
	case "<":
		return b2f(x < y)
	case "<=":
		return b2f(x <= y)
	case ">":
		return b2f(x > y)
	case ">=":
		return b2f(x >= y)
	case "==":
		return b2f(x == y)
	case "!=":
		return b2f(x != y)
	}
	panic(fmt.Errorf("rhist: unknown binary operator %q", n.op))
}

type fcond struct{ cond, yes, no fnode }

func (n fcond) eval(env *fenv) float64 {
	if n.cond.eval(env) != 0 {
		return n.yes.eval(env)
	}
	return n.no.eval(env)
}

type fcall struct {
	fct  func(args []float64) float64
	args []fnode
}

func (n fcall) eval(env *fenv) float64 {
	args := make([]float64, len(n.args))
	for i, a := range n.args {
		args[i] = a.eval(env)
	}
	return n.fct(args)
}

func b2f(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

// ffuncs are the functions a formula may call, by the name it calls them.
// ROOT spells most of them twice over, bare and under TMath::, and the
// parser strips that prefix before looking a name up here.
var ffuncs = map[string]struct {
	arity int // -1: any
	fct   func(args []float64) float64
}{
	"abs":     {1, func(a []float64) float64 { return math.Abs(a[0]) }},
	"fabs":    {1, func(a []float64) float64 { return math.Abs(a[0]) }},
	"sqrt":    {1, func(a []float64) float64 { return math.Sqrt(a[0]) }},
	"exp":     {1, func(a []float64) float64 { return math.Exp(a[0]) }},
	"log":     {1, func(a []float64) float64 { return math.Log(a[0]) }},
	"ln":      {1, func(a []float64) float64 { return math.Log(a[0]) }},
	"log10":   {1, func(a []float64) float64 { return math.Log10(a[0]) }},
	"log2":    {1, func(a []float64) float64 { return math.Log2(a[0]) }},
	"sin":     {1, func(a []float64) float64 { return math.Sin(a[0]) }},
	"cos":     {1, func(a []float64) float64 { return math.Cos(a[0]) }},
	"tan":     {1, func(a []float64) float64 { return math.Tan(a[0]) }},
	"asin":    {1, func(a []float64) float64 { return math.Asin(a[0]) }},
	"acos":    {1, func(a []float64) float64 { return math.Acos(a[0]) }},
	"atan":    {1, func(a []float64) float64 { return math.Atan(a[0]) }},
	"sinh":    {1, func(a []float64) float64 { return math.Sinh(a[0]) }},
	"cosh":    {1, func(a []float64) float64 { return math.Cosh(a[0]) }},
	"tanh":    {1, func(a []float64) float64 { return math.Tanh(a[0]) }},
	"asinh":   {1, func(a []float64) float64 { return math.Asinh(a[0]) }},
	"acosh":   {1, func(a []float64) float64 { return math.Acosh(a[0]) }},
	"atanh":   {1, func(a []float64) float64 { return math.Atanh(a[0]) }},
	"floor":   {1, func(a []float64) float64 { return math.Floor(a[0]) }},
	"ceil":    {1, func(a []float64) float64 { return math.Ceil(a[0]) }},
	"int":     {1, func(a []float64) float64 { return math.Trunc(a[0]) }},
	"nint":    {1, func(a []float64) float64 { return math.RoundToEven(a[0]) }},
	"erf":     {1, func(a []float64) float64 { return math.Erf(a[0]) }},
	"erfc":    {1, func(a []float64) float64 { return math.Erfc(a[0]) }},
	"gamma":   {1, func(a []float64) float64 { return math.Gamma(a[0]) }},
	"lngamma": {1, func(a []float64) float64 { v, _ := math.Lgamma(a[0]); return v }},
	"sign":    {1, func(a []float64) float64 { return sign(a[0]) }},

	// TMath::Landau, which ROOT spells with one argument or with three:
	// the value, and optionally where the peak sits and how wide it is.
	"landau": {-1, landauOf},
	"even":   {1, func(a []float64) float64 { return b2f(math.Mod(a[0], 2) == 0) }},
	"odd":    {1, func(a []float64) float64 { return b2f(math.Mod(a[0], 2) != 0) }},

	"pow":   {2, func(a []float64) float64 { return math.Pow(a[0], a[1]) }},
	"atan2": {2, func(a []float64) float64 { return math.Atan2(a[0], a[1]) }},
	"fmod":  {2, func(a []float64) float64 { return math.Mod(a[0], a[1]) }},
	"hypot": {2, func(a []float64) float64 { return math.Hypot(a[0], a[1]) }},

	"min": {-1, func(a []float64) float64 { return reduce(a, math.Min) }},
	"max": {-1, func(a []float64) float64 { return reduce(a, math.Max) }},

	"pi":       {0, func([]float64) float64 { return math.Pi }},
	"e":        {0, func([]float64) float64 { return math.E }},
	"twopi":    {0, func([]float64) float64 { return 2 * math.Pi }},
	"piover2":  {0, func([]float64) float64 { return math.Pi / 2 }},
	"piover4":  {0, func([]float64) float64 { return math.Pi / 4 }},
	"degtorad": {0, func([]float64) float64 { return math.Pi / 180 }},
	"radtodeg": {0, func([]float64) float64 { return 180 / math.Pi }},
	"infinity": {0, func([]float64) float64 { return math.Inf(+1) }},
	"qnan":     {0, func([]float64) float64 { return math.NaN() }},
}

// landauOf evaluates the Landau density, taking either the value alone or
// the value with a location and a scale, as TMath::Landau does.
func landauOf(a []float64) float64 {
	switch len(a) {
	case 1:
		return hepmath.Landau(a[0])
	case 3:
		s := a[2]
		if s <= 0 {
			return 0
		}
		return hepmath.Landau((a[0]-a[1])/s) / s
	}
	return math.NaN()
}

func sign(v float64) float64 {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return +1
	}
	return 0
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

// fvars are the names a formula calls its variables, and the slot each one
// reads. ROOT also spells them x[0], x[1], ..., which the lexer folds to
// these same names.
var fvars = map[string]int{"x": 0, "y": 1, "z": 2, "t": 3}

// ftoken is a lexical token of a formula.
type ftoken struct {
	kind ftokenKind
	str  string
	val  float64
	pos  int
}

type ftokenKind int

const (
	ftokEOF ftokenKind = iota
	ftokNumber
	ftokIdent
	ftokParam // [0] or [name]
	ftokOp
	ftokLParen
	ftokRParen
	ftokComma
)

// flex turns a formula into tokens.
type flex struct {
	src  string
	pos  int
	toks []ftoken
}

func (lx *flex) errorf(format string, args ...any) error {
	return fmt.Errorf("rhist: %s (in %q)", fmt.Sprintf(format, args...), lx.src)
}

// operators, longest first so that "<=" wins over "<".
var fops = []string{"&&", "||", "==", "!=", "<=", ">=", "**", "+", "-", "*", "/", "%", "^", "<", ">", "!", "?", ":"}

func (lx *flex) lex() error {
	for lx.pos < len(lx.src) {
		c := lx.src[lx.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			lx.pos++

		case c == '(':
			lx.emit(ftoken{kind: ftokLParen, str: "(", pos: lx.pos})
			lx.pos++

		case c == ')':
			lx.emit(ftoken{kind: ftokRParen, str: ")", pos: lx.pos})
			lx.pos++

		case c == ',':
			lx.emit(ftoken{kind: ftokComma, str: ",", pos: lx.pos})
			lx.pos++

		case c == '[':
			end := strings.IndexByte(lx.src[lx.pos:], ']')
			if end < 0 {
				return lx.errorf("unclosed '[' at %d", lx.pos)
			}
			name := lx.src[lx.pos+1 : lx.pos+end]
			lx.emit(ftoken{kind: ftokParam, str: name, pos: lx.pos})
			lx.pos += end + 1

		case c >= '0' && c <= '9', c == '.':
			if err := lx.lexNumber(); err != nil {
				return err
			}

		case isIdentStart(rune(c)):
			lx.lexIdent()

		default:
			if op := lx.lexOp(); op != "" {
				continue
			}
			return lx.errorf("unexpected character %q at %d", string(c), lx.pos)
		}
	}
	lx.emit(ftoken{kind: ftokEOF, pos: lx.pos})
	return nil
}

func (lx *flex) emit(tok ftoken) { lx.toks = append(lx.toks, tok) }

func (lx *flex) lexOp() string {
	for _, op := range fops {
		if strings.HasPrefix(lx.src[lx.pos:], op) {
			// ROOT writes the power operator both ways.
			str := op
			if str == "**" {
				str = "^"
			}
			lx.emit(ftoken{kind: ftokOp, str: str, pos: lx.pos})
			lx.pos += len(op)
			return op
		}
	}
	return ""
}

func (lx *flex) lexNumber() error {
	beg := lx.pos
	for lx.pos < len(lx.src) {
		c := lx.src[lx.pos]
		switch {
		case c >= '0' && c <= '9', c == '.':
			lx.pos++
		case c == 'e' || c == 'E':
			// an exponent, but only when a sign or digit follows: otherwise
			// this 'e' starts the next token.
			if lx.pos+1 < len(lx.src) {
				n := lx.src[lx.pos+1]
				if n == '+' || n == '-' || (n >= '0' && n <= '9') {
					lx.pos += 2
					continue
				}
			}
			goto done
		default:
			goto done
		}
	}
done:
	str := lx.src[beg:lx.pos]
	v, err := strconv.ParseFloat(str, 64)
	if err != nil {
		return lx.errorf("could not parse number %q at %d", str, beg)
	}
	lx.emit(ftoken{kind: ftokNumber, str: str, val: v, pos: beg})
	return nil
}

func (lx *flex) lexIdent() {
	beg := lx.pos
	for lx.pos < len(lx.src) {
		c := rune(lx.src[lx.pos])
		if isIdentStart(c) || unicode.IsDigit(c) {
			lx.pos++
			continue
		}
		// C++ namespace qualification, as in TMath::Sqrt.
		if c == ':' && strings.HasPrefix(lx.src[lx.pos:], "::") {
			lx.pos += 2
			continue
		}
		break
	}
	lx.emit(ftoken{kind: ftokIdent, str: lx.src[beg:lx.pos], pos: beg})
}

func isIdentStart(c rune) bool {
	return c == '_' || unicode.IsLetter(c)
}

// fparser turns tokens into a tree.
type fparser struct {
	lx     *flex
	toks   []ftoken
	pos    int
	params func(name string) (int, error)
	npar   int // highest parameter index seen, plus one
}

func (p *fparser) peek() ftoken { return p.toks[p.pos] }
func (p *fparser) next() ftoken { t := p.toks[p.pos]; p.pos++; return t }
func (p *fparser) errorf(format string, args ...any) error {
	return p.lx.errorf(format, args...)
}

// binary operator precedence, loosest first.
var fprec = map[string]int{
	"||": 1,
	"&&": 2,
	"==": 3, "!=": 3,
	"<": 4, "<=": 4, ">": 4, ">=": 4,
	"+": 5, "-": 5,
	"*": 6, "/": 6, "%": 6,
	"^": 8,
}

func (p *fparser) parse() (fnode, error) {
	n, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if tok := p.peek(); tok.kind != ftokEOF {
		return nil, p.errorf("unexpected %q at %d", tok.str, tok.pos)
	}
	return n, nil
}

func (p *fparser) parseExpr(min int) (fnode, error) {
	lhs, err := p.parseUnary()
	if err != nil {
		return nil, err
	}

	for {
		tok := p.peek()

		// the ternary conditional, looser than anything else.
		if tok.kind == ftokOp && tok.str == "?" && min == 0 {
			p.next()
			yes, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			if t := p.next(); t.kind != ftokOp || t.str != ":" {
				return nil, p.errorf("expected ':' in conditional at %d", t.pos)
			}
			no, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			lhs = fcond{cond: lhs, yes: yes, no: no}
			continue
		}

		if tok.kind != ftokOp {
			return lhs, nil
		}
		prec, ok := fprec[tok.str]
		if !ok || prec < min {
			return lhs, nil
		}
		p.next()

		// '^' associates to the right: 2^3^2 is 2^(3^2).
		nextMin := prec + 1
		if tok.str == "^" {
			nextMin = prec
		}

		rhs, err := p.parseExpr(nextMin)
		if err != nil {
			return nil, err
		}
		lhs = fbinary{op: tok.str, x: lhs, y: rhs}
	}
}

func (p *fparser) parseUnary() (fnode, error) {
	tok := p.peek()
	if tok.kind == ftokOp && (tok.str == "-" || tok.str == "+" || tok.str == "!") {
		p.next()
		// bind looser than '^', so -x^2 is -(x^2).
		x, err := p.parseExpr(fprec["^"])
		if err != nil {
			return nil, err
		}
		return funary{op: tok.str, x: x}, nil
	}
	return p.parsePrimary()
}

func (p *fparser) parsePrimary() (fnode, error) {
	tok := p.next()
	switch tok.kind {
	case ftokNumber:
		return fconst{v: tok.val}, nil

	case ftokParam:
		i, err := p.params(tok.str)
		if err != nil {
			return nil, err
		}
		if i+1 > p.npar {
			p.npar = i + 1
		}
		return fparam{i: i}, nil

	case ftokLParen:
		n, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		if t := p.next(); t.kind != ftokRParen {
			return nil, p.errorf("expected ')' at %d", t.pos)
		}
		return n, nil

	case ftokIdent:
		return p.parseIdent(tok)
	}

	return nil, p.errorf("unexpected %q at %d", tok.str, tok.pos)
}

func (p *fparser) parseIdent(tok ftoken) (fnode, error) {
	name := strings.ToLower(strings.TrimPrefix(tok.str, "TMath::"))
	name = strings.ToLower(strings.TrimPrefix(name, "tmath::"))

	// a variable: x, y, z, t — and x[0], x[1], ... for the same slots.
	if i, ok := fvars[name]; ok {
		if p.peek().kind == ftokParam {
			idx := p.next()
			j, err := strconv.Atoi(idx.str)
			if err != nil {
				return nil, p.errorf("bad variable index %q at %d", idx.str, idx.pos)
			}
			if name != "x" {
				return nil, p.errorf("only x may be indexed, got %q at %d", tok.str, tok.pos)
			}
			if j < 0 || j >= len(fvars) {
				return nil, p.errorf("variable index %d out of range at %d", j, idx.pos)
			}
			return fvar{i: j}, nil
		}
		return fvar{i: i}, nil
	}

	fct, ok := ffuncs[name]
	if !ok {
		return nil, p.errorf("unknown name %q at %d", tok.str, tok.pos)
	}

	// a constant such as pi may be written with or without its parentheses.
	if p.peek().kind != ftokLParen {
		if fct.arity != 0 {
			return nil, p.errorf("%q takes arguments at %d", tok.str, tok.pos)
		}
		return fcall{fct: fct.fct}, nil
	}
	p.next() // consume '('

	var args []fnode
	if p.peek().kind != ftokRParen {
		for {
			arg, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
			if p.peek().kind != ftokComma {
				break
			}
			p.next()
		}
	}
	if t := p.next(); t.kind != ftokRParen {
		return nil, p.errorf("expected ')' closing %q at %d", tok.str, t.pos)
	}

	if fct.arity >= 0 && len(args) != fct.arity {
		return nil, p.errorf("%q takes %d argument(s), got %d at %d", tok.str, fct.arity, len(args), tok.pos)
	}

	return fcall{fct: fct.fct, args: args}, nil
}
