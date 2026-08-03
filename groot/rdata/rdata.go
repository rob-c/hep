// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package rdata gets the numbers out of a ROOT file, and puts them back in.
//
// A ROOT file holds one or more trees; a tree is a table, with a named column
// per quantity and a row per event. This package is that table and nothing
// else: open the file, ask for a column, get a slice of numbers.
//
//	t, err := rdata.Open("data.root")
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer t.Close()
//
//	px, err := t.Floats("px")   // one number per event, for every event
//	fmt.Println(len(px), t.Rows())
//
// The name of the tree can be left out when the file holds exactly one, which
// is the usual case. When it holds more, [Trees] says what they are called and
// [Open] takes the name as a second argument.
//
// # Files on the grid, and lots of them
//
// A name may be a local path or a URL — root://, roots://, https://, davs://
// — and it may be a pattern, in which case every file it matches is read as
// though they were one long table:
//
//	t, err := rdata.Open("root://server//data/run*.root", "Events")
//
// [OpenAll] takes an explicit list for when the files do not share a pattern.
// The storage side of this is [go-hep.org/x/hep/xrootd/xrd], which is worth
// knowing about if the files have to be copied, listed or checked first.
//
// # Columns that hold several numbers per event
//
// Not every column is one number per event: the transverse momenta of the jets
// in an event are a list whose length changes from event to event. Those are
// read with [Table.Arrays], which gives one slice per event, and the length of
// each is the number of jets in that event.
//
// [Table.Columns] says what the columns are called and [Table.Type] what is in
// them, so a file nobody documented can be read anyway.
//
// # One event at a time
//
// A column is materialised in full by [Table.Floats], which is the shortest
// thing that works and is the right answer up to a few tens of millions of
// events. Past that, or when the answer is a single number and the events
// need not be kept, [Table.Each] walks the table an event at a time and holds
// nothing:
//
//	var sum float64
//	err := t.Each([]string{"px"}, func(r rdata.Row) error {
//		sum += r.Float("px")
//		return nil
//	})
//
// # Writing the answer out
//
// [Save] writes columns to a new ROOT file, which may also be a URL:
//
//	err := rdata.Save("out.root", "results",
//		rdata.Column{Name: "mass", Data: masses},
//		rdata.Column{Name: "ok", Data: flags},
//	)
//
// # When more control is needed
//
// This package is a front door, not a wall: [go-hep.org/x/hep/groot] opens the
// file, [go-hep.org/x/hep/groot/rtree] reads the tree, and both are there for
// the cases this package deliberately does not cover — trees of structs, joins,
// formulas, read-ahead tuning and the rest.
package rdata // import "go-hep.org/x/hep/groot/rdata"

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"go-hep.org/x/hep/groot"
	"go-hep.org/x/hep/groot/riofs"
	"go-hep.org/x/hep/groot/rtree"
	"go-hep.org/x/hep/xrootd/xrd"

	_ "go-hep.org/x/hep/groot/riofs/plugin/http"   // so a name may be an https:// or davs:// URL
	_ "go-hep.org/x/hep/groot/riofs/plugin/xrootd" // so a name may be a root:// or roots:// URL
)

// Table is a tree of a ROOT file, opened for reading: a column per quantity
// and a row per event.
//
// A Table holds the files it was opened from, and must be closed.
type Table struct {
	name  string
	files []*riofs.File
	tree  rtree.Tree

	rvars []rtree.ReadVar // one per leaf, with a value of the right Go type
	index map[string]int  // column name -> index into rvars
	cols  []string        // column names, in the order the file has them
}

// Open opens a ROOT file and returns the tree in it.
//
// The name may be a path on this machine or a URL on grid storage, and it may
// be a pattern such as "run*.root", in which case every file matching it is
// read as one table.
//
// The name of the tree may be given as a second argument. It can be left out
// when the file holds exactly one tree; when it holds more, the error says
// what they are called.
func Open(name string, tree ...string) (*Table, error) {
	return OpenAll([]string{name}, tree...)
}

// OpenAll opens several ROOT files and returns their trees read end to end, as
// though the events had been in one file all along.
//
// Each name is a path, a URL or a pattern, exactly as for [Open]. The tree must
// have the same name in every file, which is what the second argument names
// when it is not the only tree there.
func OpenAll(names []string, tree ...string) (*Table, error) {
	var want string
	switch len(tree) {
	case 0:
		// resolved from the file below.
	case 1:
		want = tree[0]
	default:
		return nil, fmt.Errorf("rdata: too many tree names (%d): a table is one tree", len(tree))
	}

	names, err := expand(names)
	if err != nil {
		return nil, err
	}

	var (
		t     = &Table{name: want}
		trees = make([]rtree.Tree, 0, len(names))
	)
	for _, name := range names {
		f, err := groot.Open(name)
		if err != nil {
			t.Close()
			return nil, fmt.Errorf("rdata: could not open %q: %w", name, err)
		}
		t.files = append(t.files, f)

		if t.name == "" {
			t.name, err = onlyTree(f)
			if err != nil {
				t.Close()
				return nil, err
			}
		}

		tree, err := treeOf(f, t.name)
		if err != nil {
			t.Close()
			return nil, err
		}
		trees = append(trees, tree)
	}

	switch len(trees) {
	case 1:
		t.tree = trees[0]
	default:
		t.tree = rtree.Chain(trees...)
	}

	t.rvars = rtree.NewReadVars(t.tree)
	t.index = make(map[string]int, 2*len(t.rvars))
	for i, rvar := range t.rvars {
		if _, dup := t.index[rvar.Name]; !dup {
			t.index[rvar.Name] = i
			t.cols = append(t.cols, rvar.Name)
		}
		if _, dup := t.index[rvar.Leaf]; !dup {
			t.index[rvar.Leaf] = i
		}
	}

	return t, nil
}

// Close releases the files the table was read from.
func (t *Table) Close() error {
	var err error
	for _, f := range t.files {
		if e := f.Close(); e != nil && err == nil {
			err = e
		}
	}
	t.files = nil
	t.tree = nil
	if err != nil {
		return fmt.Errorf("rdata: could not close %q: %w", t.name, err)
	}
	return nil
}

// Name returns the name of the tree this table was read from.
func (t *Table) Name() string { return t.name }

// Rows returns the number of events in the table.
func (t *Table) Rows() int64 {
	if t.tree == nil {
		return 0
	}
	return t.tree.Entries()
}

// Columns returns the name of every column, in the order the file has them.
func (t *Table) Columns() []string {
	out := make([]string, len(t.cols))
	copy(out, t.cols)
	return out
}

// Has reports whether the table has a column with that name.
func (t *Table) Has(col string) bool {
	_, ok := t.index[col]
	return ok
}

// Type returns the Go type of a column: "float64" for one number per event,
// "[]float64" for a list of them, and so on. It returns "" when there is no
// such column.
func (t *Table) Type(col string) string {
	i, ok := t.index[col]
	if !ok {
		return ""
	}
	return reflect.TypeOf(t.rvars[i].Value).Elem().String()
}

// rvar returns the read-variable of a column, by branch name or by leaf name.
func (t *Table) rvar(col string) (rtree.ReadVar, error) {
	i, ok := t.index[col]
	if !ok {
		return rtree.ReadVar{}, &Error{
			Op: "read", Name: col, Tree: t.name,
			Err:  fmt.Errorf("there is no column with that name"),
			cols: t.cols,
		}
	}
	return t.rvars[i], nil
}

// readErr gives a failure from the layers underneath the same shape as the
// ones this package raises itself, and leaves those alone.
func (t *Table) readErr(col string, err error) error {
	var e *Error
	if errors.As(err, &e) {
		return err
	}
	return &Error{Op: "read", Name: col, Tree: t.name, Err: err}
}

// Trees returns the name of every tree in a ROOT file, including the ones in
// its sub-directories, which is the question to ask of a file nobody has
// documented.
//
// The name may be a pattern, in which case the first file it matches is the
// one asked: the files of a dataset are laid out the same way.
func Trees(name string) ([]string, error) {
	names, err := expand([]string{name})
	if err != nil {
		return nil, err
	}

	f, err := groot.Open(names[0])
	if err != nil {
		return nil, fmt.Errorf("rdata: could not open %q: %w", names[0], err)
	}
	defer f.Close()

	return treeNames(f)
}

// expand turns the names that are patterns into the names they match, and
// leaves the rest alone. A pattern that matches nothing is an error: a job
// that reads no files at all should say so rather than report no events.
func expand(names []string) ([]string, error) {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if !strings.ContainsAny(name, "*?[") {
			out = append(out, name)
			continue
		}
		got, err := xrd.Glob(name)
		if err != nil {
			return nil, fmt.Errorf("rdata: could not expand %q: %w", name, err)
		}
		if len(got) == 0 {
			return nil, fmt.Errorf("rdata: nothing matches %q", name)
		}
		out = append(out, got...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("rdata: no files named")
	}
	return out, nil
}

// treeOf returns the named tree of a file, with an error that says what the
// file does hold when it is not there.
func treeOf(f *riofs.File, name string) (rtree.Tree, error) {
	obj, err := riofs.Dir(f).Get(name)
	if err != nil {
		names, _ := treeNames(f)
		return nil, &Error{
			Op: "read", Name: f.Name(), Tree: name,
			Err:   fmt.Errorf("no tree named %q", name),
			trees: names,
		}
	}
	tree, ok := obj.(rtree.Tree)
	if !ok {
		return nil, &Error{
			Op: "read", Name: f.Name(), Tree: name,
			Err: fmt.Errorf("%q is a %s, not a tree", name, obj.Class()),
		}
	}
	return tree, nil
}

// onlyTree returns the name of the tree of a file that holds exactly one, and
// otherwise an error naming the choice that has to be made.
func onlyTree(f *riofs.File) (string, error) {
	names, err := treeNames(f)
	if err != nil {
		return "", err
	}
	switch len(names) {
	case 1:
		return names[0], nil
	case 0:
		return "", &Error{
			Op: "read", Name: f.Name(),
			Err: fmt.Errorf("there is no tree in this file"),
		}
	default:
		return "", &Error{
			Op: "read", Name: f.Name(),
			Err:   fmt.Errorf("this file holds %d trees: name the one to read", len(names)),
			trees: names,
		}
	}
}

// treeNames returns the path of every tree in a file, sub-directories
// included. The keys are read, not the objects, so a file of large trees costs
// no more to ask than a file of small ones.
func treeNames(f *riofs.File) ([]string, error) {
	var (
		out  []string
		walk func(dir riofs.Directory, prefix string) error
	)
	walk = func(dir riofs.Directory, prefix string) error {
		for _, key := range dir.Keys() {
			switch key.ClassName() {
			case "TTree", "TNtuple", "TNtupleD":
				out = append(out, prefix+key.Name())
			case "TDirectory", "TDirectoryFile":
				obj, err := key.Object()
				if err != nil {
					return fmt.Errorf("rdata: could not read directory %q of %q: %w", key.Name(), f.Name(), err)
				}
				sub, ok := obj.(riofs.Directory)
				if !ok {
					continue
				}
				if err := walk(sub, prefix+key.Name()+"/"); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(f, ""); err != nil {
		return nil, err
	}
	return out, nil
}
