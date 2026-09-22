// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rntup

import (
	"encoding/binary"
	"fmt"
	"math"
)

// rbuf walks the payload of an RNTuple envelope.
//
// RNTuple payloads use their own encoding, unrelated to the ROOT streamers:
// every integer is little-endian, and a string is a 32-bit length followed
// by its UTF-8 bytes.
type rbuf struct {
	p   []byte
	c   int
	err error
}

func newRBuf(p []byte) *rbuf { return &rbuf{p: p} }

func (r *rbuf) Err() error { return r.err }
func (r *rbuf) Pos() int   { return r.c }

func (r *rbuf) setErr(err error) {
	if r.err == nil {
		r.err = err
	}
}

// next reserves n bytes and returns them, or nil once the buffer is spent.
func (r *rbuf) next(n int) []byte {
	if r.err != nil {
		return nil
	}
	if r.c+n > len(r.p) {
		r.setErr(fmt.Errorf("rntup: read of %d bytes past the end of a %d byte envelope (at %d)", n, len(r.p), r.c))
		return nil
	}
	beg := r.c
	r.c += n
	return r.p[beg:r.c]
}

func (r *rbuf) U8() uint8 {
	p := r.next(1)
	if p == nil {
		return 0
	}
	return p[0]
}

func (r *rbuf) U16() uint16 {
	p := r.next(2)
	if p == nil {
		return 0
	}
	return binary.LittleEndian.Uint16(p)
}

func (r *rbuf) U32() uint32 {
	p := r.next(4)
	if p == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(p)
}

func (r *rbuf) U64() uint64 {
	p := r.next(8)
	if p == nil {
		return 0
	}
	return binary.LittleEndian.Uint64(p)
}

func (r *rbuf) I16() int16 { return int16(r.U16()) }
func (r *rbuf) I32() int32 { return int32(r.U32()) }
func (r *rbuf) I64() int64 { return int64(r.U64()) }

func (r *rbuf) F64() float64 { return math.Float64frombits(r.U64()) }

// String reads a length-prefixed UTF-8 string.
func (r *rbuf) String() string {
	n := int(r.U32())
	if r.err != nil {
		return ""
	}
	p := r.next(n)
	if p == nil {
		return ""
	}
	return string(p)
}

// skip advances past n bytes.
func (r *rbuf) skip(n int) {
	_ = r.next(n)
}

// seek moves to an absolute offset in the envelope.
func (r *rbuf) seek(pos int) {
	if r.err != nil {
		return
	}
	if pos < 0 || pos > len(r.p) {
		r.setErr(fmt.Errorf("rntup: seek to %d outside a %d byte envelope", pos, len(r.p)))
		return
	}
	r.c = pos
}

// frame is a record or list frame: a length-delimited region of an envelope.
//
// Readers are expected to skip to the frame's end using the size it declares
// rather than by adding up what they understood, so that a frame written by
// a newer version of the format, carrying fields this reader does not know
// about, still leaves the cursor in the right place.
type frame struct {
	beg   int   // offset of the frame, including its size field
	size  int64 // size in bytes of the frame and its payload
	list  bool
	items uint32
}

// end returns the offset just past the frame.
func (f frame) end() int { return f.beg + int(f.size) }

// frame reads a frame header at the cursor.
func (r *rbuf) frame() frame {
	var (
		beg  = r.c
		size = r.I64()
	)
	if r.err != nil {
		return frame{}
	}

	f := frame{beg: beg, size: size, list: size < 0}
	if f.list {
		f.size = -size
		f.items = r.U32()
	}
	switch {
	case f.size < 8:
		r.setErr(fmt.Errorf("rntup: frame at %d declares a size of %d bytes", beg, f.size))
	case beg+int(f.size) > len(r.p):
		r.setErr(fmt.Errorf("rntup: frame at %d runs %d bytes past the end of a %d byte envelope", beg, beg+int(f.size)-len(r.p), len(r.p)))
	}
	return f
}

// list reads a list frame, failing if a record frame is found instead.
func (r *rbuf) list() frame {
	f := r.frame()
	if r.err == nil && !f.list {
		r.setErr(fmt.Errorf("rntup: expected a list frame at %d, got a record frame", f.beg))
	}
	return f
}

// record reads a record frame, failing if a list frame is found instead.
func (r *rbuf) record() frame {
	f := r.frame()
	if r.err == nil && f.list {
		r.setErr(fmt.Errorf("rntup: expected a record frame at %d, got a list frame", f.beg))
	}
	return f
}

// done moves the cursor to the end of the frame, whatever was read from it.
func (r *rbuf) done(f frame) {
	if r.err != nil {
		return
	}
	r.seek(f.end())
}

// locator is a byte range on the storage medium.
type locator struct {
	size   int64  // compressed size of the block
	offset uint64 // byte offset from the start of the file
}

func (r *rbuf) locator() locator {
	size := r.I32()
	if r.err != nil {
		return locator{}
	}
	if size >= 0 {
		// the ordinary in-file locator: a size and an offset.
		return locator{size: int64(size), offset: r.U64()}
	}

	// a non-standard locator. the low 16 bits give its own size and the
	// next 8 bits its type, so that a reader can step over the kinds it
	// does not know.
	var (
		beg  = r.c - 4
		self = int(uint32(size) & 0xffff)
		kind = int(int8((uint32(size) >> 24) & 0xff))
	)
	if kind < 0 {
		kind = -kind
	}
	switch kind {
	case 0x01: // large locator: a 64-bit size and a 64-bit offset.
		loc := locator{size: r.I64(), offset: r.U64()}
		r.seek(beg + self)
		return loc
	default:
		r.setErr(fmt.Errorf("rntup: unsupported locator type %#02x", kind))
		return locator{}
	}
}

// envelopeLink is an uncompressed length followed by a locator.
type envelopeLink struct {
	length uint64
	loc    locator
}

func (r *rbuf) envelopeLink() envelopeLink {
	return envelopeLink{length: r.U64(), loc: r.locator()}
}
