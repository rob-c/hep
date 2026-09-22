// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package rt is what a macro translated from CINT calls into.
//
// Most of a macro translates to plain go-hep: a histogram is an rhist one, a
// file is a riofs one, TMath is math. What is left over is the part of ROOT
// that is a session rather than a library — the current canvas, the current
// file, gRandom — which a Go program has no equivalent of and which this
// package provides.
//
// Nothing here is meant to be written by hand. It is what the translation
// emits, and it is kept small and readable so that the Go a translation
// produces can be taken away and edited.
package rt // import "go-hep.org/x/hep/cint/rt"

import (
	"fmt"
	"math"
	"math/rand/v2"
	"strings"

	"go-hep.org/x/hep/groot"
	"go-hep.org/x/hep/groot/rhist"
	"go-hep.org/x/hep/groot/riofs"
	"go-hep.org/x/hep/groot/root"
	"go-hep.org/x/hep/groot/rtree"
	"go-hep.org/x/hep/groot/rtree/rdraw"
	"go-hep.org/x/hep/hbook"
	"go-hep.org/x/hep/internal/hepmath"
	"gonum.org/v1/plot/plotter"
)

// Must panics if err is not nil, and otherwise gives back the value.
//
// A macro has nowhere to put an error: CINT stops on one and says what it
// was, and so does this.
func Must[T any](v T, err error) T {
	if err != nil {
		panic(fmt.Errorf("cint: %w", err))
	}
	return v
}

// ---------------------------------------------------------------- files

var openFiles []*riofs.File

// OpenFile opens or creates a file, choosing by the mode the way TFile does.
func OpenFile(name, mode string) *riofs.File {
	var (
		f   *riofs.File
		err error
	)
	switch strings.ToUpper(strings.TrimSpace(mode)) {
	case "", "READ":
		f, err = groot.Open(name)
	case "RECREATE", "CREATE", "NEW", "UPDATE":
		f, err = groot.Create(name)
	default:
		panic(fmt.Errorf("cint: unknown file mode %q", mode))
	}
	if err != nil {
		panic(fmt.Errorf("cint: could not open %q: %w", name, err))
	}

	openFiles = append(openFiles, f)
	return f
}

// GFile is the file most recently opened, which is what ROOT calls gFile.
func GFile() *riofs.File {
	if len(openFiles) == 0 {
		return nil
	}
	return openFiles[len(openFiles)-1]
}

// Get reads an object out of a file by name.
func Get(f *riofs.File, name string) root.Object {
	return Must(f.Get(name))
}

// Write puts an object into the file that is open, under its own name.
func Write(obj root.Object) {
	f := GFile()
	if f == nil {
		panic(fmt.Errorf("cint: nothing to write %T into: no file is open", obj))
	}
	named, ok := obj.(interface{ Name() string })
	if !ok {
		panic(fmt.Errorf("cint: a %T has no name to write it under", obj))
	}
	if err := f.Put(named.Name(), obj); err != nil {
		panic(fmt.Errorf("cint: could not write %q: %w", named.Name(), err))
	}
}

// WriteFile closes off the file, which is what TFile::Write does.
func WriteFile(f *riofs.File) {
	if err := f.Close(); err != nil {
		panic(fmt.Errorf("cint: could not write the file: %w", err))
	}
}

// ---------------------------------------------------------------- trees

// TreeDraw fills a histogram from a tree by expression, as TTree::Draw does.
func TreeDraw(t rtree.Tree, expr, cut string) *hbook.H1D {
	var opts []rdraw.Option
	if cut != "" {
		opts = append(opts, rdraw.Cut(cut))
	}
	h := Must(rdraw.H1D(t, expr, opts...))
	Draw(h)
	return h
}

// TreeScan prints an expression for the first entries of a tree, as
// TTree::Scan does.
func TreeScan(t rtree.Tree, expr string) {
	if expr == "" {
		fmt.Printf("tree %q: %d entries\n", t.Name(), t.Entries())
		return
	}
	h := Must(rdraw.H1D(t, expr))
	fmt.Printf("%s: %d entries, mean %v\n", expr, h.Entries(), h.XMean())
}

// ---------------------------------------------------------------- functions

// NewF1 builds a function from a formula, as TF1 does.
func NewF1(name, expr string, lo, hi float64) *rhist.F1 {
	return Must(rhist.NewF1(name, expr, lo, hi))
}

// Eval evaluates a function at a point.
func Eval(f *rhist.F1, x float64) float64 {
	return Must(f.Eval(x))
}

// ---------------------------------------------------------------- random

// Random is a source of random numbers, in the manner of TRandom.
type Random struct {
	src *rand.Rand
}

// NewRandom returns a source seeded with the given seed. A seed of zero
// gives a source that starts somewhere different every run, as TRandom does.
func NewRandom(seed uint64) *Random {
	if seed == 0 {
		return &Random{src: rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))}
	}
	return &Random{src: rand.New(rand.NewPCG(seed, seed+1))}
}

// GRandom is the source a macro reaches for without making one, which is
// what ROOT calls gRandom.
var GRandom = NewRandom(4357)

func (r *Random) SetSeed(seed uint64) { *r = *NewRandom(seed) }

// Rndm returns a number in [0, 1).
func (r *Random) Rndm() float64 { return r.src.Float64() }

// Uniform returns a number spread evenly over [lo, hi).
func (r *Random) Uniform(lo, hi float64) float64 { return lo + (hi-lo)*r.src.Float64() }

// Gaus returns a number from a gaussian of the given mean and width.
func (r *Random) Gaus(mean, sigma float64) float64 { return mean + sigma*r.src.NormFloat64() }

// Exp returns a number from an exponential of the given mean.
func (r *Random) Exp(tau float64) float64 { return tau * r.src.ExpFloat64() }

// Integer returns a whole number in [0, n).
func (r *Random) Integer(n int) int { return r.src.IntN(n) }

// Poisson returns a count drawn from a Poisson of the given mean.
func (r *Random) Poisson(mean float64) float64 {
	// Knuth's method, which is fine for the means a macro deals in.
	var (
		limit = math.Exp(-mean)
		p     = 1.0
		k     = 0
	)
	for {
		p *= r.src.Float64()
		if p <= limit {
			return float64(k)
		}
		k++
	}
}

// Landau returns a number from a Landau of the given location and scale.
func (r *Random) Landau(mpv, sigma float64) float64 {
	u := r.src.Float64()
	for u <= 0 || u >= 1 {
		u = r.src.Float64()
	}
	return mpv + sigma*landauQuantile(u)
}

// landauQuantile inverts the Landau distribution by bisection.
//
// The distribution has no closed-form inverse, but its cumulative form is
// to hand and rises monotonically, so bisecting it is both simple and as
// accurate as the cumulative itself.
func landauQuantile(u float64) float64 {
	lo, hi := -5.0, 60.0
	switch {
	case u <= hepmath.LandauCDF(lo):
		return lo
	case u >= hepmath.LandauCDF(hi):
		return hi
	}
	for range 60 {
		mid := 0.5 * (lo + hi)
		if hepmath.LandauCDF(mid) < u {
			lo = mid
			continue
		}
		hi = mid
	}
	return 0.5 * (lo + hi)
}

// ---------------------------------------------------------------- style

// Style is the collection of settings ROOT calls gStyle. Nothing here
// changes what a plot holds, only how it looks.
type Style struct {
	OptStat int
	OptFit  int
	Title   string
}

// GStyle is the one a macro reaches for.
var GStyle = &Style{}

func (s *Style) SetOptStat(v int)  { s.OptStat = v }
func (s *Style) SetOptFit(v int)   { s.OptFit = v }
func (s *Style) SetOptTitle(v int) {}
func (s *Style) SetPalette(v int)  {}

// ---------------------------------------------------------------- graphs

// Graph is a set of points, in the manner of TGraph.
type Graph struct {
	name string
	xs   []float64
	ys   []float64
}

// NewGraph returns an empty graph.
func NewGraph() *Graph { return &Graph{} }

// SetPoint places the i-th point, growing the graph if it has to.
func (g *Graph) SetPoint(i int, x, y float64) {
	for len(g.xs) <= i {
		g.xs = append(g.xs, 0)
		g.ys = append(g.ys, 0)
	}
	g.xs[i], g.ys[i] = x, y
}

// AddPoint puts one more point on the end.
func (g *Graph) AddPoint(x, y float64) {
	g.xs = append(g.xs, x)
	g.ys = append(g.ys, y)
}

// N returns how many points the graph holds.
func (g *Graph) N() int { return len(g.xs) }

func (g *Graph) SetTitle(s string) { g.name = s }

// XY makes the graph readable by gonum's plotters.
func (g *Graph) XY() plotter.XYs {
	out := make(plotter.XYs, len(g.xs))
	for i := range g.xs {
		out[i] = plotter.XY{X: g.xs[i], Y: g.ys[i]}
	}
	return out
}
