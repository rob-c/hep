// Copyright ©2017 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lcio_test

import (
	"compress/flate"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go-hep.org/x/hep/lcio"
)

func ExampleWriter() {
	dir, err := os.MkdirTemp("", "lcio-example-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	w, err := lcio.Create(filepath.Join(dir, "out.slcio"))
	if err != nil {
		log.Fatal(err)
	}
	defer w.Close()

	run := lcio.RunHeader{
		RunNumber:    42,
		Descr:        "a simple run header",
		Detector:     "my detector",
		SubDetectors: []string{"det-1", "det-2"},
		Params: lcio.Params{
			Floats: map[string][]float32{
				"floats-1": {1, 2, 3},
				"floats-2": {4, 5, 6},
			},
		},
	}

	err = w.WriteRunHeader(&run)
	if err != nil {
		log.Fatal(err)
	}

	const NEVENTS = 1
	for ievt := range NEVENTS {
		evt := lcio.Event{
			RunNumber:   run.RunNumber,
			Detector:    run.Detector,
			EventNumber: 52 + int32(ievt),
			TimeStamp:   1234567890 + int64(ievt),
			Params: lcio.Params{
				Floats: map[string][]float32{
					"_weight": {42},
				},
				Strings: map[string][]string{
					"Descr": {"a description"},
				},
			},
		}

		calhits := lcio.CalorimeterHitContainer{
			Flags: lcio.BitsRChLong | lcio.BitsRChID1 | lcio.BitsRChTime | lcio.BitsRChNoPtr | lcio.BitsRChEnergyError,
			Params: lcio.Params{
				Floats:  map[string][]float32{"f32": {1, 2, 3}},
				Ints:    map[string][]int32{"i32": {1, 2, 3}},
				Strings: map[string][]string{"str": {"1", "2", "3"}},
			},
			Hits: []lcio.CalorimeterHit{
				{
					CellID0:   1024,
					CellID1:   2048,
					Energy:    1000,
					EnergyErr: 0.1,
					Time:      1234,
					Pos:       [3]float32{11, 22, 33},
					Type:      42,
				},
			},
		}

		evt.Add("CaloHits", &calhits)

		fmt.Printf("evt has key %q: %v\n", "CaloHits", evt.Has("CaloHits"))

		err = w.WriteEvent(&evt)
		if err != nil {
			log.Fatal(err)
		}
	}

	err = w.Close()
	if err != nil {
		log.Fatal(err)
	}

	// Output:
	// evt has key "CaloHits": true
}

func TestCreateRunHeader(t *testing.T) {
	testCreateRunHeader(t, flate.NoCompression, "testdata/run-header.slcio")
}

func TestCreateCompressedRunHeader(t *testing.T) {
	testCreateRunHeader(t, flate.BestCompression, "testdata/run-header-compressed.slcio")
}

func TestCreateEvent(t *testing.T) {
	testCreateEvent(t, flate.NoCompression, "testdata/event.slcio")
}

func TestCreateCompressedEvent(t *testing.T) {
	testCreateEvent(t, flate.BestCompression, "testdata/event-compressed.slcio")
}

func testCreateRunHeader(t *testing.T, compLevel int, fname string) {
	w, err := lcio.Create(fname)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	w.SetCompressionLevel(compLevel)

	rhdr := lcio.RunHeader{
		RunNumber:    42,
		Descr:        "a simple run header",
		Detector:     "my detector",
		SubDetectors: []string{"det-1", "det-2"},
		Params: lcio.Params{
			Floats: map[string][]float32{
				"floats-1": {1, 2, 3},
				"floats-2": {4, 5, 6},
			},
			Ints: map[string][]int32{
				"ints-1": {1, 2, 3},
				"ints-2": {4, 5, 6},
			},
			Strings: map[string][]string{
				"strs-1": {"1", "2", "3"},
			},
		},
	}

	err = w.WriteRunHeader(&rhdr)
	if err != nil {
		t.Error(err)
		return
	}

	err = w.Close()
	if err != nil {
		t.Error(err)
		return
	}

	if !sameAsGolden(t, fname, compLevel) {
		return
	}

	os.Remove(fname)
}

// sameAsGolden checks a written file against the one kept beside it.
//
// An uncompressed file is compared byte for byte: every one of them is this
// package's to decide, so any difference is a change in the format it writes.
//
// A compressed one cannot be. The bytes a deflate compressor produces are
// not part of Go's compatibility promise and do change between releases of
// it, while what they uncompress to does not. So the two files are read back
// instead and what they hold is compared, which is the part this package is
// answerable for.
func sameAsGolden(t *testing.T, fname string, compLevel int) bool {
	t.Helper()

	golden := strings.Replace(fname, ".slcio", "_golden.slcio", -1)

	if compLevel == flate.NoCompression {
		chk, err := os.ReadFile(fname)
		if err != nil {
			t.Error(err)
			return false
		}
		ref, err := os.ReadFile(golden)
		if err != nil {
			t.Error(err)
			return false
		}
		if !reflect.DeepEqual(ref, chk) {
			t.Errorf("%s: --- ref ---\n%s\n", fname, hex.Dump(ref))
			t.Errorf("%s: --- chk ---\n%s\n", fname, hex.Dump(chk))
			t.Errorf("%s: differ with golden", fname)
			return false
		}
		return true
	}

	chk, err := readAll(fname)
	if err != nil {
		t.Errorf("could not read back %s: %+v", fname, err)
		return false
	}
	ref, err := readAll(golden)
	if err != nil {
		t.Errorf("could not read %s: %+v", golden, err)
		return false
	}

	if len(chk) != len(ref) {
		t.Errorf("%s holds %d record(s), %s holds %d", fname, len(chk), golden, len(ref))
		return false
	}
	for i := range ref {
		if got, want := chk[i].hdr, ref[i].hdr; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: record %d: run headers differ:\ngot= %#v\nwant=%#v", fname, i, got, want)
			return false
		}
		var (
			got  = chk[i].evt
			want = ref[i].evt
		)
		for _, tc := range []struct {
			what      string
			got, want any
		}{
			{"run number", got.RunNumber, want.RunNumber},
			{"event number", got.EventNumber, want.EventNumber},
			{"time stamp", got.TimeStamp, want.TimeStamp},
			{"detector", got.Detector, want.Detector},
			{"parameters", got.Params, want.Params},
		} {
			if !reflect.DeepEqual(tc.got, tc.want) {
				t.Errorf("%s: record %d: %s differs: got=%v, want=%v",
					fname, i, tc.what, tc.got, tc.want)
				return false
			}
		}
		if g, w := got.Names(), want.Names(); !reflect.DeepEqual(g, w) {
			t.Errorf("%s: record %d: collections differ: got=%q, want=%q", fname, i, g, w)
			return false
		}
		for _, name := range want.Names() {
			if g, w := got.Get(name), want.Get(name); !reflect.DeepEqual(g, w) {
				t.Errorf("%s: record %d: collection %q differs:\ngot= %v\nwant=%v",
					fname, i, name, g, w)
				return false
			}
		}
	}
	return true
}

// record is one run header and event of a file, as they read back.
type record struct {
	hdr lcio.RunHeader
	evt lcio.Event
}

// readAll reads every record of a file.
func readAll(fname string) ([]record, error) {
	r, err := lcio.Open(fname)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var out []record
	for r.Next() {
		out = append(out, record{hdr: r.RunHeader(), evt: r.Event()})
	}
	if err := r.Err(); err != nil && err != io.EOF {
		return nil, err
	}
	return out, nil
}

func testCreateEvent(t *testing.T, compLevel int, fname string) {
	w, err := lcio.Create(fname)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.SetCompressionLevel(compLevel)

	rhdr := lcio.RunHeader{
		RunNumber:    42,
		Descr:        "a simple run header",
		Detector:     "my detector",
		SubDetectors: []string{"det-1", "det-2"},
		Params: lcio.Params{
			Floats: map[string][]float32{
				"floats-1": {1, 2, 3},
				"floats-2": {4, 5, 6},
			},
			Strings: map[string][]string{
				"strings-1": {"1", "2", "3"},
			},
			Ints: map[string][]int32{
				"ints-1": {1, 2, 3},
			},
		},
	}

	err = w.WriteRunHeader(&rhdr)
	if err != nil {
		t.Fatal(err)
	}

	evt := lcio.Event{
		RunNumber:   rhdr.RunNumber,
		Detector:    rhdr.Detector,
		EventNumber: 52,
		TimeStamp:   1234567890,
		Params: lcio.Params{
			Floats: map[string][]float32{
				"_weight": {42},
			},
			Strings: map[string][]string{
				"Descr": {"a description"},
			},
			Ints: map[string][]int32{
				"ints": {10, 20, 30},
			},
		},
	}

	if evt.Has("not-there") {
		t.Errorf("got an unexpected collection")
	}

	mcparts := lcio.McParticleContainer{
		Flags: 0x1234,
		Params: lcio.Params{
			Floats:  map[string][]float32{},
			Strings: map[string][]string{},
			Ints:    map[string][]int32{},
		},
	}
	for i := range 3 {
		i32 := int32(i+1) * 10
		f32 := float32(i+1) * 10
		f64 := float64(i+1) * 10
		mc := lcio.McParticle{
			PDG:       i32,
			Mass:      f64,
			Charge:    f32,
			P:         [3]float64{f64, f64, f64},
			PEndPoint: [3]float64{f64, f64, f64},
			GenStatus: int32(i + 1),
			SimStatus: 1 << 31,
			ColorFlow: [2]int32{i32, i32},
			Spin:      [3]float32{f32, f32, f32},
		}
		mcparts.Particles = append(mcparts.Particles, mc)
	}
	mcparts.Particles[1].Parents = []*lcio.McParticle{&mcparts.Particles[0], &mcparts.Particles[2]}
	mcparts.Particles[0].Children = []*lcio.McParticle{&mcparts.Particles[1]}
	mcparts.Particles[2].Children = []*lcio.McParticle{&mcparts.Particles[1]}

	simhits := lcio.SimCalorimeterHitContainer{
		Flags: lcio.BitsChLong | lcio.BitsChID1 | lcio.BitsChStep | lcio.BitsChPDG,
		Params: lcio.Params{
			Strings: map[string][]string{
				"CellIDEncoding": {"M:3,S-1:3,I:9,J:9,K-1:6"},
			},
			Floats: map[string][]float32{},
			Ints:   map[string][]int32{},
		},
		Hits: []lcio.SimCalorimeterHit{
			{
				CellID0: 1024, CellID1: 256, Energy: 42.666, Pos: [3]float32{1, 2, 3},
				Contributions: []lcio.Contrib{
					{Energy: 10, Mc: &mcparts.Particles[0]},
					{Energy: 11, Mc: &mcparts.Particles[1]},
				},
			},
			{
				CellID0: 1025, CellID1: 256, Energy: 42.666, Pos: [3]float32{1, 2, 3},
				Contributions: []lcio.Contrib{
					{Energy: 10, Mc: &mcparts.Particles[0]},
					{Energy: 11, Mc: &mcparts.Particles[1]},
					{Energy: 12, Mc: &mcparts.Particles[2]},
				},
			},
		},
	}

	calhits := lcio.CalorimeterHitContainer{
		Flags: lcio.BitsRChLong | lcio.BitsRChID1 | lcio.BitsRChTime | lcio.BitsRChNoPtr | lcio.BitsRChEnergyError,
		Params: lcio.Params{
			Floats:  map[string][]float32{"f32": {1, 2, 3}},
			Ints:    map[string][]int32{"i32": {1, 2, 3}},
			Strings: map[string][]string{"str": {"1", "2", "3"}},
		},
		Hits: []lcio.CalorimeterHit{
			{
				CellID0:   1024,
				CellID1:   2048,
				Energy:    1000,
				EnergyErr: 0.1,
				Time:      1234,
				Pos:       [3]float32{11, 22, 33},
				Type:      42,
			},
		},
	}

	evt.Add("McParticles", &mcparts)
	evt.Add("SimCaloHits", &simhits)
	evt.Add("CaloHits", &calhits)

	if key := "McParticles"; !evt.Has(key) {
		t.Errorf("expected to have key %q", key)
	}

	err = w.WriteEvent(&evt)
	if err != nil {
		t.Fatal(err)
	}

	err = w.Close()
	if err != nil {
		t.Fatal(err)
	}

	r, err := lcio.Open(fname)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	r.Next()
	if r.Err() != nil {
		t.Fatal(r.Err())
	}

	if got, want := r.RunHeader(), rhdr; !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: run-headers differ.\ngot= %#v\nwant=%#v\n", fname, got, want)
	}

	if got, want := r.Event(), evt; !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: evts differ.\ngot:\n%v\nwant:\n%v\n", fname, &got, &want)
	}

	if !sameAsGolden(t, fname, compLevel) {
		return
	}

	os.Remove(fname)
}
