// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdenc // import "go-hep.org/x/hep/xrootd/internal/xrdenc"

import (
	"encoding/binary"
	"errors"
)

// WBuffer encodes values to a buffer according to the XRootD protocol.
type WBuffer struct {
	buf []byte
}

func (w *WBuffer) Bytes() []byte { return w.buf }

func (w *WBuffer) WriteU8(v uint8) {
	w.buf = append(w.buf, v)
}

func (w *WBuffer) WriteU16(v uint16) {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], v)
	w.buf = append(w.buf, buf[:]...)
}

func (w *WBuffer) WriteI32(v int32) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], uint32(v))
	w.buf = append(w.buf, buf[:]...)
}

func (w *WBuffer) WriteU32(v uint32) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], v)
	w.buf = append(w.buf, buf[:]...)
}

func (w *WBuffer) WriteI64(v int64) {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(v))
	w.buf = append(w.buf, buf[:]...)
}

func (w *WBuffer) WriteBool(v bool) {
	if v {
		w.buf = append(w.buf, 1)
		return
	}
	w.buf = append(w.buf, 0)
}

func (w *WBuffer) WriteLen(n int) {
	w.WriteI32(int32(n))
}

func (w *WBuffer) WriteBytes(vs []byte) {
	w.buf = append(w.buf, vs...)
}

func (w *WBuffer) WriteStr(str string) {
	w.WriteLen(len(str))
	w.WriteBytes([]byte(str))
}

func (w *WBuffer) Next(n int) {
	w.buf = append(w.buf, make([]byte, n)...)
}

// ErrShortBuffer is reported by RBuffer.Err when a read asked for more bytes
// than the buffer holds. The buffer is filled from the wire, so a server that
// sends a truncated or malformed message must not be able to do worse than
// this to the client.
var ErrShortBuffer = errors.New("xrdenc: unexpected end of buffer")

// RBuffer decodes values from a buffer according to the XRootD protocol.
//
// Reads never panic: a read that runs past the end of the buffer consumes what
// is left, yields the zero value for the missing part and latches an error that
// Err reports. Decoders may therefore read a whole message and check once at
// the end instead of guarding every field.
type RBuffer struct {
	buf []byte
	pos int
	err error
}

func NewRBuffer(data []byte) *RBuffer {
	return &RBuffer{buf: data}
}

// Err reports whether any read ran past the end of the buffer.
func (r *RBuffer) Err() error {
	return r.err
}

// grab consumes the next n bytes. It returns nil, and latches ErrShortBuffer,
// if fewer than n bytes are left.
func (r *RBuffer) grab(n int) []byte {
	if n < 0 || n > r.Len() {
		r.pos = len(r.buf)
		if r.err == nil {
			r.err = ErrShortBuffer
		}
		return nil
	}
	beg := r.pos
	r.pos += n
	return r.buf[beg:r.pos]
}

func (r *RBuffer) ReadU8() uint8 {
	buf := r.grab(1)
	if buf == nil {
		return 0
	}
	return buf[0]
}

func (r *RBuffer) Len() int {
	return len(r.buf) - r.pos
}

func (r *RBuffer) ReadU16() uint16 {
	buf := r.grab(2)
	if buf == nil {
		return 0
	}
	return binary.BigEndian.Uint16(buf)
}

func (r *RBuffer) ReadI32() int32 {
	buf := r.grab(4)
	if buf == nil {
		return 0
	}
	return int32(binary.BigEndian.Uint32(buf))
}

func (r *RBuffer) ReadI64() int64 {
	buf := r.grab(8)
	if buf == nil {
		return 0
	}
	return int64(binary.BigEndian.Uint64(buf))
}

func (r *RBuffer) ReadBool() bool {
	return r.ReadU8() != 0
}

func (r *RBuffer) ReadLen() int {
	return int(r.ReadI32())
}

func (r *RBuffer) ReadBytes(data []byte) {
	buf := r.grab(len(data))
	if buf == nil {
		clear(data)
		return
	}
	copy(data, buf)
}

func (r *RBuffer) ReadStr() string {
	return string(r.grab(r.ReadLen()))
}

// ReadLenBytes reads a 32-bit length and returns a copy of that many bytes.
//
// The length comes off the wire, so it is not trusted to size an allocation:
// a length that is negative or larger than what is left latches ErrShortBuffer
// and yields nil.
func (r *RBuffer) ReadLenBytes() []byte {
	buf := r.grab(r.ReadLen())
	if len(buf) == 0 {
		return nil
	}
	out := make([]byte, len(buf))
	copy(out, buf)
	return out
}

func (r *RBuffer) Skip(n int) {
	r.grab(n)
}

func (r *RBuffer) Bytes() []byte {
	return r.buf[r.pos:]
}

func (r *RBuffer) Pos() int {
	return r.pos
}
