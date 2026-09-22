// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package rdf analyses a tree by describing what is wanted of it rather than
// by looping over it, in the manner of ROOT's RDataFrame.
//
//	df := rdf.New(t).
//		Define("mt", "sqrt(2*pt*met*(1-cos(dphi)))").
//		Filter("pt > 20 && abs(eta) < 2.5")
//
//	n := df.Count()
//	h := df.Histo1D("mt", rdf.Bins(100, 0, 200))
//
//	err := df.Run()
//
// Nothing is read until Run, and then the tree is read once however many
// results were asked for. A loop written by hand reads it once too, but only
// for as long as it stays one loop: the point of describing the work instead
// is that adding a histogram does not add a pass.
//
// Each result is a handle. Value reads it, and runs the frame first if it has
// not been run, so Run is optional and only worth calling to catch an error
// at the point it happened.
//
// # Filters and columns
//
// Define adds a column computed from the ones already there, and may use a
// column an earlier Define made. Filter drops the entries an expression is
// false for, and takes a name so that Report can say afterwards how many
// entries each one kept — the cut flow that every analysis ends up printing.
//
// The expressions are the same ones rdraw takes.
package rdf // import "go-hep.org/x/hep/groot/rtree/rdf"

import (
	"fmt"
	"math"

	"strings"

	"go-hep.org/x/hep/groot/rtree"
	"go-hep.org/x/hep/hbook"
	"go-hep.org/x/hep/internal/rexpr"
)

// Frame is a description of what to read from a tree and what to make of it.
//
// A Frame is immutable: Define and Filter return a new one, and the old one
// stays as it was, so a frame can be branched into several analyses that
// still share the one pass over the tree.
type Frame struct {
	tree  *shared
	steps []step
}

// shared is what every frame derived from the same tree has in common: the
// tree itself, and the actions waiting to be run over it.
type shared struct {
	tree    rtree.Tree
	actions []*action
	ran     bool
	err     error
	reports []reportOf

	start, n int64
}

// step is a Define or a Filter.
type step struct {
	kind  stepKind
	name  string
	expr  *rexpr.Expr
	title string
}

type stepKind int

const (
	stepDefine stepKind = iota
	stepFilter
)

// New starts a frame over the whole of a tree.
func New(t rtree.Tree) *Frame {
	return &Frame{tree: &shared{tree: t, start: 0, n: -1}}
}

// NewRange starts a frame over n entries of a tree, beginning at first. A
// negative n means to the end.
func NewRange(t rtree.Tree, first, n int64) *Frame {
	return &Frame{tree: &shared{tree: t, start: first, n: n}}
}

// Err returns the error the frame ran into, if any.
func (df *Frame) Err() error { return df.tree.err }

// Define adds a column computed from the columns already there.
//
// The column may be used by anything downstream of it, Define and Filter
// alike, and shadows a branch of the same name.
func (df *Frame) Define(name, expr string) *Frame {
	e, err := rexpr.New(expr)
	if err != nil {
		df.fail(fmt.Errorf("rdf: could not define %q: %w", name, err))
		return df
	}
	return df.with(step{kind: stepDefine, name: name, expr: e})
}

// Filter keeps the entries the expression is true for.
//
// The name, if one is given, is what Report calls this cut.
func (df *Frame) Filter(expr string, name ...string) *Frame {
	e, err := rexpr.New(expr)
	if err != nil {
		df.fail(fmt.Errorf("rdf: could not filter on %q: %w", expr, err))
		return df
	}

	title := expr
	if len(name) > 0 && name[0] != "" {
		title = name[0]
	}
	return df.with(step{kind: stepFilter, expr: e, title: title})
}

func (df *Frame) with(s step) *Frame {
	steps := make([]step, len(df.steps), len(df.steps)+1)
	copy(steps, df.steps)
	return &Frame{tree: df.tree, steps: append(steps, s)}
}

func (df *Frame) fail(err error) {
	if df.tree.err == nil {
		df.tree.err = err
	}
}

// action is one thing to compute while reading the tree.
type action struct {
	steps []step

	// fill is given the values of the columns for an entry that got this
	// far, and folds it into whatever the action is accumulating.
	fill func(vals map[string]float64) error

	// exprs are the expressions this action evaluates, beyond the steps.
	exprs []*rexpr.Expr

	done func() error
}

func (df *Frame) add(a *action) {
	a.steps = df.steps
	df.tree.actions = append(df.tree.actions, a)
}

// Result is a value that is not there until the frame has run.
type Result[T any] struct {
	df  *Frame
	val T
}

// Value returns the result, running the frame first if it has not run.
//
// Value panics if the frame failed: use Err, or call Run and check it, for
// the cases where that is not what is wanted.
func (r *Result[T]) Value() T {
	err := r.df.Run()
	if err != nil {
		panic(fmt.Errorf("rdf: %w", err))
	}
	return r.val
}

// Count counts the entries that reach this point.
func (df *Frame) Count() *Result[int64] {
	res := &Result[int64]{df: df}
	df.add(&action{
		fill: func(map[string]float64) error {
			res.val++
			return nil
		},
	})
	return res
}

// Sum adds up an expression over the entries that reach this point.
func (df *Frame) Sum(expr string) *Result[float64] {
	res := &Result[float64]{df: df}
	e, err := rexpr.New(expr)
	if err != nil {
		df.fail(fmt.Errorf("rdf: could not sum %q: %w", expr, err))
		return res
	}
	df.add(&action{
		exprs: []*rexpr.Expr{e},
		fill: func(vals map[string]float64) error {
			v, err := e.Eval(vals)
			if err != nil {
				return err
			}
			res.val += v
			return nil
		},
	})
	return res
}

// Mean averages an expression over the entries that reach this point.
func (df *Frame) Mean(expr string) *Result[float64] {
	res := &Result[float64]{df: df}
	e, err := rexpr.New(expr)
	if err != nil {
		df.fail(fmt.Errorf("rdf: could not average %q: %w", expr, err))
		return res
	}

	var (
		sum float64
		n   int64
	)
	df.add(&action{
		exprs: []*rexpr.Expr{e},
		fill: func(vals map[string]float64) error {
			v, err := e.Eval(vals)
			if err != nil {
				return err
			}
			sum += v
			n++
			return nil
		},
		done: func() error {
			if n > 0 {
				res.val = sum / float64(n)
			}
			return nil
		},
	})
	return res
}

// MinMax returns the smallest and largest value an expression takes over the
// entries that reach this point.
func (df *Frame) MinMax(expr string) *Result[[2]float64] {
	res := &Result[[2]float64]{df: df}
	e, err := rexpr.New(expr)
	if err != nil {
		df.fail(fmt.Errorf("rdf: could not range over %q: %w", expr, err))
		return res
	}

	lo, hi := math.Inf(+1), math.Inf(-1)
	df.add(&action{
		exprs: []*rexpr.Expr{e},
		fill: func(vals map[string]float64) error {
			v, err := e.Eval(vals)
			if err != nil {
				return err
			}
			lo = math.Min(lo, v)
			hi = math.Max(hi, v)
			return nil
		},
		done: func() error {
			res.val = [2]float64{lo, hi}
			return nil
		},
	})
	return res
}

// HOption configures a histogram.
type HOption func(*hcfg)

type hcfg struct {
	bins   [3]hbin
	name   string
	weight string
}

type hbin struct {
	n      int
	lo, hi float64
	set    bool
}

// Bins sets the binning of the first axis.
func Bins(n int, lo, hi float64) HOption {
	return func(c *hcfg) { c.bins[0] = hbin{n: n, lo: lo, hi: hi, set: true} }
}

// BinsY sets the binning of the second axis.
func BinsY(n int, lo, hi float64) HOption {
	return func(c *hcfg) { c.bins[1] = hbin{n: n, lo: lo, hi: hi, set: true} }
}

// BinsZ sets the binning of the third axis.
func BinsZ(n int, lo, hi float64) HOption {
	return func(c *hcfg) { c.bins[2] = hbin{n: n, lo: lo, hi: hi, set: true} }
}

// HName names the histogram.
func HName(name string) HOption {
	return func(c *hcfg) { c.name = name }
}

// HWeight fills with the value of an expression rather than with one.
func HWeight(expr string) HOption {
	return func(c *hcfg) { c.weight = expr }
}

func newHCfg(opts []HOption) *hcfg {
	c := &hcfg{}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Histo1D fills a 1-dim histogram from an expression.
//
// Unlike rdraw, the binning is not worked out from the data: a frame reads
// the tree once, and finding a range would mean reading it twice. Without
// Bins the histogram gets 100 bins over [0, 1).
func (df *Frame) Histo1D(expr string, opts ...HOption) *Result[*hbook.H1D] {
	cfg := newHCfg(opts)
	b := cfg.bins[0]
	if !b.set {
		b = hbin{n: 100, lo: 0, hi: 1}
	}

	res := &Result[*hbook.H1D]{df: df, val: hbook.NewH1D(b.n, b.lo, b.hi)}
	res.val.Ann["name"] = hname(cfg, expr)

	e, w, err := df.exprAndWeight(expr, cfg)
	if err != nil {
		df.fail(err)
		return res
	}

	df.add(&action{
		exprs: nonNil(e, w),
		fill: func(vals map[string]float64) error {
			x, err := e.Eval(vals)
			if err != nil {
				return err
			}
			wgt, err := weightOf(w, vals)
			if err != nil {
				return err
			}
			res.val.Fill(x, wgt)
			return nil
		},
	})
	return res
}

// Histo2D fills a 2-dim histogram, whose expression is "y:x" as in ROOT.
func (df *Frame) Histo2D(expr string, opts ...HOption) *Result[*hbook.H2D] {
	cfg := newHCfg(opts)
	for i := range 2 {
		if !cfg.bins[i].set {
			cfg.bins[i] = hbin{n: 50, lo: 0, hi: 1, set: true}
		}
	}

	res := &Result[*hbook.H2D]{df: df, val: hbook.NewH2D(
		cfg.bins[0].n, cfg.bins[0].lo, cfg.bins[0].hi,
		cfg.bins[1].n, cfg.bins[1].lo, cfg.bins[1].hi,
	)}
	res.val.Ann["name"] = hname(cfg, expr)

	axes, w, err := df.axesAndWeight(expr, 2, cfg)
	if err != nil {
		df.fail(err)
		return res
	}

	df.add(&action{
		exprs: append(axes, nonNil(w)...),
		fill: func(vals map[string]float64) error {
			x, err := axes[0].Eval(vals)
			if err != nil {
				return err
			}
			y, err := axes[1].Eval(vals)
			if err != nil {
				return err
			}
			wgt, err := weightOf(w, vals)
			if err != nil {
				return err
			}
			res.val.Fill(x, y, wgt)
			return nil
		},
	})
	return res
}

// Histo3D fills a 3-dim histogram, whose expression is "z:y:x" as in ROOT.
func (df *Frame) Histo3D(expr string, opts ...HOption) *Result[*hbook.H3D] {
	cfg := newHCfg(opts)
	for i := range 3 {
		if !cfg.bins[i].set {
			cfg.bins[i] = hbin{n: 25, lo: 0, hi: 1, set: true}
		}
	}

	res := &Result[*hbook.H3D]{df: df, val: hbook.NewH3D(
		cfg.bins[0].n, cfg.bins[0].lo, cfg.bins[0].hi,
		cfg.bins[1].n, cfg.bins[1].lo, cfg.bins[1].hi,
		cfg.bins[2].n, cfg.bins[2].lo, cfg.bins[2].hi,
	)}
	res.val.Ann["name"] = hname(cfg, expr)

	axes, w, err := df.axesAndWeight(expr, 3, cfg)
	if err != nil {
		df.fail(err)
		return res
	}

	df.add(&action{
		exprs: append(axes, nonNil(w)...),
		fill: func(vals map[string]float64) error {
			x, err := axes[0].Eval(vals)
			if err != nil {
				return err
			}
			y, err := axes[1].Eval(vals)
			if err != nil {
				return err
			}
			z, err := axes[2].Eval(vals)
			if err != nil {
				return err
			}
			wgt, err := weightOf(w, vals)
			if err != nil {
				return err
			}
			res.val.Fill(x, y, z, wgt)
			return nil
		},
	})
	return res
}

func hname(cfg *hcfg, expr string) string {
	if cfg.name != "" {
		return cfg.name
	}
	return expr
}

func (df *Frame) exprAndWeight(expr string, cfg *hcfg) (e, w *rexpr.Expr, err error) {
	e, err = rexpr.New(expr)
	if err != nil {
		return nil, nil, fmt.Errorf("rdf: could not histogram %q: %w", expr, err)
	}
	if cfg.weight != "" {
		w, err = rexpr.New(cfg.weight)
		if err != nil {
			return nil, nil, fmt.Errorf("rdf: bad weight %q: %w", cfg.weight, err)
		}
	}
	return e, w, nil
}

// axesAndWeight compiles the axes of a multi-dimensional histogram, turning
// ROOT's "z:y:x" round into the order the histogram wants.
func (df *Frame) axesAndWeight(expr string, rank int, cfg *hcfg) ([]*rexpr.Expr, *rexpr.Expr, error) {
	srcs := splitAxes(expr)
	if len(srcs) != rank {
		return nil, nil, fmt.Errorf(
			"rdf: %q gives %d expression(s), want %d",
			expr, len(srcs), rank,
		)
	}

	axes := make([]*rexpr.Expr, rank)
	for i, src := range srcs {
		e, err := rexpr.New(src)
		if err != nil {
			return nil, nil, fmt.Errorf("rdf: could not histogram %q: %w", src, err)
		}
		axes[rank-1-i] = e
	}

	var w *rexpr.Expr
	if cfg.weight != "" {
		var err error
		w, err = rexpr.New(cfg.weight)
		if err != nil {
			return nil, nil, fmt.Errorf("rdf: bad weight %q: %w", cfg.weight, err)
		}
	}

	return axes, w, nil
}

// splitAxes cuts an expression on the colons that separate its axes, leaving
// alone the "::" that names a C++ namespace.
func splitAxes(expr string) []string {
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

func weightOf(w *rexpr.Expr, vals map[string]float64) (float64, error) {
	if w == nil {
		return 1, nil
	}
	return w.Eval(vals)
}

func nonNil(es ...*rexpr.Expr) []*rexpr.Expr {
	var o []*rexpr.Expr
	for _, e := range es {
		if e != nil {
			o = append(o, e)
		}
	}
	return o
}

// CutInfo says how one filter fared.
type CutInfo struct {
	Name string
	Pass int64 // entries that passed it
	All  int64 // entries that reached it
}

// Eff returns the fraction of the entries reaching this cut that passed it.
func (c CutInfo) Eff() float64 {
	if c.All == 0 {
		return 0
	}
	return float64(c.Pass) / float64(c.All)
}

// Report returns the cut flow of this frame: what each filter was given and
// what it kept.
func (df *Frame) Report() *Result[[]CutInfo] {
	res := &Result[[]CutInfo]{df: df}

	var cuts []int
	for i, s := range df.steps {
		if s.kind == stepFilter {
			cuts = append(cuts, i)
		}
	}

	info := make([]CutInfo, len(cuts))
	for i, idx := range cuts {
		info[i] = CutInfo{Name: df.steps[idx].title}
	}

	df.add(&action{
		// the counting is done by run, which is the only place that knows
		// which filter an entry stopped at.
		fill: func(map[string]float64) error { return nil },
		done: func() error {
			res.val = info
			return nil
		},
	})

	df.tree.reports = append(df.tree.reports, reportOf{steps: df.steps, info: info})
	return res
}

type reportOf struct {
	steps []step
	info  []CutInfo
}
