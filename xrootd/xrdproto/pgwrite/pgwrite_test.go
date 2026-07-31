// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pgwrite

import (
	"bytes"
	"encoding/binary"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/pgbuf"
	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

func TestRequestMarshalRoundTrip(t *testing.T) {
	data := bytes.Repeat([]byte{0x7e}, pgbuf.PageSize+10)
	req := Request{Handle: xrdfs.FileHandle{9, 8, 7, 6}, Offset: 4096, Data: data}
	var w xrdenc.WBuffer
	if err := req.MarshalXrd(&w); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// 16 params + 4 dlen + encoded page units.
	if want := 20 + pgbuf.EncodedLen(4096, len(data)); len(w.Bytes()) != want {
		t.Fatalf("marshaled length: got=%d want=%d", len(w.Bytes()), want)
	}
	var back Request
	if err := back.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Handle != req.Handle || back.Offset != req.Offset || !bytes.Equal(back.Data, data) {
		t.Fatal("roundtrip mismatch")
	}
}

func TestResponseUnmarshal(t *testing.T) {
	info := make([]byte, 8)
	binary.BigEndian.PutUint64(info, 12345)
	frame := xrdproto.StatusFrame(xrdproto.StatusBody{
		RequestID: uint8(RequestID - 3000),
		RespType:  xrdproto.FinalResult,
	}, info)
	var resp Response
	if err := resp.UnmarshalXrd(xrdenc.NewRBuffer(frame)); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Offset != 12345 {
		t.Fatalf("offset: got=%d want=12345", resp.Offset)
	}
}

// cseTrailer builds a checksum-error trailer naming the given file offsets.
func cseTrailer(offsets ...int64) []byte {
	cse := make([]byte, CSEHeaderLength+8*len(offsets))
	for i, off := range offsets {
		binary.BigEndian.PutUint64(cse[CSEHeaderLength+8*i:], uint64(off))
	}
	return cse
}

func TestParseCSE(t *testing.T) {
	for _, tc := range []struct {
		name string
		cse  []byte
		want []int64
		err  bool
	}{
		{
			name: "two corrupt pages",
			cse:  cseTrailer(0, 8192),
			want: []int64{0, 8192},
		},
		{
			// The server may send the header alone to say "nothing corrupt".
			name: "header only",
			cse:  cseTrailer(),
			want: nil,
		},
		{
			name: "shorter than the header",
			cse:  make([]byte, CSEHeaderLength-1),
			err:  true,
		},
		{
			// A trailer whose payload is not a whole number of offsets is
			// malformed: silently dropping the remainder would lose a
			// corrupt page and report a clean write.
			name: "partial offset",
			cse:  make([]byte, CSEHeaderLength+3),
			err:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCSE(tc.cse)
			switch {
			case tc.err && err == nil:
				t.Fatalf("got offsets %v, want an error", got)
			case !tc.err && err != nil:
				t.Fatalf("ParseCSE: %v", err)
			case tc.err:
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestResponseUnmarshalCSE(t *testing.T) {
	cse := cseTrailer(4096)
	info := make([]byte, 8)
	binary.BigEndian.PutUint64(info, 4096)
	frame := xrdproto.StatusFrame(xrdproto.StatusBody{
		RequestID:  uint8(RequestID - 3000),
		RespType:   xrdproto.FinalResult,
		DataLength: int32(len(cse)),
	}, info)
	// The session appends the trailing data to the frame before unmarshaling.
	frame = append(frame, cse...)

	var resp Response
	if err := resp.UnmarshalXrd(xrdenc.NewRBuffer(frame)); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Offset != 4096 {
		t.Fatalf("got offset %d, want 4096", resp.Offset)
	}
	if len(resp.Corrupt) != 1 || resp.Corrupt[0] != 4096 {
		t.Fatalf("got corrupt pages %v, want [4096]", resp.Corrupt)
	}
}

func TestRequestFlagsRoundTrip(t *testing.T) {
	req := Request{Handle: xrdfs.FileHandle{1, 2, 3, 4}, Offset: 8192, Data: []byte("page"), Flags: Retry}
	var w xrdenc.WBuffer
	if err := req.MarshalXrd(&w); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Request
	if err := got.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Flags != Retry {
		t.Fatalf("got flags %#x, want %#x", got.Flags, Retry)
	}
	if got.Offset != req.Offset || !bytes.Equal(got.Data, req.Data) {
		t.Fatalf("round trip lost data: off=%d data=%q", got.Offset, got.Data)
	}
}

func TestRequestMaxResponseLength(t *testing.T) {
	// The bound must cover a trailer that names every page written.
	data := bytes.Repeat([]byte{0x11}, 4*pgbuf.PageSize)
	req := Request{Data: data}
	want := int64(xrdproto.StatusBodyLength + 8 + CSEHeaderLength + 8*4)
	if got := req.MaxResponseLength(); got < want {
		t.Fatalf("got bound %d, too small for a trailer naming all 4 pages (%d)", got, want)
	}
}
