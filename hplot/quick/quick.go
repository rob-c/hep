// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package quick draws a slice of numbers, in one call.
//
// A plot needs a binning, a range, a plotter, a canvas and a size before it is
// a file on disk, and all five have to be decided before any of the data has
// been looked at. This package decides them from the numbers themselves:
//
//	err := quick.Hist("px.png", px)
//
// That is a histogram of px, binned the way the sample asks to be binned,
// over a range that holds all of it, saved as a PNG. The kind of file is read
// off the name: .png, .pdf, .svg, .tex and the rest that [go-hep.org/x/hep/hplot]
// writes.
//
// # From a ROOT file to a plot
//
// [go-hep.org/x/hep/groot/rdata] hands back the columns of a tree as slices of
// numbers, which is what everything here takes:
//
//	t, err := rdata.Open("data.root")
//	defer t.Close()
//
//	px, err := t.Floats("px")
//	err = quick.Hist("px.png", px, quick.Title("px of every event"))
//
// # The other plots
//
// [Hists] draws several histograms over each other with a legend, which is the
// signal-against-background plot. [Hist2D] draws one quantity against another
// as a map of counts, [Scatter] as points and [Line] as a line.
//
// # The choices, and taking them back
//
// The number of bins comes from the Freedman-Diaconis rule — the spread of the
// middle half of the sample, over the cube root of its size — which is narrow
// where the data is dense and wide where it is not. The range is the range of
// the data, rounded outward to a round number so the axis reads well. Values
// that are not finite are left out, since NaN belongs in no bin.
//
// [Bins], [Range], [Title], [XLabel], [YLabel], [LogY], [NoStats] and [Size]
// each take one of those decisions back. [Bin] returns the histogram rather
// than drawing it, for when the numbers are wanted as counts.
//
// # When more control is needed
//
// [go-hep.org/x/hep/hbook] is the histogram, [go-hep.org/x/hep/hplot] is the
// plot, and both are there for everything this package does not do: several
// plots on one canvas, fits, error bands, styles and the rest.
package quick // import "go-hep.org/x/hep/hplot/quick"

import (
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"

	"go-hep.org/x/hep/hbook"
	"go-hep.org/x/hep/hplot"
	"gonum.org/v1/plot/vg"
)

// Option is a decision this package would otherwise take on its own.
type Option func(cfg *config)

// Title sets the title written above the plot.
func Title(s string) Option { return func(cfg *config) { cfg.title = s } }

// XLabel sets the label of the horizontal axis. It defaults to the title,
// since the quantity being plotted is nearly always what the plot is about.
func XLabel(s string) Option { return func(cfg *config) { cfg.xlabel, cfg.xset = s, true } }

// YLabel sets the label of the vertical axis, which is "Events" for a
// histogram and empty for the rest.
func YLabel(s string) Option { return func(cfg *config) { cfg.ylabel, cfg.yset = s, true } }

// Bins sets how many bins to use, instead of letting the sample decide. It is
// taken as given: the ceiling of two hundred bins is on the automatic rule,
// not on the caller.
func Bins(n int) Option { return func(cfg *config) { cfg.bins = n } }

// Range sets the range to histogram, instead of taking it from the data.
//
// Values outside it are not dropped: they are counted as underflow and
// overflow, which the summary box reports.
func Range(lo, hi float64) Option {
	return func(cfg *config) { cfg.lo, cfg.hi, cfg.rng = lo, hi, true }
}

// LogY scales the vertical axis logarithmically, which is what a spectrum
// falling over several orders of magnitude needs to be readable.
func LogY() Option { return func(cfg *config) { cfg.logy = true } }

// NoStats hides the box giving the number of entries, the mean and the RMS.
func NoStats() Option { return func(cfg *config) { cfg.stats = false } }

// Size sets the size of the plot in inches. A height of zero, which is the
// default, is the width divided by the golden ratio.
func Size(w, h float64) Option { return func(cfg *config) { cfg.w, cfg.h = w, h } }

type config struct {
	title  string
	xlabel string
	ylabel string
	xset   bool
	yset   bool

	bins int
	lo   float64
	hi   float64
	rng  bool

	logy  bool
	stats bool

	w float64
	h float64
}

// newConfig is where the defaults of this package are written down.
func newConfig(opts []Option) *config {
	cfg := &config{
		stats: true,
		w:     6, // inches: a plot that fills a slide and still fits in a paper.
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// Bin histograms a column of numbers, choosing the binning the same way the
// plots in this package do, and returns the histogram instead of drawing it.
//
// Values that are not finite are left out.
func Bin(data []float64, opts ...Option) (*hbook.H1D, error) {
	return bin(data, newConfig(opts))
}

func bin(data []float64, cfg *config) (*hbook.H1D, error) {
	xs, err := finite(data)
	if err != nil {
		return nil, err
	}

	n, lo, hi, err := binning(xs, cfg, 200)
	if err != nil {
		return nil, err
	}

	h := hbook.NewH1D(n, lo, hi)
	for _, x := range xs {
		h.Fill(x, 1)
	}
	return h, nil
}

// finite returns the values that can be binned, sorted, and says so when there
// are none.
func finite(data []float64) ([]float64, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("quick: there are no numbers to plot")
	}

	xs := make([]float64, 0, len(data))
	for _, x := range data {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			continue
		}
		xs = append(xs, x)
	}
	if len(xs) == 0 {
		return nil, fmt.Errorf("quick: none of these %d numbers is finite: there is nothing to bin", len(data))
	}
	slices.Sort(xs)
	return xs, nil
}

// binning decides how many bins to use and where they start and stop. xs is
// sorted and holds at least one value; max is the largest number of bins worth
// drawing for this kind of plot.
func binning(xs []float64, cfg *config, max int) (n int, lo, hi float64, err error) {
	var (
		xmin = xs[0]
		xmax = xs[len(xs)-1]
	)

	if cfg.rng && cfg.lo >= cfg.hi {
		return 0, 0, 0, fmt.Errorf("quick: [%v, %v] is not a range to bin over: it has to start below where it stops",
			cfg.lo, cfg.hi)
	}

	n = cfg.bins
	if n <= 0 {
		n = clamp(rule(xs), max)
	}

	switch {
	case cfg.rng:
		return n, cfg.lo, cfg.hi, nil

	case xmin == xmax:
		// A column that never changes still deserves a plot: one bin, with the
		// value in the middle of it rather than on its edge.
		pad := math.Abs(xmin) * 0.05
		if pad == 0 {
			pad = 0.5
		}
		return n, xmin - pad, xmax + pad, nil

	case cfg.bins > 0:
		// The caller has asked for this many bins, so the range cannot be
		// rounded to a step that would change it. It is widened by a sliver
		// instead, so that the largest value lands in the last bin rather than
		// in the overflow.
		return n, xmin, xmax + (xmax-xmin)*1e-6, nil
	}

	// Round the edges outward to a multiple of a readable step, which is what
	// makes the axis say 0, 20, 40 rather than 3.7, 23.1, 42.5.
	step := niceStep((xmax - xmin) / float64(n))
	lo = math.Floor(xmin/step) * step
	hi = math.Ceil(xmax/step) * step
	if hi <= xmax {
		hi += step
	}

	n = clamp(int(math.Round((hi-lo)/step)), max)
	return n, lo, hi, nil
}

// rule is the Freedman-Diaconis rule: bins as wide as twice the spread of the
// middle half of the sample, over the cube root of its size. It follows the
// data where a fixed count cannot, and falls back on Sturges' rule for the
// samples it says nothing about — the very small, and the ones whose middle
// half is a single value.
func rule(xs []float64) int {
	n := len(xs)
	if n < 4 {
		return n
	}

	var (
		iqr  = quantile(xs, 0.75) - quantile(xs, 0.25)
		span = xs[n-1] - xs[0]
	)
	if w := 2 * iqr / math.Cbrt(float64(n)); w > 0 && span > 0 {
		return int(math.Ceil(span / w))
	}
	return int(math.Ceil(math.Log2(float64(n)))) + 1
}

// quantile is the value below which that fraction of a sorted sample lies,
// interpolated between the two values it falls between.
func quantile(xs []float64, p float64) float64 {
	if len(xs) == 1 {
		return xs[0]
	}
	pos := p * float64(len(xs)-1)
	i := int(pos)
	if i >= len(xs)-1 {
		return xs[len(xs)-1]
	}
	return xs[i] + (pos-float64(i))*(xs[i+1]-xs[i])
}

// niceStep is the round number nearest x — 1, 2, 2.5, 5 or 10 times a power of
// ten — measured as a ratio, since a bin twice as wide as asked for is as far
// off as one half as wide.
func niceStep(x float64) float64 {
	if x <= 0 || math.IsInf(x, 0) {
		return 1
	}
	var (
		pow  = math.Pow(10, math.Floor(math.Log10(x)))
		best = pow
		dist = math.Inf(+1)
	)
	for _, f := range []float64{1, 2, 2.5, 5, 10} {
		step := f * pow
		if d := math.Abs(math.Log(step / x)); d < dist {
			best, dist = step, d
		}
	}
	return best
}

func clamp(n, max int) int {
	switch {
	case n < 1:
		return 1
	case n > max:
		return max
	}
	return n
}

// formats are the kinds of file hplot writes, which is how the name of the
// file gets to say what kind of plot it is.
var formats = []string{".eps", ".jpeg", ".jpg", ".json", ".pdf", ".png", ".svg", ".tex", ".tif", ".tiff"}

// save writes the plot, having first made sure the name says what to write.
func save(p *hplot.Plot, dst string, cfg *config) error {
	if dst == "" {
		return fmt.Errorf("quick: the plot needs a file name to be saved under")
	}

	ext := strings.ToLower(filepath.Ext(dst))
	if !slices.Contains(formats, ext) {
		return fmt.Errorf("quick: %q does not say what kind of plot to make: end the name in %s",
			filepath.Base(dst), strings.Join(formats, ", "))
	}

	err := hplot.Save(p, vg.Length(cfg.w)*vg.Inch, vg.Length(cfg.h)*vg.Inch, dst)
	if err != nil {
		return fmt.Errorf("quick: could not save %q: %w", dst, err)
	}
	return nil
}
