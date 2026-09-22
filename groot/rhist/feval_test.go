// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rhist

import (
	"math"
	"strings"
	"testing"

	"reflect"

	"go-hep.org/x/hep/groot/internal/rtests"
	"go-hep.org/x/hep/groot/rbytes"
	"go-hep.org/x/hep/groot/riofs"
	"go-hep.org/x/hep/groot/rtypes"
)

func TestFormulaEval(t *testing.T) {
	const tol = 1e-5

	for _, tc := range []struct {
		expr string
		pars []float64
		x    []float64
		want float64
	}{
		// arithmetic and precedence
		{expr: "1+2*3", want: 7},
		{expr: "(1+2)*3", want: 9},
		{expr: "2^3^2", want: 512}, // right-associative
		{expr: "-2^2", want: -4},   // unary minus binds looser than ^
		{expr: "2**3", want: 8},    // ROOT spells power both ways
		{expr: "7/2", want: 3.5},
		{expr: "7%3", want: 1},
		{expr: "1e-2", want: 0.01},
		{expr: "1.5e3", want: 1500},

		// variables
		{expr: "x", x: []float64{3}, want: 3},
		{expr: "x*x", x: []float64{4}, want: 16},
		{expr: "x+y+z+t", x: []float64{1, 2, 3, 4}, want: 10},
		{expr: "x[0]+x[1]", x: []float64{5, 6}, want: 11},

		// parameters
		{expr: "[0]+[1]*x", pars: []float64{2, 3}, x: []float64{4}, want: 14},
		{expr: "[2]", pars: []float64{0, 0, 9}, want: 9},

		// functions
		{expr: "sqrt(16)", want: 4},
		{expr: "abs(-3)", want: 3},
		{expr: "exp(0)", want: 1},
		{expr: "log(exp(2))", want: 2},
		{expr: "pow(2,10)", want: 1024},
		{expr: "max(1,5,3)", want: 5},
		{expr: "min(1,5,3)", want: 1},
		{expr: "atan2(1,1)", want: math.Pi / 4},
		{expr: "TMath::Sqrt(25)", want: 5},
		{expr: "TMath::Abs(-7)", want: 7},
		{expr: "pi", want: math.Pi},
		{expr: "pi()", want: math.Pi},
		{expr: "sin(pi()/2)", want: 1},
		{expr: "int(3.7)", want: 3},
		{expr: "sign(-2)", want: -1},

		// comparisons and logic
		{expr: "1<2", want: 1},
		{expr: "2<1", want: 0},
		{expr: "1<2 && 3>2", want: 1},
		{expr: "1>2 || 3>2", want: 1},
		{expr: "!0", want: 1},
		{expr: "x>0 ? 1 : -1", x: []float64{5}, want: 1},
		{expr: "x>0 ? 1 : -1", x: []float64{-5}, want: -1},

		// predefined shapes
		{expr: "gaus", pars: []float64{2, 0, 1}, x: []float64{0}, want: 2},
		{expr: "expo", pars: []float64{0, 1}, x: []float64{2}, want: math.Exp(2)},
		{expr: "pol2", pars: []float64{1, 2, 3}, x: []float64{2}, want: 1 + 4 + 12},
		{expr: "pol0", pars: []float64{7}, x: []float64{99}, want: 7},
		{expr: "gaus(0)+pol1(3)", pars: []float64{1, 0, 1, 10, 2}, x: []float64{0}, want: 1 + 10},

		// Landau, at its peak: the density is 0.180655 at 0.222782 below
		// the location, and the shape scales it by the height.
		{expr: "landau", pars: []float64{2, 0, 1}, x: []float64{-0.222782}, want: 2 * 0.180655},
		{expr: "landau(x)", x: []float64{-0.222782}, want: 0.180655},
		{expr: "TMath::Landau(x)", x: []float64{-0.222782}, want: 0.180655},
		// with a location and a scale, the density is divided by the scale.
		{expr: "landau(x,0,2)", x: []float64{-0.445564}, want: 0.180655 / 2},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			f, err := NewFormula("f", tc.expr)
			if err != nil {
				t.Fatalf("could not build formula: %+v", err)
			}
			if len(tc.pars) > 0 {
				f.SetParams(tc.pars)
			}

			got, err := f.Eval(tc.x...)
			if err != nil {
				t.Fatalf("could not evaluate: %+v", err)
			}
			if math.Abs(got-tc.want) > tol {
				t.Fatalf("got=%v, want=%v", got, tc.want)
			}
		})
	}
}

// TestFormulaErrors checks a formula groot cannot evaluate says so, rather
// than returning a number that is quietly wrong.
func TestFormulaErrors(t *testing.T) {
	for _, tc := range []struct {
		expr string
		want string
	}{
		{"1+", "unexpected"},
		{"(1+2", "expected ')'"},
		{"sqrt(", "unexpected"},
		{"sqrt(1,2)", `takes 1 argument(s), got 2`},
		{"nosuchfunc(1)", `unknown name "nosuchfunc"`},
		{"cheb3", `shape "cheb3" is not supported`},
		{"x $ 2", "unexpected character"},
		{"[0", "unclosed '['"},
		{"y[0]", "only x may be indexed"},
		{"x>0 ? 1", "expected ':'"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			_, err := NewFormula("f", tc.expr)
			if err == nil {
				t.Fatalf("expected an error for %q", tc.expr)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.want)
			}
		})
	}
}

func TestF1Eval(t *testing.T) {
	f, err := NewF1("f1", "[0]*x*x + [1]", -10, 10)
	if err != nil {
		t.Fatalf("could not build TF1: %+v", err)
	}

	if got, want := f.Name(), "f1"; got != want {
		t.Errorf("name: got=%q, want=%q", got, want)
	}
	if got, want := f.XMin(), -10.0; got != want {
		t.Errorf("xmin: got=%v, want=%v", got, want)
	}
	if got, want := f.XMax(), 10.0; got != want {
		t.Errorf("xmax: got=%v, want=%v", got, want)
	}
	if got, want := f.NPar(), 2; got != want {
		t.Errorf("npar: got=%d, want=%d", got, want)
	}

	err = f.SetParams([]float64{3, 1})
	if err != nil {
		t.Fatalf("could not set parameters: %+v", err)
	}

	fct, err := f.Func()
	if err != nil {
		t.Fatalf("could not compile: %+v", err)
	}
	for _, tc := range []struct{ x, want float64 }{
		{0, 1},
		{1, 4},
		{2, 13},
		{-2, 13},
	} {
		if got := fct(tc.x); got != tc.want {
			t.Errorf("f(%v): got=%v, want=%v", tc.x, got, tc.want)
		}
	}

	got, err := f.Eval(3)
	if err != nil {
		t.Fatalf("could not evaluate: %+v", err)
	}
	if want := 28.0; got != want {
		t.Errorf("f(3): got=%v, want=%v", got, want)
	}
}

// TestF1NamedParams checks a formula may name its parameters rather than
// number them, and that the names survive.
func TestF1NamedParams(t *testing.T) {
	f, err := NewF1("f", "[slope]*x + [offset]", 0, 1)
	if err != nil {
		t.Fatalf("could not build TF1: %+v", err)
	}
	if got, want := f.NPar(), 2; got != want {
		t.Fatalf("npar: got=%d, want=%d", got, want)
	}

	err = f.SetParams([]float64{2, 5})
	if err != nil {
		t.Fatalf("could not set parameters: %+v", err)
	}

	got, err := f.Eval(10)
	if err != nil {
		t.Fatalf("could not evaluate: %+v", err)
	}
	if want := 25.0; got != want {
		t.Fatalf("got=%v, want=%v", got, want)
	}

	if got, want := f.Formula().ParamNames(), []string{"slope", "offset"}; len(got) != len(want) {
		t.Fatalf("param names: got=%v, want=%v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("param names: got=%v, want=%v", got, want)
			}
		}
	}
}

// TestF1EvalFromROOTFile evaluates a TF1 that C++ ROOT wrote, which is the
// thing the evaluator exists for: the formula string, its parameter names and
// their fitted values all come from the file.
func TestF1EvalFromROOTFile(t *testing.T) {
	for _, fname := range []string{
		"../testdata/tformula.root",
		"../testdata/tformula-v14.root",
	} {
		t.Run(fname, func(t *testing.T) {
			f, err := riofs.Open(fname)
			if err != nil {
				t.Fatalf("could not open %q: %+v", fname, err)
			}
			defer f.Close()

			obj, err := f.Get("func1")
			if err != nil {
				t.Fatalf("could not read func1: %+v", err)
			}

			fct, ok := obj.(*F1)
			if !ok {
				t.Fatalf("func1 is a %T, want a *rhist.F1", obj)
			}

			// ROOT wrote f(x) = [p0] + [p1]*x with p0=10 and p1=20.
			if got, want := fct.Formula().Expr(), "[p0]+[p1]*x"; got != want {
				t.Fatalf("formula: got=%q, want=%q", got, want)
			}
			if got, want := fct.NPar(), 2; got != want {
				t.Fatalf("npar: got=%d, want=%d", got, want)
			}

			eval, err := fct.Func()
			if err != nil {
				t.Fatalf("could not compile func1: %+v", err)
			}

			for _, tc := range []struct{ x, want float64 }{
				{0, 10},
				{1, 30},
				{2, 50},
				{-1, -10},
				{0.5, 20},
			} {
				if got := eval(tc.x); got != tc.want {
					t.Errorf("func1(%v): got=%v, want=%v", tc.x, got, tc.want)
				}
			}
		})
	}
}

func TestF2F3Eval(t *testing.T) {
	f2, err := NewF2("f2", "[0]*x + [1]*y", -1, 1, -2, 2)
	if err != nil {
		t.Fatalf("could not build TF2: %+v", err)
	}
	if got, want := f2.Class(), "TF2"; got != want {
		t.Errorf("class: got=%q, want=%q", got, want)
	}
	if got, want := f2.NDim(), 2; got != want {
		t.Errorf("ndim: got=%d, want=%d", got, want)
	}
	if got, want := [2]float64{f2.YMin(), f2.YMax()}, [2]float64{-2, 2}; got != want {
		t.Errorf("y-range: got=%v, want=%v", got, want)
	}
	if err := f2.SetParams([]float64{2, 3}); err != nil {
		t.Fatalf("could not set parameters: %+v", err)
	}
	if got, err := f2.Eval(1, 2); err != nil || got != 8 {
		t.Fatalf("f2(1,2): got=%v (err=%v), want=8", got, err)
	}

	f3, err := NewF3("f3", "x*y*z", 0, 1, 0, 1, 0, 1)
	if err != nil {
		t.Fatalf("could not build TF3: %+v", err)
	}
	if got, want := f3.Class(), "TF3"; got != want {
		t.Errorf("class: got=%q, want=%q", got, want)
	}
	if got, want := f3.NDim(), 3; got != want {
		t.Errorf("ndim: got=%d, want=%d", got, want)
	}
	if got, want := [2]float64{f3.ZMin(), f3.ZMax()}, [2]float64{0, 1}; got != want {
		t.Errorf("z-range: got=%v, want=%v", got, want)
	}
	if got, err := f3.Eval(2, 3, 4); err != nil || got != 24 {
		t.Fatalf("f3(2,3,4): got=%v (err=%v), want=24", got, err)
	}
}

func TestF2F3RoundTrip(t *testing.T) {
	f2, err := NewF2("f2", "[0]*x+[1]*y", -1, 1, -2, 2)
	if err != nil {
		t.Fatalf("could not build TF2: %+v", err)
	}
	f3, err := NewF3("f3", "x+y+z", -1, 1, -2, 2, -3, 3)
	if err != nil {
		t.Fatalf("could not build TF3: %+v", err)
	}

	for _, tc := range []struct {
		name string
		want rtests.ROOTer
	}{
		{name: "TF2", want: f2},
		{name: "TF3", want: f3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wbuf := rbytes.NewWBuffer(nil, nil, 0, nil)
			_, err := tc.want.MarshalROOT(wbuf)
			if err != nil {
				t.Fatalf("could not marshal: %+v", err)
			}

			obj := rtypes.Factory.Get(tc.name)().Interface().(rtests.ROOTer)
			rbuf := rbytes.NewRBuffer(wbuf.Bytes(), nil, 0, nil)
			err = obj.UnmarshalROOT(rbuf)
			if err != nil {
				t.Fatalf("could not unmarshal: %+v", err)
			}

			rw := rbytes.NewWBuffer(nil, nil, 0, nil)
			_, err = obj.MarshalROOT(rw)
			if err != nil {
				t.Fatalf("could not re-marshal: %+v", err)
			}
			if !reflect.DeepEqual(rw.Bytes(), wbuf.Bytes()) {
				t.Fatalf("round-trip lost bytes")
			}
		})
	}
}
