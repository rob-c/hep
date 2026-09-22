// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cint

import (
	"fmt"
	"strings"
)

// parser reads the tokens of a macro into a tree.
type parser struct {
	toks []token
	pos  int

	// types is every name that starts a declaration: the built-in types,
	// the ROOT classes this package knows, and whatever the macro itself
	// declared. C++ cannot tell "A * b;" from a declaration without it.
	types map[string]bool

	includes []string
}

func parse(src string) (*file, error) {
	lx := newLexer(src)
	toks, err := lx.lex()
	if err != nil {
		return nil, err
	}

	p := &parser{toks: toks, types: knownTypes(), includes: lx.includes}
	return p.parseFile()
}

// knownTypes returns the names that begin a declaration before the macro has
// said anything.
func knownTypes() map[string]bool {
	out := make(map[string]bool, len(builtinTypes)+len(rootTypes))
	for name := range builtinTypes {
		out[name] = true
	}
	for name := range rootTypes {
		out[name] = true
	}
	for _, name := range []string{
		"const", "static", "unsigned", "signed", "long", "short", "auto",
		"struct", "class", "std",
		// the standard library containers, which a macro writes either
		// with std:: in front or without.
		"vector", "array", "map", "set", "pair", "list", "deque", "string",
	} {
		out[name] = true
	}
	return out
}

func (p *parser) peek() token      { return p.toks[p.pos] }
func (p *parser) at(s string) bool { return p.peek().is(s) }

func (p *parser) ahead(n int) token {
	if p.pos+n >= len(p.toks) {
		return p.toks[len(p.toks)-1]
	}
	return p.toks[p.pos+n]
}

func (p *parser) next() token {
	t := p.toks[p.pos]
	if t.kind != tokEOF {
		p.pos++
	}
	return t
}

func (p *parser) accept(s string) bool {
	if p.at(s) {
		p.next()
		return true
	}
	return false
}

func (p *parser) expect(s string) (token, error) {
	if !p.at(s) {
		return token{}, p.errf("expected %q, got %v", s, p.peek())
	}
	return p.next(), nil
}

func (p *parser) errf(format string, args ...any) error {
	return fmt.Errorf("cint: line %d: %s", p.peek().line, fmt.Sprintf(format, args...))
}

// ---------------------------------------------------------------- file

func (p *parser) parseFile() (*file, error) {
	f := &file{includes: p.includes}

	for p.peek().kind != tokEOF {
		// a stray semicolon between declarations is legal and means
		// nothing.
		if p.accept(";") {
			continue
		}

		// "using namespace std;" and its friends say where names come
		// from, which a Go translation works out for itself.
		if p.at("using") {
			for !p.at(";") && p.peek().kind != tokEOF {
				p.next()
			}
			p.accept(";")
			continue
		}
		if p.at("typedef") {
			// remember the new name so that it can start a declaration,
			// and otherwise leave the alias alone.
			var last token
			for !p.at(";") && p.peek().kind != tokEOF {
				last = p.next()
			}
			p.accept(";")
			if last.kind == tokIdent {
				p.types[last.text] = true
			}
			continue
		}
		if p.at("R__LOAD_LIBRARY") {
			p.next()
			p.skipParens()
			p.accept(";")
			continue
		}

		d, err := p.parseDecl()
		if err != nil {
			return nil, err
		}
		if d != nil {
			f.decls = append(f.decls, d)
		}
	}

	return f, nil
}

// skipParens steps over a balanced pair of parentheses.
func (p *parser) skipParens() {
	if !p.at("(") {
		return
	}
	depth := 0
	for p.peek().kind != tokEOF {
		switch {
		case p.at("("):
			depth++
		case p.at(")"):
			depth--
		}
		p.next()
		if depth == 0 {
			return
		}
	}
}

func (p *parser) parseDecl() (decl, error) {
	if p.at("struct") || p.at("class") {
		return p.parseStruct()
	}

	line := p.peek().line
	typ, err := p.parseType()
	if err != nil {
		return nil, err
	}

	if p.peek().kind != tokIdent {
		return nil, p.errf("expected a name after the type %q, got %v", typ.name, p.peek())
	}
	name := p.next().text

	// a function if what follows is a parameter list.
	if p.at("(") {
		return p.parseFunc(typ, name, line)
	}

	vd, err := p.parseVarRest(typ, name, line)
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(";"); err != nil {
		return nil, err
	}
	return vd, nil
}

func (p *parser) parseStruct() (decl, error) {
	line := p.peek().line
	p.next() // struct or class

	if p.peek().kind != tokIdent {
		return nil, p.errf("expected a name after struct, got %v", p.peek())
	}
	sd := &structDecl{name: p.next().text, line: line}
	p.types[sd.name] = true

	// a base class list, which a Go translation has no place to put.
	if p.accept(":") {
		for !p.at("{") && p.peek().kind != tokEOF {
			p.next()
		}
	}

	if _, err := p.expect("{"); err != nil {
		return nil, err
	}

	for !p.at("}") && p.peek().kind != tokEOF {
		if p.accept(";") {
			continue
		}
		// access labels say nothing a Go struct can hold.
		if p.at("public") || p.at("private") || p.at("protected") {
			p.next()
			p.accept(":")
			continue
		}

		fline := p.peek().line
		typ, err := p.parseType()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tokIdent {
			return nil, p.errf("expected a member name, got %v", p.peek())
		}
		name := p.next().text

		if p.at("(") {
			m, err := p.parseFunc(typ, name, fline)
			if err != nil {
				return nil, err
			}
			sd.methods = append(sd.methods, m.(*funcDecl))
			continue
		}

		vd, err := p.parseVarRest(typ, name, fline)
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(";"); err != nil {
			return nil, err
		}
		sd.fields = append(sd.fields, *vd)
	}

	if _, err := p.expect("}"); err != nil {
		return nil, err
	}
	p.accept(";")
	return sd, nil
}

func (p *parser) parseFunc(ret ctype, name string, line int) (decl, error) {
	fd := &funcDecl{ret: ret, name: name, line: line}

	if _, err := p.expect("("); err != nil {
		return nil, err
	}
	for !p.at(")") {
		if p.at("void") && p.ahead(1).is(")") {
			p.next()
			break
		}

		typ, err := p.parseType()
		if err != nil {
			return nil, err
		}
		var pname string
		if p.peek().kind == tokIdent {
			pname = p.next().text
		}
		// an array parameter, "double x[]".
		for p.accept("[") {
			for !p.at("]") && p.peek().kind != tokEOF {
				p.next()
			}
			if _, err := p.expect("]"); err != nil {
				return nil, err
			}
			typ.ptr++
		}

		prm := param{typ: typ, name: pname}
		if p.accept("=") {
			d, err := p.parseAssign()
			if err != nil {
				return nil, err
			}
			prm.dflt = d
		}
		fd.params = append(fd.params, prm)

		if !p.accept(",") {
			break
		}
	}
	if _, err := p.expect(")"); err != nil {
		return nil, err
	}

	// "const" and the like after the parameter list.
	for p.peek().kind == tokIdent && !p.at("{") {
		p.next()
	}

	// a declaration without a body says the function exists somewhere
	// else, which a macro has no way to provide.
	if p.accept(";") {
		return nil, fmt.Errorf(
			"cint: line %d: %q is declared but not defined, and a translation has nothing to call",
			line, name,
		)
	}

	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	fd.body = body
	return fd, nil
}

// parseVarRest reads the declarators after the type and the first name.
func (p *parser) parseVarRest(typ ctype, name string, line int) (*varDecl, error) {
	vd := &varDecl{typ: typ, line: line}

	for {
		d := declarator{name: name}

		// "double x[10]"
		if p.accept("[") {
			if !p.at("]") {
				n, err := p.parseAssign()
				if err != nil {
					return nil, err
				}
				d.arr = n
			}
			if _, err := p.expect("]"); err != nil {
				return nil, err
			}
		}

		switch {
		case p.accept("="):
			init, err := p.parseInit()
			if err != nil {
				return nil, err
			}
			d.init = init

		case p.at("("):
			// "TH1F h("h", "", 10, 0, 1)" builds it in place.
			args, err := p.parseArgs()
			if err != nil {
				return nil, err
			}
			d.args = args
			d.ctor = true

		case p.at("{"):
			init, err := p.parseInit()
			if err != nil {
				return nil, err
			}
			d.init = init
		}

		vd.names = append(vd.names, d)

		if !p.accept(",") {
			return vd, nil
		}

		// another declarator, which may have stars of its own.
		stars := 0
		for p.accept("*") {
			stars++
		}
		if p.peek().kind != tokIdent {
			return nil, p.errf("expected another name, got %v", p.peek())
		}
		name = p.next().text
		_ = stars
	}
}

func (p *parser) parseInit() (expr, error) {
	if p.at("{") {
		line := p.peek().line
		p.next()
		out := &initExpr{line: line}
		for !p.at("}") && p.peek().kind != tokEOF {
			e, err := p.parseInit()
			if err != nil {
				return nil, err
			}
			out.elems = append(out.elems, e)
			if !p.accept(",") {
				break
			}
		}
		if _, err := p.expect("}"); err != nil {
			return nil, err
		}
		return out, nil
	}
	return p.parseAssign()
}

// ---------------------------------------------------------------- types

var builtinTypes = map[string]string{
	"void":      "",
	"bool":      "bool",
	"Bool_t":    "bool",
	"char":      "byte",
	"Char_t":    "byte",
	"short":     "int16",
	"Short_t":   "int16",
	"int":       "int",
	"Int_t":     "int",
	"long":      "int64",
	"Long_t":    "int64",
	"Long64_t":  "int64",
	"float":     "float64",
	"Float_t":   "float64",
	"double":    "float64",
	"Double_t":  "float64",
	"size_t":    "int",
	"UInt_t":    "uint32",
	"ULong64_t": "uint64",
	"TString":   "string",
	"string":    "string",
}

// parseType reads a type as C++ writes it.
func (p *parser) parseType() (ctype, error) {
	var (
		typ   ctype
		parts []string
	)

	for {
		switch {
		case p.at("const"):
			p.next()
			typ.konst = true
			continue
		case p.at("static") || p.at("inline") || p.at("virtual") || p.at("extern"):
			p.next()
			continue
		case p.at("unsigned") || p.at("signed") || p.at("long") || p.at("short"):
			parts = append(parts, p.next().text)
			continue
		case p.at("struct") || p.at("class"):
			p.next()
			continue
		}
		break
	}

	if p.peek().kind == tokIdent {
		name := p.next().text
		// "std::vector", "ROOT::RDataFrame"
		for p.at("::") {
			p.next()
			if p.peek().kind != tokIdent {
				return typ, p.errf("expected a name after ::, got %v", p.peek())
			}
			if name == "std" {
				name = p.next().text
			} else {
				name += "::" + p.next().text
			}
		}
		parts = append(parts, name)
	}

	if len(parts) == 0 {
		return typ, p.errf("expected a type, got %v", p.peek())
	}
	typ.name = strings.Join(parts, " ")
	typ.auto = typ.name == "auto"

	// template arguments.
	if p.at("<") {
		p.next()
		for !p.at(">") && p.peek().kind != tokEOF {
			arg, err := p.parseType()
			if err != nil {
				return typ, err
			}
			typ.args = append(typ.args, arg)
			if !p.accept(",") {
				break
			}
		}
		if _, err := p.expect(">"); err != nil {
			return typ, err
		}
	}

	for {
		switch {
		case p.accept("*"):
			typ.ptr++
		case p.accept("&"):
			typ.ref = true
		case p.at("const"):
			p.next()
			typ.konst = true
		default:
			return typ, nil
		}
	}
}

// startsDecl reports whether the statement at the cursor is a declaration
// rather than an expression, which is the one thing C++ cannot be read
// without knowing.
//
// The test is to read a type and see whether a name and a declarator follow
// it. "TH1F *h = ..." does and is a declaration; "a * b;" would too, which is
// why the first token has to be a name already known to be a type.
func (p *parser) startsDecl() bool {
	t := p.peek()
	if t.kind != tokIdent || !p.types[t.text] {
		return false
	}

	save := p.pos
	defer func() { p.pos = save }()

	if _, err := p.parseType(); err != nil {
		return false
	}
	if p.peek().kind != tokIdent {
		return false
	}
	p.next()

	switch {
	case p.at(";"), p.at("="), p.at("("), p.at("["), p.at(","), p.at("{"):
		return true
	case p.at(":"):
		// "for (auto x : xs)", where the name is followed by what it
		// runs over rather than by what it is set to.
		return true
	}
	return false
}
