// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rntup

import (
	"encoding/binary"

	"github.com/zeebo/xxh3"
)

// wbuf builds the payload of an RNTuple envelope.
//
// It is the other half of rbuf: little-endian integers, length-prefixed
// strings, and frames whose size is not known until what is in them has been
// written and so is filled in afterwards.
type wbuf struct {
	p []byte

	// lists remembers which frames were opened as lists, since a list
	// carries its size negated and the size is not written until the
	// frame closes.
	lists []int
}

func (w *wbuf) bytes() []byte { return w.p }
func (w *wbuf) len() int      { return len(w.p) }

func (w *wbuf) u8(v uint8) { w.p = append(w.p, v) }

func (w *wbuf) u16(v uint16) {
	w.p = binary.LittleEndian.AppendUint16(w.p, v)
}

func (w *wbuf) u32(v uint32) {
	w.p = binary.LittleEndian.AppendUint32(w.p, v)
}

func (w *wbuf) u64(v uint64) {
	w.p = binary.LittleEndian.AppendUint64(w.p, v)
}

func (w *wbuf) i32(v int32) { w.u32(uint32(v)) }
func (w *wbuf) i64(v int64) { w.u64(uint64(v)) }

func (w *wbuf) str(s string) {
	w.u32(uint32(len(s)))
	w.p = append(w.p, s...)
}

func (w *wbuf) raw(p []byte) { w.p = append(w.p, p...) }

// record opens a record frame and returns the mark that closes it.
func (w *wbuf) record() int {
	mark := len(w.p)
	w.i64(0) // the size, filled in by close
	return mark
}

// list opens a list frame of n items.
func (w *wbuf) list(n int) int {
	mark := len(w.p)
	w.i64(0)
	w.u32(uint32(n))
	w.lists = append(w.lists, mark)
	return mark
}

// close fills in the size of the frame the mark opened.
//
// A list frame carries its size negated, which is how a reader tells the two
// apart, so the sign is taken from whether a count was written.
func (w *wbuf) close(mark int) {
	size := int64(len(w.p) - mark)
	if w.isList(mark) {
		size = -size
	}
	binary.LittleEndian.PutUint64(w.p[mark:], uint64(size))
}

// isList reports whether the frame at the mark was opened as a list, which
// is remembered by the marks list rather than read back out of the buffer.
func (w *wbuf) isList(mark int) bool {
	for _, m := range w.lists {
		if m == mark {
			return true
		}
	}
	return false
}

// locator writes a byte range on disk.
func (w *wbuf) locator(size int64, offset uint64) {
	w.i32(int32(size))
	w.u64(offset)
}

// envelopeLink writes an uncompressed length followed by a locator.
func (w *wbuf) envelopeLink(length uint64, size int64, offset uint64) {
	w.u64(length)
	w.locator(size, offset)
}

// envelope wraps a payload in the header and checksum an envelope carries.
func envelope(kind uint16, payload []byte) []byte {
	const hdrLen = 8

	total := hdrLen + len(payload) + checksumLen

	out := make([]byte, 0, total)
	out = binary.LittleEndian.AppendUint64(out, uint64(kind)|uint64(total)<<16)
	out = append(out, payload...)
	out = binary.LittleEndian.AppendUint64(out, xxh3.Hash(out))

	return out
}
