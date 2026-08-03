// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rdata

import (
	"fmt"
	"reflect"

	"go-hep.org/x/hep/groot"
	"go-hep.org/x/hep/groot/rtree"
)

// Column is one column of a table on its way out: a name, and the values, one
// per event.
//
// Data is a slice — []float64, []int32, []string, []bool and the rest of the
// numeric kinds — and every column of a file must be the same length, because
// that length is the number of events.
type Column struct {
	Name string
	Data any
}

// Save writes columns to a new ROOT file, which anything that reads ROOT can
// then open, this package included.
//
//	err := rdata.Save("out.root", "results",
//		rdata.Column{Name: "mass", Data: masses},
//		rdata.Column{Name: "ok", Data: flags},
//	)
//
// The name may be a path on this machine or a URL on grid storage. An existing
// file of that name is replaced.
//
// Columns holding several numbers per event are not written this way: they need
// a column counting them, and how that count is shared between them is a
// decision this package will not make for you. [go-hep.org/x/hep/groot/rtree]
// writes those.
func Save(name, tree string, cols ...Column) error {
	if tree == "" {
		return &Error{Op: "write", Name: name, Err: fmt.Errorf("the tree needs a name")}
	}
	if len(cols) == 0 {
		return &Error{Op: "write", Name: name, Tree: tree, Err: fmt.Errorf("there are no columns to write")}
	}

	var (
		rows  = -1
		ptrs  = make([]reflect.Value, len(cols))
		data  = make([]reflect.Value, len(cols))
		wvars = make([]rtree.WriteVar, len(cols))
		seen  = make(map[string]bool, len(cols))
	)
	for i, col := range cols {
		switch {
		case col.Name == "":
			return &Error{Op: "write", Name: name, Tree: tree,
				Err: fmt.Errorf("column %d has no name", i)}
		case seen[col.Name]:
			return &Error{Op: "write", Name: col.Name, Tree: tree,
				Err: fmt.Errorf("two columns cannot both be called that")}
		}
		seen[col.Name] = true

		v := reflect.ValueOf(col.Data)
		if !v.IsValid() || v.Kind() != reflect.Slice {
			return &Error{Op: "write", Name: col.Name, Tree: tree,
				Err:  fmt.Errorf("a column is a slice with one value per event, not %T", col.Data),
				hint: "a single number is a column of one event: []float64{x}"}
		}

		typ, err := writeType(v.Type().Elem())
		if err != nil {
			return &Error{Op: "write", Name: col.Name, Tree: tree, Err: err,
				hint: "a column holding several numbers per event is written with groot/rtree, which can name the column that counts them"}
		}

		switch {
		case rows < 0:
			rows = v.Len()
		case v.Len() != rows:
			return &Error{Op: "write", Name: col.Name, Tree: tree,
				Err: fmt.Errorf("this column has %d values and %q has %d: every column is one value per event",
					v.Len(), cols[0].Name, rows)}
		}

		ptrs[i] = reflect.New(typ)
		data[i] = v
		wvars[i] = rtree.WriteVar{Name: col.Name, Value: ptrs[i].Interface()}
	}

	f, err := groot.Create(name)
	if err != nil {
		return &Error{Op: "write", Name: name, Tree: tree, Err: err}
	}

	// A ROOT file is finished when it is closed, so closing it is part of
	// writing it and its error is not one to drop on the floor.
	err = fill(f, tree, rows, wvars, ptrs, data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return &Error{Op: "write", Name: name, Tree: tree, Err: err}
	}
	return nil
}

// fill writes the events into a tree of an already-open file.
func fill(f *groot.File, tree string, rows int, wvars []rtree.WriteVar, ptrs, data []reflect.Value) error {
	w, err := rtree.NewWriter(f, tree, wvars)
	if err != nil {
		return err
	}

	for row := range rows {
		for i := range ptrs {
			ptrs[i].Elem().Set(data[i].Index(row).Convert(ptrs[i].Type().Elem()))
		}
		if _, err := w.Write(); err != nil {
			return fmt.Errorf("could not write event %d: %w", row, err)
		}
	}

	return w.Close()
}

// writeType is the type a column is written as. Go's int and uint have no
// ROOT counterpart — their width is whatever the machine is — so they are
// written at the width that loses nothing.
func writeType(typ reflect.Type) (reflect.Type, error) {
	switch typ.Kind() {
	case reflect.Int:
		return reflect.TypeOf(int64(0)), nil
	case reflect.Uint:
		return reflect.TypeOf(uint64(0)), nil
	case reflect.Bool, reflect.String,
		reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return typ, nil
	}
	return nil, fmt.Errorf("each event of this column holds %s, which this package cannot write", typ)
}
