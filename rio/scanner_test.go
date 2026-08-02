// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rio

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
)

func TestScanner(t *testing.T) {
	evts := makeEvents(10)
	buf := new(bytes.Buffer)
	w, err := NewWriter(buf)
	if err != nil {
		t.Fatalf("could not create rio writer: %v", err)
	}
	defer w.Close()

	for i := range evts {
		id := fmt.Sprintf("evt-%03d", i+1)
		err = w.WriteValue(id, &evts[i])
		if err != nil {
			t.Fatalf("could not write %s: %v", id, err)
		}
	}

	err = w.Close()
	if err != nil {
		t.Fatalf("could not close rio writer: %v", err)
	}

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("could not create rio reader: %v", err)
	}
	defer r.Close()

	nevts := 0
	sc := NewScanner(r)
	sc.Select([]Selector{
		{Name: "evt-001", Unpack: true},
		{Name: "evt-002", Unpack: true},
		{Name: "evt-003", Unpack: true},
		{Name: "evt-004", Unpack: true},
		{Name: "evt-005", Unpack: true},
		{Name: "evt-006", Unpack: true},
		{Name: "evt-007", Unpack: true},
		{Name: "evt-008", Unpack: true},
		{Name: "evt-009", Unpack: true},
		{Name: "evt-010", Unpack: true},
	})

	var evt event
	for sc.Scan() {
		rec := sc.Record()
		if rec == nil {
			break
		}
		blk := rec.Block(rec.Name())
		err = blk.Read(&evt)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(evt, evts[nevts]) {
			t.Fatalf("%s: events differ.\ngot= %#v\nwant=%#v\n", rec.Name(), evt, evts[nevts])
		}
		nevts++
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("error during scan: %v", err)
	}

	if nevts != len(evts) {
		t.Fatalf("invalid number of events: got=%d, want=%d", nevts, len(evts))
	}
}

func makeEvents(n int) []event {
	evts := make([]event, 0, n)
	for i := range n {
		evts = append(evts, event{
			runnbr: int64(1000 + n),
			evtnbr: int64(10000 + n),
			id:     fmt.Sprintf("id-%03d", i),
			eles:   []electron{newElectron(float64(i+1)+100, float64(i+2)+100, float64(i+3)+100, float64(i+4)+100)},
			muons:  []muon{newMuon(float64(i+1)+200, float64(i+2)+200, float64(i+3)+200, float64(i+4)+200)},
		})
	}
	return evts
}

// rioStream returns a rio stream holding n events, named evt-001 and up.
func rioStream(t *testing.T, n int) ([]byte, []event) {
	t.Helper()

	var (
		evts = makeEvents(n)
		buf  = new(bytes.Buffer)
	)

	w, err := NewWriter(buf)
	if err != nil {
		t.Fatalf("could not create rio writer: %v", err)
	}

	for i := range evts {
		id := fmt.Sprintf("evt-%03d", i+1)
		err = w.WriteValue(id, &evts[i])
		if err != nil {
			t.Fatalf("could not write %s: %v", id, err)
		}
	}

	err = w.Close()
	if err != nil {
		t.Fatalf("could not close rio writer: %v", err)
	}

	return buf.Bytes(), evts
}

// TestScannerSkip checks the records a scanner steps over: the ones no
// selector asked for, and the ones asked for without being unpacked. Both
// have to leave the stream on the next record's header.
func TestScannerSkip(t *testing.T) {
	const nevts = 10

	for _, tc := range []struct {
		name string
		sel  []Selector
		want []string
	}{
		{
			name: "no-selection",
			// a scanner with no selection at all returns every record
			// of the stream, the metadata one included.
			want: []string{
				"evt-001", "evt-002", "evt-003", "evt-004", "evt-005",
				"evt-006", "evt-007", "evt-008", "evt-009", "evt-010",
				".rio.meta",
			},
		},
		{
			name: "packed",
			sel: []Selector{
				{Name: "evt-003"},
				{Name: "evt-007"},
			},
			want: []string{"evt-003", "evt-007"},
		},
		{
			name: "mixed",
			sel: []Selector{
				{Name: "evt-002", Unpack: true},
				{Name: "evt-005"},
				{Name: "evt-010", Unpack: true},
			},
			want: []string{"evt-002", "evt-005", "evt-010"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, evts := rioStream(t, nevts)

			r, err := NewReader(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("could not create rio reader: %v", err)
			}
			defer r.Close()

			sc := NewScanner(r)
			if tc.sel != nil {
				sc.Select(tc.sel)
			}

			var got []string
			for sc.Scan() {
				rec := sc.Record()
				if rec == nil {
					break
				}
				got = append(got, rec.Name())

				if !rec.unpack {
					continue
				}

				var (
					evt event
					blk = rec.Block(rec.Name())
				)
				err = blk.Read(&evt)
				if err != nil {
					t.Fatalf("%s: %v", rec.Name(), err)
				}
				var i int
				_, err = fmt.Sscanf(rec.Name(), "evt-%03d", &i)
				if err != nil {
					t.Fatalf("could not parse record name %q: %v", rec.Name(), err)
				}
				if !reflect.DeepEqual(evt, evts[i-1]) {
					t.Fatalf("%s: events differ.\ngot= %#v\nwant=%#v", rec.Name(), evt, evts[i-1])
				}
			}
			if err := sc.Err(); err != nil {
				t.Fatalf("error during scan: %v", err)
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("invalid records.\ngot= %v\nwant=%v", got, tc.want)
			}
		})
	}
}

// TestScannerCorruptedStream checks that a stream whose framing does not make
// sense is an error, and not a panic.
func TestScannerCorruptedStream(t *testing.T) {
	data, _ := rioStream(t, 2)

	// the frame of the first record header, right after the rio magic and
	// the header length.
	copy(data[8:12], []byte{0xde, 0xad, 0xde, 0xad})

	r, err := NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("could not create rio reader: %v", err)
	}
	defer r.Close()

	sc := NewScanner(r)
	if sc.Scan() {
		t.Fatalf("scanned a record out of a corrupted stream")
	}

	want := "rio: read header corrupted (frame=rio.frameType{0xde, 0xad, 0xde, 0xad})"
	if got := sc.Err(); got == nil || got.Error() != want {
		t.Fatalf("invalid error:\ngot= %v\nwant=%v", got, want)
	}
}
