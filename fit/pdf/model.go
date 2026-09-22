// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"fmt"
	"math"
	"math/rand/v2"

	"go-hep.org/x/hep/fit/minuit"
)

// --- products ---

// Product is several densities multiplied together over the same observable.
type Product struct {
	pdfs []PDF
}

// Mul returns the product of several densities, as RooProdPdf is.
//
// The usual reason for one is a shape multiplied by an efficiency: the shape
// says what would have been there and the efficiency what was seen of it.
//
// The product is normalised as a whole, so the components need not be.
func Mul(pdfs ...PDF) *Product {
	if len(pdfs) == 0 {
		panic("pdf: a product needs at least one density")
	}
	return &Product{pdfs: pdfs}
}

func (p *Product) Name() string { return "product" }

func (p *Product) ParNames() []string {
	var o []string
	for _, f := range p.pdfs {
		o = append(o, f.ParNames()...)
	}
	return o
}

func (p *Product) NPar() int {
	n := 0
	for _, f := range p.pdfs {
		n += f.NPar()
	}
	return n
}

// split hands each component its own parameters.
func (p *Product) split(par []float64) [][]float64 {
	o := make([][]float64, len(p.pdfs))
	at := 0
	for i, f := range p.pdfs {
		o[i] = par[at : at+f.NPar()]
		at += f.NPar()
	}
	return o
}

func (p *Product) Shape(x float64, par []float64) float64 {
	var (
		ps = p.split(par)
		o  = 1.0
	)
	for i, f := range p.pdfs {
		o *= f.Shape(x, ps[i])
	}
	return o
}

// Integral is by quadrature: the integral of a product is not the product of
// the integrals, and nothing here knows any more than that.
func (p *Product) Integral(lo, hi float64, par []float64) float64 {
	return simpson(func(x float64) float64 { return p.Shape(x, par) }, lo, hi)
}

// --- constraints ---

// Constrain adds a gaussian penalty on a parameter to a likelihood, which is
// how a measurement made elsewhere is folded into this one.
//
// The penalty is -ln of a gaussian, so it costs half a unit when the
// parameter is one sigma from where it is being held. That is the same half
// unit the error definition uses, which is what makes the uncertainty that
// comes out include the constraint rather than ignore it.
func Constrain(fcn minuit.FCN, i int, mean, sigma float64) minuit.FCN {
	if sigma <= 0 {
		panic(fmt.Errorf("pdf: a constraint needs a positive width, got %v", sigma))
	}

	return func(npar int, grad []float64, par []float64, iflag int) float64 {
		v := fcn(npar, grad, par, iflag)
		if i < 0 || i >= len(par) {
			return v
		}
		d := (par[i] - mean) / sigma
		return v + 0.5*d*d
	}
}

// --- toys ---

// Generate draws n values from a density over [lo, hi].
//
// It is accept-reject: the density is sampled onto a grid to find how high it
// gets, and candidates are drawn until enough of them survive. That costs
// more calls the spikier the density is, and for a density with a very narrow
// peak in a very wide range it will be slow.
//
// Generate returns an error for a density that is zero or negative
// everywhere it was looked at, there being nothing to draw from.
func Generate(rnd *rand.Rand, p PDF, lo, hi float64, par []float64, n int) ([]float64, error) {
	if hi <= lo {
		return nil, fmt.Errorf("pdf: cannot generate over [%v, %v]", lo, hi)
	}
	if n < 0 {
		return nil, fmt.Errorf("pdf: cannot generate %d values", n)
	}

	// What to sample. A sum's coefficients multiply components that have
	// been normalised first, so sampling its raw Shape would draw the wrong
	// mixture: a component with a large integral would be over-represented
	// by exactly that integral.
	shape := func(x float64) float64 { return p.Shape(x, par) }
	if sum, ok := p.(*Sum); ok {
		shape = func(x float64) float64 { return sum.evalAt(x, lo, hi, par) }
	}

	// how high the density gets, from a scan fine enough to find a peak
	// that a fit would care about.
	const scan = 10000
	var (
		peak float64
		dx   = (hi - lo) / scan
	)
	for i := range scan + 1 {
		v := shape(lo + float64(i)*dx)
		if v > peak {
			peak = v
		}
	}

	if peak <= 0 {
		return nil, fmt.Errorf(
			"pdf: %s is not positive anywhere in [%v, %v]: nothing to generate",
			p.Name(), lo, hi,
		)
	}

	// a little headroom, in case the grid stepped over the very top.
	peak *= 1.05

	o := make([]float64, 0, n)
	for len(o) < n {
		x := lo + rnd.Float64()*(hi-lo)
		if rnd.Float64()*peak < shape(x) {
			o = append(o, x)
		}
	}

	return o, nil
}

// --- simultaneous fits ---

// Channel is one dataset in a simultaneous fit, with the density it is
// described by and which of the fit's parameters that density uses.
type Channel struct {
	// Name is what the channel is called, for the error messages.
	Name string

	// Data is the channel's events, and Lo and Hi the range they are
	// fitted over.
	Data   []float64
	Lo, Hi float64

	// PDF describes them.
	PDF PDF

	// Pars says which of the fit's parameters feeds each of PDF's, in the
	// order PDF wants them. Two channels naming the same parameter is what
	// makes the fit simultaneous: that parameter is then measured by both.
	Pars []int
}

// FitSimultaneous fits several datasets at once, sharing parameters between
// them, as RooSimultaneous does.
//
// It is how a measurement is made in more than one place at a time: a signal
// shape shared across channels and a background left free in each, or the
// same quantity measured in data and in a control sample that pins down a
// nuisance.
//
// The likelihood is the sum of the channels', so a channel with more events
// in it has more to say, which is as it should be.
func FitSimultaneous(chans []Channel, pars []minuit.Par) (*Result, error) {
	if len(chans) == 0 {
		return nil, fmt.Errorf("pdf: no channels to fit")
	}

	for _, c := range chans {
		switch {
		case c.PDF == nil:
			return nil, fmt.Errorf("pdf: channel %q has no density", c.Name)
		case len(c.Pars) != c.PDF.NPar():
			return nil, fmt.Errorf(
				"pdf: channel %q maps %d parameters onto a density that takes %d",
				c.Name, len(c.Pars), c.PDF.NPar(),
			)
		case c.Hi <= c.Lo:
			return nil, fmt.Errorf(
				"pdf: channel %q has the range [%v, %v]", c.Name, c.Lo, c.Hi,
			)
		}
		for _, i := range c.Pars {
			if i < 0 || i >= len(pars) {
				return nil, fmt.Errorf(
					"pdf: channel %q reaches for parameter %d, and there are %d",
					c.Name, i, len(pars),
				)
			}
		}
	}

	// each channel's likelihood, ready to be handed its own parameters.
	nlls := make([]minuit.FCN, len(chans))
	for i, c := range chans {
		nlls[i] = NLL(c.Data, c.PDF, c.Lo, c.Hi)
	}

	fcn := minuit.FuncOf(func(par []float64) float64 {
		var sum float64
		for i, c := range chans {
			sub := make([]float64, len(c.Pars))
			for j, k := range c.Pars {
				sub[j] = par[k]
			}
			v := nlls[i](len(sub), nil, sub, minuit.IFlagVal)
			if math.IsInf(v, +1) || math.IsNaN(v) {
				return math.Inf(+1)
			}
			sum += v
		}
		return sum
	})

	res, err := fitFCN(fcn, nil, chans[0].Lo, chans[0].Hi, pars)
	if err != nil {
		return res, err
	}
	res.Channels = chans
	return res, nil
}

// --- profile likelihood ---

// ScanPoint is one point of a profile likelihood scan.
type ScanPoint struct {
	// Value is where the parameter was held.
	Value float64

	// NLL is the negative log-likelihood there, once every other parameter
	// had been fitted again around it.
	NLL float64

	// Delta is NLL less the NLL at the minimum, which is the quantity an
	// interval is read off: it crosses UP at one standard deviation.
	Delta float64
}

// Scan profiles the likelihood over one parameter: it holds the parameter at
// each of n values across [lo, hi], fits everything else again around it, and
// reports how much worse the likelihood got.
//
// This is what an interval or a limit is read off, and it says more than an
// uncertainty does: a parabola in Delta means the parabolic uncertainty was
// the whole story, and anything else means it was not.
//
// Scan is expensive. It is a whole fit per point.
func (r *Result) Scan(i int, lo, hi float64, n int) ([]ScanPoint, error) {
	switch {
	case r.fcn == nil:
		return nil, fmt.Errorf("pdf: this result cannot be scanned: it has no likelihood")
	case i < 0 || i >= len(r.pars):
		return nil, fmt.Errorf("pdf: no parameter %d to scan", i)
	case n < 2:
		return nil, fmt.Errorf("pdf: a scan needs at least two points, got %d", n)
	case hi <= lo:
		return nil, fmt.Errorf("pdf: cannot scan over [%v, %v]", lo, hi)
	}

	o := make([]ScanPoint, 0, n)
	step := (hi - lo) / float64(n-1)

	for k := range n {
		v := lo + float64(k)*step

		m := minuit.New(len(r.pars))
		m.SetPrintLevel(-1)
		m.SetFCN(r.fcn)
		if err := m.SetErrorDef(errorDef); err != nil {
			return nil, err
		}

		for j, p := range r.pars {
			var (
				val  = p.Value
				step = p.Step
			)
			// start the others from where the fit left them, which is
			// nearer than where they began and so cheaper to get back to.
			if j < len(r.Minuit.Values()) {
				val = r.Minuit.Values()[j]
			}
			if j == i {
				val, step = v, 0 // held
			}
			if step == 0 && j != i {
				step = 0.1 * math.Max(1, math.Abs(val))
			}

			err := m.Parameter(j, p.Name, val, step, p.Min, p.Max)
			if err != nil {
				return nil, fmt.Errorf("pdf: could not set parameter %d for the scan: %w", j, err)
			}
		}

		if err := m.Command("MIGRAD"); err != nil {
			return nil, fmt.Errorf("pdf: the scan failed at %v: %w", v, err)
		}

		o = append(o, ScanPoint{Value: v, NLL: m.FMin(), Delta: m.FMin() - r.NLL})
	}

	return o, nil
}

// Interval reads an interval off a scan: where Delta crosses the given rise,
// which is UP for one standard deviation.
//
// It returns the crossings either side of the minimum, by straight-line
// interpolation between the scan points, and says whether it found them. A
// scan that does not reach high enough on one side has no crossing there,
// which is the honest answer for a parameter the data only bounds on one
// side.
func Interval(scan []ScanPoint, rise float64) (lo, hi float64, ok bool) {
	if len(scan) < 2 {
		return 0, 0, false
	}

	// the best point in the scan.
	best := 0
	for i := range scan {
		if scan[i].Delta < scan[best].Delta {
			best = i
		}
	}

	cross := func(a, b ScanPoint) float64 {
		d := b.Delta - a.Delta
		if d == 0 {
			return a.Value
		}
		return a.Value + (rise-a.Delta)*(b.Value-a.Value)/d
	}

	var foundLo, foundHi bool
	for i := best; i > 0; i-- {
		if scan[i-1].Delta >= rise {
			lo = cross(scan[i], scan[i-1])
			foundLo = true
			break
		}
	}
	for i := best; i < len(scan)-1; i++ {
		if scan[i+1].Delta >= rise {
			hi = cross(scan[i], scan[i+1])
			foundHi = true
			break
		}
	}

	return lo, hi, foundLo && foundHi
}
