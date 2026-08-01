// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package symlink_test

import (
	"encoding/binary"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto/symlink"
)

func TestRequest(t *testing.T) {
	for _, want := range []symlink.Request{
		{Target: "/data/raw/run42.root", Link: "/data/by-tag/latest.root"},
		{Target: "/a", Link: "/b"},
		// A name with a space in it is the case the separate length field
		// exists for: splitting on the space alone would cut it in half.
		{Target: "/data/run 42.root", Link: "/data/latest.root"},
	} {
		var (
			w   xrdenc.WBuffer
			got symlink.Request
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
	// The length of the target is the fifteenth and sixteenth parameter bytes,
	// exactly as in a mv request: a server reads the two paths out of one
	// string and has nothing else to tell it where the first one ends.
	req := symlink.Request{Target: "/data/raw/run42.root", Link: "/l"}

	var w xrdenc.WBuffer
	if err := req.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal request: %v", err)
	}
	raw := w.Bytes()

	if got, want := binary.BigEndian.Uint16(raw[14:16]), uint16(len(req.Target)); got != want {
		t.Fatalf("target length: got = %d, want = %d", got, want)
	}
	if got, want := binary.BigEndian.Uint32(raw[16:20]), uint32(len(req.Target)+len(req.Link)+1); got != want {
		t.Fatalf("data length: got = %d, want = %d", got, want)
	}
	if got, want := string(raw[20:]), req.Target+" "+req.Link; got != want {
		t.Fatalf("payload: got = %q, want = %q", got, want)
	}
	for i, b := range raw[:14] {
		if b != 0 {
			t.Fatalf("reserved parameter byte %d is %d, want 0", i, b)
		}
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
			want: "targetLen",
		},
		{
			name: "no separator at all",
			raw:  append(append(make([]byte, 14), 0x00, 0x00), append([]byte{0, 0, 0, 3}, []byte("abc")...)...),
			want: "separated",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var req symlink.Request
			err := req.UnmarshalXrd(xrdenc.NewRBuffer(tc.raw))
			if err == nil {
				t.Fatalf("a symlink request that cannot be split was accepted as %#v", req)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestRequestID(t *testing.T) {
	var req symlink.Request
	if got, want := req.ReqID(), symlink.RequestID; got != want {
		t.Fatalf("ReqID = %d, want %d", got, want)
	}
	if req.ShouldSign() {
		// A vendor extension is in no server's protection profile: a server
		// that has never heard of kXR_symlink has no rule saying a signature
		// belongs in front of it.
		t.Fatal("a symlink request asks to be signed")
	}
}
