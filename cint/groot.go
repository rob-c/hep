// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cint

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
)

// GRoot runs CINT macros, in the manner of ROOT's gROOT.
//
// It does not interpret them. A macro is translated to Go, built by the Go
// compiler and run as a program, so what runs is compiled code and the
// mistakes in a macro are found by a compiler rather than on the line that
// finally reached them.
//
// That is the trade: a run costs a compile, and in exchange a macro that
// runs at all is a macro that type-checks throughout.
type GRoot struct {
	// Dir is where the program is built. A temporary directory is used,
	// and removed afterwards, when this is empty.
	Dir string

	// Stdout and Stderr are where the macro's output goes. They default to
	// the process's own.
	Stdout io.Writer
	Stderr io.Writer

	// WorkDir is the directory the macro runs in, which is what it will
	// read and write files relative to. It defaults to the caller's.
	WorkDir string

	// Keep leaves the translated Go behind rather than removing it, which
	// is how a macro becomes a Go program for good.
	Keep bool

	// Env is the environment the build and the run get. It defaults to
	// the caller's.
	Env []string
}

// ROOT is the one a program reaches for, so that it can write
// cint.ROOT.Macro("x.C") where a macro writes gROOT->Macro("x.C").
var ROOT = &GRoot{}

// Translate reads a macro and returns the Go it becomes, without building
// or running anything.
func (g *GRoot) Translate(path string) ([]byte, error) {
	return TranslateFile(path)
}

// LoadMacro translates a macro and builds it without running it, which is
// what ROOT's .L does: it says whether the macro is sound.
func (g *GRoot) LoadMacro(path string) error {
	_, err := g.build(path, false)
	return err
}

// Macro translates a macro, builds it and runs it, which is what ROOT's .x
// does.
func (g *GRoot) Macro(path string, args ...string) error {
	_, err := g.build(path, true, args...)
	return err
}

// ProcessLine runs one line the way gROOT->ProcessLine does.
//
// A line beginning with a dot is one of ROOT's own commands: ".x" and ".L"
// take a macro. Anything else is treated as the body of one, so that a
// single statement can be run without a file to put it in.
func (g *GRoot) ProcessLine(line string) error {
	line = strings.TrimSpace(line)

	if rest, ok := strings.CutPrefix(line, "."); ok {
		cmd, arg, _ := strings.Cut(rest, " ")
		arg = strings.TrimSpace(arg)
		switch cmd {
		case "x", "X":
			if arg == "" {
				return fmt.Errorf("cint: .x needs a macro to run")
			}
			return g.Macro(strings.TrimSuffix(arg, "+"))
		case "L":
			if arg == "" {
				return fmt.Errorf("cint: .L needs a macro to load")
			}
			return g.LoadMacro(strings.TrimSuffix(arg, "+"))
		case "q", "quit", "exit":
			return nil
		default:
			return fmt.Errorf("cint: %q is not a command this knows", "."+cmd)
		}
	}

	// a bare statement: wrap it in the macro it would have been.
	if !strings.HasSuffix(line, ";") && !strings.HasSuffix(line, "}") {
		line += ";"
	}
	src := "void __line() {\n" + line + "\n}\n"

	dir, err := os.MkdirTemp("", "hep-cint-")
	if err != nil {
		return fmt.Errorf("cint: %w", err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "__line.C")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		return fmt.Errorf("cint: %w", err)
	}
	return g.Macro(path)
}

// build translates a macro, writes it into a module of its own and hands it
// to the Go toolchain.
func (g *GRoot) build(path string, run bool, args ...string) (string, error) {
	src, err := TranslateFile(path)
	if err != nil {
		return "", err
	}

	dir := g.Dir
	switch {
	case dir == "":
		dir, err = os.MkdirTemp("", "hep-cint-")
		if err != nil {
			return "", fmt.Errorf("cint: %w", err)
		}
		if !g.Keep {
			defer os.RemoveAll(dir)
		}
	default:
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("cint: %w", err)
		}
	}

	main := filepath.Join(dir, "main.go")
	if err := os.WriteFile(main, src, 0o644); err != nil {
		return "", fmt.Errorf("cint: could not write the translation: %w", err)
	}

	if err := g.writeModule(dir); err != nil {
		return "", err
	}
	if err := g.tidy(dir); err != nil {
		return "", err
	}

	if g.Keep || g.Dir != "" {
		fmt.Fprintf(g.stderr(), "cint: %s translated into %s\n", path, main)
	}

	bin := filepath.Join(dir, "macro")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = dir
	build.Stdout = g.stderr()
	build.Stderr = g.stderr()
	build.Env = g.env()
	if err := build.Run(); err != nil {
		return dir, fmt.Errorf("cint: %s did not build: %w", path, err)
	}

	if !run {
		return dir, nil
	}

	// the macro runs where the caller is, not where it happened to be
	// built, so that a file it writes lands where a macro would put it.
	wd := g.WorkDir
	if wd == "" {
		wd, _ = os.Getwd()
	}

	cmd := exec.Command(bin, args...)
	cmd.Dir = wd
	cmd.Stdin = os.Stdin
	cmd.Stdout = g.stdout()
	cmd.Stderr = g.stderr()
	cmd.Env = g.env()
	if err := cmd.Run(); err != nil {
		return dir, fmt.Errorf("cint: %s failed: %w", path, err)
	}
	return dir, nil
}

func (g *GRoot) stdout() io.Writer {
	if g.Stdout != nil {
		return g.Stdout
	}
	return os.Stdout
}

func (g *GRoot) stderr() io.Writer {
	if g.Stderr != nil {
		return g.Stderr
	}
	return os.Stderr
}

func (g *GRoot) env() []string {
	if g.Env != nil {
		return g.Env
	}
	return os.Environ()
}

// writeModule gives the translated program a module of its own, pointed at
// whichever go-hep is to hand.
func (g *GRoot) writeModule(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		return nil
	}

	var buf bytes.Buffer
	buf.WriteString("module hepcintmacro\n\n")
	fmt.Fprintf(&buf, "go %s\n\n", goVersion())
	fmt.Fprintf(&buf, "require %s %s\n", modPath, g.hepVersion())

	src := g.hepDir()
	if src != "" {
		// the go-hep to build against is the one already on this
		// machine, which is what the caller is running.
		fmt.Fprintf(&buf, "\nreplace %s => %s\n", modPath, src)
	}

	err := os.WriteFile(filepath.Join(dir, "go.mod"), buf.Bytes(), 0o644)
	if err != nil {
		return err
	}
	return g.writeSums(dir, src)
}

// writeSums gives the module the checksums of everything go-hep pulls in.
//
// go-hep's own go.sum already lists them at the versions its go.mod asks
// for, and those are the versions this module will resolve to, so copying
// it saves the build from having to work them out — and from needing the
// network to do it.
func (g *GRoot) writeSums(dir, src string) error {
	if src != "" {
		sums, err := os.ReadFile(filepath.Join(src, "go.sum"))
		if err == nil {
			return os.WriteFile(filepath.Join(dir, "go.sum"), sums, 0o644)
		}
	}

	return nil
}

// tidy fills in what the module needs.
//
// The checksums copied from go-hep get this done without the network, since
// every version it resolves to is one go-hep already pinned.
func (g *GRoot) tidy(dir string) error {
	var buf bytes.Buffer
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	cmd.Stderr = &buf
	cmd.Env = g.env()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cint: could not work out what the translation needs: %w\n%s", err, buf.String())
	}
	return nil
}

const modPath = "go-hep.org/x/hep"

func goVersion() string {
	v := strings.TrimPrefix(runtimeVersion(), "go")
	// a go.mod wants a release, not a patch of one.
	parts := strings.Split(v, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return "1.25"
}

// hepVersion is the go-hep this was built against, so that a macro is run
// against the same one.
//
// A program built from a checkout rather than from a release reports its
// version as "(devel)", which a go.mod will not take. The replace directive
// is what points the build at the right source in that case, and the version
// beside it only has to be well formed.
func (g *GRoot) hepVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range bi.Deps {
			if dep.Path == modPath && isVersion(dep.Version) {
				return dep.Version
			}
		}
		if bi.Main.Path == modPath && isVersion(bi.Main.Version) {
			return bi.Main.Version
		}
	}
	return "v0.0.0"
}

// isVersion reports whether a go.mod would accept the string as a version.
func isVersion(v string) bool {
	if !strings.HasPrefix(v, "v") {
		return false
	}
	for _, c := range v[1:] {
		if c >= '0' && c <= '9' {
			return true
		}
		break
	}
	return false
}

// hepDir finds the go-hep on this machine, so that the translation is built
// against it rather than fetched.
func (g *GRoot) hepDir() string {
	wd := g.WorkDir
	if wd == "" {
		wd, _ = os.Getwd()
	}

	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", modPath)
	cmd.Dir = wd
	cmd.Env = g.env()
	out, err := cmd.Output()
	if err == nil {
		if dir := strings.TrimSpace(string(out)); dir != "" {
			return dir
		}
	}

	// not in a module that knows about it: fall back on where this
	// program's own source is, which is the case when hep-cint is run
	// out of a checkout.
	if _, file, _, ok := callerFile(); ok {
		dir := filepath.Dir(filepath.Dir(file))
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
	}
	return ""
}
