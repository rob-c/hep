// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hardlink_test

import (
	"encoding/binary"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto/hardlink"
)

func TestRequest(t *testing.T) {
	for _, want := range []hardlink.Request{
		{OldPath: "/data/raw/run42.root", NewPath: "/data/by-tag/v3/run42.root"},
		{OldPath: "/a", NewPath: "/b"},
		// A name with a space in it is the case the separate length field
		// exists for: splitting on the space alone would cut it in half.
		{OldPath: "/data/run 42.root", NewPath: "/data/latest.root"},
	} {
		var (
			w   xrdenc.WBuffer
			got hardlink.Request
		)
		if err := want.MarshalXrd(&w); err != nil {
			t.Fatalf("could not marshal request: %v", err)
		}
		if err := got.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
			t.Fatalf("could not unmarshal request: %v", err)
		}
		if got != want {
			t.Fatalf("round trip:\ngot  = %#v\nwant = %#v", got, want)
		}
	}
}

func TestRequestLayout(t *testing.T) {
	req := hardlink.Request{OldPath: "/data/raw/run42.root", NewPath: "/n"}

	var w xrdenc.WBuffer
	if err := req.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal request: %v", err)
	}
	raw := w.Bytes()

	if got, want := binary.BigEndian.Uint16(raw[14:16]), uint16(len(req.OldPath)); got != want {
		t.Fatalf("old path length: got = %d, want = %d", got, want)
	}
	if got, want := string(raw[20:]), req.OldPath+" "+req.NewPath; got != want {
		t.Fatalf("payload: got = %q, want = %q", got, want)
	}
}

func TestRequestRejectsWhatItCannotSplit(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  []byte
		want string
	}{
		{
			name: "length past the paths",
			raw:  append(append(make([]byte, 14), 0x00, 0x64), append([]byte{0, 0, 0, 3}, []byte("a b")...)...),
			want: "oldLen",
		},
		{
			name: "no separator at all",
			raw:  append(append(make([]byte, 14), 0x00, 0x00), append([]byte{0, 0, 0, 3}, []byte("abc")...)...),
			want: "separated",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var req hardlink.Request
			err := req.UnmarshalXrd(xrdenc.NewRBuffer(tc.raw))
			if err == nil {
				t.Fatalf("a link request that cannot be split was accepted as %#v", req)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestRequestID(t *testing.T) {
	var req hardlink.Request
	if got, want := req.ReqID(), hardlink.RequestID; got != want {
		t.Fatalf("ReqID = %d, want %d", got, want)
	}
	if req.ShouldSign() {
		t.Fatal("a link request asks to be signed")
	}
}
