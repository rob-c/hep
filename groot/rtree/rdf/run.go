// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rdf

import (
	"fmt"
	"reflect"

	"go-hep.org/x/hep/groot/rtree"
	"go-hep.org/x/hep/internal/rexpr"
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

	ctx := rexpr.Ctx{
		Vals:    make(map[string]rexpr.Value, len(order)+8),
		Entries: sh.tree.Entries(),
	}

	err = r.Read(func(rctx rtree.RCtx) error {
		for i, read := range readers {
			ctx.Vals[order[i]] = read()
		}
		ctx.Entry = rctx.Entry

		// Each action walks its own chain of Defines and Filters. Chains
		// that share a prefix do that prefix more than once, which costs
		// arithmetic and never costs a read: the reading is what a pass is
		// for, and there is still only one.
		for _, a := range sh.actions {
			ok, err := runSteps(a.steps, &ctx)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}

			// an action over a collection runs once per element; one
			// over single values, and one with nothing to evaluate at
			// all such as Count, runs once for the entry.
			n, err := spanOf(a.exprs, &ctx)
			if err != nil {
				return err
			}
			for iter := range n {
				if err := a.fill(&ctx, iter); err != nil {
					return err
				}
			}
		}

		// and the cut flows, which have to count what each filter was given
		// rather than only what came out of the last one.
		for i := range sh.reports {
			err := countCuts(&sh.reports[i], &ctx)
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

// spanOf returns how many values a set of expressions yields together for
// the entry now loaded. Expressions over single values go with any number of
// elements; the ones that loop over a collection have to agree.
func spanOf(exprs []*rexpr.Expr, ctx *rexpr.Ctx) (int, error) {
	var (
		out  = 1
		over = false
	)
	for _, e := range exprs {
		n, coll, err := e.Span(ctx)
		if err != nil {
			return 0, err
		}
		switch {
		case !coll:
		case !over:
			out, over = n, true
		case out != n:
			return 0, fmt.Errorf(
				"rdf: %q yields %d value(s) where another part of the same action yields %d",
				e, n, out,
			)
		}
	}
	return out, nil
}

// runSteps applies a chain of Defines and Filters to one entry, and says
// whether the entry survived.
func runSteps(steps []step, ctx *rexpr.Ctx) (bool, error) {
	for _, s := range steps {
		switch s.kind {
		case stepDefine:
			v, err := valueOf(s.expr, ctx)
			if err != nil {
				return false, fmt.Errorf("rdf: could not define %q: %w", s.name, err)
			}
			ctx.Vals[s.name] = v

		case stepFilter:
			v, err := entryValue(s.expr, ctx, "filter")
			if err != nil {
				return false, err
			}
			if v == 0 {
				return false, nil
			}
		}
	}
	return true, nil
}

// valueOf computes a defined column, which is a collection when what
// defines it is one and a single value otherwise.
func valueOf(e *rexpr.Expr, ctx *rexpr.Ctx) (rexpr.Value, error) {
	n, coll, err := e.Span(ctx)
	if err != nil {
		return rexpr.Value{}, err
	}
	if !coll {
		v, err := e.At(ctx, 0)
		if err != nil {
			return rexpr.Value{}, err
		}
		return rexpr.Num(v), nil
	}

	vs := make([]float64, n)
	for i := range n {
		v, err := e.At(ctx, i)
		if err != nil {
			return rexpr.Value{}, err
		}
		vs[i] = v
	}
	return rexpr.Slice(vs), nil
}

// entryValue computes an expression that has to answer once for the whole
// entry, as a filter does.
func entryValue(e *rexpr.Expr, ctx *rexpr.Ctx, what string) (float64, error) {
	_, coll, err := e.Span(ctx)
	if err != nil {
		return 0, err
	}
	if coll {
		return 0, fmt.Errorf(
			"rdf: %s %q is over a collection, and a %s has to answer once for the whole entry: "+
				"reduce it with Length$, Sum$, Min$ or Max$",
			what, e, what,
		)
	}
	return e.At(ctx, 0)
}

// countCuts walks a report's chain, counting what reached each filter and
// what passed it.
func countCuts(rep *reportOf, ctx *rexpr.Ctx) error {
	cut := 0
	for _, s := range rep.steps {
		switch s.kind {
		case stepDefine:
			v, err := valueOf(s.expr, ctx)
			if err != nil {
				return fmt.Errorf("rdf: could not define %q: %w", s.name, err)
			}
			ctx.Vals[s.name] = v

		case stepFilter:
			v, err := entryValue(s.expr, ctx, "filter")
			if err != nil {
				return err
			}
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
// float64, whatever they are stored as, and as collections where the branch
// holds an array or a slice.
func bind(t rtree.Tree, names map[string]bool) ([]rtree.ReadVar, []func() rexpr.Value, []string, error) {
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
		readers []func() rexpr.Value
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

// readerOf returns a function pulling a branch's value out, as one number
// or, for an array or a slice branch, as the collection of them.
func readerOf(ptr any) (func() rexpr.Value, error) {
	rv := reflect.ValueOf(ptr)
	if rv.Kind() != reflect.Pointer {
		return nil, fmt.Errorf("expected a pointer, got %T", ptr)
	}

	v := rv.Elem()
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		elem, err := numberOf(v.Type().Elem())
		if err != nil {
			return nil, fmt.Errorf("holds %s: %w", v.Type(), err)
		}
		var buf []float64
		return func() rexpr.Value {
			buf = buf[:0]
			for i := range v.Len() {
				buf = append(buf, elem(v.Index(i)))
			}
			return rexpr.Slice(buf)
		}, nil
	}

	num, err := numberOf(v.Type())
	if err != nil {
		return nil, err
	}
	return func() rexpr.Value { return rexpr.Num(num(v)) }, nil
}

// numberOf returns a function turning a value of the given type into the
// float64 an expression works in.
func numberOf(typ reflect.Type) (func(reflect.Value) float64, error) {
	switch typ.Kind() {
	case reflect.Float32, reflect.Float64:
		return reflect.Value.Float, nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return func(v reflect.Value) float64 { return float64(v.Int()) }, nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return func(v reflect.Value) float64 { return float64(v.Uint()) }, nil

	case reflect.Bool:
		return func(v reflect.Value) float64 {
			if v.Bool() {
				return 1
			}
			return 0
		}, nil
	}

	return nil, fmt.Errorf("holds %s, which is not a number", typ)
}
