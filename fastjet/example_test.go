// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fastjet_test

import (
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"sort"

	"go-hep.org/x/hep/fastjet"
)

// ExampleNewClusterSequence clusters a handful of particles into anti-kt jets.
func ExampleNewClusterSequence() {
	particles := []fastjet.Jet{
		// two hard particles, close enough in angle to end up in one jet
		fastjet.NewJet(+50.0, +0.0, 0, 50.0),
		fastjet.NewJet(+20.0, +5.0, 0, 21.0),
		// and a third one, recoiling against them
		fastjet.NewJet(-60.0, +0.0, 0, 60.0),
	}

	def := fastjet.NewJetDefinition(fastjet.AntiKtAlgorithm, 0.4, fastjet.EScheme, fastjet.BestStrategy)

	cs, err := fastjet.NewClusterSequence(particles, def)
	if err != nil {
		log.Fatalf("could not cluster: %+v", err)
	}

	jets, err := cs.InclusiveJets(5.0)
	if err != nil {
		log.Fatalf("could not extract jets: %+v", err)
	}

	// the jets come back in the order the clustering happened to build them,
	// so an analysis that speaks of "the leading jet" has to sort them first.
	sort.Slice(jets, func(i, j int) bool { return jets[i].Pt() > jets[j].Pt() })

	fmt.Printf("clustered %d particles into %d jets with %v\n", len(particles), len(jets), def.Algorithm())
	for i := range jets {
		cts, err := cs.Constituents(&jets[i])
		if err != nil {
			log.Fatalf("could not extract constituents: %+v", err)
		}
		fmt.Printf("jet[%d]: pt=%.1f rap=%+.2f #constituents=%d\n",
			i, jets[i].Pt(), jets[i].Rapidity(), len(cts),
		)
	}

	// Output:
	// clustered 3 particles into 2 jets with antikt
	// jet[0]: pt=70.2 rap=+0.00 #constituents=2
	// jet[1]: pt=60.0 rap=+0.00 #constituents=1
}

// ExampleNewClusterSequenceArea measures how much rapidity-azimuth area each
// jet occupies, which is what a pile-up subtraction needs to know.
func ExampleNewClusterSequenceArea() {
	particles := []fastjet.Jet{
		fastjet.NewJet(+50.0, +0.0, 0, 50.0),
		fastjet.NewJet(-50.0, +0.0, 0, 50.0),
	}

	const r = 0.6
	def := fastjet.NewJetDefinition(fastjet.AntiKtAlgorithm, r, fastjet.EScheme, fastjet.BestStrategy)

	// the zero AreaDefinition would do just as well: it asks for an active
	// area with ghosts out to |y| < 6, which is more than this event needs.
	area := fastjet.NewAreaDefinition(fastjet.ActiveArea, fastjet.GhostedAreaSpec{
		GhostMaxRap: 3,
		GhostArea:   0.01,
	})

	csa, err := fastjet.NewClusterSequenceArea(particles, def, area)
	if err != nil {
		log.Fatalf("could not cluster: %+v", err)
	}

	jets, err := csa.InclusiveJets(5.0)
	if err != nil {
		log.Fatalf("could not extract jets: %+v", err)
	}

	// an isolated anti-kt jet is a disc of radius R, so its area comes out at
	// pi*R^2 whatever the ghosts happen to do. Where the ghosts do show up is
	// in the last digit: they measure the area, they do not compute it.
	for i := range jets {
		fmt.Printf("jet[%d]: pt=%.1f area/(pi*R^2)=%.1f\n",
			i, jets[i].Pt(), csa.Area(&jets[i])/(math.Pi*r*r),
		)
	}

	// Output:
	// jet[0]: pt=50.0 area/(pi*R^2)=1.0
	// jet[1]: pt=50.0 area/(pi*R^2)=1.0
}

// ExampleNewJetMedianBackgroundEstimator recovers a hard jet buried in a
// diffuse background, of the kind pile-up leaves in a collider event.
func ExampleNewJetMedianBackgroundEstimator() {
	const (
		rapMax  = 2.0
		hardPt  = 100.0
		meanPt  = 0.5 // GeV per background particle
		nBkg    = 800
		density = nBkg * meanPt / (2 * rapMax * 2 * math.Pi) // true rho
	)

	rnd := rand.New(rand.NewPCG(1234, 5678))

	// a soft background, spread uniformly over the central region
	particles := make([]fastjet.Jet, 0, nBkg+1)
	for range nBkg {
		var (
			rap = rapMax * (2*rnd.Float64() - 1)
			phi = 2 * math.Pi * rnd.Float64()
			pt  = -meanPt * math.Log(rnd.Float64()) // exponential spectrum
		)
		particles = append(particles, fastjet.NewJet(
			pt*math.Cos(phi), pt*math.Sin(phi), pt*math.Sinh(rap), pt*math.Cosh(rap),
		))
	}

	// and one hard particle sitting in the middle of it
	particles = append(particles, fastjet.NewJet(hardPt, 0, 0, hardPt))

	const r = 0.5
	def := fastjet.NewJetDefinition(fastjet.KtAlgorithm, r, fastjet.EScheme, fastjet.BestStrategy)

	// the background estimate needs the pure-ghost jets: the empty patches of
	// the event are the ones with no background in them, and leaving them out
	// would bias the estimate upwards.
	area := fastjet.NewAreaDefinition(fastjet.ActiveAreaExplicitGhosts, fastjet.GhostedAreaSpec{
		GhostMaxRap: rapMax,
		GhostArea:   0.02,
	})

	csa, err := fastjet.NewClusterSequenceArea(particles, def, area)
	if err != nil {
		log.Fatalf("could not cluster: %+v", err)
	}

	bkg, err := fastjet.NewJetMedianBackgroundEstimator(csa, fastjet.WithBkgExcludeHardest(2))
	if err != nil {
		log.Fatalf("could not estimate the background: %+v", err)
	}

	jets, err := csa.InclusiveJets(0)
	if err != nil {
		log.Fatalf("could not extract jets: %+v", err)
	}

	// the hardest jet is the one holding the hard particle
	hard := &jets[0]
	for i := range jets {
		if jets[i].Pt() > hard.Pt() {
			hard = &jets[i]
		}
	}
	sub := bkg.Subtract(hard)

	fmt.Printf("rho:        %.1f GeV per unit area (true %.1f)\n", bkg.Rho(), density)
	fmt.Printf("hard jet:   pt=%.1f GeV over an area of %.1f\n", hard.Pt(), csa.Area(hard))
	fmt.Printf("subtracted: pt=%.1f GeV (the hard particle had %.1f)\n", sub.Pt(), hardPt)

	// Output:
	// rho:        14.6 GeV per unit area (true 15.9)
	// hard jet:   pt=112.7 GeV over an area of 1.0
	// subtracted: pt=99.2 GeV (the hard particle had 100.0)
}
