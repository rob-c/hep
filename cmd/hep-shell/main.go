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
// compiled go-hep.
//
// hep-kernel is the same session behind a Jupyter notebook.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"go-hep.org/x/hep/cmd/internal/hepsh"
)

func main() {
	eval := flag.String("e", "", "evaluate this and leave")
	quiet := flag.Bool("q", false, "no banner")
	flag.Parse()

	sh, err := newShell(os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hep-shell: %+v\n", err)
		os.Exit(1)
	}

	if *eval != "" {
		if err := sh.eval(*eval); err != nil {
			fmt.Fprintf(os.Stderr, "hep-shell: %+v\n", err)
			os.Exit(1)
		}
		return
	}

	for _, fname := range flag.Args() {
		if err := sh.sess.Run(fname); err != nil {
			fmt.Fprintf(os.Stderr, "hep-shell: %+v\n", err)
			os.Exit(1)
		}
	}

	if !*quiet {
		sh.banner()
	}
	sh.loop()
}

type shell struct {
	sess *hepsh.Session
	out  io.Writer
	err  io.Writer

	// pipe reads lines when the input is not a terminal, where there is no
	// line editing to do and every byte matters.
	pipe *bufio.Reader

	n int // how many lines have been accepted, as ROOT counts them
}

func newShell(out, errw io.Writer) (*shell, error) {
	sess, err := hepsh.New(out, errw)
	if err != nil {
		return nil, err
	}
	return &shell{sess: sess, out: out, err: errw, pipe: bufio.NewReader(os.Stdin)}, nil
}

func (sh *shell) banner() {
	fmt.Fprintf(sh.out, `   ------------------------------------------------------------
  | Welcome to hep-shell                                       |
  | an interactive Go prompt, with go-hep already imported     |
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
		if hepsh.Unbalanced(src) {
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
	out, err := sh.sess.Eval(src)
	if err != nil {
		return err
	}
	if out != "" {
		fmt.Fprintln(sh.out, out)
	}
	return nil
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
		return false, sh.sess.Run(arg)

	case ".ls":
		decls := sh.sess.Decls()
		if len(decls) == 0 {
			fmt.Fprintln(sh.out, "  (nothing defined yet)")
			return false, nil
		}
		for _, n := range decls {
			fmt.Fprintf(sh.out, "  %s\n", n)
		}
		return false, nil

	case ".imports":
		for _, p := range sh.sess.Imports() {
			fmt.Fprintf(sh.out, "  %s\n", p)
		}
		return false, nil

	case ".reset":
		err := sh.sess.Reset()
		if err != nil {
			return false, err
		}
		sh.n = 0
		fmt.Fprintln(sh.out, "  (session reset)")
		return false, nil
	}

	return false, fmt.Errorf("unknown command %q: try .help", cmd)
}
