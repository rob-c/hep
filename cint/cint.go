// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cint translates a CINT macro into Go.
//
// A ROOT macro is C++ as CINT accepts it: a function named after the file,
// arithmetic, loops, and calls into ROOT. This package reads that, and
// writes Go that calls into go-hep and does the same thing.
//
//	src, err := cint.TranslateFile("hsimple.C")
//
// What comes out is meant to be read and kept. Translating a macro is
// worth doing because it stops being a macro: it becomes Go that compiles,
// that a test can call, and that the next person can change without a C++
// interpreter anywhere in sight.
//
// # What it covers
//
// The C++ a macro is written in: functions, structs, declarations, if, for
// (counted and ranged), while, do-while, switch, return, break, continue,
// and the arithmetic, comparisons and calls in between. Along with the ROOT
// vocabulary a macro is mostly made of — the histogram classes, TFile,
// TTree, TF1, TCanvas, TGraph, TRandom, TMath, printf and cout.
//
// What it does not cover it refuses, naming the line and saying why. A
// translation that quietly did something else would be worse than no
// translation at all, so there is no silent best effort anywhere in here.
//
// # Where the differences are
//
// C++ widens a whole number to a real wherever one is wanted and Go does
// not, so the conversions C++ left unwritten are written out. A char* and a
// TString are both a Go string. A float is a float64, since a macro's
// arithmetic is done in double anyway and the narrower type buys nothing.
//
// ROOT's own session — gROOT, gDirectory, the graphics that draw as the
// macro runs — has no equivalent in a compiled program. The small part of it
// a macro actually leans on is in go-hep.org/x/hep/cint/rt: a current
// canvas, a current file, gRandom. The rest is refused rather than faked.
//
// # Running one
//
// GRoot runs a macro the way ROOT's gROOT does, by translating it and
// building it rather than interpreting it. The hep-cint command does the
// same from a shell, and hep-shell will run a .C file at the prompt.
package cint // import "go-hep.org/x/hep/cint"

import (
	"fmt"
	"go/format"
	"os"
	"strings"
)

// Option configures a translation.
type Option func(*config)

type config struct {
	pkg   string
	name  string
	main  bool
	entry string
}

func newConfig(opts []Option) *config {
	cfg := &config{pkg: "main", name: "a macro", main: true}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// Package sets the package clause of the Go that comes out. It is "main" by
// default, which is what a macro is: a program.
func Package(name string) Option {
	return func(cfg *config) { cfg.pkg = name }
}

// Name is what the macro is called, which the translation says at the top
// and which decides the entry point when the macro has more than one
// function, as ROOT decides it.
func Name(name string) Option {
	return func(cfg *config) { cfg.name = name }
}

// Main says whether to write a main that calls the macro. It is on by
// default; turn it off to translate a macro into a package of functions.
func Main(v bool) Option {
	return func(cfg *config) { cfg.main = v }
}

// Entry names the function main should call, for a macro whose entry point
// is not the one named after the file.
func Entry(name string) Option {
	return func(cfg *config) { cfg.entry = name }
}

// Translate turns a macro into Go.
func Translate(src []byte, opts ...Option) ([]byte, error) {
	cfg := newConfig(opts)

	f, err := parse(string(src))
	if err != nil {
		return nil, err
	}

	e := newEmitter()
	e.file = cfg.name

	out, err := e.emitFile(f, cfg)
	if err != nil {
		return nil, err
	}

	pretty, err := format.Source([]byte(out))
	if err != nil {
		// the translation is still worth having: hand it back along with
		// what Go made of it, so the trouble can be seen.
		return []byte(out), fmt.Errorf(
			"cint: the translation of %s is not valid Go: %w", cfg.name, err,
		)
	}
	return pretty, nil
}

// TranslateFile reads a macro and translates it.
func TranslateFile(path string, opts ...Option) ([]byte, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cint: could not read %q: %w", path, err)
	}

	opts = append([]Option{Name(path)}, opts...)
	return Translate(src, opts...)
}

// IsMacro reports whether a path looks like a CINT macro rather than Go.
func IsMacro(path string) bool {
	for _, ext := range []string{".C", ".c", ".cxx", ".cpp", ".cc", ".C+", ".C++"} {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

// EntryOf returns the function a macro is run by.
//
// ROOT runs the function named after the file, which is the convention every
// macro follows; where there is no such function this falls back on the
// first one the macro defines.
func EntryOf(path string) (string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("cint: could not read %q: %w", path, err)
	}

	f, err := parse(string(src))
	if err != nil {
		return "", err
	}

	e := newEmitter()
	e.file = path
	for _, d := range f.decls {
		if fd, ok := d.(*funcDecl); ok {
			e.funcs[fd.name] = fd
		}
	}

	entry := e.entryPoint(f)
	if entry == "" {
		return "", fmt.Errorf("cint: %s defines no function to run", path)
	}
	return entry, nil
}
