// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command hep-shell is an interactive Go prompt with go-hep loaded, in the
// manner of ROOT's C++ one.
//
// Type Go at it and it runs, keeping what it has been told between lines, so
// a session builds up the way a ROOT session does:
//
//	hep [0] f, err := groot.Open("data.root")
//	hep [1] t := f.Get("tree").(rtree.Tree)
//	hep [2] t.Entries()
//	(int64) 20000
//
// The packages a session reaches for — hbook, hplot, groot, rtree, rhist,
// fit, minuit — are imported before the prompt appears, so they can be used
// without saying so first. Anything else needs the usual import statement.
//
// An expression on its own has its value printed, with its type in front of
// it, which is how ROOT answers too.
//
// # Commands
//
// The commands beginning with a dot are the shell's own, and follow ROOT's:
//
//	.q, .quit     leave
//	.help, .?     this list
//	.x FILE       run a file, as ROOT runs a macro
//	.L FILE       load a file's declarations into the session
//	.ls           list what the session has defined
//	.imports      list what is already imported
//	.reset        start again, forgetting everything
//
// # What it is
//
// The interpreter is yaegi, which runs Go source in pure Go — there is no
// C++, no LLVM and no ROOT anywhere in it. What it calls into is the
// compiled go-hep, reached through the tables in internal/symbols.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
	"golang.org/x/term"

	"go-hep.org/x/hep/cmd/hep-shell/internal/symbols"
)

func main() {
	log := flag.String("e", "", "evaluate this and leave")
	quiet := flag.Bool("q", false, "no banner")
	flag.Parse()

	sh, err := newShell(os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hep-shell: %+v\n", err)
		os.Exit(1)
	}

	if *log != "" {
		if err := sh.eval(*log); err != nil {
			fmt.Fprintf(os.Stderr, "hep-shell: %+v\n", err)
			os.Exit(1)
		}
		return
	}

	for _, fname := range flag.Args() {
		if err := sh.run(fname); err != nil {
			fmt.Fprintf(os.Stderr, "hep-shell: %+v\n", err)
			os.Exit(1)
		}
	}

	if !*quiet {
		sh.banner()
	}
	sh.loop()
}

// preloaded are the packages a session gets without asking.
var preloaded = []string{
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

type shell struct {
	ip  *interp.Interpreter
	out io.Writer
	err io.Writer

	// pipe reads lines when the input is not a terminal, where there is no
	// line editing to do and every byte matters.
	pipe *bufio.Reader

	n       int      // how many lines have been accepted, as ROOT counts them
	imports []string // what has been imported, in the order it was
	decls   []string // the names the session has defined
}

func newShell(out, errw io.Writer) (*shell, error) {
	sh := &shell{out: out, err: errw, pipe: bufio.NewReader(os.Stdin)}
	if err := sh.reset(); err != nil {
		return nil, err
	}
	return sh, nil
}

// reset starts the session again, with nothing in it but the preloaded
// packages.
func (sh *shell) reset() error {
	ip := interp.New(interp.Options{Stdout: sh.out, Stderr: sh.err})

	err := ip.Use(stdlib.Symbols)
	if err != nil {
		return fmt.Errorf("could not load the standard library: %w", err)
	}

	err = ip.Use(symbols.Symbols)
	if err != nil {
		return fmt.Errorf("could not load go-hep: %w", err)
	}

	sh.ip = ip
	sh.imports = nil
	sh.decls = nil

	for _, p := range preloaded {
		if err := sh.importPkg(p); err != nil {
			return fmt.Errorf("could not import %q: %w", p, err)
		}
	}

	return nil
}

// importPkg brings a package into the session, and does nothing if it is
// already there: importing twice is an error to the interpreter, and no news
// at all to the person at the prompt.
func (sh *shell) importPkg(path string) error {
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

func (sh *shell) banner() {
	fmt.Fprintf(sh.out, `   ------------------------------------------------------------
  | Welcome to hep-shell                                       |
  | an interactive Go prompt, with go-hep already imported      |
  |                                                            |
  | Type Go and it runs. An expression prints its value.       |
  | .help lists the commands, .q leaves.                       |
   ------------------------------------------------------------

`)
}

// loop reads a line at a time and runs it, gathering continuation lines while
// what has been typed cannot yet be a complete piece of Go.
func (sh *shell) loop() {
	var (
		in    io.Reader = os.Stdin
		lines *term.Terminal
	)

	// a terminal gets line editing and history; a pipe gets neither, and
	// neither does it want them.
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		state, err := term.MakeRaw(int(f.Fd()))
		if err == nil {
			defer term.Restore(int(f.Fd()), state)
			lines = term.NewTerminal(struct {
				io.Reader
				io.Writer
			}{in, sh.out}, "")
		}
	}

	var pending []string
	for {
		prompt := fmt.Sprintf("hep [%d] ", sh.n)
		if len(pending) > 0 {
			prompt = strings.Repeat(" ", len(prompt)-4) + "... "
		}

		line, err := sh.readLine(lines, prompt)
		if err != nil {
			if len(pending) > 0 {
				fmt.Fprintln(sh.out)
			}
			return
		}

		switch {
		case len(pending) == 0 && strings.TrimSpace(line) == "":
			continue

		case len(pending) == 0 && strings.HasPrefix(strings.TrimSpace(line), "."):
			done, err := sh.command(strings.TrimSpace(line))
			if err != nil {
				fmt.Fprintf(sh.err, "%v\n", err)
			}
			if done {
				return
			}
			continue
		}

		pending = append(pending, line)
		src := strings.Join(pending, "\n")

		// hold off while the braces are still open: a function body or a
		// loop arrives over several lines.
		if unbalanced(src) {
			continue
		}
		pending = pending[:0]

		if err := sh.eval(src); err != nil {
			fmt.Fprintf(sh.err, "%v\n", err)
			continue
		}
		sh.n++
	}
}

func (sh *shell) readLine(lines *term.Terminal, prompt string) (string, error) {
	if lines != nil {
		lines.SetPrompt(prompt)
		return lines.ReadLine()
	}

	fmt.Fprint(sh.out, prompt)

	line, err := sh.pipe.ReadString('\n')
	switch {
	case err == io.EOF && line == "":
		return "", io.EOF
	case err != nil && err != io.EOF:
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// eval runs a piece of Go and prints what it evaluated to, if anything.
func (sh *shell) eval(src string) error {
	v, err := sh.ip.Eval(src)
	if err != nil {
		return err
	}

	sh.note(src)

	if !v.IsValid() {
		return nil
	}

	// Only an expression has a value worth showing. A declaration, an
	// assignment or a loop evaluates to something in yaegi too, but showing
	// it would be answering a question nobody asked.
	if !isExpr(src) {
		return nil
	}

	fmt.Fprintf(sh.out, "(%s) %v\n", typeOf(v), format(v))
	return nil
}

// isExpr reports whether src is an expression rather than a statement.
//
// It asks the Go parser rather than looking for ":=" and friends: "h.Fill(x)"
// and "h, err = f()" and "for i := range 10 {}" are told apart by parsing
// them, and not reliably by anything short of it.
func isExpr(src string) bool {
	_, err := parser.ParseExpr(strings.TrimSpace(src))
	return err == nil
}

// typeOf names the type of a value the way Go does.
func typeOf(v reflect.Value) string {
	if !v.IsValid() {
		return "<nil>"
	}
	return v.Type().String()
}

// maxPrint is how much of a value the shell will show before it stops. A
// ROOT file or a tree printed whole is thousands of characters of internals,
// which buries the answer rather than giving it.
const maxPrint = 480

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

	if len(s) > maxPrint {
		s = s[:maxPrint] + "... (truncated)"
	}
	return s
}

// note remembers what a line brought into the session, for .ls and .imports.
func (sh *shell) note(src string) {
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

// command runs one of the shell's own dot-commands, and says whether the
// shell should stop.
func (sh *shell) command(line string) (bool, error) {
	cmd, arg, _ := strings.Cut(line, " ")
	arg = strings.TrimSpace(arg)

	switch cmd {
	case ".q", ".quit", ".exit":
		return true, nil

	case ".help", ".?":
		fmt.Fprint(sh.out, `  .q, .quit     leave
  .help, .?     this list
  .x FILE       run a file, as ROOT runs a macro
  .L FILE       load a file's declarations into the session
  .ls           list what the session has defined
  .imports      list what is already imported
  .reset        start again, forgetting everything
`)
		return false, nil

	case ".x", ".L":
		if arg == "" {
			return false, fmt.Errorf("%s needs a file", cmd)
		}
		return false, sh.run(arg)

	case ".ls":
		if len(sh.decls) == 0 {
			fmt.Fprintln(sh.out, "  (nothing defined yet)")
			return false, nil
		}
		names := append([]string(nil), sh.decls...)
		sort.Strings(names)
		for _, n := range unique(names) {
			fmt.Fprintf(sh.out, "  %s\n", n)
		}
		return false, nil

	case ".imports":
		for _, p := range sh.imports {
			fmt.Fprintf(sh.out, "  %s\n", p)
		}
		return false, nil

	case ".reset":
		err := sh.reset()
		if err != nil {
			return false, err
		}
		sh.n = 0
		fmt.Fprintln(sh.out, "  (session reset)")
		return false, nil
	}

	return false, fmt.Errorf("unknown command %q: try .help", cmd)
}

// run reads a file and evaluates it, the way ROOT runs a macro.
//
// A file that is a whole Go program has its package clause and imports taken
// off first, since the session already is a program and already has them.
func (sh *shell) run(fname string) error {
	raw, err := os.ReadFile(fname)
	if err != nil {
		return fmt.Errorf("could not read %q: %w", fname, err)
	}

	src, imports := splitProgram(string(raw))

	for _, p := range imports {
		if err := sh.importPkg(p); err != nil {
			return fmt.Errorf("%s: could not import %q: %w", filepath.Base(fname), p, err)
		}
	}

	if strings.TrimSpace(src) == "" {
		return nil
	}

	if _, err := sh.ip.Eval(src); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(fname), err)
	}

	sh.noteFile(string(raw))
	return nil
}

// noteFile records what a loaded file declared, so that .ls can say so.
//
// The file is parsed rather than scanned for keywords: a macro is a whole Go
// file and the parser reads one properly.
func (sh *shell) noteFile(src string) {
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

// splitProgram takes the package clause and the imports off a file, and
// returns what is left along with the paths that were imported.
func splitProgram(src string) (string, []string) {
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

// importPath pulls the path out of one line of an import, ignoring any name
// in front of it.
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

// unbalanced reports whether what has been typed has brackets still open, and
// so wants another line before it can be run.
//
// It counts brackets outside of strings, runes and comments, which is enough
// to tell a half-typed function body from a finished one.
func unbalanced(src string) bool {
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

func unique(sorted []string) []string {
	o := sorted[:0]
	var prev string
	for i, s := range sorted {
		if i > 0 && s == prev {
			continue
		}
		o = append(o, s)
		prev = s
	}
	return o
}
