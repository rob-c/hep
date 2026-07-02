// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pgbuf

import (
	"bytes"
	"testing"
)

func TestEncodeDecodeAligned(t *testing.T) {
	data := bytes.Repeat([]byte{0xab}, 2*PageSize+100) // 2 full pages + short tail
	frame := Encode(0, data)
	wantLen := (2*PageSize + 100) + 3*4 // 3 units, 4-byte crc each
	if len(frame) != wantLen {
		t.Fatalf("encoded length: got=%d want=%d", len(frame), wantLen)
	}
	if got := EncodedLen(0, len(data)); got != wantLen {
		t.Fatalf("EncodedLen: got=%d want=%d", got, wantLen)
	}
	back, err := Decode(0, frame)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(back, data) {
		t.Fatal("roundtrip mismatch")
	}
}

func TestEncodeUnalignedFirstPage(t *testing.T) {
	// Starting at offset 4000: first unit carries only 96 bytes to realign.
	data := bytes.Repeat([]byte{0x5c}, 96+PageSize)
	frame := Encode(4000, data)
	wantLen := (96 + 4) + (PageSize + 4)
	if len(frame) != wantLen {
		t.Fatalf("encoded length: got=%d want=%d", len(frame), wantLen)
	}
	back, err := Decode(4000, frame)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(back, data) {
		t.Fatal("roundtrip mismatch")
	}
}

func TestDecodeCorruptPage(t *testing.T) {
	frame := Encode(0, bytes.Repeat([]byte{1}, PageSize))
	frame[10] ^= 0xff
	if _, err := Decode(0, frame); err == nil {
		t.Fatal("Decode accepted a corrupted page")
	}
}

func TestDecodeTruncatedUnit(t *testing.T) {
	frame := Encode(0, bytes.Repeat([]byte{1}, 100))
	if _, err := Decode(0, frame[:3]); err == nil {
		t.Fatal("Decode accepted a truncated unit header")
	}
}
