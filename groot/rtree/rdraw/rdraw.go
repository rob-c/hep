// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package rdraw fills histograms from a tree by expression, the way
// TTree::Draw does.
//
//	h, err := rdraw.H1D(t, "pt", rdraw.Cut("pt > 20"), rdraw.Bins(100, 0, 200))
//
// An expression is arithmetic over the branches of the tree, with the usual
// comparisons, && and ||, and a library of maths functions that answer to
// their ROOT names as well as their Go ones, so "TMath::Abs(eta) < 2.5"
// and "abs(eta) < 2.5" both work.
//
// # The order of the axes
//
// As in ROOT, a 2-dim expression is written "y:x" and a 3-dim one "z:y:x":
// the first is the vertical axis. It reads backwards and it is what every
// ROOT user's fingers already do.
//
// # Binning
//
// Without Bins the range is found first and the histogram filled second,
// which reads the tree twice. Giving the binning reads it once.
//
// # What it does not do
//
// A branch holding an array or a slice is refused rather than guessed at.
// ROOT's Draw loops over those implicitly, and which of several possible
// loops it means depends on the shapes involved; getting that wrong would
// silently fill the wrong histogram.
package rdraw // import "go-hep.org/x/hep/groot/rtree/rdraw"

import (
	"fmt"
	"math"
	"reflect"
	"strings"

	"go-hep.org/x/hep/groot/rtree"
	"go-hep.org/x/hep/hbook"
)

// Option configures a Draw.
type Option func(cfg *config)

type config struct {
	cut    string
	weight string
	name   string
	title  string

	bins  [3]binning
	start int64
	n     int64
}

type binning struct {
	set    bool
	n      int
	lo, hi float64
}

func newConfig(opts []Option) *config {
	cfg := &config{start: 0, n: -1}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// Cut keeps only the entries the expression is true for, as the second
// argument of TTree::Draw does.
func Cut(expr string) Option {
	return func(cfg *config) { cfg.cut = expr }
}

// Weight fills with the value of the expression rather than with one.
func Weight(expr string) Option {
	return func(cfg *config) { cfg.weight = expr }
}

// Bins sets the binning of the first axis.
func Bins(n int, lo, hi float64) Option {
	return func(cfg *config) { cfg.bins[0] = binning{set: true, n: n, lo: lo, hi: hi} }
}

// BinsY sets the binning of the second axis.
func BinsY(n int, lo, hi float64) Option {
	return func(cfg *config) { cfg.bins[1] = binning{set: true, n: n, lo: lo, hi: hi} }
}

// BinsZ sets the binning of the third axis.
func BinsZ(n int, lo, hi float64) Option {
	return func(cfg *config) { cfg.bins[2] = binning{set: true, n: n, lo: lo, hi: hi} }
}

// Name names the histogram.
func Name(name string) Option {
	return func(cfg *config) { cfg.name = name }
}

// Title titles the histogram.
func Title(title string) Option {
	return func(cfg *config) { cfg.title = title }
}

// Range reads n entries starting at first, as the last two arguments of
// TTree::Draw do. A negative n reads to the end.
func Range(first, n int64) Option {
	return func(cfg *config) { cfg.start, cfg.n = first, n }
}

// H1D fills a 1-dim histogram from the tree.
func H1D(t rtree.Tree, expr string, opts ...Option) (*hbook.H1D, error) {
	h, err := draw(t, expr, 1, newConfig(opts))
	if err != nil {
		return nil, err
	}
	return h.(*hbook.H1D), nil
}

// H2D fills a 2-dim histogram from the tree, whose expression is "y:x".
func H2D(t rtree.Tree, expr string, opts ...Option) (*hbook.H2D, error) {
	h, err := draw(t, expr, 2, newConfig(opts))
	if err != nil {
		return nil, err
	}
	return h.(*hbook.H2D), nil
}

// H3D fills a 3-dim histogram from the tree, whose expression is "z:y:x".
func H3D(t rtree.Tree, expr string, opts ...Option) (*hbook.H3D, error) {
	h, err := draw(t, expr, 3, newConfig(opts))
	if err != nil {
		return nil, err
	}
	return h.(*hbook.H3D), nil
}

// Draw fills a histogram whose rank is the number of expressions given,
// separated by colons, and returns it as an *hbook.H1D, *hbook.H2D or
// *hbook.H3D.
func Draw(t rtree.Tree, expr string, opts ...Option) (hbook.Object, error) {
	rank := len(splitExprs(expr))
	if rank < 1 || rank > 3 {
		return nil, fmt.Errorf("rdraw: %q asks for %d dimensions, want 1, 2 or 3", expr, rank)
	}
	return draw(t, expr, rank, newConfig(opts))
}

// splitExprs cuts an expression on the colons that separate its axes,
// leaving alone any that belong to something else.
func splitExprs(expr string) []string {
	var (
		o     []string
		depth int
		last  int
	)
	for i := 0; i < len(expr); i++ {
		switch expr[i] {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case ':':
			// "::" is C++ saying which namespace it means, as in
			// TMath::Abs, and separates nothing.
			if i+1 < len(expr) && expr[i+1] == ':' {
				i++
				continue
			}
			if depth == 0 {
				o = append(o, strings.TrimSpace(expr[last:i]))
				last = i + 1
			}
		}
	}
	return append(o, strings.TrimSpace(expr[last:]))
}

// draw does the work behind H1D, H2D, H3D and Draw.
//
// ROOT writes the axes backwards -- "y:x" is y against x -- so the
// expressions are reversed here into the order the histogram wants them.
func draw(t rtree.Tree, expr string, rank int, cfg *config) (hbook.Object, error) {
	srcs := splitExprs(expr)
	if len(srcs) != rank {
		return nil, fmt.Errorf(
			"rdraw: %q gives %d expression(s), want %d",
			expr, len(srcs), rank,
		)
	}

	// "z:y:x" names the axes from the top down: turn it the right way round.
	axes := make([]string, rank)
	for i, src := range srcs {
		axes[rank-1-i] = src
	}

	ev, err := newEvaluator(t, axes, cfg)
	if err != nil {
		return nil, err
	}

	bins := cfg.bins
	if !allSet(bins[:rank]) {
		bins, err = ev.autoBins(t, cfg, rank)
		if err != nil {
			return nil, err
		}
	}

	h, fill, err := newHist(bins, rank, expr, cfg)
	if err != nil {
		return nil, err
	}

	err = ev.run(t, cfg, func(vs []float64, w float64) {
		fill(vs, w)
	})
	if err != nil {
		return nil, err
	}

	return h, nil
}

func allSet(bins []binning) bool {
	for _, b := range bins {
		if !b.set {
			return false
		}
	}
	return true
}

// newHist makes the histogram of the right rank, and the function that fills it.
func newHist(bins [3]binning, rank int, expr string, cfg *config) (hbook.Object, func(vs []float64, w float64), error) {
	name := cfg.name
	if name == "" {
		name = expr
	}

	ann := func(h hbook.Object) {
		a := h.Annotation()
		a["name"] = name
		if cfg.title != "" {
			a["title"] = cfg.title
		}
	}

	switch rank {
	case 1:
		h := hbook.NewH1D(bins[0].n, bins[0].lo, bins[0].hi)
		ann(h)
		return h, func(vs []float64, w float64) { h.Fill(vs[0], w) }, nil

	case 2:
		h := hbook.NewH2D(
			bins[0].n, bins[0].lo, bins[0].hi,
			bins[1].n, bins[1].lo, bins[1].hi,
		)
		ann(h)
		return h, func(vs []float64, w float64) { h.Fill(vs[0], vs[1], w) }, nil

	case 3:
		h := hbook.NewH3D(
			bins[0].n, bins[0].lo, bins[0].hi,
			bins[1].n, bins[1].lo, bins[1].hi,
			bins[2].n, bins[2].lo, bins[2].hi,
		)
		ann(h)
		return h, func(vs []float64, w float64) { h.Fill(vs[0], vs[1], vs[2], w) }, nil
	}

	return nil, nil, fmt.Errorf("rdraw: cannot draw %d dimensions", rank)
}

// evaluator reads the branches an expression needs and evaluates it.
type evaluator struct {
	axes   []*expr
	cut    *expr
	weight *expr

	rvars []rtree.ReadVar
	vals  map[string]float64

	// readers pulls each branch's value out as a float64.
	readers []func() float64
	names   []string
}

func newEvaluator(t rtree.Tree, axes []string, cfg *config) (*evaluator, error) {
	ev := &evaluator{vals: make(map[string]float64)}

	var idents []string
	add := func(src string) (*expr, error) {
		if src == "" {
			return nil, nil
		}
		e, err := newExpr(src)
		if err != nil {
			return nil, err
		}
		idents = append(idents, e.idents...)
		return e, nil
	}

	for _, src := range axes {
		e, err := add(src)
		if err != nil {
			return nil, err
		}
		if e == nil {
			return nil, fmt.Errorf("rdraw: empty expression")
		}
		ev.axes = append(ev.axes, e)
	}

	var err error
	if ev.cut, err = add(cfg.cut); err != nil {
		return nil, fmt.Errorf("rdraw: bad cut: %w", err)
	}
	if ev.weight, err = add(cfg.weight); err != nil {
		return nil, fmt.Errorf("rdraw: bad weight: %w", err)
	}

	err = ev.bind(t, idents)
	if err != nil {
		return nil, err
	}

	return ev, nil
}

// bind finds the branches the expressions name and arranges to read them.
func (ev *evaluator) bind(t rtree.Tree, idents []string) error {
	all := rtree.NewReadVars(t)
	byName := make(map[string]*rtree.ReadVar, len(all))
	for i := range all {
		byName[all[i].Name] = &all[i]
		// a branch with one leaf of the same name answers to either.
		if all[i].Leaf != "" && all[i].Leaf != all[i].Name {
			byName[all[i].Name+"."+all[i].Leaf] = &all[i]
		}
	}

	seen := make(map[string]bool)
	for _, name := range idents {
		if seen[name] {
			continue
		}
		seen[name] = true

		rvar, ok := byName[name]
		if !ok {
			return fmt.Errorf("rdraw: tree %q has no branch %q", t.Name(), name)
		}

		read, err := readerOf(rvar.Value)
		if err != nil {
			return fmt.Errorf("rdraw: branch %q: %w", name, err)
		}

		ev.rvars = append(ev.rvars, *rvar)
		ev.readers = append(ev.readers, read)
		ev.names = append(ev.names, name)
	}

	return nil
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
		return func() float64 { return b2f(v.Bool()) }, nil

	case reflect.Slice, reflect.Array:
		return nil, fmt.Errorf(
			"holds %s, which this package does not loop over: "+
				"read it with rtree.Reader and fill the histogram yourself",
			v.Type(),
		)
	}

	return nil, fmt.Errorf("holds %s, which is not a number", v.Type())
}

// run reads the tree, handing each entry's axis values and weight to fill.
func (ev *evaluator) run(t rtree.Tree, cfg *config, fill func(vs []float64, w float64)) error {
	opts := []rtree.ReadOption{rtree.WithRange(cfg.start, rangeEnd(cfg, t))}

	r, err := rtree.NewReader(t, ev.rvars, opts...)
	if err != nil {
		return fmt.Errorf("rdraw: could not read the tree: %w", err)
	}
	defer r.Close()

	vs := make([]float64, len(ev.axes))

	err = r.Read(func(rctx rtree.RCtx) error {
		for i, read := range ev.readers {
			ev.vals[ev.names[i]] = read()
		}

		if ev.cut != nil {
			keep, err := ev.cut.eval(ev.vals)
			if err != nil {
				return err
			}
			if keep == 0 {
				return nil
			}
		}

		w := 1.0
		if ev.weight != nil {
			w, err = ev.weight.eval(ev.vals)
			if err != nil {
				return err
			}
		}

		for i, e := range ev.axes {
			v, err := e.eval(ev.vals)
			if err != nil {
				return err
			}
			vs[i] = v
		}

		fill(vs, w)
		return nil
	})
	if err != nil {
		return fmt.Errorf("rdraw: could not read the tree: %w", err)
	}

	return nil
}

func rangeEnd(cfg *config, t rtree.Tree) int64 {
	if cfg.n < 0 {
		return t.Entries()
	}
	end := cfg.start + cfg.n
	return min(end, t.Entries())
}

// autoBins reads the tree once to find the range of each axis, so that a
// Draw given no binning still gets a sensible one.
func (ev *evaluator) autoBins(t rtree.Tree, cfg *config, rank int) ([3]binning, error) {
	var (
		bins = cfg.bins
		lo   = [3]float64{math.Inf(+1), math.Inf(+1), math.Inf(+1)}
		hi   = [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
		n    int
	)

	err := ev.run(t, cfg, func(vs []float64, w float64) {
		n++
		for i, v := range vs {
			lo[i] = math.Min(lo[i], v)
			hi[i] = math.Max(hi[i], v)
		}
	})
	if err != nil {
		return bins, err
	}

	if n == 0 {
		return bins, fmt.Errorf("rdraw: nothing to draw: no entry passed the cut")
	}

	const defaultBins = 100
	for i := range rank {
		if bins[i].set {
			continue
		}

		min, max := lo[i], hi[i]
		switch {
		case min == max:
			// one value: give it a bin to sit in the middle of.
			d := math.Max(1, math.Abs(min)) * 0.5
			min, max = min-d, max+d
		default:
			// a little room either side, so the extremes are not on an edge.
			d := (max - min) * 0.05
			min, max = min-d, max+d
		}

		nb := defaultBins
		if rank > 1 {
			nb = 50 // a 2- or 3-dim histogram with 100 bins a side is mostly empty
		}
		bins[i] = binning{set: true, n: nb, lo: min, hi: max}
	}

	return bins, nil
}
