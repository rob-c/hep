// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rdf

import (
	"fmt"
	"reflect"

	"go-hep.org/x/hep/groot/rtree"
)

// Run reads the tree and computes everything that was asked of it.
//
// It reads the tree once however many results were asked for, and does
// nothing at all the second time it is called, so a result may be taken
// whenever it is wanted.
func (df *Frame) Run() error {
	sh := df.tree

	if sh.err != nil {
		return sh.err
	}
	if sh.ran {
		return nil
	}
	sh.ran = true

	if len(sh.actions) == 0 {
		return nil
	}

	// every name any action or step mentions has to be read or computed.
	names := make(map[string]bool)
	defined := make(map[string]bool)

	for _, a := range sh.actions {
		for _, s := range a.steps {
			for _, id := range s.expr.Idents() {
				if !defined[id] {
					names[id] = true
				}
			}
			if s.kind == stepDefine {
				defined[s.name] = true
				delete(names, s.name)
			}
		}
		for _, e := range a.exprs {
			for _, id := range e.Idents() {
				if !defined[id] {
					names[id] = true
				}
			}
		}
	}

	rvars, readers, order, err := bind(sh.tree, names)
	if err != nil {
		sh.err = err
		return err
	}

	r, err := rtree.NewReader(sh.tree, rvars, rtree.WithRange(sh.start, end(sh)))
	if err != nil {
		sh.err = fmt.Errorf("rdf: could not read the tree: %w", err)
		return sh.err
	}
	defer r.Close()

	vals := make(map[string]float64, len(order)+8)

	err = r.Read(func(rctx rtree.RCtx) error {
		for i, read := range readers {
			vals[order[i]] = read()
		}

		// Each action walks its own chain of Defines and Filters. Chains
		// that share a prefix do that prefix more than once, which costs
		// arithmetic and never costs a read: the reading is what a pass is
		// for, and there is still only one.
		for _, a := range sh.actions {
			ok, err := runSteps(a.steps, vals)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			if err := a.fill(vals); err != nil {
				return err
			}
		}

		// and the cut flows, which have to count what each filter was given
		// rather than only what came out of the last one.
		for i := range sh.reports {
			err := countCuts(&sh.reports[i], vals)
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		sh.err = fmt.Errorf("rdf: could not read the tree: %w", err)
		return sh.err
	}

	for _, a := range sh.actions {
		if a.done == nil {
			continue
		}
		if err := a.done(); err != nil {
			sh.err = err
			return err
		}
	}

	return nil
}

// runSteps applies a chain of Defines and Filters to one entry, and says
// whether the entry survived.
func runSteps(steps []step, vals map[string]float64) (bool, error) {
	for _, s := range steps {
		v, err := s.expr.Eval(vals)
		if err != nil {
			return false, err
		}
		switch s.kind {
		case stepDefine:
			vals[s.name] = v
		case stepFilter:
			if v == 0 {
				return false, nil
			}
		}
	}
	return true, nil
}

// countCuts walks a report's chain, counting what reached each filter and
// what passed it.
func countCuts(rep *reportOf, vals map[string]float64) error {
	cut := 0
	for _, s := range rep.steps {
		v, err := s.expr.Eval(vals)
		if err != nil {
			return err
		}
		switch s.kind {
		case stepDefine:
			vals[s.name] = v
		case stepFilter:
			rep.info[cut].All++
			if v == 0 {
				return nil
			}
			rep.info[cut].Pass++
			cut++
		}
	}
	return nil
}

func end(sh *shared) int64 {
	if sh.n < 0 {
		return sh.tree.Entries()
	}
	return min(sh.start+sh.n, sh.tree.Entries())
}

// bind finds the branches the frame needs and arranges to read them as
// float64, whatever they are stored as.
func bind(t rtree.Tree, names map[string]bool) ([]rtree.ReadVar, []func() float64, []string, error) {
	all := rtree.NewReadVars(t)
	byName := make(map[string]*rtree.ReadVar, len(all))
	for i := range all {
		byName[all[i].Name] = &all[i]
		if all[i].Leaf != "" && all[i].Leaf != all[i].Name {
			byName[all[i].Name+"."+all[i].Leaf] = &all[i]
		}
	}

	var (
		rvars   []rtree.ReadVar
		readers []func() float64
		order   []string
	)

	for name := range names {
		rvar, ok := byName[name]
		if !ok {
			return nil, nil, nil, fmt.Errorf("rdf: tree %q has no branch %q", t.Name(), name)
		}

		read, err := readerOf(rvar.Value)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("rdf: branch %q: %w", name, err)
		}

		rvars = append(rvars, *rvar)
		readers = append(readers, read)
		order = append(order, name)
	}

	return rvars, readers, order, nil
}

// readerOf returns a function pulling a float64 out of a branch's value.
func readerOf(ptr any) (func() float64, error) {
	rv := reflect.ValueOf(ptr)
	if rv.Kind() != reflect.Pointer {
		return nil, fmt.Errorf("expected a pointer, got %T", ptr)
	}

	v := rv.Elem()
	switch v.Kind() {
	case reflect.Float32, reflect.Float64:
		return func() float64 { return v.Float() }, nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return func() float64 { return float64(v.Int()) }, nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return func() float64 { return float64(v.Uint()) }, nil

	case reflect.Bool:
		return func() float64 {
			if v.Bool() {
				return 1
			}
			return 0
		}, nil

	case reflect.Slice, reflect.Array:
		return nil, fmt.Errorf(
			"holds %s, which this package does not loop over: "+
				"read it with rtree.Reader and do the work by hand",
			v.Type(),
		)
	}

	return nil, fmt.Errorf("holds %s, which is not a number", v.Type())
}
