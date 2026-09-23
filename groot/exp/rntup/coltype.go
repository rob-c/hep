// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rntup

import (
	"encoding/binary"
	"fmt"
	"math"
)

// ColType is the on-disk encoding of a column's elements.
type ColType uint16

const (
	ColBit          ColType = 0x00 // a boolean, one bit per element
	ColByte         ColType = 0x01 // an uninterpreted byte
	ColChar         ColType = 0x02
	ColInt8         ColType = 0x03
	ColUInt8        ColType = 0x04
	ColInt16        ColType = 0x05
	ColUInt16       ColType = 0x06
	ColInt32        ColType = 0x07
	ColUInt32       ColType = 0x08
	ColInt64        ColType = 0x09
	ColUInt64       ColType = 0x0a
	ColReal16       ColType = 0x0b
	ColReal32       ColType = 0x0c
	ColReal64       ColType = 0x0d
	ColIndex32      ColType = 0x0e // offsets into a collection, counted within the cluster
	ColIndex64      ColType = 0x0f
	ColSwitch       ColType = 0x10 // an Index64 and a 32-bit tag naming a column
	ColSplitInt16   ColType = 0x11
	ColSplitUInt16  ColType = 0x12
	ColSplitInt32   ColType = 0x13
	ColSplitUInt32  ColType = 0x14
	ColSplitInt64   ColType = 0x15
	ColSplitUInt64  ColType = 0x16
	ColSplitReal16  ColType = 0x17
	ColSplitReal32  ColType = 0x18
	ColSplitReal64  ColType = 0x19
	ColSplitIndex32 ColType = 0x1a
	ColSplitIndex64 ColType = 0x1b
	ColReal32Trunc  ColType = 0x1c // a float32 with the low mantissa bits dropped
	ColReal32Quant  ColType = 0x1d // a real quantized into an integer over a range
)

var colNames = map[ColType]string{
	ColBit: "Bit", ColByte: "Byte", ColChar: "Char",
	ColInt8: "Int8", ColUInt8: "UInt8",
	ColInt16: "Int16", ColUInt16: "UInt16",
	ColInt32: "Int32", ColUInt32: "UInt32",
	ColInt64: "Int64", ColUInt64: "UInt64",
	ColReal16: "Real16", ColReal32: "Real32", ColReal64: "Real64",
	ColIndex32: "Index32", ColIndex64: "Index64", ColSwitch: "Switch",
	ColSplitInt16: "SplitInt16", ColSplitUInt16: "SplitUInt16",
	ColSplitInt32: "SplitInt32", ColSplitUInt32: "SplitUInt32",
	ColSplitInt64: "SplitInt64", ColSplitUInt64: "SplitUInt64",
	ColSplitReal16: "SplitReal16", ColSplitReal32: "SplitReal32",
	ColSplitReal64:  "SplitReal64",
	ColSplitIndex32: "SplitIndex32", ColSplitIndex64: "SplitIndex64",
	ColReal32Trunc: "Real32Trunc", ColReal32Quant: "Real32Quant",
}

func (c ColType) String() string {
	if s, ok := colNames[c]; ok {
		return s
	}
	return fmt.Sprintf("ColType(%#02x)", uint16(c))
}

// bits returns the number of bits one element takes on storage, for the
// column types whose width is fixed.
func (c ColType) bits() (int, bool) {
	switch c {
	case ColBit:
		return 1, true
	case ColByte, ColChar, ColInt8, ColUInt8:
		return 8, true
	case ColInt16, ColUInt16, ColReal16,
		ColSplitInt16, ColSplitUInt16, ColSplitReal16:
		return 16, true
	case ColInt32, ColUInt32, ColReal32, ColIndex32,
		ColSplitInt32, ColSplitUInt32, ColSplitReal32, ColSplitIndex32:
		return 32, true
	case ColInt64, ColUInt64, ColReal64, ColIndex64,
		ColSplitInt64, ColSplitUInt64, ColSplitReal64, ColSplitIndex64:
		return 64, true
	case ColSwitch:
		return 96, true
	case ColReal32Trunc, ColReal32Quant:
		// variable width: the header says how wide.
		return 0, false
	default:
		return 0, false
	}
}

// split reports whether the column's pages have their bytes rearranged, all
// the first bytes first, then all the second bytes, and so on.
func (c ColType) split() bool {
	switch c {
	case ColSplitInt16, ColSplitUInt16, ColSplitInt32, ColSplitUInt32,
		ColSplitInt64, ColSplitUInt64, ColSplitReal16, ColSplitReal32,
		ColSplitReal64, ColSplitIndex32, ColSplitIndex64:
		return true
	}
	return false
}

// zigzag reports whether signed values are mapped onto unsigned ones before
// splitting, so that small negative numbers stay small.
func (c ColType) zigzag() bool {
	switch c {
	case ColSplitInt16, ColSplitInt32, ColSplitInt64:
		return true
	}
	return false
}

// delta reports whether each element is stored as its difference from the
// one before, which is how collection offsets are kept.
func (c ColType) delta() bool {
	switch c {
	case ColSplitIndex32, ColSplitIndex64:
		return true
	}
	return false
}

// index reports whether the column holds collection offsets.
func (c ColType) index() bool {
	switch c {
	case ColIndex32, ColIndex64, ColSplitIndex32, ColSplitIndex64:
		return true
	}
	return false
}

// unsplit undoes the byte transposition a split column's page went through.
//
// The writer emitted all of the elements' first bytes, then all of their
// second bytes, and so on; this puts each element's bytes back together.
func unsplit(dst, src []byte, width, n int) {
	for b := range width {
		var (
			off = b * n
			p   = src[off : off+n]
		)
		for i, v := range p {
			dst[i*width+b] = v
		}
	}
}

// undelta turns a run of differences back into the values they came from.
func undelta(p []byte, width, n int) {
	switch width {
	case 4:
		var sum uint32
		for i := range n {
			sum += binary.LittleEndian.Uint32(p[i*4:])
			binary.LittleEndian.PutUint32(p[i*4:], sum)
		}
	case 8:
		var sum uint64
		for i := range n {
			sum += binary.LittleEndian.Uint64(p[i*8:])
			binary.LittleEndian.PutUint64(p[i*8:], sum)
		}
	}
}

// unzigzag maps the unsigned values back onto the signed ones they encode.
func unzigzag(p []byte, width, n int) {
	switch width {
	case 2:
		for i := range n {
			v := binary.LittleEndian.Uint16(p[i*2:])
			binary.LittleEndian.PutUint16(p[i*2:], uint16(int16(v>>1)^-int16(v&1)))
		}
	case 4:
		for i := range n {
			v := binary.LittleEndian.Uint32(p[i*4:])
			binary.LittleEndian.PutUint32(p[i*4:], uint32(int32(v>>1)^-int32(v&1)))
		}
	case 8:
		for i := range n {
			v := binary.LittleEndian.Uint64(p[i*8:])
			binary.LittleEndian.PutUint64(p[i*8:], uint64(int64(v>>1)^-int64(v&1)))
		}
	}
}

// decodePage turns the raw bytes of a page into the column's elements laid
// out one after another in their natural width, little-endian.
//
// The encodings are applied within a page, so a page is the unit that has to
// be decoded whole.
func decodePage(c *Column, raw []byte, n int) ([]byte, error) {
	switch c.Type {
	case ColBit:
		// one bit per element, packed least-significant bit first. give
		// the caller one byte per element instead.
		need := (n + 7) / 8
		if len(raw) < need {
			return nil, fmt.Errorf("rntup: bit page holds %d bytes, want %d for %d elements", len(raw), need, n)
		}
		out := make([]byte, n)
		for i := range n {
			out[i] = (raw[i/8] >> (i % 8)) & 1
		}
		return out, nil

	case ColReal32Trunc:
		return decodeTrunc(c, raw, n)

	case ColReal32Quant:
		return decodeQuant(c, raw, n)
	}

	bits, ok := c.Type.bits()
	if !ok {
		return nil, fmt.Errorf("rntup: cannot decode a %v column", c.Type)
	}
	width := bits / 8

	if len(raw) < width*n {
		return nil, fmt.Errorf(
			"rntup: %v page holds %d bytes, want %d for %d elements",
			c.Type, len(raw), width*n, n,
		)
	}
	raw = raw[:width*n]

	out := make([]byte, width*n)
	switch {
	case c.Type.split():
		unsplit(out, raw, width, n)
	default:
		copy(out, raw)
	}

	switch {
	case c.Type.delta():
		undelta(out, width, n)
	case c.Type.zigzag():
		unzigzag(out, width, n)
	}

	return out, nil
}

// decodeTrunc expands floats whose low mantissa bits were dropped back into
// full float32s. The kept bits are the most significant ones, packed
// end to end with no padding between elements.
func decodeTrunc(c *Column, raw []byte, n int) ([]byte, error) {
	bits := int(c.Bits)
	if bits < 10 || bits > 31 {
		return nil, fmt.Errorf("rntup: Real32Trunc column with %d bits on storage", bits)
	}
	if need := (bits*n + 7) / 8; len(raw) < need {
		return nil, fmt.Errorf("rntup: truncated page holds %d bytes, want %d", len(raw), need)
	}

	out := make([]byte, 4*n)
	for i := range n {
		v := uint32(readBits(raw, i*bits, bits))
		binary.LittleEndian.PutUint32(out[i*4:], v<<(32-bits))
	}
	return out, nil
}

// decodeQuant expands integers back into the reals they stand for, spread
// evenly over the range the column declares.
func decodeQuant(c *Column, raw []byte, n int) ([]byte, error) {
	bits := int(c.Bits)
	if bits < 1 || bits > 32 {
		return nil, fmt.Errorf("rntup: Real32Quant column with %d bits on storage", bits)
	}
	if need := (bits*n + 7) / 8; len(raw) < need {
		return nil, fmt.Errorf("rntup: quantized page holds %d bytes, want %d", len(raw), need)
	}

	// with all bits set the value is Max, so the divisor is one short of
	// a power of two. a single-bit column can only say Min or Max.
	scale := float64(uint64(1)<<uint(bits)) - 1

	out := make([]byte, 4*n)
	for i := range n {
		q := float64(readBits(raw, i*bits, bits))
		v := c.Min + (c.Max-c.Min)*q/scale
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(float32(v)))
	}
	return out, nil
}

// readBits pulls n bits out of p starting at bit off, least significant bit
// of each byte first.
func readBits(p []byte, off, n int) uint64 {
	var v uint64
	for i := range n {
		var (
			bit  = off + i
			byt  = bit / 8
			mask = byte(1) << (bit % 8)
		)
		if p[byt]&mask != 0 {
			v |= 1 << uint(i)
		}
	}
	return v
}

// split rearranges the bytes of a page the way a split column wants them:
// all of the elements' first bytes, then all of their second bytes, and so
// on. It is what unsplit undoes.
//
// Nothing is lost and nothing is gained by itself. What it buys is that the
// bytes of a column now sit next to the bytes that resemble them — the high
// bytes of a run of similar numbers are mostly the same, the low bytes are
// mostly noise — which is a great deal easier to compress.
func split(dst, src []byte, width, n int) {
	for b := range width {
		var (
			off = b * n
			p   = dst[off : off+n]
		)
		for i := range n {
			p[i] = src[i*width+b]
		}
	}
}

// delta replaces each element with its difference from the one before,
// leaving the first alone. It is what undelta undoes.
func delta(p []byte, width, n int) {
	switch width {
	case 4:
		var prev uint32
		for i := range n {
			v := binary.LittleEndian.Uint32(p[i*4:])
			binary.LittleEndian.PutUint32(p[i*4:], v-prev)
			prev = v
		}
	case 8:
		var prev uint64
		for i := range n {
			v := binary.LittleEndian.Uint64(p[i*8:])
			binary.LittleEndian.PutUint64(p[i*8:], v-prev)
			prev = v
		}
	}
}

// zigzag maps signed values onto unsigned ones so that small negative
// numbers stay small. It is what unzigzag undoes.
func zigzag(p []byte, width, n int) {
	switch width {
	case 2:
		for i := range n {
			v := int16(binary.LittleEndian.Uint16(p[i*2:]))
			binary.LittleEndian.PutUint16(p[i*2:], uint16(v<<1^v>>15))
		}
	case 4:
		for i := range n {
			v := int32(binary.LittleEndian.Uint32(p[i*4:]))
			binary.LittleEndian.PutUint32(p[i*4:], uint32(v<<1^v>>31))
		}
	case 8:
		for i := range n {
			v := int64(binary.LittleEndian.Uint64(p[i*8:]))
			binary.LittleEndian.PutUint64(p[i*8:], uint64(v<<1^v>>63))
		}
	}
}

// encodePage turns a column's elements, laid out one after another in their
// natural width, into the bytes its encoding calls for.
//
// It is decodePage backwards, and in the other order: a reader puts the
// bytes back together before undoing the delta or the zigzag, so a writer
// has to apply those first and rearrange the bytes last.
func encodePage(c *Column, data []byte, n int) ([]byte, error) {
	if !c.Type.split() {
		return data, nil
	}

	bits, ok := c.Type.bits()
	if !ok {
		return nil, fmt.Errorf("rntup: cannot encode a %v column", c.Type)
	}
	width := bits / 8

	if len(data) < width*n {
		return nil, fmt.Errorf(
			"rntup: %v column holds %d bytes, want %d for %d elements",
			c.Type, len(data), width*n, n,
		)
	}

	// the transforms work in place, so on a copy: what was handed over
	// belongs to the caller.
	buf := make([]byte, width*n)
	copy(buf, data)

	switch {
	case c.Type.delta():
		delta(buf, width, n)
	case c.Type.zigzag():
		zigzag(buf, width, n)
	}

	out := make([]byte, width*n)
	split(out, buf, width, n)
	return out, nil
}
