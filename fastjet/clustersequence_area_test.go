// Copyright ©2017 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fastjet_test

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"sort"
	"testing"

	"go-hep.org/x/hep/fastjet"
)

// testGhosts is a ghost specification coarse enough to keep the tests quick
// while still resolving a jet of radius 0.4 into a hundred-odd ghosts.
var testGhosts = fastjet.GhostedAreaSpec{GhostMaxRap: 2, GhostArea: 0.01}

// TestClusterSequenceArea compares against areas computed by C++ FastJet for
// the same event, with the same jet and area definitions.
//
// The jet kinematics have to agree to the precision the reference file was
// written with -- the ghosts are far too soft to move a jet. The areas cannot:
// they are read off a random ghost ensemble, and go-hep's ghosts are not
// FastJet's. What they must do is agree within the scatter that ensemble
// carries, and show no bias when averaged over the jets of the event.
func TestClusterSequenceArea(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		alg  fastjet.JetAlgorithm
		// tol is the per-jet relative tolerance on the area. Anti-kt jets
		// are circular whatever the ghosts do, so their areas barely move.
		// kt jets take whatever shape the soft ghosts around them suggest,
		// and their areas move by tens of percent from one ensemble to the
		// next -- which is what AreaErr exists to report.
		tol float64
	}{
		{name: "area_ghost_active_kt_r1.0_escheme_best", alg: fastjet.KtAlgorithm, tol: 0.25},
		{name: "area_ghost_active_antikt_r1.0_escheme_best", alg: fastjet.AntiKtAlgorithm, tol: 0.05},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if testing.Short() {
				t.Skip("short mode: the reference event carries 7497 ghosts")
			}

			particles, err := loadParticles("testdata/single-pp-event.dat")
			if err != nil {
				t.Fatal(err)
			}

			def := fastjet.NewJetDefinition(tc.alg, 1.0, fastjet.EScheme, fastjet.BestStrategy)
			csa, err := fastjet.NewClusterSequenceArea(particles, def, fastjet.AreaDefinition{})
			if err != nil {
				t.Fatalf("could not cluster with areas: %+v", err)
			}

			jets, err := csa.InclusiveJets(5)
			if err != nil {
				t.Fatalf("could not get inclusive jets: %+v", err)
			}
			sort.Sort(fastjet.ByPt(jets))

			want, err := loadRefAreas("testdata/" + tc.name + ".ref")
			if err != nil {
				t.Fatalf("could not read reference file: %+v", err)
			}

			if got := len(jets); got != len(want) {
				t.Fatalf("got %d jets, want %d", got, len(want))
			}

			var sumRatio float64
			for i := range jets {
				var (
					jet = &jets[i]
					ref = want[i]
					got = []float64{jet.Rapidity(), angle0to2Pi(jet.Phi()), jet.Pt()}
				)
				for j, name := range []string{"rapidity", "phi", "pt"} {
					if math.Abs(got[j]-ref[j]) > 1e-3 {
						t.Errorf("jet %d: got %s=%v, want %v", i, name, got[j], ref[j])
					}
				}

				if ref[3] <= 0 {
					t.Fatalf("jet %d: reference area is %v", i, ref[3])
				}
				area := csa.Area(jet)
				ratio := area / ref[3]
				sumRatio += ratio
				if math.Abs(ratio-1) > tc.tol {
					t.Errorf("jet %d: got area=%v, want %v (ratio %v, tolerance %v)",
						i, area, ref[3], ratio, tc.tol,
					)
				}

				// a single ghost ensemble says nothing about its own scatter
				if err := csa.AreaErr(jet); err != 0 {
					t.Errorf("jet %d: got area error %v from a single ghost ensemble, want 0", i, err)
				}
			}

			// individual kt areas wander; their average must not.
			if mean := sumRatio / float64(len(jets)); math.Abs(mean-1) > 0.05 {
				t.Errorf("mean area ratio is %v, want 1 to within 5%%", mean)
			}
		})
	}
}

// TestClusterSequenceAreaOfHardJet measures the area a single hard particle
// carves out of an otherwise empty event and compares it to the catchment
// areas of Cacciari, Salam and Soyez (arXiv:0802.1188), in units of pi*R^2:
//
//	anti-kt          1.0004 +- 0.0028
//	kt               0.812  +- 0.277
//	Cambridge/Aachen 0.814  +- 0.261
//
// Anti-kt's jets are circles of radius R around the hard particle whatever the
// ghosts do, which is exactly why the algorithm is the one used to define jets
// experimentally. kt and Cambridge/Aachen instead let the soft ghosts decide
// where the jet ends, so their areas come out about a fifth smaller and swing
// by a third of themselves from one ghost ensemble to the next -- hence the
// repeated ensembles here, without which there is nothing to compare against.
func TestClusterSequenceAreaOfHardJet(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		alg    fastjet.JetAlgorithm
		want   float64 // catchment area, in units of pi*R^2
		tol    float64
		repeat int
	}{
		{alg: fastjet.AntiKtAlgorithm, want: 1.0, tol: 0.05, repeat: 1},
		{alg: fastjet.KtAlgorithm, want: 0.81, tol: 0.15, repeat: 10},
		{alg: fastjet.CambridgeAlgorithm, want: 0.81, tol: 0.15, repeat: 10},
	} {
		for _, r := range []float64{0.4, 0.7, 1.0} {
			name := fmt.Sprintf("%v_r%.1f", tc.alg, r)
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				var (
					particles = []fastjet.Jet{fastjet.NewJet(100, 0, 0, 100)}
					def       = fastjet.NewJetDefinition(tc.alg, r, fastjet.EScheme, fastjet.BestStrategy)
					spec      = testGhosts
				)
				spec.Repeat = tc.repeat

				csa, err := fastjet.NewClusterSequenceArea(
					particles, def,
					fastjet.NewAreaDefinition(fastjet.ActiveArea, spec),
				)
				if err != nil {
					t.Fatalf("could not cluster with areas: %+v", err)
				}

				jets, err := csa.InclusiveJets(1)
				if err != nil {
					t.Fatalf("could not get inclusive jets: %+v", err)
				}
				if len(jets) != 1 {
					t.Fatalf("got %d jets, want 1", len(jets))
				}

				var (
					area  = csa.Area(&jets[0])
					ratio = area / (math.Pi * r * r)
				)
				if math.Abs(ratio-tc.want) > tc.tol {
					t.Errorf("got area=%v, i.e. %v*pi*R^2; want %v to within %v",
						area, ratio, tc.want, tc.tol,
					)
				}

				// the four-vector area points along the jet, and carries at
				// least as much energy as the scalar area since the ghosts
				// spread out in rapidity.
				a4 := csa.Area4Vector(&jets[0])
				if a4.E() < area {
					t.Errorf("four-vector area E=%v is below the scalar area %v", a4.E(), area)
				}
				if a4.Px() <= 0 {
					t.Errorf("four-vector area does not point along the jet: px=%v", a4.Px())
				}
			})
		}
	}
}

// TestClusterSequenceAreaGhostHandling checks that the ghosts stay out of the
// results unless they were explicitly asked for.
func TestClusterSequenceAreaGhostHandling(t *testing.T) {
	t.Parallel()

	var (
		particles = []fastjet.Jet{
			fastjet.NewJet(100, 0, 0, 100),
			fastjet.NewJet(-80, 10, 0, 81),
		}
		def = fastjet.NewJetDefinition(fastjet.KtAlgorithm, 0.6, fastjet.EScheme, fastjet.BestStrategy)
	)

	active, err := fastjet.NewClusterSequenceArea(
		particles, def, fastjet.NewAreaDefinition(fastjet.ActiveArea, testGhosts),
	)
	if err != nil {
		t.Fatalf("could not cluster with areas: %+v", err)
	}

	explicit, err := fastjet.NewClusterSequenceArea(
		particles, def, fastjet.NewAreaDefinition(fastjet.ActiveAreaExplicitGhosts, testGhosts),
	)
	if err != nil {
		t.Fatalf("could not cluster with explicit ghosts: %+v", err)
	}

	hard, err := active.InclusiveJets(0)
	if err != nil {
		t.Fatalf("could not get inclusive jets: %+v", err)
	}
	if len(hard) != len(particles) {
		t.Fatalf("got %d jets, want %d: the pure-ghost jets were not dropped", len(hard), len(particles))
	}
	for i := range hard {
		if active.IsPureGhost(&hard[i]) {
			t.Errorf("jet %d survived an active-area selection but is pure ghost", i)
		}
		cts, err := active.Constituents(&hard[i])
		if err != nil {
			t.Fatalf("could not get constituents: %+v", err)
		}
		if len(cts) != 1 {
			t.Errorf("jet %d: got %d real constituents, want 1", i, len(cts))
		}
	}

	all, err := explicit.InclusiveJets(0)
	if err != nil {
		t.Fatalf("could not get inclusive jets: %+v", err)
	}
	if len(all) <= len(hard) {
		t.Fatalf("got %d jets with explicit ghosts, want more than the %d hard ones", len(all), len(hard))
	}

	var ghosts int
	for i := range all {
		if explicit.IsPureGhost(&all[i]) {
			ghosts++
		}
	}
	if want := len(all) - len(hard); ghosts != want {
		t.Errorf("got %d pure-ghost jets out of %d, want %d", ghosts, len(all), want)
	}

	// the ghosts a jet caught are the ghosts it caught, whether or not the
	// empty regions of the event were reported alongside it.
	sort.Sort(fastjet.ByPt(hard))
	sort.Sort(fastjet.ByPt(all))
	for i := range hard {
		got, want := explicit.Area(&all[i]), active.Area(&hard[i])
		if got != want {
			t.Errorf("jet %d: got area %v with explicit ghosts, want %v", i, got, want)
		}
	}
}

// TestClusterSequenceAreaRepeat checks that repeating the ghost ensemble both
// gives the areas an uncertainty and leaves their central value alone.
func TestClusterSequenceAreaRepeat(t *testing.T) {
	t.Parallel()

	var (
		particles = []fastjet.Jet{fastjet.NewJet(100, 0, 0, 100)}
		def       = fastjet.NewJetDefinition(fastjet.AntiKtAlgorithm, 0.7, fastjet.EScheme, fastjet.BestStrategy)
		spec      = testGhosts
	)
	spec.Repeat = 5

	csa, err := fastjet.NewClusterSequenceArea(
		particles, def, fastjet.NewAreaDefinition(fastjet.ActiveArea, spec),
	)
	if err != nil {
		t.Fatalf("could not cluster with areas: %+v", err)
	}

	jets, err := csa.InclusiveJets(1)
	if err != nil {
		t.Fatalf("could not get inclusive jets: %+v", err)
	}
	if len(jets) != 1 {
		t.Fatalf("got %d jets, want 1", len(jets))
	}

	var (
		area = csa.Area(&jets[0])
		aerr = csa.AreaErr(&jets[0])
		want = math.Pi * 0.7 * 0.7
	)
	if math.Abs(area/want-1) > 0.06 {
		t.Errorf("got area=%v, want pi*R^2=%v", area, want)
	}
	switch {
	case aerr <= 0:
		t.Errorf("got area error %v from 5 ghost ensembles, want a positive one", aerr)
	case aerr > 0.5*want:
		t.Errorf("got area error %v, which is not a small fraction of the area %v", aerr, want)
	}
}

// TestClusterSequenceAreaIsReproducible checks that the ghost seed does what
// it says: the same seed gives the same areas, a different one does not, and
// the two agree to within the ghost scatter.
func TestClusterSequenceAreaIsReproducible(t *testing.T) {
	t.Parallel()

	particles := []fastjet.Jet{fastjet.NewJet(100, 0, 0, 100)}
	def := fastjet.NewJetDefinition(fastjet.KtAlgorithm, 0.7, fastjet.EScheme, fastjet.BestStrategy)

	areaWithSeed := func(seed uint64) float64 {
		t.Helper()

		spec := testGhosts
		spec.Seed = seed
		csa, err := fastjet.NewClusterSequenceArea(
			particles, def, fastjet.NewAreaDefinition(fastjet.ActiveArea, spec),
		)
		if err != nil {
			t.Fatalf("could not cluster with areas: %+v", err)
		}
		jets, err := csa.InclusiveJets(1)
		if err != nil {
			t.Fatalf("could not get inclusive jets: %+v", err)
		}
		if len(jets) != 1 {
			t.Fatalf("got %d jets, want 1", len(jets))
		}
		return csa.Area(&jets[0])
	}

	var (
		a1 = areaWithSeed(1234)
		a2 = areaWithSeed(1234)
		a3 = areaWithSeed(4321)
	)
	if a1 != a2 {
		t.Errorf("the same seed gave two different areas: %v and %v", a1, a2)
	}
	if a1 == a3 {
		t.Errorf("two different seeds gave the same area %v: the ghosts are not being reseeded", a1)
	}
	if math.Abs(a1/a3-1) > 0.1 {
		t.Errorf("areas from two ghost ensembles differ by more than the ghost scatter: %v vs %v", a1, a3)
	}
}

// TestClusterSequenceAreaExclusive checks the exclusive entry points, which
// have to drop the pure-ghost jets just as the inclusive one does.
func TestClusterSequenceAreaExclusive(t *testing.T) {
	t.Parallel()

	var (
		particles = []fastjet.Jet{
			fastjet.NewJet(100, 0, 0, 100),
			fastjet.NewJet(-80, 10, 0, 81),
		}
		def = fastjet.NewJetDefinition(fastjet.KtAlgorithm, 0.6, fastjet.EScheme, fastjet.BestStrategy)
	)

	csa, err := fastjet.NewClusterSequenceArea(
		particles, def, fastjet.NewAreaDefinition(fastjet.ActiveArea, testGhosts),
	)
	if err != nil {
		t.Fatalf("could not cluster with areas: %+v", err)
	}

	if got := csa.NumExclusiveJets(1e-8); got <= 0 {
		t.Fatalf("got %d exclusive jets, want a positive number", got)
	}

	jets, err := csa.ExclusiveJetsUpTo(csa.NumExclusiveJets(0))
	if err != nil {
		t.Fatalf("could not get exclusive jets: %+v", err)
	}
	for i := range jets {
		if csa.IsPureGhost(&jets[i]) {
			t.Errorf("jet %d is pure ghost but survived an active-area selection", i)
		}
	}

	if _, err := csa.ExclusiveJets(1e30); err != nil {
		t.Fatalf("could not get exclusive jets: %+v", err)
	}
	if _, err := csa.ExclusiveJetsUpTo(1 << 30); err == nil {
		t.Fatalf("asking for more exclusive jets than there are particles should fail")
	}
}

// TestClusterSequenceAreaOfForeignJet checks that a jet from somewhere else
// gets no area rather than an index panic.
func TestClusterSequenceAreaOfForeignJet(t *testing.T) {
	t.Parallel()

	var (
		particles = []fastjet.Jet{fastjet.NewJet(100, 0, 0, 100)}
		def       = fastjet.NewJetDefinition(fastjet.KtAlgorithm, 0.7, fastjet.EScheme, fastjet.BestStrategy)
	)
	csa, err := fastjet.NewClusterSequenceArea(
		particles, def, fastjet.NewAreaDefinition(fastjet.ActiveArea, testGhosts),
	)
	if err != nil {
		t.Fatalf("could not cluster with areas: %+v", err)
	}

	foreign := fastjet.NewJet(10, 10, 0, 20)
	if got := csa.Area(&foreign); got != 0 {
		t.Errorf("got area %v for a jet this sequence never clustered, want 0", got)
	}
	if got := csa.AreaErr(&foreign); got != 0 {
		t.Errorf("got area error %v for a jet this sequence never clustered, want 0", got)
	}
	if got := csa.Area4Vector(&foreign); got.E() != 0 {
		t.Errorf("got four-vector area %v for a jet this sequence never clustered, want a null one", got)
	}
	if csa.IsPureGhost(&foreign) {
		t.Errorf("a jet this sequence never clustered was reported as pure ghost")
	}
	if got := csa.Area(nil); got != 0 {
		t.Errorf("got area %v for a nil jet, want 0", got)
	}
}

// TestClusterSequenceAreaAccessors checks the bookkeeping a caller needs to
// know what the ghosts actually did.
func TestClusterSequenceAreaAccessors(t *testing.T) {
	t.Parallel()

	var (
		particles = []fastjet.Jet{fastjet.NewJet(100, 0, 0, 100)}
		def       = fastjet.NewJetDefinition(fastjet.KtAlgorithm, 0.7, fastjet.EScheme, fastjet.N3DumbStrategy)
		area      = fastjet.NewAreaDefinition(fastjet.ActiveArea, testGhosts)
	)
	csa, err := fastjet.NewClusterSequenceArea(particles, def, area)
	if err != nil {
		t.Fatalf("could not cluster with areas: %+v", err)
	}

	if got, want := csa.JetDefinition(), def; got.Algorithm() != want.Algorithm() || got.R() != want.R() {
		t.Errorf("got jet definition %v, want %v", got.Description(), want.Description())
	}
	if got, want := csa.AreaDefinition().Type(), fastjet.ActiveArea; got != want {
		t.Errorf("got area type %v, want %v", got, want)
	}
	if got := csa.ClusterSequence(); got == nil {
		t.Errorf("got a nil cluster sequence")
	}

	// the ghost grid has to fit a whole number of ghosts around the azimuth,
	// so the area a ghost stands for is close to, not equal to, the request.
	if got, want := csa.GhostArea(), testGhosts.GhostArea; math.Abs(got/want-1) > 0.02 {
		t.Errorf("got ghost area %v, want %v to within 2%%", got, want)
	}
	if got := csa.NumGhosts(); got < 1000 {
		t.Errorf("got %d ghosts, want the several thousand the spec asks for", got)
	}
	if got := csa.ClusterSequence().InclusiveJets; got == nil {
		t.Errorf("the underlying cluster sequence is unusable")
	}
}

// TestClusterSequenceAreaErrors checks that the combinations that cannot be
// measured say so up front rather than returning a wrong number.
func TestClusterSequenceAreaErrors(t *testing.T) {
	t.Parallel()

	var (
		particles = []fastjet.Jet{fastjet.NewJet(100, 0, 0, 100)}
		kt        = fastjet.NewJetDefinition(fastjet.KtAlgorithm, 0.7, fastjet.EScheme, fastjet.BestStrategy)
	)

	for _, tc := range []struct {
		name string
		def  fastjet.JetDefinition
		area fastjet.AreaDefinition
		want string
	}{
		{
			name: "passive-area",
			def:  kt,
			area: fastjet.NewAreaDefinition(fastjet.PassiveArea, testGhosts),
			want: "fastjet: PassiveArea is not implemented",
		},
		{
			name: "voronoi-area",
			def:  kt,
			area: fastjet.NewAreaDefinition(fastjet.VoronoiArea, testGhosts),
			want: "fastjet: VoronoiArea is not implemented",
		},
		{
			name: "ee-algorithm",
			def:  fastjet.NewJetDefinition(fastjet.EeKtAlgorithm, 0.7, fastjet.EScheme, fastjet.BestStrategy),
			area: fastjet.NewAreaDefinition(fastjet.ActiveArea, testGhosts),
			want: "fastjet: ee-kt does not measure distances in rapidity-azimuth, so it has no area there",
		},
		{
			name: "negative-ghost-area",
			def:  kt,
			area: fastjet.NewAreaDefinition(fastjet.ActiveArea, fastjet.GhostedAreaSpec{GhostArea: -1}),
			want: "fastjet: negative GhostArea (-1)",
		},
		{
			name: "negative-repeat",
			def:  kt,
			area: fastjet.NewAreaDefinition(fastjet.ActiveArea, fastjet.GhostedAreaSpec{Repeat: -2}),
			want: "fastjet: negative Repeat (-2)",
		},
		{
			name: "negative-max-rap",
			def:  kt,
			area: fastjet.NewAreaDefinition(fastjet.ActiveArea, fastjet.GhostedAreaSpec{GhostMaxRap: -3}),
			want: "fastjet: negative GhostMaxRap (-3)",
		},
		{
			name: "negative-ghost-pt",
			def:  kt,
			area: fastjet.NewAreaDefinition(fastjet.ActiveArea, fastjet.GhostedAreaSpec{MeanGhostPt: -1e-99}),
			want: "fastjet: negative MeanGhostPt (-1e-99)",
		},
		{
			name: "negative-pt-scatter",
			def:  kt,
			area: fastjet.NewAreaDefinition(fastjet.ActiveArea, fastjet.GhostedAreaSpec{PtScatter: -0.5}),
			want: "fastjet: negative PtScatter (-0.5)",
		},
		{
			name: "negative-grid-scatter",
			def:  kt,
			area: fastjet.NewAreaDefinition(fastjet.ActiveArea, fastjet.GhostedAreaSpec{GridScatter: -0.5}),
			want: "fastjet: negative GridScatter (-0.5)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := fastjet.NewClusterSequenceArea(particles, tc.def, tc.area)
			switch {
			case err == nil:
				t.Fatalf("expected an error")
			case err.Error() != tc.want:
				t.Fatalf("got error %q, want %q", err.Error(), tc.want)
			}
		})
	}
}

func loadRefAreas(name string) ([][5]float64, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var refs [][5]float64
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		var i int
		var ref [5]float64
		_, err = fmt.Sscanf(scan.Text(), "%5d %f %f %f %f +- %f", &i, &ref[0], &ref[1], &ref[2], &ref[3], &ref[4])
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	err = scan.Err()
	if err != nil {
		return nil, err
	}
	return refs, nil
}
