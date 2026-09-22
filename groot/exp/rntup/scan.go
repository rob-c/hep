// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rntup

import (
	"fmt"
	"reflect"
)

// clusterState holds the column cursors in use for one cluster, so that
// walking the entries of a cluster decodes each page once.
type clusterState struct {
	r  *Reader
	cl *Cluster

	cursors []*colCursor // by column ID, filled in on first use
}

func newClusterState(r *Reader, cl *Cluster) *clusterState {
	return &clusterState{r: r, cl: cl, cursors: make([]*colCursor, len(r.schema.Columns))}
}

// cursor returns a cursor over the column of a slot that is live in this
// cluster.
//
// A slot lists one column per alternative representation of a field. Exactly
// one of them carries the data in any given cluster and the others are
// suppressed there, so which one to read is a per-cluster decision.
func (cs *clusterState) cursor(slot []int) (*colCursor, error) {
	if len(slot) == 0 {
		return nil, fmt.Errorf("rntup: field has no column to read")
	}

	var absent *Column
	for _, col := range slot {
		if col < 0 || col >= len(cs.cursors) {
			return nil, fmt.Errorf("rntup: no such column %d", col)
		}
		if c := cs.cursors[col]; c != nil {
			return c, nil
		}

		if col >= len(cs.cl.Columns) {
			// a column added to the schema after this cluster was
			// written has no pages here at all.
			absent = &cs.r.schema.Columns[col]
			continue
		}

		pages := &cs.cl.Columns[col]
		if pages.Suppressed {
			continue
		}

		c, err := newColCursor(cs.r, &cs.r.schema.Columns[col], pages, cs.cl.FirstEntry)
		if err != nil {
			return nil, err
		}
		cs.cursors[col] = c
		return c, nil
	}

	if absent != nil {
		c, err := newZeroCursor(cs.r, absent)
		if err != nil {
			return nil, err
		}
		cs.cursors[absent.ID] = c
		return c, nil
	}

	return nil, fmt.Errorf(
		"rntup: every representation of column %v is suppressed in this cluster", slot,
	)
}

// ReadVar binds a top-level field of an RNTuple to a Go value.
type ReadVar struct {
	Name  string // the name of the field to read
	Value any    // a pointer to the value the field is read into
}

// NewReadVars returns a ReadVar for every top-level field of the RNTuple,
// each holding a freshly allocated value of the Go type that field maps to.
//
// It is the starting point for reading an RNTuple whose schema is not known
// ahead of time.
func NewReadVars(r *Reader) ([]ReadVar, error) {
	var out []ReadVar
	for _, id := range r.schema.TopLevel() {
		f := r.schema.Field(id)
		fr, err := r.fieldReader(f)
		if err != nil {
			return nil, err
		}
		out = append(out, ReadVar{
			Name:  f.Name,
			Value: reflect.New(fr.rtype()).Interface(),
		})
	}
	return out, nil
}

// GoType returns the Go type the named top-level field reads into.
func (r *Reader) GoType(name string) (reflect.Type, error) {
	f := r.schema.Lookup(name)
	if f == nil {
		return nil, fmt.Errorf("rntup: RNTuple %q has no field %q", r.Name(), name)
	}
	fr, err := r.fieldReader(f)
	if err != nil {
		return nil, err
	}
	return fr.rtype(), nil
}

// binding is a field reader tied to the Go value it fills.
type binding struct {
	name string
	fr   fieldReader
	dst  reflect.Value

	// when the caller bound a type of their own rather than the one the
	// field reads into, the field is read into scratch and copied over.
	scratch reflect.Value
	cp      copier
}

// fill reads the field for one entry and hands it to the caller's value.
func (b *binding) fill(cs *clusterState, i uint64) error {
	if b.cp == nil {
		return b.fr.read(cs, i, b.dst)
	}
	err := b.fr.read(cs, i, b.scratch)
	if err != nil {
		return err
	}
	return b.cp(b.dst, b.scratch)
}

func (r *Reader) bind(rvars []ReadVar) ([]binding, error) {
	out := make([]binding, 0, len(rvars))
	for i, rvar := range rvars {
		f := r.schema.Lookup(rvar.Name)
		if f == nil {
			return nil, fmt.Errorf("rntup: RNTuple %q has no field %q", r.Name(), rvar.Name)
		}

		fr, err := r.fieldReader(f)
		if err != nil {
			return nil, err
		}

		if rvar.Value == nil {
			return nil, fmt.Errorf("rntup: read-var %d (%q) has a nil value", i, rvar.Name)
		}
		rv := reflect.ValueOf(rvar.Value)
		if rv.Kind() != reflect.Pointer || rv.IsNil() {
			return nil, fmt.Errorf(
				"rntup: read-var %d (%q) must be a non-nil pointer, got %T",
				i, rvar.Name, rvar.Value,
			)
		}

		b := binding{name: rvar.Name, fr: fr, dst: rv.Elem()}
		if got, want := b.dst.Type(), fr.rtype(); got != want {
			// the caller bound a type of their own. it is allowed so
			// long as it lines up with what the field holds, which is
			// what lets a C++ struct be read into a Go one.
			cp, err := newCopier(got, want)
			if err != nil {
				return nil, fmt.Errorf(
					"rntup: field %q reads into a %v, but read-var %d is a pointer to %v: %w",
					rvar.Name, want, i, got, err,
				)
			}
			b.cp = cp
			b.scratch = reflect.New(want).Elem()
		}

		out = append(out, b)
	}
	return out, nil
}

// Read calls fn once per entry, having filled in the values bound by rvars.
//
// Returning an error from fn stops the walk and hands the error back.
func (r *Reader) Read(rvars []ReadVar, fn func(entry uint64) error) error {
	return r.ReadRange(rvars, 0, r.nentries, fn)
}

// ReadRange is Read over the entries in [beg, end).
func (r *Reader) ReadRange(rvars []ReadVar, beg, end uint64, fn func(entry uint64) error) error {
	if end > r.nentries {
		return fmt.Errorf("rntup: entry range [%d, %d) runs past the %d entries of %q",
			beg, end, r.nentries, r.Name())
	}
	if beg > end {
		return fmt.Errorf("rntup: entry range [%d, %d) runs backwards", beg, end)
	}

	bindings, err := r.bind(rvars)
	if err != nil {
		return err
	}

	for i := range r.NClusters() {
		cl := r.cluster(i)
		lo := max(cl.FirstEntry, beg)
		hi := min(cl.FirstEntry+cl.NEntries, end)
		if lo >= hi {
			continue
		}

		cs := newClusterState(r, cl)
		for entry := lo; entry < hi; entry++ {
			local := entry - cl.FirstEntry
			for i := range bindings {
				b := &bindings[i]
				err := b.fill(cs, local)
				if err != nil {
					return fmt.Errorf("rntup: could not read field %q of entry %d: %w", b.name, entry, err)
				}
			}
			err := fn(entry)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
