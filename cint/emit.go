// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cint

import (
	"fmt"
	"sort"
	"strings"
)

// emitter writes a parsed macro out as Go.
type emitter struct {
	buf strings.Builder

	imports map[string]bool
	scopes  []map[string]symbol
	notes   []string
	depth   int

	// funcs are the macro's own functions, so that a call to one is not
	// mistaken for a ROOT one.
	funcs map[string]*funcDecl
	// structs are the types the macro declared.
	structs map[string]bool

	// file is what the macro is called, which names its entry point.
	file string
}

// symbol is what is known about a name in scope.
type symbol struct {
	gotype string // the Go type, for the arithmetic around it
	class  string // the ROOT class, for the calls on it
}

func newEmitter() *emitter {
	return &emitter{
		imports: make(map[string]bool),
		scopes:  []map[string]symbol{{}},
		funcs:   make(map[string]*funcDecl),
		structs: make(map[string]bool),
	}
}

func (e *emitter) errf(line int, format string, args ...any) error {
	return fmt.Errorf("cint: line %d: %s", line, fmt.Sprintf(format, args...))
}

func (e *emitter) note(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	for _, n := range e.notes {
		if n == msg {
			return
		}
	}
	e.notes = append(e.notes, msg)
}

func (e *emitter) use(paths ...string) {
	for _, p := range paths {
		if p != "" {
			e.imports[p] = true
		}
	}
}

// ---------------------------------------------------------------- scopes

func (e *emitter) push() { e.scopes = append(e.scopes, map[string]symbol{}) }
func (e *emitter) pop()  { e.scopes = e.scopes[:len(e.scopes)-1] }

func (e *emitter) define(name string, s symbol) {
	e.scopes[len(e.scopes)-1][name] = s
}

func (e *emitter) lookup(name string) (symbol, bool) {
	for i := len(e.scopes) - 1; i >= 0; i-- {
		if s, ok := e.scopes[i][name]; ok {
			return s, true
		}
	}
	return symbol{}, false
}

// ---------------------------------------------------------------- writing

func (e *emitter) indent() {
	e.buf.WriteString(strings.Repeat("\t", e.depth))
}

func (e *emitter) linef(format string, args ...any) {
	e.indent()
	fmt.Fprintf(&e.buf, format, args...)
	e.buf.WriteByte('\n')
}

// ---------------------------------------------------------------- types

// goType returns the Go type a C++ type maps onto.
func (e *emitter) goType(t ctype, line int) (string, error) {
	if t.auto {
		return "", nil // worked out from what it is set to
	}

	switch t.name {
	case "vector", "std::vector", "array", "std::array":
		if len(t.args) == 0 {
			return "", e.errf(line, "a vector without an element type")
		}
		elem, err := e.goType(t.args[0], line)
		if err != nil {
			return "", err
		}
		return "[]" + elem, nil
	case "map", "std::map":
		if len(t.args) < 2 {
			return "", e.errf(line, "a map without its key and value types")
		}
		k, err := e.goType(t.args[0], line)
		if err != nil {
			return "", err
		}
		v, err := e.goType(t.args[1], line)
		if err != nil {
			return "", err
		}
		return "map[" + k + "]" + v, nil
	}

	// "unsigned int" and its like.
	name := t.name
	switch name {
	case "unsigned", "unsigned int":
		return "uint", nil
	case "unsigned long", "unsigned long long":
		return "uint64", nil
	case "long long", "signed long long":
		return "int64", nil
	case "unsigned char":
		return "byte", nil
	case "unsigned short":
		return "uint16", nil
	}
	name = strings.TrimPrefix(name, "signed ")

	if g, ok := builtinTypes[name]; ok {
		// a char* is a string, not a pointer to a byte.
		if (name == "char" || name == "Char_t") && t.ptr > 0 {
			return "string", nil
		}
		if g == "" {
			return "", nil
		}
		if t.ptr > 0 {
			return strings.Repeat("[]", t.ptr) + g, nil
		}
		return g, nil
	}

	if rt, ok := rootTypes[name]; ok {
		e.use(rt.imports...)
		return rt.gotype, nil
	}

	if e.structs[name] {
		if t.ptr > 0 {
			return "*" + name, nil
		}
		return name, nil
	}

	return "", e.errf(line, "nothing is known about the type %q", t.name)
}

// classOf returns the ROOT class a C++ type names, if it is one.
func classOf(t ctype) string {
	if _, ok := rootTypes[t.name]; ok {
		return t.name
	}
	return ""
}

// ---------------------------------------------------------------- program

// emitFile writes a whole macro.
func (e *emitter) emitFile(f *file, cfg *config) (string, error) {
	// the macro's own declarations come first, so that a call to one of
	// them is known before the body that calls it is written.
	for _, d := range f.decls {
		switch d := d.(type) {
		case *funcDecl:
			e.funcs[d.name] = d
		case *structDecl:
			e.structs[d.name] = true
		}
	}

	var body strings.Builder
	for i, d := range f.decls {
		if i > 0 {
			body.WriteByte('\n')
		}
		e.buf.Reset()
		if err := e.emitDecl(d); err != nil {
			return "", err
		}
		body.WriteString(e.buf.String())
	}

	entry := cfg.entry
	if entry == "" {
		entry = e.entryPoint(f)
	}

	var out strings.Builder
	fmt.Fprintf(&out, "// Code generated from %s by hep-cint. DO NOT EDIT.\n", cfg.name)
	out.WriteString("//\n")
	out.WriteString("// It is Go, and it is meant to be read and changed: the point of\n")
	out.WriteString("// translating a macro is to stop having a macro.\n")
	for _, n := range e.notes {
		fmt.Fprintf(&out, "//\n// NOTE: %s\n", n)
	}
	out.WriteString("\n")

	fmt.Fprintf(&out, "package %s\n\n", cfg.pkg)

	if paths := e.used(body.String()); len(paths) > 0 {
		out.WriteString("import (\n")
		// the standard library first, as gofmt groups them.
		var std, ext []string
		for _, p := range paths {
			if strings.Contains(strings.SplitN(p, "/", 2)[0], ".") {
				ext = append(ext, p)
				continue
			}
			std = append(std, p)
		}
		for _, p := range std {
			fmt.Fprintf(&out, "\t%q\n", p)
		}
		if len(std) > 0 && len(ext) > 0 {
			out.WriteString("\n")
		}
		for _, p := range ext {
			fmt.Fprintf(&out, "\t%q\n", p)
		}
		out.WriteString(")\n\n")
	}

	out.WriteString(body.String())

	if cfg.main && entry != "" {
		fmt.Fprintf(&out, "\nfunc main() {\n\t%s(%s)\n}\n", entry, e.entryArgs(entry))
	}

	return out.String(), nil
}

const pkgOS = "os"

// entryPoint returns the function a macro is run by, which ROOT takes to be
// the one named after the file.
func (e *emitter) entryPoint(f *file) string {
	var first string
	for _, d := range f.decls {
		fd, ok := d.(*funcDecl)
		if !ok {
			continue
		}
		if first == "" {
			first = fd.name
		}
		if fd.name == baseName(e.file) {
			return fd.name
		}
	}
	return first
}

// entryArgs fills in the defaults of the entry point's parameters, since
// nothing is passing it anything.
func (e *emitter) entryArgs(name string) string {
	fd, ok := e.funcs[name]
	if !ok {
		return ""
	}
	var args []string
	for _, p := range fd.params {
		if p.dflt != nil {
			s, _, err := e.expr(p.dflt)
			if err == nil {
				args = append(args, s)
				continue
			}
		}
		args = append(args, zeroOf(p.typ))
	}
	return strings.Join(args, ", ")
}

func zeroOf(t ctype) string {
	switch builtinTypes[t.name] {
	case "string":
		return `""`
	case "bool":
		return "false"
	case "":
		return "nil"
	}
	return "0"
}

// used returns the imports the body actually reaches for.
//
// A mapping says which packages its Go needs, and a call that was dropped,
// or one written a different way than the mapping expected, can leave an
// import behind that nothing uses — which Go refuses to compile. Rather than
// keep every mapping's list exactly right, the body is asked.
func (e *emitter) used(body string) []string {
	var out []string
	for p := range e.imports {
		if strings.Contains(body, pkgName(p)+".") {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// pkgName is what a package is called once it is imported.
func pkgName(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

func baseName(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		path = path[i+1:]
	}
	if i := strings.IndexByte(path, '.'); i >= 0 {
		path = path[:i]
	}
	return path
}
