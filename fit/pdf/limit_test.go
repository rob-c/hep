// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf_test

import (
	"math"
	"testing"

	"go-hep.org/x/hep/fit/minuit"
	"go-hep.org/x/hep/fit/pdf"
	"go-hep.org/x/hep/hbook"
)

// TestCLsProperties checks the ratio behaves the way the method requires.
func TestCLsProperties(t *testing.T) {
	// with no discrimination at all nothing is excluded.
	if got := pdf.CLs(0, 0); math.Abs(got-1) > 1e-12 {
		t.Errorf("CLs(0,0): got=%v, want=1", got)
	}

	// CLs is never above one: it is a confidence level.
	for _, q := range []float64{0, 0.5, 1, 4, 9, 25} {
		for _, qa := range []float64{0, 0.5, 1, 4, 9, 25} {
			if got := pdf.CLs(q, qa); got < 0 || got > 1 {
				t.Errorf("CLs(%v,%v)=%v, want it in [0,1]", q, qa, got)
			}
		}
	}

	// a larger statistic excludes harder, all else equal.
	prev := 2.0
	for _, q := range []float64{0, 1, 4, 9, 16} {
		got := pdf.CLs(q, 4)
		if got > prev+1e-12 {
			t.Errorf("CLs rose from %v to %v as q went to %v", prev, got, q)
		}
		prev = got
	}

	// when the data land exactly on the Asimov expectation, CLs is twice
	// the plain p-value, which is the standard relation.
	for _, q := range []float64{1, 4, 9} {
		var (
			got  = pdf.CLs(q, q)
			want = 2 * (1 - 0.5*math.Erfc(-math.Sqrt(q)/math.Sqrt2))
		)
		if math.Abs(got-want) > 1e-12 {
			t.Errorf("CLs(%v,%v): got=%v, want=%v", q, q, got, want)
		}
	}
}

// TestTestStat checks the statistic is one-sided: a best fit above the value
// being tested is not evidence against it.
func TestTestStat(t *testing.T) {
	// best fit below the tested value: the statistic is the usual one.
	if got, want := pdf.TestStat(10, 8, 2.0, 1.0), 4.0; math.Abs(got-want) > 1e-12 {
		t.Errorf("got=%v, want=%v", got, want)
	}
	// best fit above it: nothing.
	if got := pdf.TestStat(10, 8, 1.0, 2.0); got != 0 {
		t.Errorf("got=%v, want=0", got)
	}
	// a negative difference cannot happen but must not propagate.
	if got := pdf.TestStat(8, 10, 2.0, 1.0); got != 0 {
		t.Errorf("got=%v, want=0", got)
	}
}

// TestUpperLimit sets a limit on a signal strength and checks the answer
// against the thing it must satisfy: fitting the Asimov dataset itself should
// give an observed limit equal to the median expected one, since that is what
// the Asimov dataset is for.
func TestUpperLimit(t *testing.T) {
	// a counting model: signal peaks in the middle, background falls.
	var (
		sigNom = tmpl(1, 5, 15, 25, 25, 15, 5, 1)
		bkgNom = tmpl(400, 340, 290, 246, 210, 178, 152, 128)
	)

	sig, err := pdf.NewMorph(sigNom, nil, nil, pdf.InterpPolyExp)
	if err != nil {
		t.Fatal(err)
	}
	bkg, err := pdf.NewMorph(bkgNom, nil, nil, pdf.InterpPolyExp)
	if err != nil {
		t.Fatal(err)
	}

	model, err := pdf.NewBinned(1, []pdf.Sample{
		{Name: "signal", Template: sig, Norms: []int{0}},
		{Name: "background", Template: bkg},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// The Asimov dataset for background only: every bin holds exactly what
	// the background expects. It is the data too, here, so the observed
	// limit ought to come out at the median expected one.
	asimov := hbook.NewH1D(8, 0, 8)
	for i := range 8 {
		asimov.Fill(float64(i)+0.5, model.Expected(i, []float64{0}))
	}

	pars := []minuit.Par{{Name: "mu", Value: 0.5, Step: 0.2, Min: 0, Max: 20}}

	res, err := pdf.FitBinnedModel(model, asimov, pars)
	if err != nil {
		t.Fatalf("could not fit: %+v", err)
	}

	// the best fit to background-only data should be no signal.
	if mu := res.Values()[0]; math.Abs(mu) > 0.05 {
		t.Errorf("the best fit to background-only data is mu=%v, want 0", mu)
	}

	nll, err := model.NLL(asimov)
	if err != nil {
		t.Fatal(err)
	}

	lim, err := pdf.UpperLimit(res, 0, 0.01, 8, 40, 0.95, nll)
	if err != nil {
		t.Fatalf("could not set a limit: %+v", err)
	}

	if !lim.Found {
		t.Fatalf("no limit found in the scan")
	}
	if lim.Observed <= 0 {
		t.Fatalf("the limit is %v", lim.Observed)
	}

	// the thing that has to hold: on Asimov data the observed limit is the
	// median expected one.
	if math.Abs(lim.Observed-lim.Expected)/lim.Expected > 0.05 {
		t.Errorf(
			"on the Asimov dataset the observed limit is %v and the expected %v; they should agree",
			lim.Observed, lim.Expected,
		)
	}

	// and the bands are in order and straddle the median.
	for _, tc := range []struct {
		name   string
		lo, hi float64
	}{
		{"two sigma", lim.Band2[0], lim.Band2[1]},
		{"one sigma", lim.Band1[0], lim.Band1[1]},
	} {
		if !(tc.lo < lim.Expected && lim.Expected < tc.hi) {
			t.Errorf("the %s band [%v, %v] does not straddle the median %v",
				tc.name, tc.lo, tc.hi, lim.Expected)
		}
	}
	if !(lim.Band2[0] < lim.Band1[0] && lim.Band1[1] < lim.Band2[1]) {
		t.Errorf("the two-sigma band %v is not outside the one-sigma band %v",
			lim.Band2, lim.Band1)
	}
}

// TestUpperLimitNotFound checks a scan that never reaches the confidence
// level says so, rather than returning the end of the range as though it
// meant something.
func TestUpperLimitNotFound(t *testing.T) {
	var (
		sigNom = tmpl(1, 1, 1, 1)
		bkgNom = tmpl(1000, 1000, 1000, 1000)
	)

	sig, err := pdf.NewMorph(sigNom, nil, nil, pdf.InterpPolyExp)
	if err != nil {
		t.Fatal(err)
	}
	bkg, err := pdf.NewMorph(bkgNom, nil, nil, pdf.InterpPolyExp)
	if err != nil {
		t.Fatal(err)
	}

	model, err := pdf.NewBinned(1, []pdf.Sample{
		{Name: "signal", Template: sig, Norms: []int{0}},
		{Name: "background", Template: bkg},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	data := hbook.NewH1D(4, 0, 4)
	for i := range 4 {
		data.Fill(float64(i)+0.5, model.Expected(i, []float64{0}))
	}

	res, err := pdf.FitBinnedModel(model, data, []minuit.Par{
		{Name: "mu", Value: 0.5, Step: 0.2, Min: 0, Max: 100},
	})
	if err != nil {
		t.Fatal(err)
	}

	// a signal a thousandth the size of the background, scanned only over a
	// tiny range: nothing here excludes anything.
	lim, err := pdf.UpperLimit(res, 0, 0, 0.001, 10, 0.95, nil)
	if err != nil {
		t.Fatalf("could not scan: %+v", err)
	}
	if lim.Found {
		t.Errorf("a limit of %v was reported where the scan never reached 95%%", lim.Observed)
	}
}

func TestUpperLimitErrors(t *testing.T) {
	m, err := pdf.NewMorph(tmpl(10, 10), nil, nil, pdf.InterpExp)
	if err != nil {
		t.Fatal(err)
	}
	model, err := pdf.NewBinned(1, []pdf.Sample{{Name: "s", Template: m, Norms: []int{0}}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	data := hbook.NewH1D(2, 0, 2)
	data.Fill(0.5, 10)
	data.Fill(1.5, 10)

	res, err := pdf.FitBinnedModel(model, data, []minuit.Par{
		{Name: "mu", Value: 1, Step: 0.2, Min: 0, Max: 10},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name   string
		par    int
		lo, hi float64
		n      int
		level  float64
	}{
		{"no such parameter", 9, 0, 1, 10, 0.95},
		{"too few points", 0, 0, 1, 1, 0.95},
		{"inverted range", 0, 1, 0, 10, 0.95},
		{"bad level", 0, 0, 1, 10, 1.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pdf.UpperLimit(res, tc.par, tc.lo, tc.hi, tc.n, tc.level, nil); err == nil {
				t.Fatal("expected an error")
			}
		})
	}

	t.Run("no likelihood", func(t *testing.T) {
		if _, err := pdf.UpperLimit(nil, 0, 0, 1, 10, 0.95, nil); err == nil {
			t.Fatal("expected an error")
		}
	})
}

// TestDiscovery checks the significance of a signal, and that a signal
// fitting below zero gives none rather than a negative one.
func TestDiscovery(t *testing.T) {
	m, err := pdf.NewMorph(tmpl(10, 10), nil, nil, pdf.InterpExp)
	if err != nil {
		t.Fatal(err)
	}
	model, err := pdf.NewBinned(1, []pdf.Sample{{Name: "s", Template: m, Norms: []int{0}}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	data := hbook.NewH1D(2, 0, 2)
	data.Fill(0.5, 20)
	data.Fill(1.5, 20)

	res, err := pdf.FitBinnedModel(model, data, []minuit.Par{
		{Name: "mu", Value: 1, Step: 0.2, Min: 0, Max: 10},
	})
	if err != nil {
		t.Fatal(err)
	}

	// half a unit of likelihood is one sigma, two units are two.
	got, err := pdf.Discovery(res, 0, res.NLL+2)
	if err != nil {
		t.Fatalf("could not take the significance: %+v", err)
	}
	if want := 2.0; math.Abs(got-want) > 1e-9 {
		t.Errorf("significance: got=%v, want=%v", got, want)
	}

	if _, err := pdf.Discovery(res, 9, res.NLL+2); err == nil {
		t.Error("a parameter that is not there was accepted")
	}
	if _, err := pdf.Discovery(nil, 0, 0); err == nil {
		t.Error("a missing fit was accepted")
	}
}
