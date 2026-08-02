// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fastjet

import (
	"fmt"
	"math"
	"slices"
	"sort"
)

// JetMedianBackgroundEstimator measures the diffuse background sitting under
// the jets of an event: pile-up, underlying event, and anything else spread
// more or less uniformly over the detector.
//
// The estimate is the median of pt/area taken over the jets of the event. A
// median rather than a mean because a handful of hard jets would drag a mean
// upwards, whereas they leave the middle of the distribution alone; that is
// what makes the same number usable for an event with two hard jets in it and
// for one with none.
//
// The result, Rho, is the background transverse momentum per unit
// rapidity-azimuth area. Multiplying it by a jet's area gives the amount of
// that jet's momentum that came from the background rather than from the hard
// scatter -- which is what Subtract removes.
type JetMedianBackgroundEstimator struct {
	csa *ClusterSequenceArea

	rho      float64
	sigma    float64
	njets    int
	meanArea float64
}

// bkgConfig holds the tunable parts of a JetMedianBackgroundEstimator.
type bkgConfig struct {
	rapMax   float64
	nExclude int
	quantile float64
}

// BkgOption configures a JetMedianBackgroundEstimator.
type BkgOption func(*bkgConfig)

// WithBkgRapMax restricts the estimate to jets with |rapidity| below v.
//
// It defaults to the ghosts' rapidity reach less one jet radius, which is the
// largest range over which every jet still has its whole area covered by
// ghosts. Narrowing it further is how a measurement made in a central region
// gets a background estimated in that same region.
func WithBkgRapMax(v float64) BkgOption {
	return func(cfg *bkgConfig) {
		cfg.rapMax = v
	}
}

// WithBkgExcludeHardest leaves the n hardest jets out of the estimate.
//
// It defaults to 0. In an event with a hard scatter in it, 2 is the usual
// choice: the two jets recoiling against each other are not background, and
// with only a few jets in the event they can pull the median with them.
func WithBkgExcludeHardest(n int) BkgOption {
	return func(cfg *bkgConfig) {
		cfg.nExclude = n
	}
}

// WithBkgSigmaQuantile sets the lower quantile of the pt/area distribution
// that Sigma is measured against. It defaults to 0.1587, the quantile one
// standard deviation below the median of a Gaussian.
func WithBkgSigmaQuantile(q float64) BkgOption {
	return func(cfg *bkgConfig) {
		cfg.quantile = q
	}
}

// NewJetMedianBackgroundEstimator estimates the background of the event
// clustered by csa.
//
// csa has to have been built with an ActiveAreaExplicitGhosts area: the empty
// regions of the event are exactly the ones that carry no background, and
// dropping them -- which a plain ActiveArea does -- would bias the estimate
// upwards. Cluster with a kt or Cambridge/Aachen definition rather than
// anti-kt, whose jets are circular by construction and so do not adapt their
// area to the background they sit in.
func NewJetMedianBackgroundEstimator(csa *ClusterSequenceArea, opts ...BkgOption) (*JetMedianBackgroundEstimator, error) {
	if csa == nil {
		return nil, fmt.Errorf("fastjet: nil ClusterSequenceArea")
	}
	if csa.area.typ != ActiveAreaExplicitGhosts {
		return nil, fmt.Errorf(
			"fastjet: background estimation needs %v, got %v: without the pure-ghost jets the empty regions of the event are missing and the background comes out too high",
			ActiveAreaExplicitGhosts, csa.area.typ,
		)
	}

	cfg := bkgConfig{
		rapMax:   math.Max(0, csa.spec.GhostMaxRap-csa.def.R()),
		quantile: 0.1587,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	switch {
	case cfg.nExclude < 0:
		return nil, fmt.Errorf("fastjet: negative number of jets to exclude (%d)", cfg.nExclude)
	case cfg.quantile <= 0 || cfg.quantile >= 0.5:
		return nil, fmt.Errorf("fastjet: sigma quantile %v out of range (0, 0.5)", cfg.quantile)
	}

	jets, err := csa.InclusiveJets(0)
	if err != nil {
		return nil, err
	}

	type entry struct {
		pt   float64
		area float64
	}
	var sel []entry
	for i := range jets {
		jet := &jets[i]
		if math.Abs(jet.Rapidity()) > cfg.rapMax {
			continue
		}
		area := csa.Area(jet)
		if area <= 0 {
			continue
		}
		sel = append(sel, entry{pt: jet.Pt(), area: area})
	}

	// The hardest jets are the ones that are not background, so they go
	// before anything is measured.
	sort.Slice(sel, func(i, j int) bool { return sel[i].pt > sel[j].pt })
	if cfg.nExclude < len(sel) {
		sel = sel[cfg.nExclude:]
	} else {
		sel = nil
	}

	if len(sel) == 0 {
		return nil, fmt.Errorf(
			"fastjet: no jets left to estimate the background from (|y| < %v, %d hardest excluded)",
			cfg.rapMax, cfg.nExclude,
		)
	}

	var (
		rhos     = make([]float64, len(sel))
		sumArea  float64
		estimate = &JetMedianBackgroundEstimator{csa: csa, njets: len(sel)}
	)
	for i, e := range sel {
		rhos[i] = e.pt / e.area
		sumArea += e.area
	}
	slices.Sort(rhos)

	estimate.meanArea = sumArea / float64(len(sel))
	estimate.rho = quantileOf(rhos, 0.5)

	// The width of the background is read off the same distribution: how far
	// below the median the one-sigma quantile sits, rescaled from a per-area
	// density to the fluctuation a jet of average area would see.
	sigma := (estimate.rho - quantileOf(rhos, cfg.quantile)) * math.Sqrt(estimate.meanArea)
	estimate.sigma = math.Max(0, sigma)

	return estimate, nil
}

// Rho returns the background transverse momentum per unit area.
func (bkg *JetMedianBackgroundEstimator) Rho() float64 {
	return bkg.rho
}

// Sigma returns the point-to-point fluctuation of the background, in units of
// transverse momentum per square root of area. A jet of area A sees a
// background fluctuation of Sigma*sqrt(A), which is the irreducible resolution
// cost of sitting in the background -- subtracting Rho*A removes the average,
// not the noise.
func (bkg *JetMedianBackgroundEstimator) Sigma() float64 {
	return bkg.sigma
}

// NumJets returns how many jets the estimate was taken over.
func (bkg *JetMedianBackgroundEstimator) NumJets() int {
	return bkg.njets
}

// MeanArea returns the mean area of those jets.
func (bkg *JetMedianBackgroundEstimator) MeanArea() float64 {
	return bkg.meanArea
}

// Subtract removes the background from jet, using the jet's four-vector area
// so that the subtraction points the same way the jet's area does.
//
// A jet that was nothing but background subtracts to nothing: rather than
// return a jet with negative transverse momentum, which no downstream code
// expects, Subtract returns a null jet in that case.
func (bkg *JetMedianBackgroundEstimator) Subtract(jet *Jet) Jet {
	if jet == nil {
		return NewJet(0, 0, 0, 0)
	}

	area := bkg.csa.Area4Vector(jet)
	sub := NewJet(
		jet.Px()-bkg.rho*area.Px(),
		jet.Py()-bkg.rho*area.Py(),
		jet.Pz()-bkg.rho*area.Pz(),
		jet.E()-bkg.rho*area.E(),
	)
	if sub.E() < 0 || sub.Pt2() > jet.Pt2() {
		return NewJet(0, 0, 0, 0)
	}
	sub.UserInfo = jet.UserInfo
	return sub
}

// quantileOf returns the q-quantile of an ascending slice, interpolating
// between the two samples it falls between.
func quantileOf(sorted []float64, q float64) float64 {
	switch n := len(sorted); {
	case n == 0:
		return 0
	case n == 1:
		return sorted[0]
	default:
		var (
			pos = q * float64(n-1)
			lo  = int(math.Floor(pos))
			hi  = lo + 1
		)
		if hi >= n {
			return sorted[n-1]
		}
		w := pos - float64(lo)
		return sorted[lo]*(1-w) + sorted[hi]*w
	}
}
