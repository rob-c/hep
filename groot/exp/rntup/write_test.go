// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rntup_test

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go-hep.org/x/hep/groot/exp/rntup"
	"go-hep.org/x/hep/groot/rbytes"
	"go-hep.org/x/hep/groot/rdict"
	"go-hep.org/x/hep/groot/riofs"
)

// TestWriteRoundTrip writes an RNTuple of every kind of field this supports
// and reads it back.
func TestWriteRoundTrip(t *testing.T) {
	type lorentz struct {
		Pt   float32 `rntup:"pt"`
		Eta  float32 `rntup:"eta"`
		Phi  float32 `rntup:"phi"`
		Mass float32 `rntup:"mass"`
	}

	const n = 20
	path := filepath.Join(t.TempDir(), "out.root")

	var (
		b    bool
		i8   int8
		u8   uint8
		i16  int16
		u16  uint16
		i32  int32
		u32  uint32
		i64  int64
		u64  uint64
		f32  float32
		f64  float64
		str  string
		vec  []float64
		arr  [3]int32
		rec  lorentz
		recs []lorentz
	)

	wvars := []rntup.WriteVar{
		{Name: "b", Value: &b}, {Name: "i8", Value: &i8}, {Name: "u8", Value: &u8},
		{Name: "i16", Value: &i16}, {Name: "u16", Value: &u16},
		{Name: "i32", Value: &i32}, {Name: "u32", Value: &u32},
		{Name: "i64", Value: &i64}, {Name: "u64", Value: &u64},
		{Name: "f32", Value: &f32}, {Name: "f64", Value: &f64},
		{Name: "str", Value: &str}, {Name: "vec", Value: &vec},
		{Name: "arr", Value: &arr}, {Name: "rec", Value: &rec},
		{Name: "recs", Value: &recs},
	}

	// set everything from the entry number, so that what should come back
	// can be said in one place.
	set := func(k int) {
		b = k%2 == 0
		i8, u8 = int8(k), uint8(k)
		i16, u16 = int16(k*100), uint16(k*100)
		i32, u32 = int32(k*1000), uint32(k*1000)
		i64, u64 = int64(k)*1e12, uint64(k)*1e12
		f32, f64 = float32(k)*1.5, math.Sqrt(float64(k))
		str = fmt.Sprintf("row-%d", k)

		vec = vec[:0]
		for j := range k % 4 {
			vec = append(vec, float64(j))
		}
		arr = [3]int32{int32(k), int32(k * 2), int32(k * 3)}
		rec = lorentz{Pt: float32(k), Eta: float32(k) + 1, Phi: float32(k) + 2, Mass: float32(k) + 3}
		recs = recs[:0]
		for j := range k % 3 {
			recs = append(recs, lorentz{Pt: float32(j)})
		}
	}

	w, err := rntup.Create(path, "ntuple", wvars, rntup.ClusterSize(7))
	if err != nil {
		t.Fatalf("could not create: %+v", err)
	}
	for k := range n {
		set(k)
		if err := w.Write(); err != nil {
			t.Fatalf("could not write entry %d: %+v", k, err)
		}
	}
	if got, want := w.Entries(), uint64(n); got != want {
		t.Errorf("entries: got=%d, want=%d", got, want)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("could not close: %+v", err)
	}

	r, err := rntup.Open(path, "ntuple")
	if err != nil {
		t.Fatalf("could not open: %+v", err)
	}
	defer r.Close()

	if got, want := r.Entries(), uint64(n); got != want {
		t.Fatalf("entries: got=%d, want=%d", got, want)
	}
	// seven entries to a cluster over twenty entries is three clusters.
	if got, want := r.NClusters(), 3; got != want {
		t.Errorf("clusters: got=%d, want=%d", got, want)
	}
	if got, want := r.Writer(), "go-hep"; got != want {
		t.Errorf("writer: got=%q, want=%q", got, want)
	}

	var (
		gb    bool
		gi8   int8
		gu8   uint8
		gi16  int16
		gu16  uint16
		gi32  int32
		gu32  uint32
		gi64  int64
		gu64  uint64
		gf32  float32
		gf64  float64
		gstr  string
		gvec  []float64
		garr  [3]int32
		grec  lorentz
		grecs []lorentz
	)
	rvars := []rntup.ReadVar{
		{Name: "b", Value: &gb}, {Name: "i8", Value: &gi8}, {Name: "u8", Value: &gu8},
		{Name: "i16", Value: &gi16}, {Name: "u16", Value: &gu16},
		{Name: "i32", Value: &gi32}, {Name: "u32", Value: &gu32},
		{Name: "i64", Value: &gi64}, {Name: "u64", Value: &gu64},
		{Name: "f32", Value: &gf32}, {Name: "f64", Value: &gf64},
		{Name: "str", Value: &gstr}, {Name: "vec", Value: &gvec},
		{Name: "arr", Value: &garr}, {Name: "rec", Value: &grec},
		{Name: "recs", Value: &grecs},
	}

	err = r.Read(rvars, func(e uint64) error {
		k := int(e)
		set(k)

		for _, tc := range []struct {
			name      string
			got, want any
		}{
			{"b", gb, b}, {"i8", gi8, i8}, {"u8", gu8, u8},
			{"i16", gi16, i16}, {"u16", gu16, u16},
			{"i32", gi32, i32}, {"u32", gu32, u32},
			{"i64", gi64, i64}, {"u64", gu64, u64},
			{"f32", gf32, f32}, {"f64", gf64, f64},
			{"str", gstr, str}, {"arr", garr, arr}, {"rec", grec, rec},
		} {
			if !reflect.DeepEqual(tc.got, tc.want) {
				t.Fatalf("entry %d: %s got=%v, want=%v", e, tc.name, tc.got, tc.want)
			}
		}
		if len(gvec) != len(vec) || (len(vec) > 0 && !reflect.DeepEqual(gvec, vec)) {
			t.Fatalf("entry %d: vec got=%v, want=%v", e, gvec, vec)
		}
		if len(grecs) != len(recs) || (len(recs) > 0 && !reflect.DeepEqual(grecs, recs)) {
			t.Fatalf("entry %d: recs got=%v, want=%v", e, grecs, recs)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("could not read: %+v", err)
	}
}

// TestWriteCompression checks that compressing the pages makes the file
// smaller and changes nothing about what comes back out of it.
func TestWriteCompression(t *testing.T) {
	const n = 5000

	sizes := make(map[string]int64, 2)
	for _, tc := range []struct {
		name  string
		compr int32
	}{
		{"none", 0},
		{"zlib", 101},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "out.root")

			var x float64
			w, err := rntup.Create(path, "ntuple",
				[]rntup.WriteVar{{Name: "x", Value: &x}},
				rntup.Compression(tc.compr),
			)
			if err != nil {
				t.Fatalf("could not create: %+v", err)
			}
			for k := range n {
				x = float64(k)
				if err := w.Write(); err != nil {
					t.Fatalf("could not write: %+v", err)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatalf("could not close: %+v", err)
			}

			fi, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			sizes[tc.name] = fi.Size()

			r, err := rntup.Open(path, "ntuple")
			if err != nil {
				t.Fatalf("could not open: %+v", err)
			}
			defer r.Close()

			var got float64
			err = r.Read([]rntup.ReadVar{{Name: "x", Value: &got}}, func(e uint64) error {
				if want := float64(e); got != want {
					return fmt.Errorf("entry %d: got=%v, want=%v", e, got, want)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("could not read: %+v", err)
			}
		})
	}

	if sizes["zlib"] >= sizes["none"] {
		t.Errorf("compressing should have made it smaller: %d bytes against %d",
			sizes["zlib"], sizes["none"])
	}
}

// TestWriteSchema checks that the schema a writer builds says what the Go
// types mean in C++, which is what another reader goes by.
func TestWriteSchema(t *testing.T) {
	type point struct {
		X float64 `rntup:"x"`
		Y float64 `rntup:"y"`
	}

	var (
		n   int32
		s   string
		vec []float32
		p   point
	)
	path := filepath.Join(t.TempDir(), "out.root")

	w, err := rntup.Create(path, "ntuple", []rntup.WriteVar{
		{Name: "n", Value: &n},
		{Name: "s", Value: &s},
		{Name: "vec", Value: &vec},
		{Name: "p", Value: &p},
	})
	if err != nil {
		t.Fatalf("could not create: %+v", err)
	}
	if err := w.Write(); err != nil {
		t.Fatalf("could not write: %+v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("could not close: %+v", err)
	}

	r, err := rntup.Open(path, "ntuple")
	if err != nil {
		t.Fatalf("could not open: %+v", err)
	}
	defer r.Close()

	for _, tc := range []struct {
		field string
		typ   string
		role  rntup.StructRole
	}{
		{"n", "std::int32_t", rntup.RoleLeaf},
		{"s", "std::string", rntup.RoleLeaf},
		{"vec", "std::vector<float>", rntup.RoleCollection},
		{"p", "point", rntup.RoleRecord},
	} {
		f := r.Schema().Lookup(tc.field)
		if f == nil {
			t.Errorf("no field %q", tc.field)
			continue
		}
		if got, want := f.Type, tc.typ; got != want {
			t.Errorf("field %q: type got=%q, want=%q", tc.field, got, want)
		}
		if got, want := f.Role, tc.role; got != want {
			t.Errorf("field %q: role got=%v, want=%v", tc.field, got, want)
		}
	}

	// a record's members are named the way the tags said.
	p2 := r.Schema().Lookup("p")
	if p2 == nil || len(p2.Children) != 2 {
		t.Fatalf("the record should have two members, got %+v", p2)
	}
	for i, want := range []string{"x", "y"} {
		if got := r.Schema().Field(p2.Children[i]).Name; got != want {
			t.Errorf("member %d: got=%q, want=%q", i, got, want)
		}
	}
}

// TestWriteErrors checks what a writer refuses.
func TestWriteErrors(t *testing.T) {
	dir := t.TempDir()

	t.Run("no-fields", func(t *testing.T) {
		_, err := rntup.Create(filepath.Join(dir, "a.root"), "ntuple", nil)
		if err == nil {
			t.Fatal("an RNTuple with no fields was accepted")
		}
	})

	t.Run("not-a-pointer", func(t *testing.T) {
		_, err := rntup.Create(filepath.Join(dir, "b.root"), "ntuple",
			[]rntup.WriteVar{{Name: "x", Value: 1.0}})
		if err == nil {
			t.Fatal("a field bound to a value was accepted")
		}
	})

	t.Run("unnamed-field", func(t *testing.T) {
		var x float64
		_, err := rntup.Create(filepath.Join(dir, "c.root"), "ntuple",
			[]rntup.WriteVar{{Value: &x}})
		if err == nil {
			t.Fatal("a field with no name was accepted")
		}
	})

	t.Run("a-type-with-no-column", func(t *testing.T) {
		var m map[string]int
		_, err := rntup.Create(filepath.Join(dir, "d.root"), "ntuple",
			[]rntup.WriteVar{{Name: "m", Value: &m}})
		if err == nil {
			t.Fatal("a map was accepted")
		}
		if got := err.Error(); !strings.Contains(got, "map") {
			t.Errorf("the message should name the type, got: %v", got)
		}
	})

	t.Run("write-after-close", func(t *testing.T) {
		var x float64
		w, err := rntup.Create(filepath.Join(dir, "e.root"), "ntuple",
			[]rntup.WriteVar{{Name: "x", Value: &x}})
		if err != nil {
			t.Fatal(err)
		}
		if err := w.Write(); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if err := w.Write(); err == nil {
			t.Fatal("writing after close was accepted")
		}
		// and closing twice is not an error.
		if err := w.Close(); err != nil {
			t.Errorf("closing twice should be fine, got: %+v", err)
		}
	})
}

// TestWriteEmpty checks that an RNTuple nothing was written to is still a
// readable one holding no entries.
func TestWriteEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.root")

	var x float64
	w, err := rntup.Create(path, "ntuple", []rntup.WriteVar{{Name: "x", Value: &x}})
	if err != nil {
		t.Fatalf("could not create: %+v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("could not close: %+v", err)
	}

	r, err := rntup.Open(path, "ntuple")
	if err != nil {
		t.Fatalf("could not open: %+v", err)
	}
	defer r.Close()

	if got, want := r.Entries(), uint64(0); got != want {
		t.Errorf("entries: got=%d, want=%d", got, want)
	}

	n := 0
	err = r.Read([]rntup.ReadVar{{Name: "x", Value: &x}}, func(uint64) error {
		n++
		return nil
	})
	if err != nil {
		t.Fatalf("could not read: %+v", err)
	}
	if n != 0 {
		t.Errorf("read %d entries from an empty RNTuple", n)
	}
}

// ---------------------------------------------------------------- uproot

// uprootPython returns a python that can import uproot, which is another
// implementation of this format and so is worth checking against: reading
// back what this wrote with this proves the two halves agree with each
// other, and nothing more.
func uprootPython(t *testing.T) string {
	t.Helper()

	for _, py := range []string{
		os.Getenv("HEP_UPROOT_PYTHON"),
		"python3",
		"python",
	} {
		if py == "" {
			continue
		}
		exe, err := exec.LookPath(py)
		if err != nil {
			continue
		}
		if err := exec.Command(exe, "-c", "import uproot").Run(); err == nil {
			return exe
		}
	}

	t.Skip("skipping: no python with uproot to cross-check against")
	return ""
}

// TestWriteReadableByUproot writes an RNTuple and has uproot read it.
//
// This is the check that matters: uproot is an implementation of the format
// that owes nothing to this one, so agreeing with it says the bytes are
// right rather than merely self-consistent.
func TestWriteReadableByUproot(t *testing.T) {
	py := uprootPython(t)

	const n = 1000
	dir := t.TempDir()
	path := filepath.Join(dir, "out.root")

	type point struct {
		X float64 `rntup:"x"`
		Y float64 `rntup:"y"`
	}

	var (
		i32 int32
		f64 float64
		ok  bool
		str string
		vec []float64
		pt  point
	)
	w, err := rntup.Create(path, "ntuple", []rntup.WriteVar{
		{Name: "i32", Value: &i32},
		{Name: "f64", Value: &f64},
		{Name: "ok", Value: &ok},
		{Name: "str", Value: &str},
		{Name: "vec", Value: &vec},
		{Name: "pt", Value: &pt},
	}, rntup.ClusterSize(400))
	if err != nil {
		t.Fatalf("could not create: %+v", err)
	}
	for k := range n {
		i32 = int32(k)
		f64 = math.Sqrt(float64(k))
		ok = k%2 == 0
		str = fmt.Sprintf("row-%d", k)
		vec = vec[:0]
		for j := range k % 4 {
			vec = append(vec, float64(j))
		}
		pt = point{X: float64(k), Y: float64(k) * 2}
		if err := w.Write(); err != nil {
			t.Fatalf("could not write: %+v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("could not close: %+v", err)
	}

	script := filepath.Join(dir, "check.py")
	err = os.WriteFile(script, []byte(`
import math, sys
import uproot

nt = uproot.open(sys.argv[1])["ntuple"]
n = int(sys.argv[2])
assert nt.num_entries == n, f"entries {nt.num_entries} != {n}"

a = nt.arrays()
for k in range(n):
    r = a[k]
    assert r.i32 == k, f"i32 at {k}: {r.i32}"
    assert abs(r.f64 - math.sqrt(k)) < 1e-12, f"f64 at {k}: {r.f64}"
    assert bool(r.ok) == (k % 2 == 0), f"ok at {k}: {r.ok}"
    assert r.str == f"row-{k}", f"str at {k}: {r.str}"
    assert list(r.vec) == [float(j) for j in range(k % 4)], f"vec at {k}: {list(r.vec)}"
    assert r.pt.x == k and r.pt.y == 2 * k, f"pt at {k}: {r.pt}"

print("ok")
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command(py, script, path, fmt.Sprint(n)).CombinedOutput()
	if err != nil {
		t.Fatalf("uproot could not read what was written: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "ok" {
		t.Errorf("uproot said: %s", got)
	}
}

// TestAnchorStreamerMatchesROOT checks that the streamer this registers for
// the anchor is the one ROOT writes.
//
// The anchor is the only part of an RNTuple written as an ordinary ROOT
// object, so it is the only part that needs a streamer, and a file is no
// good to ROOT without the right one. The comparison is against a file ROOT
// itself wrote: the members, their types, the class version and the checksum
// ROOT computed from them all have to agree.
func TestAnchorStreamerMatchesROOT(t *testing.T) {
	f, err := riofs.Open(testfile("test_int_float_rntuple_v1-0-0-0"))
	if err != nil {
		t.Fatalf("could not open: %+v", err)
	}
	defer f.Close()

	var want rbytes.StreamerInfo
	for _, si := range f.StreamerInfos() {
		if si.Name() == "ROOT::RNTuple" {
			want = si
			break
		}
	}
	if want == nil {
		t.Fatal("the file ROOT wrote holds no streamer for ROOT::RNTuple")
	}

	got, err := rdict.StreamerInfos.StreamerInfo("ROOT::RNTuple", -1)
	if err != nil {
		t.Fatalf("no streamer is registered for ROOT::RNTuple: %+v", err)
	}

	if got, want := got.ClassVersion(), want.ClassVersion(); got != want {
		t.Errorf("class version: got=%d, want=%d", got, want)
	}
	if got, want := got.CheckSum(), want.CheckSum(); got != want {
		t.Errorf("checksum: got=%#x, want=%#x", got, want)
	}

	var (
		gels = got.Elements()
		wels = want.Elements()
	)
	if len(gels) != len(wels) {
		t.Fatalf("members: got %d, want %d", len(gels), len(wels))
	}
	for i := range wels {
		if got, want := gels[i].Name(), wels[i].Name(); got != want {
			t.Errorf("member %d: name got=%q, want=%q", i, got, want)
		}
		if got, want := gels[i].Type(), wels[i].Type(); got != want {
			t.Errorf("member %q: type got=%v, want=%v", wels[i].Name(), got, want)
		}
		if got, want := gels[i].Size(), wels[i].Size(); got != want {
			t.Errorf("member %q: size got=%d, want=%d", wels[i].Name(), got, want)
		}
	}
}
