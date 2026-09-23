// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package spectrum finds peaks in a spectrum and the background under them,
// which is what ROOT's TSpectrum is for.
//
// A measured spectrum is a few peaks sitting on a continuum that wanders.
// Reading the peaks off means deciding where the continuum is, and the
// trouble with that is circular: the continuum is what is left once the
// peaks are taken out, and the peaks are what stands above the continuum.
//
// Background settles it by clipping. A peak is narrow and the continuum is
// not, so repeatedly replacing each channel by the lower of itself and the
// average of the two channels a given distance either side of it wears the
// peaks away and leaves the continuum standing. Doing that for widths from
// one channel up to the widest peak leaves a curve that follows the
// continuum and ignores the peaks. It is the method Ryan and others
// published in 1988 and that Morháč and others refined, and it is what
// TSpectrum does.
//
//	bkg := spectrum.Background(ys)
//	peaks := spectrum.Search(h, spectrum.Sigma(2), spectrum.Threshold(0.1))
//
// Search takes the background out, smooths what is left, and reports what
// still stands above the threshold.
//
// Two things are worth knowing about the estimate. It sits a little below
// the continuum, by a percent or two, because clipping only ever lowers a
// channel and never raises one back. And the threshold is what decides
// whether a bump is a peak: counts that fluctuate by their own square root
// leave bumps of a few percent of the tallest peak, which the default of a
// twentieth — ROOT's default too — will report. Raise it for a noisy
// spectrum.
package spectrum // import "go-hep.org/x/hep/hbook/spectrum"

import (
	"fmt"
	"math"
	"sort"

	"go-hep.org/x/hep/hbook"
)

// Option configures a search or a background estimate.
type Option func(*config)

type config struct {
	iters     int     // how wide a peak may be, in channels
	sigma     float64 // the width of the peaks being looked for, in channels
	threshold float64 // how tall a peak must be, as a fraction of the tallest
	order     int     // 2, 4, 6 or 8: the clipping window's shape
	smooth    int     // how far the smoothing reaches, in channels; 0 for none
	noBkg     bool
}

func newConfig(opts []Option) *config {
	cfg := &config{
		iters:     20,
		sigma:     2,
		threshold: 0.05,
		order:     2,
		smooth:    3,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// Iterations sets how far the clipping reaches, in channels. It should be
// about the half-width of the widest peak: anything narrower than this is
// taken for a peak and anything wider for background.
func Iterations(n int) Option {
	return func(cfg *config) {
		if n > 0 {
			cfg.iters = n
		}
	}
}

// Sigma sets the width of the peaks being looked for, in channels. A search
// uses it to decide how far apart two maxima have to be to be two peaks.
func Sigma(v float64) Option {
	return func(cfg *config) {
		if v > 0 {
			cfg.sigma = v
		}
	}
}

// Threshold sets how tall a peak has to be to count, as a fraction of the
// tallest one found. It is 0.05 by default, as in ROOT.
func Threshold(v float64) Option {
	return func(cfg *config) { cfg.threshold = v }
}

// Order sets the shape of the clipping window: 2 compares a channel with
// the average of the two either side, and 4, 6 and 8 bring in further
// neighbours, which follows a curving background more closely at the cost of
// being readier to mistake a broad peak for one.
func Order(n int) Option {
	return func(cfg *config) {
		switch n {
		case 2, 4, 6, 8:
			cfg.order = n
		}
	}
}

// Smoothing sets how far the smoothing before a search reaches, in
// channels. Zero does not smooth.
func Smoothing(n int) Option {
	return func(cfg *config) {
		if n >= 0 {
			cfg.smooth = n
		}
	}
}

// NoBackground searches the spectrum as it stands, without taking a
// background out of it first. It is for a spectrum that has had one taken
// out already.
func NoBackground() Option {
	return func(cfg *config) { cfg.noBkg = true }
}

// Background returns the continuum under a spectrum, channel by channel.
//
// The result is never above the spectrum it was given and never negative.
func Background(ys []float64, opts ...Option) []float64 {
	cfg := newConfig(opts)
	return background(ys, cfg)
}

func background(ys []float64, cfg *config) []float64 {
	n := len(ys)
	out := make([]float64, n)
	if n == 0 {
		return out
	}

	// Clipping works on counts that span orders of magnitude, so it is done
	// on a scale that squashes them: twice the logarithm of a square root,
	// which turns a Poisson spectrum's spread into something even and makes
	// the clipping behave the same way at the foot of a peak as at the top.
	v := make([]float64, n)
	for i, y := range ys {
		v[i] = lls(y)
	}

	buf := make([]float64, n)
	for w := 1; w <= cfg.iters; w++ {
		copy(buf, v)
		for i := w; i < n-w; i++ {
			if m := clip(v, i, w, cfg.order); m < v[i] {
				buf[i] = m
			}
		}
		copy(v, buf)
	}

	for i := range out {
		out[i] = unlls(v[i])
		// the clipping only ever lowers a channel, but the round trip
		// through the transform can leave a rounding crumb above it.
		if out[i] > ys[i] {
			out[i] = ys[i]
		}
		if out[i] < 0 {
			out[i] = 0
		}
	}
	return out
}

// clip returns what the channel at i would be if it were background: the
// average of its neighbours a distance w away, or a weighted average
// reaching further out for the higher orders.
func clip(v []float64, i, w, order int) float64 {
	a := 0.5 * (v[i-w] + v[i+w])
	if order == 2 {
		return a
	}

	// The higher orders subtract off the curvature that the two-point
	// average leaves behind, so that a background bending under a peak is
	// followed rather than cut into.
	half := w / 2
	if half < 1 || i-w-half < 0 || i+w+half >= len(v) {
		return a
	}

	b := 0.5 * (v[i-w-half] + v[i+w+half])
	switch order {
	case 4:
		return math.Min(a, (4*a-b)/3)
	case 6, 8:
		return math.Min(a, (6*a-b)/5)
	}
	return a
}

// lls squashes a count onto the scale the clipping works on.
func lls(y float64) float64 {
	if y < 0 {
		y = 0
	}
	return math.Log(math.Log(math.Sqrt(y+1)+1) + 1)
}

// unlls brings a value back from it.
func unlls(v float64) float64 {
	e := math.Exp(math.Exp(v) - 1)
	return (e-1)*(e-1) - 1
}

// Smooth returns the spectrum smoothed over a window of the given width in
// channels, which is what a search does before looking for maxima.
//
// The window is a gaussian of about a third of the width, so that a channel
// counts for less the further it is from the middle, and the ends are held
// by shortening the window rather than by assuming anything beyond them.
func Smooth(ys []float64, window int) []float64 {
	n := len(ys)
	out := make([]float64, n)
	if window <= 0 || n == 0 {
		copy(out, ys)
		return out
	}

	var (
		sigma = float64(window) / 3
		w     = make([]float64, window+1)
	)
	for d := range w {
		w[d] = math.Exp(-0.5 * float64(d) * float64(d) / (sigma * sigma))
	}

	for i := range n {
		var sum, norm float64
		for d := -window; d <= window; d++ {
			j := i + d
			if j < 0 || j >= n {
				continue
			}
			k := w[abs(d)]
			sum += k * ys[j]
			norm += k
		}
		if norm > 0 {
			out[i] = sum / norm
		}
	}
	return out
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Peak is a peak found in a spectrum.
type Peak struct {
	X     float64 // where it is, on the histogram's axis
	Y     float64 // how far it stands above the background
	Bin   int     // the bin it was found in, counting from zero
	Width float64 // an estimate of its full width at half its height
}

// Search returns the peaks of a histogram, tallest first.
//
// The background is taken out, what is left is smoothed, and every channel
// that stands above both its neighbours and the threshold is a peak. Two
// maxima closer together than the width being looked for are one peak, the
// taller of them.
//
// The position is refined by fitting a parabola through the peak channel and
// the two beside it, so that a peak is located to better than the width of a
// bin.
func Search(h *hbook.H1D, opts ...Option) ([]Peak, error) {
	if h == nil {
		return nil, fmt.Errorf("spectrum: no histogram to search")
	}
	cfg := newConfig(opts)

	var (
		bins = h.Binning.Bins
		n    = len(bins)
		ys   = make([]float64, n)
		xs   = make([]float64, n)
	)
	if n < 3 {
		return nil, fmt.Errorf("spectrum: a histogram of %d bins is too few to find a peak in", n)
	}
	for i := range bins {
		ys[i] = bins[i].SumW()
		xs[i] = bins[i].XMid()
	}

	sig := ys
	if !cfg.noBkg {
		bkg := background(ys, cfg)
		sig = make([]float64, n)
		for i := range ys {
			sig[i] = ys[i] - bkg[i]
		}
	}
	if cfg.smooth > 0 {
		sig = Smooth(sig, cfg.smooth)
	}

	// the tallest channel sets what the threshold means.
	high := 0.0
	for _, v := range sig {
		high = math.Max(high, v)
	}
	if high <= 0 {
		return nil, nil
	}
	cut := cfg.threshold * high

	var peaks []Peak
	for i := 1; i < n-1; i++ {
		if sig[i] < cut || sig[i] < sig[i-1] || sig[i] < sig[i+1] {
			continue
		}
		// a flat top is one peak, reported at its first channel.
		if sig[i] == sig[i-1] {
			continue
		}

		x, width := refine(xs, sig, i)
		peaks = append(peaks, Peak{X: x, Y: sig[i], Bin: i, Width: width})
	}

	peaks = merge(peaks, cfg.sigma*binWidth(xs))
	sort.Slice(peaks, func(i, j int) bool { return peaks[i].Y > peaks[j].Y })
	return peaks, nil
}

// binWidth returns how wide a bin is, which turns a width in channels into
// one on the axis.
func binWidth(xs []float64) float64 {
	if len(xs) < 2 {
		return 1
	}
	return xs[1] - xs[0]
}

// refine locates a peak to better than a bin by fitting a parabola through
// its channel and the two beside it, and estimates its width from how sharp
// that parabola is.
func refine(xs, ys []float64, i int) (x, width float64) {
	var (
		y0, y1, y2 = ys[i-1], ys[i], ys[i+1]
		w          = binWidth(xs)
		denom      = y0 - 2*y1 + y2
	)
	x = xs[i]
	if denom != 0 {
		// the turning point of the parabola through the three, in bins.
		d := 0.5 * (y0 - y2) / denom
		if math.Abs(d) <= 1 {
			x = xs[i] + d*w
		}
	}

	// A gaussian of width s has a second difference of -h/s² at its top,
	// where h is its height, so the curvature gives the width. The full
	// width at half the height is that times the usual factor.
	if denom < 0 && y1 > 0 {
		s := math.Sqrt(-y1 / denom)
		width = 2 * math.Sqrt(2*math.Ln2) * s * w
	}
	return x, width
}

// merge folds maxima closer together than the width being looked for into
// one peak, keeping the taller.
func merge(peaks []Peak, sep float64) []Peak {
	if len(peaks) < 2 || sep <= 0 {
		return peaks
	}

	out := peaks[:0:0]
	for _, p := range peaks {
		if n := len(out); n > 0 && math.Abs(p.X-out[n-1].X) < sep {
			if p.Y > out[n-1].Y {
				out[n-1] = p
			}
			continue
		}
		out = append(out, p)
	}
	return out
}
