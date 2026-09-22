// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rntup

import (
	"fmt"

	"github.com/zeebo/xxh3"
)

// elemWidth returns how many bytes one element of the column takes once it
// has been decoded, which is not what it takes on storage for the packed
// column types.
func elemWidth(c *Column) (int, error) {
	switch c.Type {
	case ColBit:
		return 1, nil // one byte per boolean once unpacked
	case ColReal32Trunc, ColReal32Quant:
		return 4, nil // both widen back into a float32
	}
	bits, ok := c.Type.bits()
	if !ok {
		return 0, fmt.Errorf("rntup: cannot size a %v column", c.Type)
	}
	return bits / 8, nil
}

// storedBytes returns how many bytes n elements of the column take on
// storage, before compression.
func storedBytes(c *Column, n int) (int, error) {
	switch c.Type {
	case ColBit:
		return (n + 7) / 8, nil
	case ColReal32Trunc, ColReal32Quant:
		return (int(c.Bits)*n + 7) / 8, nil
	}
	bits, ok := c.Type.bits()
	if !ok {
		return 0, fmt.Errorf("rntup: cannot size a %v column", c.Type)
	}
	return bits / 8 * n, nil
}

// readPage reads one page and returns its elements, decoded and laid end to
// end in their natural width.
func (r *Reader) readPage(c *Column, pg Page, compr uint32) ([]byte, error) {
	n := int(pg.NElements)
	if n < 0 {
		return nil, fmt.Errorf("rntup: page of column %d holds %d elements", c.ID, n)
	}

	length, err := storedBytes(c, n)
	if err != nil {
		return nil, err
	}

	raw, err := readBlock(r.r, pg.Loc.offset, pg.Loc.size, int64(length))
	if err != nil {
		return nil, fmt.Errorf("rntup: could not read a page of column %d: %w", c.ID, err)
	}

	if pg.Checksum {
		err := r.checkPage(pg)
		if err != nil {
			return nil, fmt.Errorf("rntup: page of column %d: %w", c.ID, err)
		}
	}

	return decodePage(c, raw, n)
}

// checkPage verifies the checksum stored just past a page, which covers the
// page as it sits on disk, compressed.
func (r *Reader) checkPage(pg Page) error {
	raw := make([]byte, pg.Loc.size+checksumLen)
	_, err := r.r.ReadAt(raw, int64(pg.Loc.offset))
	if err != nil {
		return fmt.Errorf("could not read the page and its checksum: %w", err)
	}

	var (
		body = raw[:pg.Loc.size]
		want = newRBuf(raw[pg.Loc.size:]).U64()
	)
	if got := xxh3.Hash(body); got != want {
		return fmt.Errorf("checksum mismatch: got=%#016x, want=%#016x", got, want)
	}
	return nil
}

// colCursor reads elements of one column within one cluster, holding on to
// the page it last decoded so that a walk through the entries decodes each
// page once.
type colCursor struct {
	r     *Reader
	col   *Column
	pages *ColumnPages
	width int

	// the decoded page and the cluster-local element range it covers.
	data     []byte
	beg, end uint64

	// where each page starts, cluster-local, so that a lookup is a search
	// rather than a walk.
	starts []uint64

	// zeros marks a column that has no pages in this cluster because it
	// was added to the schema later. Everything before its first element
	// reads as zero.
	zeros bool

	// skew is how many elements at the start of the cluster a deferred
	// column has no pages for. They read as zero: the column was added to
	// the schema partway through this cluster, and the leading zeros are
	// not written out.
	skew uint64
}

// newZeroCursor returns a cursor over a column that this cluster has no
// pages for, which reads as zero throughout.
func newZeroCursor(r *Reader, col *Column) (*colCursor, error) {
	w, err := elemWidth(col)
	if err != nil {
		return nil, err
	}
	return &colCursor{r: r, col: col, width: w, zeros: true, starts: []uint64{0}}, nil
}

// newColCursor builds a cursor over a column within one cluster. base is the
// element the cluster starts at, which for the columns a deferred column may
// be attached to is the cluster's first entry.
func newColCursor(r *Reader, col *Column, pages *ColumnPages, base uint64) (*colCursor, error) {
	w, err := elemWidth(col)
	if err != nil {
		return nil, err
	}

	c := &colCursor{r: r, col: col, pages: pages, width: w}
	if col.Deferred() && uint64(pages.FirstElem) > base {
		c.skew = uint64(pages.FirstElem) - base
	}
	var at uint64
	for _, pg := range pages.Pages {
		c.starts = append(c.starts, at)
		at += uint64(pg.NElements)
	}
	c.starts = append(c.starts, at)
	return c, nil
}

// len returns how many elements of the column this cluster holds.
func (c *colCursor) len() uint64 { return c.starts[len(c.starts)-1] }

// at returns the bytes of the cluster-local i-th element.
func (c *colCursor) at(i uint64) ([]byte, error) {
	if c.zeros || i < c.skew {
		// before its first element a deferred column reads as zero,
		// which for every type RNTuple supports is the zero value.
		return make([]byte, c.width), nil
	}
	i -= c.skew

	if i >= c.beg && i < c.end {
		off := (i - c.beg) * uint64(c.width)
		return c.data[off : off+uint64(c.width)], nil
	}

	if i >= c.len() {
		// a deferred column reads as zero before its first element, and
		// a column can legitimately run short of a cluster's entries
		// when it was added partway through writing.
		if c.col.Deferred() {
			return make([]byte, c.width), nil
		}
		return nil, fmt.Errorf(
			"rntup: element %d is past the %d elements column %d holds in this cluster",
			i, c.len(), c.col.ID,
		)
	}

	// find the page holding i.
	lo, hi := 0, len(c.pages.Pages)
	for lo < hi-1 {
		mid := (lo + hi) / 2
		if i < c.starts[mid] {
			hi = mid
			continue
		}
		lo = mid
	}

	data, err := c.r.readPage(c.col, c.pages.Pages[lo], c.pages.Compr)
	if err != nil {
		return nil, err
	}

	c.data = data
	c.beg = c.starts[lo]
	c.end = c.starts[lo+1]

	off := (i - c.beg) * uint64(c.width)
	if off+uint64(c.width) > uint64(len(c.data)) {
		return nil, fmt.Errorf("rntup: page of column %d is short of element %d", c.col.ID, i)
	}
	return c.data[off : off+uint64(c.width)], nil
}
