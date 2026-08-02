// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fastjet

import (
	"math"
	"testing"
)

func TestGhostedAreaSpecNormalize(t *testing.T) {
	t.Parallel()

	got, err := GhostedAreaSpec{}.normalize()
	if err != nil {
		t.Fatalf("could not normalize the zero spec: %+v", err)
	}

	want := GhostedAreaSpec{
		GhostMaxRap: defaultGhostMaxRap,
		GhostArea:   defaultGhostArea,
		Repeat:      1,
		GridScatter: defaultGridScatter,
		PtScatter:   defaultPtScatter,
		MeanGhostPt: defaultGhostPt,
		Seed:        defaultGhostSeed,
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	// a spec that sets everything is left alone
	full := GhostedAreaSpec{
		GhostMaxRap: 3,
		GhostArea:   0.05,
		Repeat:      4,
		GridScatter: 0.5,
		PtScatter:   0.2,
		MeanGhostPt: 1e-50,
		Seed:        42,
	}
	got, err = full.normalize()
	if err != nil {
		t.Fatalf("could not normalize %+v: %+v", full, err)
	}
	if got != full {
		t.Fatalf("got %+v, want %+v", got, full)
	}
}

func TestGhostedAreaSpecGrid(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		spec GhostedAreaSpec
	}{
		{spec: GhostedAreaSpec{GhostMaxRap: 6, GhostArea: 0.01}},
		{spec: GhostedAreaSpec{GhostMaxRap: 2, GhostArea: 0.05}},
		{spec: GhostedAreaSpec{GhostMaxRap: 4.5, GhostArea: 0.002}},
	} {
		spec, err := tc.spec.normalize()
		if err != nil {
			t.Fatalf("could not normalize %+v: %+v", tc.spec, err)
		}
		g := spec.grid()

		// a whole number of ghosts has to fit around the azimuth
		if got, want := float64(g.nphi)*g.dphi, 2*math.Pi; math.Abs(got-want) > 1e-12 {
			t.Errorf("%v ghosts of width %v span %v in azimuth, want %v", g.nphi, g.dphi, got, want)
		}

		// the area a ghost stands for has to be close to the one asked for
		if got, want := g.area, spec.GhostArea; math.Abs(got/want-1) > 0.02 {
			t.Errorf("got ghost area %v, want %v to within 2%%", got, want)
		}

		// and the lattice has to cover the rapidity range asked for, to
		// within the one row that does not fit
		if got, want := float64(g.nrap)*g.drap, spec.GhostMaxRap; got > want || got < want-g.drap {
			t.Errorf("ghosts reach |y| < %v, want %v", got, want)
		}

		if got, want := g.n(), (2*g.nrap+1)*g.nphi; got != want {
			t.Errorf("got %d ghosts, want %d", got, want)
		}

		// the ghosts have to tile the region without gaps or overlaps
		var (
			covered = float64(g.n()) * g.area
			region  = (2*float64(g.nrap) + 1) * g.drap * 2 * math.Pi
		)
		if math.Abs(covered/region-1) > 1e-12 {
			t.Errorf("%d ghosts cover %v of a %v region", g.n(), covered, region)
		}
	}
}

func TestAreaTypeString(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		typ  AreaType
		want string
	}{
		{ActiveArea, "ActiveArea"},
		{ActiveAreaExplicitGhosts, "ActiveAreaExplicitGhosts"},
		{PassiveArea, "PassiveArea"},
		{VoronoiArea, "VoronoiArea"},
	} {
		if got := tc.typ.String(); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}

	func() {
		defer func() {
			err := recover()
			if err == nil {
				t.Errorf("expected a panic for an out-of-range AreaType")
			}
		}()
		_ = AreaType(42).String()
	}()
}

func TestAreaDefinitionString(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		def  AreaDefinition
		want string
	}{
		{
			def:  AreaDefinition{},
			want: "ActiveArea(ghost-area=0.01, max-rap=6, repeat=1)",
		},
		{
			def: NewAreaDefinition(ActiveAreaExplicitGhosts, GhostedAreaSpec{
				GhostMaxRap: 2, GhostArea: 0.05, Repeat: 3,
			}),
			want: "ActiveAreaExplicitGhosts(ghost-area=0.05, max-rap=2, repeat=3)",
		},
		{
			def:  NewAreaDefinition(ActiveArea, GhostedAreaSpec{GhostArea: -1}),
			want: "ActiveArea(invalid ghost spec: fastjet: negative GhostArea (-1))",
		},
	} {
		if got := tc.def.String(); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
}

func TestAreaDefinitionValidate(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		def  AreaDefinition
		want string
	}{
		{
			name: "zero-value",
			def:  AreaDefinition{},
		},
		{
			name: "explicit-ghosts",
			def:  NewAreaDefinition(ActiveAreaExplicitGhosts, GhostedAreaSpec{GhostMaxRap: 2}),
		},
		{
			name: "passive",
			def:  NewAreaDefinition(PassiveArea, GhostedAreaSpec{}),
			want: "fastjet: PassiveArea is not implemented",
		},
		{
			name: "voronoi",
			def:  NewAreaDefinition(VoronoiArea, GhostedAreaSpec{}),
			want: "fastjet: VoronoiArea is not implemented",
		},
		{
			// an out-of-range type has no name to report, so it must not
			// be reported through String.
			name: "invalid-type",
			def:  NewAreaDefinition(AreaType(42), GhostedAreaSpec{}),
			want: "fastjet: invalid AreaType (42)",
		},
		{
			name: "invalid-ghost-spec",
			def:  NewAreaDefinition(ActiveArea, GhostedAreaSpec{GhostArea: -1}),
			want: "fastjet: negative GhostArea (-1)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.def.Validate()
			switch {
			case err == nil && tc.want != "":
				t.Fatalf("expected an error (%v)", tc.want)
			case err != nil && tc.want == "":
				t.Fatalf("could not validate area definition: %+v", err)
			case err != nil && err.Error() != tc.want:
				t.Fatalf("got error %q, want %q", err.Error(), tc.want)
			}
		})
	}
}

func TestAreaDefinitionAccessors(t *testing.T) {
	t.Parallel()

	spec := GhostedAreaSpec{GhostMaxRap: 3, Repeat: 2}
	def := NewAreaDefinition(ActiveAreaExplicitGhosts, spec)

	if got, want := def.Type(), ActiveAreaExplicitGhosts; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := def.GhostSpec(); got != spec {
		t.Errorf("got %+v, want %+v: the spec was not returned as it was given", got, spec)
	}
}

func TestJetAlgorithmString(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		alg  JetAlgorithm
		want string
		rap  bool // measures distances in rapidity-azimuth
	}{
		{UndefinedJetAlgorithm, "undefined", false},
		{KtAlgorithm, "kt", true},
		{CambridgeAlgorithm, "cambridge", true},
		{AntiKtAlgorithm, "antikt", true},
		{GenKtAlgorithm, "genkt", true},
		{CambridgeForPassiveAlgorithm, "cambridge-for-passive", true},
		{GenKtForPassiveAlgorithm, "genkt-for-passive", true},
		{EeKtAlgorithm, "ee-kt", false},
		{EeGenKtAlgorithm, "ee-genkt", false},
		{PluginAlgorithm, "plugin", false},
	} {
		if got := tc.alg.String(); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
		if got := tc.alg.usesRapPhi(); got != tc.rap {
			t.Errorf("%v: got usesRapPhi=%v, want %v", tc.want, got, tc.rap)
		}
	}

	func() {
		defer func() {
			err := recover()
			if err == nil {
				t.Errorf("expected a panic for an out-of-range JetAlgorithm")
			}
		}()
		_ = JetAlgorithm(42).String()
	}()
}

func TestQuantileOf(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		vals []float64
		q    float64
		want float64
	}{
		{vals: nil, q: 0.5, want: 0},
		{vals: []float64{7}, q: 0.5, want: 7},
		{vals: []float64{7}, q: 0.1, want: 7},
		{vals: []float64{0, 1}, q: 0.5, want: 0.5},
		{vals: []float64{0, 1, 2, 3, 4}, q: 0.5, want: 2},
		{vals: []float64{0, 1, 2, 3, 4}, q: 0, want: 0},
		{vals: []float64{0, 1, 2, 3, 4}, q: 1, want: 4},
		{vals: []float64{0, 1, 2, 3, 4}, q: 0.25, want: 1},
		{vals: []float64{0, 10}, q: 0.1587, want: 1.587},
	} {
		if got := quantileOf(tc.vals, tc.q); math.Abs(got-tc.want) > 1e-12 {
			t.Errorf("quantile %v of %v: got %v, want %v", tc.q, tc.vals, got, tc.want)
		}
	}
}
