// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"fmt"
	"math"

	"go-hep.org/x/hep/fit/minuit"
	"go-hep.org/x/hep/hbook"
)

// Sample is one contribution to a binned model: a template, what multiplies
// it, and how it moves under the systematics.
type Sample struct {
	// Name is what the sample is called.
	Name string

	// Template is the sample's shape and normalisation. Its bins are what
	// is expected of it before anything is applied.
	Template *Morph

	// Alphas says which of the model's parameters feeds each of the
	// template's nuisance parameters, in order.
	Alphas []int

	// Norms are parameters that multiply the whole sample: a signal
	// strength, or a floating background normalisation. Each is applied as
	// it is, so one is the nominal.
	Norms []int

	// LogNorms are parameters that multiply the whole sample
	// exponentially, by kappa^alpha. They are how a normalisation
	// uncertainty of a few per cent is written: kappa is one plus that
	// fraction, and the parameter is measured in standard deviations.
	LogNorms []LogNorm
}

// LogNorm is a normalisation uncertainty applied as kappa^alpha.
type LogNorm struct {
	// Par is which of the model's parameters this uses.
	Par int

	// Kappa is the factor at one standard deviation up. A ten per cent
	// uncertainty is 1.1.
	Kappa float64
}

// Constraint holds a nuisance parameter to a measurement made elsewhere.
type Constraint struct {
	// Par is which of the model's parameters is constrained.
	Par int

	// Mean and Sigma are where it is held and how tightly. A systematic
	// measured in standard deviations is held at zero with a width of one,
	// which is what NewBinned gives every alpha unless told otherwise.
	Mean, Sigma float64
}

// Binned is a binned likelihood model with systematic uncertainties, in the
// manner of HistFactory.
//
// Each bin expects the sum of its samples, every sample scaled by whatever
// multiplies it and reshaped by the systematics that move it. The data are
// counts, so each bin is a Poisson, and each nuisance parameter carries a
// gaussian constraint saying what was known about it before this fit.
//
// That likelihood -- Poisson in the bins, gaussian on the nuisances -- is
// what nearly every limit and nearly every cross-section in collider physics
// is extracted from.
type Binned struct {
	samples []Sample
	cons    []Constraint
	npar    int
	nbins   int
	widths  []float64
	edges   []float64
}

// NewBinned returns a binned model.
//
// npar is how many parameters the model has altogether: the samples index
// into that one list, which is what lets two samples share a systematic.
//
// Every parameter a template uses as a nuisance is given a unit gaussian
// constraint unless Constrain says otherwise, since that is what "measured
// in standard deviations" means.
func NewBinned(npar int, samples []Sample, cons []Constraint) (*Binned, error) {
	if len(samples) == 0 {
		return nil, fmt.Errorf("pdf: a binned model needs at least one sample")
	}

	b := &Binned{samples: samples, npar: npar}

	for i, s := range samples {
		switch {
		case s.Template == nil:
			return nil, fmt.Errorf("pdf: sample %q has no template", s.Name)
		case len(s.Alphas) != s.Template.NPar():
			return nil, fmt.Errorf(
				"pdf: sample %q maps %d parameters onto a template with %d systematics",
				s.Name, len(s.Alphas), s.Template.NPar(),
			)
		}

		switch i {
		case 0:
			b.nbins = s.Template.NBins()
			b.widths = s.Template.widths
			b.edges = s.Template.edges
		default:
			if s.Template.NBins() != b.nbins {
				return nil, fmt.Errorf(
					"pdf: sample %q has %d bins, the first has %d",
					s.Name, s.Template.NBins(), b.nbins,
				)
			}
		}

		for _, list := range [][]int{s.Alphas, s.Norms} {
			for _, p := range list {
				if p < 0 || p >= npar {
					return nil, fmt.Errorf(
						"pdf: sample %q reaches for parameter %d, and there are %d",
						s.Name, p, npar,
					)
				}
			}
		}
		for _, ln := range s.LogNorms {
			if ln.Par < 0 || ln.Par >= npar {
				return nil, fmt.Errorf(
					"pdf: sample %q reaches for parameter %d, and there are %d",
					s.Name, ln.Par, npar,
				)
			}
			if ln.Kappa <= 0 {
				return nil, fmt.Errorf(
					"pdf: sample %q has a normalisation factor of %v, which has no logarithm",
					s.Name, ln.Kappa,
				)
			}
		}
	}

	// every systematic is constrained unless it was given one already.
	held := make(map[int]bool, len(cons))
	for _, c := range cons {
		if c.Par < 0 || c.Par >= npar {
			return nil, fmt.Errorf(
				"pdf: a constraint names parameter %d, and there are %d", c.Par, npar,
			)
		}
		if c.Sigma <= 0 {
			return nil, fmt.Errorf(
				"pdf: the constraint on parameter %d has a width of %v", c.Par, c.Sigma,
			)
		}
		held[c.Par] = true
	}

	b.cons = append(b.cons, cons...)
	for _, s := range samples {
		for _, p := range s.Alphas {
			if !held[p] {
				b.cons = append(b.cons, Constraint{Par: p, Mean: 0, Sigma: 1})
				held[p] = true
			}
		}
		for _, ln := range s.LogNorms {
			if !held[ln.Par] {
				b.cons = append(b.cons, Constraint{Par: ln.Par, Mean: 0, Sigma: 1})
				held[ln.Par] = true
			}
		}
	}

	return b, nil
}

// NBins returns how many bins the model has.
func (b *Binned) NBins() int { return b.nbins }

// NPar returns how many parameters it has.
func (b *Binned) NPar() int { return b.npar }

// Constraints returns the constraints on the nuisance parameters, the ones
// added for the systematics included.
func (b *Binned) Constraints() []Constraint {
	return append([]Constraint(nil), b.cons...)
}

// Expected returns how many events the model expects in a bin.
func (b *Binned) Expected(bin int, par []float64) float64 {
	var o float64
	for _, s := range b.samples {
		o += b.sampleIn(s, bin, par)
	}
	return o
}

// ExpectedOf returns how many events one sample expects in a bin, which is
// what a stacked plot of the fitted model is drawn from.
func (b *Binned) ExpectedOf(sample, bin int, par []float64) float64 {
	if sample < 0 || sample >= len(b.samples) {
		return 0
	}
	return b.sampleIn(b.samples[sample], bin, par)
}

func (b *Binned) sampleIn(s Sample, bin int, par []float64) float64 {
	// the template's own nuisance parameters, gathered from the model's.
	alphas := make([]float64, len(s.Alphas))
	for i, p := range s.Alphas {
		alphas[i] = par[p]
	}

	o := s.Template.Bin(bin, alphas)

	for _, p := range s.Norms {
		o *= par[p]
	}
	for _, ln := range s.LogNorms {
		o *= math.Pow(ln.Kappa, par[ln.Par])
	}

	if o < 0 {
		return 0
	}
	return o
}

// Samples returns the samples the model is made of.
func (b *Binned) Samples() []Sample { return append([]Sample(nil), b.samples...) }

// NLL returns the negative log-likelihood of the model against observed
// counts: a Poisson in each bin, and a gaussian on each nuisance parameter.
//
// The counts are taken from the histogram bin by bin, so it must be binned
// the way the templates are.
func (b *Binned) NLL(data *hbook.H1D) (minuit.FCN, error) {
	bins := data.Binning.Bins
	if len(bins) != b.nbins {
		return nil, fmt.Errorf(
			"pdf: the data has %d bins and the model has %d", len(bins), b.nbins,
		)
	}

	counts := make([]float64, len(bins))
	for i := range bins {
		counts[i] = bins[i].SumW()
	}

	return minuit.FuncOf(func(par []float64) float64 {
		var nll float64

		for i := range counts {
			mu := b.Expected(i, par)
			switch {
			case mu > 0:
				nll += mu - counts[i]*math.Log(mu)
			case counts[i] > 0:
				// the model expects nothing where something was seen.
				return math.Inf(+1)
			}
		}

		// what was known about the nuisances before this fit.
		for _, c := range b.cons {
			d := (par[c.Par] - c.Mean) / c.Sigma
			nll += 0.5 * d * d
		}

		if math.IsNaN(nll) {
			return math.Inf(+1)
		}
		return nll
	}), nil
}

// FitBinnedModel fits a binned model to observed counts.
func FitBinnedModel(b *Binned, data *hbook.H1D, pars []minuit.Par, opts ...FitOption) (*Result, error) {
	if len(pars) != b.npar {
		return nil, fmt.Errorf(
			"pdf: the model has %d parameters, got %d", b.npar, len(pars),
		)
	}

	nll, err := b.NLL(data)
	if err != nil {
		return nil, err
	}

	return fitFCN(nll, nil, 0, 0, pars, opts...)
}
