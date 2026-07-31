// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package writev

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

// TestRequestMarshalKeepsDataOutOfDlen is the whole point of this codec: stock
// servers enforce dlen%16 == 0, so counting the segment data inside dlen makes
// every vector write fail with kXR_ArgInvalid.
func TestRequestMarshalKeepsDataOutOfDlen(t *testing.T) {
	h := xrdfs.FileHandle{1, 2, 3, 4}
	req := Request{
		Options: OptionSync,
		Segments: []Segment{
			{Handle: h, Offset: 0, Data: []byte("hello")},
			{Handle: h, Offset: 4096, Data: []byte("xy")},
		},
	}

	var w xrdenc.WBuffer
	if err := req.MarshalXrd(&w); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	raw := w.Bytes()

	dlen := binary.BigEndian.Uint32(raw[16:20])
	if want := uint32(2 * SegmentLength); dlen != want {
		t.Fatalf("dlen: got=%d want=%d (it must cover the descriptors only)", dlen, want)
	}
	if dlen%SegmentLength != 0 {
		t.Fatalf("dlen %d is not a whole number of descriptors", dlen)
	}
	if got, want := len(raw), 20+int(dlen)+len("hello")+len("xy"); got != want {
		t.Fatalf("frame length: got=%d want=%d", got, want)
	}
	if raw[0] != OptionSync {
		t.Fatalf("the options byte is at params[0]: got %#x", raw[0])
	}
	if !bytes.Equal(raw[1:16], make([]byte, 15)) {
		t.Fatalf("the reserved params are not zero: % x", raw[1:16])
	}
	// The data streams after the descriptor block, concatenated and in order.
	if got, want := raw[20+dlen:], []byte("helloxy"); !bytes.Equal(got, want) {
		t.Fatalf("trailer: got=%q want=%q", got, want)
	}
	// Descriptor 1: fhandle | wlen | offset.
	if got, want := binary.BigEndian.Uint32(raw[24:28]), uint32(5); got != want {
		t.Fatalf("segment 0 wlen: got=%d want=%d", got, want)
	}
	if got, want := binary.BigEndian.Uint64(raw[44:52]), uint64(4096); got != want {
		t.Fatalf("segment 1 offset: got=%d want=%d", got, want)
	}
}

// TestRequestUnmarshalRecoversDescriptors checks the decode a framing reader
// can do: it sees the header and dlen bytes, so it recovers every descriptor
// and the size of the data still to be read, but not the data itself.
func TestRequestUnmarshalRecoversDescriptors(t *testing.T) {
	h := xrdfs.FileHandle{7, 7, 7, 7}
	req := Request{Segments: []Segment{
		{Handle: h, Offset: 10, Data: []byte("abc")},
		{Handle: h, Offset: 20, Data: nil},
	}}

	var w xrdenc.WBuffer
	if err := req.MarshalXrd(&w); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	frame := w.Bytes()[:20+2*SegmentLength] // what ReadRequest would deliver

	var back Request
	if err := back.UnmarshalXrd(xrdenc.NewRBuffer(frame)); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.Segments) != 2 {
		t.Fatalf("got %d segments, want 2", len(back.Segments))
	}
	for i, want := range []struct {
		off int64
		n   int
	}{{10, 3}, {20, 0}} {
		seg := back.Segments[i]
		if seg.Handle != h || seg.Offset != want.off || len(seg.Data) != want.n {
			t.Fatalf("segment %d: got %v/%d/%d, want %v/%d/%d",
				i, seg.Handle, seg.Offset, len(seg.Data), h, want.off, want.n)
		}
	}
}

func TestRequestValidate(t *testing.T) {
	big := make([]byte, 0)
	for _, tc := range []struct {
		name string
		req  Request
		want string
	}{
		{"empty", Request{}, "with no segments"},
		{
			"too many segments",
			Request{Segments: make([]Segment, xrdproto.MaxVectorSegments+1)},
			"exceeds the 1024-segment limit",
		},
		{"ok", Request{Segments: []Segment{{Data: big}}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("Validate: %v", err)
			case tc.want == "":
			case err == nil:
				t.Fatal("Validate accepted a request it should have refused")
			case !strings.Contains(err.Error(), tc.want):
				t.Fatalf("got %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestRequestUnmarshalBoundsTheDeclaredLengths matters because the declared
// lengths decide how many bytes are read from the connection next: a peer that
// declares 4 GiB across its descriptors must be refused before anything is
// reserved for it.
func TestRequestUnmarshalBoundsTheDeclaredLengths(t *testing.T) {
	const nsegs = 32
	raw := make([]byte, 20+nsegs*SegmentLength)
	binary.BigEndian.PutUint32(raw[16:20], nsegs*SegmentLength)
	for i := range nsegs {
		binary.BigEndian.PutUint32(raw[20+i*SegmentLength+4:], 1<<30)
	}

	var req Request
	err := req.UnmarshalXrd(xrdenc.NewRBuffer(raw))
	if err == nil {
		t.Fatal("a vector declaring 32 GiB of data was accepted")
	}
	if !strings.Contains(err.Error(), "exceeds the 268435456-byte limit") {
		t.Fatalf("got %q", err)
	}
}

func TestRequestUnmarshalRefusesMalformedFrames(t *testing.T) {
	ragged := make([]byte, 20+SegmentLength)
	binary.BigEndian.PutUint32(ragged[16:20], SegmentLength+1)

	negative := make([]byte, 20+SegmentLength)
	binary.BigEndian.PutUint32(negative[16:20], SegmentLength)
	binary.BigEndian.PutUint32(negative[24:], ^uint32(0)) // -1

	for _, tc := range []struct {
		name string
		raw  []byte
		want string
	}{
		{"short header", make([]byte, 12), "needs 20 bytes of header"},
		{"ragged payload", ragged, "whole number of 16-byte segments"},
		{"negative length", negative, "negative length"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var req Request
			err := req.UnmarshalXrd(xrdenc.NewRBuffer(tc.raw))
			if err == nil {
				t.Fatalf("a malformed frame was decoded into %+v", req.Segments)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %q, want it to mention %q", err, tc.want)
			}
		})
	}
}
