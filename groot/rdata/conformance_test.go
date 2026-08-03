// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for rdata, which exists so that getting the numbers out of a
// ROOT file is one call and not a paragraph.
//
// What is pinned here is the promise the package makes to somebody who has
// never written Go: that the tree need not be named when there is only one,
// that a column comes back as a slice of numbers whatever the file stores it
// as, that many files read as one, that what is written comes back the same,
// and — the part that decides whether they get anywhere at all — that an error
// says which call would have worked.

package rdata_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-hep.org/x/hep/groot"
	"go-hep.org/x/hep/groot/rdata"
	"go-hep.org/x/hep/groot/rtree"
	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrd"
)

const (
	simple = "../testdata/simple.root"     // 4 events: one/two/three
	flat   = "../testdata/small-flat-tree" // 100 events: scalars, arrays and slices
)

func TestConformance_TheTreeNeedNotBeNamed(t *testing.T) {
	tbl, err := rdata.Open(simple)
	if err != nil {
		t.Fatalf("could not open: %+v", err)
	}
	defer tbl.Close()

	if got, want := tbl.Name(), "tree"; got != want {
		t.Fatalf("found the tree %q, want %q", got, want)
	}
	if got, want := tbl.Rows(), int64(4); got != want {
		t.Fatalf("counted %d events, want %d", got, want)
	}
	if got, want := tbl.Columns(), []string{"one", "two", "three"}; !equal(got, want) {
		t.Fatalf("columns are %q, want %q", got, want)
	}
	if got, want := tbl.Type("two"), "float32"; got != want {
		t.Fatalf("column \"two\" holds %q, want %q", got, want)
	}
	if !tbl.Has("one") || tbl.Has("nope") {
		t.Fatalf("Has does not agree with Columns")
	}

	// And the file says what is in it without being opened for reading first.
	names, err := rdata.Trees(simple)
	if err != nil {
		t.Fatalf("could not list the trees: %+v", err)
	}
	if got, want := names, []string{"tree"}; !equal(got, want) {
		t.Fatalf("trees are %q, want %q", got, want)
	}
}

func TestConformance_AColumnComesBackAsNumbers(t *testing.T) {
	tbl, err := rdata.Open(simple)
	if err != nil {
		t.Fatalf("could not open: %+v", err)
	}
	defer tbl.Close()

	// The file stores these as int32, float32 and a ROOT string. What comes
	// back is what anybody wanting to plot them would ask for.
	got, err := tbl.Floats("one")
	if err != nil {
		t.Fatalf("could not read \"one\": %+v", err)
	}
	if want := []float64{1, 2, 3, 4}; !equal(got, want) {
		t.Fatalf("read %v, want %v", got, want)
	}

	ints, err := tbl.Ints("one")
	if err != nil {
		t.Fatalf("could not read \"one\" as whole numbers: %+v", err)
	}
	if want := []int64{1, 2, 3, 4}; !equal(ints, want) {
		t.Fatalf("read %v, want %v", ints, want)
	}

	f32, err := tbl.Floats("two")
	if err != nil {
		t.Fatalf("could not read \"two\": %+v", err)
	}
	for i, want := range []float64{1.1, 2.2, 3.3, 4.4} {
		if math.Abs(f32[i]-want) > 1e-6 {
			t.Fatalf("event %d is %v, want %v", i, f32[i], want)
		}
	}

	str, err := tbl.Strings("three")
	if err != nil {
		t.Fatalf("could not read \"three\": %+v", err)
	}
	if want := []string{"uno", "dos", "tres", "quatro"}; !equal(str, want) {
		t.Fatalf("read %v, want %v", str, want)
	}
}

// TestConformance_SeveralNumbersPerEvent: the jets in an event are a list
// whose length is part of the physics, and it changes event by event.
func TestConformance_SeveralNumbersPerEvent(t *testing.T) {
	tbl, err := rdata.Open(flat + ".root")
	if err != nil {
		t.Fatalf("could not open: %+v", err)
	}
	defer tbl.Close()

	n, err := tbl.Ints("N")
	if err != nil {
		t.Fatalf("could not read \"N\": %+v", err)
	}

	slice, err := tbl.Arrays("SliceFloat64")
	if err != nil {
		t.Fatalf("could not read \"SliceFloat64\": %+v", err)
	}
	if got, want := len(slice), int(tbl.Rows()); got != want {
		t.Fatalf("read %d events, want %d", got, want)
	}
	for i, xs := range slice {
		if got, want := len(xs), int(n[i]); got != want {
			t.Fatalf("event %d holds %d numbers, want %d", i, got, want)
		}
	}

	// The buffer the reader re-uses must not show through: two events of the
	// same length are still two answers, and the second must not have
	// overwritten the first.
	var (
		differ bool
		first  = make(map[int]int, len(slice))
	)
	for i, xs := range slice {
		if len(xs) == 0 {
			continue
		}
		j, seen := first[len(xs)]
		switch {
		case !seen:
			first[len(xs)] = i
		case !equal(xs, slice[j]):
			differ = true
		}
	}
	if !differ {
		t.Fatalf("no two events of the same length differ: the reader's buffer is showing through")
	}

	// A fixed-size column is a list of the same length every time.
	fixed, err := tbl.Arrays("ArrayFloat64")
	if err != nil {
		t.Fatalf("could not read \"ArrayFloat64\": %+v", err)
	}
	for i, xs := range fixed {
		if len(xs) != 10 {
			t.Fatalf("event %d holds %d numbers, want 10", i, len(xs))
		}
	}
}

// TestConformance_TheErrorSaysWhichCallWouldHaveWorked is the difference
// between somebody carrying on and somebody giving up.
func TestConformance_TheErrorSaysWhichCallWouldHaveWorked(t *testing.T) {
	tbl, err := rdata.Open(flat + ".root")
	if err != nil {
		t.Fatalf("could not open: %+v", err)
	}
	defer tbl.Close()

	for _, tc := range []struct {
		name string
		call func() error
		says []string
	}{
		{
			name: "a column that is not there",
			call: func() error { _, err := tbl.Floats("pt"); return err },
			says: []string{"there is no column with that name", "the columns are:", "Int32"},
		},
		{
			name: "text read as a number",
			call: func() error { _, err := tbl.Floats("Str"); return err },
			says: []string{"holds string", "read it with Strings"},
		},
		{
			name: "a list read as one number",
			call: func() error { _, err := tbl.Floats("SliceFloat64"); return err },
			says: []string{"several numbers per event", "read it with Arrays"},
		},
		{
			name: "one number read as a list",
			call: func() error { _, err := tbl.Arrays("Float64"); return err },
			says: []string{"is not a list of numbers", "read it with Floats"},
		},
		{
			name: "a fraction read as a whole number",
			call: func() error { _, err := tbl.Ints("Float64"); return err },
			says: []string{"is not a whole number", "read it with Floats"},
		},
		{
			name: "a number read as text",
			call: func() error { _, err := tbl.Strings("Int32"); return err },
			says: []string{"is not text"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("no error at all")
			}
			for _, want := range tc.says {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("the error does not say %q:\n%v", want, err)
				}
			}
		})
	}

	// A file that is not there fails the way every Go program tests for.
	_, err = rdata.Open(filepath.Join(t.TempDir(), "nope.root"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a missing file gave %v, want something that is fs.ErrNotExist", err)
	}
}

func TestConformance_EachHoldsOneEventAtATime(t *testing.T) {
	tbl, err := rdata.Open(flat + ".root")
	if err != nil {
		t.Fatalf("could not open: %+v", err)
	}
	defer tbl.Close()

	all, err := tbl.Floats("Float64")
	if err != nil {
		t.Fatalf("could not read \"Float64\": %+v", err)
	}

	var (
		sum  float64
		want float64
		n    int64
	)
	for _, x := range all {
		want += x
	}

	err = tbl.Each([]string{"Float64", "N", "SliceFloat64", "Str"}, func(r rdata.Row) error {
		if r.Entry() != n {
			return fmt.Errorf("event %d arrived as %d", n, r.Entry())
		}
		if got, want := len(r.Floats("SliceFloat64")), int(r.Int("N")); got != want {
			return fmt.Errorf("event %d holds %d numbers, want %d", n, got, want)
		}
		if r.Text("Str") == "" {
			return fmt.Errorf("event %d has no text", n)
		}
		sum += r.Float("Float64")
		n++
		return nil
	})
	if err != nil {
		t.Fatalf("could not walk the table: %+v", err)
	}
	if n != tbl.Rows() {
		t.Fatalf("walked %d events, want %d", n, tbl.Rows())
	}
	if math.Abs(sum-want) > 1e-9 {
		t.Fatalf("summed to %v one event at a time and %v all at once", sum, want)
	}

	// The caller's own error stops the walk and comes back untouched.
	stop := errors.New("that is enough")
	n = 0
	err = tbl.Each([]string{"Float64"}, func(r rdata.Row) error {
		n++
		if n == 3 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Fatalf("the walk swallowed the caller's error: %v", err)
	}
	if n != 3 {
		t.Fatalf("the walk went on for %d events after being told to stop", n-3)
	}

	// Asking inside the loop for a column the loop was not given is a mistake
	// worth reporting, not a silent zero.
	err = tbl.Each([]string{"Float64"}, func(r rdata.Row) error {
		_ = r.Float("Int32")
		return nil
	})
	if err == nil {
		t.Fatal("reading a column that was not asked for went unreported")
	}
	if !strings.Contains(err.Error(), "was not given a column") {
		t.Fatalf("the error does not say what happened: %v", err)
	}

	// And nil is every column, for when the file is small and the hurry is not.
	n = 0
	err = tbl.Each(nil, func(r rdata.Row) error {
		if r.Float("Int32") != r.Float("Int32") {
			return io.EOF
		}
		n++
		return nil
	})
	if err != nil {
		t.Fatalf("could not walk every column: %+v", err)
	}
	if n != tbl.Rows() {
		t.Fatalf("walked %d events, want %d", n, tbl.Rows())
	}
}

// TestConformance_ManyFilesAreOneTable: a dataset is never one file.
func TestConformance_ManyFilesAreOneTable(t *testing.T) {
	files := []string{
		"../testdata/chain.flat.1.root",
		"../testdata/chain.flat.2.root",
	}

	tbl, err := rdata.OpenAll(files)
	if err != nil {
		t.Fatalf("could not open: %+v", err)
	}
	defer tbl.Close()

	if got, want := tbl.Rows(), int64(10); got != want {
		t.Fatalf("counted %d events, want %d", got, want)
	}
	got, err := tbl.Floats("F64")
	if err != nil {
		t.Fatalf("could not read \"F64\": %+v", err)
	}
	if len(got) != 10 {
		t.Fatalf("read %d events, want 10", len(got))
	}

	// The same thing said as a pattern, which is how anybody would say it.
	dir := t.TempDir()
	for i, name := range files {
		copyFile(t, filepath.Join(dir, fmt.Sprintf("run%d.root", i)), name)
	}

	glob, err := rdata.Open(filepath.Join(dir, "run*.root"))
	if err != nil {
		t.Fatalf("could not open the pattern: %+v", err)
	}
	defer glob.Close()

	if got, want := glob.Rows(), int64(10); got != want {
		t.Fatalf("the pattern found %d events, want %d", got, want)
	}

	// A pattern matching nothing is a failure: a job that read no files at all
	// should not report no events.
	if _, err := rdata.Open(filepath.Join(dir, "nope*.root")); err == nil {
		t.Fatal("a pattern matching nothing opened anyway")
	}
}

// TestConformance_TheTreeIsNamedWhenThereIsAChoice: guessing would be worse
// than asking, and the error is where the answer has to be.
func TestConformance_TheTreeIsNamedWhenThereIsAChoice(t *testing.T) {
	name := filepath.Join(t.TempDir(), "two.root")
	makeTwoTrees(t, name)

	_, err := rdata.Open(name)
	if err == nil {
		t.Fatal("a file of two trees opened without being told which")
	}
	for _, want := range []string{"holds 2 trees", "name the one to read", "sub/deeper"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error does not say %q:\n%v", want, err)
		}
	}

	// Named, including one in a sub-directory, it opens.
	for _, tc := range []struct {
		tree string
		rows int64
	}{
		{tree: "top", rows: 3},
		{tree: "sub/deeper", rows: 2},
	} {
		t.Run(tc.tree, func(t *testing.T) {
			tbl, err := rdata.Open(name, tc.tree)
			if err != nil {
				t.Fatalf("could not open %q: %+v", tc.tree, err)
			}
			defer tbl.Close()

			if got := tbl.Rows(); got != tc.rows {
				t.Fatalf("counted %d events, want %d", got, tc.rows)
			}
		})
	}

	// A name that is not there says what is.
	_, err = rdata.Open(name, "nope")
	if err == nil {
		t.Fatal("a tree that is not there opened anyway")
	}
	for _, want := range []string{"no tree named", "the trees in it are:", "top", "sub/deeper"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error does not say %q:\n%v", want, err)
		}
	}

	// And Trees says the same thing without an error to read it out of.
	names, err := rdata.Trees(name)
	if err != nil {
		t.Fatalf("could not list the trees: %+v", err)
	}
	if want := []string{"top", "sub/deeper"}; !equal(names, want) {
		t.Fatalf("trees are %q, want %q", names, want)
	}
}

func TestConformance_SavedColumnsComeBack(t *testing.T) {
	var (
		name  = filepath.Join(t.TempDir(), "out.root")
		mass  = []float64{91.2, 125.1, 172.5}
		count = []int{1, 2, 3}
		ok    = []bool{true, false, true}
		tag   = []string{"z", "h", "t"}
	)

	err := rdata.Save(name, "results",
		rdata.Column{Name: "mass", Data: mass},
		rdata.Column{Name: "count", Data: count},
		rdata.Column{Name: "ok", Data: ok},
		rdata.Column{Name: "tag", Data: tag},
	)
	if err != nil {
		t.Fatalf("could not save: %+v", err)
	}

	tbl, err := rdata.Open(name)
	if err != nil {
		t.Fatalf("could not open what was just written: %+v", err)
	}
	defer tbl.Close()

	if got, want := tbl.Name(), "results"; got != want {
		t.Fatalf("the tree is called %q, want %q", got, want)
	}
	if got, want := tbl.Rows(), int64(3); got != want {
		t.Fatalf("counted %d events, want %d", got, want)
	}

	if got, err := tbl.Floats("mass"); err != nil || !equal(got, mass) {
		t.Fatalf("read %v (%v), want %v", got, err, mass)
	}
	if got, err := tbl.Ints("count"); err != nil || !equal(got, []int64{1, 2, 3}) {
		t.Fatalf("read %v (%v), want %v", got, err, count)
	}
	if got, err := tbl.Ints("ok"); err != nil || !equal(got, []int64{1, 0, 1}) {
		t.Fatalf("read %v (%v), want %v", got, err, ok)
	}
	if got, err := tbl.Strings("tag"); err != nil || !equal(got, tag) {
		t.Fatalf("read %v (%v), want %v", got, err, tag)
	}
}

func TestConformance_SaveRefusesWhatItCannotWrite(t *testing.T) {
	dir := t.TempDir()

	for _, tc := range []struct {
		name string
		cols []rdata.Column
		tree string
		says string
	}{
		{
			name: "no columns",
			tree: "t",
			says: "there are no columns to write",
		},
		{
			name: "columns of different lengths",
			tree: "t",
			cols: []rdata.Column{
				{Name: "a", Data: []float64{1, 2, 3}},
				{Name: "b", Data: []float64{1, 2}},
			},
			says: "every column is one value per event",
		},
		{
			name: "a value that is not a column",
			tree: "t",
			cols: []rdata.Column{{Name: "a", Data: 42.0}},
			says: "a column is a slice with one value per event",
		},
		{
			name: "several numbers per event",
			tree: "t",
			cols: []rdata.Column{{Name: "a", Data: [][]float64{{1, 2}, {3}}}},
			says: "which this package cannot write",
		},
		{
			name: "two columns of the same name",
			tree: "t",
			cols: []rdata.Column{
				{Name: "a", Data: []float64{1}},
				{Name: "a", Data: []float64{2}},
			},
			says: "two columns cannot both be called that",
		},
		{
			name: "a tree with no name",
			cols: []rdata.Column{{Name: "a", Data: []float64{1}}},
			says: "the tree needs a name",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := rdata.Save(filepath.Join(dir, "out.root"), tc.tree, tc.cols...)
			if err == nil {
				t.Fatal("no error at all")
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("the error does not say %q:\n%v", tc.says, err)
			}
		})
	}
}

// makeTwoTrees writes a file holding a tree at the top and another in a
// sub-directory, which is how a real experiment's files are laid out.
func makeTwoTrees(t *testing.T, name string) {
	t.Helper()

	f, err := groot.Create(name)
	if err != nil {
		t.Fatalf("could not create %q: %+v", name, err)
	}
	defer f.Close()

	var x float64

	top, err := rtree.NewWriter(f, "top", []rtree.WriteVar{{Name: "x", Value: &x}})
	if err != nil {
		t.Fatalf("could not create the top tree: %+v", err)
	}
	for range 3 {
		if _, err := top.Write(); err != nil {
			t.Fatalf("could not write: %+v", err)
		}
	}
	if err := top.Close(); err != nil {
		t.Fatalf("could not close the top tree: %+v", err)
	}

	dir, err := f.Mkdir("sub")
	if err != nil {
		t.Fatalf("could not create the sub-directory: %+v", err)
	}
	sub, err := rtree.NewWriter(dir, "deeper", []rtree.WriteVar{{Name: "x", Value: &x}})
	if err != nil {
		t.Fatalf("could not create the sub tree: %+v", err)
	}
	for range 2 {
		if _, err := sub.Write(); err != nil {
			t.Fatalf("could not write: %+v", err)
		}
	}
	if err := sub.Close(); err != nil {
		t.Fatalf("could not close the sub tree: %+v", err)
	}

	if err := f.Close(); err != nil {
		t.Fatalf("could not close %q: %+v", name, err)
	}
}

func copyFile(t *testing.T, dst, src string) {
	t.Helper()

	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("could not read %q: %+v", src, err)
	}
	if err := os.WriteFile(dst, data, 0644); err != nil {
		t.Fatalf("could not write %q: %+v", dst, err)
	}
}

func equal[T comparable](got, want []T) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestConformance_AURLIsJustAnotherName is the claim the package makes about
// grid storage: the same call, a different string. It is checked against a
// real XRootD server, in this process, over a real socket.
func TestConformance_AURLIsJustAnotherName(t *testing.T) {
	dir, url := fsServer(t)

	copyFile(t, filepath.Join(dir, "simple.root"), simple)

	tbl, err := rdata.Open(url + "simple.root")
	if err != nil {
		t.Fatalf("could not open over the network: %+v", err)
	}
	defer tbl.Close()

	if got, want := tbl.Rows(), int64(4); got != want {
		t.Fatalf("counted %d events, want %d", got, want)
	}
	got, err := tbl.Floats("one")
	if err != nil {
		t.Fatalf("could not read \"one\" over the network: %+v", err)
	}
	if want := []float64{1, 2, 3, 4}; !equal(got, want) {
		t.Fatalf("read %v, want %v", got, want)
	}

	// A pattern is expanded on the server, too.
	glob, err := rdata.Open(url + "*.root")
	if err != nil {
		t.Fatalf("could not open a remote pattern: %+v", err)
	}
	defer glob.Close()

	if got, want := glob.Rows(), int64(4); got != want {
		t.Fatalf("the pattern found %d events, want %d", got, want)
	}

	// And the answer goes back the same way it came.
	err = rdata.Save(url+"out.root", "results",
		rdata.Column{Name: "mass", Data: []float64{91.2, 125.1}},
	)
	if err != nil {
		t.Fatalf("could not save over the network: %+v", err)
	}

	out, err := rdata.Open(url + "out.root")
	if err != nil {
		t.Fatalf("could not open what was just written: %+v", err)
	}
	defer out.Close()

	if got, err := out.Floats("mass"); err != nil || !equal(got, []float64{91.2, 125.1}) {
		t.Fatalf("read %v (%v), want [91.2 125.1]", got, err)
	}
}

// fsServer starts an XRootD server over a temporary directory and returns that
// directory together with the root:// prefix reaching it. A path is appended
// to the prefix: the extra slash separates the endpoint from an absolute path
// on it.
func fsServer(t *testing.T) (dir, url string) {
	t.Helper()

	dir = t.TempDir()

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}

	srv := xrootd.NewServer(xrootd.NewFSHandler(dir), func(err error) {
		t.Logf("xrd-srv: %v", err)
	})
	go func() {
		if err := srv.Serve(listener); err != nil && err != xrootd.ErrServerClosed {
			t.Logf("xrd-srv: could not serve: %v", err)
		}
	}()
	t.Cleanup(func() {
		// The xrd package caches its connections: let them go before the
		// server does.
		_ = xrd.Close()
		_ = srv.Shutdown(context.Background())
	})

	return dir, fmt.Sprintf("root://%s/", listener.Addr())
}
