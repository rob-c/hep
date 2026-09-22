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

// Study runs a fit against many generated datasets and looks at what comes
// back, as RooMCStudy does.
//
// It is how a fit is checked before it is believed. Generate data from known
// parameters, fit it, and look at the pull of each parameter:
//
//	pull = (fitted - true) / uncertainty
//
// Over many toys the pull should be a unit gaussian. A mean away from zero
// says the fit is biased; a width away from one says its uncertainties are
// wrong, too small if the width is above one and too large if below. Neither
// shows up in a single fit, which is why this exists.
type Study struct {
	// PDF is what generates the toys and what is fitted back to them.
	PDF PDF

	// True are the parameters the toys are generated from.
	True []float64

	// Start are the parameters each fit starts from, which should not be
	// the true ones: a fit that only works when started at the answer is
	// not working.
	Start []minuit.Par

	// N is how many events each toy holds. For an extended fit it is
	// ignored, the yields saying how many there are.
	N int

	// Lo and Hi are the range.
	Lo, Hi float64
}

// StudyResult is what a study found.
type StudyResult struct {
	// Fits is how many toys were fitted, and Failed how many would not
	// converge. A study with many failures is itself the finding.
	Fits   int
	Failed int

	// Values, Errors and Pulls hold one slice per parameter, each with one
	// entry per toy that converged.
	Values [][]float64
	Errors [][]float64
	Pulls  [][]float64
}

// PullMean returns the mean pull of a parameter, which should be zero.
func (r *StudyResult) PullMean(i int) float64 { return mean(r.Pulls[i]) }

// PullWidth returns the width of the pull of a parameter, which should be
// one.
func (r *StudyResult) PullWidth(i int) float64 { return stdDev(r.Pulls[i]) }

// Bias returns the mean of the fitted values of a parameter less the true
// one, in the units the parameter is in.
func (r *StudyResult) Bias(i int, truth float64) float64 {
	return mean(r.Values[i]) - truth
}

// Run generates and fits n toys.
//
// A toy that will not converge is counted and skipped rather than stopping
// the study: some fraction of them failing is ordinary, and how large that
// fraction is happens to be one of the things worth knowing.
func (s *Study) Run(rnd *rand.Rand, n int) (*StudyResult, error) {
	switch {
	case s.PDF == nil:
		return nil, fmt.Errorf("pdf: a study needs a density")
	case len(s.True) != s.PDF.NPar():
		return nil, fmt.Errorf(
			"pdf: %s takes %d parameters and the study gives %d true ones",
			s.PDF.Name(), s.PDF.NPar(), len(s.True),
		)
	case len(s.Start) != s.PDF.NPar():
		return nil, fmt.Errorf(
			"pdf: %s takes %d parameters and the study gives %d starting ones",
			s.PDF.Name(), s.PDF.NPar(), len(s.Start),
		)
	case s.Hi <= s.Lo:
		return nil, fmt.Errorf("pdf: the study has the range [%v, %v]", s.Lo, s.Hi)
	case n < 1:
		return nil, fmt.Errorf("pdf: cannot run %d toys", n)
	}

	npar := s.PDF.NPar()
	out := &StudyResult{
		Values: make([][]float64, npar),
		Errors: make([][]float64, npar),
		Pulls:  make([][]float64, npar),
	}

	// how many events a toy holds: the yields, if this is an extended sum.
	size := s.N
	if sum, ok := s.PDF.(*Sum); ok && sum.Extended() {
		size = int(math.Round(sum.Yield(s.True)))
	}
	if size < 1 {
		return nil, fmt.Errorf("pdf: a toy of %d events has nothing to fit", size)
	}

	for range n {
		data, err := Generate(rnd, s.PDF, s.Lo, s.Hi, s.True, size)
		if err != nil {
			return nil, fmt.Errorf("pdf: could not generate a toy: %w", err)
		}

		res, err := FitUnbinned(data, s.PDF, s.Lo, s.Hi, s.Start)
		if err != nil {
			out.Failed++
			continue
		}

		out.Fits++
		for i := range npar {
			v, e := res.Value(i)
			out.Values[i] = append(out.Values[i], v)
			out.Errors[i] = append(out.Errors[i], e)
			if e > 0 {
				out.Pulls[i] = append(out.Pulls[i], (v-s.True[i])/e)
			}
		}
	}

	if out.Fits == 0 {
		return out, fmt.Errorf("pdf: not one of the %d toys converged", n)
	}

	return out, nil
}

func mean(vs []float64) float64 {
	if len(vs) == 0 {
		return math.NaN()
	}
	var o float64
	for _, v := range vs {
		o += v
	}
	return o / float64(len(vs))
}

func stdDev(vs []float64) float64 {
	if len(vs) < 2 {
		return math.NaN()
	}
	var (
		m   = mean(vs)
		sum float64
	)
	for _, v := range vs {
		d := v - m
		sum += d * d
	}
	return math.Sqrt(sum / float64(len(vs)-1))
}
