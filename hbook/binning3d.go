// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hbook

import "sort"

// NumOutflows3D is the number of outflow regions around a 3-dim binning.
//
// A point that misses every bin is below, inside or above the range of each
// of the three axes, which is 3x3x3 possibilities. One of them — inside on
// all three — is the binning itself, leaving 26 outflows.
const NumOutflows3D = 26

// outflow3D returns the index in Binning3D.Outflows of the region a point
// sits in, given where it falls on each axis: -1 below, 0 inside, +1 above.
//
// outflow3D panics if asked for (0,0,0), which is not an outflow.
func outflow3D(sx, sy, sz int) int {
	i := (sx+1)*9 + (sy+1)*3 + (sz + 1)
	switch {
	case i == 13:
		panic("hbook: (0,0,0) is the binning, not an outflow")
	case i > 13:
		// skip the slot the binning itself would have taken.
		return i - 1
	}
	return i
}

// axisSide3D turns the index Bin1Ds.IndexOf returned into the side of the
// axis the value fell on: -1 below, 0 inside, +1 above.
func axisSide3D(i int) int {
	switch i {
	case UnderflowBin1D:
		return -1
	case OverflowBin1D:
		return +1
	}
	return 0
}

// Binning3D is a 3-dim binning of the x-y-z space.
type Binning3D struct {
	Bins     []Bin3D
	Dist     Dist3D
	Outflows [NumOutflows3D]Dist3D
	XRange   Range
	YRange   Range
	ZRange   Range
	Nx       int
	Ny       int
	Nz       int
	XEdges   []Bin1D
	YEdges   []Bin1D
	ZEdges   []Bin1D
}

func newBinning3D(nx int, xlow, xhigh float64, ny int, ylow, yhigh float64, nz int, zlow, zhigh float64) Binning3D {
	switch {
	case xlow >= xhigh:
		panic(errInvalidXAxis)
	case ylow >= yhigh:
		panic(errInvalidYAxis)
	case zlow >= zhigh:
		panic(errInvalidZAxis)
	case nx <= 0:
		panic(errEmptyXAxis)
	case ny <= 0:
		panic(errEmptyYAxis)
	case nz <= 0:
		panic(errEmptyZAxis)
	}

	bng := Binning3D{
		Bins:   make([]Bin3D, nx*ny*nz),
		XRange: Range{Min: xlow, Max: xhigh},
		YRange: Range{Min: ylow, Max: yhigh},
		ZRange: Range{Min: zlow, Max: zhigh},
		Nx:     nx,
		Ny:     ny,
		Nz:     nz,
		XEdges: make([]Bin1D, nx),
		YEdges: make([]Bin1D, ny),
		ZEdges: make([]Bin1D, nz),
	}

	var (
		xwidth = bng.XRange.Width() / float64(bng.Nx)
		ywidth = bng.YRange.Width() / float64(bng.Ny)
		zwidth = bng.ZRange.Width() / float64(bng.Nz)
	)

	for ix := range bng.XEdges {
		bng.XEdges[ix].Range.Min = xlow + float64(ix)*xwidth
		bng.XEdges[ix].Range.Max = xlow + float64(ix+1)*xwidth
	}
	for iy := range bng.YEdges {
		bng.YEdges[iy].Range.Min = ylow + float64(iy)*ywidth
		bng.YEdges[iy].Range.Max = ylow + float64(iy+1)*ywidth
	}
	for iz := range bng.ZEdges {
		bng.ZEdges[iz].Range.Min = zlow + float64(iz)*zwidth
		bng.ZEdges[iz].Range.Max = zlow + float64(iz+1)*zwidth
	}
	bng.setBinRanges()

	return bng
}

func newBinning3DFromEdges(xedges, yedges, zedges []float64) Binning3D {
	switch {
	case len(xedges) <= 1:
		panic(errShortXAxis)
	case !sort.IsSorted(sort.Float64Slice(xedges)):
		panic(errNotSortedXAxis)
	case len(yedges) <= 1:
		panic(errShortYAxis)
	case !sort.IsSorted(sort.Float64Slice(yedges)):
		panic(errNotSortedYAxis)
	case len(zedges) <= 1:
		panic(errShortZAxis)
	case !sort.IsSorted(sort.Float64Slice(zedges)):
		panic(errNotSortedZAxis)
	}

	var (
		nx = len(xedges) - 1
		ny = len(yedges) - 1
		nz = len(zedges) - 1
	)

	bng := Binning3D{
		Bins:   make([]Bin3D, nx*ny*nz),
		XRange: Range{Min: xedges[0], Max: xedges[nx]},
		YRange: Range{Min: yedges[0], Max: yedges[ny]},
		ZRange: Range{Min: zedges[0], Max: zedges[nz]},
		Nx:     nx,
		Ny:     ny,
		Nz:     nz,
		XEdges: make([]Bin1D, nx),
		YEdges: make([]Bin1D, ny),
		ZEdges: make([]Bin1D, nz),
	}

	for i, edges := range [][]float64{xedges, yedges, zedges} {
		var (
			dst = [][]Bin1D{bng.XEdges, bng.YEdges, bng.ZEdges}[i]
			dup = []error{errDupEdgesXAxis, errDupEdgesYAxis, errDupEdgesZAxis}[i]
		)
		for j := range dst {
			min, max := edges[j], edges[j+1]
			if min == max {
				panic(dup)
			}
			dst[j].Range.Min = min
			dst[j].Range.Max = max
		}
	}
	bng.setBinRanges()

	return bng
}

// setBinRanges gives every bin the ranges its position on the three axes
// implies. Bins are laid out x-fastest, then y, then z.
func (bng *Binning3D) setBinRanges() {
	for iz := range bng.ZEdges {
		for iy := range bng.YEdges {
			for ix := range bng.XEdges {
				bin := &bng.Bins[bng.binIndex(ix, iy, iz)]
				bin.XRange = bng.XEdges[ix].Range
				bin.YRange = bng.YEdges[iy].Range
				bin.ZRange = bng.ZEdges[iz].Range
			}
		}
	}
}

// binIndex returns the index in Bins of the bin at (ix,iy,iz).
func (bng *Binning3D) binIndex(ix, iy, iz int) int {
	return (iz*bng.Ny+iy)*bng.Nx + ix
}

// clone returns a deep copy of the binning. Bin3D, Bin1D and Dist3D hold no
// reference types, so copying the slices copies everything.
func (bng *Binning3D) clone() Binning3D {
	o := Binning3D{
		Bins:     make([]Bin3D, len(bng.Bins)),
		Dist:     bng.Dist,
		Outflows: bng.Outflows,
		XRange:   bng.XRange,
		YRange:   bng.YRange,
		ZRange:   bng.ZRange,
		Nx:       bng.Nx,
		Ny:       bng.Ny,
		Nz:       bng.Nz,
		XEdges:   make([]Bin1D, len(bng.XEdges)),
		YEdges:   make([]Bin1D, len(bng.YEdges)),
		ZEdges:   make([]Bin1D, len(bng.ZEdges)),
	}
	copy(o.Bins, bng.Bins)
	copy(o.XEdges, bng.XEdges)
	copy(o.YEdges, bng.YEdges)
	copy(o.ZEdges, bng.ZEdges)
	return o
}

func (bng *Binning3D) entries() int64 {
	return bng.Dist.Entries()
}

func (bng *Binning3D) effEntries() float64 {
	return bng.Dist.EffEntries()
}

// xMin returns the low edge of the X-axis
func (bng *Binning3D) xMin() float64 { return bng.XRange.Min }

// xMax returns the high edge of the X-axis
func (bng *Binning3D) xMax() float64 { return bng.XRange.Max }

// yMin returns the low edge of the Y-axis
func (bng *Binning3D) yMin() float64 { return bng.YRange.Min }

// yMax returns the high edge of the Y-axis
func (bng *Binning3D) yMax() float64 { return bng.YRange.Max }

// zMin returns the low edge of the Z-axis
func (bng *Binning3D) zMin() float64 { return bng.ZRange.Min }

// zMax returns the high edge of the Z-axis
func (bng *Binning3D) zMax() float64 { return bng.ZRange.Max }

func (bng *Binning3D) fill(x, y, z, w float64) {
	idx := bng.coordToIndex(x, y, z)
	bng.Dist.fill(x, y, z, w)
	switch {
	case idx == len(bng.Bins):
		// GAP bin: the point is inside the axes but inside no bin.
		return
	case idx < 0:
		bng.Outflows[-idx-1].fill(x, y, z, w)
	default:
		bng.Bins[idx].fill(x, y, z, w)
	}
}

// coordToIndex returns the index in Bins of the bin holding (x,y,z), or a
// negative value -(o+1) naming the outflow o the point landed in, or
// len(Bins) when the point fell in a gap of the binning.
func (bng *Binning3D) coordToIndex(x, y, z float64) int {
	var (
		ix = Bin1Ds(bng.XEdges).IndexOf(x)
		iy = Bin1Ds(bng.YEdges).IndexOf(y)
		iz = Bin1Ds(bng.ZEdges).IndexOf(z)
	)

	// a gap on any one axis is a gap: the point is within that axis' range
	// and yet in none of its bins, so there is no bin to put it in.
	if ix == bng.Nx || iy == bng.Ny || iz == bng.Nz {
		return len(bng.Bins)
	}

	sx, sy, sz := axisSide3D(ix), axisSide3D(iy), axisSide3D(iz)
	if sx == 0 && sy == 0 && sz == 0 {
		return bng.binIndex(ix, iy, iz)
	}

	return -(outflow3D(sx, sy, sz) + 1)
}
