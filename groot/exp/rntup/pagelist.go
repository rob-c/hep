// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rntup

import "fmt"

// Page is one block of a column's elements on storage.
type Page struct {
	NElements int32 // how many elements the page holds
	Checksum  bool  // whether an XxHash-3 of the compressed page follows it
	Loc       locator
}

// ColumnPages is what one cluster holds for one column.
type ColumnPages struct {
	Pages      []Page
	FirstElem  int64 // the index, within the column, of this cluster's first element
	Compr      uint32
	Suppressed bool // the column's other representation is the live one here
}

// Cluster is a run of entries, held column by column.
type Cluster struct {
	FirstEntry uint64
	NEntries   uint64
	Flags      uint8

	Columns []ColumnPages
}

// clusterSharded is reserved for a future version of the format that splits
// a cluster across several places. A reader that ignored it would silently
// read a fraction of the data, so this one refuses the file instead.
const clusterSharded = 0x01

// PageList locates the pages of a group of clusters.
type PageList struct {
	HeaderChecksum uint64
	Clusters       []Cluster
}

// readPageList parses a page list envelope.
func readPageList(r *rbuf) (*PageList, error) {
	var pl PageList

	pl.HeaderChecksum = r.U64()

	// the cluster summaries, which say what entries each cluster covers.
	list := r.list()
	if r.Err() != nil {
		return nil, r.Err()
	}
	for range int(list.items) {
		rec := r.record()
		if r.Err() != nil {
			return nil, r.Err()
		}

		var (
			first = r.U64()
			word  = r.U64()
		)
		c := Cluster{
			FirstEntry: first,
			NEntries:   word & 0x00ffffffffffffff,
			Flags:      uint8(word >> 56),
		}
		if c.Flags&clusterSharded != 0 {
			return nil, fmt.Errorf("rntup: sharded clusters are not supported")
		}
		pl.Clusters = append(pl.Clusters, c)
		r.done(rec)
	}
	r.done(list)

	err := pl.readPages(r)
	if err != nil {
		return nil, err
	}

	return &pl, r.Err()
}

// readPages reads the nested list frames that give, for every cluster and
// every column, where each page lives.
func (pl *PageList) readPages(r *rbuf) error {
	top := r.list()
	if r.Err() != nil {
		return r.Err()
	}
	if int(top.items) != len(pl.Clusters) {
		return fmt.Errorf(
			"rntup: page list describes %d clusters but %d were summarized",
			top.items, len(pl.Clusters),
		)
	}

	for i := range int(top.items) {
		outer := r.list()
		if r.Err() != nil {
			return r.Err()
		}

		cluster := &pl.Clusters[i]
		for range int(outer.items) {
			inner := r.list()
			if r.Err() != nil {
				return r.Err()
			}

			var col ColumnPages
			for range int(inner.items) {
				n := r.I32()
				p := Page{NElements: n, Checksum: n < 0}
				if p.Checksum {
					p.NElements = -n
				}
				p.Loc = r.locator()
				if r.Err() != nil {
					return r.Err()
				}
				col.Pages = append(col.Pages, p)
			}

			// the element offset and the compression settings sit
			// inside the inner frame, after the pages.
			col.FirstElem = r.I64()
			col.Suppressed = col.FirstElem < 0
			if !col.Suppressed {
				col.Compr = r.U32()
			}
			if r.Err() != nil {
				return r.Err()
			}

			cluster.Columns = append(cluster.Columns, col)
			r.done(inner)
		}
		r.done(outer)
	}
	r.done(top)

	return r.Err()
}
