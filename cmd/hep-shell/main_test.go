// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-hep.org/x/hep/cmd/internal/hepsh"
)

// run feeds the shell a session and returns everything it said.
func run(t *testing.T, session string) string {
	t.Helper()

	var out, errw bytes.Buffer
	sh, err := newShell(&out, &errw)
	if err != nil {
		t.Fatalf("could not start the shell: %+v", err)
	}
	sh.pipe = bufio.NewReader(strings.NewReader(session))

	sh.loop()

	return out.String() + errw.String()
}

// TestSession checks a session keeps what it was told between lines, which is
// the whole point of a prompt.
func TestSession(t *testing.T) {
	got := run(t, `h := hbook.NewH1D(10, 0, 10)
for i := 0; i < 100; i++ { h.Fill(float64(i%10)+0.5, 1) }
h.Entries()
h.XMean()
.q
`)

	for _, want := range []string{"(int64) 100", "(float64) 5"} {
		if !strings.Contains(got, want) {
			t.Errorf("session output does not contain %q:\n%s", want, got)
		}
	}
}

// TestOnlyExpressionsPrint checks a statement stays quiet and an expression
// does not, which is what makes the prompt readable.
func TestOnlyExpressionsPrint(t *testing.T) {
	for _, tc := range []struct {
		name  string
		src   string
		print bool
	}{
		{"short decl", `x := 40`, false},
		{"assignment", `y := 1
y = 2`, false},
		{"loop", `for i := 0; i < 3; i++ { _ = i }`, false},
		{"import", `import "strings"`, false},
		{"func decl", `func f() int { return 1 }`, false},
		{"expression", `40 + 2`, true},
		{"call", `math.Sqrt(16)`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := run(t, tc.src+"\n.q\n")
			if printed := strings.Contains(got, "("); printed != tc.print {
				t.Errorf("printed=%v, want=%v for %q:\n%s", printed, tc.print, tc.src, got)
			}
		})
	}
}

// TestValueAndType checks a printed value carries its type, as ROOT's does.
func TestValueAndType(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`40 + 2`, "(int) 42"},
		{`math.Sqrt(16)`, "(float64) 4"},
		{`"a" + "b"`, "(string) ab"},
		{`3 > 2`, "(bool) true"},
	} {
		got := run(t, tc.src+"\n.q\n")
		if !strings.Contains(got, tc.want) {
			t.Errorf("%q: want %q, got:\n%s", tc.src, tc.want, got)
		}
	}
}

// TestMultiLine checks a declaration spread over several lines is gathered up
// before it is run.
func TestMultiLine(t *testing.T) {
	got := run(t, `func twice(x float64) float64 {
	return 2 * x
}
twice(21)
.q
`)
	if !strings.Contains(got, "(float64) 42") {
		t.Errorf("multi-line declaration did not take:\n%s", got)
	}
}

// TestMacro checks .x runs a file the way ROOT runs a macro, package clause,
// imports and all.
func TestMacro(t *testing.T) {
	macro := filepath.Join(t.TempDir(), "macro.go")
	err := os.WriteFile(macro, []byte(`package main

import (
	"fmt"

	"go-hep.org/x/hep/hbook"
)

func mkhist(n int) *hbook.H1D {
	h := hbook.NewH1D(10, 0, 10)
	for i := 0; i < n; i++ {
		h.Fill(float64(i%10)+0.5, 1)
	}
	fmt.Printf("filled %d\n", n)
	return h
}
`), 0644)
	if err != nil {
		t.Fatalf("could not write the macro: %+v", err)
	}

	got := run(t, ".x "+macro+`
h := mkhist(42)
h.Entries()
.ls
.q
`)

	for _, want := range []string{"filled 42", "(int64) 42", "func mkhist", "  h"} {
		if !strings.Contains(got, want) {
			t.Errorf("macro session does not contain %q:\n%s", want, got)
		}
	}
}

// TestReset checks the session can be emptied and carries on working.
func TestReset(t *testing.T) {
	got := run(t, `x := 1
.reset
x
.q
`)
	if !strings.Contains(got, "session reset") {
		t.Errorf("no sign of the reset:\n%s", got)
	}
	if !strings.Contains(got, "undefined: x") {
		t.Errorf("x survived the reset:\n%s", got)
	}
}

// TestErrorsDoNotKillTheSession checks a mistake is reported and the next
// line still runs, which is what a prompt is for.
func TestErrorsDoNotKillTheSession(t *testing.T) {
	got := run(t, `nosuchthing()
40 + 2
.q
`)
	if !strings.Contains(got, "undefined: nosuchthing") {
		t.Errorf("the mistake was not reported:\n%s", got)
	}
	if !strings.Contains(got, "(int) 42") {
		t.Errorf("the session did not carry on:\n%s", got)
	}
}

// TestUnknownCommand checks a dot-command that is not one says so.
func TestUnknownCommand(t *testing.T) {
	got := run(t, ".nope\n.q\n")
	if !strings.Contains(got, `unknown command ".nope"`) {
		t.Errorf("unknown command not reported:\n%s", got)
	}
}

// TestGoHepIsLoaded checks every package the shell promises is there.
func TestGoHepIsLoaded(t *testing.T) {
	var b strings.Builder
	for _, tc := range []string{
		`hbook.NewH1D(2, 0, 1) != nil`,
		`minuit.New(1) != nil`,
		`hplot.New() != nil`,
	} {
		b.WriteString(tc + "\n")
	}
	b.WriteString(".q\n")

	got := run(t, b.String())
	if n := strings.Count(got, "(bool) true"); n != 3 {
		t.Errorf("got %d of 3 packages working:\n%s", n, got)
	}
}

// TestUnbalanced checks the shell can tell a half-typed line from a finished
// one, brackets inside strings and comments included.
func TestUnbalanced(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want bool
	}{
		{`x := 1`, false},
		{`func f() {`, true},
		{`func f() {}`, false},
		{`h.Fill(1,`, true},
		{`h.Fill(1, 2)`, false},
		{`s := "{"`, false},
		{`s := "}"`, false},
		{`c := '{'`, false},
		{`s := ` + "`{`", false},
		{`x := 1 // {`, false},
		{`x := 1 /* { */`, false},
		{`a := []int{1,`, true},
	} {
		if got := hepsh.Unbalanced(tc.src); got != tc.want {
			t.Errorf("unbalanced(%q): got=%v, want=%v", tc.src, got, tc.want)
		}
	}
}

// TestSplitProgram checks a whole Go file is taken apart into the imports it
// asks for and the declarations it makes.
func TestSplitProgram(t *testing.T) {
	src := `package main

import (
	"fmt"
	xx "strings"
)

import "math"

func f() {}
`
	body, imports := hepsh.SplitProgram(src)

	want := []string{"fmt", "strings", "math"}
	if len(imports) != len(want) {
		t.Fatalf("imports: got=%v, want=%v", imports, want)
	}
	for i := range want {
		if imports[i] != want[i] {
			t.Fatalf("imports: got=%v, want=%v", imports, want)
		}
	}

	if strings.Contains(body, "package main") || strings.Contains(body, "import") {
		t.Errorf("body still has the package clause or the imports:\n%s", body)
	}
	if !strings.Contains(body, "func f() {}") {
		t.Errorf("body lost the declaration:\n%s", body)
	}
}

// TestCINTMacro checks that a ROOT macro runs at the prompt, which is what
// ".x" does in a ROOT session.
//
// The macro is translated to Go and then run, so what the session ends up
// holding is Go: the function the macro defined is still there afterwards
// and can be called again, as it can in ROOT.
func TestCINTMacro(t *testing.T) {
	macro := filepath.Join(t.TempDir(), "counts.C")
	err := os.WriteFile(macro, []byte(`
// a macro in the shape ROOT's own are written in.
#include <TH1F.h>

void counts() {
   TH1D *h = new TH1D("h", "a title", 10, 0, 10);
   for (int i = 0; i < 100; i++) {
      h->Fill(i % 10);
   }
   printf("entries=%v mean=%v\n", h->GetEntries(), h->GetMean());
}
`), 0644)
	if err != nil {
		t.Fatalf("could not write the macro: %+v", err)
	}

	got := run(t, ".x "+macro+`
counts()
.ls
.q
`)

	for _, want := range []string{
		// a hundred entries over ten bins: the mean is 4.5.
		"entries=100 mean=4.5",
		// and calling it again runs it again.
		"func counts",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the session does not contain %q:\n%s", want, got)
		}
	}

	// it ran twice: once from .x and once from the call.
	if n := strings.Count(got, "entries=100 mean=4.5"); n != 2 {
		t.Errorf("the macro should have run twice, it ran %d time(s):\n%s", n, got)
	}
}

// TestCINTMacroErrorsAreReported checks that a macro this cannot translate
// says so at the prompt and leaves the session standing.
func TestCINTMacroErrorsAreReported(t *testing.T) {
	macro := filepath.Join(t.TempDir(), "bad.C")
	err := os.WriteFile(macro, []byte(`void bad() { gROOT->Reset(); }`), 0644)
	if err != nil {
		t.Fatalf("could not write the macro: %+v", err)
	}

	got := run(t, ".x "+macro+`
1+1
.q
`)

	if !strings.Contains(got, "gROOT") {
		t.Errorf("the session should say what it could not translate:\n%s", got)
	}
	if !strings.Contains(got, "(int) 2") {
		t.Errorf("the session should have carried on:\n%s", got)
	}
}
