// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package readv

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

func TestRequestMarshalRoundTrip(t *testing.T) {
	req := Request{Segments: []Segment{
		{Handle: xrdfs.FileHandle{1, 2, 3, 4}, Length: 100, Offset: 0},
		{Handle: xrdfs.FileHandle{5, 6, 7, 8}, Length: 7, Offset: 1 << 40},
	}}

	var w xrdenc.WBuffer
	if err := req.MarshalXrd(&w); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	raw := w.Bytes()

	// 16 params + 4 dlen + one 16-byte readahead_list entry per segment.
	if got, want := len(raw), 20+2*SegmentLength; got != want {
		t.Fatalf("marshaled length: got=%d want=%d", got, want)
	}
	if got, want := binary.BigEndian.Uint32(raw[16:20]), uint32(2*SegmentLength); got != want {
		t.Fatalf("dlen: got=%d want=%d", got, want)
	}
	// The descriptor layout is fhandle[4] | rlen[4] | offset[8], and the
	// params are reserved: a non-zero byte there is a field this client does
	// not own.
	if !bytes.Equal(raw[:16], make([]byte, 16)) {
		t.Fatalf("the reserved params are not zero: % x", raw[:16])
	}
	if got, want := binary.BigEndian.Uint32(raw[24:28]), uint32(100); got != want {
		t.Fatalf("segment 0 rlen: got=%d want=%d", got, want)
	}
	if got, want := binary.BigEndian.Uint64(raw[28:36]), uint64(0); got != want {
		t.Fatalf("segment 0 offset: got=%d want=%d", got, want)
	}
	if got, want := binary.BigEndian.Uint64(raw[44:52]), uint64(1<<40); got != want {
		t.Fatalf("segment 1 offset: got=%d want=%d", got, want)
	}

	var back Request
	if err := back.UnmarshalXrd(xrdenc.NewRBuffer(raw)); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(back.Segments, req.Segments) {
		t.Fatalf("roundtrip:\ngot  = %+v\nwant = %+v", back.Segments, req.Segments)
	}
}

func TestRequestMaxResponseLength(t *testing.T) {
	req := Request{Segments: []Segment{{Length: 100}, {Length: 20}}}
	// Two echo headers plus the bytes asked for.
	if got, want := req.MaxResponseLength(), int64(2*SegmentLength+120); got != want {
		t.Fatalf("got=%d want=%d", got, want)
	}
	var _ xrdproto.ResponseLimiter = &req
}

func TestRequestValidate(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  Request
		want string
	}{
		{"empty", Request{}, "with no segments"},
		{"negative", Request{Segments: []Segment{{Length: -1}}}, "negative segment length"},
		{
			"too many segments",
			Request{Segments: make([]Segment, xrdproto.MaxVectorSegments+1)},
			"exceeds the 1024-segment limit",
		},
		{
			"too many bytes",
			Request{Segments: []Segment{{Length: 1 << 30}, {Length: 1 << 30}}},
			"exceeds the 268435456-byte limit",
		},
		{"ok", Request{Segments: []Segment{{Length: 1}}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("Validate: %v", err)
			case tc.want == "":
			case err == nil:
				t.Fatalf("Validate accepted a request it should have refused")
			case !strings.Contains(err.Error(), tc.want):
				t.Fatalf("got %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// chunk builds one reply segment: the 16-byte echo header carrying the ACTUAL
// length, then that many bytes.
func chunk(h xrdfs.FileHandle, off int64, data []byte) []byte {
	out := make([]byte, SegmentLength)
	copy(out, h[:])
	binary.BigEndian.PutUint32(out[4:], uint32(len(data)))
	binary.BigEndian.PutUint64(out[8:], uint64(off))
	return append(out, data...)
}

func TestResponseUnmarshal(t *testing.T) {
	h1 := xrdfs.FileHandle{1, 2, 3, 4}
	h2 := xrdfs.FileHandle{9, 9, 9, 9}
	body := append(chunk(h1, 0, []byte("hello")), chunk(h2, 4096, []byte("xy"))...)

	var resp Response
	if err := resp.UnmarshalXrd(xrdenc.NewRBuffer(body)); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := []Chunk{
		{Handle: h1, Offset: 0, Data: []byte("hello")},
		{Handle: h2, Offset: 4096, Data: []byte("xy")},
	}
	if !reflect.DeepEqual(resp.Chunks, want) {
		t.Fatalf("got  = %+v\nwant = %+v", resp.Chunks, want)
	}

	var w xrdenc.WBuffer
	if err := resp.MarshalXrd(&w); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Equal(w.Bytes(), body) {
		t.Fatalf("re-marshal:\ngot  = % x\nwant = % x", w.Bytes(), body)
	}
}

// TestResponseUnmarshalRefusesMalformedBodies covers the ways a reply can be
// well framed and still not decodable: any of them would otherwise hand back
// data attributed to the wrong segment.
func TestResponseUnmarshalRefusesMalformedBodies(t *testing.T) {
	h := xrdfs.FileHandle{1, 2, 3, 4}
	full := chunk(h, 0, []byte("hello"))

	for _, tc := range []struct {
		name string
		body []byte
		want string
	}{
		{"header fragment", full[:8], "fragment of a 16-byte segment header"},
		{"truncated data", full[:len(full)-2], "exceeds the 3 bytes left"},
		{"trailing fragment", append(append([]byte{}, full...), 1, 2, 3), "fragment of a 16-byte segment header"},
		{"negative length", negLen(h), "negative length"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var resp Response
			err := resp.UnmarshalXrd(xrdenc.NewRBuffer(tc.body))
			if err == nil {
				t.Fatalf("a malformed reply was decoded into %+v", resp.Chunks)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func negLen(h xrdfs.FileHandle) []byte {
	out := make([]byte, SegmentLength+8)
	copy(out, h[:])
	binary.BigEndian.PutUint32(out[4:], ^uint32(0)) // -1
	return out
}

func TestRequestUnmarshalRefusesRaggedPayload(t *testing.T) {
	raw := make([]byte, 20+SegmentLength)
	binary.BigEndian.PutUint32(raw[16:20], SegmentLength+1)
	var req Request
	err := req.UnmarshalXrd(xrdenc.NewRBuffer(raw))
	if err == nil {
		t.Fatal("a payload that is not a whole number of segments was accepted")
	}
	if !strings.Contains(err.Error(), "whole number of 16-byte segments") {
		t.Fatalf("got %q", err)
	}
}
