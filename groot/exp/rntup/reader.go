// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rntup

import (
	"fmt"
	"io"

	"go-hep.org/x/hep/groot/riofs"
)

// Reader reads entries from an RNTuple held in a ROOT file.
type Reader struct {
	r io.ReaderAt
	f *riofs.File // set when the reader owns the file and must close it

	anchor *Anchor
	schema *Schema
	footer *Footer

	pls      []*PageList // one page list per cluster group
	clusters []clusterRef
	nentries uint64
}

// clusterRef points at a cluster within the page list of its group.
type clusterRef struct {
	pl  int
	idx int
}

// Open opens the named RNTuple in the ROOT file at path.
//
// The returned Reader owns the file and closes it.
func Open(path, name string) (*Reader, error) {
	f, err := riofs.Open(path)
	if err != nil {
		return nil, fmt.Errorf("rntup: could not open %q: %w", path, err)
	}

	r, err := NewReader(f, name)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	r.f = f
	return r, nil
}

// NewReader reads the named RNTuple out of an already-open ROOT file.
//
// The file is not closed when the reader is.
func NewReader(f *riofs.File, name string) (*Reader, error) {
	obj, err := f.Get(name)
	if err != nil {
		return nil, fmt.Errorf("rntup: could not find RNTuple %q: %w", name, err)
	}

	a, ok := obj.(*Anchor)
	if !ok {
		return nil, fmt.Errorf("rntup: %q is a %T, not an RNTuple", name, obj)
	}
	a.name = name

	return newReader(f, a)
}

func newReader(src io.ReaderAt, a *Anchor) (*Reader, error) {
	r := &Reader{r: src, anchor: a}

	hdr, err := readEnvelope(src, envHeader, a.SeekHeader, int64(a.NBytesHeader), int64(a.LenHeader))
	if err != nil {
		return nil, err
	}
	r.schema, err = readHeader(hdr)
	if err != nil {
		return nil, fmt.Errorf("rntup: could not read the schema of %q: %w", a.name, err)
	}

	ftr, err := readEnvelope(src, envFooter, a.SeekFooter, int64(a.NBytesFooter), int64(a.LenFooter))
	if err != nil {
		return nil, err
	}
	r.footer, err = readFooter(ftr, r.schema)
	if err != nil {
		return nil, fmt.Errorf("rntup: could not read the footer of %q: %w", a.name, err)
	}

	err = r.readPageLists()
	if err != nil {
		return nil, err
	}

	return r, nil
}

func (r *Reader) readPageLists() error {
	for i, g := range r.footer.Groups {
		buf, err := readEnvelope(
			r.r, envPageList,
			g.PageList.loc.offset, g.PageList.loc.size, int64(g.PageList.length),
		)
		if err != nil {
			return fmt.Errorf("rntup: could not read page list %d: %w", i, err)
		}

		pl, err := readPageList(buf)
		if err != nil {
			return fmt.Errorf("rntup: could not read page list %d: %w", i, err)
		}
		if got, want := len(pl.Clusters), int(g.NClusters); got != want {
			return fmt.Errorf(
				"rntup: cluster group %d says it holds %d clusters, its page list has %d",
				i, want, got,
			)
		}

		r.pls = append(r.pls, pl)
		for j := range pl.Clusters {
			r.clusters = append(r.clusters, clusterRef{pl: i, idx: j})
			r.nentries += pl.Clusters[j].NEntries
		}
	}
	return nil
}

// Close releases the reader. The underlying file is closed only if the
// reader opened it.
func (r *Reader) Close() error {
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}

// Name returns the name of the RNTuple.
func (r *Reader) Name() string { return r.anchor.Name() }

// Description returns the description the RNTuple was written with.
func (r *Reader) Description() string { return r.schema.Description }

// Writer returns the library or program that wrote the RNTuple.
func (r *Reader) Writer() string { return r.schema.Writer }

// Entries returns how many entries the RNTuple holds.
func (r *Reader) Entries() uint64 { return r.nentries }

// Schema returns the description of the RNTuple's fields and columns.
func (r *Reader) Schema() *Schema { return r.schema }

// Anchor returns the anchor the RNTuple was found through.
func (r *Reader) Anchor() *Anchor { return r.anchor }

// NClusters returns how many clusters the entries are spread over.
func (r *Reader) NClusters() int { return len(r.clusters) }

// Cluster returns the i-th cluster, in entry order.
func (r *Reader) Cluster(i int) *Cluster { return r.cluster(i) }

// cluster returns the i-th cluster, in entry order.
func (r *Reader) cluster(i int) *Cluster {
	ref := r.clusters[i]
	return &r.pls[ref.pl].Clusters[ref.idx]
}
