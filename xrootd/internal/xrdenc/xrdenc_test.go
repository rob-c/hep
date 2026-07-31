// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdenc_test

import (
	"bytes"
	"errors"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
)

// TestWriteReadRoundTrip pins the wire encoding: every scalar is big endian,
// strings and byte blobs are preceded by a 32-bit length, and Next writes the
// reserved bytes the protocol wants zeroed.
func TestWriteReadRoundTrip(t *testing.T) {
	var w xrdenc.WBuffer
	w.WriteU8(0x12)
	w.WriteU16(0x1234)
	w.WriteI32(-2)
	w.WriteU32(0xdeadbeef)
	w.WriteI64(-1 << 40)
	w.WriteBool(true)
	w.WriteBool(false)
	w.Next(3)
	w.WriteBytes([]byte("abc"))
	w.WriteStr("go-hep")
	w.WriteLen(7)

	want := []byte{
		0x12,
		0x12, 0x34,
		0xff, 0xff, 0xff, 0xfe,
		0xde, 0xad, 0xbe, 0xef,
		0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x00, 0x00,
		1, 0,
		0, 0, 0,
		'a', 'b', 'c',
		0, 0, 0, 6, 'g', 'o', '-', 'h', 'e', 'p',
		0, 0, 0, 7,
	}
	if got := w.Bytes(); !bytes.Equal(got, want) {
		t.Fatalf("wire encoding changed:\ngot  %x\nwant %x", got, want)
	}

	r := xrdenc.NewRBuffer(w.Bytes())
	if got := r.ReadU8(); got != 0x12 {
		t.Errorf("ReadU8 is %#x, want 0x12", got)
	}
	if got := r.ReadU16(); got != 0x1234 {
		t.Errorf("ReadU16 is %#x, want 0x1234", got)
	}
	if got := r.ReadI32(); got != -2 {
		t.Errorf("ReadI32 is %d, want -2", got)
	}
	if got := r.ReadI32(); uint32(got) != 0xdeadbeef {
		t.Errorf("ReadI32 is %#x, want 0xdeadbeef", uint32(got))
	}
	if got := r.ReadI64(); got != -1<<40 {
		t.Errorf("ReadI64 is %d, want %d", got, int64(-1)<<40)
	}
	if !r.ReadBool() {
		t.Error("ReadBool is false, want true")
	}
	if r.ReadBool() {
		t.Error("ReadBool is true, want false")
	}
	r.Skip(3)
	raw := make([]byte, 3)
	r.ReadBytes(raw)
	if string(raw) != "abc" {
		t.Errorf("ReadBytes is %q, want %q", raw, "abc")
	}
	if got := r.ReadStr(); got != "go-hep" {
		t.Errorf("ReadStr is %q, want %q", got, "go-hep")
	}
	if got := r.ReadLen(); got != 7 {
		t.Errorf("ReadLen is %d, want 7", got)
	}
	if r.Len() != 0 {
		t.Errorf("%d bytes are left over", r.Len())
	}
	if err := r.Err(); err != nil {
		t.Errorf("reading back a buffer that was just written reported %v", err)
	}
}

// TestReadPastTheEnd is the property the decoders rely on: a buffer filled from
// the wire is attacker-controlled, so a read that runs off the end reports
// itself instead of panicking.
func TestReadPastTheEnd(t *testing.T) {
	for _, tc := range []struct {
		name string
		read func(*xrdenc.RBuffer)
	}{
		{"ReadU8", func(r *xrdenc.RBuffer) { r.ReadU8() }},
		{"ReadU16", func(r *xrdenc.RBuffer) { r.ReadU16() }},
		{"ReadI32", func(r *xrdenc.RBuffer) { r.ReadI32() }},
		{"ReadI64", func(r *xrdenc.RBuffer) { r.ReadI64() }},
		{"ReadBool", func(r *xrdenc.RBuffer) { r.ReadBool() }},
		{"ReadBytes", func(r *xrdenc.RBuffer) { r.ReadBytes(make([]byte, 9)) }},
		{"ReadStr", func(r *xrdenc.RBuffer) { r.ReadStr() }},
		{"ReadLenBytes", func(r *xrdenc.RBuffer) { r.ReadLenBytes() }},
		{"Skip", func(r *xrdenc.RBuffer) { r.Skip(9) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// An empty body: the shortest thing a peer can send that is
			// still a message, and less than any of the reads wants.
			r := xrdenc.NewRBuffer(nil)
			func() {
				defer func() {
					if p := recover(); p != nil {
						t.Fatalf("reading past the end panicked: %v", p)
					}
				}()
				tc.read(r)
			}()
			if err := r.Err(); !errors.Is(err, xrdenc.ErrShortBuffer) {
				t.Fatalf("Err is %v, want ErrShortBuffer", err)
			}
			if r.Len() != 0 {
				t.Fatalf("a failed read left %d bytes readable", r.Len())
			}
		})
	}
}

// TestFailedReadsYieldZeroValues: a decoder that reads a whole message before
// checking Err must not be handed data from the wrong offset in the meantime.
func TestFailedReadsYieldZeroValues(t *testing.T) {
	r := xrdenc.NewRBuffer([]byte{1, 2, 3})
	dst := []byte{'a', 'b', 'c', 'd'}
	r.ReadBytes(dst)
	if !bytes.Equal(dst, make([]byte, 4)) {
		t.Errorf("a short ReadBytes left %q in the destination, want it cleared", dst)
	}
	if got := r.ReadI64(); got != 0 {
		t.Errorf("a read after a failure is %d, want 0", got)
	}
	if got := r.ReadStr(); got != "" {
		t.Errorf("a read after a failure is %q, want empty", got)
	}
}

// TestErrIsSticky: the first failure is the one reported, so a decoder that
// checks once at the end still learns about a failure in its first field.
func TestErrIsSticky(t *testing.T) {
	r := xrdenc.NewRBuffer(nil)
	r.ReadI64()
	first := r.Err()
	if first == nil {
		t.Fatal("reading 8 bytes from an empty buffer reported no error")
	}
	r.ReadU8()
	if r.Err() != first {
		t.Fatal("a later failure replaced the first one")
	}
}

// TestReadLenBytesRefusesPeerChosenSizes: the length is read before the bytes
// it counts, so it must never size an allocation on its own.
func TestReadLenBytesRefusesPeerChosenSizes(t *testing.T) {
	for _, n := range []int32{-1, -1 << 31, 1 << 30, 1<<31 - 1} {
		var w xrdenc.WBuffer
		w.WriteI32(n)
		w.WriteBytes([]byte("data"))

		r := xrdenc.NewRBuffer(w.Bytes())
		if got := r.ReadLenBytes(); got != nil {
			t.Errorf("a declared length of %d returned %d bytes", n, len(got))
		}
		if err := r.Err(); !errors.Is(err, xrdenc.ErrShortBuffer) {
			t.Errorf("a declared length of %d reported %v, want ErrShortBuffer", n, err)
		}
	}
}

// TestReadLenBytesCopies: the returned slice outlives the buffer it came from,
// which is reused by the connection reader.
func TestReadLenBytesCopies(t *testing.T) {
	buf := []byte{0, 0, 0, 2, 'h', 'i'}
	got := xrdenc.NewRBuffer(buf).ReadLenBytes()
	if string(got) != "hi" {
		t.Fatalf("ReadLenBytes is %q, want %q", got, "hi")
	}
	buf[4], buf[5] = 'n', 'o'
	if string(got) != "hi" {
		t.Fatalf("ReadLenBytes aliased the buffer: it now reads %q", got)
	}
}

// TestPosAndBytesTrackTheCursor: Bytes returns what is left, not the whole
// buffer, and is how the run-to-the-end fields are decoded.
func TestPosAndBytesTrackTheCursor(t *testing.T) {
	r := xrdenc.NewRBuffer([]byte("0123456789"))
	r.Skip(4)
	if got := r.Pos(); got != 4 {
		t.Errorf("Pos is %d, want 4", got)
	}
	if got := string(r.Bytes()); got != "456789" {
		t.Errorf("Bytes is %q, want %q", got, "456789")
	}
	if got := r.Len(); got != 6 {
		t.Errorf("Len is %d, want 6", got)
	}
}

// TestZeroLengthReadsSucceed: an empty payload is a legal message, not a
// truncated one.
func TestZeroLengthReadsSucceed(t *testing.T) {
	r := xrdenc.NewRBuffer(nil)
	r.ReadBytes(nil)
	r.Skip(0)
	if err := r.Err(); err != nil {
		t.Fatalf("reading nothing from an empty buffer reported %v", err)
	}

	r = xrdenc.NewRBuffer([]byte{0, 0, 0, 0})
	if got := r.ReadStr(); got != "" {
		t.Fatalf("a zero-length string is %q", got)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("a zero-length string reported %v", err)
	}
}
