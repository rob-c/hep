// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package quick

import (
	"fmt"
	"image/color"
	"math"

	"go-hep.org/x/hep/hbook"
	"go-hep.org/x/hep/hplot"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/palette/brewer"
	"gonum.org/v1/plot/plotutil"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
)

// Hist draws a histogram of a column of numbers and saves it, choosing the
// binning, the range and the size of the plot from the data.
//
//	err := quick.Hist("px.png", px, quick.Title("px of every event"))
//
// The name of the file says what kind of plot to write — .png, .pdf, .svg and
// the rest hplot supports. Values that are not finite are left out.
func Hist(dst string, data []float64, opts ...Option) error {
	cfg := newConfig(opts)

	h, err := bin(data, cfg)
	if err != nil {
		return err
	}

	p := newPlot(cfg, "Events", true)
	p.Add(draw1D(h, cfg, 0, true))

	return save(p, dst, cfg)
}

// Series is one named column among several drawn together.
type Series struct {
	Name string
	Data []float64
}

// Hists draws several histograms over each other, each in its own colour and
// named in a legend, which is how a signal is compared with a background or
// one run with the next.
//
//	err := quick.Hists("mass.png", []quick.Series{
//		{Name: "data", Data: obs},
//		{Name: "simulation", Data: sim},
//	})
//
// They share one binning, taken from all of the values together, so that the
// bins line up and the shapes can be compared. The summary box is left off
// here: the legend is what names the curves.
func Hists(dst string, series []Series, opts ...Option) error {
	if len(series) == 0 {
		return fmt.Errorf("quick: there are no series to plot")
	}

	cfg := newConfig(opts)
	cfg.stats = false

	var all []float64
	for i, s := range series {
		if s.Name == "" {
			return fmt.Errorf("quick: series %d has no name: the legend is made of them", i)
		}
		all = append(all, s.Data...)
	}

	// One binning for all of them, so that bin 3 means the same thing in each.
	xs, err := finite(all)
	if err != nil {
		return err
	}
	n, lo, hi, err := binning(xs, cfg, 200)
	if err != nil {
		return err
	}

	p := newPlot(cfg, "Events", true)
	for i, s := range series {
		h := hbook.NewH1D(n, lo, hi)
		for _, x := range s.Data {
			h.Fill(x, 1)
		}
		plt := draw1D(h, cfg, i, false)
		p.Add(plt)
		p.Legend.Add(s.Name, plt)
	}
	p.Legend.Top = true

	return save(p, dst, cfg)
}

// Hist2D draws one quantity against another as a map of counts, which says
// where the events are when there are too many of them to draw as points.
//
// Both axes are binned automatically; [Bins] sets the number of bins of each
// of them and [Range] the range of the horizontal one. [LogY] has nothing to
// scale here and is ignored.
func Hist2D(dst string, x, y []float64, opts ...Option) error {
	cfg := newConfig(opts)

	if err := sameLength(x, y); err != nil {
		return err
	}

	xs, err := finite(x)
	if err != nil {
		return err
	}
	ys, err := finite(y)
	if err != nil {
		return err
	}

	// A hundred bins a side is ten thousand cells: past that a plot of any
	// usual size is drawing more cells than it has pixels.
	nx, xlo, xhi, err := binning(xs, cfg, 100)
	if err != nil {
		return err
	}
	ycfg := *cfg
	ycfg.rng = false
	ny, ylo, yhi, err := binning(ys, &ycfg, 100)
	if err != nil {
		return err
	}

	h := hbook.NewH2D(nx, xlo, xhi, ny, ylo, yhi)
	for i := range x {
		if finiteAt(x[i]) && finiteAt(y[i]) {
			h.Fill(x[i], y[i], 1)
		}
	}

	cfg.logy = false // the counts are the colour here, not the vertical axis.

	// No grid: a mesh drawn over a map of colours is read as part of the map.
	p := newPlot(cfg, "", false)
	p.Add(hplot.NewH2D(h, counts))

	return save(p, dst, cfg)
}

// Scatter draws one quantity against another as points, which is what to use
// when there are few enough events to see them one by one.
func Scatter(dst string, x, y []float64, opts ...Option) error {
	cfg := newConfig(opts)

	if err := sameLength(x, y); err != nil {
		return err
	}

	if err := logSafe(cfg, y); err != nil {
		return err
	}

	s, err := hplot.NewScatter(hplot.ZipXY(x, y))
	if err != nil {
		return fmt.Errorf("quick: could not draw these points: %w", err)
	}
	s.GlyphStyle = draw.GlyphStyle{
		Color:  plotutil.Color(0),
		Radius: vg.Points(2),
		Shape:  draw.CircleGlyph{},
	}

	p := newPlot(cfg, "", true)
	p.Add(s)

	return save(p, dst, cfg)
}

// Line draws one quantity against another as a line, in the order the values
// are given, which is what a measurement against time or against a scan
// parameter looks like.
func Line(dst string, x, y []float64, opts ...Option) error {
	cfg := newConfig(opts)

	if err := sameLength(x, y); err != nil {
		return err
	}

	if err := logSafe(cfg, y); err != nil {
		return err
	}

	l, err := hplot.NewLine(hplot.ZipXY(x, y))
	if err != nil {
		return fmt.Errorf("quick: could not draw this line: %w", err)
	}
	l.Color = plotutil.Color(0)
	l.Width = vg.Points(1.5)

	p := newPlot(cfg, "", true)
	p.Add(l)

	return save(p, dst, cfg)
}

// newPlot is the canvas every plot in this package starts from: titled,
// labelled, gridded, and scaled the way the options asked for.
func newPlot(cfg *config, ylabel string, grid bool) *hplot.Plot {
	p := hplot.New()
	p.Title.Text = cfg.title

	// The axis label is nearly always the quantity the plot is about, so the
	// title stands in for it until something else is asked for.
	p.X.Label.Text = cfg.title
	if cfg.xset {
		p.X.Label.Text = cfg.xlabel
	}
	p.Y.Label.Text = ylabel
	if cfg.yset {
		p.Y.Label.Text = cfg.ylabel
	}

	if cfg.logy {
		p.Y.Scale = plot.LogScale{}
		p.Y.Tick.Marker = plot.LogTicks{}
	}

	if grid {
		p.Add(hplot.NewGrid())
	}
	return p
}

// draw1D styles a histogram: filled when it is the only one, outlined when it
// is drawn over its neighbours and would hide them.
func draw1D(h *hbook.H1D, cfg *config, i int, filled bool) *hplot.H1D {
	plt := hplot.NewH1D(h, hplot.WithLogY(cfg.logy))
	plt.LineStyle.Color = plotutil.Color(i)
	plt.LineStyle.Width = vg.Points(1.5)
	if filled {
		plt.FillColor = fade(plotutil.Color(i), 0x40)
	}
	if cfg.stats {
		plt.Infos.Style = hplot.HInfoSummary
	}
	return plt
}

// counts is the colour scale of a map of counts: pale where there is nothing
// and dark where the events are, which is the way round a sequential scale is
// read. The default of hplot is a diverging scale, which says "far from the
// middle" rather than "many", and paints the empty half of the plot red.
var counts, _ = brewer.GetPalette(brewer.TypeSequential, "YlGnBu", 9)

// fade is a colour at a given opacity, for a fill that does not hide what is
// drawn under it.
func fade(c color.Color, a uint8) color.NRGBA {
	r, g, b, _ := c.RGBA()
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: a}
}

// logSafe refuses a logarithmic axis that would have to hold a number at or
// below zero, which the scale underneath does not survive being asked to do.
func logSafe(cfg *config, y []float64) error {
	if !cfg.logy {
		return nil
	}
	for _, v := range y {
		if v <= 0 {
			return fmt.Errorf("quick: LogY cannot draw %v: a logarithmic axis holds only numbers above zero", v)
		}
	}
	return nil
}

func sameLength(x, y []float64) error {
	if len(x) != len(y) {
		return fmt.Errorf("quick: there are %d values along x and %d along y: a point needs both", len(x), len(y))
	}
	if len(x) == 0 {
		return fmt.Errorf("quick: there are no numbers to plot")
	}
	return nil
}

func finiteAt(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }
