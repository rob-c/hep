// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cint

import (
	"fmt"
	"strings"
)

func (e *emitter) emitStmts(list []stmt) error {
	for _, s := range list {
		if err := e.emitStmt(s); err != nil {
			return err
		}
	}
	return nil
}

func (e *emitter) emitStmt(s stmt) error {
	switch s := s.(type) {
	case *blockStmt:
		e.push()
		defer e.pop()
		e.linef("{")
		e.depth++
		if err := e.emitStmts(s.list); err != nil {
			return err
		}
		e.depth--
		e.linef("}")
		return nil

	case *declStmt:
		return e.emitVarDecl(s.decl, declLocal)

	case *exprStmt:
		return e.emitExprStmt(s.x)

	case *ifStmt:
		return e.emitIf(s)

	case *forStmt:
		return e.emitFor(s)

	case *rangeStmt:
		return e.emitRange(s)

	case *whileStmt:
		return e.emitWhile(s)

	case *returnStmt:
		if s.x == nil {
			e.linef("return")
			return nil
		}
		x, _, err := e.expr(s.x)
		if err != nil {
			return err
		}
		e.linef("return %s", x)
		return nil

	case *branchStmt:
		e.linef("%s", s.kind)
		return nil

	case *switchStmt:
		return e.emitSwitch(s)
	}

	return fmt.Errorf("cint: cannot write out a %T", s)
}

// emitExprStmt writes an expression used for what it does rather than for
// its value. Go only allows a few of those, so the rest are given to the
// blank identifier.
func (e *emitter) emitExprStmt(x expr) error {
	switch x := x.(type) {
	case *assignExpr:
		return e.emitAssign(x)

	case *incDecExpr:
		v, _, err := e.expr(x.x)
		if err != nil {
			return err
		}
		e.linef("%s%s", v, x.op)
		return nil

	case *binaryExpr:
		if x.op == "," {
			// a for-clause doing two things at once.
			if err := e.emitExprStmt(x.x); err != nil {
				return err
			}
			return e.emitExprStmt(x.y)
		}

	case *streamExpr:
		s, _, err := e.expr(x)
		if err != nil {
			return err
		}
		e.linef("%s", s)
		return nil

	case *callExpr:
		// a container method that changes what it is called on is an
		// assignment in Go, which only a statement can hold.
		if done, err := e.emitMutator(x); done {
			return err
		}
	}

	s, _, err := e.expr(x)
	if err != nil {
		return err
	}
	if s == "" {
		// a call this translation drops, such as one that only sets a
		// colour.
		return nil
	}
	if isCall(x) {
		e.linef("%s", s)
		return nil
	}
	e.linef("_ = %s", s)
	return nil
}

func isCall(x expr) bool {
	_, ok := x.(*callExpr)
	return ok
}

func (e *emitter) emitAssign(x *assignExpr) error {
	lhs, ltype, err := e.expr(x.x)
	if err != nil {
		return err
	}
	rhs, _, err := e.exprAs(x.y, ltype.gotype)
	if err != nil {
		return err
	}
	e.linef("%s %s %s", lhs, x.op, rhs)
	return nil
}

func (e *emitter) emitIf(s *ifStmt) error {
	cond, err := e.boolExpr(s.cond)
	if err != nil {
		return err
	}

	e.push()
	e.linef("if %s {", cond)
	e.depth++
	if err := e.emitStmts(s.then.list); err != nil {
		e.pop()
		return err
	}
	e.depth--
	e.pop()

	switch els := s.els.(type) {
	case nil:
		e.linef("}")
	case *ifStmt:
		// "} else if" has to be written as one line.
		e.indent()
		e.buf.WriteString("} else ")
		saved := e.depth
		e.depth = 0
		err := e.emitIf(els)
		e.depth = saved
		return err
	default:
		e.push()
		e.linef("} else {")
		e.depth++
		// the else is already a block here, so its contents go straight
		// in rather than inside a second pair of braces.
		list := []stmt{s.els}
		if b, ok := s.els.(*blockStmt); ok {
			list = b.list
		}
		if err := e.emitStmts(list); err != nil {
			e.pop()
			return err
		}
		e.depth--
		e.pop()
		e.linef("}")
	}
	return nil
}

func (e *emitter) emitFor(s *forStmt) error {
	e.push()
	defer e.pop()

	// the clauses of a for are written on one line, so they are built up
	// separately and put together.
	var init, post string
	if s.init != nil {
		sub, err := e.sub(func() error {
			if d, ok := s.init.(*declStmt); ok {
				return e.emitVarDecl(d.decl, declInline)
			}
			return e.emitStmt(s.init)
		})
		if err != nil {
			return err
		}
		init = strings.TrimSpace(sub)
	}
	if s.post != nil {
		sub, err := e.sub(func() error { return e.emitExprStmt(s.post) })
		if err != nil {
			return err
		}
		post = strings.TrimSpace(sub)
	}

	var cond string
	if s.cond != nil {
		c, err := e.boolExpr(s.cond)
		if err != nil {
			return err
		}
		cond = c
	}

	switch {
	case init == "" && cond == "" && post == "":
		e.linef("for {")
	case init == "" && post == "":
		e.linef("for %s {", cond)
	default:
		if strings.Contains(init, "\n") || strings.Contains(post, "\n") {
			return e.errf(s.line, "the clauses of this for do more than Go can say on one line")
		}
		e.linef("for %s; %s; %s {", init, cond, post)
	}

	e.depth++
	if err := e.emitStmts(s.body.list); err != nil {
		return err
	}
	e.depth--
	e.linef("}")
	return nil
}

// sub writes something into a buffer of its own, for the places where a
// statement has to end up inside a line.
func (e *emitter) sub(fn func() error) (string, error) {
	var (
		saved = e.buf.String()
		depth = e.depth
	)
	e.buf.Reset()
	e.depth = 0

	err := fn()
	out := e.buf.String()

	e.buf.Reset()
	e.buf.WriteString(saved)
	e.depth = depth

	return out, err
}

func (e *emitter) emitRange(s *rangeStmt) error {
	e.push()
	defer e.pop()

	over, otype, err := e.expr(s.over)
	if err != nil {
		return err
	}

	elem := strings.TrimPrefix(otype.gotype, "[]")
	if elem == otype.gotype {
		elem = ""
	}
	if !s.typ.auto {
		g, err := e.goType(s.typ, s.line)
		if err != nil {
			return err
		}
		elem = g
	}
	e.define(s.name, symbol{gotype: elem})

	e.linef("for _, %s := range %s {", s.name, over)
	e.depth++
	if err := e.emitStmts(s.body.list); err != nil {
		return err
	}
	e.depth--
	e.linef("}")
	return nil
}

func (e *emitter) emitWhile(s *whileStmt) error {
	e.push()
	defer e.pop()

	cond, err := e.boolExpr(s.cond)
	if err != nil {
		return err
	}

	if s.post {
		// a do-while runs its body before it asks, which Go says with a
		// loop that breaks at the bottom.
		e.linef("for {")
		e.depth++
		if err := e.emitStmts(s.body.list); err != nil {
			return err
		}
		e.linef("if !(%s) {", cond)
		e.depth++
		e.linef("break")
		e.depth--
		e.linef("}")
		e.depth--
		e.linef("}")
		return nil
	}

	e.linef("for %s {", cond)
	e.depth++
	if err := e.emitStmts(s.body.list); err != nil {
		return err
	}
	e.depth--
	e.linef("}")
	return nil
}

func (e *emitter) emitSwitch(s *switchStmt) error {
	tag, _, err := e.expr(s.tag)
	if err != nil {
		return err
	}

	e.linef("switch %s {", tag)
	for _, c := range s.cases {
		e.push()

		switch len(c.vals) {
		case 0:
			e.linef("default:")
		default:
			var vals []string
			for _, v := range c.vals {
				sv, _, err := e.expr(v)
				if err != nil {
					e.pop()
					return err
				}
				vals = append(vals, sv)
			}
			e.linef("case %s:", strings.Join(vals, ", "))
		}

		// a Go case does not fall through, so the break that ends a C++
		// one says nothing and is dropped.
		body := c.body
		if n := len(body); n > 0 {
			if b, ok := body[n-1].(*branchStmt); ok && b.kind == "break" {
				body = body[:n-1]
			}
		}

		e.depth++
		if err := e.emitStmts(body); err != nil {
			e.pop()
			return err
		}
		e.depth--
		e.pop()
	}
	e.linef("}")
	return nil
}

// emitMutator writes the container calls that change what they are called
// on, which Go says with an assignment rather than a method.
func (e *emitter) emitMutator(x *callExpr) (bool, error) {
	fn, ok := x.fun.(*selExpr)
	if !ok {
		return false, nil
	}

	recv, sym, err := e.expr(fn.x)
	if err != nil {
		// not something this knows how to reach: let the ordinary path
		// report it.
		return false, nil
	}
	if !strings.HasPrefix(sym.gotype, "[]") {
		return false, nil
	}
	elem := strings.TrimPrefix(sym.gotype, "[]")

	switch fn.sel {
	case "push_back", "emplace_back":
		if len(x.args) != 1 {
			return true, e.errf(x.line, "%s takes one argument, got %d", fn.sel, len(x.args))
		}
		v, _, err := e.exprAs(x.args[0], elem)
		if err != nil {
			return true, err
		}
		e.linef("%s = append(%s, %s)", recv, recv, v)
		return true, nil

	case "clear", "Clear":
		e.linef("%s = %s[:0]", recv, recv)
		return true, nil

	case "resize":
		if len(x.args) != 1 {
			return true, e.errf(x.line, "resize takes one argument, got %d", len(x.args))
		}
		n, _, err := e.exprAs(x.args[0], "int")
		if err != nil {
			return true, err
		}
		e.linef("%s = make([]%s, %s)", recv, elem, n)
		return true, nil
	}
	return false, nil
}
