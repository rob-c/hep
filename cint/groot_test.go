// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cint_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go-hep.org/x/hep/cint"
)

// These tests build and run what they translate, which costs a compile
// each. They say what a macro printed, which is the only way to be sure the
// translation of it means what the macro meant.
func needsToolchain(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping: building a macro takes a compile")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("skipping: no Go toolchain to build with")
	}
}

// write puts a macro in a directory of the test's own.
func write(t *testing.T, name, src string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestMacro runs a macro and checks what it printed.
func TestMacro(t *testing.T) {
	needsToolchain(t)

	path := write(t, "counts.C", `
void counts() {
   TH1D *h = new TH1D("h", "a title", 10, 0, 10);
   for (int i = 0; i < 100; i++) {
      h->Fill(i % 10);
   }
   printf("entries=%v integral=%v mean=%v bin3=%v\n",
          h->GetEntries(), h->Integral(), h->GetMean(), h->GetBinContent(3));
}
`)

	var out bytes.Buffer
	g := &cint.GRoot{Stdout: &out, Stderr: &out}
	if err := g.Macro(path); err != nil {
		t.Fatalf("could not run the macro: %+v\n%s", err, out.String())
	}

	// a hundred entries spread evenly over ten bins, so the mean is 4.5
	// and every bin holds ten.
	want := "entries=100 integral=100 mean=4.5 bin3=10\n"
	if got := out.String(); got != want {
		t.Errorf("got=%q, want=%q", got, want)
	}
}

// TestMacroArithmetic checks that the promotions C++ left unwritten come out
// meaning the same thing once they are written.
func TestMacroArithmetic(t *testing.T) {
	needsToolchain(t)

	path := write(t, "sums.C", `
double dist(double x, double y) { return TMath::Sqrt(x*x + y*y); }

void sums() {
   int    n = 0;
   double s = 0;
   for (int i = 0; i < 5; i++) {
      n += i;
      s += dist(i, 2*i);
   }
   printf("%v %.6f\n", n, s);
}
`)

	var out bytes.Buffer
	g := &cint.GRoot{Stdout: &out, Stderr: &out}
	if err := g.Macro(path); err != nil {
		t.Fatalf("could not run the macro: %+v\n%s", err, out.String())
	}

	// the distances are i*sqrt(5), so they add to 10*sqrt(5).
	want := "10 22.360680\n"
	if got := out.String(); got != want {
		t.Errorf("got=%q, want=%q", got, want)
	}
}

// TestMacroWritesAFile checks that a macro writing a file puts it where the
// caller is, rather than wherever it happened to be built.
func TestMacroWritesAFile(t *testing.T) {
	needsToolchain(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "save.C")
	src := `
void save() {
   TFile *f = TFile::Open("out.root", "RECREATE");
   TH1D *h = new TH1D("h", "a title", 4, 0, 4);
   h->Fill(1.5);
   h->Fill(2.5);
   h->Write();
   f->Write();
   printf("wrote %v\n", h->GetEntries());
}
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	g := &cint.GRoot{Stdout: &out, Stderr: &out, WorkDir: dir}
	if err := g.Macro(path); err != nil {
		t.Fatalf("could not run the macro: %+v\n%s", err, out.String())
	}

	if got, want := out.String(), "wrote 2\n"; got != want {
		t.Errorf("got=%q, want=%q", got, want)
	}

	written := filepath.Join(dir, "out.root")
	fi, err := os.Stat(written)
	if err != nil {
		t.Fatalf("the macro should have written %q: %+v", written, err)
	}
	if fi.Size() == 0 {
		t.Errorf("%q is empty", written)
	}
}

// TestProcessLine checks the one-liner form, which is what a ROOT session
// does with gROOT->ProcessLine.
func TestProcessLine(t *testing.T) {
	needsToolchain(t)

	var out bytes.Buffer
	g := &cint.GRoot{Stdout: &out, Stderr: &out}

	if err := g.ProcessLine(`printf("%v\n", 6*7);`); err != nil {
		t.Fatalf("could not run the line: %+v\n%s", err, out.String())
	}
	if got, want := out.String(), "42\n"; got != want {
		t.Errorf("got=%q, want=%q", got, want)
	}
}

// TestProcessLineDotX checks that ProcessLine takes ROOT's own commands.
func TestProcessLineDotX(t *testing.T) {
	needsToolchain(t)

	path := write(t, "hello.C", `void hello() { printf("hello\n"); }`)

	var out bytes.Buffer
	g := &cint.GRoot{Stdout: &out, Stderr: &out}

	if err := g.ProcessLine(".x " + path); err != nil {
		t.Fatalf("could not run the macro: %+v\n%s", err, out.String())
	}
	if got, want := out.String(), "hello\n"; got != want {
		t.Errorf("got=%q, want=%q", got, want)
	}

	// .L builds it without running it, so nothing more should be printed.
	out.Reset()
	if err := g.LoadMacro(path); err != nil {
		t.Fatalf("could not load the macro: %+v\n%s", err, out.String())
	}
	if got := out.String(); got != "" {
		t.Errorf("loading a macro should not run it, got %q", got)
	}
}

// TestKeep checks that the Go a macro became can be kept, which is the
// point of translating one.
func TestKeep(t *testing.T) {
	needsToolchain(t)

	var (
		path = write(t, "kept.C", `void kept() { printf("kept\n"); }`)
		dir  = t.TempDir()
		out  bytes.Buffer
	)

	g := &cint.GRoot{Stdout: &out, Stderr: &out, Dir: dir, Keep: true}
	if err := g.Macro(path); err != nil {
		t.Fatalf("could not run the macro: %+v\n%s", err, out.String())
	}

	src, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("the translation should have been kept: %+v", err)
	}
	if got := string(src); !strings.Contains(got, "func kept()") {
		t.Errorf("the translation should hold the macro:\n%s", got)
	}
}

// TestMacroErrors checks that a macro this cannot translate is refused
// before anything is built.
func TestMacroErrors(t *testing.T) {
	path := write(t, "bad.C", `void bad() { TProfile2D *p = new TProfile2D("p", "", 1, 0, 1, 1, 0, 1); }`)

	var out bytes.Buffer
	g := &cint.GRoot{Stdout: &out, Stderr: &out}

	err := g.Macro(path)
	if err == nil {
		t.Fatal("a macro that cannot be translated was accepted")
	}
	if got := err.Error(); !strings.Contains(got, "TProfile2D") {
		t.Errorf("the message should name what it could not translate, got: %v", got)
	}
}
