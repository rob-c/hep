// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package minuit_test

import (
	"image/color"
	"math"
	"math/rand/v2"
	"path/filepath"
	"testing"

	"go-hep.org/x/hep/fit/minuit"
	"go-hep.org/x/hep/groot"
	"go-hep.org/x/hep/groot/rtree"
	"go-hep.org/x/hep/hbook"
	"go-hep.org/x/hep/hbook/ntup/ntroot"
	"go-hep.org/x/hep/hplot"
	"gonum.org/v1/plot/vg"
)

// TestWorkflow does in Go what a ROOT macro does in C++: write a TTree, read
// a variable out of it into a histogram, fit a function to the histogram and
// draw the two together.
//
//	TFile *f = TFile::Open("data.root");
//	TTree *t = (TTree*)f->Get("tree");
//	TH1D *h = new TH1D("h", "h", 60, -3, 7);
//	t->Draw("x >> h");
//	h->Fit("gaus");
//	c->SaveAs("fit.png");
func TestWorkflow(t *testing.T) {
	const (
		mean  = 2.0
		sigma = 0.75
		nevts = 20000
	)

	tmp := t.TempDir()
	fname := filepath.Join(tmp, "data.root")

	// --- write a TTree, so the fit has something of its own to read. ---
	func() {
		f, err := groot.Create(fname)
		if err != nil {
			t.Fatalf("could not create ROOT file: %+v", err)
		}
		defer f.Close()

		var x float64
		tree, err := rtree.NewWriter(f, "tree", []rtree.WriteVar{{Name: "x", Value: &x}})
		if err != nil {
			t.Fatalf("could not create tree writer: %+v", err)
		}

		rnd := rand.New(rand.NewPCG(1234, 5678))
		for range nevts {
			x = mean + sigma*rnd.NormFloat64()
			if _, err := tree.Write(); err != nil {
				t.Fatalf("could not write an entry: %+v", err)
			}
		}
		if err := tree.Close(); err != nil {
			t.Fatalf("could not close tree writer: %+v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("could not close ROOT file: %+v", err)
		}
	}()

	// --- read the branch into a histogram: TTree::Draw("x >> h"). ---
	nt, err := ntroot.Open(fname, "tree")
	if err != nil {
		t.Fatalf("could not open the tree as an ntuple: %+v", err)
	}

	h := hbook.NewH1D(60, -3, 7)
	h, err = nt.ScanH1D("x", h)
	if err != nil {
		t.Fatalf("could not fill the histogram from the tree: %+v", err)
	}

	if got, want := h.Entries(), int64(nevts); got != want {
		t.Fatalf("entries: got=%d, want=%d", got, want)
	}

	// --- fit a gaussian to it: h->Fit("gaus"). ---
	fit, err := minuit.FitH1D(h, minuit.Gaussian, []minuit.Par{
		{Name: "height", Value: 1000, Step: 100},
		{Name: "mean", Value: 0, Step: 0.5},
		{Name: "sigma", Value: 1, Step: 0.2, Min: 1e-3, Max: 10},
	})
	if err != nil {
		t.Fatalf("could not fit the histogram: %+v", err)
	}

	var (
		height, heightErr, _ = fit.Value(0)
		gotMean, meanErr, _  = fit.Value(1)
		gotSigma, sigErr, _  = fit.Value(2)
	)

	if math.Abs(gotMean-mean) > 5*meanErr {
		t.Errorf("mean: got=%v +/- %v, want=%v", gotMean, meanErr, mean)
	}
	if math.Abs(gotSigma-sigma) > 5*sigErr {
		t.Errorf("sigma: got=%v +/- %v, want=%v", gotSigma, sigErr, sigma)
	}
	if height <= 0 {
		t.Errorf("height: got=%v +/- %v, want it positive", height, heightErr)
	}

	// the fit should describe the data: a chi-square per degree of freedom
	// far from one would mean it does not.
	xs, _, _ := h1dFilled(h)
	if chi2ndf := fit.Chi2NDF(len(xs)); chi2ndf > 2 {
		t.Errorf("chi2/ndf: got=%v, want it near 1", chi2ndf)
	}

	// --- and the asymmetric uncertainties, which HESSE cannot give. ---
	if err := fit.Command("MINOS"); err != nil {
		t.Fatalf("could not run MINOS: %+v", err)
	}
	eplus, eminus, eparab, _ := fit.Errors(1)
	if eplus <= 0 || eminus >= 0 {
		t.Errorf("MINOS errors on the mean: got +%v %v, want them either side of zero", eplus, eminus)
	}
	// a gaussian fit is close to parabolic, so the three should agree.
	if math.Abs(eplus-eparab) > 0.2*eparab || math.Abs(-eminus-eparab) > 0.2*eparab {
		t.Errorf("MINOS and parabolic errors disagree: +%v %v vs %v", eplus, eminus, eparab)
	}

	// --- draw the histogram and the fitted curve: c->SaveAs(...). ---
	p := hplot.New()
	p.Title.Text = "a fit to a branch of a TTree"
	p.X.Label.Text = "x"
	p.Y.Label.Text = "entries"

	hp := hplot.NewH1D(h)
	hp.Infos.Style = hplot.HInfoSummary
	p.Add(hp)

	fun := hplot.NewFunction(fit.Func(minuit.Gaussian))
	fun.Color = color.RGBA{R: 255, A: 255}
	fun.Samples = 200
	p.Add(fun)
	p.Add(hplot.NewGrid())

	err = p.Save(16*vg.Centimeter, 12*vg.Centimeter, filepath.Join(tmp, "fit.png"))
	if err != nil {
		t.Fatalf("could not save the plot: %+v", err)
	}
}

// h1dFilled returns the bins of h that hold entries.
func h1dFilled(h *hbook.H1D) (xs, ys, errs []float64) {
	for i := range h.Binning.Bins {
		bin := &h.Binning.Bins[i]
		if bin.Entries() <= 0 {
			continue
		}
		xs = append(xs, bin.XMid())
		ys = append(ys, bin.SumW())
		errs = append(errs, math.Sqrt(bin.SumW2()))
	}
	return xs, ys, errs
}

// newGausHist fills a histogram from a gaussian without any randomness, so
// that a fit to it gives the same answer every time.
func newGausHist(mean, sigma float64) *hbook.H1D {
	h := hbook.NewH1D(60, -3, 7)
	for i := range h.Binning.Bins {
		bin := &h.Binning.Bins[i]
		x := bin.XMid()
		d := (x - mean) / sigma
		// the entries each bin would have collected from 20000 of them.
		// They go in one at a time rather than as a single weight, so that
		// the bin ends up with the sqrt(N) uncertainty of a real count
		// instead of the 100% one a single weighted fill would give it.
		n := int(math.Round(20000 * bin.XWidth() / (sigma * math.Sqrt(2*math.Pi)) * math.Exp(-0.5*d*d)))
		for range n {
			h.Fill(x, 1)
		}
	}
	return h
}

// TestGausPars checks the starting values taken off a histogram are close
// enough to the answer for a fit to begin from them.
func TestGausPars(t *testing.T) {
	h := newGausHist(2.0, 0.75)
	pars := minuit.GausPars(h)

	if got, want := len(pars), 3; got != want {
		t.Fatalf("got %d parameters, want %d", got, want)
	}
	if got := pars[1].Value; math.Abs(got-2.0) > 0.1 {
		t.Errorf("starting mean: got=%v, want~2", got)
	}
	if got := pars[2].Value; math.Abs(got-0.75) > 0.1 {
		t.Errorf("starting sigma: got=%v, want~0.75", got)
	}
	if pars[0].Value <= 0 {
		t.Errorf("starting height: got=%v, want it positive", pars[0].Value)
	}
	// the width is held positive and bounded at both ends, since a limited
	// parameter is varied through a sine.
	if pars[2].Min <= 0 || math.IsInf(pars[2].Max, 0) {
		t.Errorf("sigma limits: got [%v, %v], want two finite positive ones", pars[2].Min, pars[2].Max)
	}
}

// TestFitXY fits a straight line to points, the other way data arrives.
func TestFitXY(t *testing.T) {
	var (
		xs   = []float64{0, 1, 2, 3, 4, 5}
		errs = []float64{0.1, 0.1, 0.1, 0.1, 0.1, 0.1}
		ys   = make([]float64, len(xs))
	)
	for i, x := range xs {
		ys[i] = 3 + 2*x
	}

	fit, err := minuit.FitXY(xs, ys, errs, minuit.Polynomial(1), []minuit.Par{
		{Name: "a0", Value: 0, Step: 0.5},
		{Name: "a1", Value: 0, Step: 0.5},
	})
	if err != nil {
		t.Fatalf("could not fit: %+v", err)
	}

	a0, _, _ := fit.Value(0)
	a1, _, _ := fit.Value(1)
	if math.Abs(a0-3) > 1e-3 || math.Abs(a1-2) > 1e-3 {
		t.Fatalf("got a0=%v a1=%v, want 3 and 2", a0, a1)
	}

	// The points sit exactly on the line, so there is nothing left over --
	// down to the accuracy MIGRAD promises, which is to stop once the
	// remaining fall in the function is below 1e-3 * tolerance * UP.
	if got := fit.FMin(); got > 1e-3 {
		t.Errorf("chi2: got=%v, want~0", got)
	}

	// too few points for the parameters is refused rather than fitted.
	_, err = minuit.FitXY(xs[:1], ys[:1], errs[:1], minuit.Polynomial(1), []minuit.Par{
		{Name: "a0", Value: 0, Step: 0.5},
		{Name: "a1", Value: 0, Step: 0.5},
	})
	if err == nil {
		t.Error("fitting 2 parameters to 1 point was accepted")
	}
}
