// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rntup

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand/v2"
	"testing"
)

// TestEncodeDecodeAreInverses checks that what a writer does to a page is
// exactly what a reader undoes.
//
// The transforms are where a silent mistake would live: a page that is
// split, delta-encoded or zigzagged wrongly still reads back as numbers, and
// they would be the wrong numbers rather than an error. So every column type
// is taken through both halves and has to come out as it went in.
func TestEncodeDecodeAreInverses(t *testing.T) {
	rnd := rand.New(rand.NewPCG(1234, 5678))

	for _, tc := range []struct {
		typ   ColType
		bits  uint16
		width int
	}{
		{ColInt16, 16, 2}, {ColSplitInt16, 16, 2},
		{ColUInt16, 16, 2}, {ColSplitUInt16, 16, 2},
		{ColInt32, 32, 4}, {ColSplitInt32, 32, 4},
		{ColUInt32, 32, 4}, {ColSplitUInt32, 32, 4},
		{ColInt64, 64, 8}, {ColSplitInt64, 64, 8},
		{ColUInt64, 64, 8}, {ColSplitUInt64, 64, 8},
		{ColReal32, 32, 4}, {ColSplitReal32, 32, 4},
		{ColReal64, 64, 8}, {ColSplitReal64, 64, 8},
		{ColIndex64, 64, 8}, {ColSplitIndex64, 64, 8},
		{ColIndex32, 32, 4}, {ColSplitIndex32, 32, 4},
	} {
		t.Run(tc.typ.String(), func(t *testing.T) {
			const n = 257 // not a whole number of anything, on purpose

			col := &Column{Type: tc.typ, Bits: tc.bits}

			// an index column holds offsets, which only ever grow;
			// anything else holds whatever it likes.
			want := make([]byte, tc.width*n)
			var running uint64
			for i := range n {
				var v uint64
				switch {
				case tc.typ.index():
					running += uint64(rnd.IntN(5))
					v = running
				default:
					v = rnd.Uint64()
				}
				switch tc.width {
				case 2:
					binary.LittleEndian.PutUint16(want[i*2:], uint16(v))
				case 4:
					binary.LittleEndian.PutUint32(want[i*4:], uint32(v))
				case 8:
					binary.LittleEndian.PutUint64(want[i*8:], v)
				}
			}

			enc, err := encodePage(col, want, n)
			if err != nil {
				t.Fatalf("could not encode: %+v", err)
			}
			if got, want := len(enc), tc.width*n; got != want {
				t.Fatalf("encoded to %d bytes, want %d", got, want)
			}

			got, err := decodePage(col, enc, n)
			if err != nil {
				t.Fatalf("could not decode: %+v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("the round trip changed the elements of a %v column", tc.typ)
			}

			// a split column should actually have moved the bytes,
			// or it is not doing anything.
			if tc.typ.split() && bytes.Equal(enc, want) {
				t.Errorf("a %v column encoded to the bytes it was given", tc.typ)
			}
			if !tc.typ.split() && !bytes.Equal(enc, want) {
				t.Errorf("a %v column should be stored as it stands", tc.typ)
			}
		})
	}
}

// TestSplitPutsLikeBytesTogether checks that splitting does what it is for:
// a run of similar numbers should end up with its high bytes in one place.
func TestSplitPutsLikeBytesTogether(t *testing.T) {
	const n = 64

	col := &Column{Type: ColSplitReal64, Bits: 64}

	// values that differ only in their low bits, as a measured quantity
	// does.
	data := make([]byte, 8*n)
	for i := range n {
		binary.LittleEndian.PutUint64(data[i*8:], math.Float64bits(1000+float64(i)*1e-9))
	}

	enc, err := encodePage(col, data, n)
	if err != nil {
		t.Fatalf("could not encode: %+v", err)
	}

	// the last byte of a little-endian double is the top of its exponent,
	// which is the same for all of these, so after splitting the final
	// stretch of the page should be one byte repeated.
	tail := enc[len(enc)-n:]
	for i, b := range tail {
		if b != tail[0] {
			t.Fatalf("byte %d of the last run is %#x, want %#x: splitting did not group them", i, b, tail[0])
		}
	}
}

// TestZigzagKeepsSmallNegativesSmall checks the mapping signed columns go
// through, which is the whole reason for it.
func TestZigzagKeepsSmallNegativesSmall(t *testing.T) {
	const n = 4

	data := make([]byte, 4*n)
	for i, v := range []int32{0, -1, 1, -2} {
		binary.LittleEndian.PutUint32(data[i*4:], uint32(v))
	}

	buf := make([]byte, len(data))
	copy(buf, data)
	zigzag(buf, 4, n)

	// zigzag sends 0,-1,1,-2 to 0,1,2,3.
	for i, want := range []uint32{0, 1, 2, 3} {
		if got := binary.LittleEndian.Uint32(buf[i*4:]); got != want {
			t.Errorf("element %d: got=%d, want=%d", i, got, want)
		}
	}

	unzigzag(buf, 4, n)
	if !bytes.Equal(buf, data) {
		t.Error("zigzag and its inverse did not come back to where they started")
	}
}
