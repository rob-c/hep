// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hbook

import (
	"fmt"
	"math"

	"gonum.org/v1/gonum/mathext"
	"gonum.org/v1/gonum/stat/distuv"
)

// IntervalKind selects how the uncertainty on an efficiency is computed.
//
// All of them answer the same question -- what range of true efficiencies is
// compatible with the counts observed -- and differ in how they trade exactness
// against width. None of them can leave [0, 1], which is the whole point:
// propagating the numerator and denominator errors in quadrature, as one would
// for a ratio of two independent measurements, gives an interval that extends
// past 1 and that is nonzero at an efficiency of exactly 1, neither of which
// can be right for a count divided by a count it is part of.
type IntervalKind int

const (
	// ClopperPearson is the interval obtained by inverting the binomial test.
	// It is exact, in the sense that it never covers less than the level asked
	// for, at the price of covering more: it is conservative, especially for
	// small samples. It is the default.
	ClopperPearson IntervalKind = iota

	// Wilson is the interval obtained by inverting the score test. Its coverage
	// oscillates around the level asked for rather than sitting above it, which
	// makes it noticeably narrower than ClopperPearson while staying sensible
	// at 0 and 1. It is the usual recommendation for everyday use.
	Wilson

	// AgrestiCoull is the normal interval computed as though z^2/2 successes
	// and z^2/2 failures had been added to the sample. It is a hair wider than
	// Wilson and simpler to explain.
	AgrestiCoull

	// Normal is the textbook interval, eff +/- z*sqrt(eff*(1-eff)/n), clipped
	// to [0, 1]. It is wrong near 0 and 1 -- where it collapses to zero width
	// no matter how few events went into it -- and is offered for comparison
	// with results that were produced that way.
	Normal
)

func (k IntervalKind) String() string {
	switch k {
	case ClopperPearson:
		return "clopper-pearson"
	case Wilson:
		return "wilson"
	case AgrestiCoull:
		return "agresti-coull"
	case Normal:
		return "normal"
	default:
		panic(fmt.Errorf("hbook: invalid IntervalKind (%d)", int(k)))
	}
}

// DefaultEffLevel is the confidence level efficiencies are quoted at unless
// told otherwise: the probability a Gaussian assigns to one standard
// deviation either side of its mean, so that the error bars read like the
// familiar +/- 1 sigma.
const DefaultEffLevel = 0.682689492137086

// BinomialInterval returns the confidence interval on the efficiency k/n at
// the given confidence level, for k successes out of n trials.
//
// n <= 0 returns (0, 0): no trials, nothing to say. k outside [0, n] is
// clamped into it, so that a numerator nudged past its denominator by
// rounding does not produce a nonsensical interval.
func BinomialInterval(kind IntervalKind, k, n, level float64) (lo, hi float64) {
	switch {
	case n <= 0:
		return 0, 0
	case k < 0:
		k = 0
	case k > n:
		k = n
	}

	if level <= 0 || level >= 1 {
		panic(fmt.Errorf("hbook: confidence level %v out of range (0, 1)", level))
	}

	switch kind {
	case ClopperPearson:
		// The interval is read off the Beta distribution the binomial
		// likelihood integrates to: no normal approximation anywhere.
		alpha := 1 - level
		switch {
		case k <= 0:
			lo = 0
		default:
			lo = mathext.InvRegIncBeta(k, n-k+1, alpha/2)
		}
		switch {
		case k >= n:
			hi = 1
		default:
			hi = mathext.InvRegIncBeta(k+1, n-k, 1-alpha/2)
		}
		return lo, hi

	case Wilson:
		var (
			z   = zOf(level)
			z2  = z * z
			mid = (k + z2/2) / (n + z2)
			hw  = z / (n + z2) * math.Sqrt(k*(n-k)/n+z2/4)
		)
		return clamp01(mid - hw), clamp01(mid + hw)

	case AgrestiCoull:
		var (
			z   = zOf(level)
			z2  = z * z
			nn  = n + z2
			eff = (k + z2/2) / nn
			hw  = z * math.Sqrt(eff*(1-eff)/nn)
		)
		return clamp01(eff - hw), clamp01(eff + hw)

	case Normal:
		var (
			z   = zOf(level)
			eff = k / n
			hw  = z * math.Sqrt(eff*(1-eff)/n)
		)
		return clamp01(eff - hw), clamp01(eff + hw)

	default:
		panic(fmt.Errorf("hbook: invalid IntervalKind (%d)", int(kind)))
	}
}

// zOf returns the number of standard deviations that a two-sided interval of
// the given confidence level spans either side of a normal mean.
func zOf(level float64) float64 {
	return distuv.UnitNormal.Quantile(0.5 * (1 + level))
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}

// EffBin is one bin of an Efficiency.
type EffBin struct {
	XRange Range   // extent of the bin along x
	Pass   float64 // effective number of entries that passed
	Total  float64 // effective number of entries in total
	Eff    float64 // Pass/Total, or 0 for an empty bin
	ErrLo  float64 // distance down from Eff to the lower confidence limit
	ErrHi  float64 // distance up from Eff to the upper confidence limit
}

// XMid returns the centre of the bin along x.
func (b EffBin) XMid() float64 {
	return 0.5 * (b.XRange.Min + b.XRange.Max)
}

// EffLo returns the lower confidence limit on the efficiency.
func (b EffBin) EffLo() float64 { return b.Eff - b.ErrLo }

// EffHi returns the upper confidence limit on the efficiency.
func (b EffBin) EffHi() float64 { return b.Eff + b.ErrHi }

// Efficiency is the fraction of a sample that passed some selection, bin by
// bin, together with an uncertainty that respects the fact that the fraction
// is bounded by 0 and 1.
//
// It is what a ratio of two histograms should be whenever the numerator counts
// a subset of what the denominator counts -- a trigger efficiency, a
// reconstruction efficiency, a cut acceptance. DivideH1D is not that: it
// propagates the two errors in quadrature, which is right for independent
// measurements and wrong here, where an event in the numerator is also in the
// denominator. The difference shows up exactly where efficiencies are most
// interesting, at the turn-on and at the plateau.
type Efficiency struct {
	ann   Annotation
	bins  []EffBin
	kind  IntervalKind
	level float64
}

// effConfig holds the tunable parts of an Efficiency.
type effConfig struct {
	kind  IntervalKind
	level float64
}

// EffOption configures an Efficiency.
type EffOption func(*effConfig)

// WithEffInterval selects how the confidence interval is computed. It defaults
// to ClopperPearson.
func WithEffInterval(kind IntervalKind) EffOption {
	return func(cfg *effConfig) {
		cfg.kind = kind
	}
}

// WithEffLevel sets the confidence level the interval is quoted at. It
// defaults to DefaultEffLevel, the one-sigma equivalent.
func WithEffLevel(level float64) EffOption {
	return func(cfg *effConfig) {
		cfg.level = level
	}
}

// NewEfficiency computes the efficiency of pass with respect to total, bin by
// bin.
//
// The two histograms have to share a binning, and every bin of pass has to
// hold no more than the corresponding bin of total: pass counts a subset of
// what total counts, and a numerator that escapes its denominator is a sign
// the two were filled from different samples rather than something to be
// silently clipped.
//
// Weighted fills are handled through effective entries, (sum w)^2/(sum w^2),
// which is the number of unweighted events that would carry the same
// statistical weight. For unweighted fills that is the entry count itself and
// the intervals are exact.
func NewEfficiency(pass, total *H1D, opts ...EffOption) (*Efficiency, error) {
	if pass == nil || total == nil {
		return nil, fmt.Errorf("hbook: nil histogram")
	}

	cfg := effConfig{level: DefaultEffLevel}
	for _, opt := range opts {
		opt(&cfg)
	}
	switch {
	case cfg.level <= 0 || cfg.level >= 1:
		return nil, fmt.Errorf("hbook: confidence level %v out of range (0, 1)", cfg.level)
	case cfg.kind < ClopperPearson || cfg.kind > Normal:
		return nil, fmt.Errorf("hbook: invalid IntervalKind (%d)", int(cfg.kind))
	}

	var (
		nums = pass.Binning.Bins
		dens = total.Binning.Bins
	)
	if len(nums) != len(dens) {
		return nil, fmt.Errorf(
			"hbook: %q has %d bins, %q has %d: the two have to share a binning",
			pass.Name(), len(nums), total.Name(), len(dens),
		)
	}

	eff := &Efficiency{
		ann:   make(Annotation),
		bins:  make([]EffBin, len(nums)),
		kind:  cfg.kind,
		level: cfg.level,
	}

	for i := range nums {
		var (
			num = &nums[i]
			den = &dens[i]
		)
		if !fuzzyEq(num.XMin(), den.XMin()) || !fuzzyEq(num.XMax(), den.XMax()) {
			return nil, fmt.Errorf(
				"hbook: x binnings are not equivalent in %v / %v", pass.Name(), total.Name(),
			)
		}
		switch {
		case num.SumW() < 0 || den.SumW() < 0:
			return nil, fmt.Errorf(
				"hbook: bin %d of %v / %v holds a negative sum of weights (%v / %v): it is not a count of anything",
				i, pass.Name(), total.Name(), num.SumW(), den.SumW(),
			)
		case num.SumW() > den.SumW() && !fuzzyEq(num.SumW(), den.SumW()):
			return nil, fmt.Errorf(
				"hbook: bin %d of %v / %v passes more than it counts (%v > %v): the numerator has to be a subset of the denominator",
				i, pass.Name(), total.Name(), num.SumW(), den.SumW(),
			)
		}

		b := EffBin{XRange: den.XEdges()}
		// effective entries: the number of unweighted events carrying the same
		// statistical weight as the weighted ones actually filled.
		if w2 := den.SumW2(); w2 > 0 {
			b.Total = den.SumW() * den.SumW() / w2
		}
		if b.Total > 0 {
			b.Eff = clamp01(num.SumW() / den.SumW())
			b.Pass = b.Eff * b.Total
			lo, hi := BinomialInterval(cfg.kind, b.Pass, b.Total, cfg.level)
			b.ErrLo = math.Max(0, b.Eff-lo)
			b.ErrHi = math.Max(0, hi-b.Eff)
		}
		eff.bins[i] = b
	}

	return eff, nil
}

// Annotation returns the annotations attached to this efficiency.
func (eff *Efficiency) Annotation() Annotation {
	return eff.ann
}

// Name returns the name of this efficiency.
func (eff *Efficiency) Name() string {
	v, ok := eff.ann["name"]
	if !ok {
		return ""
	}
	n, ok := v.(string)
	if !ok {
		return ""
	}
	return n
}

// Rank returns the number of dimensions of this efficiency.
func (*Efficiency) Rank() int { return 1 }

// IntervalKind returns how the confidence intervals were computed.
func (eff *Efficiency) IntervalKind() IntervalKind { return eff.kind }

// Level returns the confidence level the intervals are quoted at.
func (eff *Efficiency) Level() float64 { return eff.level }

// Len returns the number of bins, implementing the
// gonum/plot/plotter.XYer interface.
func (eff *Efficiency) Len() int { return len(eff.bins) }

// Bin returns the i-th bin.
func (eff *Efficiency) Bin(i int) EffBin { return eff.bins[i] }

// Bins returns the bins of this efficiency.
func (eff *Efficiency) Bins() []EffBin { return eff.bins }

// Value returns the efficiency of the i-th bin, implementing the
// gonum/plot/plotter.Valuer interface.
func (eff *Efficiency) Value(i int) float64 { return eff.bins[i].Eff }

// XY returns the centre of the i-th bin and its efficiency, implementing the
// gonum/plot/plotter.XYer interface.
func (eff *Efficiency) XY(i int) (x, y float64) {
	b := eff.bins[i]
	return b.XMid(), b.Eff
}

// XError returns the extent of the i-th bin either side of its centre,
// implementing the gonum/plot/plotter.XErrorer interface.
func (eff *Efficiency) XError(i int) (down, up float64) {
	b := eff.bins[i]
	return b.XMid() - b.XRange.Min, b.XRange.Max - b.XMid()
}

// YError returns the confidence interval on the i-th bin, implementing the
// gonum/plot/plotter.YErrorer interface.
func (eff *Efficiency) YError(i int) (down, up float64) {
	b := eff.bins[i]
	return b.ErrLo, b.ErrHi
}

// DataRange returns the minimum and maximum x and y values, implementing the
// gonum/plot.DataRanger interface.
func (eff *Efficiency) DataRange() (xmin, xmax, ymin, ymax float64) {
	xmin, ymin = math.Inf(+1), math.Inf(+1)
	xmax, ymax = math.Inf(-1), math.Inf(-1)
	for _, b := range eff.bins {
		xmin = math.Min(xmin, b.XRange.Min)
		xmax = math.Max(xmax, b.XRange.Max)
		ymin = math.Min(ymin, b.EffLo())
		ymax = math.Max(ymax, b.EffHi())
	}
	return xmin, xmax, ymin, ymax
}

// S2D returns the efficiency as a 2-dim scatter, with the bin extent as the
// error along x and the confidence interval as the error along y. It is the
// form the plotting packages take.
func (eff *Efficiency) S2D() *S2D {
	s := NewS2D()
	for _, b := range eff.bins {
		x := b.XMid()
		s.Fill(Point2D{
			X:    x,
			Y:    b.Eff,
			ErrX: Range{Min: x - b.XRange.Min, Max: b.XRange.Max - x},
			ErrY: Range{Min: b.ErrLo, Max: b.ErrHi},
		})
	}
	for k, v := range eff.ann {
		s.ann[k] = v
	}
	return s
}
