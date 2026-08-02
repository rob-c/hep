// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fastjet

// briefJet is the minimal per-particle state the N2Plain strategy needs:
// the rapidity-phi position, the algorithm's scale, and a cached nearest
// neighbour. Caching the neighbour is what turns the O(N^3) triple loop of
// the N3Dumb strategy into O(N^2): each recombination invalidates only the
// handful of particles that pointed at one of the two jets it consumed.
type briefJet struct {
	rap    float64 // rapidity
	phi    float64 // azimuth
	kt2    float64 // jetScaleForAlgorithm
	nnDist float64 // squared rapidity-phi distance to nn, capped at R^2
	nn     int     // slot of the nearest neighbour, or -1 for the beam
	jetIdx int     // index into ClusterSequence.jets
}

// runN2Plain clusters with the plain O(N^2) strategy.
//
// The N3Dumb strategy re-scans every pair at every step, which costs O(N^3)
// overall — tolerable for the few dozen particles of a parton-level event,
// but not for the several thousand a ghosted area calculation adds. This
// keeps a nearest-neighbour cache instead and only repairs the entries a
// recombination actually invalidates.
//
// It implements the rapidity-phi (hadron collider) algorithms. The e+e-
// algorithms measure angles rather than rapidity-phi distances and stay with
// the N3Dumb strategy.
func (cs *ClusterSequence) runN2Plain() error {
	n := len(cs.jets)
	if n == 0 {
		return nil
	}

	bj := make([]briefJet, n)
	for i := range bj {
		bj[i] = cs.briefJetOf(i)
	}

	// initial nearest neighbours: every pair once, updating both ends.
	for i := range bj {
		for k := range i {
			d := bjDist(&bj[i], &bj[k])
			if d < bj[i].nnDist {
				bj[i].nnDist, bj[i].nn = d, k
			}
			if d < bj[k].nnDist {
				bj[k].nnDist, bj[k].nn = d, i
			}
		}
	}

	// diJ[i] is the recombination distance of slot i with its neighbour,
	// still to be divided by R^2. A slot with no neighbour inside R keeps
	// nnDist == R^2, so its diJ is exactly the beam distance and the two
	// cases need no separate bookkeeping.
	diJ := make([]float64, n)
	setDiJ := func(i int) {
		kt2 := bj[i].kt2
		if nn := bj[i].nn; nn >= 0 && bj[nn].kt2 < kt2 {
			kt2 = bj[nn].kt2
		}
		diJ[i] = bj[i].nnDist * kt2
	}
	for i := range bj {
		setDiJ(i)
	}

	stale := make([]bool, n)

	for n > 0 {
		best := 0
		for i := 1; i < n; i++ {
			if diJ[i] < diJ[best] {
				best = i
			}
		}
		dmin := diJ[best] * cs.invR2

		// Anything whose neighbour is about to be consumed must look for a
		// new one. Record it before the slots move.
		clear(stale[:n])
		var lo, hi int
		switch nn := bj[best].nn; {
		case nn >= 0:
			lo, hi = best, nn
			if lo > hi {
				lo, hi = hi, lo
			}
		default:
			lo, hi = best, best
		}
		for i := range n {
			if nn := bj[i].nn; nn == lo || nn == hi {
				stale[i] = true
			}
		}

		newSlot := -1
		switch {
		case lo != hi:
			k, err := cs.ijRecombinationStep(bj[lo].jetIdx, bj[hi].jetIdx, dmin)
			if err != nil {
				return err
			}
			// the product of the recombination takes the lower slot; the
			// upper one is vacated below.
			bj[lo] = cs.briefJetOf(k)
			stale[lo] = false
			newSlot = lo
		default:
			err := cs.ibRecombinationStep(bj[best].jetIdx, dmin)
			if err != nil {
				return err
			}
		}

		// Vacate slot hi (the beam case vacates best itself) by moving the
		// tail into it, and redirect the neighbours that pointed at the tail.
		// newSlot needs no such fixup: it is lo, and lo < hi <= n after the
		// decrement, so the tail is never the jet just created.
		n--
		if hi != n {
			bj[hi], diJ[hi], stale[hi] = bj[n], diJ[n], stale[n]
			for i := range n {
				if bj[i].nn == n {
					bj[i].nn = hi
				}
			}
		}

		for i := range n {
			if !stale[i] {
				continue
			}
			bj[i].nnDist, bj[i].nn = cs.r2, -1
			for k := range n {
				if k == i {
					continue
				}
				if d := bjDist(&bj[i], &bj[k]); d < bj[i].nnDist {
					bj[i].nnDist, bj[i].nn = d, k
				}
			}
			setDiJ(i)
		}

		// The jet just created may be closer to some slot than whatever
		// that slot currently points at — and vice versa.
		if newSlot >= 0 {
			for i := range n {
				if i == newSlot {
					continue
				}
				d := bjDist(&bj[i], &bj[newSlot])
				if d < bj[i].nnDist {
					bj[i].nnDist, bj[i].nn = d, newSlot
					setDiJ(i)
				}
				if d < bj[newSlot].nnDist {
					bj[newSlot].nnDist, bj[newSlot].nn = d, i
				}
			}
			setDiJ(newSlot)
		}
	}
	return nil
}

// briefJetOf snapshots cs.jets[idx] into the clustering state, with no
// nearest neighbour yet assigned.
func (cs *ClusterSequence) briefJetOf(idx int) briefJet {
	jet := &cs.jets[idx]
	return briefJet{
		rap:    jet.Rapidity(),
		phi:    jet.Phi(),
		kt2:    cs.jetScaleForAlgorithm(jet),
		nnDist: cs.r2,
		nn:     -1,
		jetIdx: idx,
	}
}

// bjDist is the squared rapidity-azimuth distance between two slots.
func bjDist(a, b *briefJet) float64 {
	return deltaR2(a.rap, a.phi, b.rap, b.phi)
}
