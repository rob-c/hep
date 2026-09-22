// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command hep-cint translates a CINT macro into Go, and runs it.
//
//	$> hep-cint hsimple.C            # write the Go to standard output
//	$> hep-cint -o hsimple.go hsimple.C
//	$> hep-cint -run hsimple.C       # translate, build and run it
//
// A ROOT macro is C++ as CINT accepts it. What comes out is Go that calls
// into go-hep and does the same thing, and that is meant to be kept: the
// point of translating a macro is to stop having a macro.
//
// With -run the macro is built by the Go compiler and run as a program.
// Nothing is interpreted, so a macro that runs at all is one that
// type-checks throughout, and the mistakes in it are found by a compiler
// rather than on the line that finally reached them.
//
// What the translation does not cover it refuses, naming the line and
// saying why, rather than quietly producing Go that does something else.
//
//	$> hep-cint -h
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"go-hep.org/x/hep/cint"
)

func main() {
	log.SetPrefix("hep-cint: ")
	log.SetFlags(0)

	var (
		out  = flag.String("o", "", "write the Go to this file instead of standard output")
		run  = flag.Bool("run", false, "build the translation and run it")
		keep = flag.String("keep", "", "build into this directory and leave it there")
		pkg  = flag.String("pkg", "main", "the package clause of the Go that comes out")
		main = flag.Bool("main", true, "write a main that calls the macro")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: hep-cint [options] macro.C

hep-cint translates a CINT macro into Go.

Options:
`)
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	path := flag.Arg(0)

	switch {
	case *run || *keep != "":
		g := &cint.GRoot{Dir: *keep, Keep: *keep != ""}
		if err := g.Macro(path); err != nil {
			log.Fatalf("%+v", err)
		}

	default:
		src, err := cint.TranslateFile(path, cint.Package(*pkg), cint.Main(*main))
		if err != nil {
			// the translation is still worth showing when Go would not
			// take it: that is how it gets fixed.
			if len(src) > 0 {
				os.Stdout.Write(src)
			}
			log.Fatalf("%+v", err)
		}

		if *out == "" {
			os.Stdout.Write(src)
			return
		}
		if err := os.WriteFile(*out, src, 0o644); err != nil {
			log.Fatalf("could not write %q: %+v", *out, err)
		}
	}
}
