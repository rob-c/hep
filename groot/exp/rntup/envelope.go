// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rntup

import (
	"bytes"
	"fmt"
	"io"

	"github.com/zeebo/xxh3"
	"go-hep.org/x/hep/groot/internal/rcompress"
)

// envelope type IDs.
const (
	envHeader   = 0x01
	envFooter   = 0x02
	envPageList = 0x03
)

// checksumLen is the size of the XxHash-3 checksum an envelope ends with.
const checksumLen = 8

func envName(kind uint16) string {
	switch kind {
	case envHeader:
		return "header"
	case envFooter:
		return "footer"
	case envPageList:
		return "page list"
	default:
		return fmt.Sprintf("envelope(%#02x)", kind)
	}
}

// readBlock reads nbytes at off and decompresses it to length bytes.
//
// RNTuple wraps its envelopes and its pages in the same compression blocks
// ROOT uses elsewhere, so a block that did not shrink is stored verbatim.
func readBlock(r io.ReaderAt, off uint64, nbytes, length int64) ([]byte, error) {
	if nbytes < 0 || length < 0 {
		return nil, fmt.Errorf("rntup: block at %d has a negative size", off)
	}

	raw := make([]byte, nbytes)
	_, err := r.ReadAt(raw, int64(off))
	if err != nil {
		return nil, fmt.Errorf("rntup: could not read %d bytes at %d: %w", nbytes, off, err)
	}

	if nbytes == length {
		return raw, nil
	}

	buf := make([]byte, length)
	err = rcompress.Decompress(buf, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("rntup: could not decompress the block at %d: %w", off, err)
	}
	return buf, nil
}

// readEnvelope reads the envelope of the given kind at off, checks it and
// returns a cursor over its payload, with the envelope header and the
// trailing checksum trimmed off.
func readEnvelope(r io.ReaderAt, kind uint16, off uint64, nbytes, length int64) (*rbuf, error) {
	raw, err := readBlock(r, off, nbytes, length)
	if err != nil {
		return nil, fmt.Errorf("rntup: could not read the %s envelope: %w", envName(kind), err)
	}
	return parseEnvelope(raw, kind)
}

// parseEnvelope checks an uncompressed envelope and returns a cursor over
// its payload.
func parseEnvelope(raw []byte, kind uint16) (*rbuf, error) {
	const hdrLen = 8

	if len(raw) < hdrLen+checksumLen {
		return nil, fmt.Errorf(
			"rntup: %s envelope is %d bytes, too short to hold a header and a checksum",
			envName(kind), len(raw),
		)
	}

	buf := newRBuf(raw)
	var (
		word = buf.U64()
		got  = uint16(word & 0xffff)
		size = word >> 16
	)
	if got != kind {
		return nil, fmt.Errorf(
			"rntup: expected a %s envelope, got %s",
			envName(kind), envName(got),
		)
	}
	if size != uint64(len(raw)) {
		return nil, fmt.Errorf(
			"rntup: %s envelope declares %d bytes but %d were read",
			envName(kind), size, len(raw),
		)
	}

	var (
		end  = len(raw) - checksumLen
		want = newRBuf(raw[end:]).U64()
	)
	if sum := xxh3.Hash(raw[:end]); sum != want {
		return nil, fmt.Errorf(
			"rntup: %s envelope checksum mismatch: got=%#016x, want=%#016x",
			envName(kind), sum, want,
		)
	}

	// hand back a cursor positioned just past the envelope header and
	// stopping short of the checksum, so that a frame running into either
	// is reported rather than parsed.
	buf = newRBuf(raw[:end])
	buf.seek(hdrLen)
	return buf, buf.Err()
}

// featureFlags reads the list of feature flags, which runs on for as long as
// the top bit of each 64-bit word is set.
//
// A flag this reader does not know means the file uses something that would
// be misread if ignored, so the caller refuses the file rather than guessing.
func (r *rbuf) featureFlags() ([]uint64, error) {
	var flags []uint64
	for {
		f := r.U64()
		if r.Err() != nil {
			return nil, r.Err()
		}
		flags = append(flags, f&^(1<<63))
		if int64(f) >= 0 {
			break
		}
	}

	for i, f := range flags {
		known := uint64(0)
		if i == 0 {
			known = featNestedDeferredColumns
		}
		if rest := f &^ known; rest != 0 {
			return nil, fmt.Errorf(
				"rntup: unsupported feature flags %#016x in word %d", rest, i,
			)
		}
	}
	return flags, nil
}

// the feature flags this reader knows about.
const (
	// featNestedDeferredColumns marks an RNTuple holding a deferred column
	// inside a collection, which happens when two RNTuples whose collection
	// used different column encodings are merged.
	featNestedDeferredColumns = 1 << 0
)
