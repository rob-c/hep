// Copyright ©2017 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fads

import (
	"fmt"
	"math"
	"reflect"
	"sort"

	"go-hep.org/x/hep/fastjet"
	"go-hep.org/x/hep/fmom"
	"go-hep.org/x/hep/fwk"
)

// FastJetFinder finds jets using the fastjet library.
//
// The jets go to the Output collection, in decreasing order of transverse
// momentum, each one holding the input candidates it was built from.
//
// # Jet areas
//
// AreaAlgorithm asks for the area each jet occupies in the rapidity-azimuth
// plane, which is what a pile-up subtraction needs in order to know how much
// of a jet's momentum was not its own:
//
//	0  no area                      (the default)
//	1  active area
//	2  passive area                 (not implemented)
//	3  Voronoi area                 (not implemented)
//	4  active area, explicit ghosts
//
// The area is measured by filling the event with infinitely soft ghosts and
// counting the ones each jet takes in; GhostEtaMax, GhostArea, Repeat,
// GridScatter, PtScatter and MeanGhostPt describe that ensemble. Each jet
// carries its area as a 4-vector, in the Area field.
//
// # Background density
//
// ComputeRho fills the Rho collection with the transverse momentum per unit
// area that the diffuse background -- pile-up, underlying event -- carries.
// It is the median of pt/area over the jets of the event, so that a couple of
// hard jets do not drag it upwards.
//
// It needs the areas of AreaAlgorithm 4: the pure-ghost jets that only that
// option keeps are the empty regions of the event, and dropping them would
// bias the median towards the populated ones.
//
// RhoEtaRange maps the lower edge of a rapidity band to its upper edge, and
// the background is estimated separately in each of them -- a forward region
// of a hadron collider does not have the same pile-up as its central one.
// Each band comes back as a candidate whose momentum is that density and
// whose first two edges are the band it was measured over. Left unset, it
// covers everything the ghosts reach, less one jet radius.
type FastJetFinder struct {
	fwk.TaskBase

	input  string
	output string
	rho    string

	jetDef           fastjet.JetDefinition
	jetAlg           fastjet.JetAlgorithm
	paramR           float64
	jetPtMin         float64
	coneRadius       float64
	seedThreshold    float64
	coneAreaFraction float64
	maxIters         int
	maxPairSize      int
	iratch           int
	adjacencyCut     int
	overlapThreshold float64

	// fastjet area method ---
	areaDef    *fastjet.AreaDefinition
	areaAlg    int
	computeRho bool

	// ghost based areas ---
	ghostEtaMax float64
	repeat      int
	ghostArea   float64
	gridScatter float64
	ptScatter   float64
	meanGhostPt float64

	// voronoi areas ---
	effectiveRfact float64
	etaRangeMap    map[float64]float64
}

func (tsk *FastJetFinder) Configure(ctx fwk.Context) error {
	var err error

	err = tsk.DeclInPort(tsk.input, reflect.TypeFor[[]Candidate]())
	if err != nil {
		return err
	}

	err = tsk.DeclOutPort(tsk.output, reflect.TypeFor[[]Candidate]())
	if err != nil {
		return err
	}

	err = tsk.DeclOutPort(tsk.rho, reflect.TypeFor[[]Candidate]())
	if err != nil {
		return err
	}

	if tsk.jetAlg != fastjet.AntiKtAlgorithm {
		return fmt.Errorf("fastjet-finder: only implemented for AntiKt")
	}

	tsk.jetDef = fastjet.NewJetDefinition(tsk.jetAlg, tsk.paramR, fastjet.EScheme, fastjet.BestStrategy)

	return tsk.configureAreas()
}

// configureAreas translates the area properties into the area definition the
// clustering takes, and makes sure it can support the background estimate.
func (tsk *FastJetFinder) configureAreas() error {
	var err error

	tsk.areaDef, err = tsk.areaDefinition()
	if err != nil {
		return err
	}

	if tsk.computeRho {
		switch {
		case tsk.areaDef == nil:
			return fmt.Errorf(
				"fastjet-finder: ComputeRho needs jet areas: set AreaAlgorithm to %d",
				areaAlgActiveExplicitGhosts,
			)
		case tsk.areaDef.Type() != fastjet.ActiveAreaExplicitGhosts:
			return fmt.Errorf(
				"fastjet-finder: ComputeRho needs %v (AreaAlgorithm=%d), got %v: without the pure-ghost jets the empty regions of the event are missing and the background comes out too high",
				fastjet.ActiveAreaExplicitGhosts, areaAlgActiveExplicitGhosts, tsk.areaDef.Type(),
			)
		}

		// A background measured out to the edge of the ghosts would take in
		// jets whose area is only partly covered by them. One jet radius in
		// from that edge is the largest range that stays honest.
		if len(tsk.etaRangeMap) == 0 {
			tsk.etaRangeMap = map[float64]float64{
				0: math.Max(0, tsk.ghostEtaMax-tsk.paramR),
			}
		}
	}

	return nil
}

// Delphes' AreaAlgorithm codes.
const (
	areaAlgNone = iota
	areaAlgActive
	areaAlgPassive
	areaAlgVoronoi
	areaAlgActiveExplicitGhosts
)

// areaDefinition translates the AreaAlgorithm and ghost properties into the
// area definition the clustering takes, or nil if no areas were asked for.
func (tsk *FastJetFinder) areaDefinition() (*fastjet.AreaDefinition, error) {
	var typ fastjet.AreaType
	switch tsk.areaAlg {
	case areaAlgNone:
		return nil, nil
	case areaAlgActive:
		typ = fastjet.ActiveArea
	case areaAlgPassive:
		typ = fastjet.PassiveArea
	case areaAlgVoronoi:
		typ = fastjet.VoronoiArea
	case areaAlgActiveExplicitGhosts:
		typ = fastjet.ActiveAreaExplicitGhosts
	default:
		return nil, fmt.Errorf("fastjet-finder: invalid AreaAlgorithm (%d)", tsk.areaAlg)
	}

	def := fastjet.NewAreaDefinition(typ, fastjet.GhostedAreaSpec{
		GhostMaxRap: tsk.ghostEtaMax,
		Repeat:      tsk.repeat,
		GhostArea:   tsk.ghostArea,
		GridScatter: tsk.gridScatter,
		PtScatter:   tsk.ptScatter,
		MeanGhostPt: tsk.meanGhostPt,
	})
	if err := def.Validate(); err != nil {
		return nil, fmt.Errorf("fastjet-finder: invalid area definition: %w", err)
	}

	return &def, nil
}

// rhos estimates the background transverse momentum per unit area in each of
// the configured rapidity ranges.
//
// Each range comes back as a candidate whose momentum is that density, and
// whose first two edges are the range it was measured over -- which is how a
// downstream subtraction finds the one that applies to a given jet.
func (tsk *FastJetFinder) rhos(csa *fastjet.ClusterSequenceArea) ([]Candidate, error) {
	out := make([]Candidate, 0, len(tsk.etaRangeMap))
	if !tsk.computeRho {
		return out, nil
	}

	// map iteration order is not stable, and the output collection is
	// indexed by position downstream.
	etaMins := make([]float64, 0, len(tsk.etaRangeMap))
	for etaMin := range tsk.etaRangeMap {
		etaMins = append(etaMins, etaMin)
	}
	sort.Float64s(etaMins)

	for _, etaMin := range etaMins {
		etaMax := tsk.etaRangeMap[etaMin]
		bkg, err := fastjet.NewJetMedianBackgroundEstimator(
			csa, fastjet.WithBkgRapRange(etaMin, etaMax),
		)
		if err != nil {
			return nil, err
		}

		rho := bkg.Rho()
		cand := Candidate{Mom: fmom.NewPxPyPzE(rho, 0, 0, rho)}
		cand.Edges[0] = etaMin
		cand.Edges[1] = etaMax
		out = append(out, cand)
	}

	return out, nil
}

func (tsk *FastJetFinder) StartTask(ctx fwk.Context) error {
	var err error

	return err
}

func (tsk *FastJetFinder) StopTask(ctx fwk.Context) error {
	var err error

	return err
}

func (tsk *FastJetFinder) Process(ctx fwk.Context) error {
	var err error

	store := ctx.Store()

	v, err := store.Get(tsk.input)
	if err != nil {
		return err
	}
	input := v.([]Candidate)

	output := make([]Candidate, 0)
	defer func() {
		err = store.Put(tsk.output, output)
	}()

	injets := make([]fastjet.Jet, 0, len(input))
	for i := range input {
		cand := &input[i]
		jet := fastjet.NewJet(cand.Mom.Px(), cand.Mom.Py(), cand.Mom.Pz(), cand.Mom.E())
		jet.UserInfo = i
		injets = append(injets, jet)
	}

	// construct jets
	var (
		bldr fastjet.Builder
		csa  *fastjet.ClusterSequenceArea
	)
	if tsk.areaDef != nil {
		csa, err = fastjet.NewClusterSequenceArea(injets, tsk.jetDef, *tsk.areaDef)
		if err != nil {
			return err
		}
		bldr = csa
	} else {
		bldr, err = fastjet.NewClusterSequence(injets, tsk.jetDef)
		if err != nil {
			return err
		}
	}

	// compute rho and store it
	rhos, err := tsk.rhos(csa)
	if err != nil {
		return err
	}

	err = store.Put(tsk.rho, rhos)
	if err != nil {
		return err
	}

	outjets, err := bldr.InclusiveJets(tsk.jetPtMin)
	if err != nil {
		return err
	}
	sort.Sort(fastjet.ByPt(outjets))

	output = make([]Candidate, 0, len(outjets))
	for i := range outjets {
		var (
			jet     = &outjets[i]
			area    fmom.PxPyPzE
			detaMax = 0.0
			dphiMax = 0.0
		)
		if csa != nil {
			// explicit ghosts leave the empty regions of the event in the
			// jet list, for the background estimate. They are not jets.
			if csa.IsPureGhost(jet) {
				continue
			}
			area = csa.Area4Vector(jet)
		}

		cand := Candidate{
			Mom: jet.PxPyPzE,
		}

		time := 0.0
		wtime := 0.0
		csts, err := bldr.Constituents(jet)
		if err != nil {
			return err
		}

		for j := range csts {
			idx := csts[j].UserInfo.(int)
			cst := &input[idx]
			deta := math.Abs(cand.Mom.Eta() - cst.Mom.Eta())
			dphi := math.Abs(fmom.DeltaPhi(&cand.Mom, &cst.Mom))
			if deta > detaMax {
				detaMax = deta
			}
			if dphi > dphiMax {
				dphiMax = dphi
			}

			esqrt := math.Sqrt(cst.Mom.E())
			time += esqrt * cst.Pos.T()
			wtime += esqrt

			cand.Add(cst)
		}

		cand.Pos.P4.T = time / wtime
		cand.Area = area
		cand.DEta = detaMax
		cand.DPhi = dphiMax

		output = append(output, cand)
	}

	// fmt.Printf("%s: input=%02d outjets=%02d\n", tsk.Name(), len(input), len(output))
	return err
}

func newFastJetFinder(typ, name string, mgr fwk.App) (fwk.Component, error) {
	var err error

	tsk := &FastJetFinder{
		TaskBase: fwk.NewTask(typ, name, mgr),

		input:  "/fads/fastjet/input",
		output: "/fads/fastjet/output",
		rho:    "/fads/fastjet/rho",

		jetAlg:           fastjet.AntiKtAlgorithm,
		paramR:           0.5,
		jetPtMin:         10.0,
		coneRadius:       0.5,
		seedThreshold:    1.0,
		coneAreaFraction: 1.0,
		maxIters:         100,
		maxPairSize:      2,
		iratch:           1,
		adjacencyCut:     2,
		overlapThreshold: 0.75,

		// fastjet area method ---
		areaDef:    nil,
		areaAlg:    0,
		computeRho: false,

		// ghost based areas ---
		ghostEtaMax: 5.0,
		repeat:      1,
		ghostArea:   0.01,
		gridScatter: 1.0,
		ptScatter:   0.1,
		meanGhostPt: 1e-100,

		// voronoi areas ---
		effectiveRfact: 1.0,
		etaRangeMap:    make(map[float64]float64),
	}

	err = tsk.DeclProp("Input", &tsk.input)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("Rho", &tsk.rho)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("Output", &tsk.output)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("JetAlgorithm", &tsk.jetAlg)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("ParameterR", &tsk.paramR)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("JetPtMin", &tsk.jetPtMin)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("ConeRadius", &tsk.coneRadius)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("SeedThreshold", &tsk.seedThreshold)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("ConeAreaFraction", &tsk.coneAreaFraction)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("MaxIterations", &tsk.maxIters)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("MaxPairSize", &tsk.maxPairSize)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("Iratch", &tsk.iratch)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("AdjacencyCut", &tsk.adjacencyCut)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("OverlapThreshold", &tsk.overlapThreshold)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("AreaAlgorithm", &tsk.areaAlg)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("ComputeRho", &tsk.computeRho)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("GhostEtaMax", &tsk.ghostEtaMax)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("Repeat", &tsk.repeat)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("GhostArea", &tsk.ghostArea)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("GridScatter", &tsk.gridScatter)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("PtScatter", &tsk.ptScatter)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("MeanGhostPt", &tsk.meanGhostPt)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("EffectiveRfact", &tsk.effectiveRfact)
	if err != nil {
		return nil, err
	}

	err = tsk.DeclProp("RhoEtaRange", &tsk.etaRangeMap)
	if err != nil {
		return nil, err
	}

	return tsk, err
}

func init() {
	fwk.Register(reflect.TypeFor[FastJetFinder](), newFastJetFinder)
}
