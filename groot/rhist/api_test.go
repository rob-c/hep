// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rhist

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"go-hep.org/x/hep/groot/riofs"
	"go-hep.org/x/hep/groot/root"
	"go-hep.org/x/hep/hbook"
)

// TestH1Fill checks a TH1 can be built and filled the way a ROOT user builds
// and fills one, and that the statistics come out right.
func TestH1Fill(t *testing.T) {
	h := NewH1D("h", "a title", 10, 0, 10)

	if got, want := h.Name(), "h"; got != want {
		t.Errorf("name: got=%q, want=%q", got, want)
	}
	if got, want := h.Title(), "a title"; got != want {
		t.Errorf("title: got=%q, want=%q", got, want)
	}
	if got, want := h.NbinsX(), 10; got != want {
		t.Errorf("nbins: got=%d, want=%d", got, want)
	}

	// one entry in each bin, weight equal to the bin number.
	for i := range 10 {
		h.Fill(float64(i)+0.5, float64(i+1))
	}

	if got, want := h.Entries(), 10.0; got != want {
		t.Errorf("entries: got=%v, want=%v", got, want)
	}
	if got, want := h.Integral(), 55.0; got != want {
		t.Errorf("integral: got=%v, want=%v", got, want)
	}

	for i := range 10 {
		if got, want := h.BinContent(i+1), float64(i+1); got != want {
			t.Errorf("bin %d: got=%v, want=%v", i+1, got, want)
		}
	}

	// the mean of x weighted by the bin number: sum(w*x)/sum(w).
	var sw, swx float64
	for i := range 10 {
		w := float64(i + 1)
		sw += w
		swx += w * (float64(i) + 0.5)
	}
	if got, want := h.Mean(), swx/sw; math.Abs(got-want) > 1e-12 {
		t.Errorf("mean: got=%v, want=%v", got, want)
	}

	if got, want := h.Maximum(), 10.0; got != want {
		t.Errorf("maximum: got=%v, want=%v", got, want)
	}
	if got, want := h.MaximumBin(), 10; got != want {
		t.Errorf("maximum bin: got=%d, want=%d", got, want)
	}
	if got, want := h.Minimum(), 1.0; got != want {
		t.Errorf("minimum: got=%v, want=%v", got, want)
	}
}

// TestH1UnderAndOverflow checks entries outside the axis land in the cells
// ROOT keeps for them, and stay out of the mean.
func TestH1UnderAndOverflow(t *testing.T) {
	h := NewH1D("h", "", 4, 0, 4)

	h.Fill(-1, 1) // underflow
	h.Fill(2.5, 1)
	h.Fill(99, 1) // overflow

	if got, want := h.BinContent(0), 1.0; got != want {
		t.Errorf("underflow: got=%v, want=%v", got, want)
	}
	if got, want := h.BinContent(5), 1.0; got != want {
		t.Errorf("overflow: got=%v, want=%v", got, want)
	}
	if got, want := h.Integral(), 1.0; got != want {
		t.Errorf("integral should leave out the flows: got=%v, want=%v", got, want)
	}
	if got, want := h.Entries(), 3.0; got != want {
		t.Errorf("entries should count them all: got=%v, want=%v", got, want)
	}
	// the mean is of what fell inside.
	if got, want := h.Mean(), 2.5; math.Abs(got-want) > 1e-12 {
		t.Errorf("mean: got=%v, want=%v", got, want)
	}
}

// TestFindBin checks the numbering matches ROOT's: 0 is the underflow, 1 to
// n are the bins, n+1 is the overflow.
func TestFindBin(t *testing.T) {
	h := NewH1D("h", "", 4, 0, 4)

	for _, tc := range []struct {
		x    float64
		want int
	}{
		{-0.001, 0},
		{0, 1},
		{0.5, 1},
		{0.999, 1},
		{1, 2},
		{3.999, 4},
		{4, 5},
		{100, 5},
	} {
		if got := h.FindBin(tc.x); got != tc.want {
			t.Errorf("FindBin(%v): got=%d, want=%d", tc.x, got, tc.want)
		}
	}
}

// TestVariableBins checks a histogram given explicit edges bins by them.
func TestVariableBins(t *testing.T) {
	h := NewH1DFromEdges("h", "", []float64{0, 1, 10, 100})

	if got, want := h.NbinsX(), 3; got != want {
		t.Fatalf("nbins: got=%d, want=%d", got, want)
	}

	for _, tc := range []struct {
		x    float64
		want int
	}{
		{-1, 0},
		{0.5, 1},
		{5, 2},
		{50, 3},
		{1000, 4},
	} {
		if got := h.FindBin(tc.x); got != tc.want {
			t.Errorf("FindBin(%v): got=%d, want=%d", tc.x, got, tc.want)
		}
	}

	h.Fill(50, 2)
	if got, want := h.BinContent(3), 2.0; got != want {
		t.Errorf("bin 3: got=%v, want=%v", got, want)
	}
}

// TestScale checks scaling moves the contents and the uncertainties the way
// it should: a factor f on the contents is f squared on the squared errors,
// so the relative uncertainty does not move.
func TestScale(t *testing.T) {
	h := NewH1D("h", "", 4, 0, 4)
	for range 100 {
		h.Fill(1.5, 1)
	}

	var (
		before    = h.BinContent(2)
		beforeErr = h.BinError(2)
	)

	h.Scale(2)

	if got, want := h.BinContent(2), 2*before; math.Abs(got-want) > 1e-9 {
		t.Errorf("content: got=%v, want=%v", got, want)
	}
	if got, want := h.BinError(2), 2*beforeErr; math.Abs(got-want) > 1e-9 {
		t.Errorf("error: got=%v, want=%v", got, want)
	}
	// which is to say the relative uncertainty is untouched.
	if got, want := h.BinError(2)/h.BinContent(2), beforeErr/before; math.Abs(got-want) > 1e-12 {
		t.Errorf("relative error moved: got=%v, want=%v", got, want)
	}
}

// TestSetBin checks a histogram can be built bin by bin, which is how a
// template arrives from somewhere else.
func TestSetBin(t *testing.T) {
	h := NewH1D("h", "", 3, 0, 3)

	h.SetBinContent(1, 10)
	h.SetBinContent(2, 20)
	h.SetBinContent(3, 30)
	h.SetBinError(2, 5)

	if got, want := h.Integral(), 60.0; got != want {
		t.Errorf("integral: got=%v, want=%v", got, want)
	}
	if got, want := h.BinError(2), 5.0; math.Abs(got-want) > 1e-12 {
		t.Errorf("error: got=%v, want=%v", got, want)
	}

	h.Reset()
	if got, want := h.Integral(), 0.0; got != want {
		t.Errorf("after reset: got=%v, want=%v", got, want)
	}
}

// TestH2Fill checks the 2-dim histogram fills and projects.
func TestH2Fill(t *testing.T) {
	h := NewH2D("h", "", 4, 0, 4, 2, 0, 2)

	// four entries, two in each y row.
	h.Fill(0.5, 0.5, 1)
	h.Fill(1.5, 0.5, 2)
	h.Fill(0.5, 1.5, 3)
	h.Fill(2.5, 1.5, 4)

	if got, want := h.Integral(), 10.0; got != want {
		t.Errorf("integral: got=%v, want=%v", got, want)
	}
	if got, want := h.BinContent(1, 1), 1.0; got != want {
		t.Errorf("bin (1,1): got=%v, want=%v", got, want)
	}
	if got, want := h.BinContent(3, 2), 4.0; got != want {
		t.Errorf("bin (3,2): got=%v, want=%v", got, want)
	}

	px := h.ProjectionX("px")
	if got, want := px.Integral(), 10.0; got != want {
		t.Errorf("projection x: integral got=%v, want=%v", got, want)
	}
	// x bin 1 holds the two entries at x=0.5: weights 1 and 3.
	if got, want := px.BinContent(1), 4.0; got != want {
		t.Errorf("projection x bin 1: got=%v, want=%v", got, want)
	}

	py := h.ProjectionY("py")
	if got, want := py.Integral(), 10.0; got != want {
		t.Errorf("projection y: integral got=%v, want=%v", got, want)
	}
	// y bin 1 holds weights 1 and 2.
	if got, want := py.BinContent(1), 3.0; got != want {
		t.Errorf("projection y bin 1: got=%v, want=%v", got, want)
	}

	// the projections keep the means of the axis they kept.
	if got, want := px.Mean(), h.MeanX(); math.Abs(got-want) > 1e-12 {
		t.Errorf("projection x mean: got=%v, want=%v", got, want)
	}
	if got, want := py.Mean(), h.MeanY(); math.Abs(got-want) > 1e-12 {
		t.Errorf("projection y mean: got=%v, want=%v", got, want)
	}
}

// TestH3Fill checks the 3-dim histogram fills and projects, and that a
// projection keeps everything that was in it.
func TestH3Fill(t *testing.T) {
	h := NewH3D("h", "", 3, 0, 3, 3, 0, 3, 3, 0, 3)

	w := 1.0
	for ix := range 3 {
		for iy := range 3 {
			for iz := range 3 {
				h.Fill(float64(ix)+0.5, float64(iy)+0.5, float64(iz)+0.5, w)
				w++
			}
		}
	}

	total := 0.0
	for i := 1.0; i <= 27; i++ {
		total += i
	}

	if got := h.Integral(); math.Abs(got-total) > 1e-9 {
		t.Errorf("integral: got=%v, want=%v", got, total)
	}
	if got, want := h.Entries(), 27.0; got != want {
		t.Errorf("entries: got=%v, want=%v", got, want)
	}

	pz := h.ProjectionZ("pz")
	if got := pz.Integral(); math.Abs(got-total) > 1e-9 {
		t.Errorf("projection z: integral got=%v, want=%v", got, total)
	}

	pxy := h.ProjectionXY("pxy")
	if got := pxy.Integral(); math.Abs(got-total) > 1e-9 {
		t.Errorf("projection xy: integral got=%v, want=%v", got, total)
	}
	if got, want := pxy.NbinsX(), 3; got != want {
		t.Errorf("projection xy nbins x: got=%d, want=%d", got, want)
	}

	// the means survive the projections.
	if got, want := pz.Mean(), h.MeanZ(); math.Abs(got-want) > 1e-12 {
		t.Errorf("projection z mean: got=%v, want=%v", got, want)
	}
	if got, want := pxy.MeanX(), h.MeanX(); math.Abs(got-want) > 1e-12 {
		t.Errorf("projection xy mean x: got=%v, want=%v", got, want)
	}
}

// TestFilledMatchesHbook checks a histogram filled through the ROOT surface
// agrees with the same one filled through hbook, which is the other way in.
func TestFilledMatchesHbook(t *testing.T) {
	var (
		root = NewH1D("h", "", 10, 0, 10)
		book = hbook.NewH1D(10, 0, 10)
	)

	for i := range 1000 {
		x := float64(i%10) + 0.5
		w := float64(i%3) + 1
		root.Fill(x, w)
		book.Fill(x, w)
	}

	if got, want := root.Integral(), book.SumW(); math.Abs(got-want) > 1e-9 {
		t.Errorf("integral: root=%v, hbook=%v", got, want)
	}
	if got, want := root.Mean(), book.XMean(); math.Abs(got-want) > 1e-9 {
		t.Errorf("mean: root=%v, hbook=%v", got, want)
	}
	// the two spreads differ on purpose. ROOT's TH1::GetStdDev is the
	// spread of the distribution as filled, while hbook's XStdDev carries
	// Bessel's correction for a sample, over the effective number of
	// entries. They sit a factor sqrt(n/(n-1)) apart.
	var (
		neff = book.SumW() * book.SumW() / book.SumW2()
		want = book.XStdDev() * math.Sqrt((neff-1)/neff)
	)
	if got := root.StdDev(); math.Abs(got-want) > 1e-9 {
		t.Errorf("std dev: got=%v, want=%v (hbook=%v over neff=%v)", got, want, book.XStdDev(), neff)
	}

	// and converting it over gives the same thing again.
	conv := root.AsH1D()
	if got, want := conv.SumW(), book.SumW(); math.Abs(got-want) > 1e-9 {
		t.Errorf("converted sumw: got=%v, want=%v", got, want)
	}
}

// TestFillNAndPanics checks the bulk fill and that mismatched lengths are
// refused rather than half-applied.
func TestFillNAndPanics(t *testing.T) {
	h := NewH1D("h", "", 4, 0, 4)
	h.FillN([]float64{0.5, 1.5, 2.5}, []float64{1, 2, 3})
	if got, want := h.Integral(), 6.0; got != want {
		t.Errorf("integral: got=%v, want=%v", got, want)
	}

	h.Reset()
	h.FillN([]float64{0.5, 1.5}, nil)
	if got, want := h.Integral(), 2.0; got != want {
		t.Errorf("unit weights: got=%v, want=%v", got, want)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("mismatched lengths were accepted")
		}
	}()
	h.FillN([]float64{1, 2}, []float64{1})
}

// TestEveryFlavourFills checks each of the fifteen types can be made and
// filled, the integer ones included.
func TestEveryFlavourFills(t *testing.T) {
	for _, tc := range []struct {
		name string
		fill func() float64
	}{
		{"TH1C", func() float64 { h := NewH1C("h", "", 4, 0, 4); h.Fill(1.5, 3); return h.Integral() }},
		{"TH1S", func() float64 { h := NewH1S("h", "", 4, 0, 4); h.Fill(1.5, 3); return h.Integral() }},
		{"TH1I", func() float64 { h := NewH1I("h", "", 4, 0, 4); h.Fill(1.5, 3); return h.Integral() }},
		{"TH1F", func() float64 { h := NewH1F("h", "", 4, 0, 4); h.Fill(1.5, 3); return h.Integral() }},
		{"TH1D", func() float64 { h := NewH1D("h", "", 4, 0, 4); h.Fill(1.5, 3); return h.Integral() }},

		{"TH2C", func() float64 { h := NewH2C("h", "", 4, 0, 4, 4, 0, 4); h.Fill(1.5, 1.5, 3); return h.Integral() }},
		{"TH2S", func() float64 { h := NewH2S("h", "", 4, 0, 4, 4, 0, 4); h.Fill(1.5, 1.5, 3); return h.Integral() }},
		{"TH2I", func() float64 { h := NewH2I("h", "", 4, 0, 4, 4, 0, 4); h.Fill(1.5, 1.5, 3); return h.Integral() }},
		{"TH2F", func() float64 { h := NewH2F("h", "", 4, 0, 4, 4, 0, 4); h.Fill(1.5, 1.5, 3); return h.Integral() }},
		{"TH2D", func() float64 { h := NewH2D("h", "", 4, 0, 4, 4, 0, 4); h.Fill(1.5, 1.5, 3); return h.Integral() }},

		{"TH3C", func() float64 {
			h := NewH3C("h", "", 4, 0, 4, 4, 0, 4, 4, 0, 4)
			h.Fill(1.5, 1.5, 1.5, 3)
			return h.Integral()
		}},
		{"TH3S", func() float64 {
			h := NewH3S("h", "", 4, 0, 4, 4, 0, 4, 4, 0, 4)
			h.Fill(1.5, 1.5, 1.5, 3)
			return h.Integral()
		}},
		{"TH3I", func() float64 {
			h := NewH3I("h", "", 4, 0, 4, 4, 0, 4, 4, 0, 4)
			h.Fill(1.5, 1.5, 1.5, 3)
			return h.Integral()
		}},
		{"TH3F", func() float64 {
			h := NewH3F("h", "", 4, 0, 4, 4, 0, 4, 4, 0, 4)
			h.Fill(1.5, 1.5, 1.5, 3)
			return h.Integral()
		}},
		{"TH3D", func() float64 {
			h := NewH3D("h", "", 4, 0, 4, 4, 0, 4, 4, 0, 4)
			h.Fill(1.5, 1.5, 1.5, 3)
			return h.Integral()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, want := tc.fill(), 3.0; got != want {
				t.Errorf("got=%v, want=%v", got, want)
			}
		})
	}
}

// TestFilledRoundTrip checks a histogram built and filled entirely through
// this package survives a trip through a ROOT file, contents, uncertainties
// and running sums alike.
func TestFilledRoundTrip(t *testing.T) {
	tmp, err := os.MkdirTemp("", "groot-rhist-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	fname := filepath.Join(tmp, "histos.root")

	h1 := NewH1D("h1", "one dim", 10, 0, 10)
	h2 := NewH2D("h2", "two dim", 4, 0, 4, 4, 0, 4)
	h3 := NewH3D("h3", "three dim", 3, 0, 3, 3, 0, 3, 3, 0, 3)

	for i := range 100 {
		x := float64(i%10) + 0.5
		w := float64(i%4) + 1
		h1.Fill(x, w)
		h2.Fill(x/3, x/4, w)
		h3.Fill(x/4, x/4, x/4, w)
	}

	func() {
		w, err := riofs.Create(fname)
		if err != nil {
			t.Fatal(err)
		}
		defer w.Close()

		for _, h := range []root.Named{h1, h2, h3} {
			err := w.Put(h.Name(), h)
			if err != nil {
				t.Fatal(err)
			}
		}
		err = w.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()

	r, err := riofs.Open(fname)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	t.Run("TH1D", func(t *testing.T) {
		o, err := r.Get("h1")
		if err != nil {
			t.Fatal(err)
		}
		got := o.(*H1D)

		if got, want := got.Title(), "one dim"; got != want {
			t.Errorf("title: got=%q, want=%q", got, want)
		}
		if got, want := got.Integral(), h1.Integral(); got != want {
			t.Errorf("integral: got=%v, want=%v", got, want)
		}
		if got, want := got.Entries(), h1.Entries(); got != want {
			t.Errorf("entries: got=%v, want=%v", got, want)
		}
		if got, want := got.Mean(), h1.Mean(); math.Abs(got-want) > 1e-12 {
			t.Errorf("mean: got=%v, want=%v", got, want)
		}
		if got, want := got.StdDev(), h1.StdDev(); math.Abs(got-want) > 1e-12 {
			t.Errorf("std dev: got=%v, want=%v", got, want)
		}
		for i := range got.NbinsX() + 2 {
			if got, want := got.BinContent(i), h1.BinContent(i); got != want {
				t.Errorf("bin %d: got=%v, want=%v", i, got, want)
			}
			if got, want := got.BinError(i), h1.BinError(i); math.Abs(got-want) > 1e-12 {
				t.Errorf("bin %d error: got=%v, want=%v", i, got, want)
			}
		}
		// and it can still be filled once it is back.
		got.Fill(5.5, 1)
		if got, want := got.Integral(), h1.Integral()+1; got != want {
			t.Errorf("refill: got=%v, want=%v", got, want)
		}
	})

	t.Run("TH2D", func(t *testing.T) {
		o, err := r.Get("h2")
		if err != nil {
			t.Fatal(err)
		}
		got := o.(*H2D)

		if got, want := got.Integral(), h2.Integral(); got != want {
			t.Errorf("integral: got=%v, want=%v", got, want)
		}
		for ix := range got.NbinsX() + 2 {
			for iy := range got.NbinsY() + 2 {
				if got, want := got.BinContent(ix, iy), h2.BinContent(ix, iy); got != want {
					t.Errorf("bin (%d,%d): got=%v, want=%v", ix, iy, got, want)
				}
			}
		}
		if got, want := got.ProjectionX("px").Integral(), h2.ProjectionX("px").Integral(); got != want {
			t.Errorf("projection x: got=%v, want=%v", got, want)
		}
	})

	t.Run("TH3D", func(t *testing.T) {
		o, err := r.Get("h3")
		if err != nil {
			t.Fatal(err)
		}
		got := o.(*H3D)

		if got, want := got.Integral(), h3.Integral(); got != want {
			t.Errorf("integral: got=%v, want=%v", got, want)
		}
		if got, want := got.MeanZ(), h3.MeanZ(); math.Abs(got-want) > 1e-12 {
			t.Errorf("mean z: got=%v, want=%v", got, want)
		}
		if got, want := got.ProjectionZ("pz").Integral(), h3.ProjectionZ("pz").Integral(); got != want {
			t.Errorf("projection z: got=%v, want=%v", got, want)
		}
	})
}
