// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fastjet_test

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"testing"

	"go-hep.org/x/hep/fastjet"
)

// randomEvent builds a reproducible pseudo-event: n particles spread over
// the central rapidity range with a falling pt spectrum.
func randomEvent(rnd *rand.Rand, n int) []fastjet.Jet {
	jets := make([]fastjet.Jet, n)
	for i := range jets {
		var (
			pt  = 0.5 + 40*rnd.Float64()*rnd.Float64()
			rap = 5*rnd.Float64() - 2.5
			phi = 2 * math.Pi * rnd.Float64()
			px  = pt * math.Cos(phi)
			py  = pt * math.Sin(phi)
			pz  = pt * math.Sinh(rap)
			e   = pt * math.Cosh(rap)
		)
		jets[i] = fastjet.NewJet(px, py, pz, e)
	}
	return jets
}

// TestN2PlainMatchesN3Dumb checks that the O(N^2) strategy is not merely
// plausible but produces the very same clustering as the reference O(N^3)
// one, for every rapidity-phi algorithm and a spread of radii.
func TestN2PlainMatchesN3Dumb(t *testing.T) {
	t.Parallel()

	for _, alg := range []struct {
		alg   fastjet.JetAlgorithm
		name  string
		extra float64
	}{
		{fastjet.KtAlgorithm, "kt", 0},
		{fastjet.AntiKtAlgorithm, "antikt", 0},
		{fastjet.CambridgeAlgorithm, "cambridge", 0},
		{fastjet.GenKtAlgorithm, "genkt_p+0.5", 0.5},
		{fastjet.GenKtAlgorithm, "genkt_p-0.5", -0.5},
	} {
		for _, r := range []float64{0.4, 0.7, 1.0} {
			name := fmt.Sprintf("%s_r%.1f", alg.name, r)
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				rnd := rand.New(rand.NewPCG(1234, uint64(len(name))))
				for i := range 20 {
					var (
						parts = randomEvent(rnd, 5+i*5)
						plain = fastjet.NewJetDefinitionExtra(alg.alg, r, fastjet.EScheme, fastjet.N2PlainStrategy, alg.extra)
						dumb  = fastjet.NewJetDefinitionExtra(alg.alg, r, fastjet.EScheme, fastjet.N3DumbStrategy, alg.extra)
					)

					got := clusterInclusive(t, parts, plain, 0)
					want := clusterInclusive(t, parts, dumb, 0)

					if len(got) != len(want) {
						t.Fatalf("event %d: got %d jets, want %d", i, len(got), len(want))
					}
					for j := range got {
						const tol = 1e-9
						g, w := &got[j], &want[j]
						if math.Abs(g.Px()-w.Px()) > tol ||
							math.Abs(g.Py()-w.Py()) > tol ||
							math.Abs(g.Pz()-w.Pz()) > tol ||
							math.Abs(g.E()-w.E()) > tol {
							t.Fatalf(
								"event %d, jet %d:\ngot= %v\nwant=%v",
								i, j, g.PxPyPzE, w.PxPyPzE,
							)
						}
					}
				}
			})
		}
	}
}

// TestN2PlainConstituents checks the two strategies also agree on which
// particles ended up in which jet, not just on the summed four-momenta.
func TestN2PlainConstituents(t *testing.T) {
	t.Parallel()

	var (
		rnd   = rand.New(rand.NewPCG(99, 7))
		parts = randomEvent(rnd, 60)
		plain = fastjet.NewJetDefinition(fastjet.AntiKtAlgorithm, 0.6, fastjet.EScheme, fastjet.N2PlainStrategy)
		dumb  = fastjet.NewJetDefinition(fastjet.AntiKtAlgorithm, 0.6, fastjet.EScheme, fastjet.N3DumbStrategy)
	)

	var runs [2][][]float64
	for i, def := range []fastjet.JetDefinition{plain, dumb} {
		cs, err := fastjet.NewClusterSequence(parts, def)
		if err != nil {
			t.Fatalf("%v: clustering failed: %v", def.Strategy(), err)
		}
		jets, err := cs.InclusiveJets(5)
		if err != nil {
			t.Fatalf("%v: incl-jets failed: %v", def.Strategy(), err)
		}
		sort.Sort(fastjet.ByPt(jets))

		var got [][]float64
		for j := range jets {
			cts, err := cs.Constituents(&jets[j])
			if err != nil {
				t.Fatalf("%v: constituents failed: %v", def.Strategy(), err)
			}
			pts := make([]float64, len(cts))
			for k := range cts {
				pts[k] = cts[k].Pt()
			}
			sort.Float64s(pts)
			got = append(got, pts)
		}

		runs[i] = got
	}

	got, want := runs[0], runs[1]
	if len(got) != len(want) {
		t.Fatalf("got %d jets, want %d", len(got), len(want))
	}
	if len(got) == 0 {
		t.Fatalf("no jets above threshold: the test proves nothing")
	}
	for j := range got {
		if len(got[j]) != len(want[j]) {
			t.Fatalf("jet %d: got %d constituents, want %d", j, len(got[j]), len(want[j]))
		}
		for k := range got[j] {
			if math.Abs(got[j][k]-want[j][k]) > 1e-9 {
				t.Fatalf("jet %d: constituents differ:\ngot= %v\nwant=%v", j, got[j], want[j])
			}
		}
	}
}

// TestN2PlainEeFallsBack checks that the e+e- algorithms, whose distance is
// an opening angle rather than a rapidity-phi separation, are not silently
// clustered with the rapidity-phi code when N2Plain is asked for.
func TestN2PlainEeFallsBack(t *testing.T) {
	t.Parallel()

	var (
		rnd   = rand.New(rand.NewPCG(5, 5))
		parts = randomEvent(rnd, 30)
		plain = fastjet.NewJetDefinition(fastjet.EeKtAlgorithm, 0.7, fastjet.EScheme, fastjet.N2PlainStrategy)
		dumb  = fastjet.NewJetDefinition(fastjet.EeKtAlgorithm, 0.7, fastjet.EScheme, fastjet.N3DumbStrategy)
	)

	got := clusterInclusive(t, parts, plain, 0)
	want := clusterInclusive(t, parts, dumb, 0)

	if len(got) != len(want) {
		t.Fatalf("got %d jets, want %d", len(got), len(want))
	}
	for i := range got {
		if math.Abs(got[i].E()-want[i].E()) > 1e-9 {
			t.Fatalf("jet %d: got E=%v, want %v", i, got[i].E(), want[i].E())
		}
	}
}

// TestN2PlainDegenerate exercises the paths a random event rarely reaches:
// an empty event, a single particle, and exactly coincident particles.
func TestN2PlainDegenerate(t *testing.T) {
	t.Parallel()

	def := fastjet.NewJetDefinition(fastjet.KtAlgorithm, 0.7, fastjet.EScheme, fastjet.N2PlainStrategy)

	for _, tc := range []struct {
		name  string
		parts []fastjet.Jet
		njets int
	}{
		{name: "empty", parts: nil, njets: 0},
		{
			name:  "single",
			parts: []fastjet.Jet{fastjet.NewJet(10, 0, 0, 10)},
			njets: 1,
		},
		{
			name: "coincident",
			parts: []fastjet.Jet{
				fastjet.NewJet(10, 0, 0, 10),
				fastjet.NewJet(10, 0, 0, 10),
				fastjet.NewJet(10, 0, 0, 10),
			},
			njets: 1,
		},
		{
			name: "back-to-back",
			parts: []fastjet.Jet{
				fastjet.NewJet(+10, 0, 0, 10),
				fastjet.NewJet(-10, 0, 0, 10),
			},
			njets: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			jets := clusterInclusive(t, tc.parts, def, 0)
			if len(jets) != tc.njets {
				t.Fatalf("got %d jets, want %d", len(jets), tc.njets)
			}
		})
	}
}

func clusterInclusive(t *testing.T, parts []fastjet.Jet, def fastjet.JetDefinition, ptmin float64) []fastjet.Jet {
	t.Helper()

	cs, err := fastjet.NewClusterSequence(parts, def)
	if err != nil {
		t.Fatalf("%v: clustering failed: %v", def.Strategy(), err)
	}
	jets, err := cs.InclusiveJets(ptmin)
	if err != nil {
		t.Fatalf("%v: incl-jets failed: %v", def.Strategy(), err)
	}
	sort.Sort(fastjet.ByPt(jets))
	return jets
}

func BenchmarkClusterN2Plain(b *testing.B) {
	benchCluster(b, fastjet.N2PlainStrategy)
}

func BenchmarkClusterN3Dumb(b *testing.B) {
	benchCluster(b, fastjet.N3DumbStrategy)
}

func benchCluster(b *testing.B, strat fastjet.Strategy) {
	var (
		rnd   = rand.New(rand.NewPCG(42, 42))
		parts = randomEvent(rnd, 400)
		def   = fastjet.NewJetDefinition(fastjet.AntiKtAlgorithm, 0.4, fastjet.EScheme, strat)
	)

	b.ResetTimer()
	for b.Loop() {
		cs, err := fastjet.NewClusterSequence(parts, def)
		if err != nil {
			b.Fatal(err)
		}
		_, err = cs.InclusiveJets(5)
		if err != nil {
			b.Fatal(err)
		}
	}
}
