// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rdata

import (
	"fmt"
	"reflect"

	"go-hep.org/x/hep/groot/rtree"
)

// Floats returns a column as one number per event, whatever kind of number the
// file happens to store it as.
//
// A column that holds several numbers per event — the momenta of the jets, say
// — is read with [Table.Arrays] instead, and the error says so.
func (t *Table) Floats(col string) ([]float64, error) {
	rvar, err := t.rvar(col)
	if err != nil {
		return nil, err
	}

	v := reflect.ValueOf(rvar.Value).Elem()
	if _, ok := asFloat(v); !ok {
		return nil, t.wrongType(col, v.Type(), "a number")
	}

	out := make([]float64, 0, t.Rows())
	err = t.each([]rtree.ReadVar{rvar}, func(int64) error {
		x, _ := asFloat(v)
		out = append(out, x)
		return nil
	})
	if err != nil {
		return nil, t.readErr(col, err)
	}
	return out, nil
}

// Ints returns a column as one whole number per event.
//
// A column of floating-point numbers is not read this way: rounding is a
// decision, and it is the caller's to make from [Table.Floats].
func (t *Table) Ints(col string) ([]int64, error) {
	rvar, err := t.rvar(col)
	if err != nil {
		return nil, err
	}

	v := reflect.ValueOf(rvar.Value).Elem()
	if _, ok := asInt(v); !ok {
		return nil, t.wrongType(col, v.Type(), "a whole number")
	}

	out := make([]int64, 0, t.Rows())
	err = t.each([]rtree.ReadVar{rvar}, func(int64) error {
		x, _ := asInt(v)
		out = append(out, x)
		return nil
	})
	if err != nil {
		return nil, t.readErr(col, err)
	}
	return out, nil
}

// Strings returns a column as one string per event.
func (t *Table) Strings(col string) ([]string, error) {
	rvar, err := t.rvar(col)
	if err != nil {
		return nil, err
	}

	v := reflect.ValueOf(rvar.Value).Elem()
	if v.Kind() != reflect.String {
		return nil, t.wrongType(col, v.Type(), "text")
	}

	out := make([]string, 0, t.Rows())
	err = t.each([]rtree.ReadVar{rvar}, func(int64) error {
		out = append(out, v.String())
		return nil
	})
	if err != nil {
		return nil, t.readErr(col, err)
	}
	return out, nil
}

// Arrays returns a column that holds several numbers per event: one slice per
// event, whose length is how many there were in that event.
func (t *Table) Arrays(col string) ([][]float64, error) {
	rvar, err := t.rvar(col)
	if err != nil {
		return nil, err
	}

	v := reflect.ValueOf(rvar.Value).Elem()
	if _, ok := asFloats(v); !ok {
		return nil, t.wrongType(col, v.Type(), "a list of numbers")
	}

	out := make([][]float64, 0, t.Rows())
	err = t.each([]rtree.ReadVar{rvar}, func(int64) error {
		xs, _ := asFloats(v)
		out = append(out, xs)
		return nil
	})
	if err != nil {
		return nil, t.readErr(col, err)
	}
	return out, nil
}

// Row is one event, as seen from inside [Table.Each].
//
// Its accessors return zero when asked for a column the loop was not given, or
// for one whose contents are of another kind; Each then stops and returns that
// as its error, so a loop body needs no error handling of its own.
type Row struct {
	entry int64
	tree  string
	vals  map[string]reflect.Value // column -> pointer to its value
	err   *error
}

// Entry returns the position of this event in the table, counting from zero.
func (r Row) Entry() int64 { return r.entry }

// Float returns a column of this event as a number.
func (r Row) Float(col string) float64 {
	v, ok := r.lookup(col)
	if !ok {
		return 0
	}
	x, ok := asFloat(v)
	if !ok {
		r.fail(r.wrongType(col, v.Type(), "a number"))
		return 0
	}
	return x
}

// Int returns a column of this event as a whole number.
func (r Row) Int(col string) int64 {
	v, ok := r.lookup(col)
	if !ok {
		return 0
	}
	x, ok := asInt(v)
	if !ok {
		r.fail(r.wrongType(col, v.Type(), "a whole number"))
		return 0
	}
	return x
}

// Text returns a column of this event as a string.
func (r Row) Text(col string) string {
	v, ok := r.lookup(col)
	if !ok {
		return ""
	}
	if v.Kind() != reflect.String {
		r.fail(r.wrongType(col, v.Type(), "text"))
		return ""
	}
	return v.String()
}

// Floats returns a column of this event that holds several numbers.
func (r Row) Floats(col string) []float64 {
	v, ok := r.lookup(col)
	if !ok {
		return nil
	}
	xs, ok := asFloats(v)
	if !ok {
		r.fail(r.wrongType(col, v.Type(), "a list of numbers"))
		return nil
	}
	return xs
}

func (r Row) lookup(col string) (reflect.Value, bool) {
	ptr, ok := r.vals[col]
	if !ok {
		r.fail(&Error{
			Op: "read", Name: col, Tree: r.tree,
			Err:  fmt.Errorf("this loop was not given a column named %q", col),
			hint: "name it in the first argument of Each, or pass nil there for every column",
		})
		return reflect.Value{}, false
	}
	return ptr.Elem(), true
}

func (r Row) wrongType(col string, got reflect.Type, want string) error {
	return &Error{
		Op: "read", Name: col, Tree: r.tree,
		Err:  fmt.Errorf("this column holds %s, which is not %s", got, want),
		hint: hintFor(got),
	}
}

// fail records the first thing that went wrong. The rest of the event is
// still handed to the caller — a Row cannot stop a loop it does not own — but
// Each returns this instead of nil.
func (r Row) fail(err error) {
	if *r.err == nil {
		*r.err = err
	}
}

// Each walks the table one event at a time, calling fn for each, and holds no
// more than one event's worth of data at a time. It is what to use when the
// table is too large to fit in memory, or when the answer is a number rather
// than a column.
//
// Only the named columns are read, which is most of why this is fast; passing
// nil reads every column, which is convenient and is not.
//
// An error returned by fn stops the walk and is returned as-is, so
// [io/fs.SkipAll]-style sentinels of the caller's own work as expected.
func (t *Table) Each(cols []string, fn func(r Row) error) error {
	if cols == nil {
		cols = t.cols
	}

	var (
		rvars = make([]rtree.ReadVar, 0, len(cols))
		vals  = make(map[string]reflect.Value, 2*len(cols))
		once  = make(map[string]bool, len(cols))
	)
	for _, col := range cols {
		rvar, err := t.rvar(col)
		if err != nil {
			return err
		}
		ptr := reflect.ValueOf(rvar.Value)
		vals[rvar.Name] = ptr
		vals[rvar.Leaf] = ptr

		// The same column named twice is one column: a reader given it twice
		// would read it twice. A branch of several leaves is several, though,
		// so both halves of the name count.
		key := rvar.Name + "/" + rvar.Leaf
		if once[key] {
			continue
		}
		once[key] = true
		rvars = append(rvars, rvar)
	}

	var rerr error
	err := t.each(rvars, func(entry int64) error {
		err := fn(Row{entry: entry, tree: t.name, vals: vals, err: &rerr})
		if err != nil {
			return err
		}
		return rerr
	})
	if err != nil {
		return err
	}
	return rerr
}

// each is the read loop every accessor above is built from.
func (t *Table) each(rvars []rtree.ReadVar, fn func(entry int64) error) error {
	if t.tree == nil {
		return &Error{Op: "read", Name: t.name, Err: fmt.Errorf("this table is closed")}
	}

	r, err := rtree.NewReader(t.tree, rvars)
	if err != nil {
		return &Error{Op: "read", Name: t.name, Err: err}
	}
	defer r.Close()

	return r.Read(func(ctx rtree.RCtx) error {
		return fn(ctx.Entry)
	})
}

// wrongType is the error for a column that is there but holds something else.
func (t *Table) wrongType(col string, got reflect.Type, want string) error {
	return &Error{
		Op: "read", Name: col, Tree: t.name,
		Err:  fmt.Errorf("this column holds %s, which is not %s", got, want),
		hint: hintFor(got),
	}
}

// hintFor names the accessor that does read a column of this type, which is
// nearly always what the caller wanted to be told.
func hintFor(typ reflect.Type) string {
	switch typ.Kind() {
	case reflect.String:
		return "read it with Strings, or with Row.Text"
	case reflect.Slice, reflect.Array:
		switch typ.Elem().Kind() {
		case reflect.Bool,
			reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			return "it holds several numbers per event: read it with Arrays, or with Row.Floats"
		}
	case reflect.Float32, reflect.Float64:
		return "read it with Floats, or with Row.Float"
	}
	return "Table.Type says what a column holds, and Table.Columns what there is to choose from"
}

// asFloat reads any kind of number as a float64, which is what a plot, a fit
// and a histogram all want anyway.
func asFloat(v reflect.Value) (float64, bool) {
	switch v.Kind() {
	case reflect.Bool:
		if v.Bool() {
			return 1, true
		}
		return 0, true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(v.Uint()), true
	case reflect.Float32, reflect.Float64:
		return v.Float(), true
	}
	return 0, false
}

// asInt reads a whole number. A floating-point column is refused rather than
// truncated: which way to round is the caller's decision, not this package's.
func asInt(v reflect.Value) (int64, bool) {
	switch v.Kind() {
	case reflect.Bool:
		if v.Bool() {
			return 1, true
		}
		return 0, true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(v.Uint()), true
	}
	return 0, false
}

// asFloats reads a list of numbers, copied out of the buffer the reader keeps
// re-using, so that the events already gathered stay what they were.
func asFloats(v reflect.Value) ([]float64, bool) {
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
	default:
		return nil, false
	}
	n := v.Len()
	out := make([]float64, n)
	for i := range n {
		x, ok := asFloat(v.Index(i))
		if !ok {
			return nil, false
		}
		out[i] = x
	}
	if n == 0 {
		// An empty event says nothing about the element type, so the type
		// itself has to be asked.
		if _, ok := zeroOf(v.Type().Elem()); !ok {
			return nil, false
		}
	}
	return out, true
}

// zeroOf reports whether a value of that type is a number this package reads.
func zeroOf(typ reflect.Type) (float64, bool) {
	return asFloat(reflect.New(typ).Elem())
}
