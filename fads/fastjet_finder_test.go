// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fads

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"

	"go-hep.org/x/hep/fastjet"
	"go-hep.org/x/hep/fmom"
	"go-hep.org/x/hep/fwk"
)

// fjStore is the data store a FastJetFinder sees during Process.
type fjStore struct {
	data map[string]any
}

func (store *fjStore) Get(key string) (any, error) {
	v, ok := store.data[key]
	if !ok {
		return nil, fmt.Errorf("fads: no such key %q", key)
	}
	return v, nil
}

func (store *fjStore) Put(key string, value any) error {
	store.data[key] = value
	return nil
}

func (store *fjStore) Has(key string) bool {
	_, ok := store.data[key]
	return ok
}

// fjCtx is the fwk context a FastJetFinder sees during Process. Only the
// store is reachable from there.
type fjCtx struct {
	store *fjStore
}

func (ctx fjCtx) ID() int64          { return 0 }
func (ctx fjCtx) Slot() int          { return 0 }
func (ctx fjCtx) Store() fwk.Store   { return ctx.store }
func (ctx fjCtx) Msg() fwk.MsgStream { panic("fads: no message stream") }
func (ctx fjCtx) Svc(string) (fwk.Svc, error) {
	return nil, fmt.Errorf("fads: no service")
}

// fjFinder returns a finder configured as the fwk factory would, with the
// area properties overridden by fct.
func fjFinder(t *testing.T, fct func(tsk *FastJetFinder)) *FastJetFinder {
	t.Helper()

	tsk := &FastJetFinder{
		input:  "/fads/fastjet/input",
		output: "/fads/fastjet/output",
		rho:    "/fads/fastjet/rho",

		jetAlg:   fastjet.AntiKtAlgorithm,
		paramR:   0.5,
		jetPtMin: 10.0,

		ghostEtaMax: 2.0,
		repeat:      1,
		ghostArea:   0.02,
		gridScatter: 1.0,
		ptScatter:   0.1,
		meanGhostPt: 1e-100,

		effectiveRfact: 1.0,
		etaRangeMap:    make(map[float64]float64),
	}
	if fct != nil {
		fct(tsk)
	}
	tsk.jetDef = fastjet.NewJetDefinition(
		tsk.jetAlg, tsk.paramR, fastjet.EScheme, fastjet.BestStrategy,
	)

	return tsk
}

// fjCand returns a candidate with the given transverse momentum, rapidity and
// azimuth, massless and at the origin.
func fjCand(pt, rap, phi float64) Candidate {
	var cand Candidate
	cand.Mom = fmom.NewPxPyPzE(
		pt*math.Cos(phi),
		pt*math.Sin(phi),
		pt*math.Sinh(rap),
		pt*math.Cosh(rap),
	)
	return cand
}

// fjRun feeds input through tsk and returns the jets and the rho estimates.
func fjRun(t *testing.T, tsk *FastJetFinder, input []Candidate) (jets, rhos []Candidate) {
	t.Helper()

	ctx := fjCtx{store: &fjStore{data: map[string]any{tsk.input: input}}}
	err := tsk.Process(ctx)
	if err != nil {
		t.Fatalf("could not process event: %+v", err)
	}

	v, err := ctx.store.Get(tsk.output)
	if err != nil {
		t.Fatalf("could not retrieve jets: %+v", err)
	}
	jets = v.([]Candidate)

	v, err = ctx.store.Get(tsk.rho)
	if err != nil {
		t.Fatalf("could not retrieve rho: %+v", err)
	}

	return jets, v.([]Candidate)
}

func TestFastJetFinderAreaDefinition(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		fct  func(tsk *FastJetFinder)
		want fastjet.AreaType
		err  string
	}{
		{name: "none", fct: func(tsk *FastJetFinder) { tsk.areaAlg = areaAlgNone }},
		{
			name: "active",
			fct:  func(tsk *FastJetFinder) { tsk.areaAlg = areaAlgActive },
			want: fastjet.ActiveArea,
		},
		{
			name: "active-explicit-ghosts",
			fct:  func(tsk *FastJetFinder) { tsk.areaAlg = areaAlgActiveExplicitGhosts },
			want: fastjet.ActiveAreaExplicitGhosts,
		},
		{
			name: "passive",
			fct:  func(tsk *FastJetFinder) { tsk.areaAlg = areaAlgPassive },
			err:  "fastjet-finder: invalid area definition: fastjet: PassiveArea is not implemented",
		},
		{
			name: "voronoi",
			fct:  func(tsk *FastJetFinder) { tsk.areaAlg = areaAlgVoronoi },
			err:  "fastjet-finder: invalid area definition: fastjet: VoronoiArea is not implemented",
		},
		{
			name: "invalid",
			fct:  func(tsk *FastJetFinder) { tsk.areaAlg = 42 },
			err:  "fastjet-finder: invalid AreaAlgorithm (42)",
		},
		{
			name: "invalid-ghost-spec",
			fct: func(tsk *FastJetFinder) {
				tsk.areaAlg = areaAlgActive
				tsk.ghostArea = -1
			},
			err: "fastjet-finder: invalid area definition: fastjet: negative GhostArea (-1)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tsk := fjFinder(t, tc.fct)
			err := tsk.configureAreas()
			switch {
			case err != nil && tc.err == "":
				t.Fatalf("could not configure areas: %+v", err)
			case err != nil && err.Error() != tc.err:
				t.Fatalf("invalid error:\ngot= %v\nwant=%v", err.Error(), tc.err)
			case err == nil && tc.err != "":
				t.Fatalf("expected an error (%v)", tc.err)
			case err != nil:
				return
			}

			switch {
			case tsk.areaAlg == areaAlgNone:
				if tsk.areaDef != nil {
					t.Fatalf("got an area definition (%v) without one being asked for", tsk.areaDef)
				}
			case tsk.areaDef == nil:
				t.Fatalf("no area definition")
			case tsk.areaDef.Type() != tc.want:
				t.Fatalf("invalid area type: got=%v, want=%v", tsk.areaDef.Type(), tc.want)
			}

			if tsk.areaDef == nil {
				return
			}
			if got, want := tsk.areaDef.GhostSpec().GhostMaxRap, tsk.ghostEtaMax; got != want {
				t.Fatalf("invalid ghost reach: got=%v, want=%v", got, want)
			}
		})
	}
}

func TestFastJetFinderComputeRhoConfig(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		fct  func(tsk *FastJetFinder)
		err  string
	}{
		{
			name: "no-area",
			fct: func(tsk *FastJetFinder) {
				tsk.computeRho = true
			},
			err: "fastjet-finder: ComputeRho needs jet areas: set AreaAlgorithm to 4",
		},
		{
			name: "no-explicit-ghosts",
			fct: func(tsk *FastJetFinder) {
				tsk.computeRho = true
				tsk.areaAlg = areaAlgActive
			},
			err: "fastjet-finder: ComputeRho needs ActiveAreaExplicitGhosts (AreaAlgorithm=4), got ActiveArea: without the pure-ghost jets the empty regions of the event are missing and the background comes out too high",
		},
		{
			name: "ok",
			fct: func(tsk *FastJetFinder) {
				tsk.computeRho = true
				tsk.areaAlg = areaAlgActiveExplicitGhosts
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tsk := fjFinder(t, tc.fct)
			err := tsk.configureAreas()
			switch {
			case err != nil && tc.err == "":
				t.Fatalf("could not configure areas: %+v", err)
			case err != nil && err.Error() != tc.err:
				t.Fatalf("invalid error:\ngot= %v\nwant=%v", err.Error(), tc.err)
			case err == nil && tc.err != "":
				t.Fatalf("expected an error (%v)", tc.err)
			case err != nil:
				return
			}

			// an unset RhoEtaRange measures the background out to one jet
			// radius inside the ghosts.
			want := map[float64]float64{0: tsk.ghostEtaMax - tsk.paramR}
			if got := tsk.etaRangeMap; len(got) != 1 || got[0] != want[0] {
				t.Fatalf("invalid default rho range:\ngot= %v\nwant=%v", got, want)
			}
		})
	}
}

func TestFastJetFinder(t *testing.T) {
	t.Parallel()

	// two jets, the harder one wider than the other.
	input := []Candidate{
		fjCand(60, +0.0, 0.0),
		fjCand(30, +0.3, 0.3),
		fjCand(50, +0.0, math.Pi),
		fjCand(10, +0.02, math.Pi+0.02),
	}

	tsk := fjFinder(t, nil)
	err := tsk.configureAreas()
	if err != nil {
		t.Fatalf("could not configure areas: %+v", err)
	}

	jets, rhos := fjRun(t, tsk, input)

	if got, want := len(jets), 2; got != want {
		t.Fatalf("invalid number of jets: got=%d, want=%d", got, want)
	}
	if got, want := len(rhos), 0; got != want {
		t.Fatalf("invalid number of rho estimates: got=%d, want=%d", got, want)
	}

	// the E-scheme adds the constituents as 4-vectors, so a jet whose
	// constituents are not collinear has less pt than their scalar sum.
	for i, want := range []float64{89.10225227710119, 59.998333365740905} {
		if got := jets[i].Mom.Pt(); math.Abs(got-want) > 1e-9 {
			t.Fatalf("jet[%d]: invalid pt: got=%v, want=%v", i, got, want)
		}
		if got := len(jets[i].Candidates); got != 2 {
			t.Fatalf("jet[%d]: invalid number of constituents: got=%d, want=2", i, got)
		}
		if got := jets[i].Area; got != (fmom.PxPyPzE{}) {
			t.Fatalf("jet[%d]: got an area (%v) without one being asked for", i, got)
		}
	}

	// the extent of a jet is its own, not that of the jets before it: the
	// narrow jet comes second, and must not have inherited the wide one's.
	if jets[1].DEta >= jets[0].DEta || jets[1].DPhi >= jets[0].DPhi {
		t.Fatalf(
			"the second jet is narrower than the first, but reports (deta=%v, dphi=%v) against (deta=%v, dphi=%v)",
			jets[1].DEta, jets[1].DPhi, jets[0].DEta, jets[0].DPhi,
		)
	}
}

func TestFastJetFinderAreas(t *testing.T) {
	t.Parallel()

	const (
		rapMax = 1.5 // ghostEtaMax - paramR
		meanPt = 0.4
		nBkg   = 600
	)

	var (
		rnd   = rand.New(rand.NewPCG(1234, 5678))
		input = []Candidate{
			fjCand(60, +0.0, 0.0),
			fjCand(60, +0.0, math.Pi),
		}
		// a uniform soft background over the region the ghosts cover.
		density = nBkg * meanPt / (2 * rapMax * 2 * math.Pi)
	)
	for range nBkg {
		input = append(input, fjCand(
			meanPt*(0.5+rnd.Float64()),
			rapMax*(2*rnd.Float64()-1),
			2*math.Pi*rnd.Float64(),
		))
	}

	tsk := fjFinder(t, func(tsk *FastJetFinder) {
		tsk.areaAlg = areaAlgActiveExplicitGhosts
		tsk.computeRho = true
	})
	err := tsk.configureAreas()
	if err != nil {
		t.Fatalf("could not configure areas: %+v", err)
	}

	jets, rhos := fjRun(t, tsk, input)

	if len(jets) == 0 {
		t.Fatalf("no jet")
	}

	// the pure-ghost jets that carry the empty regions of the event are not
	// jets, and none of them may reach the output.
	for i, jet := range jets {
		if got := jet.Mom.Pt(); got < tsk.jetPtMin {
			t.Fatalf("jet[%d]: pt=%v below the %v threshold", i, got, tsk.jetPtMin)
		}
		if len(jet.Candidates) == 0 {
			t.Fatalf("jet[%d]: no constituent", i)
		}

		// an anti-kt jet is a disk of radius R. The area is a 4-vector, so
		// its pt is the vector sum over that disk and comes out somewhat
		// below the scalar area the disk covers.
		area := jet.Area.Pt() / (math.Pi * tsk.paramR * tsk.paramR)
		if math.Abs(area-1) > 0.2 {
			t.Fatalf("jet[%d]: invalid area: got=%v*pi*R^2, want=1", i, area)
		}
	}

	if got, want := len(rhos), 1; got != want {
		t.Fatalf("invalid number of rho estimates: got=%d, want=%d", got, want)
	}

	rho := rhos[0]
	if got, want := rho.Edges[0], 0.0; got != want {
		t.Fatalf("invalid rho range minimum: got=%v, want=%v", got, want)
	}
	if got, want := rho.Edges[1], rapMax; got != want {
		t.Fatalf("invalid rho range maximum: got=%v, want=%v", got, want)
	}
	if got := rho.Mom.Pt(); math.Abs(got-density)/density > 0.2 {
		t.Fatalf("invalid rho: got=%v, want=%v (+/- 20%%)", got, density)
	}
}

func TestFastJetFinderRhoRanges(t *testing.T) {
	t.Parallel()

	// a background that is twice as dense in the forward region as in the
	// central one, so that the two ranges cannot be confused.
	var (
		rnd   = rand.New(rand.NewPCG(1234, 5678))
		input []Candidate
	)
	for range 300 {
		input = append(input,
			fjCand(0.4, 0.75*(2*rnd.Float64()-1), 2*math.Pi*rnd.Float64()),
			fjCand(0.8, 0.75+0.75*rnd.Float64(), 2*math.Pi*rnd.Float64()),
			fjCand(0.8, -0.75-0.75*rnd.Float64(), 2*math.Pi*rnd.Float64()),
		)
	}

	tsk := fjFinder(t, func(tsk *FastJetFinder) {
		tsk.areaAlg = areaAlgActiveExplicitGhosts
		tsk.computeRho = true
		tsk.etaRangeMap = map[float64]float64{
			0:    0.75,
			0.75: 1.5,
		}
	})
	err := tsk.configureAreas()
	if err != nil {
		t.Fatalf("could not configure areas: %+v", err)
	}

	_, rhos := fjRun(t, tsk, input)

	if got, want := len(rhos), 2; got != want {
		t.Fatalf("invalid number of rho estimates: got=%d, want=%d", got, want)
	}

	// ranges come back in a stable order, whatever the map's iteration order.
	for i, want := range [][2]float64{{0, 0.75}, {0.75, 1.5}} {
		if got := [2]float64{rhos[i].Edges[0], rhos[i].Edges[1]}; got != want {
			t.Fatalf("rho[%d]: invalid range: got=%v, want=%v", i, got, want)
		}
	}

	central, forward := rhos[0].Mom.Pt(), rhos[1].Mom.Pt()
	if central <= 0 {
		t.Fatalf("invalid central rho: got=%v, want>0", central)
	}
	if ratio := forward / central; math.Abs(ratio-2) > 0.4 {
		t.Fatalf("invalid forward/central rho ratio: got=%v, want=2 (+/- 0.4)", ratio)
	}
}
