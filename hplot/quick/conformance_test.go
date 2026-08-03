// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package quick_test

import (
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-hep.org/x/hep/groot/rdata"
	"go-hep.org/x/hep/hbook"
	"go-hep.org/x/hep/hplot/quick"
)

// normal is a sample nobody has to think about: the same one every run.
func normal(n int, mu, sigma float64) []float64 {
	rnd := rand.New(rand.NewPCG(1234, 5678))
	out := make([]float64, n)
	for i := range out {
		out[i] = mu + sigma*rnd.NormFloat64()
	}
	return out
}

// TestConformance_NoValueFallsOffTheEdge is the promise the automatic binning
// makes: every number given lands in a bin of the histogram, rather than in
// the overflow where it would be counted but not drawn.
func TestConformance_NoValueFallsOffTheEdge(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []float64
		opts []quick.Option
		bins int // the number asked for, when one was
	}{
		{name: "a normal sample", data: normal(10000, 0, 1)},
		{name: "a wide normal sample", data: normal(1000, 1e6, 2.5e5)},
		{name: "counts", data: []float64{0, 1, 1, 2, 2, 2, 3, 4, 5, 6, 7, 8}},
		{name: "two values", data: []float64{-1, 1}},
		{name: "one value", data: []float64{42}},
		{name: "the same value, many times", data: make([]float64, 500)},
		{name: "a value at zero", data: []float64{0, 0.5, 1}},
		{name: "negative", data: []float64{-9.5, -3.25, -1}},
		{name: "a chosen number of bins", data: normal(1000, 0, 1), opts: []quick.Option{quick.Bins(7)}, bins: 7},
		{name: "more bins than the rule would allow", data: normal(1000, 0, 1), opts: []quick.Option{quick.Bins(333)}, bins: 333},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, err := quick.Bin(tc.data, tc.opts...)
			if err != nil {
				t.Fatalf("could not bin: %+v", err)
			}

			if got, want := h.Entries(), int64(len(tc.data)); got != want {
				t.Fatalf("counted %d entries, want %d", got, want)
			}
			if n := h.Binning.Underflow().Entries(); n != 0 {
				t.Fatalf("%d values fell below the first bin", n)
			}
			if n := h.Binning.Overflow().Entries(); n != 0 {
				t.Fatalf("%d values fell past the last bin", n)
			}
			switch n := len(h.Binning.Bins); {
			case tc.bins > 0:
				if n != tc.bins {
					t.Fatalf("asked for %d bins and got %d", tc.bins, n)
				}
			case n < 1 || n > 200:
				t.Fatalf("%d bins is not a number the rule should have chosen", n)
			}
			if got := drawn(h); got != int64(len(tc.data)) {
				t.Fatalf("the bins hold %d entries, want %d", got, len(tc.data))
			}
		})
	}
}

// drawn is how many entries are in the bins that get drawn.
func drawn(h *hbook.H1D) int64 {
	var n int64
	for _, bin := range h.Binning.Bins {
		n += bin.Entries()
	}
	return n
}

// TestConformance_TheAxisReadsWell pins the other half of the automatic
// binning: edges on round numbers, and a bin count that follows the sample
// rather than a constant.
func TestConformance_TheAxisReadsWell(t *testing.T) {
	h, err := quick.Bin([]float64{0.4, 1.2, 3.7, 9.1, 17.3, 23.8, 41.5})
	if err != nil {
		t.Fatalf("could not bin: %+v", err)
	}

	rng := h.Binning.XRange
	if rng.Min != 0 {
		t.Fatalf("the first bin starts at %v, want a round number below the data", rng.Min)
	}
	if rng.Max < 41.5 {
		t.Fatalf("the last bin stops at %v, before the largest value", rng.Max)
	}

	// A sample that is spread out asks for more bins than one that is not.
	var (
		wide, _   = quick.Bin(normal(5000, 0, 10))
		narrow, _ = quick.Bin(normal(5000, 0, 10*1e-3))
	)
	if len(wide.Binning.Bins) != len(narrow.Binning.Bins) {
		t.Fatalf("the same shape at two scales was binned %d and %d ways",
			len(wide.Binning.Bins), len(narrow.Binning.Bins))
	}

	few, err := quick.Bin([]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	if err != nil {
		t.Fatalf("could not bin: %+v", err)
	}
	if got := len(few.Binning.Bins); got > len(wide.Binning.Bins) {
		t.Fatalf("ten values were binned %d ways and five thousand %d ways",
			got, len(wide.Binning.Bins))
	}
}

// TestConformance_WhatCannotBeBinnedIsLeftOut covers the values a file coming
// out of a real job actually holds.
func TestConformance_WhatCannotBeBinnedIsLeftOut(t *testing.T) {
	data := []float64{1, math.NaN(), 2, math.Inf(+1), 3, math.Inf(-1), 4}

	h, err := quick.Bin(data)
	if err != nil {
		t.Fatalf("could not bin: %+v", err)
	}
	if got, want := h.Entries(), int64(4); got != want {
		t.Fatalf("counted %d entries, want %d", got, want)
	}

	for _, tc := range []struct {
		name string
		data []float64
		want string
	}{
		{name: "nothing at all", data: nil, want: "there are no numbers to plot"},
		{name: "nothing finite", data: []float64{math.NaN(), math.Inf(-1)}, want: "none of these 2 numbers is finite"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := quick.Bin(tc.data)
			if err == nil {
				t.Fatalf("binning %v did not fail", tc.data)
			}
			if got := err.Error(); !strings.Contains(got, tc.want) {
				t.Fatalf("the error says %q, want it to say %q", got, tc.want)
			}
		})
	}
}

// TestConformance_TheChoicesCanBeTakenBack checks that an option beats the
// rule it replaces.
func TestConformance_TheChoicesCanBeTakenBack(t *testing.T) {
	data := normal(1000, 0, 1)

	h, err := quick.Bin(data, quick.Bins(13))
	if err != nil {
		t.Fatalf("could not bin: %+v", err)
	}
	if got, want := len(h.Binning.Bins), 13; got != want {
		t.Fatalf("asked for %d bins and got %d", want, got)
	}

	// A range that leaves data out is a decision, not a mistake: the values
	// outside it are counted as under- and overflow.
	h, err = quick.Bin(data, quick.Range(-1, 1), quick.Bins(10))
	if err != nil {
		t.Fatalf("could not bin: %+v", err)
	}
	if got, want := h.Binning.XRange, (hbook.Range{Min: -1, Max: 1}); got != want {
		t.Fatalf("binned over %v, want %v", got, want)
	}
	if out := h.Binning.Underflow().Entries() + h.Binning.Overflow().Entries(); out == 0 {
		t.Fatalf("a normal sample binned over [-1,1] left nothing outside it")
	}
	if got, want := h.Entries(), int64(len(data)); got != want {
		t.Fatalf("counted %d entries, want %d: what falls outside is still counted", got, want)
	}
}

// TestConformance_TheNameSaysWhatToDraw covers the one thing the caller must
// still decide, and the error when they have not.
func TestConformance_TheNameSaysWhatToDraw(t *testing.T) {
	var (
		dir  = t.TempDir()
		data = normal(500, 0, 1)
	)

	for _, name := range []string{"h.png", "h.pdf", "h.svg", "H.PNG"} {
		t.Run(name, func(t *testing.T) {
			dst := filepath.Join(dir, name)
			if err := quick.Hist(dst, data); err != nil {
				t.Fatalf("could not draw %q: %+v", name, err)
			}
			if size(t, dst) == 0 {
				t.Fatalf("%q was written empty", name)
			}
		})
	}

	for _, tc := range []struct {
		name string
		dst  string
		want string
	}{
		{name: "no name", dst: "", want: "needs a file name"},
		{name: "no extension", dst: filepath.Join(dir, "plot"), want: "does not say what kind of plot"},
		{name: "not a plot", dst: filepath.Join(dir, "plot.root"), want: ".png"},
		{name: "nowhere to write it", dst: filepath.Join(dir, "no", "such", "dir", "p.png"), want: "could not save"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := quick.Hist(tc.dst, data)
			if err == nil {
				t.Fatalf("drawing to %q did not fail", tc.dst)
			}
			if got := err.Error(); !strings.Contains(got, tc.want) {
				t.Fatalf("the error says %q, want it to say %q", got, tc.want)
			}
		})
	}
}

// TestConformance_EveryKindOfPlotDraws walks the whole surface of the package,
// options included, since a plot that panics is worse than one that is ugly.
func TestConformance_EveryKindOfPlotDraws(t *testing.T) {
	var (
		dir = t.TempDir()
		x   = normal(2000, 0, 1)
		y   = normal(2000, 5, 2)
		pos = make([]float64, len(y))
	)
	for i, v := range y {
		pos[i] = math.Abs(v) + 0.1
	}

	for _, tc := range []struct {
		name string
		draw func(dst string) error
	}{
		{"hist", func(dst string) error { return quick.Hist(dst, x) }},
		{"hist-titled", func(dst string) error {
			return quick.Hist(dst, x, quick.Title("px"), quick.XLabel("px [GeV]"), quick.YLabel("events"))
		}},
		{"hist-logy", func(dst string) error { return quick.Hist(dst, x, quick.LogY()) }},
		{"hist-plain", func(dst string) error { return quick.Hist(dst, x, quick.NoStats(), quick.Size(4, 3)) }},
		{"hists", func(dst string) error {
			return quick.Hists(dst, []quick.Series{{Name: "data", Data: x}, {Name: "simulation", Data: y}})
		}},
		{"hist2d", func(dst string) error { return quick.Hist2D(dst, x, y, quick.Title("y against x")) }},
		{"scatter", func(dst string) error { return quick.Scatter(dst, x, y) }},
		{"line", func(dst string) error { return quick.Line(dst, x, y) }},
		{"line-logy", func(dst string) error { return quick.Line(dst, x, pos, quick.LogY()) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dst := filepath.Join(dir, tc.name+".png")
			if err := tc.draw(dst); err != nil {
				t.Fatalf("could not draw: %+v", err)
			}
			if size(t, dst) == 0 {
				t.Fatalf("%q was written empty", tc.name)
			}
		})
	}
}

// TestConformance_TheErrorSaysWhatIsMissing covers the ways the data given to
// a plot can fail to be a plot.
func TestConformance_TheErrorSaysWhatIsMissing(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "p.png")

	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{
			name: "one coordinate missing",
			err:  quick.Scatter(dst, []float64{1, 2, 3}, []float64{1, 2}),
			want: "there are 3 values along x and 2 along y",
		},
		{
			name: "no points at all",
			err:  quick.Line(dst, nil, nil),
			want: "there are no numbers to plot",
		},
		{
			name: "no series",
			err:  quick.Hists(dst, nil),
			want: "there are no series to plot",
		},
		{
			name: "a series with no name",
			err:  quick.Hists(dst, []quick.Series{{Data: []float64{1, 2}}}),
			want: "series 0 has no name",
		},
		{
			name: "a range that does not grow",
			err:  quick.Hist(dst, []float64{1, 2, 3}, quick.Range(5, 5)),
			want: "[5, 5] is not a range to bin over",
		},
		{
			name: "a logarithmic axis through zero",
			err:  quick.Line(dst, []float64{1, 2}, []float64{0, 1}, quick.LogY()),
			want: "only numbers above zero",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err == nil {
				t.Fatalf("that did not fail")
			}
			if got := tc.err.Error(); !strings.Contains(got, tc.want) {
				t.Fatalf("the error says %q, want it to say %q", got, tc.want)
			}
		})
	}
}

// TestConformance_AColumnOfAROOTFileIsJustNumbers is the whole path this
// package exists to shorten: open a file, take a column, draw it.
func TestConformance_AColumnOfAROOTFileIsJustNumbers(t *testing.T) {
	tbl, err := rdata.Open("../../groot/testdata/simple.root")
	if err != nil {
		t.Fatalf("could not open the file: %+v", err)
	}
	defer tbl.Close()

	one, err := tbl.Floats("one")
	if err != nil {
		t.Fatalf("could not read the column: %+v", err)
	}

	dst := filepath.Join(t.TempDir(), "one.png")
	if err := quick.Hist(dst, one, quick.Title("one")); err != nil {
		t.Fatalf("could not draw the column: %+v", err)
	}
	if size(t, dst) == 0 {
		t.Fatalf("the plot was written empty")
	}
}

func size(t *testing.T, name string) int64 {
	t.Helper()

	fi, err := os.Stat(name)
	if err != nil {
		t.Fatalf("could not stat %q: %v", name, err)
	}
	return fi.Size()
}
