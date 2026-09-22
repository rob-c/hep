// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cint

import (
	"fmt"
	"strings"
)

// expr writes an expression out as Go, along with what is known about its
// type.
func (e *emitter) expr(x expr) (string, symbol, error) {
	switch x := x.(type) {
	case *litExpr:
		return e.literal(x)

	case *identExpr:
		return e.ident(x)

	case *scopeExpr:
		return e.scope(x)

	case *unaryExpr:
		return e.unary(x)

	case *incDecExpr:
		// C++ lets an increment be part of an expression and Go does
		// not, so one that is used for its value cannot be translated.
		return "", symbol{}, e.errf(x.line,
			"%q is used for its value, which Go has no way to write: give it a line of its own", x.op)

	case *binaryExpr:
		return e.binary(x)

	case *assignExpr:
		return "", symbol{}, e.errf(x.line,
			"an assignment is used for its value, which Go has no way to write: give it a line of its own")

	case *condExpr:
		return "", symbol{}, e.errf(x.line,
			"Go has no \"?:\": write it as an if, or as a small function")

	case *selExpr:
		return e.selector(x)

	case *callExpr:
		return e.call(x)

	case *indexExpr:
		return e.index(x)

	case *newExpr:
		return e.newObject(x)

	case *castExpr:
		return e.cast(x)

	case *initExpr:
		s, err := e.initList(x, "")
		return s, symbol{}, err

	case *streamExpr:
		return e.stream(x)
	}

	return "", symbol{}, fmt.Errorf("cint: cannot write out a %T", x)
}

// exprAs writes an expression and converts it to the wanted Go type, which
// is where the promotions C++ leaves unwritten get written.
func (e *emitter) exprAs(x expr, want string) (string, symbol, error) {
	s, sym, err := e.expr(x)
	if err != nil {
		return "", symbol{}, err
	}
	return e.convert(s, sym, want, x), sym, nil
}

// convert wraps an expression in a conversion where Go needs one.
//
// A literal needs none: Go's untyped constants already go wherever a number
// is wanted. It is the typed expressions, a variable of one width used where
// another is wanted, that C++ converted silently and Go will not.
func (e *emitter) convert(s string, sym symbol, want string, x expr) string {
	switch {
	case want == "" || sym.gotype == "" || sym.gotype == want:
		return s
	case !isNumeric(want) || !isNumeric(sym.gotype):
		return s
	case isUntyped(x):
		return s
	}
	return want + "(" + s + ")"
}

// isUntyped reports whether an expression is a constant Go will place
// wherever it is wanted without a conversion.
func isUntyped(x expr) bool {
	switch x := x.(type) {
	case *litExpr:
		return x.kind == tokInt || x.kind == tokFloat
	case *unaryExpr:
		switch x.op {
		case "-", "+":
			return isUntyped(x.x)
		}
	case *binaryExpr:
		return isUntyped(x.x) && isUntyped(x.y)
	}
	return false
}

func isNumeric(t string) bool {
	switch t {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "byte",
		"float32", "float64":
		return true
	}
	return false
}

// boolExpr writes a condition. C++ takes any number as a truth value and Go
// does not, so a bare number is compared against zero.
func (e *emitter) boolExpr(x expr) (string, error) {
	s, sym, err := e.expr(x)
	if err != nil {
		return "", err
	}
	if sym.gotype == "bool" || sym.gotype == "" {
		return s, nil
	}
	if isNumeric(sym.gotype) {
		return s + " != 0", nil
	}
	return s, nil
}

func (e *emitter) literal(x *litExpr) (string, symbol, error) {
	switch x.kind {
	case tokInt:
		return x.text, symbol{gotype: "int"}, nil
	case tokFloat:
		return x.text, symbol{gotype: "float64"}, nil
	case tokString:
		return x.text, symbol{gotype: "string"}, nil
	case tokChar:
		// C++ writes a character in single quotes and so does Go, but
		// Go calls what comes out a rune.
		return x.text, symbol{gotype: "byte"}, nil
	}
	return "", symbol{}, e.errf(x.line, "cannot write out the literal %q", x.text)
}

func (e *emitter) ident(x *identExpr) (string, symbol, error) {
	if s, ok := e.lookup(x.name); ok {
		return x.name, s, nil
	}
	if g, ok := globals[x.name]; ok {
		e.use(g.pkg)
		return g.expr, symbol{class: g.class}, nil
	}
	switch x.name {
	case "true", "false":
		return x.name, symbol{gotype: "bool"}, nil
	case "kTRUE":
		return "true", symbol{gotype: "bool"}, nil
	case "kFALSE":
		return "false", symbol{gotype: "bool"}, nil
	case "nullptr", "NULL":
		return "nil", symbol{}, nil
	case "gROOT", "gSystem", "gDirectory":
		return "", symbol{}, e.errf(x.line,
			"%s is the running ROOT session, which a translated macro does not have", x.name)
	}
	if _, ok := e.funcs[x.name]; ok {
		return x.name, symbol{}, nil
	}
	return "", symbol{}, e.errf(x.line, "nothing is known about the name %q", x.name)
}

func (e *emitter) scope(x *scopeExpr) (string, symbol, error) {
	// a colour or another named constant of ROOT's.
	if x.scope == "TMath" {
		if m, ok := mathFuncs[x.sel]; ok && m.max == 0 {
			e.use(m.imports...)
			return m.emit("", nil), symbol{gotype: m.gotype}, nil
		}
	}
	if x.scope == "std" {
		switch x.sel {
		case "endl":
			return `"\n"`, symbol{gotype: "string"}, nil
		}
	}
	return "", symbol{}, e.errf(x.line, "nothing is known about %s::%s", x.scope, x.sel)
}

func (e *emitter) unary(x *unaryExpr) (string, symbol, error) {
	switch x.op {
	case "*", "&":
		// C++ takes an address or follows a pointer where Go, for the
		// objects a macro deals in, already has one.
		return e.expr(x.x)
	}

	if x.op == "!" {
		s, err := e.boolExpr(x.x)
		if err != nil {
			return "", symbol{}, err
		}
		return "!(" + s + ")", symbol{gotype: "bool"}, nil
	}

	s, sym, err := e.expr(x.x)
	if err != nil {
		return "", symbol{}, err
	}
	if x.op == "~" {
		return "^" + s, sym, nil
	}
	return x.op + s, sym, nil
}

func (e *emitter) binary(x *binaryExpr) (string, symbol, error) {
	switch x.op {
	case "&&", "||":
		lhs, err := e.boolExpr(x.x)
		if err != nil {
			return "", symbol{}, err
		}
		rhs, err := e.boolExpr(x.y)
		if err != nil {
			return "", symbol{}, err
		}
		return lhs + " " + x.op + " " + rhs, symbol{gotype: "bool"}, nil

	case ",":
		return "", symbol{}, e.errf(x.line,
			"a comma expression is used for its value, which Go has no way to write")
	}

	lhs, ltype, err := e.expr(x.x)
	if err != nil {
		return "", symbol{}, err
	}
	rhs, rtype, err := e.expr(x.y)
	if err != nil {
		return "", symbol{}, err
	}

	// the two sides have to agree in Go, where in C++ the narrower one was
	// widened without being written.
	want := wider(ltype.gotype, rtype.gotype)
	lhs = e.convert(lhs, ltype, want, x.x)
	rhs = e.convert(rhs, rtype, want, x.y)

	out := lhs + " " + x.op + " " + rhs
	switch x.op {
	case "==", "!=", "<", "<=", ">", ">=":
		return out, symbol{gotype: "bool"}, nil
	}
	return out, symbol{gotype: want}, nil
}

// wider returns the type two numbers meet in.
func wider(a, b string) string {
	switch {
	case a == b:
		return a
	case a == "" || !isNumeric(a):
		return b
	case b == "" || !isNumeric(b):
		return a
	case a == "float64" || b == "float64":
		return "float64"
	case a == "float32" || b == "float32":
		return "float32"
	case a == "int64" || b == "int64":
		return "int64"
	case a == "uint64" || b == "uint64":
		return "uint64"
	case a == "int" || b == "int":
		return "int"
	}
	return a
}

func (e *emitter) selector(x *selExpr) (string, symbol, error) {
	recv, sym, err := e.expr(x.x)
	if err != nil {
		return "", symbol{}, err
	}

	// a member of a struct the macro declared.
	if e.structs[strings.TrimPrefix(sym.gotype, "*")] {
		return recv + "." + exported(x.sel), symbol{}, nil
	}

	// a method used without calling it cannot be translated on its own;
	// the call around it is what knows the mapping.
	return "", symbol{}, e.errf(x.line,
		"%q is reached for without being called, which a translation cannot make sense of", x.sel)
}

func (e *emitter) index(x *indexExpr) (string, symbol, error) {
	base, sym, err := e.expr(x.x)
	if err != nil {
		return "", symbol{}, err
	}
	idx, _, err := e.exprAs(x.index, "int")
	if err != nil {
		return "", symbol{}, err
	}

	elem := ""
	if t := sym.gotype; strings.HasPrefix(t, "[]") {
		elem = strings.TrimPrefix(t, "[]")
	} else if i := strings.IndexByte(t, ']'); strings.HasPrefix(t, "[") && i > 0 {
		elem = t[i+1:]
	}
	return base + "[" + idx + "]", symbol{gotype: elem}, nil
}

func (e *emitter) cast(x *castExpr) (string, symbol, error) {
	gotype, err := e.goType(x.typ, x.line)
	if err != nil {
		return "", symbol{}, err
	}
	s, sym, err := e.expr(x.x)
	if err != nil {
		return "", symbol{}, err
	}

	class := classOf(x.typ)
	if class != "" {
		// "(TTree*)f->Get("t")" picks a type out of what a file holds,
		// which Go says with a type assertion.
		return s + ".(" + gotype + ")", symbol{gotype: gotype, class: class}, nil
	}
	if gotype == "" || gotype == sym.gotype {
		return s, sym, nil
	}
	return gotype + "(" + s + ")", symbol{gotype: gotype}, nil
}

func (e *emitter) newObject(x *newExpr) (string, symbol, error) {
	if x.arr != nil {
		gotype, err := e.goType(x.typ, x.line)
		if err != nil {
			return "", symbol{}, err
		}
		n, _, err := e.exprAs(x.arr, "int")
		if err != nil {
			return "", symbol{}, err
		}
		return fmt.Sprintf("make([]%s, %s)", gotype, n), symbol{gotype: "[]" + gotype}, nil
	}

	rt, ok := rootTypes[x.typ.name]
	if !ok {
		if e.structs[x.typ.name] {
			return "&" + x.typ.name + "{}", symbol{gotype: "*" + x.typ.name}, nil
		}
		return "", symbol{}, e.errf(x.line, "nothing is known about how to make a %q", x.typ.name)
	}
	if rt.ctor == nil {
		return "", symbol{}, e.errf(x.line, "a %q cannot be made in a macro", x.typ.name)
	}

	s, err := e.callMapping(rt.ctor, "", x.args, x.typ.name, x.line)
	if err != nil {
		return "", symbol{}, err
	}
	return s, symbol{gotype: rt.gotype, class: x.typ.name}, nil
}

func (e *emitter) stream(x *streamExpr) (string, symbol, error) {
	e.use(pkgFmt)

	var parts []string
	for _, p := range x.parts {
		s, _, err := e.expr(p)
		if err != nil {
			return "", symbol{}, err
		}
		parts = append(parts, s)
	}

	fn := "fmt.Print"
	if x.endl {
		fn = "fmt.Println"
	}
	if x.dst == "cerr" {
		e.use(pkgOS)
		fn = "fmt.Fprint"
		if x.endl {
			fn = "fmt.Fprintln"
		}
		parts = append([]string{"os.Stderr"}, parts...)
	}
	return fn + "(" + strings.Join(parts, ", ") + ")", symbol{}, nil
}
