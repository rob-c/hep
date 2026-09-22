// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cint

// The tree a macro is parsed into.
//
// It is a C++ tree, not a Go one: the translation to Go happens when it is
// written out, so that what the parser accepted and what the emitter made of
// it stay separable.

// ctype is a C++ type as it was written.
type ctype struct {
	name  string  // "int", "TH1F", "std::vector"
	args  []ctype // template arguments
	ptr   int     // how many stars
	ref   bool
	konst bool
	auto  bool
}

// decl is a top-level declaration.
type decl interface{ isDecl() }

// funcDecl is a function definition, which for a macro is usually the one
// named after the file.
type funcDecl struct {
	ret    ctype
	name   string
	params []param
	body   *blockStmt
	line   int
}

type param struct {
	typ  ctype
	name string
	dflt expr // a default argument, if it has one
}

// varDecl is a declaration, of a global or of a local.
type varDecl struct {
	typ   ctype
	names []declarator
	line  int
}

type declarator struct {
	name string
	ptr  int  // stars on this declarator rather than on the type
	arr  expr // a fixed size, for "double x[10]"
	init expr // what it is set to
	args []expr
	// ctor records "TH1F h("h","",10,0,1)", which is a constructor call
	// written without new.
	ctor bool
}

// structDecl is a struct or class definition.
type structDecl struct {
	name    string
	fields  []varDecl
	methods []*funcDecl
	line    int
}

func (*funcDecl) isDecl()   {}
func (*varDecl) isDecl()    {}
func (*structDecl) isDecl() {}

// stmt is a statement.
type stmt interface{ isStmt() }

type blockStmt struct {
	list []stmt
}

type declStmt struct {
	decl *varDecl
}

type exprStmt struct {
	x expr
}

type ifStmt struct {
	cond expr
	then *blockStmt
	els  stmt // a block, another if, or nothing
	line int
}

type forStmt struct {
	init stmt // a declaration or an expression
	cond expr
	post expr
	body *blockStmt
	line int
}

// rangeStmt is "for (auto x : xs)".
type rangeStmt struct {
	typ  ctype
	name string
	over expr
	body *blockStmt
	line int
}

type whileStmt struct {
	cond expr
	body *blockStmt
	post bool // a do-while, whose condition is tested after the body
	line int
}

type returnStmt struct {
	x    expr
	line int
}

type branchStmt struct {
	kind string // "break" or "continue"
	line int
}

type switchStmt struct {
	tag   expr
	cases []switchCase
	line  int
}

type switchCase struct {
	// vals empty means "default".
	vals []expr
	body []stmt
}

func (*blockStmt) isStmt()  {}
func (*declStmt) isStmt()   {}
func (*exprStmt) isStmt()   {}
func (*ifStmt) isStmt()     {}
func (*forStmt) isStmt()    {}
func (*rangeStmt) isStmt()  {}
func (*whileStmt) isStmt()  {}
func (*returnStmt) isStmt() {}
func (*branchStmt) isStmt() {}
func (*switchStmt) isStmt() {}

// expr is an expression.
type expr interface{ isExpr() }

type identExpr struct {
	name string
	line int
}

type litExpr struct {
	kind kind
	text string
	line int
}

type unaryExpr struct {
	op   string
	x    expr
	line int
}

// incDecExpr is "++x", "x++" and their decrementing twins, which Go has only
// as a statement.
type incDecExpr struct {
	op     string // "++" or "--"
	x      expr
	prefix bool
	line   int
}

type binaryExpr struct {
	op   string
	x, y expr
	line int
}

type assignExpr struct {
	op   string // "=", "+=", ...
	x, y expr
	line int
}

type condExpr struct {
	cond expr
	then expr
	els  expr
	line int
}

// selExpr is "x.y" and "x->y", which mean the same thing in Go.
type selExpr struct {
	x    expr
	sel  string
	line int
}

// scopeExpr is "TMath::Abs" and its like.
type scopeExpr struct {
	scope string
	sel   string
	line  int
}

type callExpr struct {
	fun  expr
	args []expr
	line int
}

type indexExpr struct {
	x     expr
	index expr
	line  int
}

// newExpr is "new T(args)".
type newExpr struct {
	typ  ctype
	args []expr
	arr  expr // "new double[n]"
	line int
}

// castExpr is the C-style "(T)x" and "(T*)x".
type castExpr struct {
	typ  ctype
	x    expr
	line int
}

// initExpr is a braced list, "{1, 2, 3}".
type initExpr struct {
	elems []expr
	line  int
}

// streamExpr is "cout << a << b << endl", collected into what it prints.
type streamExpr struct {
	dst   string // "cout" or "cerr"
	parts []expr
	endl  bool
	line  int
}

func (*identExpr) isExpr()  {}
func (*litExpr) isExpr()    {}
func (*unaryExpr) isExpr()  {}
func (*incDecExpr) isExpr() {}
func (*binaryExpr) isExpr() {}
func (*assignExpr) isExpr() {}
func (*condExpr) isExpr()   {}
func (*selExpr) isExpr()    {}
func (*scopeExpr) isExpr()  {}
func (*callExpr) isExpr()   {}
func (*indexExpr) isExpr()  {}
func (*newExpr) isExpr()    {}
func (*castExpr) isExpr()   {}
func (*initExpr) isExpr()   {}
func (*streamExpr) isExpr() {}

// file is a whole macro.
type file struct {
	decls    []decl
	includes []string
}

// lineOf returns the line an expression was written on, for error messages.
func lineOf(x expr) int {
	switch x := x.(type) {
	case *identExpr:
		return x.line
	case *litExpr:
		return x.line
	case *unaryExpr:
		return x.line
	case *incDecExpr:
		return x.line
	case *binaryExpr:
		return x.line
	case *assignExpr:
		return x.line
	case *condExpr:
		return x.line
	case *selExpr:
		return x.line
	case *scopeExpr:
		return x.line
	case *callExpr:
		return x.line
	case *indexExpr:
		return x.line
	case *newExpr:
		return x.line
	case *castExpr:
		return x.line
	case *initExpr:
		return x.line
	case *streamExpr:
		return x.line
	}
	return 0
}
