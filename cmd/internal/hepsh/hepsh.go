// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package hepsh is the Go session that hep-shell and hep-kernel both drive.
//
// It holds the interpreter, the packages a go-hep session starts with, and
// the rule for whether a line has a value worth showing. What it does not
// hold is anything about how the session is talked to: a terminal and a
// notebook want the same session and nothing else in common.
package hepsh // import "go-hep.org/x/hep/cmd/internal/hepsh"

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"

	"go-hep.org/x/hep/cmd/internal/symbols"
)

// Preloaded are the packages a session gets without asking.
var Preloaded = []string{
	"fmt",
	"math",
	"os",
	"go-hep.org/x/hep/hbook",
	"go-hep.org/x/hep/hplot",
	"go-hep.org/x/hep/fit",
	"go-hep.org/x/hep/fit/minuit",
	"go-hep.org/x/hep/groot",
	"go-hep.org/x/hep/groot/rtree",
	"go-hep.org/x/hep/groot/rhist",
	"go-hep.org/x/hep/hbook/ntup/ntroot",
	"go-hep.org/x/hep/hbook/rootcnv",
}

// MaxPrint is how much of a value a session will show before it stops. A ROOT
// file or a tree printed whole is thousands of characters of internals, which
// buries the answer rather than giving it.
const MaxPrint = 480

// Session is a Go session with go-hep loaded.
type Session struct {
	ip  *interp.Interpreter
	out io.Writer
	err io.Writer

	imports []string
	decls   []string
}

// New returns a session writing what the interpreted code prints to out and
// errw.
func New(out, errw io.Writer) (*Session, error) {
	sh := &Session{out: out, err: errw}
	if err := sh.Reset(); err != nil {
		return nil, err
	}
	return sh, nil
}

// Reset starts the session again, with nothing in it but the preloaded
// packages.
func (sh *Session) Reset() error {
	ip := interp.New(interp.Options{Stdout: sh.out, Stderr: sh.err})

	err := ip.Use(stdlib.Symbols)
	if err != nil {
		return fmt.Errorf("hepsh: could not load the standard library: %w", err)
	}

	err = ip.Use(symbols.Symbols)
	if err != nil {
		return fmt.Errorf("hepsh: could not load go-hep: %w", err)
	}

	sh.ip = ip
	sh.imports = nil
	sh.decls = nil

	for _, p := range Preloaded {
		if err := sh.Import(p); err != nil {
			return fmt.Errorf("hepsh: could not import %q: %w", p, err)
		}
	}

	return nil
}

// Import brings a package into the session, and does nothing if it is already
// there: importing twice is an error to the interpreter, and no news at all
// to whoever is driving it.
func (sh *Session) Import(path string) error {
	if slices.Contains(sh.imports, path) {
		return nil
	}

	_, err := sh.ip.Eval(fmt.Sprintf("import %q", path))
	if err != nil {
		return err
	}

	sh.imports = append(sh.imports, path)
	return nil
}

// Imports returns what the session has imported, in the order it did.
func (sh *Session) Imports() []string { return slices.Clone(sh.imports) }

// Decls returns the names the session has defined, sorted and without
// repeats.
func (sh *Session) Decls() []string {
	names := slices.Clone(sh.decls)
	sort.Strings(names)
	return slices.Compact(names)
}

// Eval runs a piece of Go and returns what it evaluated to, ready to be
// shown, or the empty string for a line with no value worth showing.
func (sh *Session) Eval(src string) (string, error) {
	v, err := sh.ip.Eval(src)
	if err != nil {
		return "", err
	}

	sh.note(src)

	if !v.IsValid() || !IsExpr(src) {
		return "", nil
	}

	return fmt.Sprintf("(%s) %s", typeOf(v), format(v)), nil
}

// IsExpr reports whether src is an expression rather than a statement.
//
// It asks the Go parser rather than looking for ":=" and friends:
// "h.Fill(x)", "h, err = f()" and "for i := range 10 {}" are told apart by
// parsing them, and not reliably by anything short of it.
func IsExpr(src string) bool {
	_, err := parser.ParseExpr(strings.TrimSpace(src))
	return err == nil
}

func typeOf(v reflect.Value) string {
	if !v.IsValid() {
		return "<nil>"
	}
	return v.Type().String()
}

// format prints a value, following a pointer to a struct so that a histogram
// or a fit shows what it holds rather than its address.
func format(v reflect.Value) string {
	var s string
	switch {
	case v.Kind() == reflect.Ptr && !v.IsNil() && v.Elem().Kind() == reflect.Struct:
		s = fmt.Sprintf("&%+v", v.Elem().Interface())
	default:
		s = fmt.Sprintf("%+v", v.Interface())
	}

	if len(s) > MaxPrint {
		s = s[:MaxPrint] + "... (truncated)"
	}
	return s
}

// note remembers what a line brought into the session.
func (sh *Session) note(src string) {
	s := strings.TrimSpace(src)

	if strings.HasPrefix(s, "import ") {
		p := strings.Trim(strings.TrimPrefix(s, "import "), `"`)
		if !slices.Contains(sh.imports, p) {
			sh.imports = append(sh.imports, p)
		}
		return
	}

	switch {
	case strings.HasPrefix(s, "func "):
		name := strings.TrimPrefix(s, "func ")
		if i := strings.IndexAny(name, "("); i > 0 {
			sh.decls = append(sh.decls, "func "+strings.TrimSpace(name[:i]))
		}
	case strings.HasPrefix(s, "type "):
		name := strings.Fields(strings.TrimPrefix(s, "type "))
		if len(name) > 0 {
			sh.decls = append(sh.decls, "type "+name[0])
		}
	case strings.Contains(s, ":="):
		// the variable a loop or an if declares belongs to it, not to the
		// session: it is gone by the time the next line is typed.
		for _, kw := range []string{"for ", "if ", "switch ", "select "} {
			if strings.HasPrefix(s, kw) {
				return
			}
		}
		lhs := strings.TrimSpace(s[:strings.Index(s, ":=")])
		for _, name := range strings.Split(lhs, ",") {
			name = strings.TrimSpace(name)
			if name != "" && name != "_" {
				sh.decls = append(sh.decls, name)
			}
		}
	}
}

// Run reads a file and evaluates it, the way ROOT runs a macro.
//
// A file that is a whole Go program has its package clause and imports taken
// off first, since the session already is a program and already has them.
func (sh *Session) Run(fname string) error {
	raw, err := os.ReadFile(fname)
	if err != nil {
		return fmt.Errorf("hepsh: could not read %q: %w", fname, err)
	}
	return sh.RunSource(string(raw), fname)
}

// RunSource evaluates a whole Go file's worth of source.
func (sh *Session) RunSource(raw, name string) error {
	src, imports := SplitProgram(raw)

	for _, p := range imports {
		if err := sh.Import(p); err != nil {
			return fmt.Errorf("%s: could not import %q: %w", name, p, err)
		}
	}

	if strings.TrimSpace(src) == "" {
		return nil
	}

	if _, err := sh.ip.Eval(src); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	sh.noteFile(raw)
	return nil
}

// noteFile records what a loaded file declared.
func (sh *Session) noteFile(src string) {
	f, err := parser.ParseFile(token.NewFileSet(), "macro.go", src, parser.SkipObjectResolution)
	if err != nil {
		// it ran, so it parsed somewhere: nothing here is worth an error.
		return
	}

	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				sh.decls = append(sh.decls, "func "+d.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					sh.decls = append(sh.decls, "type "+spec.Name.Name)
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						if name.Name != "_" {
							sh.decls = append(sh.decls, name.Name)
						}
					}
				}
			}
		}
	}
}

// SplitProgram takes the package clause and the imports off a file, and
// returns what is left along with the paths that were imported.
func SplitProgram(src string) (string, []string) {
	var (
		body    strings.Builder
		imports []string
		inBlock bool
	)

	for _, line := range strings.Split(src, "\n") {
		s := strings.TrimSpace(line)

		switch {
		case inBlock:
			if s == ")" {
				inBlock = false
				continue
			}
			if p := importPath(s); p != "" {
				imports = append(imports, p)
			}
			continue

		case strings.HasPrefix(s, "package "):
			continue

		case s == "import (":
			inBlock = true
			continue

		case strings.HasPrefix(s, "import "):
			if p := importPath(strings.TrimPrefix(s, "import ")); p != "" {
				imports = append(imports, p)
			}
			continue
		}

		body.WriteString(line)
		body.WriteString("\n")
	}

	return body.String(), imports
}

func importPath(s string) string {
	i := strings.Index(s, `"`)
	if i < 0 {
		return ""
	}
	j := strings.Index(s[i+1:], `"`)
	if j < 0 {
		return ""
	}
	return s[i+1 : i+1+j]
}

// Unbalanced reports whether what has been typed has brackets still open, and
// so wants another line before it can be run.
//
// It counts brackets outside of strings, runes and comments, which is enough
// to tell a half-typed function body from a finished one.
func Unbalanced(src string) bool {
	var (
		depth               int
		inStr, inChr, inRaw bool
		inLine, inBlock     bool
	)

	for i := 0; i < len(src); i++ {
		c := src[i]

		switch {
		case inLine:
			if c == '\n' {
				inLine = false
			}
			continue

		case inBlock:
			if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				inBlock = false
				i++
			}
			continue

		case inRaw:
			if c == '`' {
				inRaw = false
			}
			continue

		case inStr:
			switch c {
			case '\\':
				i++
			case '"':
				inStr = false
			}
			continue

		case inChr:
			switch c {
			case '\\':
				i++
			case '\'':
				inChr = false
			}
			continue
		}

		switch c {
		case '/':
			if i+1 < len(src) {
				switch src[i+1] {
				case '/':
					inLine = true
					i++
				case '*':
					inBlock = true
					i++
				}
			}
		case '"':
			inStr = true
		case '\'':
			inChr = true
		case '`':
			inRaw = true
		case '{', '(', '[':
			depth++
		case '}', ')', ']':
			depth--
		}
	}

	return depth > 0
}
