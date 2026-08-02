// Copyright ©2017 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fastjet

import (
	"fmt"
	"math"
	"math/rand/v2"

	"go-hep.org/x/hep/fmom"
)

// ghostInfo marks a Jet as a ghost: an infinitely soft particle added to the
// event purely so that the region it lands in can be attributed to a jet.
// It travels in Jet.UserInfo, which survives the copy the clustering makes
// of every input particle.
type ghostInfo struct{}

// isGhost reports whether jet is one of the ghosts added to measure areas.
func isGhost(jet *Jet) bool {
	_, ok := jet.UserInfo.(ghostInfo)
	return ok
}

// areaJet is what a repeated ghost run remembers about one of its jets: just
// enough to match it back to the corresponding jet of the reference run.
type areaJet struct {
	rap   float64
	phi   float64
	area  float64
	area4 fmom.PxPyPzE
}

// ClusterSequenceArea clusters an event and measures how much rapidity-azimuth
// area each of the resulting jets occupies.
//
// The area is what turns a jet's transverse momentum into something that can
// be corrected for pile-up and underlying event: subtracting rho times the
// area removes, on average, the diffuse background the jet swept up. See
// JetMedianBackgroundEstimator for the rho that goes with it.
//
// Areas are measured by filling the event with ghosts -- particles so soft
// they cannot change how the real ones cluster -- and counting how many of
// them each jet catches. That means a ClusterSequenceArea clusters several
// thousand extra particles, and its jets, while numerically the same as the
// ones a plain ClusterSequence would give, are reached by a good deal more
// work.
type ClusterSequenceArea struct {
	cs   *ClusterSequence
	def  JetDefinition
	area AreaDefinition
	spec GhostedAreaSpec // normalized: defaults filled in
	grid ghostGrid

	// repeats holds the jets of the ghost ensembles beyond the first, used
	// to average areas and to give them an uncertainty. It is empty unless
	// the spec asks for more than one repetition.
	repeats [][]areaJet
}

// NewClusterSequenceArea clusters jets according to def and measures their
// areas according to area.
//
// The zero AreaDefinition asks for an active area with the default ghosts,
// which is the right choice for pile-up subtraction.
func NewClusterSequenceArea(jets []Jet, def JetDefinition, area AreaDefinition) (*ClusterSequenceArea, error) {
	switch area.typ {
	case ActiveArea, ActiveAreaExplicitGhosts:
		// ok
	default:
		return nil, fmt.Errorf("fastjet: %v is not implemented", area.typ)
	}

	if !def.Algorithm().usesRapPhi() {
		return nil, fmt.Errorf(
			"fastjet: %v does not measure distances in rapidity-azimuth, so it has no area there",
			def.Algorithm(),
		)
	}

	spec, err := area.ghost.normalize()
	if err != nil {
		return nil, err
	}

	csa := &ClusterSequenceArea{
		def:  def,
		area: area,
		spec: spec,
		grid: spec.grid(),
	}

	// The ghosts multiply the particle count by a factor of a hundred or
	// more, which the O(N^3) strategy cannot absorb. The strategy does not
	// change the answer, only the time it takes to reach it, so the ghosted
	// clustering picks its own regardless of what the caller asked for.
	gdef := NewJetDefinitionExtra(
		def.Algorithm(), def.R(), def.RecombinationScheme(), BestStrategy, def.ExtraParam(),
	)

	for i := range spec.Repeat {
		cs, err := csa.cluster(jets, gdef, spec.Seed+uint64(i))
		if err != nil {
			return nil, err
		}
		if i == 0 {
			csa.cs = cs
			continue
		}
		snap, err := csa.snapshot(cs)
		if err != nil {
			return nil, err
		}
		csa.repeats = append(csa.repeats, snap)
	}

	return csa, nil
}

// cluster runs the clustering on the event plus one ensemble of ghosts.
func (csa *ClusterSequenceArea) cluster(jets []Jet, def JetDefinition, seed uint64) (*ClusterSequence, error) {
	all := make([]Jet, 0, len(jets)+csa.grid.n())
	all = append(all, jets...)
	all = csa.appendGhosts(all, seed)
	return NewClusterSequence(all, def)
}

// appendGhosts adds one ensemble of ghosts to jets. They sit on a
// rapidity-azimuth lattice, jittered so that no two of them share a
// coordinate, with transverse momenta far below any physical scale.
func (csa *ClusterSequenceArea) appendGhosts(jets []Jet, seed uint64) []Jet {
	var (
		g    = csa.grid
		spec = csa.spec
		rnd  = rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	)

	for irap := -g.nrap; irap <= g.nrap; irap++ {
		for iphi := range g.nphi {
			var (
				rap = float64(irap)*g.drap + g.drap*spec.GridScatter*(rnd.Float64()-0.5)
				phi = (float64(iphi)+0.5)*g.dphi + g.dphi*spec.GridScatter*(rnd.Float64()-0.5)
				pt  = spec.MeanGhostPt * (1 + (2*rnd.Float64()-1)*spec.PtScatter)
			)
			ghost := NewJet(
				pt*math.Cos(phi),
				pt*math.Sin(phi),
				pt*math.Sinh(rap),
				pt*math.Cosh(rap),
			)
			ghost.UserInfo = ghostInfo{}
			jets = append(jets, ghost)
		}
	}
	return jets
}

// snapshot records the jets of a repeated ghost run, so that the areas of the
// reference run can be averaged against them.
func (csa *ClusterSequenceArea) snapshot(cs *ClusterSequence) ([]areaJet, error) {
	jets, err := cs.InclusiveJets(0)
	if err != nil {
		return nil, err
	}

	out := make([]areaJet, 0, len(jets))
	for i := range jets {
		jet := &jets[i]
		cts, err := cs.Constituents(jet)
		if err != nil {
			return nil, err
		}
		n, p4 := csa.ghostContent(cts)
		if n == len(cts) {
			// a jet built out of nothing but ghosts marks out an empty
			// region: it is not a counterpart of any jet of the reference run
			continue
		}
		out = append(out, areaJet{
			rap:   jet.Rapidity(),
			phi:   jet.Phi(),
			area:  float64(n) * csa.grid.area,
			area4: p4,
		})
	}
	return out, nil
}

// ghostContent returns how many of the constituents are ghosts, and the
// four-vector area they map out: each ghost contributes its direction, as a
// unit-transverse-momentum four-vector, weighted by the area it stands for.
func (csa *ClusterSequenceArea) ghostContent(cts []Jet) (int, fmom.PxPyPzE) {
	var (
		n              int
		px, py, pz, ee float64
	)
	for i := range cts {
		ct := &cts[i]
		if !isGhost(ct) {
			continue
		}
		n++
		pt := ct.Pt()
		if pt == 0 {
			continue
		}
		w := csa.grid.area / pt
		px += w * ct.Px()
		py += w * ct.Py()
		pz += w * ct.Pz()
		ee += w * ct.E()
	}
	return n, fmom.NewPxPyPzE(px, py, pz, ee)
}

// knows reports whether jet came out of this ClusterSequenceArea, and so has
// an area to report.
func (csa *ClusterSequenceArea) knows(jet *Jet) bool {
	return jet != nil && 0 <= jet.hidx && jet.hidx < len(csa.cs.history)
}

// areasOf returns the area of jet in the reference ghost run, followed by its
// area in each further run, matched by proximity in rapidity-azimuth.
func (csa *ClusterSequenceArea) areasOf(jet *Jet) []areaJet {
	if !csa.knows(jet) {
		return nil
	}

	cts, err := csa.cs.Constituents(jet)
	if err != nil {
		return nil
	}
	n, p4 := csa.ghostContent(cts)

	out := make([]areaJet, 1, 1+len(csa.repeats))
	out[0] = areaJet{
		rap:   jet.Rapidity(),
		phi:   jet.Phi(),
		area:  float64(n) * csa.grid.area,
		area4: p4,
	}

	// A jet of one run corresponds to a jet of another when they sit on top
	// of each other: the ghosts move between runs, the hard particles do not.
	// Beyond one jet radius there is no correspondence to speak of, and the
	// run simply does not contribute.
	for _, rep := range csa.repeats {
		best, dmin := -1, csa.cs.r2
		for i := range rep {
			d := deltaR2(jet.Rapidity(), jet.Phi(), rep[i].rap, rep[i].phi)
			if d < dmin {
				best, dmin = i, d
			}
		}
		if best >= 0 {
			out = append(out, rep[best])
		}
	}
	return out
}

// Area returns the rapidity-azimuth area of jet, averaged over the ghost
// ensembles. It returns 0 for a jet this ClusterSequenceArea did not produce.
func (csa *ClusterSequenceArea) Area(jet *Jet) float64 {
	areas := csa.areasOf(jet)
	if len(areas) == 0 {
		return 0
	}
	var sum float64
	for _, a := range areas {
		sum += a.area
	}
	return sum / float64(len(areas))
}

// AreaErr returns the uncertainty on Area, taken from the scatter between
// ghost ensembles. It is 0 unless the GhostedAreaSpec asked for more than one
// repetition, since a single ensemble says nothing about its own spread.
func (csa *ClusterSequenceArea) AreaErr(jet *Jet) float64 {
	areas := csa.areasOf(jet)
	if len(areas) < 2 {
		return 0
	}

	var sum, sum2 float64
	for _, a := range areas {
		sum += a.area
		sum2 += a.area * a.area
	}
	var (
		n    = float64(len(areas))
		mean = sum / n
		v    = sum2/n - mean*mean
	)
	if v <= 0 {
		return 0
	}
	return math.Sqrt(v / (n - 1))
}

// Area4Vector returns the four-vector area of jet, averaged over the ghost
// ensembles. Unlike the scalar area it knows which way the jet's area points,
// which matters for massive jets and for subtracting a background from a jet
// whose area is lopsided.
func (csa *ClusterSequenceArea) Area4Vector(jet *Jet) fmom.PxPyPzE {
	areas := csa.areasOf(jet)
	if len(areas) == 0 {
		return fmom.NewPxPyPzE(0, 0, 0, 0)
	}

	var px, py, pz, ee float64
	for _, a := range areas {
		px += a.area4.Px()
		py += a.area4.Py()
		pz += a.area4.Pz()
		ee += a.area4.E()
	}
	n := float64(len(areas))
	return fmom.NewPxPyPzE(px/n, py/n, pz/n, ee/n)
}

// IsPureGhost reports whether jet is made of ghosts alone, and so marks out an
// empty region of the event rather than a real jet.
func (csa *ClusterSequenceArea) IsPureGhost(jet *Jet) bool {
	if !csa.knows(jet) {
		return false
	}
	cts, err := csa.cs.Constituents(jet)
	if err != nil || len(cts) == 0 {
		return false
	}
	for i := range cts {
		if !isGhost(&cts[i]) {
			return false
		}
	}
	return true
}

// Constituents returns the constituents of jet with the ghosts removed, so
// that what comes back is the list of real particles the jet was built from.
func (csa *ClusterSequenceArea) Constituents(jet *Jet) ([]Jet, error) {
	cts, err := csa.cs.Constituents(jet)
	if err != nil {
		return nil, err
	}

	out := cts[:0]
	for i := range cts {
		if !isGhost(&cts[i]) {
			out = append(out, cts[i])
		}
	}
	return out, nil
}

// InclusiveJets returns the jets with transverse momentum above ptmin.
//
// For an ActiveArea the jets made only of ghosts are left out; for an
// ActiveAreaExplicitGhosts they are kept, and IsPureGhost tells them apart.
func (csa *ClusterSequenceArea) InclusiveJets(ptmin float64) ([]Jet, error) {
	jets, err := csa.cs.InclusiveJets(ptmin)
	if err != nil {
		return nil, err
	}
	return csa.filter(jets)
}

// NumExclusiveJets returns the number of exclusive jets that clustering down
// to dcut would leave, counting the ghost jets: with several thousand ghosts
// in the event that number says very little about the event itself, and
// ExclusiveJets is the more useful entry point.
func (csa *ClusterSequenceArea) NumExclusiveJets(dcut float64) int {
	return csa.cs.NumExclusiveJets(dcut)
}

// ExclusiveJets returns the jets left by clustering down to dcut, with the
// pure-ghost ones removed unless the area definition asks for explicit ghosts.
func (csa *ClusterSequenceArea) ExclusiveJets(dcut float64) ([]Jet, error) {
	jets, err := csa.cs.ExclusiveJets(dcut)
	if err != nil {
		return nil, err
	}
	return csa.filter(jets)
}

// ExclusiveJetsUpTo returns the jets left by clustering back to njets of them,
// with the pure-ghost ones removed unless the area definition asks for
// explicit ghosts.
//
// njets counts the ghost jets too, so asking for a handful of jets in an event
// stuffed with ghosts will return far fewer than that once they are dropped.
func (csa *ClusterSequenceArea) ExclusiveJetsUpTo(njets int) ([]Jet, error) {
	jets, err := csa.cs.ExclusiveJetsUpTo(njets)
	if err != nil {
		return nil, err
	}
	return csa.filter(jets)
}

// filter drops the pure-ghost jets, unless the caller asked to see them.
func (csa *ClusterSequenceArea) filter(jets []Jet) ([]Jet, error) {
	if csa.area.typ == ActiveAreaExplicitGhosts {
		return jets, nil
	}

	out := jets[:0]
	for i := range jets {
		cts, err := csa.cs.Constituents(&jets[i])
		if err != nil {
			return nil, err
		}
		hard := false
		for j := range cts {
			if !isGhost(&cts[j]) {
				hard = true
				break
			}
		}
		if hard {
			out = append(out, jets[i])
		}
	}
	return out, nil
}

// ClusterSequence returns the underlying cluster sequence, which holds the
// ghosts as well as the real particles.
func (csa *ClusterSequenceArea) ClusterSequence() *ClusterSequence {
	return csa.cs
}

// AreaDefinition returns the definition the areas were measured with.
func (csa *ClusterSequenceArea) AreaDefinition() AreaDefinition {
	return csa.area
}

// JetDefinition returns the definition the jets were clustered with.
func (csa *ClusterSequenceArea) JetDefinition() JetDefinition {
	return csa.def
}

// NumGhosts returns how many ghosts each ensemble held.
func (csa *ClusterSequenceArea) NumGhosts() int {
	return csa.grid.n()
}

// GhostArea returns the rapidity-azimuth area a single ghost stands for. It is
// close to, but not exactly, the GhostArea that was asked for: the ghost grid
// has to fit a whole number of ghosts around the azimuth.
func (csa *ClusterSequenceArea) GhostArea() float64 {
	return csa.grid.area
}

// deltaR2 is the squared rapidity-azimuth separation, wrapping in azimuth.
func deltaR2(rap1, phi1, rap2, phi2 float64) float64 {
	dphi := math.Abs(phi1 - phi2)
	if dphi > math.Pi {
		dphi = 2*math.Pi - dphi
	}
	drap := rap1 - rap2
	return dphi*dphi + drap*drap
}
