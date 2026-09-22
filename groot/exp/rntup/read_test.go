// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rntup_test

import (
	"math"
	"path/filepath"
	"reflect"
	"testing"

	"go-hep.org/x/hep/groot/exp/rntup"
)

// The files these tests read were written by ROOT and are checked against
// the values the macros that generated them wrote. See the README in the
// testdata directory.
func testfile(name string) string {
	return filepath.Join("..", "..", "testdata", "rntuple", name+".root")
}

func open(t *testing.T, file, name string) *rntup.Reader {
	t.Helper()
	r, err := rntup.Open(testfile(file), name)
	if err != nil {
		t.Fatalf("could not open %s: %+v", file, err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

// TestScalars reads the simplest RNTuple there is: two scalar fields, one
// int and one float, over ten entries.
func TestScalars(t *testing.T) {
	r := open(t, "test_int_float_rntuple_v1-0-0-0", "ntuple")

	if got, want := r.Entries(), uint64(10); got != want {
		t.Fatalf("entries: got=%d, want=%d", got, want)
	}

	var (
		i  int32
		f  float32
		n  int
		rv = []rntup.ReadVar{
			{Name: "one_integers", Value: &i},
			{Name: "two_floats", Value: &f},
		}
	)
	err := r.Read(rv, func(e uint64) error {
		// the macro counts down from 9 to 0, with the float a tenth of
		// ten times the int.
		var (
			wi = int32(9 - e)
			wf = float32(9-e) * 1.1
		)
		if i != wi {
			t.Errorf("entry %d: int got=%v, want=%v", e, i, wi)
		}
		if math.Abs(float64(f-wf)) > 1e-5 {
			t.Errorf("entry %d: float got=%v, want=%v", e, f, wf)
		}
		n++
		return nil
	})
	if err != nil {
		t.Fatalf("could not read: %+v", err)
	}
	if got, want := n, 10; got != want {
		t.Fatalf("read %d entries, want %d", got, want)
	}
}

// TestJagged reads collections whose length changes from entry to entry.
func TestJagged(t *testing.T) {
	r := open(t, "test_1jag_int_float_rntuple_v1-0-0-0", "ntuple")

	if got, want := r.Entries(), uint64(100); got != want {
		t.Fatalf("entries: got=%d, want=%d", got, want)
	}

	var (
		ints   []int32
		floats []float32
		rv     = []rntup.ReadVar{
			{Name: "one_v_integers", Value: &ints},
			{Name: "two_v_floats", Value: &floats},
		}
	)
	err := r.Read(rv, func(e uint64) error {
		// the macro counts down from 100, emptying both vectors every
		// tenth entry and appending one element after every fill.
		var (
			base = 100 - 10*int(e/10)
			n    = int(e % 10)
		)
		if got := len(ints); got != n {
			t.Fatalf("entry %d: got %d ints, want %d", e, got, n)
		}
		if got := len(floats); got != n {
			t.Fatalf("entry %d: got %d floats, want %d", e, got, n)
		}
		for k := range n {
			if got, want := ints[k], int32(base-k); got != want {
				t.Errorf("entry %d, int %d: got=%v, want=%v", e, k, got, want)
			}
			want := float32(base-k) / 10
			if got := floats[k]; math.Abs(float64(got-want)) > 1e-5 {
				t.Errorf("entry %d, float %d: got=%v, want=%v", e, k, got, want)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("could not read: %+v", err)
	}
}

// TestStrings reads an RNTuple holding strings, which are kept as a column
// of offsets into a column of characters.
func TestStrings(t *testing.T) {
	r := open(t, "ntpl001_staff_rntuple_v1-0-0-0", "Staff")

	if got, want := r.Entries(), uint64(3354); got != want {
		t.Fatalf("entries: got=%d, want=%d", got, want)
	}

	type row struct {
		Category                                    int32
		Flag                                        uint32
		Age, Service, Children, Grade, Step, Hrweek int32
		Cost                                        int32
		Division, Nation                            string
	}

	var (
		got row
		rv  = []rntup.ReadVar{
			{Name: "Category", Value: &got.Category},
			{Name: "Flag", Value: &got.Flag},
			{Name: "Age", Value: &got.Age},
			{Name: "Service", Value: &got.Service},
			{Name: "Children", Value: &got.Children},
			{Name: "Grade", Value: &got.Grade},
			{Name: "Step", Value: &got.Step},
			{Name: "Hrweek", Value: &got.Hrweek},
			{Name: "Cost", Value: &got.Cost},
			{Name: "Division", Value: &got.Division},
			{Name: "Nation", Value: &got.Nation},
		}
	)

	// the first three rows of ROOT's cernstaff.dat, which is what the
	// macro reads in, in the order it declares the fields.
	want := []row{
		{202, 15, 58, 28, 0, 10, 13, 40, 11975, "PS", "DE"},
		{530, 15, 63, 33, 0, 9, 13, 40, 10228, "EP", "CH"},
		{316, 15, 56, 31, 2, 9, 13, 40, 10730, "PS", "FR"},
	}

	err := r.ReadRange(rv, 0, 3, func(e uint64) error {
		if got != want[e] {
			t.Errorf("entry %d:\ngot= %+v\nwant=%+v", e, got, want[e])
		}
		return nil
	})
	if err != nil {
		t.Fatalf("could not read: %+v", err)
	}

	// every entry should have a two-letter nation and a non-empty division.
	err = r.Read(rv, func(e uint64) error {
		if len(got.Nation) != 2 {
			t.Fatalf("entry %d: nation %q is not two letters", e, got.Nation)
		}
		if got.Division == "" {
			t.Fatalf("entry %d: division is empty", e)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("could not read: %+v", err)
	}
}

// TestNestedStructs reads a struct holding a struct holding a struct with a
// collection in it, binding it to Go structs of the test's own making.
func TestNestedStructs(t *testing.T) {
	r := open(t, "test_nested_structs_rntuple_v1-0-0-0", "ntuple")

	type subSub struct {
		I int32
		V []int32
	}
	type sub struct {
		I            int32
		SubSubStruct subSub `rntup:"sub_sub_struct"`
	}
	type top struct {
		I         int32
		SubStruct sub `rntup:"sub_struct"`
	}

	var (
		got top
		rv  = []rntup.ReadVar{{Name: "my_struct", Value: &got}}
	)
	err := r.Read(rv, func(e uint64) error {
		i := int32(e)
		want := top{
			I: i,
			SubStruct: sub{
				I:            i + 1,
				SubSubStruct: subSub{I: i + 2, V: []int32{i, i + 1}},
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("entry %d:\ngot= %+v\nwant=%+v", e, got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("could not read: %+v", err)
	}
	if got, want := r.Entries(), uint64(10); got != want {
		t.Fatalf("entries: got=%d, want=%d", got, want)
	}
}

// TestSTLContainers reads the standard library types RNTuple supports:
// strings, vectors, arrays, vectors of vectors, tuples, pairs and variants,
// including a vector of variants.
func TestSTLContainers(t *testing.T) {
	r := open(t, "test_stl_containers_rntuple_v1-0-0-0", "ntuple")

	type pair struct {
		I int32  `rntup:"_0"`
		S string `rntup:"_1"`
	}

	var (
		s   string
		vi  []int32
		af  [3]float32
		vvi [][]int32
		vs  []string
		va  any
		vva []any
		tup pair
		prr pair
		lv  struct{ Pt, Eta, Phi, Mass float32 }
		rv  = []rntup.ReadVar{
			{Name: "string", Value: &s},
			{Name: "vector_int32", Value: &vi},
			{Name: "array_float", Value: &af},
			{Name: "vector_vector_int32", Value: &vvi},
			{Name: "vector_string", Value: &vs},
			{Name: "variant_int32_string", Value: &va},
			{Name: "vector_variant_int64_string", Value: &vva},
			{Name: "tuple_int32_string", Value: &tup},
			{Name: "pair_int32_string", Value: &prr},
			{Name: "lorentz_vector", Value: &lv},
		}
	)

	// the macro fills five entries, the n-th holding the numbers one to n
	// and their names.
	names := []string{"one", "two", "three", "four", "five"}
	// the variant alternates between the number and its name.
	wantVar := []any{int32(1), "two", "three", int32(4), int32(5)}
	// the vector of variants takes a name first, then the numbers.
	wantVecVar := []any{"one", int64(2), int64(3), int64(4), int64(5)}

	err := r.Read(rv, func(e uint64) error {
		n := int(e) + 1

		if got, want := s, names[e]; got != want {
			t.Errorf("entry %d: string got=%q, want=%q", e, got, want)
		}

		if got, want := len(vi), n; got != want {
			t.Fatalf("entry %d: vector_int32 has %d elements, want %d", e, got, want)
		}
		for k := range n {
			if got, want := vi[k], int32(k+1); got != want {
				t.Errorf("entry %d: vector_int32[%d] got=%v, want=%v", e, k, got, want)
			}
		}

		if got, want := af, [3]float32{float32(n), float32(n), float32(n)}; got != want {
			t.Errorf("entry %d: array_float got=%v, want=%v", e, got, want)
		}

		if got, want := len(vvi), n; got != want {
			t.Fatalf("entry %d: vector_vector_int32 has %d elements, want %d", e, got, want)
		}
		for k := range n {
			if got, want := vvi[k], []int32{int32(k + 1)}; !reflect.DeepEqual(got, want) {
				t.Errorf("entry %d: vector_vector_int32[%d] got=%v, want=%v", e, k, got, want)
			}
		}

		if got, want := vs, names[:n]; !reflect.DeepEqual(got, want) {
			t.Errorf("entry %d: vector_string got=%v, want=%v", e, got, want)
		}

		if got, want := va, wantVar[e]; got != want {
			t.Errorf("entry %d: variant got=%v (%T), want=%v (%T)", e, got, got, want, want)
		}
		if got, want := vva, wantVecVar[:n]; !reflect.DeepEqual(got, want) {
			t.Errorf("entry %d: vector of variants got=%v, want=%v", e, got, want)
		}

		if got, want := tup, (pair{int32(n), names[e]}); got != want {
			t.Errorf("entry %d: tuple got=%v, want=%v", e, got, want)
		}
		if got, want := prr, (pair{int32(n), names[e]}); got != want {
			t.Errorf("entry %d: pair got=%v, want=%v", e, got, want)
		}

		f := float32(n)
		if lv.Pt != f || lv.Eta != f || lv.Phi != f || lv.Mass != f {
			t.Errorf("entry %d: lorentz vector got=%+v, want all %v", e, lv, f)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("could not read: %+v", err)
	}
}

// TestClassInheritance reads classes that inherit from one and from two
// bases, which RNTuple holds as unnamed subfields.
func TestClassInheritance(t *testing.T) {
	r := open(t, "test_class_inheritance_rntuple_v1-0-0-1", "rntpl")

	rvars, err := rntup.NewReadVars(r)
	if err != nil {
		t.Fatalf("could not build read-vars: %+v", err)
	}
	if got, want := len(rvars), 4; got != want {
		t.Fatalf("got %d fields, want %d", got, want)
	}

	// the base of every one of them holds an int, a double and a vector,
	// all counting with the entry number.
	type base struct {
		A1 int32   `rntup:"base_a1"`
		A2 float64 `rntup:"base_a2"`
		A3 []int32 `rntup:"base_a3"`
	}
	type child struct {
		Base base    `rntup:"_0"`
		C1   int32   `rntup:"child_1"`
		C2   float64 `rntup:"child_2"`
	}

	var (
		got child
		rv  = []rntup.ReadVar{{Name: "child", Value: &got}}
	)
	type grandchild struct {
		Child child   `rntup:"_0"`
		G1    int32   `rntup:"grandchild_1"`
		G2    float64 `rntup:"grandchild_2"`
	}

	var gc grandchild
	rv = append(rv, rntup.ReadVar{Name: "grandchild", Value: &gc})

	err = r.Read(rv, func(e uint64) error {
		i := int32(e)
		want := child{
			Base: base{A1: i, A2: float64(e) / 10, A3: []int32{0, i, 2 * i}},
			C1:   2 * i,
			C2:   float64(20 * e),
		}

		// the grandchild carries the child, which carries the base.
		if got, want := gc.G1, 3*i; got != want {
			t.Errorf("entry %d: grandchild_1 got=%v, want=%v", e, got, want)
		}
		if got, want := gc.G2, float64(30*e); math.Abs(got-want) > 1e-9 {
			t.Errorf("entry %d: grandchild_2 got=%v, want=%v", e, got, want)
		}
		if got, want := gc.Child.Base.A1, i; got != want {
			t.Errorf("entry %d: grandchild's base_a1 got=%v, want=%v", e, got, want)
		}
		if got, want := gc.Child.C1, 2*i; got != want {
			t.Errorf("entry %d: grandchild's child_1 got=%v, want=%v", e, got, want)
		}
		if got.Base.A1 != want.Base.A1 || got.C1 != want.C1 {
			t.Errorf("entry %d:\ngot= %+v\nwant=%+v", e, got, want)
		}
		if math.Abs(got.Base.A2-want.Base.A2) > 1e-9 {
			t.Errorf("entry %d: base_a2 got=%v, want=%v", e, got.Base.A2, want.Base.A2)
		}
		if !reflect.DeepEqual(got.Base.A3, want.Base.A3) {
			t.Errorf("entry %d: base_a3 got=%v, want=%v", e, got.Base.A3, want.Base.A3)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("could not read: %+v", err)
	}
}

// TestSchemaExtension reads an RNTuple whose schema grew while it was being
// written: the second field appears after 200 entries and the third after
// 400. The entries before a field was added read as its zero value.
func TestSchemaExtension(t *testing.T) {
	r := open(t, "test_extension_columns_rntuple_v1-0-0-0", "ntuple")

	if got, want := r.Entries(), uint64(600); got != want {
		t.Fatalf("entries: got=%d, want=%d", got, want)
	}
	if r.NClusters() < 2 {
		t.Fatalf("expected the entries to be spread over several clusters, got %d", r.NClusters())
	}

	var (
		i  int32
		f  float32
		v  []int32
		rv = []rntup.ReadVar{
			{Name: "int_field", Value: &i},
			{Name: "float_field", Value: &f},
			{Name: "intvec_field", Value: &v},
		}
	)
	err := r.Read(rv, func(e uint64) error {
		var (
			wi = int32(e % 200)
			wf float32
			wv []int32
		)
		if e >= 200 {
			wf = float32(e%200) + 0.5
		}
		if e >= 400 {
			wv = []int32{int32(e % 200), int32(e%200) + 1}
		}

		if i != wi {
			t.Fatalf("entry %d: int got=%v, want=%v", e, i, wi)
		}
		if f != wf {
			t.Fatalf("entry %d: float got=%v, want=%v", e, f, wf)
		}
		if !reflect.DeepEqual(v, wv) && !(len(v) == 0 && len(wv) == 0) {
			t.Fatalf("entry %d: vector got=%v, want=%v", e, v, wv)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("could not read: %+v", err)
	}
}

// TestMultipleRepresentations reads a field written through two different
// column encodings, where which of them carries the data changes from one
// cluster to the next and the other is suppressed.
func TestMultipleRepresentations(t *testing.T) {
	r := open(t, "test_multiple_representations_rntuple_v1-0-0-0", "ntuple")

	if got, want := r.NClusters(), 3; got != want {
		t.Fatalf("clusters: got=%d, want=%d", got, want)
	}

	// the field has two representations: single and half precision. the
	// macro writes 1 through the first, 2 through the second and 3 through
	// the first again, committing a cluster in between.
	f := r.Schema().Lookup("real")
	if f == nil {
		t.Fatal("no field named \"real\"")
	}
	if got, want := len(f.Reps), 2; got != want {
		t.Fatalf("representations: got=%d, want=%d", got, want)
	}

	var (
		v  float32
		rv = []rntup.ReadVar{{Name: "real", Value: &v}}
	)
	err := r.Read(rv, func(e uint64) error {
		if got, want := v, float32(e+1); got != want {
			t.Errorf("entry %d: got=%v, want=%v", e, got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("could not read: %+v", err)
	}
}

// TestMultipleClusterGroups reads an RNTuple whose clusters are spread over
// several cluster groups, each with a page list of its own.
func TestMultipleClusterGroups(t *testing.T) {
	r := open(t, "test_multiple_cluster_groups_rntuple_v1-0-0-0", "ntuple")

	if got, want := r.Entries(), uint64(1000); got != want {
		t.Fatalf("entries: got=%d, want=%d", got, want)
	}
	if r.NClusters() < 3 {
		t.Fatalf("expected several clusters, got %d", r.NClusters())
	}

	var (
		one int32
		vec []int16
		rv  = []rntup.ReadVar{
			{Name: "one", Value: &one},
			{Name: "int_vector", Value: &vec},
		}
	)
	err := r.Read(rv, func(e uint64) error {
		if got, want := one, int32(e); got != want {
			t.Fatalf("entry %d: one got=%v, want=%v", e, got, want)
		}
		want := []int16{int16(e), int16(e + 1)}
		if !reflect.DeepEqual(vec, want) {
			t.Fatalf("entry %d: vector got=%v, want=%v", e, vec, want)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("could not read: %+v", err)
	}
}

// TestIndexMulticluster reads collections spread over several clusters, so
// that the offsets in the index columns have to be read relative to the
// cluster they are in.
func TestIndexMulticluster(t *testing.T) {
	r := open(t, "test_index_multicluster_rntuple_v1-0-0-0", "ntuple")

	if got, want := r.Entries(), uint64(200); got != want {
		t.Fatalf("entries: got=%d, want=%d", got, want)
	}
	if r.NClusters() < 2 {
		t.Fatalf("expected several clusters, got %d", r.NClusters())
	}

	var (
		vec []int16
		rv  = []rntup.ReadVar{{Name: "int_vector", Value: &vec}}
	)
	err := r.Read(rv, func(e uint64) error {
		// the macro runs twice over the numbers 0 to 99, offsetting the
		// second element by the pass number.
		var (
			j = int16(e / 100)
			i = int16(e % 100)
		)
		want := []int16{i, i + j}
		if !reflect.DeepEqual(vec, want) {
			t.Fatalf("entry %d: got=%v, want=%v", e, vec, want)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("could not read: %+v", err)
	}
}

// TestBit reads a boolean field, which RNTuple packs a bit to an entry.
func TestBit(t *testing.T) {
	r := open(t, "test_bit_rntuple_v1-0-0-0", "ntuple")

	want := []bool{true, false, false, true, false, false, true, false, false, true}

	var (
		b  bool
		rv = []rntup.ReadVar{{Name: "one_bit", Value: &b}}
	)
	err := r.Read(rv, func(e uint64) error {
		if got := b; got != want[e] {
			t.Errorf("entry %d: got=%v, want=%v", e, got, want[e])
		}
		return nil
	})
	if err != nil {
		t.Fatalf("could not read: %+v", err)
	}
}

// TestBitset reads a std::bitset, which RNTuple holds as a fixed-size array
// over a single bit column.
func TestBitset(t *testing.T) {
	r := open(t, "test_atomic_bitset_rntuple_v1-0-0-0", "ntuple")

	// the macro sets the bitset to 42, then ors in 0b1010101010101010,
	// then ands with 0b1100110011001100.
	want := []uint64{
		42,
		42 | 0b1010101010101010,
		(42 | 0b1010101010101010) & 0b1100110011001100,
	}

	var (
		n  int32
		bs [42]bool
		rv = []rntup.ReadVar{
			{Name: "atomic_int", Value: &n},
			{Name: "bitset", Value: &bs},
		}
	)
	err := r.Read(rv, func(e uint64) error {
		if got, want := n, int32(e+1); got != want {
			t.Errorf("entry %d: atomic int got=%v, want=%v", e, got, want)
		}
		// the bits come out least significant first.
		var got uint64
		for i, b := range bs {
			if b {
				got |= 1 << uint(i)
			}
		}
		if got != want[e] {
			t.Errorf("entry %d: bitset got=%b, want=%b", e, got, want[e])
		}
		return nil
	})
	if err != nil {
		t.Fatalf("could not read: %+v", err)
	}
}

// TestFloatTypes reads the lossy real column types: floats with the low
// mantissa bits dropped, and reals quantized into an integer over a range.
func TestFloatTypes(t *testing.T) {
	r := open(t, "test_float_types_rntuple_v1-0-0-0", "ntuple")

	var (
		t10, t16, t24, t31 float32
		q8, q32            float32
		rv                 = []rntup.ReadVar{
			{Name: "trunc10", Value: &t10},
			{Name: "trunc16", Value: &t16},
			{Name: "trunc24", Value: &t24},
			{Name: "trunc31", Value: &t31},
			{Name: "quant8", Value: &q8},
			{Name: "quant32", Value: &q32},
		}
	)

	// the first entry holds 1.23456789f in every field, whose bits the
	// macro spells out as 00111111100111100000011001010010. A truncated
	// column keeps that many of the leading bits and zeroes the rest, so
	// what comes back is exact and can be written down.
	const bits = 0b00111111100111100000011001010010

	err := r.ReadRange(rv, 0, 1, func(e uint64) error {
		const want = float64(float32(1.23456789))

		for _, tc := range []struct {
			name string
			n    uint
			got  float32
		}{
			{"trunc10", 10, t10},
			{"trunc16", 16, t16},
			{"trunc24", 24, t24},
			{"trunc31", 31, t31},
		} {
			want := math.Float32frombits(bits &^ (1<<(32-tc.n) - 1))
			if tc.got != want {
				t.Errorf("%s: got=%v, want=%v", tc.name, tc.got, want)
			}
		}

		// a quantized column spreads the range it was given over the
		// integers it has, so more bits land closer.
		if got := float64(q8); math.Abs(got-want) > 0.02 {
			t.Errorf("quant8 got=%v, want about %v", got, want)
		}
		if got := float64(q32); math.Abs(got-want) > 1e-6 {
			t.Errorf("quant32 got=%v, want=%v", got, want)
		}
		if math.Abs(float64(q32)-want) > math.Abs(float64(q8)-want) {
			t.Errorf("quant32 (%v) is further off than quant8 (%v)", q32, q8)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("could not read: %+v", err)
	}
}

// TestUncompressed reads an RNTuple whose envelopes and pages were written
// without compression.
func TestUncompressed(t *testing.T) {
	r := open(t, "rntviewer-testfile-uncomp-single-rntuple-v1-0-0-0", "Contributors")

	if got, want := r.Entries(), uint64(22); got != want {
		t.Fatalf("entries: got=%d, want=%d", got, want)
	}

	var (
		first, last string
		rv          = []rntup.ReadVar{
			{Name: "firstName", Value: &first},
			{Name: "lastName", Value: &last},
		}
		got []string
	)
	err := r.ReadRange(rv, 0, 3, func(e uint64) error {
		got = append(got, first+" "+last)
		return nil
	})
	if err != nil {
		t.Fatalf("could not read: %+v", err)
	}

	want := []string{"Jakob Blomer", "Philippe Canal", "Axel Naumann"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got=%q, want=%q", got, want)
	}
}

// TestLargeEntryCount reads an RNTuple of a hundred million entries, which
// no reader could hold in memory at once. Pages are found by searching, so
// reaching the last entry should not cost more than reaching the first.
func TestLargeEntryCount(t *testing.T) {
	r := open(t, "test_int_multicluster_rntuple_v1-0-0-0", "ntuple")

	if got, want := r.Entries(), uint64(100_000_000); got != want {
		t.Fatalf("entries: got=%d, want=%d", got, want)
	}

	// the macro writes fifty million entries holding 2, then fifty
	// million holding 1.
	var (
		v  int16
		rv = []rntup.ReadVar{{Name: "one_integers", Value: &v}}
	)
	for _, tc := range []struct {
		entry uint64
		want  int16
	}{
		{0, 2},
		{49_999_999, 2},
		{50_000_000, 1},
		{99_999_999, 1},
	} {
		err := r.ReadRange(rv, tc.entry, tc.entry+1, func(uint64) error { return nil })
		if err != nil {
			t.Fatalf("could not read entry %d: %+v", tc.entry, err)
		}
		if v != tc.want {
			t.Errorf("entry %d: got=%v, want=%v", tc.entry, v, tc.want)
		}
	}
}

// TestReadVarsInferred checks that the Go types the reader works out for a
// schema it has not been told about are the ones the C++ types call for.
func TestReadVarsInferred(t *testing.T) {
	r := open(t, "test_stl_containers_rntuple_v1-0-0-0", "ntuple")

	rvars, err := rntup.NewReadVars(r)
	if err != nil {
		t.Fatalf("could not build read-vars: %+v", err)
	}

	got := make(map[string]string, len(rvars))
	for _, rvar := range rvars {
		got[rvar.Name] = reflect.TypeOf(rvar.Value).Elem().String()
	}

	for _, tc := range []struct {
		name string
		typ  string
	}{
		{"string", "string"},
		{"vector_int32", "[]int32"},
		{"array_float", "[3]float32"},
		{"vector_vector_int32", "[][]int32"},
		{"vector_string", "[]string"},
		{"variant_int32_string", "interface {}"},
		{"vector_variant_int64_string", "[]interface {}"},
	} {
		if got, want := got[tc.name], tc.typ; got != want {
			t.Errorf("field %q: got=%v, want=%v", tc.name, got, want)
		}
	}
}

// TestErrors checks the reader says what is wrong rather than reading
// something wrong.
func TestErrors(t *testing.T) {
	r := open(t, "test_int_float_rntuple_v1-0-0-0", "ntuple")

	t.Run("no-such-field", func(t *testing.T) {
		var v int32
		err := r.Read([]rntup.ReadVar{{Name: "nope", Value: &v}}, func(uint64) error { return nil })
		if err == nil {
			t.Fatal("reading a field that does not exist was accepted")
		}
	})

	t.Run("not-a-pointer", func(t *testing.T) {
		err := r.Read([]rntup.ReadVar{{Name: "one_integers", Value: int32(0)}}, func(uint64) error { return nil })
		if err == nil {
			t.Fatal("binding a non-pointer was accepted")
		}
	})

	t.Run("incompatible-type", func(t *testing.T) {
		var v []string
		err := r.Read([]rntup.ReadVar{{Name: "one_integers", Value: &v}}, func(uint64) error { return nil })
		if err == nil {
			t.Fatal("binding an int field to a slice of strings was accepted")
		}
	})

	t.Run("range-past-the-end", func(t *testing.T) {
		var v int32
		rv := []rntup.ReadVar{{Name: "one_integers", Value: &v}}
		err := r.ReadRange(rv, 0, 1000, func(uint64) error { return nil })
		if err == nil {
			t.Fatal("reading past the last entry was accepted")
		}
	})

	t.Run("no-such-ntuple", func(t *testing.T) {
		_, err := rntup.Open(testfile("test_int_float_rntuple_v1-0-0-0"), "nope")
		if err == nil {
			t.Fatal("opening an RNTuple that does not exist was accepted")
		}
	})
}

// TestConvertingBind checks a field can be read into a Go type of the
// caller's choosing, so long as it lines up with what the field holds.
func TestConvertingBind(t *testing.T) {
	r := open(t, "test_int_float_rntuple_v1-0-0-0", "ntuple")

	// the field is an int32 on disk; ask for it as an int64.
	var (
		i  int64
		f  float64
		rv = []rntup.ReadVar{
			{Name: "one_integers", Value: &i},
			{Name: "two_floats", Value: &f},
		}
	)
	err := r.ReadRange(rv, 0, 1, func(uint64) error {
		if got, want := i, int64(9); got != want {
			t.Errorf("int got=%v, want=%v", got, want)
		}
		if got, want := f, 9.9; math.Abs(got-want) > 1e-5 {
			t.Errorf("float got=%v, want=%v", got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("could not read: %+v", err)
	}
}
