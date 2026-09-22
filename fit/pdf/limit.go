// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"fmt"
	"math"

	"go-hep.org/x/hep/fit/minuit"
)

// The asymptotic formulae for a profile-likelihood test statistic, from
// Cowan, Cranmer, Gross and Vitells, "Asymptotic formulae for likelihood-
// based tests of new physics", Eur. Phys. J. C71 (2011) 1554.
//
// The idea is that the profile likelihood ratio has a known distribution
// once there is enough data, so a limit can be read straight off one fit
// instead of out of a few million toys. That is what makes limit setting
// something you can do while you wait.

// TestStat is the profile likelihood ratio for testing a value of the
// parameter of interest, in the form used for an upper limit:
//
//	q = -2 ln( L(mu, best nuisances) / L(best mu, best nuisances) )
//
// held at zero when the best fit is above mu, since a signal larger than the
// one being tested is not evidence against it.
func TestStat(nllAtMu, nllBest, mu, muBest float64) float64 {
	if muBest > mu {
		return 0
	}
	q := 2 * (nllAtMu - nllBest)
	if q < 0 {
		return 0
	}
	return q
}

// normCDF is the cumulative of the standard normal.
func normCDF(x float64) float64 {
	return 0.5 * math.Erfc(-x/math.Sqrt2)
}

// pValue returns the probability of a test statistic at least this large,
// which for the asymptotic distribution is one less the normal cumulative of
// its square root.
func pValue(q float64) float64 {
	if q <= 0 {
		return 1
	}
	return 1 - normCDF(math.Sqrt(q))
}

// CLs returns the confidence level of the signal-plus-background hypothesis
// divided by that of the background-only one:
//
//	CLs = p(s+b) / (1 - p(b))
//
// which asymptotically is
//
//	CLs = ( 1 - Phi(sqrt(q)) ) / Phi( sqrt(q) - sqrt(qA) )
//
// with q the test statistic the data give and qA the one the Asimov dataset
// gives -- the dataset whose every bin holds exactly what the background
// expects, and which stands in for the median of the toys.
//
// The ratio is what keeps a limit from excluding a signal the experiment had
// no sensitivity to. Where the data fluctuate low, p(s+b) alone would exclude
// arbitrarily small signals; dividing by 1 - p(b), which is also small there,
// holds it back.
func CLs(q, qA float64) float64 {
	var (
		s = math.Sqrt(math.Max(0, q))
		a = math.Sqrt(math.Max(0, qA))

		clsb = 1 - normCDF(s)
		clb  = normCDF(s - a)
	)
	if clb <= 0 {
		return 1
	}
	return math.Min(1, clsb/clb)
}

// Limit is an upper limit on a parameter.
type Limit struct {
	// Observed is the limit the data give.
	Observed float64

	// Expected is the median limit the background-only hypothesis would
	// give, and Band1 and Band2 the one- and two-sigma spreads around it:
	// the green and yellow of every limit plot.
	Expected float64
	Band1    [2]float64
	Band2    [2]float64

	// Found says whether the scan reached the confidence level asked for.
	// A scan that never gets there has no limit in it, and saying so is
	// better than returning the end of the range as though it meant
	// something.
	Found bool
}

// UpperLimit finds the value of a parameter excluded at the given confidence
// level, by the asymptotic CLs method.
//
// par is which parameter is being limited -- the signal strength, usually --
// and lo and hi the range to look in, scanned at n points. The confidence
// level is the usual 0.95 for a 95% limit.
//
// The expected limit and its bands come from the Asimov dataset: the one
// whose every bin holds exactly what the background-only hypothesis expects,
// which stands in for the median of the toys that would otherwise have to be
// thrown.
//
// asimov is the likelihood of that dataset. Pass nil and only the observed
// limit is computed, the expected band being left at zero.
func UpperLimit(res *Result, par int, lo, hi float64, n int, level float64, asimov minuit.FCN) (*Limit, error) {
	switch {
	case res == nil || res.fcn == nil:
		return nil, fmt.Errorf("pdf: no likelihood to set a limit from")
	case par < 0 || par >= len(res.pars):
		return nil, fmt.Errorf("pdf: no parameter %d to limit", par)
	case n < 2:
		return nil, fmt.Errorf("pdf: a limit scan needs at least two points, got %d", n)
	case hi <= lo:
		return nil, fmt.Errorf("pdf: cannot scan over [%v, %v]", lo, hi)
	case level <= 0 || level >= 1:
		return nil, fmt.Errorf("pdf: the confidence level must be between 0 and 1, got %v", level)
	}

	obs, err := res.Scan(par, lo, hi, n)
	if err != nil {
		return nil, fmt.Errorf("pdf: could not scan for the limit: %w", err)
	}

	muBest := res.Values()[par]

	// the expected limit needs the same scan of the Asimov likelihood.
	var exp []ScanPoint
	if asimov != nil {
		a := &Result{Minuit: res.Minuit, fcn: asimov, pars: res.pars}

		// the Asimov minimum, which the scan measures from.
		best, err := fitFCN(asimov, nil, 0, 0, res.pars)
		if err != nil {
			return nil, fmt.Errorf("pdf: could not fit the Asimov dataset: %w", err)
		}
		a.Minuit = best.Minuit
		a.NLL = best.NLL

		exp, err = a.Scan(par, lo, hi, n)
		if err != nil {
			return nil, fmt.Errorf("pdf: could not scan the Asimov dataset: %w", err)
		}
	}

	out := &Limit{}

	// CLs against mu, and where it crosses 1 - level.
	want := 1 - level
	cls := make([]float64, len(obs))
	for i, p := range obs {
		q := TestStat(p.NLL, res.NLL, p.Value, muBest)

		// the same statistic for the Asimov dataset, which sets the scale
		// of what a fluctuation looks like. Its scan is already measured
		// from its own minimum, so twice Delta is the statistic.
		qa := 0.0
		if exp != nil {
			qa = math.Max(0, 2*exp[i].Delta)
		}

		cls[i] = CLs(q, qa)
	}

	out.Observed, out.Found = crossing(obs, cls, want)

	if exp != nil {
		// The expected limit and its bands: the Asimov statistic gives the
		// median, and moving the test by N sigma gives the band edges.
		for _, tc := range []struct {
			nsig float64
			dst  *float64
		}{
			{0, &out.Expected},
			{-1, &out.Band1[0]},
			{+1, &out.Band1[1]},
			{-2, &out.Band2[0]},
			{+2, &out.Band2[1]},
		} {
			band := make([]float64, len(obs))
			for i := range obs {
				qa := math.Max(0, 2*exp[i].Delta)
				band[i] = clsBand(qa, tc.nsig)
			}
			v, _ := crossing(obs, band, want)
			*tc.dst = v
		}
	}

	return out, nil
}

// clsBand returns CLs when the data have fluctuated nsig standard deviations
// from what the background alone would give, which is how the green and
// yellow bands around an expected limit are drawn.
//
// Under background only, sqrt(q) is normal about sqrt(qA) with unit width. A
// fluctuation towards more signal makes the data less able to exclude, so it
// lowers sqrt(q): the band at +N sigma is sqrt(q) = sqrt(qA) - N. Putting
// that into the CLs above, the denominator collapses to Phi(-N), which is
// why the bands need no scan of their own.
func clsBand(qa, nsig float64) float64 {
	if qa <= 0 {
		return 1
	}

	var (
		s    = math.Sqrt(qa) - nsig
		clsb = 1 - normCDF(s)
		clb  = normCDF(-nsig)
	)
	if clb <= 0 {
		return 1
	}
	return math.Min(1, clsb/clb)
}

// crossing finds where a falling curve passes through want, by straight-line
// interpolation between the points either side of it.
func crossing(pts []ScanPoint, vals []float64, want float64) (float64, bool) {
	for i := range len(vals) - 1 {
		var (
			a = vals[i]
			b = vals[i+1]
		)
		if (a-want)*(b-want) > 0 {
			continue
		}
		if a == b {
			return pts[i].Value, true
		}
		t := (want - a) / (b - a)
		return pts[i].Value + t*(pts[i+1].Value-pts[i].Value), true
	}
	return 0, false
}

// Discovery returns the significance with which the background-only
// hypothesis is rejected, in standard deviations.
//
// nllNull is the likelihood with the signal held at zero and everything else
// fitted around it, and res the free fit. A signal that fits below zero gives
// no significance rather than a negative one.
func Discovery(res *Result, par int, nllNull float64) (float64, error) {
	if res == nil {
		return 0, fmt.Errorf("pdf: no fit to take a significance from")
	}
	if par < 0 || par >= len(res.pars) {
		return 0, fmt.Errorf("pdf: no parameter %d", par)
	}

	if res.Values()[par] <= 0 {
		return 0, nil
	}
	return Significance(nllNull, res.NLL), nil
}
