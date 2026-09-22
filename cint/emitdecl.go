// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cint

import (
	"fmt"
	"strings"
)

func (e *emitter) emitDecl(d decl) error {
	switch d := d.(type) {
	case *funcDecl:
		return e.emitFunc(d)
	case *structDecl:
		return e.emitStruct(d)
	case *varDecl:
		return e.emitVarDecl(d, declGlobal)
	}
	return fmt.Errorf("cint: cannot write out a %T", d)
}

func (e *emitter) emitStruct(d *structDecl) error {
	e.linef("type %s struct {", d.name)
	e.depth++
	for _, f := range d.fields {
		gotype, err := e.goType(f.typ, f.line)
		if err != nil {
			return err
		}
		for _, name := range f.names {
			typ := gotype
			if name.arr != nil {
				n, _, err := e.expr(name.arr)
				if err != nil {
					return err
				}
				typ = "[" + n + "]" + gotype
			}
			e.linef("%s %s", exported(name.name), typ)
		}
	}
	e.depth--
	e.linef("}")

	if len(d.methods) > 0 {
		e.note("the methods of %s were dropped: a macro's own class methods are not translated yet", d.name)
	}
	return nil
}

// exported makes a C++ member name one Go will export, since a translated
// struct is meant to be used from the Go around it.
func exported(name string) string {
	if name == "" {
		return "X"
	}
	if c := name[0]; c >= 'a' && c <= 'z' {
		return strings.ToUpper(name[:1]) + name[1:]
	}
	return name
}

func (e *emitter) emitFunc(d *funcDecl) error {
	e.push()
	defer e.pop()

	var params []string
	for _, p := range d.params {
		gotype, err := e.goType(p.typ, d.line)
		if err != nil {
			return err
		}
		if gotype == "" {
			continue
		}
		name := p.name
		if name == "" {
			name = "_"
		}
		params = append(params, name+" "+gotype)
		e.define(name, symbol{gotype: gotype, class: classOf(p.typ)})
		if p.dflt != nil {
			e.note("the default value of %q in %s was dropped: Go has no default arguments, so every caller has to pass it", p.name, d.name)
		}
	}

	ret, err := e.goType(d.ret, d.line)
	if err != nil {
		return err
	}
	if ret != "" {
		ret = " " + ret
	}

	e.linef("func %s(%s)%s {", d.name, strings.Join(params, ", "), ret)
	e.depth++
	if err := e.emitStmts(d.body.list); err != nil {
		return err
	}
	e.depth--
	e.linef("}")
	return nil
}

// declMode says where a declaration is being written, which decides how Go
// will let it be spelled.
type declMode int

const (
	declGlobal declMode = iota // at the top level: var
	declLocal                  // inside a function: var, which keeps the type plain
	declInline                 // in the clause of a for, where only := will do
)

// emitVarDecl writes a declaration.
func (e *emitter) emitVarDecl(d *varDecl, mode declMode) error {
	gotype, err := e.goType(d.typ, d.line)
	if err != nil {
		return err
	}
	class := classOf(d.typ)

	for _, name := range d.names {
		switch {
		case name.ctor:
			// "TH1F h("h", "", 10, 0, 1)" builds it in place.
			rt, ok := rootTypes[d.typ.name]
			if !ok || rt.ctor == nil {
				return e.errf(d.line, "%q cannot be built: nothing is known about how to make one", d.typ.name)
			}
			call, err := e.callMapping(rt.ctor, "", name.args, d.typ.name, d.line)
			if err != nil {
				return err
			}
			e.define(name.name, symbol{gotype: rt.gotype, class: class})
			e.assign(mode, name.name, rt.gotype, call, false)

		case name.arr != nil:
			n, _, err := e.expr(name.arr)
			if err != nil {
				return err
			}
			typ := "[" + n + "]" + gotype
			if name.init != nil {
				init, err := e.initList(name.init, gotype)
				if err != nil {
					return err
				}
				e.define(name.name, symbol{gotype: typ})
				e.assign(mode, name.name, typ, typ+init, false)
				continue
			}
			e.define(name.name, symbol{gotype: typ})
			e.linef("var %s %s", name.name, typ)

		case name.init == nil:
			if gotype == "" {
				return e.errf(d.line, "%q is declared void, which holds nothing", name.name)
			}
			e.define(name.name, symbol{gotype: gotype, class: class})
			e.linef("var %s %s", name.name, gotype)

		default:
			init, itype, err := e.exprAs(name.init, gotype)
			if err != nil {
				return err
			}

			typ := gotype
			cls := class
			if d.typ.auto {
				// "auto h = new TH1F(...)" takes its type from what
				// it is set to.
				typ = itype.gotype
				cls = itype.class
			}
			if cls == "" {
				cls = itype.class
			}

			e.define(name.name, symbol{gotype: typ, class: cls})
			if d.typ.auto {
				e.assign(mode, name.name, "", init, false)
				continue
			}
			// a constant already of the right type needs no conversion
			// when the for-clause drops the type: "for i := 0" is int
			// whether it is written out or not.
			needsConv := isUntyped(name.init) && itype.gotype != typ
			e.assign(mode, name.name, typ, init, needsConv)
		}
	}
	return nil
}

// assign writes a binding.
//
// The clause of a for will take nothing but ":=", which loses the type the
// declaration named, so where that matters the value is converted instead:
// "for i := 0" and "for x := float64(0)" both say what C++ said.
func (e *emitter) assign(mode declMode, name, gotype, init string, untyped bool) {
	if mode == declInline {
		if gotype != "" && untyped && isNumeric(gotype) {
			e.linef("%s := %s(%s)", name, gotype, init)
			return
		}
		e.linef("%s := %s", name, init)
		return
	}

	if gotype == "" {
		if mode == declGlobal {
			e.linef("var %s = %s", name, init)
			return
		}
		e.linef("%s := %s", name, init)
		return
	}
	e.linef("var %s %s = %s", name, gotype, init)
}

// initList writes a braced initializer as a Go composite literal.
func (e *emitter) initList(x expr, elem string) (string, error) {
	lst, ok := x.(*initExpr)
	if !ok {
		s, _, err := e.expr(x)
		return s, err
	}

	var parts []string
	for _, el := range lst.elems {
		s, _, err := e.expr(el)
		if err != nil {
			return "", err
		}
		parts = append(parts, s)
	}
	return "{" + strings.Join(parts, ", ") + "}", nil
}
