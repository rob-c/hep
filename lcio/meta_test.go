// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lcio

import (
	"math"
	"path/filepath"
	"reflect"
	"testing"

	"go-hep.org/x/hep/sio"
)

// TestIndexRoundTrip checks that an index survives a trip through a SIO
// stream, for each of the layouts its control word can ask for.
func TestIndexRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		idx  Index
	}{
		{
			name: "multi-run",
			idx: Index{
				RunMin:     10,
				BaseOffset: 4096,
				Offsets: []Offset{
					{RunOffset: 0, EventNumber: -1, Location: 0},
					{RunOffset: 0, EventNumber: 1, Location: 128},
					{RunOffset: 2, EventNumber: 2, Location: 256},
				},
			},
		},
		{
			name: "single-run",
			idx: Index{
				ControlWord: IndexSingleRun,
				RunMin:      10,
				BaseOffset:  4096,
				Offsets: []Offset{
					{EventNumber: 1, Location: 128},
					{EventNumber: 2, Location: 256},
				},
			},
		},
		{
			// the layout of any file large enough to need more than 2 GB
			// of offsets, which the 32-bit locations cannot address.
			name: "int64-offsets",
			idx: Index{
				ControlWord: IndexSingleRun | IndexInt64Offset,
				RunMin:      10,
				BaseOffset:  1 << 40,
				Offsets: []Offset{
					{EventNumber: 1, Location: 1 << 33},
					{EventNumber: 2, Location: 1 << 34},
				},
			},
		},
		{
			name: "params",
			idx: Index{
				ControlWord: IndexSingleRun | IndexParams,
				RunMin:      10,
				Offsets: []Offset{
					{
						EventNumber: 1,
						Location:    128,
						Ints:        []int32{1, 2, 3},
						Floats:      []float32{1.5, 2.5},
						Strings:     []string{"hello", "world"},
					},
					// no parameter at all, which is written and read
					// back as three empty lists.
					{
						EventNumber: 2,
						Location:    256,
						Ints:        []int32{},
						Floats:      []float32{},
						Strings:     []string{},
					},
				},
			},
		},
		{
			name: "empty",
			idx:  Index{ControlWord: IndexSingleRun},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fname := filepath.Join(t.TempDir(), "index.sio")

			func() {
				f, err := sio.Create(fname)
				if err != nil {
					t.Fatalf("could not create sio stream: %+v", err)
				}
				defer f.Close()

				idx := tc.idx
				rec := f.Record(Records.Index)
				rec.SetUnpack(true)
				err = rec.Connect(Blocks.Index, &idx)
				if err != nil {
					t.Fatalf("could not connect index block: %+v", err)
				}

				err = f.WriteRecord(rec)
				if err != nil {
					t.Fatalf("could not write index record: %+v", err)
				}

				err = f.Sync()
				if err != nil {
					t.Fatalf("could not flush index record: %+v", err)
				}
			}()

			var idx Index
			func() {
				f, err := sio.Open(fname)
				if err != nil {
					t.Fatalf("could not open sio stream: %+v", err)
				}
				defer f.Close()

				rec := f.Record(Records.Index)
				rec.SetUnpack(true)
				err = rec.Connect(Blocks.Index, &idx)
				if err != nil {
					t.Fatalf("could not connect index block: %+v", err)
				}

				_, err = f.ReadRecord()
				if err != nil {
					t.Fatalf("could not read index record: %+v", err)
				}
			}()

			want := tc.idx
			if want.Offsets == nil {
				// an index with no offset reads back as an empty slice.
				want.Offsets = []Offset{}
			}
			if !reflect.DeepEqual(idx, want) {
				t.Fatalf("indices differ:\ngot= %#v\nwant=%#v", idx, want)
			}
		})
	}
}

// TestIndexOffsetTooLarge checks that an offset that does not fit in the
// layout the control word asked for is refused, rather than truncated.
func TestIndexOffsetTooLarge(t *testing.T) {
	fname := filepath.Join(t.TempDir(), "index.sio")

	f, err := sio.Create(fname)
	if err != nil {
		t.Fatalf("could not create sio stream: %+v", err)
	}
	defer f.Close()

	idx := Index{
		ControlWord: IndexSingleRun,
		Offsets: []Offset{
			{EventNumber: 1, Location: math.MaxInt32 + 1},
		},
	}

	rec := f.Record(Records.Index)
	err = rec.Connect(Blocks.Index, &idx)
	if err != nil {
		t.Fatalf("could not connect index block: %+v", err)
	}

	err = f.WriteRecord(rec)
	want := "lcio: offset 2147483648 does not fit on 32 bits: set the 2 bit of the index control word"
	if err == nil || err.Error() != want {
		t.Fatalf("invalid error:\ngot= %v\nwant=%v", err, want)
	}
}
