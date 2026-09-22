// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rhist

import (
	"reflect"
	"testing"

	"go-hep.org/x/hep/groot/internal/rtests"
	"go-hep.org/x/hep/groot/rbytes"
	"go-hep.org/x/hep/groot/rtypes"
	"go-hep.org/x/hep/hbook"
)

// TestHistoRWRoundTrip marshals every flavour of histogram groot knows how to
// build from hbook, reads it back, and checks nothing was lost on the way.
func TestHistoRWRoundTrip(t *testing.T) {
	h1 := hbook.NewH1D(4, 0, 4)
	h1.Fill(0.5, 1)
	h1.Fill(1.5, 2)
	h1.Fill(2.5, 3)
	h1.Fill(-1, 1) // underflow
	h1.Fill(+5, 1) // overflow

	h2 := hbook.NewH2D(3, 0, 3, 2, 0, 2)
	h2.Fill(0.5, 0.5, 1)
	h2.Fill(1.5, 1.5, 2)
	h2.Fill(2.5, 0.5, 3)

	for _, tc := range []struct {
		name string
		want rtests.ROOTer
	}{
		{name: "TH1C", want: NewH1CFrom(h1)},
		{name: "TH1S", want: NewH1SFrom(h1)},
		{name: "TH1I", want: NewH1IFrom(h1)},
		{name: "TH1F", want: NewH1FFrom(h1)},
		{name: "TH1D", want: NewH1DFrom(h1)},
		{name: "TH2C", want: NewH2CFrom(h2)},
		{name: "TH2S", want: NewH2SFrom(h2)},
		{name: "TH2I", want: NewH2IFrom(h2)},
		{name: "TH2F", want: NewH2FFrom(h2)},
		{name: "TH2D", want: NewH2DFrom(h2)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, want := tc.want.Class(), tc.name; got != want {
				t.Fatalf("invalid class name: got=%q, want=%q", got, want)
			}

			wbuf := rbytes.NewWBuffer(nil, nil, 0, nil)
			_, err := tc.want.MarshalROOT(wbuf)
			if err != nil {
				t.Fatalf("could not marshal ROOT: %+v", err)
			}

			fct := rtypes.Factory.Get(tc.name)
			if fct == nil {
				t.Fatalf("no factory entry for %q", tc.name)
			}

			obj := fct().Interface().(rtests.ROOTer)
			rbuf := rbytes.NewRBuffer(wbuf.Bytes(), nil, 0, nil)
			err = obj.UnmarshalROOT(rbuf)
			if err != nil {
				t.Fatalf("could not unmarshal ROOT: %+v", err)
			}

			// compare the bytes rather than the values: a round-trip turns
			// the nil slices of a freshly built histogram into empty ones,
			// which reflect.DeepEqual minds and a ROOT file does not.
			rw := rbytes.NewWBuffer(nil, nil, 0, nil)
			_, err = obj.MarshalROOT(rw)
			if err != nil {
				t.Fatalf("could not re-marshal ROOT: %+v", err)
			}

			if got, want := rw.Bytes(), wbuf.Bytes(); !reflect.DeepEqual(got, want) {
				t.Fatalf("round-trip lost bytes:\ngot= %v\nwant=%v", got, want)
			}
		})
	}
}
