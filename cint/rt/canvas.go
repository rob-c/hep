// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rt

import (
	"fmt"
	"math"

	"go-hep.org/x/hep/groot/rhist"
	"go-hep.org/x/hep/hbook"
	"go-hep.org/x/hep/hbook/rootcnv"
	"go-hep.org/x/hep/hplot"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/vg"
)

// Canvas is what a macro draws on, in the manner of TCanvas.
//
// ROOT draws as it goes, on a window. A translated macro has no window, so a
// canvas collects what was drawn on it and writes it out when the macro asks
// it to, which is what SaveAs was going to do anyway.
type Canvas struct {
	Name string
	W, H float64

	pads []pad
	cur  int
}

type pad struct {
	drawn []any
}

var canvases []*Canvas

// NewCanvas returns a canvas of the given size in pixels.
func NewCanvas(name string, w, h float64) *Canvas {
	c := &Canvas{Name: name, W: w, H: h, pads: []pad{{}}}
	canvases = append(canvases, c)
	return c
}

// GPad is the canvas being drawn on, which is what ROOT calls gPad. Drawing
// without making a canvas first makes one, as it does in ROOT.
func GPad() *Canvas {
	if len(canvases) == 0 {
		return NewCanvas("c", 800, 600)
	}
	return canvases[len(canvases)-1]
}

// Divide splits the canvas into a grid of pads.
func (c *Canvas) Divide(nx int, ny ...int) {
	n := nx
	if len(ny) > 0 {
		n = nx * ny[0]
	}
	if n < 1 {
		n = 1
	}
	c.pads = make([]pad, n)
	c.cur = 0
}

// Cd moves to the i-th pad, counting from one as ROOT does.
func (c *Canvas) Cd(i int) {
	switch {
	case i <= 0:
		c.cur = 0
	case i > len(c.pads):
		c.cur = len(c.pads) - 1
	default:
		c.cur = i - 1
	}
}

// Clear forgets everything drawn on the canvas.
func (c *Canvas) Clear() {
	for i := range c.pads {
		c.pads[i].drawn = nil
	}
}

func (c *Canvas) add(obj any) {
	if len(c.pads) == 0 {
		c.pads = []pad{{}}
	}
	c.pads[c.cur].drawn = append(c.pads[c.cur].drawn, obj)
}

// Draw puts an object on the canvas being drawn on.
//
// The option string is ROOT's, which says how to draw rather than what: it
// is kept so that a translation does not lose it, and only "same" changes
// anything here, since everything on a pad is drawn together regardless.
func Draw(obj any, opts ...string) {
	GPad().add(obj)
}

// SaveAs writes the canvas out. The format is taken from the name, as ROOT
// takes it.
func (c *Canvas) SaveAs(name string) {
	if err := c.save(name); err != nil {
		panic(fmt.Errorf("cint: could not save %q: %w", name, err))
	}
}

func (c *Canvas) save(name string) error {
	// every pad on one plot: a translated macro that divided a canvas is
	// asking for several pictures, and one file was asked for.
	p := hplot.New()
	p.Title.Text = c.Name

	n := 0
	for _, pd := range c.pads {
		for _, obj := range pd.drawn {
			ps, err := plotterOf(obj)
			if err != nil {
				return err
			}
			p.Add(ps...)
			n++
		}
	}
	if n == 0 {
		return fmt.Errorf("nothing was drawn on canvas %q", c.Name)
	}

	// a canvas is sized in pixels, which a vector plot measures in points.
	w := vg.Length(c.W)
	h := vg.Length(c.H)
	if w <= 0 || h <= 0 {
		w, h = 20*vg.Centimeter, 15*vg.Centimeter
	}
	return p.Save(w, h, name)
}

// plotterOf turns what a macro drew into something that can be plotted.
func plotterOf(obj any) ([]plot.Plotter, error) {
	switch obj := obj.(type) {
	case *hbook.H1D:
		return []plot.Plotter{hplot.NewH1D(obj)}, nil

	case *hbook.H2D:
		return []plot.Plotter{hplot.NewH2D(obj, nil)}, nil

	case *hbook.S2D:
		return []plot.Plotter{hplot.NewS2D(obj)}, nil

	case rhist.H1:
		return []plot.Plotter{hplot.NewH1D(rootcnv.H1D(obj))}, nil

	case rhist.H2:
		return []plot.Plotter{hplot.NewH2D(rootcnv.H2D(obj), nil)}, nil

	case *Graph:
		s, err := hplot.NewScatter(obj.XY())
		if err != nil {
			return nil, err
		}
		return []plot.Plotter{s}, nil

	case *rhist.F1:
		f := hplot.NewFunction(func(x float64) float64 {
			v, err := obj.Eval(x)
			if err != nil {
				return math.NaN()
			}
			return v
		})
		return []plot.Plotter{f}, nil
	}

	return nil, fmt.Errorf("cint: nothing is known about how to draw a %T", obj)
}
