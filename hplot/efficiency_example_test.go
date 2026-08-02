// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hplot_test

import (
	"image/color"
	"log"
	"math"
	"math/rand/v2"

	"go-hep.org/x/hep/hbook"
	"go-hep.org/x/hep/hplot"
	"gonum.org/v1/plot/vg"
)

// ExampleS2D_efficiency draws a selection efficiency as a function of
// transverse momentum, with the binomial confidence interval as the error bar.
//
// *hbook.Efficiency satisfies the gonum/plot XYer, XErrorer and YErrorer
// interfaces, so it goes straight into hplot.NewS2D without a conversion.
func ExampleS2D_efficiency() {
	const (
		nbins  = 20
		ptmax  = 100.0
		nevts  = 40 // events per bin
		turnOn = 30.0
		width  = 8.0
	)

	var (
		rnd   = rand.New(rand.NewPCG(1234, 5678))
		total = hbook.NewH1D(nbins, 0, ptmax)
		pass  = hbook.NewH1D(nbins, 0, ptmax)
	)

	for i := range nbins {
		pt := (float64(i) + 0.5) * ptmax / nbins
		// a trigger that turns on around 30 GeV and plateaus at 95%
		p := 0.95 / (1 + math.Exp(-(pt-turnOn)/width))
		for range nevts {
			total.Fill(pt, 1)
			if rnd.Float64() < p {
				pass.Fill(pt, 1)
			}
		}
	}

	eff, err := hbook.NewEfficiency(pass, total)
	if err != nil {
		log.Fatalf("error: %+v", err)
	}

	p := hplot.New()
	p.Title.Text = "Trigger efficiency"
	p.X.Label.Text = "pT [GeV]"
	p.Y.Label.Text = "efficiency"
	p.Y.Min = 0
	p.Y.Max = 1.1

	s := hplot.NewS2D(eff, hplot.WithXErrBars(true), hplot.WithYErrBars(true))
	s.GlyphStyle.Color = color.RGBA{R: 200, A: 255}
	s.GlyphStyle.Radius = vg.Points(2)
	p.Add(s, hplot.HLine(1, nil, nil))

	err = p.Save(15*vg.Centimeter, -1, "testdata/efficiency.png")
	if err != nil {
		log.Fatalf("error: %+v", err)
	}
}
