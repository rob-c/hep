// Copyright ©2017 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fastjet

import (
	"fmt"
	"math"
)

// AreaType selects how the area of a jet is measured.
type AreaType int

const (
	// ActiveArea measures the area a jet occupies by filling the event
	// with infinitely soft "ghost" particles, clustering them along with
	// the real ones, and counting how many end up inside each jet. Jets
	// made only of ghosts are dropped from the results.
	//
	// This is the zero value, and the one to use for pile-up subtraction.
	ActiveArea AreaType = iota

	// ActiveAreaExplicitGhosts is ActiveArea, except that the jets made
	// only of ghosts are kept. Those jets map out the empty regions of the
	// event, which is what a background estimator needs in order not to
	// bias itself towards the populated ones.
	ActiveAreaExplicitGhosts

	// PassiveArea measures the area by adding a single ghost at a time, so
	// that ghosts never cluster with each other. It is not implemented.
	PassiveArea

	// VoronoiArea measures the area from the Voronoi cells of the jet
	// constituents. It is not implemented.
	VoronoiArea
)

func (typ AreaType) String() string {
	switch typ {
	case ActiveArea:
		return "ActiveArea"
	case ActiveAreaExplicitGhosts:
		return "ActiveAreaExplicitGhosts"
	case PassiveArea:
		return "PassiveArea"
	case VoronoiArea:
		return "VoronoiArea"
	default:
		panic(fmt.Errorf("fastjet: invalid AreaType (%d)", int(typ)))
	}
}

// GhostedAreaSpec describes the ghost ensemble used to measure jet areas.
//
// The zero value is usable and stands for the defaults recommended by
// FastJet: ghosts out to |y| < 6, one ensemble, and one ghost per 0.01
// units of rapidity-azimuth area.
type GhostedAreaSpec struct {
	// GhostMaxRap is the largest |rapidity| covered by ghosts. Jets beyond
	// it get no ghosts and so no area. Defaults to 6.
	GhostMaxRap float64

	// GhostArea is the rapidity-azimuth area each ghost stands for. Smaller
	// values are more accurate and quadratically slower. Defaults to 0.01.
	GhostArea float64

	// Repeat is the number of independent ghost ensembles to cluster. Areas
	// are averaged over them, and AreaErr only becomes meaningful once it is
	// greater than one. Defaults to 1.
	Repeat int

	// GridScatter is how far, as a fraction of the grid spacing, a ghost may
	// wander from its grid site. Scattering the grid stops the ghosts from
	// lining up with each other. Defaults to 1.
	GridScatter float64

	// PtScatter is the relative spread of the ghost transverse momenta.
	// Defaults to 0.1.
	PtScatter float64

	// MeanGhostPt is the mean transverse momentum of a ghost. It has to sit
	// far below any physical scale in the event so that the ghosts do not
	// perturb the clustering of the real particles. Defaults to 1e-100.
	MeanGhostPt float64

	// Seed seeds the ghost placement. Runs with the same seed produce the
	// same ghosts, so areas are reproducible. Defaults to a fixed value:
	// pass distinct seeds explicitly to get independent ensembles.
	Seed uint64
}

const (
	defaultGhostMaxRap = 6.0
	defaultGhostArea   = 0.01
	defaultGridScatter = 1.0
	defaultPtScatter   = 0.1
	defaultGhostPt     = 1e-100
	defaultGhostSeed   = 0x5eed
)

// normalize returns spec with its unset fields replaced by their defaults.
func (spec GhostedAreaSpec) normalize() (GhostedAreaSpec, error) {
	switch {
	case spec.GhostMaxRap < 0:
		return spec, fmt.Errorf("fastjet: negative GhostMaxRap (%v)", spec.GhostMaxRap)
	case spec.GhostArea < 0:
		return spec, fmt.Errorf("fastjet: negative GhostArea (%v)", spec.GhostArea)
	case spec.Repeat < 0:
		return spec, fmt.Errorf("fastjet: negative Repeat (%v)", spec.Repeat)
	case spec.MeanGhostPt < 0:
		return spec, fmt.Errorf("fastjet: negative MeanGhostPt (%v)", spec.MeanGhostPt)
	case spec.PtScatter < 0:
		return spec, fmt.Errorf("fastjet: negative PtScatter (%v)", spec.PtScatter)
	case spec.GridScatter < 0:
		return spec, fmt.Errorf("fastjet: negative GridScatter (%v)", spec.GridScatter)
	}

	if spec.GhostMaxRap == 0 {
		spec.GhostMaxRap = defaultGhostMaxRap
	}
	if spec.GhostArea == 0 {
		spec.GhostArea = defaultGhostArea
	}
	if spec.Repeat == 0 {
		spec.Repeat = 1
	}
	if spec.GridScatter == 0 {
		spec.GridScatter = defaultGridScatter
	}
	if spec.PtScatter == 0 {
		spec.PtScatter = defaultPtScatter
	}
	if spec.MeanGhostPt == 0 {
		spec.MeanGhostPt = defaultGhostPt
	}
	if spec.Seed == 0 {
		spec.Seed = defaultGhostSeed
	}
	return spec, nil
}

// ghostGrid is the rapidity-azimuth lattice the ghosts are placed on.
type ghostGrid struct {
	nrap int     // ghosts span rapidity indices [-nrap, +nrap]
	nphi int     // ghosts per rapidity ring
	drap float64 // rapidity spacing
	dphi float64 // azimuthal spacing
	area float64 // drap*dphi: the area one ghost actually stands for
}

// grid lays out the ghost lattice implied by the spec. The azimuthal spacing
// is rounded so that a whole number of ghosts fits around the ring, and the
// rapidity spacing then follows from the requested per-ghost area -- so the
// area a ghost really stands for is not exactly spec.GhostArea, which is why
// it is carried along separately.
func (spec GhostedAreaSpec) grid() ghostGrid {
	var (
		nphi = int(math.Ceil(2 * math.Pi / math.Sqrt(spec.GhostArea)))
		dphi = 2 * math.Pi / float64(nphi)
		drap = spec.GhostArea / dphi
		nrap = int(spec.GhostMaxRap / drap)
	)
	return ghostGrid{
		nrap: nrap,
		nphi: nphi,
		drap: drap,
		dphi: dphi,
		area: drap * dphi,
	}
}

// n returns the number of ghosts the grid holds.
func (g ghostGrid) n() int {
	return (2*g.nrap + 1) * g.nphi
}

// AreaDefinition says how a ClusterSequenceArea should measure jet areas.
//
// The zero value is usable and means an active area with the default ghost
// specification.
type AreaDefinition struct {
	typ   AreaType
	ghost GhostedAreaSpec
}

// NewAreaDefinition returns the area definition of the given type, measured
// with the given ghost specification. The zero GhostedAreaSpec selects the
// default ghosts.
func NewAreaDefinition(typ AreaType, ghost GhostedAreaSpec) AreaDefinition {
	return AreaDefinition{typ: typ, ghost: ghost}
}

// Type returns the kind of area this definition measures.
func (def AreaDefinition) Type() AreaType {
	return def.typ
}

// GhostSpec returns the ghost specification as it was given: unset fields
// have not been replaced by their defaults.
func (def AreaDefinition) GhostSpec() GhostedAreaSpec {
	return def.ghost
}

// Validate reports whether the area definition is one that this package can
// measure: an implemented AreaType, together with a usable ghost ensemble.
//
// NewClusterSequenceArea calls it, so a caller only needs it to fail early --
// when a configuration is read, rather than when the first event goes through.
func (def AreaDefinition) Validate() error {
	switch def.typ {
	case ActiveArea, ActiveAreaExplicitGhosts:
		// ok
	case PassiveArea, VoronoiArea:
		return fmt.Errorf("fastjet: %v is not implemented", def.typ)
	default:
		return fmt.Errorf("fastjet: invalid AreaType (%d)", int(def.typ))
	}

	_, err := def.ghost.normalize()
	return err
}

func (def AreaDefinition) String() string {
	spec, err := def.ghost.normalize()
	if err != nil {
		return fmt.Sprintf("%v(invalid ghost spec: %v)", def.typ, err)
	}
	return fmt.Sprintf("%v(ghost-area=%v, max-rap=%v, repeat=%d)",
		def.typ, spec.GhostArea, spec.GhostMaxRap, spec.Repeat,
	)
}
