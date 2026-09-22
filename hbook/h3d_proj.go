// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hbook

import "fmt"

// Outflow2D returns the index in Binning2D.Outflows of the region a point
// sits in, given where it falls on each axis: -1 below, 0 inside, +1 above.
//
// Outflow2D panics if asked for (0,0), which is not an outflow.
func Outflow2D(sx, sy int) int {
	switch {
	case sx == -1 && sy == +1:
		return BngNW - 1
	case sx == 0 && sy == +1:
		return BngN - 1
	case sx == +1 && sy == +1:
		return BngNE - 1
	case sx == +1 && sy == 0:
		return BngE - 1
	case sx == +1 && sy == -1:
		return BngSE - 1
	case sx == 0 && sy == -1:
		return BngS - 1
	case sx == -1 && sy == -1:
		return BngSW - 1
	case sx == -1 && sy == 0:
		return BngW - 1
	}
	panic("hbook: (0,0) is the binning, not an outflow")
}

// edges turns a slice of 1-dim bins into the edges they span.
func edges(bins []Bin1D) []float64 {
	o := make([]float64, 0, len(bins)+1)
	for i, bin := range bins {
		if i == 0 {
			o = append(o, bin.Range.Min)
		}
		o = append(o, bin.Range.Max)
	}
	return o
}

// XEdges returns the edges of the x-axis of this histogram.
func (h *H3D) XEdges() []float64 { return edges(h.Binning.XEdges) }

// YEdges returns the edges of the y-axis of this histogram.
func (h *H3D) YEdges() []float64 { return edges(h.Binning.YEdges) }

// ZEdges returns the edges of the z-axis of this histogram.
func (h *H3D) ZEdges() []float64 { return edges(h.Binning.ZEdges) }

// axes names a pair of the three axes of a 3-dim histogram.
type axes struct {
	a, b  int // 0: x, 1: y, 2: z
	edges func(h *H3D) []float64
}

// ProjectionXY returns the 2-dim histogram this one becomes when summed over
// its z axis.
//
// The entries that missed the binning follow where they fell on the two axes
// that survive: one that was inside both of them, and only outside z, has no
// bin of its own to go to and is counted in the projection's overall
// distribution alone.
func (h *H3D) ProjectionXY() *H2D { return h.project(0, 1) }

// ProjectionXZ returns the 2-dim histogram this one becomes when summed over
// its y axis. See ProjectionXY for how the outflows are carried over.
func (h *H3D) ProjectionXZ() *H2D { return h.project(0, 2) }

// ProjectionYZ returns the 2-dim histogram this one becomes when summed over
// its x axis. See ProjectionXY for how the outflows are carried over.
func (h *H3D) ProjectionYZ() *H2D { return h.project(1, 2) }

// dist1D returns the component of a 3-dim distribution along axis i.
func dist1D(d *Dist3D, i int) *Dist1D {
	switch i {
	case 0:
		return &d.X
	case 1:
		return &d.Y
	}
	return &d.Z
}

// crossTerm returns the 2nd-order cross-term of a 3-dim distribution over
// the axes a and b.
func crossTerm(d *Dist3D, a, b int) float64 {
	switch {
	case a == 0 && b == 1:
		return d.Stats.SumWXY
	case a == 0 && b == 2:
		return d.Stats.SumWXZ
	}
	return d.Stats.SumWYZ
}

func (h *H3D) axisEdges(i int) []float64 {
	switch i {
	case 0:
		return h.XEdges()
	case 1:
		return h.YEdges()
	}
	return h.ZEdges()
}

// project sums this histogram over the axis that is neither a nor b.
func (h *H3D) project(a, b int) *H2D {
	o := NewH2DFromEdges(h.axisEdges(a), h.axisEdges(b))
	o.Ann = h.Ann.clone()

	var (
		n  = [3]int{h.Binning.Nx, h.Binning.Ny, h.Binning.Nz}
		na = n[a]
	)

	for iz := range n[2] {
		for iy := range n[1] {
			for ix := range n[0] {
				var (
					idx = [3]int{ix, iy, iz}
					src = &h.Binning.Bins[h.Binning.binIndex(ix, iy, iz)].Dist
					dst = &o.Binning.Bins[idx[b]*na+idx[a]].Dist
				)
				dst.X.addScaled(1, 1, *dist1D(src, a))
				dst.Y.addScaled(1, 1, *dist1D(src, b))
				dst.Stats.SumWXY += crossTerm(src, min(a, b), max(a, b))
			}
		}
	}

	// the overall distribution keeps everything, outflows included.
	o.Binning.Dist.X.addScaled(1, 1, *dist1D(&h.Binning.Dist, a))
	o.Binning.Dist.Y.addScaled(1, 1, *dist1D(&h.Binning.Dist, b))
	o.Binning.Dist.Stats.SumWXY += crossTerm(&h.Binning.Dist, min(a, b), max(a, b))

	for sx := -1; sx <= +1; sx++ {
		for sy := -1; sy <= +1; sy++ {
			for sz := -1; sz <= +1; sz++ {
				if sx == 0 && sy == 0 && sz == 0 {
					continue
				}
				var (
					s   = [3]int{sx, sy, sz}
					src = &h.Binning.Outflows[Outflow3D(sx, sy, sz)]
				)
				if s[a] == 0 && s[b] == 0 {
					// inside both surviving axes: the projection has no
					// outflow for it, and no way of telling which bin it
					// belongs to either.
					continue
				}
				dst := &o.Binning.Outflows[Outflow2D(s[a], s[b])]
				dst.X.addScaled(1, 1, *dist1D(src, a))
				dst.Y.addScaled(1, 1, *dist1D(src, b))
				dst.Stats.SumWXY += crossTerm(src, min(a, b), max(a, b))
			}
		}
	}

	return o
}

// ProjectionX returns the 1-dim histogram this one becomes when summed over
// its y and z axes.
func (h *H3D) ProjectionX() *H1D { return h.project1D(0) }

// ProjectionY returns the 1-dim histogram this one becomes when summed over
// its x and z axes.
func (h *H3D) ProjectionY() *H1D { return h.project1D(1) }

// ProjectionZ returns the 1-dim histogram this one becomes when summed over
// its x and y axes.
func (h *H3D) ProjectionZ() *H1D { return h.project1D(2) }

func (h *H3D) project1D(a int) *H1D {
	o := NewH1DFromEdges(h.axisEdges(a))
	o.Ann = h.Ann.clone()

	n := [3]int{h.Binning.Nx, h.Binning.Ny, h.Binning.Nz}
	for iz := range n[2] {
		for iy := range n[1] {
			for ix := range n[0] {
				var (
					idx = [3]int{ix, iy, iz}
					src = &h.Binning.Bins[h.Binning.binIndex(ix, iy, iz)].Dist
				)
				o.Binning.Bins[idx[a]].Dist.addScaled(1, 1, *dist1D(src, a))
			}
		}
	}

	o.Binning.Dist.addScaled(1, 1, *dist1D(&h.Binning.Dist, a))

	for sx := -1; sx <= +1; sx++ {
		for sy := -1; sy <= +1; sy++ {
			for sz := -1; sz <= +1; sz++ {
				if sx == 0 && sy == 0 && sz == 0 {
					continue
				}
				var (
					s   = [3]int{sx, sy, sz}
					src = &h.Binning.Outflows[Outflow3D(sx, sy, sz)]
				)
				switch s[a] {
				case -1:
					o.Binning.Outflows[0].addScaled(1, 1, *dist1D(src, a))
				case +1:
					o.Binning.Outflows[1].addScaled(1, 1, *dist1D(src, a))
				}
			}
		}
	}

	return o
}

// SliceXY returns the 2-dim histogram made of the bins of this one at index
// iz along z, counting from zero.
//
// SliceXY returns an error if iz names no bin. A slice holds no outflows:
// there is nothing outside a single plane of bins.
func (h *H3D) SliceXY(iz int) (*H2D, error) {
	if iz < 0 || iz >= h.Binning.Nz {
		return nil, fmt.Errorf("hbook: no z-bin %d in a histogram with %d of them", iz, h.Binning.Nz)
	}

	o := NewH2DFromEdges(h.XEdges(), h.YEdges())
	o.Ann = h.Ann.clone()

	for iy := range h.Binning.Ny {
		for ix := range h.Binning.Nx {
			var (
				src = &h.Binning.Bins[h.Binning.binIndex(ix, iy, iz)].Dist
				dst = &o.Binning.Bins[iy*h.Binning.Nx+ix].Dist
			)
			dst.X.addScaled(1, 1, src.X)
			dst.Y.addScaled(1, 1, src.Y)
			dst.Stats.SumWXY += src.Stats.SumWXY

			o.Binning.Dist.X.addScaled(1, 1, src.X)
			o.Binning.Dist.Y.addScaled(1, 1, src.Y)
			o.Binning.Dist.Stats.SumWXY += src.Stats.SumWXY
		}
	}

	return o, nil
}
