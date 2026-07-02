// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package pgbuf implements the page-unit codec shared by kXR_pgread and
// kXR_pgwrite: data is carried as [crc32c BE u32][chunk] units where chunks
// align to PageSize boundaries of the *file* offset, so the first chunk of
// an unaligned request is short.
package pgbuf

import (
	"encoding/binary"
	"fmt"

	"go-hep.org/x/hep/xrootd/xrdsum"
)

// PageSize is kXR_pgPageSZ: the page length pg data units align to.
const PageSize = 4096

// firstChunk returns the length of the first data chunk for a transfer
// starting at file offset off with n bytes remaining.
func firstChunk(off int64, n int) int {
	c := PageSize - int(off%PageSize)
	if c > n {
		c = n
	}
	return c
}

// EncodedLen returns the number of bytes Encode produces for n data bytes
// starting at file offset off.
func EncodedLen(off int64, n int) int {
	if n == 0 {
		return 0
	}
	units := 1
	rest := n - firstChunk(off, n)
	units += rest / PageSize
	if rest%PageSize != 0 {
		units++
	}
	return n + 4*units
}

// Encode splits data into page units with per-page CRC-32C headers.
func Encode(off int64, data []byte) []byte {
	out := make([]byte, 0, EncodedLen(off, len(data)))
	for len(data) > 0 {
		c := firstChunk(off, len(data))
		var crc [4]byte
		binary.BigEndian.PutUint32(crc[:], xrdsum.CRC32C(data[:c]))
		out = append(out, crc[:]...)
		out = append(out, data[:c]...)
		data = data[c:]
		off += int64(c)
	}
	return out
}

// Decode verifies and strips the page-unit framing from frame, which carries
// data starting at file offset off. It returns the reassembled data or an
// error naming the file offset of the first corrupt or malformed unit.
func Decode(off int64, frame []byte) ([]byte, error) {
	out := make([]byte, 0, len(frame))
	for len(frame) > 0 {
		if len(frame) < 4 {
			return nil, fmt.Errorf("xrootd: malformed page unit at offset %d: %d trailing bytes", off, len(frame))
		}
		want := binary.BigEndian.Uint32(frame[:4])
		frame = frame[4:]
		c := PageSize - int(off%PageSize)
		if c > len(frame) {
			c = len(frame)
		}
		if got := xrdsum.CRC32C(frame[:c]); got != want {
			return nil, fmt.Errorf("xrootd: page CRC mismatch at offset %d: got=%#08x want=%#08x", off, got, want)
		}
		out = append(out, frame[:c]...)
		frame = frame[c:]
		off += int64(c)
	}
	return out, nil
}
