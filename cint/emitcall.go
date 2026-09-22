// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cint

import "strings"

// call writes a call out as Go, working out from what is being called which
// of the mappings applies.
func (e *emitter) call(x *callExpr) (string, symbol, error) {
	switch fn := x.fun.(type) {
	case *selExpr:
		return e.methodCall(fn, x)

	case *scopeExpr:
		return e.staticCall(fn, x)

	case *identExpr:
		return e.plainCall(fn, x)
	}

	return "", symbol{}, e.errf(x.line, "cannot make sense of what this call is calling")
}

// methodCall writes "x->f(...)" and "x.f(...)".
func (e *emitter) methodCall(fn *selExpr, x *callExpr) (string, symbol, error) {
	recv, sym, err := e.expr(fn.x)
	if err != nil {
		return "", symbol{}, err
	}

	if sym.class == "" {
		// a standard library container or string, which is a plain Go
		// value rather than a ROOT object.
		if out, osym, ok, err := e.builtinMethod(recv, sym, fn, x); ok {
			return out, osym, err
		}
		return "", symbol{}, e.errf(x.line,
			"%q is called on something whose type is not known, so there is no telling what it means", fn.sel)
	}

	rt, ok := rootTypes[sym.class]
	if !ok {
		return "", symbol{}, e.errf(x.line, "nothing is known about the class %q", sym.class)
	}

	m, ok := rt.methods[fn.sel]
	if !ok {
		return "", symbol{}, e.errf(x.line,
			"%s::%s is not translated: nothing in go-hep is known to answer to it", sym.class, fn.sel)
	}

	out, err := e.callMapping(m, recv, x.args, sym.class+"::"+fn.sel, x.line)
	if err != nil {
		return "", symbol{}, err
	}
	if out == "" {
		e.note("%s::%s was dropped: it changes how a plot looks, which this translation has nowhere to put",
			sym.class, fn.sel)
	}
	return out, symbol{gotype: m.gotype, class: m.result}, nil
}

// staticCall writes "T::f(...)".
func (e *emitter) staticCall(fn *scopeExpr, x *callExpr) (string, symbol, error) {
	if fn.scope == "TMath" {
		m, ok := mathFuncs[fn.sel]
		if !ok {
			return "", symbol{}, e.errf(x.line, "TMath::%s is not translated", fn.sel)
		}
		out, err := e.callMapping(m, "", x.args, "TMath::"+fn.sel, x.line)
		return out, symbol{gotype: m.gotype}, err
	}

	if rt, ok := rootTypes[fn.scope]; ok {
		if m, ok := rt.statics[fn.sel]; ok {
			out, err := e.callMapping(m, "", x.args, fn.scope+"::"+fn.sel, x.line)
			return out, symbol{gotype: m.gotype, class: m.result}, err
		}
	}

	return "", symbol{}, e.errf(x.line, "%s::%s is not translated", fn.scope, fn.sel)
}

// plainCall writes "f(...)": one of the macro's own functions, one of C's,
// or a constructor written without new.
func (e *emitter) plainCall(fn *identExpr, x *callExpr) (string, symbol, error) {
	if fd, ok := e.funcs[fn.name]; ok {
		var args []string
		for i, a := range x.args {
			want := ""
			if i < len(fd.params) {
				g, err := e.goType(fd.params[i].typ, x.line)
				if err != nil {
					return "", symbol{}, err
				}
				want = g
			}
			s, _, err := e.exprAs(a, want)
			if err != nil {
				return "", symbol{}, err
			}
			args = append(args, s)
		}
		ret, err := e.goType(fd.ret, x.line)
		if err != nil {
			return "", symbol{}, err
		}
		return fn.name + "(" + strings.Join(args, ", ") + ")", symbol{gotype: ret, class: classOf(fd.ret)}, nil
	}

	if m, ok := freeFuncs[fn.name]; ok {
		out, err := e.callMapping(m, "", x.args, fn.name, x.line)
		return out, symbol{gotype: m.gotype}, err
	}

	// "TH1F(...)" used as a conversion, or a class built without new.
	if rt, ok := rootTypes[fn.name]; ok && rt.ctor != nil {
		out, err := e.callMapping(rt.ctor, "", x.args, fn.name, x.line)
		return out, symbol{gotype: rt.gotype, class: fn.name}, err
	}

	// a plain C++ conversion, "int(x)".
	if g, ok := builtinTypes[fn.name]; ok && g != "" && len(x.args) == 1 {
		s, _, err := e.expr(x.args[0])
		if err != nil {
			return "", symbol{}, err
		}
		return g + "(" + s + ")", symbol{gotype: g}, nil
	}

	return "", symbol{}, e.errf(x.line, "%q is not a function this translation knows", fn.name)
}

// callMapping writes one mapped call, checking how many arguments it was
// given and converting each to the type the Go call wants.
func (e *emitter) callMapping(m *mapping, recv string, args []expr, what string, line int) (string, error) {
	if len(args) < m.min {
		return "", e.errf(line, "%s takes at least %d argument(s), got %d", what, m.min, len(args))
	}
	if m.max >= 0 && len(args) > m.max {
		return "", e.errf(line, "%s takes at most %d argument(s), got %d", what, m.max, len(args))
	}

	out := make([]string, 0, len(args))
	for i, a := range args {
		want := ""
		if i < len(m.argTypes) {
			want = m.argTypes[i]
		}
		s, _, err := e.exprAs(a, want)
		if err != nil {
			return "", err
		}
		out = append(out, s)
	}

	e.use(m.imports...)
	return m.emit(recv, out), nil
}

// builtinMethod writes the calls a std::vector or a std::string answers to,
// which in Go are not methods at all.
//
// The ones that change what they are called on, push_back and clear, are
// assignments in Go and so are only allowed where a statement is: those are
// written by emitExprStmt, and reaching them here means one was used for its
// value, which is said rather than guessed at.
func (e *emitter) builtinMethod(recv string, sym symbol, fn *selExpr, x *callExpr) (string, symbol, bool, error) {
	var (
		slice = strings.HasPrefix(sym.gotype, "[]")
		str   = sym.gotype == "string"
		elem  = strings.TrimPrefix(sym.gotype, "[]")
	)
	if !slice && !str {
		return "", symbol{}, false, nil
	}

	arg := func(i int, want string) (string, error) {
		s, _, err := e.exprAs(x.args[i], want)
		return s, err
	}

	switch fn.sel {
	case "size", "Length", "length":
		return "len(" + recv + ")", symbol{gotype: "int"}, true, nil

	case "empty", "IsNull":
		return "len(" + recv + ") == 0", symbol{gotype: "bool"}, true, nil

	case "at":
		if len(x.args) != 1 {
			return "", symbol{}, true, e.errf(x.line, "at takes one argument, got %d", len(x.args))
		}
		i, err := arg(0, "int")
		if err != nil {
			return "", symbol{}, true, err
		}
		if str {
			return recv + "[" + i + "]", symbol{gotype: "byte"}, true, nil
		}
		return recv + "[" + i + "]", symbol{gotype: elem}, true, nil

	case "back":
		if str {
			return recv + "[len(" + recv + ")-1]", symbol{gotype: "byte"}, true, nil
		}
		return recv + "[len(" + recv + ")-1]", symbol{gotype: elem}, true, nil

	case "front":
		return recv + "[0]", symbol{gotype: elem}, true, nil

	case "Data", "c_str":
		if str {
			return recv, sym, true, nil
		}

	case "push_back", "emplace_back", "clear", "Clear":
		return "", symbol{}, true, e.errf(x.line,
			"%q changes what it is called on, which in Go is an assignment: give it a line of its own", fn.sel)
	}

	return "", symbol{}, true, e.errf(x.line,
		"nothing is known about %q on a %s", fn.sel, sym.gotype)
}
