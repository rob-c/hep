// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cint

// ---------------------------------------------------------------- statements

func (p *parser) parseBlock() (*blockStmt, error) {
	if _, err := p.expect("{"); err != nil {
		return nil, err
	}

	out := &blockStmt{}
	for !p.at("}") && p.peek().kind != tokEOF {
		s, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		if s != nil {
			out.list = append(out.list, s)
		}
	}

	if _, err := p.expect("}"); err != nil {
		return nil, err
	}
	return out, nil
}

// body reads either a block or the single statement that stands for one.
func (p *parser) body() (*blockStmt, error) {
	if p.at("{") {
		return p.parseBlock()
	}
	s, err := p.parseStmt()
	if err != nil {
		return nil, err
	}
	if s == nil {
		return &blockStmt{}, nil
	}
	return &blockStmt{list: []stmt{s}}, nil
}

func (p *parser) parseStmt() (stmt, error) {
	line := p.peek().line

	switch {
	case p.accept(";"):
		return nil, nil

	case p.at("{"):
		return p.parseBlock()

	case p.at("if"):
		return p.parseIf()

	case p.at("for"):
		return p.parseFor()

	case p.at("while"):
		p.next()
		if _, err := p.expect("("); err != nil {
			return nil, err
		}
		cond, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(")"); err != nil {
			return nil, err
		}
		body, err := p.body()
		if err != nil {
			return nil, err
		}
		return &whileStmt{cond: cond, body: body, line: line}, nil

	case p.at("do"):
		p.next()
		body, err := p.body()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect("while"); err != nil {
			return nil, err
		}
		if _, err := p.expect("("); err != nil {
			return nil, err
		}
		cond, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(")"); err != nil {
			return nil, err
		}
		p.accept(";")
		return &whileStmt{cond: cond, body: body, post: true, line: line}, nil

	case p.at("switch"):
		return p.parseSwitch()

	case p.at("return"):
		p.next()
		out := &returnStmt{line: line}
		if !p.at(";") {
			x, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			out.x = x
		}
		if _, err := p.expect(";"); err != nil {
			return nil, err
		}
		return out, nil

	case p.at("break"), p.at("continue"):
		kind := p.next().text
		if _, err := p.expect(";"); err != nil {
			return nil, err
		}
		return &branchStmt{kind: kind, line: line}, nil
	}

	if p.startsDecl() {
		vd, err := p.parseLocalDecl()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(";"); err != nil {
			return nil, err
		}
		return &declStmt{decl: vd}, nil
	}

	x, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(";"); err != nil {
		return nil, err
	}
	return &exprStmt{x: x}, nil
}

func (p *parser) parseLocalDecl() (*varDecl, error) {
	line := p.peek().line
	typ, err := p.parseType()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokIdent {
		return nil, p.errf("expected a name after the type %q, got %v", typ.name, p.peek())
	}
	name := p.next().text
	return p.parseVarRest(typ, name, line)
}

func (p *parser) parseIf() (stmt, error) {
	line := p.peek().line
	p.next()

	if _, err := p.expect("("); err != nil {
		return nil, err
	}
	cond, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(")"); err != nil {
		return nil, err
	}

	then, err := p.body()
	if err != nil {
		return nil, err
	}

	out := &ifStmt{cond: cond, then: then, line: line}
	if p.accept("else") {
		switch {
		case p.at("if"):
			els, err := p.parseIf()
			if err != nil {
				return nil, err
			}
			out.els = els
		default:
			els, err := p.body()
			if err != nil {
				return nil, err
			}
			out.els = els
		}
	}
	return out, nil
}

func (p *parser) parseFor() (stmt, error) {
	line := p.peek().line
	p.next()

	if _, err := p.expect("("); err != nil {
		return nil, err
	}

	// the range form, "for (auto x : xs)".
	if p.startsDecl() && p.rangeAhead() {
		typ, err := p.parseType()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tokIdent {
			return nil, p.errf("expected a name, got %v", p.peek())
		}
		name := p.next().text
		if _, err := p.expect(":"); err != nil {
			return nil, err
		}
		over, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(")"); err != nil {
			return nil, err
		}
		body, err := p.body()
		if err != nil {
			return nil, err
		}
		return &rangeStmt{typ: typ, name: name, over: over, body: body, line: line}, nil
	}

	out := &forStmt{line: line}

	switch {
	case p.accept(";"):
	case p.startsDecl():
		vd, err := p.parseLocalDecl()
		if err != nil {
			return nil, err
		}
		out.init = &declStmt{decl: vd}
		if _, err := p.expect(";"); err != nil {
			return nil, err
		}
	default:
		x, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		out.init = &exprStmt{x: x}
		if _, err := p.expect(";"); err != nil {
			return nil, err
		}
	}

	if !p.at(";") {
		cond, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		out.cond = cond
	}
	if _, err := p.expect(";"); err != nil {
		return nil, err
	}

	if !p.at(")") {
		post, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		out.post = post
	}
	if _, err := p.expect(")"); err != nil {
		return nil, err
	}

	body, err := p.body()
	if err != nil {
		return nil, err
	}
	out.body = body
	return out, nil
}

// rangeAhead reports whether the for-clause is the range form, by looking
// for the colon that separates the name from what it runs over.
func (p *parser) rangeAhead() bool {
	depth := 0
	for i := 0; ; i++ {
		t := p.ahead(i)
		switch {
		case t.kind == tokEOF:
			return false
		case t.is("("), t.is("["):
			depth++
		case t.is(")"), t.is("]"):
			if depth == 0 {
				return false
			}
			depth--
		case t.is(";"):
			if depth == 0 {
				return false
			}
		case t.is("::"):
			// a scope, not the separator.
		case t.is(":"):
			if depth == 0 {
				return true
			}
		}
	}
}

func (p *parser) parseSwitch() (stmt, error) {
	line := p.peek().line
	p.next()

	if _, err := p.expect("("); err != nil {
		return nil, err
	}
	tag, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(")"); err != nil {
		return nil, err
	}
	if _, err := p.expect("{"); err != nil {
		return nil, err
	}

	out := &switchStmt{tag: tag, line: line}
	var cur *switchCase

	for !p.at("}") && p.peek().kind != tokEOF {
		switch {
		case p.at("case"):
			p.next()
			v, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(":"); err != nil {
				return nil, err
			}
			// several labels in a row share one body.
			if cur != nil && len(cur.body) == 0 {
				cur.vals = append(cur.vals, v)
				continue
			}
			out.cases = append(out.cases, switchCase{vals: []expr{v}})
			cur = &out.cases[len(out.cases)-1]

		case p.at("default"):
			p.next()
			if _, err := p.expect(":"); err != nil {
				return nil, err
			}
			out.cases = append(out.cases, switchCase{})
			cur = &out.cases[len(out.cases)-1]

		default:
			if cur == nil {
				return nil, p.errf("a statement before the first case of a switch")
			}
			s, err := p.parseStmt()
			if err != nil {
				return nil, err
			}
			if s != nil {
				cur.body = append(cur.body, s)
			}
		}
	}

	if _, err := p.expect("}"); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------------------------------------------------------------- expressions

// binary operators from the loosest binding to the tightest.
var precedence = [][]string{
	{"||"},
	{"&&"},
	{"|"},
	{"^"},
	{"&"},
	{"==", "!="},
	{"<", "<=", ">", ">="},
	{"<<", ">>"},
	{"+", "-"},
	{"*", "/", "%"},
}

var assignOps = map[string]bool{
	"=": true, "+=": true, "-=": true, "*=": true, "/=": true, "%=": true,
	"&=": true, "|=": true, "^=": true, "<<=": true, ">>=": true,
}

func (p *parser) parseExpr() (expr, error) {
	x, err := p.parseAssign()
	if err != nil {
		return nil, err
	}
	// the comma operator, which in a macro means a for-clause doing two
	// things at once.
	for p.at(",") {
		line := p.peek().line
		p.next()
		y, err := p.parseAssign()
		if err != nil {
			return nil, err
		}
		x = &binaryExpr{op: ",", x: x, y: y, line: line}
	}
	return x, nil
}

func (p *parser) parseAssign() (expr, error) {
	x, err := p.parseCond()
	if err != nil {
		return nil, err
	}

	t := p.peek()
	if t.kind == tokPunct && assignOps[t.text] {
		p.next()
		y, err := p.parseAssign()
		if err != nil {
			return nil, err
		}
		return &assignExpr{op: t.text, x: x, y: y, line: t.line}, nil
	}
	return x, nil
}

func (p *parser) parseCond() (expr, error) {
	x, err := p.parseBinary(0)
	if err != nil {
		return nil, err
	}

	if !p.at("?") {
		return x, nil
	}
	line := p.next().line

	then, err := p.parseAssign()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(":"); err != nil {
		return nil, err
	}
	els, err := p.parseCond()
	if err != nil {
		return nil, err
	}
	return &condExpr{cond: x, then: then, els: els, line: line}, nil
}

func (p *parser) parseBinary(level int) (expr, error) {
	if level >= len(precedence) {
		return p.parseUnary()
	}

	x, err := p.parseBinary(level + 1)
	if err != nil {
		return nil, err
	}

	for {
		t := p.peek()
		if t.kind != tokPunct {
			return x, nil
		}
		found := false
		for _, op := range precedence[level] {
			if t.text == op {
				found = true
				break
			}
		}
		if !found {
			return x, nil
		}
		p.next()

		y, err := p.parseBinary(level + 1)
		if err != nil {
			return nil, err
		}

		// "cout << a << b" is printing, not a shift.
		if t.text == "<<" {
			if s := asStream(x); s != nil {
				s.add(y)
				x = s
				continue
			}
		}
		x = &binaryExpr{op: t.text, x: x, y: y, line: t.line}
	}
}

// asStream returns the output stream an expression is, if it is one.
func asStream(x expr) *streamExpr {
	switch x := x.(type) {
	case *streamExpr:
		return x
	case *identExpr:
		switch x.name {
		case "cout", "cerr", "clog":
			return &streamExpr{dst: x.name, line: x.line}
		}
	case *scopeExpr:
		if x.scope == "std" {
			switch x.sel {
			case "cout", "cerr", "clog":
				return &streamExpr{dst: x.sel, line: x.line}
			}
		}
	}
	return nil
}

func (s *streamExpr) add(x expr) {
	// "endl" ends the line rather than printing anything.
	switch x := x.(type) {
	case *identExpr:
		if x.name == "endl" {
			s.endl = true
			return
		}
	case *scopeExpr:
		if x.scope == "std" && x.sel == "endl" {
			s.endl = true
			return
		}
	}
	s.parts = append(s.parts, x)
}

func (p *parser) parseUnary() (expr, error) {
	t := p.peek()

	switch {
	case t.is("new"):
		return p.parseNew()

	case t.is("sizeof"):
		p.next()
		p.skipParens()
		return &litExpr{kind: tokInt, text: "8", line: t.line}, nil

	case t.is("++"), t.is("--"):
		p.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &incDecExpr{op: t.text, x: x, prefix: true, line: t.line}, nil

	case t.is("!"), t.is("-"), t.is("+"), t.is("~"), t.is("*"), t.is("&"):
		p.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &unaryExpr{op: t.text, x: x, line: t.line}, nil

	case t.is("("):
		// a C-style cast if what is in the parentheses is a type.
		if typ, ok := p.castAhead(); ok {
			p.next() // (
			if _, err := p.parseType(); err != nil {
				return nil, err
			}
			if _, err := p.expect(")"); err != nil {
				return nil, err
			}
			x, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			return &castExpr{typ: typ, x: x, line: t.line}, nil
		}
	}

	return p.parsePostfix()
}

// castAhead reports whether the parentheses at the cursor hold a type, and
// returns it without moving.
func (p *parser) castAhead() (ctype, bool) {
	if !p.at("(") {
		return ctype{}, false
	}
	nt := p.ahead(1)
	if nt.kind != tokIdent || !p.types[nt.text] {
		return ctype{}, false
	}

	save := p.pos
	defer func() { p.pos = save }()

	p.next() // (
	typ, err := p.parseType()
	if err != nil || !p.at(")") {
		return ctype{}, false
	}
	// "(x)" where x happens to be a known type name is still a cast only
	// if something follows it to be cast.
	after := p.ahead(1)
	if after.is(")") || after.is(";") || after.is(",") ||
		(after.kind == tokPunct && assignOps[after.text]) {
		return ctype{}, false
	}
	return typ, true
}

func (p *parser) parseNew() (expr, error) {
	line := p.next().line // new

	typ, err := p.parseType()
	if err != nil {
		return nil, err
	}

	out := &newExpr{typ: typ, line: line}
	switch {
	case p.at("["):
		p.next()
		n, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect("]"); err != nil {
			return nil, err
		}
		out.arr = n
	case p.at("("):
		args, err := p.parseArgs()
		if err != nil {
			return nil, err
		}
		out.args = args
	}
	return out, nil
}

func (p *parser) parseArgs() ([]expr, error) {
	if _, err := p.expect("("); err != nil {
		return nil, err
	}

	var args []expr
	for !p.at(")") && p.peek().kind != tokEOF {
		a, err := p.parseInit()
		if err != nil {
			return nil, err
		}
		args = append(args, a)
		if !p.accept(",") {
			break
		}
	}

	if _, err := p.expect(")"); err != nil {
		return nil, err
	}
	return args, nil
}

func (p *parser) parsePostfix() (expr, error) {
	x, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}

	for {
		t := p.peek()
		switch {
		case t.is("."), t.is("->"):
			p.next()
			if p.peek().kind != tokIdent {
				return nil, p.errf("expected a member name, got %v", p.peek())
			}
			x = &selExpr{x: x, sel: p.next().text, line: t.line}

		case t.is("("):
			args, err := p.parseArgs()
			if err != nil {
				return nil, err
			}
			x = &callExpr{fun: x, args: args, line: t.line}

		case t.is("["):
			p.next()
			i, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect("]"); err != nil {
				return nil, err
			}
			x = &indexExpr{x: x, index: i, line: t.line}

		case t.is("++"), t.is("--"):
			p.next()
			x = &incDecExpr{op: t.text, x: x, line: t.line}

		default:
			return x, nil
		}
	}
}

func (p *parser) parsePrimary() (expr, error) {
	t := p.peek()

	switch t.kind {
	case tokInt, tokFloat, tokString, tokChar:
		p.next()
		return &litExpr{kind: t.kind, text: t.text, line: t.line}, nil

	case tokIdent:
		p.next()
		if p.at("::") {
			p.next()
			if p.peek().kind != tokIdent {
				return nil, p.errf("expected a name after ::, got %v", p.peek())
			}
			return &scopeExpr{scope: t.text, sel: p.next().text, line: t.line}, nil
		}
		return &identExpr{name: t.text, line: t.line}, nil
	}

	if t.is("(") {
		p.next()
		x, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(")"); err != nil {
			return nil, err
		}
		return x, nil
	}

	if t.is("{") {
		return p.parseInit()
	}

	return nil, p.errf("expected an expression, got %v", t)
}
