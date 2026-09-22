// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rexpr_test

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"go-hep.org/x/hep/internal/rexpr"
)

// all evaluates an expression over a context and returns every value it
// yields, which is one per element of the collections it iterates over.
func all(t *testing.T, src string, ctx *rexpr.Ctx) []float64 {
	t.Helper()

	e, err := rexpr.New(src)
	if err != nil {
		t.Fatalf("could not compile %q: %+v", src, err)
	}

	n, err := e.N(ctx)
	if err != nil {
		t.Fatalf("could not size %q: %+v", src, err)
	}

	out := make([]float64, n)
	for i := range n {
		v, err := e.At(ctx, i)
		if err != nil {
			t.Fatalf("could not evaluate %q at %d: %+v", src, i, err)
		}
		out[i] = v
	}
	return out
}

// TestScalars covers the arithmetic an expression over single values is
// made of.
func TestScalars(t *testing.T) {
	vals := map[string]float64{"x": 3, "y": 4, "eta": -2.7}

	for _, tc := range []struct {
		expr string
		want float64
	}{
		{"1", 1},
		{"1 + 2*3", 7},
		{"(1+2)*3", 9},
		{"x", 3},
		{"x + y", 7},
		{"x*x + y*y", 25},
		{"sqrt(x*x + y*y)", 5},
		{"hypot(x, y)", 5},
		{"-x", -3},
		{"x > y", 0},
		{"x < y", 1},
		{"x == 3 && y == 4", 1},
		{"x == 3 && y == 5", 0},
		{"x == 9 || y == 4", 1},
		{"!(x > y)", 1},
		{"abs(eta) < 2.5", 0},
		{"TMath::Abs(eta) < 3", 1},
		{"pi > 3", 1},
		{"max(x, y)", 4},
		{"min(x, y, 1)", 1},
		{"pow(x, 2)", 9},
		{"int(2.7)", 2},
		{"fmod(7, 4)", 3},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			e, err := rexpr.New(tc.expr)
			if err != nil {
				t.Fatalf("could not compile: %+v", err)
			}
			got, err := e.Eval(vals)
			if err != nil {
				t.Fatalf("could not evaluate: %+v", err)
			}
			if math.Abs(got-tc.want) > 1e-12 {
				t.Errorf("got=%v, want=%v", got, tc.want)
			}
		})
	}
}

// TestCollections checks that an expression naming a collection is
// evaluated once per element, the way ROOT's Draw loops over an array
// branch.
func TestCollections(t *testing.T) {
	ctx := &rexpr.Ctx{
		Vals: map[string]rexpr.Value{
			"pt":  rexpr.Slice([]float64{10, 20, 30}),
			"eta": rexpr.Slice([]float64{0.5, -1.5, 2.5}),
			"w":   rexpr.Num(2),
		},
		Entry:   7,
		Entries: 100,
	}

	for _, tc := range []struct {
		expr string
		want []float64
	}{
		// one value per element.
		{"pt", []float64{10, 20, 30}},
		{"pt*2", []float64{20, 40, 60}},
		// a single value goes with every element of a collection.
		{"pt*w", []float64{20, 40, 60}},
		// two collections of the same length go element by element.
		{"pt + eta", []float64{10.5, 18.5, 32.5}},
		{"pt > 15", []float64{0, 1, 1}},
		// indexing picks one element and makes the whole thing single.
		{"pt[0]", []float64{10}},
		{"pt[2] - pt[0]", []float64{20}},
		// an indexed element still combines with a collection.
		{"pt - pt[0]", []float64{0, 10, 20}},

		// the reducers turn a collection into one value.
		{"Length$(pt)", []float64{3}},
		{"Sum$(pt)", []float64{60}},
		{"Min$(pt)", []float64{10}},
		{"Max$(pt)", []float64{30}},
		{"Sum$(pt) / Length$(pt)", []float64{20}},
		{"MaxIf$(pt, eta > 0)", []float64{30}},
		{"MinIf$(pt, eta > 0)", []float64{10}},
		// and so do not drive the loop around them.
		{"pt > Sum$(pt)/Length$(pt)", []float64{0, 0, 1}},

		// where the evaluation is.
		{"Entry$", []float64{7}},
		{"Entries$", []float64{100}},
		{"Iteration$ + pt*0", []float64{0, 1, 2}},

		// nothing at all names a collection: one value.
		{"w + 1", []float64{3}},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			got := all(t, tc.expr, ctx)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d value(s) %v, want %d %v", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if math.Abs(got[i]-tc.want[i]) > 1e-12 {
					t.Fatalf("got=%v, want=%v", got, tc.want)
				}
			}
		})
	}
}

// TestAlt checks that Alt$ pads a collection that has run out, which is how
// two collections of different lengths are put side by side on purpose.
func TestAlt(t *testing.T) {
	ctx := &rexpr.Ctx{
		Vals: map[string]rexpr.Value{
			"a": rexpr.Slice([]float64{1, 2, 3}),
			"b": rexpr.Slice([]float64{10}),
		},
	}

	if got, want := all(t, "a + Alt$(b, 0)", ctx), []float64{11, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("got=%v, want=%v", got, want)
	}
}

// TestEmptyCollection checks that an entry whose collection is empty yields
// nothing at all rather than one value of nothing.
func TestEmptyCollection(t *testing.T) {
	ctx := &rexpr.Ctx{
		Vals: map[string]rexpr.Value{
			"pt": rexpr.Slice(nil),
			"w":  rexpr.Num(1),
		},
	}

	if got := all(t, "pt*w", ctx); len(got) != 0 {
		t.Errorf("got %v, want nothing", got)
	}
	// but a reducer still answers for it.
	if got, want := all(t, "Length$(pt)", ctx), []float64{0}; !reflect.DeepEqual(got, want) {
		t.Errorf("Length$: got=%v, want=%v", got, want)
	}
	if got, want := all(t, "Sum$(pt)", ctx), []float64{0}; !reflect.DeepEqual(got, want) {
		t.Errorf("Sum$: got=%v, want=%v", got, want)
	}
}

// TestMismatchedCollections checks that two collections that do not line up
// are refused rather than quietly truncated or paired off.
func TestMismatchedCollections(t *testing.T) {
	ctx := &rexpr.Ctx{
		Vals: map[string]rexpr.Value{
			"a": rexpr.Slice([]float64{1, 2, 3}),
			"b": rexpr.Slice([]float64{1, 2}),
		},
	}

	e, err := rexpr.New("a + b")
	if err != nil {
		t.Fatalf("could not compile: %+v", err)
	}
	_, err = e.N(ctx)
	if err == nil {
		t.Fatal("putting a collection of 3 next to one of 2 was accepted")
	}
}

// TestOneElementCollection checks that a collection holding a single
// element is still treated as a collection, so that pairing it with a
// longer one is the same mistake however many elements it happens to hold.
func TestOneElementCollection(t *testing.T) {
	ctx := &rexpr.Ctx{
		Vals: map[string]rexpr.Value{
			"a": rexpr.Slice([]float64{1}),
			"b": rexpr.Slice([]float64{10, 20, 30}),
			"w": rexpr.Num(1),
		},
	}

	// on its own it yields its one element.
	if got, want := all(t, "a", ctx), []float64{1}; !reflect.DeepEqual(got, want) {
		t.Errorf("got=%v, want=%v", got, want)
	}
	// a single value still goes with any collection.
	if got, want := all(t, "b*w", ctx), []float64{10, 20, 30}; !reflect.DeepEqual(got, want) {
		t.Errorf("got=%v, want=%v", got, want)
	}
	// but a collection of one does not stretch to meet a longer one.
	e, err := rexpr.New("a + b")
	if err != nil {
		t.Fatalf("could not compile: %+v", err)
	}
	if _, err := e.N(ctx); err == nil {
		t.Fatal("a collection of 1 was stretched to meet one of 3")
	}
}

// TestIdents checks that the names an expression reads are reported, since
// that is what the caller binds to branches.
func TestIdents(t *testing.T) {
	for _, tc := range []struct {
		expr string
		want []string
	}{
		{"pt", []string{"pt"}},
		{"pt + eta", []string{"pt", "eta"}},
		{"pt + pt", []string{"pt"}},
		{"Sum$(pt) > 10", []string{"pt"}},
		{"pt[0]", []string{"pt"}},
		{"mu.pt", []string{"mu.pt"}},
		{"Entry$ > 5", nil},
		{"sqrt(x)", []string{"x"}},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			e, err := rexpr.New(tc.expr)
			if err != nil {
				t.Fatalf("could not compile: %+v", err)
			}
			if got := e.Idents(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got=%q, want=%q", got, tc.want)
			}
		})
	}
}

// TestErrors checks that an expression this package cannot evaluate is
// refused when it is compiled, not silently mis-evaluated later.
func TestErrors(t *testing.T) {
	for _, expr := range []string{
		"",
		"x +",
		"x ? 1 : 2",     // C++ has it, Go does not
		"nosuchfunc(x)", // not in the maths library
		"sqrt(1, 2)",    // wrong arity
		"Sum$()",        // wrong arity
		`"hello"`,       // not a number
		"x << 2",        // not arithmetic this evaluates
	} {
		t.Run(expr, func(t *testing.T) {
			_, err := rexpr.New(expr)
			if err == nil {
				t.Fatalf("%q was accepted", expr)
			}
		})
	}
}

// TestEvalErrors checks the failures that can only show up once there are
// values to evaluate against.
func TestEvalErrors(t *testing.T) {
	ctx := &rexpr.Ctx{
		Vals: map[string]rexpr.Value{
			"pt": rexpr.Slice([]float64{1, 2}),
			"w":  rexpr.Num(1),
		},
	}

	t.Run("no-such-branch", func(t *testing.T) {
		e, _ := rexpr.New("nope + 1")
		if _, err := e.N(ctx); err == nil {
			t.Fatal("a name that is not a branch was accepted")
		}
	})

	t.Run("index-past-the-end", func(t *testing.T) {
		e, _ := rexpr.New("pt[5]")
		if _, err := e.At(ctx, 0); err == nil {
			t.Fatal("indexing past the end was accepted")
		}
	})

	t.Run("index-a-single-value", func(t *testing.T) {
		e, _ := rexpr.New("w[0]")
		if _, err := e.At(ctx, 0); err == nil {
			t.Fatal("indexing a single value was accepted")
		}
	})
}

// TestDollarNamesRoundTrip checks that a name ending in "$", which the Go
// parser cannot hold, is still shown back as it was written.
func TestDollarNamesRoundTrip(t *testing.T) {
	const src = "Sum$(pt) > 10"

	e, err := rexpr.New(src)
	if err != nil {
		t.Fatalf("could not compile: %+v", err)
	}
	if got, want := e.String(), src; got != want {
		t.Errorf("String: got=%q, want=%q", got, want)
	}

	// the names it reads come back with their "$" too, or rather, the
	// special ones are not branch names at all and do not come back.
	e, err = rexpr.New("Sum$(pt) + Entry$")
	if err != nil {
		t.Fatalf("could not compile: %+v", err)
	}
	if got, want := e.Idents(), []string{"pt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Idents: got=%q, want=%q", got, want)
	}

	// an unknown one is refused, and named as it was written.
	_, err = rexpr.New("Nope$(pt)")
	if err == nil {
		t.Fatal("an unknown special name was accepted")
	}
	if got := err.Error(); !strings.Contains(got, "Nope$") {
		t.Errorf("error should name %q as written, got: %v", "Nope$", got)
	}
}
