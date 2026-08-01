// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sigver_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/chkpoint"
	"go-hep.org/x/hep/xrootd/xrdproto/sigver"
	"go-hep.org/x/hep/xrootd/xrdproto/write"
	"go-hep.org/x/hep/xrootd/xrdproto/writev"
)

// frame builds a marshalled request: the 4-byte header, 16 parameter bytes, a
// declared data length, and whatever follows it on the wire.
func frame(reqID uint16, params []byte, payload, trailing []byte) []byte {
	raw := make([]byte, 4, 24+len(payload)+len(trailing))
	binary.BigEndian.PutUint16(raw[2:4], reqID)
	raw = append(raw, params...)
	raw = binary.BigEndian.AppendUint32(raw, uint32(len(payload)))
	raw = append(raw, payload...)
	return append(raw, trailing...)
}

// key is the session key the exchange agreed with the server; every signature
// in this suite is keyed with it.
var key = []byte("0123456789abcdef")

// digest is what a server computes for a request it has read: the sequence
// number, then the frame and exactly the payload the frame declares, all keyed
// with the session key.
func digest(seqID int64, framed []byte) []byte {
	h := hmac.New(sha256.New, key)
	var s [8]byte
	binary.BigEndian.PutUint64(s[:], uint64(seqID))
	_, _ = h.Write(s[:])
	_, _ = h.Write(framed)
	return h.Sum(nil)
}

func TestSignatureStopsAtTheDeclaredLength(t *testing.T) {
	// A server verifies a request by hashing what it read as the request, and
	// it stops reading at the declared length. kXR_writev and a kXR_ckpXeq
	// carrying a write both put data on the wire past that point; a client
	// that hashed it too would produce a signature the server can never arrive
	// at, and every signed request of that kind would be rejected as forged.
	var (
		params   = bytes.Repeat([]byte{0xAB}, 16)
		payload  = []byte("the descriptors the frame counts")
		trailing = []byte("the data it does not")
		seqID    = int64(7)
	)

	raw := frame(chkpoint.RequestID, params, payload, trailing)
	got := sigver.NewRequest(key, chkpoint.RequestID, seqID, raw)
	want := digest(seqID, raw[:24+len(payload)])

	if !bytes.Equal(got.Signature, want) {
		t.Fatalf("the signature covers the trailing data:\ngot  = %x\nwant = %x", got.Signature, want)
	}
	if bytes.Equal(got.Signature, digest(seqID, raw)) {
		t.Fatal("the signature is over the whole buffer, trailing data included")
	}
}

func TestSignatureCoversTheWholePayloadWhenNothingTrails(t *testing.T) {
	// The ordinary case: a request whose buffer is exactly its frame is hashed
	// end to end, which is what it was before the trailing-data rule existed.
	var (
		params  = bytes.Repeat([]byte{0x01}, 16)
		payload = []byte("/tmp/file.dat")
		seqID   = int64(3)
	)

	raw := frame(3010, params, payload, nil)
	got := sigver.NewRequest(key, 3010, seqID, raw)

	if want := digest(seqID, raw); !bytes.Equal(got.Signature, want) {
		t.Fatalf("signature:\ngot  = %x\nwant = %x", got.Signature, want)
	}
}

func TestAWriteIsSignedByItsHeaderAlone(t *testing.T) {
	// kXR_write says so in the flags: hashing a gigabyte of payload to
	// authenticate where it goes would cost more than the write itself, so the
	// signature covers the header and kXR_nodata tells the server to expect
	// that.
	var (
		params  = bytes.Repeat([]byte{0x02}, 16)
		payload = bytes.Repeat([]byte{0x03}, 512)
		seqID   = int64(11)
	)

	raw := frame(write.RequestID, params, payload, nil)
	got := sigver.NewRequest(key, write.RequestID, seqID, raw)

	if want := digest(seqID, raw[:24]); !bytes.Equal(got.Signature, want) {
		t.Fatalf("signature:\ngot  = %x\nwant = %x", got.Signature, want)
	}
	if got.Flags&sigver.NoData == 0 {
		t.Fatal("a write is signed without its payload but does not say so")
	}
}

func TestAShortBufferIsHashedWhole(t *testing.T) {
	// Nothing on the wire is this short, but the length arithmetic must not
	// reach past a buffer that does not hold a whole frame.
	raw := []byte{0x00, 0x01, 0x0b, 0xc2}
	got := sigver.NewRequest(key, 3010, 1, raw)

	if want := digest(1, raw); !bytes.Equal(got.Signature, want) {
		t.Fatalf("signature:\ngot  = %x\nwant = %x", got.Signature, want)
	}
}

func TestADeclaredLengthPastTheBufferIsHashedWhole(t *testing.T) {
	// A length longer than what is there cannot be trusted to slice with.
	raw := frame(3010, bytes.Repeat([]byte{0x00}, 16), nil, nil)
	binary.BigEndian.PutUint32(raw[20:24], 1<<20)

	got := sigver.NewRequest(key, 3010, 1, raw)
	if want := digest(1, raw); !bytes.Equal(got.Signature, want) {
		t.Fatalf("signature:\ngot  = %x\nwant = %x", got.Signature, want)
	}
}

func TestRequest(t *testing.T) {
	want := sigver.NewRequest(key, write.RequestID, 42, make([]byte, 24))

	var (
		w   xrdenc.WBuffer
		got sigver.Request
	)
	if err := want.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal request: %v", err)
	}
	if err := got.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
		t.Fatalf("could not unmarshal request: %v", err)
	}
	if got.ID != want.ID || got.SeqID != want.SeqID || !bytes.Equal(got.Signature, want.Signature) {
		t.Fatalf("round trip:\ngot  = %#v\nwant = %#v", got, want)
	}
	if got, want := want.ReqID(), sigver.RequestID; got != want {
		t.Fatalf("ReqID = %d, want %d", got, want)
	}
}

func TestTheSignatureIsKeyedByTheSession(t *testing.T) {
	// Every byte a signature covers travels on the wire in the clear, so an
	// unkeyed digest of them is one anybody who saw the request can recompute:
	// they could put it in front of a request of their own and the server would
	// take it. What makes the signature evidence is the session key, which only
	// the two ends of the authenticated exchange hold.
	raw := frame(3010, bytes.Repeat([]byte{0x04}, 16), []byte("/tmp/file.dat"), nil)

	got := sigver.NewRequest(key, 3010, 5, raw)
	other := sigver.NewRequest([]byte("fedcba9876543210"), 3010, 5, raw)
	if bytes.Equal(got.Signature, other.Signature) {
		t.Fatal("two sessions sign the same request identically: the key is not used")
	}

	unkeyed := sha256.New()
	var s [8]byte
	binary.BigEndian.PutUint64(s[:], 5)
	_, _ = unkeyed.Write(s[:])
	_, _ = unkeyed.Write(raw)
	if bytes.Equal(got.Signature, unkeyed.Sum(nil)) {
		t.Fatal("the signature is a plain hash of what is already on the wire")
	}
}

var _ xrdproto.Request = (*sigver.Request)(nil)

func TestAVectorWriteIsSignedOverItsWriteList(t *testing.T) {
	// A vector write puts its segment data on the wire past the frame, which
	// once looked like a reason it could not be signed at all. It is not: the
	// server reads the frame and the write_list its length field declares, and
	// hashes those. This drives the real marshaller so the claim is checked
	// against the bytes that actually go out, not against a hand-built frame.
	req := writev.Request{Segments: []writev.Segment{
		{Handle: xrdfs.FileHandle{0x01, 0x02, 0x03, 0x04}, Offset: 0, Data: []byte("the first segment")},
		{Handle: xrdfs.FileHandle{0x01, 0x02, 0x03, 0x04}, Offset: 64, Data: []byte("and the second")},
	}}

	var wbuf xrdenc.WBuffer
	wbuf.WriteU16(0) // the stream id and the two unused header bytes
	wbuf.WriteU16(writev.RequestID)
	if err := req.MarshalXrd(&wbuf); err != nil {
		t.Fatalf("could not marshal the request: %v", err)
	}
	raw := wbuf.Bytes()

	const seqID = int64(5)
	got := sigver.NewRequest(key, writev.RequestID, seqID, raw)

	// The write_list is two 16-byte descriptors; the 31 bytes of segment data
	// that follow them are not part of the request the server read.
	signed := 24 + 2*writev.SegmentLength
	if want := digest(seqID, raw[:signed]); !bytes.Equal(got.Signature, want) {
		t.Fatalf("the signature is not the one the server computes:\ngot  = %x\nwant = %x", got.Signature, want)
	}
	if bytes.Equal(got.Signature, digest(seqID, raw)) {
		t.Fatal("the signature covers the segment data, which the server never reads as part of the request")
	}
	if got.Flags&sigver.NoData != 0 {
		t.Fatal("a vector write is signed with its write_list, so kXR_nodata would tell the server to skip what it must hash")
	}
}
