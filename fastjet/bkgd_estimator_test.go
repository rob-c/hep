// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fastjet_test

import (
	"math"
	"math/rand/v2"
	"sort"
	"testing"

	"go-hep.org/x/hep/fastjet"
)

const (
	bkgRapMax = 2.0  // rapidity reach of the toy background
	bkgMeanPt = 0.6  // mean transverse momentum of a background particle
	bkgNPart  = 900  // number of background particles
	bkgSeed   = 0xb4 // fixed so the toy event is the same on every run
)

// softBackground throws a uniform, structureless soft background over
// |rapidity| < bkgRapMax, and returns it together with the transverse momentum
// density it carries per unit rapidity-azimuth area.
func softBackground(rnd *rand.Rand) ([]fastjet.Jet, float64) {
	var (
		parts = make([]fastjet.Jet, 0, bkgNPart)
		sumpt float64
	)
	for range bkgNPart {
		var (
			pt  = bkgMeanPt * (0.5 + rnd.Float64())
			rap = 2*bkgRapMax*rnd.Float64() - bkgRapMax
			phi = 2 * math.Pi * rnd.Float64()
		)
		sumpt += pt
		parts = append(parts, fastjet.NewJet(
			pt*math.Cos(phi), pt*math.Sin(phi),
			pt*math.Sinh(rap), pt*math.Cosh(rap),
		))
	}
	return parts, sumpt / (2 * bkgRapMax * 2 * math.Pi)
}

// bkgGhosts covers exactly the rapidity range the toy background occupies.
var bkgGhosts = fastjet.GhostedAreaSpec{GhostMaxRap: bkgRapMax, GhostArea: 0.02}

// TestJetMedianBackgroundEstimator checks that the estimator recovers a
// background density that was put in by hand.
func TestJetMedianBackgroundEstimator(t *testing.T) {
	t.Parallel()

	var (
		rnd        = rand.New(rand.NewPCG(bkgSeed, bkgSeed))
		parts, rho = softBackground(rnd)
		def        = fastjet.NewJetDefinition(fastjet.KtAlgorithm, 0.5, fastjet.EScheme, fastjet.BestStrategy)
		area       = fastjet.NewAreaDefinition(fastjet.ActiveAreaExplicitGhosts, bkgGhosts)
	)

	csa, err := fastjet.NewClusterSequenceArea(parts, def, area)
	if err != nil {
		t.Fatalf("could not cluster with areas: %+v", err)
	}

	bkg, err := fastjet.NewJetMedianBackgroundEstimator(csa)
	if err != nil {
		t.Fatalf("could not estimate the background: %+v", err)
	}

	// The jet-median estimate sits a few percent low: a jet's own back
	// reaction takes a little of the background out of the region it is
	// measured over. That bias is a known, and small, property of the method.
	if got := bkg.Rho() / rho; math.Abs(got-1) > 0.1 {
		t.Errorf("got rho=%v, want %v (ratio %v)", bkg.Rho(), rho, got)
	}

	// the background is not uniform particle by particle, so it has a width
	if bkg.Sigma() <= 0 {
		t.Errorf("got sigma=%v, want a positive fluctuation", bkg.Sigma())
	}
	if bkg.Sigma() > bkg.Rho() {
		t.Errorf("got sigma=%v, which is larger than rho=%v", bkg.Sigma(), bkg.Rho())
	}

	if bkg.NumJets() < 10 {
		t.Errorf("got %d jets in the estimate, want enough of them for a median to mean anything", bkg.NumJets())
	}
	if want := math.Pi * 0.5 * 0.5; math.Abs(bkg.MeanArea()/want-1) > 0.5 {
		t.Errorf("got mean area=%v, want something like pi*R^2=%v", bkg.MeanArea(), want)
	}
}

// TestJetMedianBackgroundEstimatorRapRange checks that a background measured
// in a band away from the centre is the background of that band, and not the
// one of the event as a whole.
func TestJetMedianBackgroundEstimatorRapRange(t *testing.T) {
	t.Parallel()

	const (
		rapSplit = 1.0 // between the central band and the forward one
		nPart    = 600 // per band
	)

	var (
		rnd   = rand.New(rand.NewPCG(bkgSeed, bkgSeed))
		parts []fastjet.Jet
		add   = func(pt, rap, phi float64) {
			parts = append(parts, fastjet.NewJet(
				pt*math.Cos(phi), pt*math.Sin(phi),
				pt*math.Sinh(rap), pt*math.Cosh(rap),
			))
		}
	)
	// the two bands cover the same area, and the forward one carries twice
	// the transverse momentum.
	for range nPart {
		add(bkgMeanPt, rapSplit*(2*rnd.Float64()-1), 2*math.Pi*rnd.Float64())

		rap := rapSplit + (bkgRapMax-rapSplit)*rnd.Float64()
		if rnd.Float64() < 0.5 {
			rap = -rap
		}
		add(2*bkgMeanPt, rap, 2*math.Pi*rnd.Float64())
	}

	var (
		def  = fastjet.NewJetDefinition(fastjet.KtAlgorithm, 0.5, fastjet.EScheme, fastjet.BestStrategy)
		area = fastjet.NewAreaDefinition(fastjet.ActiveAreaExplicitGhosts, bkgGhosts)
	)

	csa, err := fastjet.NewClusterSequenceArea(parts, def, area)
	if err != nil {
		t.Fatalf("could not cluster with areas: %+v", err)
	}

	rhoOf := func(rapMin, rapMax float64) float64 {
		t.Helper()

		bkg, err := fastjet.NewJetMedianBackgroundEstimator(
			csa, fastjet.WithBkgRapRange(rapMin, rapMax),
		)
		if err != nil {
			t.Fatalf("could not estimate the background over (%v, %v): %+v", rapMin, rapMax, err)
		}
		return bkg.Rho()
	}

	var (
		central = rhoOf(0, rapSplit)
		forward = rhoOf(rapSplit, 1.5)
	)
	if central <= 0 {
		t.Fatalf("got a central rho of %v, want a positive background", central)
	}
	if got := forward / central; math.Abs(got-2) > 0.4 {
		t.Errorf("got a forward/central rho ratio of %v, want 2", got)
	}

	// the estimate over the whole region has to sit between the two.
	bkg, err := fastjet.NewJetMedianBackgroundEstimator(csa)
	if err != nil {
		t.Fatalf("could not estimate the background: %+v", err)
	}
	if got := bkg.Rho(); got < central || got > forward {
		t.Errorf("got rho=%v over the whole region, want it between %v and %v", got, central, forward)
	}
}

// TestJetMedianBackgroundEstimatorSubtract checks the whole chain the areas
// exist for: a hard jet buried in the background comes back out of it with
// close to the transverse momentum it went in with.
func TestJetMedianBackgroundEstimatorSubtract(t *testing.T) {
	t.Parallel()

	const hardPt = 200.0

	var (
		rnd        = rand.New(rand.NewPCG(bkgSeed, bkgSeed))
		parts, rho = softBackground(rnd)
		def        = fastjet.NewJetDefinition(fastjet.AntiKtAlgorithm, 0.5, fastjet.EScheme, fastjet.BestStrategy)
	)
	// a hard particle at the origin of the rapidity-azimuth plane
	parts = append(parts, fastjet.NewJet(hardPt, 0, 0, hardPt))

	csa, err := fastjet.NewClusterSequenceArea(
		parts, def, fastjet.NewAreaDefinition(fastjet.ActiveAreaExplicitGhosts, bkgGhosts),
	)
	if err != nil {
		t.Fatalf("could not cluster with areas: %+v", err)
	}

	bkg, err := fastjet.NewJetMedianBackgroundEstimator(csa, fastjet.WithBkgExcludeHardest(2))
	if err != nil {
		t.Fatalf("could not estimate the background: %+v", err)
	}
	if got := bkg.Rho() / rho; math.Abs(got-1) > 0.15 {
		t.Fatalf("got rho=%v, want %v (ratio %v)", bkg.Rho(), rho, got)
	}

	jets, err := csa.InclusiveJets(20)
	if err != nil {
		t.Fatalf("could not get inclusive jets: %+v", err)
	}
	sort.Sort(fastjet.ByPt(jets))
	if len(jets) == 0 {
		t.Fatalf("the hard jet went missing")
	}

	var (
		jet = &jets[0]
		raw = jet.Pt()
		sub = bkg.Subtract(jet)
	)
	if raw <= hardPt {
		t.Fatalf("the hard jet picked up no background at all: pt=%v", raw)
	}
	if got := sub.Pt(); math.Abs(got-hardPt) > 0.1*(raw-hardPt)+2 {
		t.Errorf("got subtracted pt=%v, want %v (unsubtracted %v)", got, hardPt, raw)
	}
	if sub.Pt() >= raw {
		t.Errorf("subtraction did not remove anything: %v -> %v", raw, sub.Pt())
	}

	// a jet that was nothing but background subtracts away to nothing
	soft, err := csa.InclusiveJets(0)
	if err != nil {
		t.Fatalf("could not get inclusive jets: %+v", err)
	}
	var nulled int
	for i := range soft {
		if !csa.IsPureGhost(&soft[i]) {
			continue
		}
		sub := bkg.Subtract(&soft[i])
		if sub.E() == 0 {
			nulled++
		}
	}
	if nulled == 0 {
		t.Errorf("no pure-ghost jet subtracted away to nothing")
	}

	null := bkg.Subtract(nil)
	if null.E() != 0 {
		t.Errorf("got %v for a nil jet, want a null one", null.PxPyPzE)
	}
}

// TestJetMedianBackgroundEstimatorOptions checks that the knobs do what they
// say and that the estimate is restricted to the region asked for.
func TestJetMedianBackgroundEstimatorOptions(t *testing.T) {
	t.Parallel()

	var (
		rnd      = rand.New(rand.NewPCG(bkgSeed, bkgSeed))
		parts, _ = softBackground(rnd)
		def      = fastjet.NewJetDefinition(fastjet.KtAlgorithm, 0.5, fastjet.EScheme, fastjet.BestStrategy)
	)
	csa, err := fastjet.NewClusterSequenceArea(
		parts, def, fastjet.NewAreaDefinition(fastjet.ActiveAreaExplicitGhosts, bkgGhosts),
	)
	if err != nil {
		t.Fatalf("could not cluster with areas: %+v", err)
	}

	all, err := fastjet.NewJetMedianBackgroundEstimator(csa)
	if err != nil {
		t.Fatalf("could not estimate the background: %+v", err)
	}

	central, err := fastjet.NewJetMedianBackgroundEstimator(csa, fastjet.WithBkgRapMax(0.5))
	if err != nil {
		t.Fatalf("could not estimate the background: %+v", err)
	}
	if central.NumJets() >= all.NumJets() {
		t.Errorf("narrowing the rapidity range did not drop any jets: %d vs %d",
			central.NumJets(), all.NumJets(),
		)
	}
	// the toy background is uniform, so restricting it must not move rho far
	if got := central.Rho() / all.Rho(); math.Abs(got-1) > 0.2 {
		t.Errorf("got rho=%v in the central region, want %v (ratio %v)", central.Rho(), all.Rho(), got)
	}

	excl, err := fastjet.NewJetMedianBackgroundEstimator(csa, fastjet.WithBkgExcludeHardest(4))
	if err != nil {
		t.Fatalf("could not estimate the background: %+v", err)
	}
	if got, want := excl.NumJets(), all.NumJets()-4; got != want {
		t.Errorf("got %d jets after excluding the 4 hardest, want %d", got, want)
	}
	// dropping the hardest jets can only lower the median
	if excl.Rho() > all.Rho() {
		t.Errorf("excluding the hardest jets raised rho: %v -> %v", all.Rho(), excl.Rho())
	}

	wide, err := fastjet.NewJetMedianBackgroundEstimator(csa, fastjet.WithBkgSigmaQuantile(0.4))
	if err != nil {
		t.Fatalf("could not estimate the background: %+v", err)
	}
	// a quantile closer to the median measures a narrower spread
	if wide.Sigma() >= all.Sigma() {
		t.Errorf("got sigma=%v at quantile 0.4, want less than %v at 0.1587", wide.Sigma(), all.Sigma())
	}
	if wide.Rho() != all.Rho() {
		t.Errorf("the sigma quantile moved rho: %v vs %v", wide.Rho(), all.Rho())
	}
}

// TestJetMedianBackgroundEstimatorErrors checks that the ways of asking for an
// estimate that cannot be trusted are refused rather than answered.
func TestJetMedianBackgroundEstimatorErrors(t *testing.T) {
	t.Parallel()

	var (
		particles = []fastjet.Jet{fastjet.NewJet(100, 0, 0, 100)}
		def       = fastjet.NewJetDefinition(fastjet.KtAlgorithm, 0.5, fastjet.EScheme, fastjet.BestStrategy)
	)

	active, err := fastjet.NewClusterSequenceArea(
		particles, def, fastjet.NewAreaDefinition(fastjet.ActiveArea, bkgGhosts),
	)
	if err != nil {
		t.Fatalf("could not cluster with areas: %+v", err)
	}

	explicit, err := fastjet.NewClusterSequenceArea(
		particles, def, fastjet.NewAreaDefinition(fastjet.ActiveAreaExplicitGhosts, bkgGhosts),
	)
	if err != nil {
		t.Fatalf("could not cluster with areas: %+v", err)
	}

	for _, tc := range []struct {
		name string
		csa  *fastjet.ClusterSequenceArea
		opts []fastjet.BkgOption
		want string
	}{
		{
			name: "nil-sequence",
			csa:  nil,
			want: "fastjet: nil ClusterSequenceArea",
		},
		{
			name: "no-explicit-ghosts",
			csa:  active,
			want: "fastjet: background estimation needs ActiveAreaExplicitGhosts, got ActiveArea: without the pure-ghost jets the empty regions of the event are missing and the background comes out too high",
		},
		{
			name: "negative-exclusion",
			csa:  explicit,
			opts: []fastjet.BkgOption{fastjet.WithBkgExcludeHardest(-1)},
			want: "fastjet: negative number of jets to exclude (-1)",
		},
		{
			name: "quantile-too-high",
			csa:  explicit,
			opts: []fastjet.BkgOption{fastjet.WithBkgSigmaQuantile(0.5)},
			want: "fastjet: sigma quantile 0.5 out of range (0, 0.5)",
		},
		{
			name: "quantile-too-low",
			csa:  explicit,
			opts: []fastjet.BkgOption{fastjet.WithBkgSigmaQuantile(0)},
			want: "fastjet: sigma quantile 0 out of range (0, 0.5)",
		},
		{
			name: "negative-rapidity-range",
			csa:  explicit,
			opts: []fastjet.BkgOption{fastjet.WithBkgRapRange(-1, 2)},
			want: "fastjet: negative rapidity range minimum (-1)",
		},
		{
			name: "empty-rapidity-range",
			csa:  explicit,
			opts: []fastjet.BkgOption{fastjet.WithBkgRapRange(2, 1)},
			want: "fastjet: empty rapidity range (2, 1)",
		},
		{
			name: "nothing-in-rapidity-range",
			csa:  explicit,
			opts: []fastjet.BkgOption{fastjet.WithBkgRapRange(10, 20)},
			want: "fastjet: no jets left to estimate the background from (10 < |y| < 20, 0 hardest excluded)",
		},
		{
			name: "nothing-to-estimate-from",
			csa:  explicit,
			opts: []fastjet.BkgOption{fastjet.WithBkgExcludeHardest(1 << 20)},
			want: "fastjet: no jets left to estimate the background from (|y| < 1.5, 1048576 hardest excluded)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := fastjet.NewJetMedianBackgroundEstimator(tc.csa, tc.opts...)
			switch {
			case err == nil:
				t.Fatalf("expected an error")
			case err.Error() != tc.want:
				t.Fatalf("got error %q, want %q", err.Error(), tc.want)
			}
		})
	}
}
