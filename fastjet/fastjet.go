// Copyright ©2017 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fastjet is a Go-based implementation of the C++ FastJet library.
//
// # Clustering
//
// A clustering is described by a JetDefinition: an algorithm (kt, anti-kt,
// Cambridge/Aachen, ...), a radius, a recombination scheme, and a strategy.
// NewClusterSequence runs it over a slice of Jets and hands back the sequence
// the jets can be read out of, either inclusively (every jet above a transverse
// momentum) or exclusively (clustering stopped at a given scale or jet count).
//
// The strategy only decides how long the clustering takes, never what it
// returns. BestStrategy, the usual choice, runs the O(N^2) algorithm for the
// hadron-collider jet definitions and falls back to the naive O(N^3) one for
// the e+e- ones, which measure opening angles rather than rapidity-azimuth
// distances.
//
// # Jet areas
//
// NewClusterSequenceArea clusters an event and measures how much
// rapidity-azimuth area each jet occupies, by filling the event with ghosts:
// particles so soft they cannot change how the real ones cluster, but which
// mark out the region each jet swept up. Areas are what turn a jet's transverse
// momentum into something correctable for pile-up and underlying event.
//
// # Background subtraction
//
// NewJetMedianBackgroundEstimator measures the diffuse background of an event
// as the median of pt/area over its jets, and subtracts it from a jet with
// Subtract. It needs an ActiveAreaExplicitGhosts area, so that the empty
// regions of the event -- the ones carrying no background at all -- are counted
// in the median rather than dropped.
package fastjet // import "go-hep.org/x/hep/fastjet"
