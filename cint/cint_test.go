// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cint_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-hep.org/x/hep/cint"
)

// body returns the translation with its header comment taken off, so that a
// test can say what it expects without repeating the banner.
func body(t *testing.T, src string, opts ...cint.Option) string {
	t.Helper()

	out, err := cint.Translate([]byte(src), opts...)
	if err != nil {
		t.Fatalf("could not translate:\n%s\nerror: %+v", src, err)
	}

	s := string(out)
	i := strings.Index(s, "package ")
	if i < 0 {
		t.Fatalf("the translation has no package clause:\n%s", s)
	}
	return s[i:]
}

// TestTranslate checks whole macros against the Go they should become.
func TestTranslate(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "arithmetic",
			src: `
void m() {
   int    n = 3;
   double x = 1.5;
   double y = x * n + 1;
   printf("%v\n", y);
}
`,
			// n is a whole number and x is a real, and C++ widened the
			// one to meet the other without saying so. Go will not, so
			// the conversion is written out.
			want: `package main

import (
	"fmt"
)

func m() {
	var n int = 3
	var x float64 = 1.5
	var y float64 = x*float64(n) + 1
	fmt.Printf("%v\n", y)
}

func main() {
	m()
}
`,
		},
		{
			name: "histogram",
			src: `
void m() {
   TH1F *h = new TH1F("h", "t", 10, 0, 1);
   h->Fill(0.5);
   h->Fill(0.5, 2);
   printf("%v\n", h->GetEntries());
}
`,
			// ROOT lets the weight be left out and go-hep does not, so
			// the one C++ implied is written in.
			want: `package main

import (
	"fmt"

	"go-hep.org/x/hep/groot/rhist"
)

func m() {
	var h *rhist.H1F = rhist.NewH1F("h", "t", 10, 0, 1)
	h.Fill(0.5, 1)
	h.Fill(0.5, 2)
	fmt.Printf("%v\n", h.Entries())
}

func main() {
	m()
}
`,
		},
		{
			name: "loop-over-a-histogram",
			src: `
void m() {
   TH1D *h = new TH1D("h", "", 10, 0, 10);
   for (int i = 0; i < 10; i++) {
      h->SetBinContent(i+1, i);
   }
}
`,
			// the bin number stays whole and the content becomes real,
			// which is what the two arguments are.
			want: `package main

import (
	"go-hep.org/x/hep/groot/rhist"
)

func m() {
	var h *rhist.H1D = rhist.NewH1D("h", "", 10, 0, 10)
	for i := 0; i < 10; i++ {
		h.SetBinContent(i+1, float64(i))
	}
}

func main() {
	m()
}
`,
		},
		{
			name: "control-flow",
			src: `
void m(int n) {
   int k = 0;
   while (n > 0) { n--; k++; }
   do { k--; } while (k > 0);
   if (k == 0) { k = 1; } else { k = 2; }
}
`,
			// a do-while runs its body before it asks, which Go says
			// with a loop that breaks at the bottom.
			want: `package main

func m(n int) {
	var k int = 0
	for n > 0 {
		n--
		k++
	}
	for {
		k--
		if !(k > 0) {
			break
		}
	}
	if k == 0 {
		k = 1
	} else {
		k = 2
	}
}

func main() {
	m(0)
}
`,
		},
		{
			name: "vector",
			src: `
void m() {
   std::vector<double> xs;
   for (int i = 0; i < 3; i++) {
      xs.push_back(i);
   }
   printf("%v %v\n", xs.size(), xs.at(0));
}
`,
			// a vector is a slice, and the calls that change it are
			// assignments rather than methods.
			want: `package main

import (
	"fmt"
)

func m() {
	var xs []float64
	for i := 0; i < 3; i++ {
		xs = append(xs, float64(i))
	}
	fmt.Printf("%v %v\n", len(xs), xs[0])
}

func main() {
	m()
}
`,
		},
		{
			name: "struct",
			src: `
struct Point { double x; double y; };

void m() {
   Point p;
   p.x = 1;
   p.y = 2;
}
`,
			// a struct's members are exported, since the Go around the
			// translation is meant to be able to reach them.
			want: `package main

type Point struct {
	X float64
	Y float64
}

func m() {
	var p Point
	p.X = 1
	p.Y = 2
}

func main() {
	m()
}
`,
		},
		{
			name: "cout",
			src: `
void m() {
   int n = 2;
   cout << "n is " << n << endl;
}
`,
			want: `package main

import (
	"fmt"
)

func m() {
	var n int = 2
	fmt.Println("n is ", n)
}

func main() {
	m()
}
`,
		},
		{
			name: "tmath",
			src: `
void m() {
   double x = TMath::Sqrt(2);
   double y = TMath::Abs(-1) + TMath::Pi();
   printf("%v %v\n", x, y);
}
`,
			want: `package main

import (
	"fmt"
	"math"
)

func m() {
	var x float64 = math.Sqrt(2)
	var y float64 = math.Abs(-1) + math.Pi
	fmt.Printf("%v %v\n", x, y)
}

func main() {
	m()
}
`,
		},
		{
			name: "switch-and-range",
			src: `
void m() {
   std::vector<int> xs;
   xs.push_back(1);
   for (auto x : xs) {
      switch (x) {
         case 1: printf("one\n"); break;
         default: printf("other\n");
      }
   }
}
`,
			// the break that ends a C++ case says nothing in Go, where
			// a case does not fall through, so it is dropped.
			want: `package main

import (
	"fmt"
)

func m() {
	var xs []int
	xs = append(xs, 1)
	for _, x := range xs {
		switch x {
		case 1:
			fmt.Printf("one\n")
		default:
			fmt.Printf("other\n")
		}
	}
}

func main() {
	m()
}
`,
		},
		{
			name: "two-functions",
			src: `
double square(double x) { return x*x; }

void m() {
   printf("%v\n", square(3));
}
`,
			// the entry point is the function named after the file, and
			// with no file to go by it is the first one defined.
			want: `package main

import (
	"fmt"
)

func square(x float64) float64 {
	return x * x
}

func m() {
	fmt.Printf("%v\n", square(3))
}

func main() {
	square(0)
}
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := body(t, tc.src)
			if got != tc.want {
				t.Errorf("translation differs:\n--- got ---\n%s\n--- want ---\n%s", got, tc.want)
			}
		})
	}
}

// TestEntryPoint checks that the function a macro is run by is the one
// named after the file, as ROOT decides it.
func TestEntryPoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "analysis.C")

	src := `
double helper(double x) { return x; }
void analysis() { helper(1); }
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	entry, err := cint.EntryOf(path)
	if err != nil {
		t.Fatalf("could not find the entry point: %+v", err)
	}
	if got, want := entry, "analysis"; got != want {
		t.Errorf("entry point: got=%q, want=%q", got, want)
	}

	out, err := cint.TranslateFile(path)
	if err != nil {
		t.Fatalf("could not translate: %+v", err)
	}
	if got, want := string(out), "func main() {\n\tanalysis()\n}"; !strings.Contains(got, want) {
		t.Errorf("main should call %q:\n%s", "analysis", got)
	}
}

// full returns the whole translation, banner and all, for the tests that
// are about what the banner says.
func full(t *testing.T, src string) string {
	t.Helper()

	out, err := cint.Translate([]byte(src))
	if err != nil {
		t.Fatalf("could not translate:\n%s\nerror: %+v", src, err)
	}
	return string(out)
}

// TestDefaultArguments checks that a macro's default arguments end up in the
// call main makes, since Go has nowhere else to put them.
func TestDefaultArguments(t *testing.T) {
	got := full(t, `void m(int n = 42, double x = 1.5) { printf("%v %v\n", n, x); }`)

	if want := "func main() {\n\tm(42, 1.5)\n}"; !strings.Contains(got, want) {
		t.Errorf("main should pass the defaults:\n%s", got)
	}
	if !strings.Contains(got, "NOTE: the default value") {
		t.Errorf("the translation should say the defaults were dropped:\n%s", got)
	}
}

// TestNotes checks that what a translation drops it says it dropped, rather
// than leaving it to be noticed.
func TestNotes(t *testing.T) {
	const src = `
void m() {
   TH1F *h = new TH1F("h", "", 10, 0, 1);
   h->SetLineColor(2);
}
`

	// the call is gone from the Go ...
	if got := body(t, src); strings.Contains(got, "SetLineColor") {
		t.Errorf("SetLineColor should have been dropped:\n%s", got)
	}
	// ... and the translation says at the top that it went.
	if got := full(t, src); !strings.Contains(got, "NOTE: TH1F::SetLineColor was dropped") {
		t.Errorf("the translation should say so:\n%s", got)
	}
}

// TestErrors checks that what cannot be translated is refused, with the line
// it was on, rather than turned into Go that does something else.
func TestErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string // part of the message
	}{
		{
			name: "unknown-class",
			src:  `void m() { TProfile2D *p = new TProfile2D("p", "", 1, 0, 1, 1, 0, 1); }`,
			want: "TProfile2D",
		},
		{
			name: "unknown-method",
			src: `void m() {
   TH1F *h = new TH1F("h", "", 10, 0, 1);
   h->Rebin(2);
}`,
			want: "TH1F::Rebin is not translated",
		},
		{
			name: "ternary",
			src:  `void m() { int x = 1; int y = x > 0 ? 1 : 2; }`,
			want: `no "?:"`,
		},
		{
			name: "assignment-in-an-expression",
			src:  `void m() { int x = 0; int y = (x = 2); }`,
			want: "an assignment is used for its value",
		},
		{
			name: "increment-in-an-expression",
			src:  `void m() { int x = 0; int y = x++; }`,
			want: "used for its value",
		},
		{
			name: "the-root-session",
			src:  `void m() { gROOT->Reset(); }`,
			want: "gROOT is the running ROOT session",
		},
		{
			name: "unknown-name",
			src:  `void m() { int x = nosuchthing; }`,
			want: `nothing is known about the name "nosuchthing"`,
		},
		{
			name: "too-many-arguments",
			src: `void m() {
   TH1F *h = new TH1F("h", "", 10, 0, 1);
   h->Fill(1, 2, 3);
}`,
			want: "takes at most",
		},
		{
			name: "a-function-with-no-body",
			src:  `void helper(int x); void m() { helper(1); }`,
			want: "declared but not defined",
		},
		{
			name: "unbalanced",
			src:  `void m() { int x = 1;`,
			want: "line",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cint.Translate([]byte(tc.src))
			if err == nil {
				t.Fatalf("this was accepted:\n%s", tc.src)
			}
			if got := err.Error(); !strings.Contains(got, tc.want) {
				t.Errorf("the message should mention %q, got: %v", tc.want, got)
			}
			if got := err.Error(); !strings.Contains(got, "line ") {
				t.Errorf("the message should say which line, got: %v", got)
			}
		})
	}
}

// TestIsMacro checks what this will take for a macro.
func TestIsMacro(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"hsimple.C", true},
		{"a/b/hsimple.C", true},
		{"hsimple.cxx", true},
		{"hsimple.cpp", true},
		{"hsimple.go", false},
		{"hsimple", false},
	} {
		if got := cint.IsMacro(tc.path); got != tc.want {
			t.Errorf("IsMacro(%q): got=%v, want=%v", tc.path, got, tc.want)
		}
	}
}

// TestComments checks that what a macro says in passing does not get in the
// way of reading it.
func TestComments(t *testing.T) {
	got := body(t, `
// a leading comment
#include <TH1F.h>
#define UNUSED 1

void m() {
   /* a block
      comment */
   int x = 1; // a trailing one
   printf("%v\n", x);
}
`)

	if !strings.Contains(got, "var x int = 1") {
		t.Errorf("the macro should have translated:\n%s", got)
	}
}

// TestPackageAndMain checks the options that decide what shape the Go comes
// out in.
func TestPackageAndMain(t *testing.T) {
	got := body(t, `void m() { printf("hi\n"); }`, cint.Package("macros"), cint.Main(false))

	if !strings.HasPrefix(got, "package macros\n") {
		t.Errorf("package clause: %s", got)
	}
	if strings.Contains(got, "func main") {
		t.Errorf("there should be no main:\n%s", got)
	}
}
