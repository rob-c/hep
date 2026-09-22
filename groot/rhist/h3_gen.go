// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Automatically generated. DO NOT EDIT.

package rhist

import (
	"fmt"
	"math"
	"reflect"

	"go-hep.org/x/hep/groot/rbytes"
	"go-hep.org/x/hep/groot/rcont"
	"go-hep.org/x/hep/groot/root"
	"go-hep.org/x/hep/groot/rtypes"
	"go-hep.org/x/hep/groot/rvers"
	"go-hep.org/x/hep/hbook"
)

// H3C implements ROOT TH3C
type H3C struct {
	th3
	arr rcont.ArrayC
}

func newH3C() *H3C {
	return &H3C{
		th3: *newH3(),
	}
}

// NewH3CFrom creates a new H3C from an hbook 3-dim histogram.
func NewH3CFrom(h *hbook.H3D) *H3C {
	var (
		hroot  = newH3C()
		bins   = h.Binning.Bins
		nxbins = h.Binning.Nx
		nybins = h.Binning.Ny
		nzbins = h.Binning.Nz
		xedges = make([]float64, 0, nxbins+1)
		yedges = make([]float64, 0, nybins+1)
		zedges = make([]float64, 0, nzbins+1)
	)

	hroot.th3.th1.entries = float64(h.Entries())
	hroot.th3.th1.tsumw = h.SumW()
	hroot.th3.th1.tsumw2 = h.SumW2()
	hroot.th3.th1.tsumwx = h.SumWX()
	hroot.th3.th1.tsumwx2 = h.SumWX2()
	hroot.th3.tsumwy = h.SumWY()
	hroot.th3.tsumwy2 = h.SumWY2()
	hroot.th3.tsumwxy = h.SumWXY()
	hroot.th3.tsumwz = h.SumWZ()
	hroot.th3.tsumwz2 = h.SumWZ2()
	hroot.th3.tsumwxz = h.SumWXZ()
	hroot.th3.tsumwyz = h.SumWYZ()

	ncells := (nxbins + 2) * (nybins + 2) * (nzbins + 2)
	hroot.th3.th1.ncells = ncells

	hroot.th3.th1.xaxis.nbins = nxbins
	hroot.th3.th1.xaxis.xmin = h.XMin()
	hroot.th3.th1.xaxis.xmax = h.XMax()

	hroot.th3.th1.yaxis.nbins = nybins
	hroot.th3.th1.yaxis.xmin = h.YMin()
	hroot.th3.th1.yaxis.xmax = h.YMax()

	hroot.th3.th1.zaxis.nbins = nzbins
	hroot.th3.th1.zaxis.xmin = h.ZMin()
	hroot.th3.th1.zaxis.xmax = h.ZMax()

	hroot.arr.Data = make([]int8, ncells)
	hroot.th3.th1.sumw2.Data = make([]float64, ncells)

	ibin := func(ix, iy, iz int) int { return (iz*nybins+iy)*nxbins + ix }

	for ix := range nxbins {
		for iy := range nybins {
			for iz := range nzbins {
				bin := bins[ibin(ix, iy, iz)]
				if iy == 0 && iz == 0 {
					xedges = append(xedges, bin.XMin())
				}
				if ix == 0 && iz == 0 {
					yedges = append(yedges, bin.YMin())
				}
				if ix == 0 && iy == 0 {
					zedges = append(zedges, bin.ZMin())
				}
				hroot.setDist3D(ix+1, iy+1, iz+1, bin.Dist.SumW(), bin.Dist.SumW2())
			}
		}
	}

	// the 26 ways of missing a 3-dim binning, each landing in the slice of
	// cells ROOT keeps for it.
	for sx := -1; sx <= +1; sx++ {
		for sy := -1; sy <= +1; sy++ {
			for sz := -1; sz <= +1; sz++ {
				if sx == 0 && sy == 0 && sz == 0 {
					continue
				}
				o := h.Binning.Outflows[hbook.Outflow3D(sx, sy, sz)]
				hroot.setDist3D(
					h3cell(sx, nxbins), h3cell(sy, nybins), h3cell(sz, nzbins),
					o.SumW(), o.SumW2(),
				)
			}
		}
	}

	xedges = append(xedges, bins[ibin(nxbins-1, 0, 0)].XMax())
	yedges = append(yedges, bins[ibin(0, nybins-1, 0)].YMax())
	zedges = append(zedges, bins[ibin(0, 0, nzbins-1)].ZMax())

	hroot.th3.th1.SetName(h.Name())
	if v, ok := h.Annotation()["title"]; ok && v != nil {
		hroot.th3.th1.SetTitle(v.(string))
	}
	hroot.th3.th1.xaxis.xbins.Data = xedges
	hroot.th3.th1.yaxis.xbins.Data = yedges
	hroot.th3.th1.zaxis.xbins.Data = zedges

	return hroot
}

// NewH3C creates a 3-dim histogram, as "new TH3C(name, title,
// nx, xmin, xmax, ny, ymin, ymax, nz, zmin, zmax)" does in C++.
func NewH3C(name, title string, nx int, xmin, xmax float64, ny int, ymin, ymax float64, nz int, zmin, zmax float64) *H3C {
	h := newH3C()
	h.th3.th1.SetName(name)
	h.th3.th1.SetTitle(title)
	h.th3.th1.xaxis.setRange(nx, xmin, xmax)
	h.th3.th1.yaxis.setRange(ny, ymin, ymax)
	h.th3.th1.zaxis.setRange(nz, zmin, zmax)
	h.reset()
	return h
}

// reset sizes the cells to the axes and empties them.
func (h *H3C) reset() {
	n := (h.th1.xaxis.nbins + 2) * (h.th1.yaxis.nbins + 2) * (h.th1.zaxis.nbins + 2)
	h.th3.th1.ncells = n
	h.arr.Data = make([]int8, n)
	h.th3.th1.sumw2.Data = make([]float64, n)
	h.th3.th1.entries = 0
	h.th3.th1.tsumw = 0
	h.th3.th1.tsumw2 = 0
	h.th3.th1.tsumwx = 0
	h.th3.th1.tsumwx2 = 0
	h.th3.tsumwy = 0
	h.th3.tsumwy2 = 0
	h.th3.tsumwxy = 0
	h.th3.tsumwz = 0
	h.th3.tsumwz2 = 0
	h.th3.tsumwxz = 0
	h.th3.tsumwyz = 0
}

// Reset empties the histogram, keeping its binning.
func (h *H3C) Reset() { h.reset() }

// FindBin returns the cell (x,y,z) falls in, as an index into the flat array
// the histogram keeps.
func (h *H3C) FindBin(x, y, z float64) int {
	return h.bin(
		h.th1.xaxis.FindBin(x),
		h.th1.yaxis.FindBin(y),
		h.th1.zaxis.FindBin(z),
	)
}

// Fill adds an entry of weight w at (x,y,z).
func (h *H3C) Fill(x, y, z, w float64) {
	var (
		ix = h.th1.xaxis.FindBin(x)
		iy = h.th1.yaxis.FindBin(y)
		iz = h.th1.zaxis.FindBin(z)
		i  = h.bin(ix, iy, iz)
	)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}

	h.arr.Data[i] += int8(w)
	if len(h.th1.sumw2.Data) > i {
		h.th1.sumw2.Data[i] += w * w
	}

	h.th3.th1.entries++

	if ix > 0 && ix <= h.th1.xaxis.nbins &&
		iy > 0 && iy <= h.th1.yaxis.nbins &&
		iz > 0 && iz <= h.th1.zaxis.nbins {
		h.th3.th1.tsumw += w
		h.th3.th1.tsumw2 += w * w
		h.th3.th1.tsumwx += w * x
		h.th3.th1.tsumwx2 += w * x * x
		h.th3.tsumwy += w * y
		h.th3.tsumwy2 += w * y * y
		h.th3.tsumwxy += w * x * y
		h.th3.tsumwz += w * z
		h.th3.tsumwz2 += w * z * z
		h.th3.tsumwxz += w * x * z
		h.th3.tsumwyz += w * y * z
	}
}

// FillN adds an entry for each (x,y,z) with the matching weight, or of weight
// one when ws is nil.
func (h *H3C) FillN(xs, ys, zs, ws []float64) {
	if len(ys) != len(xs) || len(zs) != len(xs) || (ws != nil && len(ws) != len(xs)) {
		panic(fmt.Errorf("rhist: lengths mismatch"))
	}
	for i := range xs {
		w := 1.0
		if ws != nil {
			w = ws[i]
		}
		h.Fill(xs[i], ys[i], zs[i], w)
	}
}

// BinContent returns the content of the cell at (ix,iy,iz).
func (h *H3C) BinContent(ix, iy, iz int) float64 {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	return float64(h.arr.Data[i])
}

// SetBinContent sets the content of the cell at (ix,iy,iz).
func (h *H3C) SetBinContent(ix, iy, iz int, v float64) {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}
	h.arr.Data[i] = int8(v)
}

// BinError returns the uncertainty on the cell at (ix,iy,iz).
func (h *H3C) BinError(ix, iy, iz int) float64 {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	if len(h.th1.sumw2.Data) > i {
		return math.Sqrt(h.th1.sumw2.Data[i])
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// SetBinError sets the uncertainty on the cell at (ix,iy,iz).
func (h *H3C) SetBinError(ix, iy, iz int, e float64) {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.th1.sumw2.Data) {
		return
	}
	h.th1.sumw2.Data[i] = e * e
}

// Scale multiplies every cell by f, and the uncertainties with them.
func (h *H3C) Scale(f float64) {
	for i := range h.arr.Data {
		h.arr.Data[i] = int8(float64(h.arr.Data[i]) * f)
	}
	for i := range h.th1.sumw2.Data {
		h.th1.sumw2.Data[i] *= f * f
	}
	h.th3.th1.tsumw *= f
	h.th3.th1.tsumw2 *= f * f
	h.th3.th1.tsumwx *= f
	h.th3.th1.tsumwx2 *= f
	h.th3.tsumwy *= f
	h.th3.tsumwy2 *= f
	h.th3.tsumwxy *= f
	h.th3.tsumwz *= f
	h.th3.tsumwz2 *= f
	h.th3.tsumwxz *= f
	h.th3.tsumwyz *= f
}

// Integral returns the sum of the bins proper, leaving out the under- and
// overflow.
func (h *H3C) Integral() float64 {
	var sum float64
	for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
		for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
			for iz := 1; iz <= h.th1.zaxis.nbins; iz++ {
				sum += h.BinContent(ix, iy, iz)
			}
		}
	}
	return sum
}

// ProjectionZ sums the histogram over x and y, as TH3::ProjectionZ does.
func (h *H3C) ProjectionZ(name string) *H1D {
	o := NewH1D(name, h.Title(), h.th1.zaxis.nbins, h.th1.zaxis.xmin, h.th1.zaxis.xmax)

	for iz := 1; iz <= h.th1.zaxis.nbins; iz++ {
		var sum, err2 float64
		for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
			for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
				sum += h.BinContent(ix, iy, iz)
				e := h.BinError(ix, iy, iz)
				err2 += e * e
			}
		}
		o.SetBinContent(iz, sum)
		o.SetBinError(iz, math.Sqrt(err2))
	}

	o.th1.entries = h.th1.entries
	o.th1.tsumw = h.th3.th1.tsumw
	o.th1.tsumw2 = h.th3.th1.tsumw2
	o.th1.tsumwx = h.th3.tsumwz
	o.th1.tsumwx2 = h.th3.tsumwz2
	return o
}

// ProjectionXY sums the histogram over z, as TH3::Project3D("xy") does.
func (h *H3C) ProjectionXY(name string) *H2D {
	o := NewH2D(name, h.Title(),
		h.th1.xaxis.nbins, h.th1.xaxis.xmin, h.th1.xaxis.xmax,
		h.th1.yaxis.nbins, h.th1.yaxis.xmin, h.th1.yaxis.xmax,
	)

	for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
		for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
			var sum, err2 float64
			for iz := 1; iz <= h.th1.zaxis.nbins; iz++ {
				sum += h.BinContent(ix, iy, iz)
				e := h.BinError(ix, iy, iz)
				err2 += e * e
			}
			o.SetBinContent(ix, iy, sum)
			o.SetBinError(ix, iy, math.Sqrt(err2))
		}
	}

	o.th2.th1.entries = h.th1.entries
	o.th2.th1.tsumw = h.th3.th1.tsumw
	o.th2.th1.tsumw2 = h.th3.th1.tsumw2
	o.th2.th1.tsumwx = h.th3.th1.tsumwx
	o.th2.th1.tsumwx2 = h.th3.th1.tsumwx2
	o.th2.tsumwy = h.th3.tsumwy
	o.th2.tsumwy2 = h.th3.tsumwy2
	o.th2.tsumwxy = h.th3.tsumwxy
	return o
}

// MeanX, MeanY and MeanZ return the means of the entries along each axis.
func (h *H3C) MeanX() float64 {
	if h.th3.th1.tsumw == 0 {
		return 0
	}
	return h.th3.th1.tsumwx / h.th3.th1.tsumw
}

// MeanY returns the mean along y.
func (h *H3C) MeanY() float64 {
	if h.th3.th1.tsumw == 0 {
		return 0
	}
	return h.th3.tsumwy / h.th3.th1.tsumw
}

// MeanZ returns the mean along z.
func (h *H3C) MeanZ() float64 {
	if h.th3.th1.tsumw == 0 {
		return 0
	}
	return h.th3.tsumwz / h.th3.th1.tsumw
}

func (*H3C) RVersion() int16 {
	return rvers.H3C
}

func (*H3C) isH3() {}

// Class returns the ROOT class name.
func (*H3C) Class() string {
	return "TH3C"
}

func (h *H3C) Array() rcont.ArrayC {
	return h.arr
}

// Rank returns the number of dimensions of this histogram.
func (h *H3C) Rank() int {
	return 3
}

// NbinsX returns the number of bins in X.
func (h *H3C) NbinsX() int {
	return h.th1.xaxis.nbins
}

// XAxis returns the axis along X.
func (h *H3C) XAxis() Axis {
	return &h.th1.xaxis
}

// XBinCenter returns the bin center value in X.
func (h *H3C) XBinCenter(i int) float64 {
	return float64(h.th1.xaxis.BinCenter(i))
}

// XBinContent returns the bin content value in X.
func (h *H3C) XBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// XBinError returns the bin error in X.
func (h *H3C) XBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// XBinLowEdge returns the bin lower edge value in X.
func (h *H3C) XBinLowEdge(i int) float64 {
	return h.th1.xaxis.BinLowEdge(i)
}

// XBinWidth returns the bin width in X.
func (h *H3C) XBinWidth(i int) float64 {
	return h.th1.xaxis.BinWidth(i)
}

// NbinsY returns the number of bins in Y.
func (h *H3C) NbinsY() int {
	return h.th1.yaxis.nbins
}

// YAxis returns the axis along Y.
func (h *H3C) YAxis() Axis {
	return &h.th1.yaxis
}

// YBinCenter returns the bin center value in Y.
func (h *H3C) YBinCenter(i int) float64 {
	return float64(h.th1.yaxis.BinCenter(i))
}

// YBinContent returns the bin content value in Y.
func (h *H3C) YBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// YBinError returns the bin error in Y.
func (h *H3C) YBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// YBinLowEdge returns the bin lower edge value in Y.
func (h *H3C) YBinLowEdge(i int) float64 {
	return h.th1.yaxis.BinLowEdge(i)
}

// YBinWidth returns the bin width in Y.
func (h *H3C) YBinWidth(i int) float64 {
	return h.th1.yaxis.BinWidth(i)
}

// NbinsZ returns the number of bins in Z.
func (h *H3C) NbinsZ() int {
	return h.th1.zaxis.nbins
}

// ZAxis returns the axis along Z.
func (h *H3C) ZAxis() Axis {
	return &h.th1.zaxis
}

// ZBinCenter returns the bin center value in Z.
func (h *H3C) ZBinCenter(i int) float64 {
	return float64(h.th1.zaxis.BinCenter(i))
}

// ZBinContent returns the bin content value in Z.
func (h *H3C) ZBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// ZBinError returns the bin error in Z.
func (h *H3C) ZBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// ZBinLowEdge returns the bin lower edge value in Z.
func (h *H3C) ZBinLowEdge(i int) float64 {
	return h.th1.zaxis.BinLowEdge(i)
}

// ZBinWidth returns the bin width in Z.
func (h *H3C) ZBinWidth(i int) float64 {
	return h.th1.zaxis.BinWidth(i)
}

// bin returns the regularized bin number given an (x,y,z) bin index triple.
func (h *H3C) bin(ix, iy, iz int) int {
	nx := h.th1.xaxis.nbins + 1 // overflow bin
	ny := h.th1.yaxis.nbins + 1 // overflow bin
	nz := h.th1.zaxis.nbins + 1 // overflow bin
	switch {
	case ix < 0:
		ix = 0
	case ix > nx:
		ix = nx
	}
	switch {
	case iy < 0:
		iy = 0
	case iy > ny:
		iy = ny
	}
	switch {
	case iz < 0:
		iz = 0
	case iz > nz:
		iz = nz
	}
	return ix + (nx+1)*(iy+(ny+1)*iz)
}

func (h *H3C) dist3D(ix, iy, iz int) hbook.Dist3D {
	i := h.bin(ix, iy, iz)
	vx := h.XBinContent(i)
	xerr := h.XBinError(i)
	nx := h.entries(vx, xerr)
	vy := h.YBinContent(i)
	yerr := h.YBinError(i)
	ny := h.entries(vy, yerr)
	vz := h.ZBinContent(i)
	zerr := h.ZBinError(i)
	nz := h.entries(vz, zerr)

	sumw := h.arr.Data[i]
	sumw2 := 0.0
	if len(h.th1.sumw2.Data) > 0 {
		sumw2 = h.th1.sumw2.Data[i]
	}
	return hbook.Dist3D{
		X: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     nx,
				SumW:  float64(sumw),
				SumW2: float64(sumw2),
			},
		},
		Y: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     ny,
				SumW:  float64(sumw),
				SumW2: float64(sumw2),
			},
		},
		Z: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     nz,
				SumW:  float64(sumw),
				SumW2: float64(sumw2),
			},
		},
	}
}

func (h *H3C) setDist3D(ix, iy, iz int, sumw, sumw2 float64) {
	i := h.bin(ix, iy, iz)
	h.arr.Data[i] = int8(sumw)
	h.th1.sumw2.Data[i] = sumw2
}

func (h *H3C) entries(height, err float64) int64 {
	if height <= 0 {
		return 0
	}
	v := height / err
	return int64(v*v + 0.5)
}

// AsH3D creates a new hbook.H3D from this ROOT histogram.
func (h *H3C) AsH3D() *hbook.H3D {
	var (
		nx = h.NbinsX()
		ny = h.NbinsY()
		nz = h.NbinsZ()
		hh = hbook.NewH3D(
			nx, h.XAxis().XMin(), h.XAxis().XMax(),
			ny, h.YAxis().XMin(), h.YAxis().XMax(),
			nz, h.ZAxis().XMin(), h.ZAxis().XMax(),
		)
	)
	hh.Ann = hbook.Annotation{
		"name":  h.Name(),
		"title": h.Title(),
	}

	for sx := -1; sx <= +1; sx++ {
		for sy := -1; sy <= +1; sy++ {
			for sz := -1; sz <= +1; sz++ {
				if sx == 0 && sy == 0 && sz == 0 {
					continue
				}
				hh.Binning.Outflows[hbook.Outflow3D(sx, sy, sz)] = h.dist3D(
					h3cell(sx, nx), h3cell(sy, ny), h3cell(sz, nz),
				)
			}
		}
	}

	hh.Binning.Dist = hbook.Dist3D{
		X: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     int64(h.Entries()),
				SumW:  float64(h.SumW()),
				SumW2: float64(h.SumW2()),
			},
		},
		Y: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     int64(h.Entries()),
				SumW:  float64(h.SumW()),
				SumW2: float64(h.SumW2()),
			},
		},
		Z: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     int64(h.Entries()),
				SumW:  float64(h.SumW()),
				SumW2: float64(h.SumW2()),
			},
		},
	}
	hh.Binning.Dist.X.Stats.SumWX = float64(h.SumWX())
	hh.Binning.Dist.X.Stats.SumWX2 = float64(h.SumWX2())
	hh.Binning.Dist.Y.Stats.SumWX = float64(h.SumWY())
	hh.Binning.Dist.Y.Stats.SumWX2 = float64(h.SumWY2())
	hh.Binning.Dist.Z.Stats.SumWX = float64(h.SumWZ())
	hh.Binning.Dist.Z.Stats.SumWX2 = float64(h.SumWZ2())
	hh.Binning.Dist.Stats.SumWXY = h.SumWXY()
	hh.Binning.Dist.Stats.SumWXZ = h.SumWXZ()
	hh.Binning.Dist.Stats.SumWYZ = h.SumWYZ()

	for ix := range nx {
		for iy := range ny {
			for iz := range nz {
				var (
					i    = (iz*ny+iy)*nx + ix
					xmin = h.XBinLowEdge(ix + 1)
					xmax = h.XBinWidth(ix+1) + xmin
					ymin = h.YBinLowEdge(iy + 1)
					ymax = h.YBinWidth(iy+1) + ymin
					zmin = h.ZBinLowEdge(iz + 1)
					zmax = h.ZBinWidth(iz+1) + zmin
					bin  = &hh.Binning.Bins[i]
				)
				bin.XRange.Min = xmin
				bin.XRange.Max = xmax
				bin.YRange.Min = ymin
				bin.YRange.Max = ymax
				bin.ZRange.Min = zmin
				bin.ZRange.Max = zmax
				bin.Dist = h.dist3D(ix+1, iy+1, iz+1)
			}
		}
	}

	return hh
}

func (h *H3C) ROOTMerge(src root.Object) error {
	hsrc, ok := src.(*H3C)
	if !ok {
		return fmt.Errorf("rhist: object %q is not a *rhist.H3C (%T)", src.(root.Named).Name(), src)
	}

	var (
		h1   = h.AsH3D()
		h2   = hsrc.AsH3D()
		hadd = hbook.AddH3D(h1, h2)
	)

	*h = *NewH3CFrom(hadd)
	return nil
}

func (h *H3C) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(h.Class(), h.RVersion())
	w.WriteObject(&h.th3)
	w.WriteObject(&h.arr)

	return w.SetHeader(hdr)
}

func (h *H3C) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(h.Class(), h.RVersion())
	if hdr.Vers < 1 {
		return fmt.Errorf("rhist: TH3C version too old (%d<1)", hdr.Vers)
	}

	r.ReadObject(&h.th3)
	r.ReadObject(&h.arr)

	r.CheckHeader(hdr)
	return r.Err()
}

func (h *H3C) RMembers() (mbrs []rbytes.Member) {
	mbrs = append(mbrs, h.th3.RMembers()...)
	mbrs = append(mbrs, rbytes.Member{
		Name: "fArray", Value: &h.arr.Data,
	})
	return mbrs
}

func init() {
	f := func() reflect.Value {
		o := newH3C()
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TH3C", f)
}

var (
	_ root.Object        = (*H3C)(nil)
	_ root.Merger        = (*H3C)(nil)
	_ root.Named         = (*H3C)(nil)
	_ H3                 = (*H3C)(nil)
	_ rbytes.Marshaler   = (*H3C)(nil)
	_ rbytes.Unmarshaler = (*H3C)(nil)
	_ rbytes.RSlicer     = (*H3C)(nil)
)

// H3S implements ROOT TH3S
type H3S struct {
	th3
	arr rcont.ArrayS
}

func newH3S() *H3S {
	return &H3S{
		th3: *newH3(),
	}
}

// NewH3SFrom creates a new H3S from an hbook 3-dim histogram.
func NewH3SFrom(h *hbook.H3D) *H3S {
	var (
		hroot  = newH3S()
		bins   = h.Binning.Bins
		nxbins = h.Binning.Nx
		nybins = h.Binning.Ny
		nzbins = h.Binning.Nz
		xedges = make([]float64, 0, nxbins+1)
		yedges = make([]float64, 0, nybins+1)
		zedges = make([]float64, 0, nzbins+1)
	)

	hroot.th3.th1.entries = float64(h.Entries())
	hroot.th3.th1.tsumw = h.SumW()
	hroot.th3.th1.tsumw2 = h.SumW2()
	hroot.th3.th1.tsumwx = h.SumWX()
	hroot.th3.th1.tsumwx2 = h.SumWX2()
	hroot.th3.tsumwy = h.SumWY()
	hroot.th3.tsumwy2 = h.SumWY2()
	hroot.th3.tsumwxy = h.SumWXY()
	hroot.th3.tsumwz = h.SumWZ()
	hroot.th3.tsumwz2 = h.SumWZ2()
	hroot.th3.tsumwxz = h.SumWXZ()
	hroot.th3.tsumwyz = h.SumWYZ()

	ncells := (nxbins + 2) * (nybins + 2) * (nzbins + 2)
	hroot.th3.th1.ncells = ncells

	hroot.th3.th1.xaxis.nbins = nxbins
	hroot.th3.th1.xaxis.xmin = h.XMin()
	hroot.th3.th1.xaxis.xmax = h.XMax()

	hroot.th3.th1.yaxis.nbins = nybins
	hroot.th3.th1.yaxis.xmin = h.YMin()
	hroot.th3.th1.yaxis.xmax = h.YMax()

	hroot.th3.th1.zaxis.nbins = nzbins
	hroot.th3.th1.zaxis.xmin = h.ZMin()
	hroot.th3.th1.zaxis.xmax = h.ZMax()

	hroot.arr.Data = make([]int16, ncells)
	hroot.th3.th1.sumw2.Data = make([]float64, ncells)

	ibin := func(ix, iy, iz int) int { return (iz*nybins+iy)*nxbins + ix }

	for ix := range nxbins {
		for iy := range nybins {
			for iz := range nzbins {
				bin := bins[ibin(ix, iy, iz)]
				if iy == 0 && iz == 0 {
					xedges = append(xedges, bin.XMin())
				}
				if ix == 0 && iz == 0 {
					yedges = append(yedges, bin.YMin())
				}
				if ix == 0 && iy == 0 {
					zedges = append(zedges, bin.ZMin())
				}
				hroot.setDist3D(ix+1, iy+1, iz+1, bin.Dist.SumW(), bin.Dist.SumW2())
			}
		}
	}

	// the 26 ways of missing a 3-dim binning, each landing in the slice of
	// cells ROOT keeps for it.
	for sx := -1; sx <= +1; sx++ {
		for sy := -1; sy <= +1; sy++ {
			for sz := -1; sz <= +1; sz++ {
				if sx == 0 && sy == 0 && sz == 0 {
					continue
				}
				o := h.Binning.Outflows[hbook.Outflow3D(sx, sy, sz)]
				hroot.setDist3D(
					h3cell(sx, nxbins), h3cell(sy, nybins), h3cell(sz, nzbins),
					o.SumW(), o.SumW2(),
				)
			}
		}
	}

	xedges = append(xedges, bins[ibin(nxbins-1, 0, 0)].XMax())
	yedges = append(yedges, bins[ibin(0, nybins-1, 0)].YMax())
	zedges = append(zedges, bins[ibin(0, 0, nzbins-1)].ZMax())

	hroot.th3.th1.SetName(h.Name())
	if v, ok := h.Annotation()["title"]; ok && v != nil {
		hroot.th3.th1.SetTitle(v.(string))
	}
	hroot.th3.th1.xaxis.xbins.Data = xedges
	hroot.th3.th1.yaxis.xbins.Data = yedges
	hroot.th3.th1.zaxis.xbins.Data = zedges

	return hroot
}

// NewH3S creates a 3-dim histogram, as "new TH3S(name, title,
// nx, xmin, xmax, ny, ymin, ymax, nz, zmin, zmax)" does in C++.
func NewH3S(name, title string, nx int, xmin, xmax float64, ny int, ymin, ymax float64, nz int, zmin, zmax float64) *H3S {
	h := newH3S()
	h.th3.th1.SetName(name)
	h.th3.th1.SetTitle(title)
	h.th3.th1.xaxis.setRange(nx, xmin, xmax)
	h.th3.th1.yaxis.setRange(ny, ymin, ymax)
	h.th3.th1.zaxis.setRange(nz, zmin, zmax)
	h.reset()
	return h
}

// reset sizes the cells to the axes and empties them.
func (h *H3S) reset() {
	n := (h.th1.xaxis.nbins + 2) * (h.th1.yaxis.nbins + 2) * (h.th1.zaxis.nbins + 2)
	h.th3.th1.ncells = n
	h.arr.Data = make([]int16, n)
	h.th3.th1.sumw2.Data = make([]float64, n)
	h.th3.th1.entries = 0
	h.th3.th1.tsumw = 0
	h.th3.th1.tsumw2 = 0
	h.th3.th1.tsumwx = 0
	h.th3.th1.tsumwx2 = 0
	h.th3.tsumwy = 0
	h.th3.tsumwy2 = 0
	h.th3.tsumwxy = 0
	h.th3.tsumwz = 0
	h.th3.tsumwz2 = 0
	h.th3.tsumwxz = 0
	h.th3.tsumwyz = 0
}

// Reset empties the histogram, keeping its binning.
func (h *H3S) Reset() { h.reset() }

// FindBin returns the cell (x,y,z) falls in, as an index into the flat array
// the histogram keeps.
func (h *H3S) FindBin(x, y, z float64) int {
	return h.bin(
		h.th1.xaxis.FindBin(x),
		h.th1.yaxis.FindBin(y),
		h.th1.zaxis.FindBin(z),
	)
}

// Fill adds an entry of weight w at (x,y,z).
func (h *H3S) Fill(x, y, z, w float64) {
	var (
		ix = h.th1.xaxis.FindBin(x)
		iy = h.th1.yaxis.FindBin(y)
		iz = h.th1.zaxis.FindBin(z)
		i  = h.bin(ix, iy, iz)
	)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}

	h.arr.Data[i] += int16(w)
	if len(h.th1.sumw2.Data) > i {
		h.th1.sumw2.Data[i] += w * w
	}

	h.th3.th1.entries++

	if ix > 0 && ix <= h.th1.xaxis.nbins &&
		iy > 0 && iy <= h.th1.yaxis.nbins &&
		iz > 0 && iz <= h.th1.zaxis.nbins {
		h.th3.th1.tsumw += w
		h.th3.th1.tsumw2 += w * w
		h.th3.th1.tsumwx += w * x
		h.th3.th1.tsumwx2 += w * x * x
		h.th3.tsumwy += w * y
		h.th3.tsumwy2 += w * y * y
		h.th3.tsumwxy += w * x * y
		h.th3.tsumwz += w * z
		h.th3.tsumwz2 += w * z * z
		h.th3.tsumwxz += w * x * z
		h.th3.tsumwyz += w * y * z
	}
}

// FillN adds an entry for each (x,y,z) with the matching weight, or of weight
// one when ws is nil.
func (h *H3S) FillN(xs, ys, zs, ws []float64) {
	if len(ys) != len(xs) || len(zs) != len(xs) || (ws != nil && len(ws) != len(xs)) {
		panic(fmt.Errorf("rhist: lengths mismatch"))
	}
	for i := range xs {
		w := 1.0
		if ws != nil {
			w = ws[i]
		}
		h.Fill(xs[i], ys[i], zs[i], w)
	}
}

// BinContent returns the content of the cell at (ix,iy,iz).
func (h *H3S) BinContent(ix, iy, iz int) float64 {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	return float64(h.arr.Data[i])
}

// SetBinContent sets the content of the cell at (ix,iy,iz).
func (h *H3S) SetBinContent(ix, iy, iz int, v float64) {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}
	h.arr.Data[i] = int16(v)
}

// BinError returns the uncertainty on the cell at (ix,iy,iz).
func (h *H3S) BinError(ix, iy, iz int) float64 {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	if len(h.th1.sumw2.Data) > i {
		return math.Sqrt(h.th1.sumw2.Data[i])
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// SetBinError sets the uncertainty on the cell at (ix,iy,iz).
func (h *H3S) SetBinError(ix, iy, iz int, e float64) {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.th1.sumw2.Data) {
		return
	}
	h.th1.sumw2.Data[i] = e * e
}

// Scale multiplies every cell by f, and the uncertainties with them.
func (h *H3S) Scale(f float64) {
	for i := range h.arr.Data {
		h.arr.Data[i] = int16(float64(h.arr.Data[i]) * f)
	}
	for i := range h.th1.sumw2.Data {
		h.th1.sumw2.Data[i] *= f * f
	}
	h.th3.th1.tsumw *= f
	h.th3.th1.tsumw2 *= f * f
	h.th3.th1.tsumwx *= f
	h.th3.th1.tsumwx2 *= f
	h.th3.tsumwy *= f
	h.th3.tsumwy2 *= f
	h.th3.tsumwxy *= f
	h.th3.tsumwz *= f
	h.th3.tsumwz2 *= f
	h.th3.tsumwxz *= f
	h.th3.tsumwyz *= f
}

// Integral returns the sum of the bins proper, leaving out the under- and
// overflow.
func (h *H3S) Integral() float64 {
	var sum float64
	for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
		for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
			for iz := 1; iz <= h.th1.zaxis.nbins; iz++ {
				sum += h.BinContent(ix, iy, iz)
			}
		}
	}
	return sum
}

// ProjectionZ sums the histogram over x and y, as TH3::ProjectionZ does.
func (h *H3S) ProjectionZ(name string) *H1D {
	o := NewH1D(name, h.Title(), h.th1.zaxis.nbins, h.th1.zaxis.xmin, h.th1.zaxis.xmax)

	for iz := 1; iz <= h.th1.zaxis.nbins; iz++ {
		var sum, err2 float64
		for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
			for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
				sum += h.BinContent(ix, iy, iz)
				e := h.BinError(ix, iy, iz)
				err2 += e * e
			}
		}
		o.SetBinContent(iz, sum)
		o.SetBinError(iz, math.Sqrt(err2))
	}

	o.th1.entries = h.th1.entries
	o.th1.tsumw = h.th3.th1.tsumw
	o.th1.tsumw2 = h.th3.th1.tsumw2
	o.th1.tsumwx = h.th3.tsumwz
	o.th1.tsumwx2 = h.th3.tsumwz2
	return o
}

// ProjectionXY sums the histogram over z, as TH3::Project3D("xy") does.
func (h *H3S) ProjectionXY(name string) *H2D {
	o := NewH2D(name, h.Title(),
		h.th1.xaxis.nbins, h.th1.xaxis.xmin, h.th1.xaxis.xmax,
		h.th1.yaxis.nbins, h.th1.yaxis.xmin, h.th1.yaxis.xmax,
	)

	for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
		for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
			var sum, err2 float64
			for iz := 1; iz <= h.th1.zaxis.nbins; iz++ {
				sum += h.BinContent(ix, iy, iz)
				e := h.BinError(ix, iy, iz)
				err2 += e * e
			}
			o.SetBinContent(ix, iy, sum)
			o.SetBinError(ix, iy, math.Sqrt(err2))
		}
	}

	o.th2.th1.entries = h.th1.entries
	o.th2.th1.tsumw = h.th3.th1.tsumw
	o.th2.th1.tsumw2 = h.th3.th1.tsumw2
	o.th2.th1.tsumwx = h.th3.th1.tsumwx
	o.th2.th1.tsumwx2 = h.th3.th1.tsumwx2
	o.th2.tsumwy = h.th3.tsumwy
	o.th2.tsumwy2 = h.th3.tsumwy2
	o.th2.tsumwxy = h.th3.tsumwxy
	return o
}

// MeanX, MeanY and MeanZ return the means of the entries along each axis.
func (h *H3S) MeanX() float64 {
	if h.th3.th1.tsumw == 0 {
		return 0
	}
	return h.th3.th1.tsumwx / h.th3.th1.tsumw
}

// MeanY returns the mean along y.
func (h *H3S) MeanY() float64 {
	if h.th3.th1.tsumw == 0 {
		return 0
	}
	return h.th3.tsumwy / h.th3.th1.tsumw
}

// MeanZ returns the mean along z.
func (h *H3S) MeanZ() float64 {
	if h.th3.th1.tsumw == 0 {
		return 0
	}
	return h.th3.tsumwz / h.th3.th1.tsumw
}

func (*H3S) RVersion() int16 {
	return rvers.H3S
}

func (*H3S) isH3() {}

// Class returns the ROOT class name.
func (*H3S) Class() string {
	return "TH3S"
}

func (h *H3S) Array() rcont.ArrayS {
	return h.arr
}

// Rank returns the number of dimensions of this histogram.
func (h *H3S) Rank() int {
	return 3
}

// NbinsX returns the number of bins in X.
func (h *H3S) NbinsX() int {
	return h.th1.xaxis.nbins
}

// XAxis returns the axis along X.
func (h *H3S) XAxis() Axis {
	return &h.th1.xaxis
}

// XBinCenter returns the bin center value in X.
func (h *H3S) XBinCenter(i int) float64 {
	return float64(h.th1.xaxis.BinCenter(i))
}

// XBinContent returns the bin content value in X.
func (h *H3S) XBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// XBinError returns the bin error in X.
func (h *H3S) XBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// XBinLowEdge returns the bin lower edge value in X.
func (h *H3S) XBinLowEdge(i int) float64 {
	return h.th1.xaxis.BinLowEdge(i)
}

// XBinWidth returns the bin width in X.
func (h *H3S) XBinWidth(i int) float64 {
	return h.th1.xaxis.BinWidth(i)
}

// NbinsY returns the number of bins in Y.
func (h *H3S) NbinsY() int {
	return h.th1.yaxis.nbins
}

// YAxis returns the axis along Y.
func (h *H3S) YAxis() Axis {
	return &h.th1.yaxis
}

// YBinCenter returns the bin center value in Y.
func (h *H3S) YBinCenter(i int) float64 {
	return float64(h.th1.yaxis.BinCenter(i))
}

// YBinContent returns the bin content value in Y.
func (h *H3S) YBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// YBinError returns the bin error in Y.
func (h *H3S) YBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// YBinLowEdge returns the bin lower edge value in Y.
func (h *H3S) YBinLowEdge(i int) float64 {
	return h.th1.yaxis.BinLowEdge(i)
}

// YBinWidth returns the bin width in Y.
func (h *H3S) YBinWidth(i int) float64 {
	return h.th1.yaxis.BinWidth(i)
}

// NbinsZ returns the number of bins in Z.
func (h *H3S) NbinsZ() int {
	return h.th1.zaxis.nbins
}

// ZAxis returns the axis along Z.
func (h *H3S) ZAxis() Axis {
	return &h.th1.zaxis
}

// ZBinCenter returns the bin center value in Z.
func (h *H3S) ZBinCenter(i int) float64 {
	return float64(h.th1.zaxis.BinCenter(i))
}

// ZBinContent returns the bin content value in Z.
func (h *H3S) ZBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// ZBinError returns the bin error in Z.
func (h *H3S) ZBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// ZBinLowEdge returns the bin lower edge value in Z.
func (h *H3S) ZBinLowEdge(i int) float64 {
	return h.th1.zaxis.BinLowEdge(i)
}

// ZBinWidth returns the bin width in Z.
func (h *H3S) ZBinWidth(i int) float64 {
	return h.th1.zaxis.BinWidth(i)
}

// bin returns the regularized bin number given an (x,y,z) bin index triple.
func (h *H3S) bin(ix, iy, iz int) int {
	nx := h.th1.xaxis.nbins + 1 // overflow bin
	ny := h.th1.yaxis.nbins + 1 // overflow bin
	nz := h.th1.zaxis.nbins + 1 // overflow bin
	switch {
	case ix < 0:
		ix = 0
	case ix > nx:
		ix = nx
	}
	switch {
	case iy < 0:
		iy = 0
	case iy > ny:
		iy = ny
	}
	switch {
	case iz < 0:
		iz = 0
	case iz > nz:
		iz = nz
	}
	return ix + (nx+1)*(iy+(ny+1)*iz)
}

func (h *H3S) dist3D(ix, iy, iz int) hbook.Dist3D {
	i := h.bin(ix, iy, iz)
	vx := h.XBinContent(i)
	xerr := h.XBinError(i)
	nx := h.entries(vx, xerr)
	vy := h.YBinContent(i)
	yerr := h.YBinError(i)
	ny := h.entries(vy, yerr)
	vz := h.ZBinContent(i)
	zerr := h.ZBinError(i)
	nz := h.entries(vz, zerr)

	sumw := h.arr.Data[i]
	sumw2 := 0.0
	if len(h.th1.sumw2.Data) > 0 {
		sumw2 = h.th1.sumw2.Data[i]
	}
	return hbook.Dist3D{
		X: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     nx,
				SumW:  float64(sumw),
				SumW2: float64(sumw2),
			},
		},
		Y: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     ny,
				SumW:  float64(sumw),
				SumW2: float64(sumw2),
			},
		},
		Z: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     nz,
				SumW:  float64(sumw),
				SumW2: float64(sumw2),
			},
		},
	}
}

func (h *H3S) setDist3D(ix, iy, iz int, sumw, sumw2 float64) {
	i := h.bin(ix, iy, iz)
	h.arr.Data[i] = int16(sumw)
	h.th1.sumw2.Data[i] = sumw2
}

func (h *H3S) entries(height, err float64) int64 {
	if height <= 0 {
		return 0
	}
	v := height / err
	return int64(v*v + 0.5)
}

// AsH3D creates a new hbook.H3D from this ROOT histogram.
func (h *H3S) AsH3D() *hbook.H3D {
	var (
		nx = h.NbinsX()
		ny = h.NbinsY()
		nz = h.NbinsZ()
		hh = hbook.NewH3D(
			nx, h.XAxis().XMin(), h.XAxis().XMax(),
			ny, h.YAxis().XMin(), h.YAxis().XMax(),
			nz, h.ZAxis().XMin(), h.ZAxis().XMax(),
		)
	)
	hh.Ann = hbook.Annotation{
		"name":  h.Name(),
		"title": h.Title(),
	}

	for sx := -1; sx <= +1; sx++ {
		for sy := -1; sy <= +1; sy++ {
			for sz := -1; sz <= +1; sz++ {
				if sx == 0 && sy == 0 && sz == 0 {
					continue
				}
				hh.Binning.Outflows[hbook.Outflow3D(sx, sy, sz)] = h.dist3D(
					h3cell(sx, nx), h3cell(sy, ny), h3cell(sz, nz),
				)
			}
		}
	}

	hh.Binning.Dist = hbook.Dist3D{
		X: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     int64(h.Entries()),
				SumW:  float64(h.SumW()),
				SumW2: float64(h.SumW2()),
			},
		},
		Y: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     int64(h.Entries()),
				SumW:  float64(h.SumW()),
				SumW2: float64(h.SumW2()),
			},
		},
		Z: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     int64(h.Entries()),
				SumW:  float64(h.SumW()),
				SumW2: float64(h.SumW2()),
			},
		},
	}
	hh.Binning.Dist.X.Stats.SumWX = float64(h.SumWX())
	hh.Binning.Dist.X.Stats.SumWX2 = float64(h.SumWX2())
	hh.Binning.Dist.Y.Stats.SumWX = float64(h.SumWY())
	hh.Binning.Dist.Y.Stats.SumWX2 = float64(h.SumWY2())
	hh.Binning.Dist.Z.Stats.SumWX = float64(h.SumWZ())
	hh.Binning.Dist.Z.Stats.SumWX2 = float64(h.SumWZ2())
	hh.Binning.Dist.Stats.SumWXY = h.SumWXY()
	hh.Binning.Dist.Stats.SumWXZ = h.SumWXZ()
	hh.Binning.Dist.Stats.SumWYZ = h.SumWYZ()

	for ix := range nx {
		for iy := range ny {
			for iz := range nz {
				var (
					i    = (iz*ny+iy)*nx + ix
					xmin = h.XBinLowEdge(ix + 1)
					xmax = h.XBinWidth(ix+1) + xmin
					ymin = h.YBinLowEdge(iy + 1)
					ymax = h.YBinWidth(iy+1) + ymin
					zmin = h.ZBinLowEdge(iz + 1)
					zmax = h.ZBinWidth(iz+1) + zmin
					bin  = &hh.Binning.Bins[i]
				)
				bin.XRange.Min = xmin
				bin.XRange.Max = xmax
				bin.YRange.Min = ymin
				bin.YRange.Max = ymax
				bin.ZRange.Min = zmin
				bin.ZRange.Max = zmax
				bin.Dist = h.dist3D(ix+1, iy+1, iz+1)
			}
		}
	}

	return hh
}

func (h *H3S) ROOTMerge(src root.Object) error {
	hsrc, ok := src.(*H3S)
	if !ok {
		return fmt.Errorf("rhist: object %q is not a *rhist.H3S (%T)", src.(root.Named).Name(), src)
	}

	var (
		h1   = h.AsH3D()
		h2   = hsrc.AsH3D()
		hadd = hbook.AddH3D(h1, h2)
	)

	*h = *NewH3SFrom(hadd)
	return nil
}

func (h *H3S) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(h.Class(), h.RVersion())
	w.WriteObject(&h.th3)
	w.WriteObject(&h.arr)

	return w.SetHeader(hdr)
}

func (h *H3S) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(h.Class(), h.RVersion())
	if hdr.Vers < 1 {
		return fmt.Errorf("rhist: TH3S version too old (%d<1)", hdr.Vers)
	}

	r.ReadObject(&h.th3)
	r.ReadObject(&h.arr)

	r.CheckHeader(hdr)
	return r.Err()
}

func (h *H3S) RMembers() (mbrs []rbytes.Member) {
	mbrs = append(mbrs, h.th3.RMembers()...)
	mbrs = append(mbrs, rbytes.Member{
		Name: "fArray", Value: &h.arr.Data,
	})
	return mbrs
}

func init() {
	f := func() reflect.Value {
		o := newH3S()
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TH3S", f)
}

var (
	_ root.Object        = (*H3S)(nil)
	_ root.Merger        = (*H3S)(nil)
	_ root.Named         = (*H3S)(nil)
	_ H3                 = (*H3S)(nil)
	_ rbytes.Marshaler   = (*H3S)(nil)
	_ rbytes.Unmarshaler = (*H3S)(nil)
	_ rbytes.RSlicer     = (*H3S)(nil)
)

// H3I implements ROOT TH3I
type H3I struct {
	th3
	arr rcont.ArrayI
}

func newH3I() *H3I {
	return &H3I{
		th3: *newH3(),
	}
}

// NewH3IFrom creates a new H3I from an hbook 3-dim histogram.
func NewH3IFrom(h *hbook.H3D) *H3I {
	var (
		hroot  = newH3I()
		bins   = h.Binning.Bins
		nxbins = h.Binning.Nx
		nybins = h.Binning.Ny
		nzbins = h.Binning.Nz
		xedges = make([]float64, 0, nxbins+1)
		yedges = make([]float64, 0, nybins+1)
		zedges = make([]float64, 0, nzbins+1)
	)

	hroot.th3.th1.entries = float64(h.Entries())
	hroot.th3.th1.tsumw = h.SumW()
	hroot.th3.th1.tsumw2 = h.SumW2()
	hroot.th3.th1.tsumwx = h.SumWX()
	hroot.th3.th1.tsumwx2 = h.SumWX2()
	hroot.th3.tsumwy = h.SumWY()
	hroot.th3.tsumwy2 = h.SumWY2()
	hroot.th3.tsumwxy = h.SumWXY()
	hroot.th3.tsumwz = h.SumWZ()
	hroot.th3.tsumwz2 = h.SumWZ2()
	hroot.th3.tsumwxz = h.SumWXZ()
	hroot.th3.tsumwyz = h.SumWYZ()

	ncells := (nxbins + 2) * (nybins + 2) * (nzbins + 2)
	hroot.th3.th1.ncells = ncells

	hroot.th3.th1.xaxis.nbins = nxbins
	hroot.th3.th1.xaxis.xmin = h.XMin()
	hroot.th3.th1.xaxis.xmax = h.XMax()

	hroot.th3.th1.yaxis.nbins = nybins
	hroot.th3.th1.yaxis.xmin = h.YMin()
	hroot.th3.th1.yaxis.xmax = h.YMax()

	hroot.th3.th1.zaxis.nbins = nzbins
	hroot.th3.th1.zaxis.xmin = h.ZMin()
	hroot.th3.th1.zaxis.xmax = h.ZMax()

	hroot.arr.Data = make([]int32, ncells)
	hroot.th3.th1.sumw2.Data = make([]float64, ncells)

	ibin := func(ix, iy, iz int) int { return (iz*nybins+iy)*nxbins + ix }

	for ix := range nxbins {
		for iy := range nybins {
			for iz := range nzbins {
				bin := bins[ibin(ix, iy, iz)]
				if iy == 0 && iz == 0 {
					xedges = append(xedges, bin.XMin())
				}
				if ix == 0 && iz == 0 {
					yedges = append(yedges, bin.YMin())
				}
				if ix == 0 && iy == 0 {
					zedges = append(zedges, bin.ZMin())
				}
				hroot.setDist3D(ix+1, iy+1, iz+1, bin.Dist.SumW(), bin.Dist.SumW2())
			}
		}
	}

	// the 26 ways of missing a 3-dim binning, each landing in the slice of
	// cells ROOT keeps for it.
	for sx := -1; sx <= +1; sx++ {
		for sy := -1; sy <= +1; sy++ {
			for sz := -1; sz <= +1; sz++ {
				if sx == 0 && sy == 0 && sz == 0 {
					continue
				}
				o := h.Binning.Outflows[hbook.Outflow3D(sx, sy, sz)]
				hroot.setDist3D(
					h3cell(sx, nxbins), h3cell(sy, nybins), h3cell(sz, nzbins),
					o.SumW(), o.SumW2(),
				)
			}
		}
	}

	xedges = append(xedges, bins[ibin(nxbins-1, 0, 0)].XMax())
	yedges = append(yedges, bins[ibin(0, nybins-1, 0)].YMax())
	zedges = append(zedges, bins[ibin(0, 0, nzbins-1)].ZMax())

	hroot.th3.th1.SetName(h.Name())
	if v, ok := h.Annotation()["title"]; ok && v != nil {
		hroot.th3.th1.SetTitle(v.(string))
	}
	hroot.th3.th1.xaxis.xbins.Data = xedges
	hroot.th3.th1.yaxis.xbins.Data = yedges
	hroot.th3.th1.zaxis.xbins.Data = zedges

	return hroot
}

// NewH3I creates a 3-dim histogram, as "new TH3I(name, title,
// nx, xmin, xmax, ny, ymin, ymax, nz, zmin, zmax)" does in C++.
func NewH3I(name, title string, nx int, xmin, xmax float64, ny int, ymin, ymax float64, nz int, zmin, zmax float64) *H3I {
	h := newH3I()
	h.th3.th1.SetName(name)
	h.th3.th1.SetTitle(title)
	h.th3.th1.xaxis.setRange(nx, xmin, xmax)
	h.th3.th1.yaxis.setRange(ny, ymin, ymax)
	h.th3.th1.zaxis.setRange(nz, zmin, zmax)
	h.reset()
	return h
}

// reset sizes the cells to the axes and empties them.
func (h *H3I) reset() {
	n := (h.th1.xaxis.nbins + 2) * (h.th1.yaxis.nbins + 2) * (h.th1.zaxis.nbins + 2)
	h.th3.th1.ncells = n
	h.arr.Data = make([]int32, n)
	h.th3.th1.sumw2.Data = make([]float64, n)
	h.th3.th1.entries = 0
	h.th3.th1.tsumw = 0
	h.th3.th1.tsumw2 = 0
	h.th3.th1.tsumwx = 0
	h.th3.th1.tsumwx2 = 0
	h.th3.tsumwy = 0
	h.th3.tsumwy2 = 0
	h.th3.tsumwxy = 0
	h.th3.tsumwz = 0
	h.th3.tsumwz2 = 0
	h.th3.tsumwxz = 0
	h.th3.tsumwyz = 0
}

// Reset empties the histogram, keeping its binning.
func (h *H3I) Reset() { h.reset() }

// FindBin returns the cell (x,y,z) falls in, as an index into the flat array
// the histogram keeps.
func (h *H3I) FindBin(x, y, z float64) int {
	return h.bin(
		h.th1.xaxis.FindBin(x),
		h.th1.yaxis.FindBin(y),
		h.th1.zaxis.FindBin(z),
	)
}

// Fill adds an entry of weight w at (x,y,z).
func (h *H3I) Fill(x, y, z, w float64) {
	var (
		ix = h.th1.xaxis.FindBin(x)
		iy = h.th1.yaxis.FindBin(y)
		iz = h.th1.zaxis.FindBin(z)
		i  = h.bin(ix, iy, iz)
	)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}

	h.arr.Data[i] += int32(w)
	if len(h.th1.sumw2.Data) > i {
		h.th1.sumw2.Data[i] += w * w
	}

	h.th3.th1.entries++

	if ix > 0 && ix <= h.th1.xaxis.nbins &&
		iy > 0 && iy <= h.th1.yaxis.nbins &&
		iz > 0 && iz <= h.th1.zaxis.nbins {
		h.th3.th1.tsumw += w
		h.th3.th1.tsumw2 += w * w
		h.th3.th1.tsumwx += w * x
		h.th3.th1.tsumwx2 += w * x * x
		h.th3.tsumwy += w * y
		h.th3.tsumwy2 += w * y * y
		h.th3.tsumwxy += w * x * y
		h.th3.tsumwz += w * z
		h.th3.tsumwz2 += w * z * z
		h.th3.tsumwxz += w * x * z
		h.th3.tsumwyz += w * y * z
	}
}

// FillN adds an entry for each (x,y,z) with the matching weight, or of weight
// one when ws is nil.
func (h *H3I) FillN(xs, ys, zs, ws []float64) {
	if len(ys) != len(xs) || len(zs) != len(xs) || (ws != nil && len(ws) != len(xs)) {
		panic(fmt.Errorf("rhist: lengths mismatch"))
	}
	for i := range xs {
		w := 1.0
		if ws != nil {
			w = ws[i]
		}
		h.Fill(xs[i], ys[i], zs[i], w)
	}
}

// BinContent returns the content of the cell at (ix,iy,iz).
func (h *H3I) BinContent(ix, iy, iz int) float64 {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	return float64(h.arr.Data[i])
}

// SetBinContent sets the content of the cell at (ix,iy,iz).
func (h *H3I) SetBinContent(ix, iy, iz int, v float64) {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}
	h.arr.Data[i] = int32(v)
}

// BinError returns the uncertainty on the cell at (ix,iy,iz).
func (h *H3I) BinError(ix, iy, iz int) float64 {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	if len(h.th1.sumw2.Data) > i {
		return math.Sqrt(h.th1.sumw2.Data[i])
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// SetBinError sets the uncertainty on the cell at (ix,iy,iz).
func (h *H3I) SetBinError(ix, iy, iz int, e float64) {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.th1.sumw2.Data) {
		return
	}
	h.th1.sumw2.Data[i] = e * e
}

// Scale multiplies every cell by f, and the uncertainties with them.
func (h *H3I) Scale(f float64) {
	for i := range h.arr.Data {
		h.arr.Data[i] = int32(float64(h.arr.Data[i]) * f)
	}
	for i := range h.th1.sumw2.Data {
		h.th1.sumw2.Data[i] *= f * f
	}
	h.th3.th1.tsumw *= f
	h.th3.th1.tsumw2 *= f * f
	h.th3.th1.tsumwx *= f
	h.th3.th1.tsumwx2 *= f
	h.th3.tsumwy *= f
	h.th3.tsumwy2 *= f
	h.th3.tsumwxy *= f
	h.th3.tsumwz *= f
	h.th3.tsumwz2 *= f
	h.th3.tsumwxz *= f
	h.th3.tsumwyz *= f
}

// Integral returns the sum of the bins proper, leaving out the under- and
// overflow.
func (h *H3I) Integral() float64 {
	var sum float64
	for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
		for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
			for iz := 1; iz <= h.th1.zaxis.nbins; iz++ {
				sum += h.BinContent(ix, iy, iz)
			}
		}
	}
	return sum
}

// ProjectionZ sums the histogram over x and y, as TH3::ProjectionZ does.
func (h *H3I) ProjectionZ(name string) *H1D {
	o := NewH1D(name, h.Title(), h.th1.zaxis.nbins, h.th1.zaxis.xmin, h.th1.zaxis.xmax)

	for iz := 1; iz <= h.th1.zaxis.nbins; iz++ {
		var sum, err2 float64
		for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
			for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
				sum += h.BinContent(ix, iy, iz)
				e := h.BinError(ix, iy, iz)
				err2 += e * e
			}
		}
		o.SetBinContent(iz, sum)
		o.SetBinError(iz, math.Sqrt(err2))
	}

	o.th1.entries = h.th1.entries
	o.th1.tsumw = h.th3.th1.tsumw
	o.th1.tsumw2 = h.th3.th1.tsumw2
	o.th1.tsumwx = h.th3.tsumwz
	o.th1.tsumwx2 = h.th3.tsumwz2
	return o
}

// ProjectionXY sums the histogram over z, as TH3::Project3D("xy") does.
func (h *H3I) ProjectionXY(name string) *H2D {
	o := NewH2D(name, h.Title(),
		h.th1.xaxis.nbins, h.th1.xaxis.xmin, h.th1.xaxis.xmax,
		h.th1.yaxis.nbins, h.th1.yaxis.xmin, h.th1.yaxis.xmax,
	)

	for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
		for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
			var sum, err2 float64
			for iz := 1; iz <= h.th1.zaxis.nbins; iz++ {
				sum += h.BinContent(ix, iy, iz)
				e := h.BinError(ix, iy, iz)
				err2 += e * e
			}
			o.SetBinContent(ix, iy, sum)
			o.SetBinError(ix, iy, math.Sqrt(err2))
		}
	}

	o.th2.th1.entries = h.th1.entries
	o.th2.th1.tsumw = h.th3.th1.tsumw
	o.th2.th1.tsumw2 = h.th3.th1.tsumw2
	o.th2.th1.tsumwx = h.th3.th1.tsumwx
	o.th2.th1.tsumwx2 = h.th3.th1.tsumwx2
	o.th2.tsumwy = h.th3.tsumwy
	o.th2.tsumwy2 = h.th3.tsumwy2
	o.th2.tsumwxy = h.th3.tsumwxy
	return o
}

// MeanX, MeanY and MeanZ return the means of the entries along each axis.
func (h *H3I) MeanX() float64 {
	if h.th3.th1.tsumw == 0 {
		return 0
	}
	return h.th3.th1.tsumwx / h.th3.th1.tsumw
}

// MeanY returns the mean along y.
func (h *H3I) MeanY() float64 {
	if h.th3.th1.tsumw == 0 {
		return 0
	}
	return h.th3.tsumwy / h.th3.th1.tsumw
}

// MeanZ returns the mean along z.
func (h *H3I) MeanZ() float64 {
	if h.th3.th1.tsumw == 0 {
		return 0
	}
	return h.th3.tsumwz / h.th3.th1.tsumw
}

func (*H3I) RVersion() int16 {
	return rvers.H3I
}

func (*H3I) isH3() {}

// Class returns the ROOT class name.
func (*H3I) Class() string {
	return "TH3I"
}

func (h *H3I) Array() rcont.ArrayI {
	return h.arr
}

// Rank returns the number of dimensions of this histogram.
func (h *H3I) Rank() int {
	return 3
}

// NbinsX returns the number of bins in X.
func (h *H3I) NbinsX() int {
	return h.th1.xaxis.nbins
}

// XAxis returns the axis along X.
func (h *H3I) XAxis() Axis {
	return &h.th1.xaxis
}

// XBinCenter returns the bin center value in X.
func (h *H3I) XBinCenter(i int) float64 {
	return float64(h.th1.xaxis.BinCenter(i))
}

// XBinContent returns the bin content value in X.
func (h *H3I) XBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// XBinError returns the bin error in X.
func (h *H3I) XBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// XBinLowEdge returns the bin lower edge value in X.
func (h *H3I) XBinLowEdge(i int) float64 {
	return h.th1.xaxis.BinLowEdge(i)
}

// XBinWidth returns the bin width in X.
func (h *H3I) XBinWidth(i int) float64 {
	return h.th1.xaxis.BinWidth(i)
}

// NbinsY returns the number of bins in Y.
func (h *H3I) NbinsY() int {
	return h.th1.yaxis.nbins
}

// YAxis returns the axis along Y.
func (h *H3I) YAxis() Axis {
	return &h.th1.yaxis
}

// YBinCenter returns the bin center value in Y.
func (h *H3I) YBinCenter(i int) float64 {
	return float64(h.th1.yaxis.BinCenter(i))
}

// YBinContent returns the bin content value in Y.
func (h *H3I) YBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// YBinError returns the bin error in Y.
func (h *H3I) YBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// YBinLowEdge returns the bin lower edge value in Y.
func (h *H3I) YBinLowEdge(i int) float64 {
	return h.th1.yaxis.BinLowEdge(i)
}

// YBinWidth returns the bin width in Y.
func (h *H3I) YBinWidth(i int) float64 {
	return h.th1.yaxis.BinWidth(i)
}

// NbinsZ returns the number of bins in Z.
func (h *H3I) NbinsZ() int {
	return h.th1.zaxis.nbins
}

// ZAxis returns the axis along Z.
func (h *H3I) ZAxis() Axis {
	return &h.th1.zaxis
}

// ZBinCenter returns the bin center value in Z.
func (h *H3I) ZBinCenter(i int) float64 {
	return float64(h.th1.zaxis.BinCenter(i))
}

// ZBinContent returns the bin content value in Z.
func (h *H3I) ZBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// ZBinError returns the bin error in Z.
func (h *H3I) ZBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// ZBinLowEdge returns the bin lower edge value in Z.
func (h *H3I) ZBinLowEdge(i int) float64 {
	return h.th1.zaxis.BinLowEdge(i)
}

// ZBinWidth returns the bin width in Z.
func (h *H3I) ZBinWidth(i int) float64 {
	return h.th1.zaxis.BinWidth(i)
}

// bin returns the regularized bin number given an (x,y,z) bin index triple.
func (h *H3I) bin(ix, iy, iz int) int {
	nx := h.th1.xaxis.nbins + 1 // overflow bin
	ny := h.th1.yaxis.nbins + 1 // overflow bin
	nz := h.th1.zaxis.nbins + 1 // overflow bin
	switch {
	case ix < 0:
		ix = 0
	case ix > nx:
		ix = nx
	}
	switch {
	case iy < 0:
		iy = 0
	case iy > ny:
		iy = ny
	}
	switch {
	case iz < 0:
		iz = 0
	case iz > nz:
		iz = nz
	}
	return ix + (nx+1)*(iy+(ny+1)*iz)
}

func (h *H3I) dist3D(ix, iy, iz int) hbook.Dist3D {
	i := h.bin(ix, iy, iz)
	vx := h.XBinContent(i)
	xerr := h.XBinError(i)
	nx := h.entries(vx, xerr)
	vy := h.YBinContent(i)
	yerr := h.YBinError(i)
	ny := h.entries(vy, yerr)
	vz := h.ZBinContent(i)
	zerr := h.ZBinError(i)
	nz := h.entries(vz, zerr)

	sumw := h.arr.Data[i]
	sumw2 := 0.0
	if len(h.th1.sumw2.Data) > 0 {
		sumw2 = h.th1.sumw2.Data[i]
	}
	return hbook.Dist3D{
		X: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     nx,
				SumW:  float64(sumw),
				SumW2: float64(sumw2),
			},
		},
		Y: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     ny,
				SumW:  float64(sumw),
				SumW2: float64(sumw2),
			},
		},
		Z: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     nz,
				SumW:  float64(sumw),
				SumW2: float64(sumw2),
			},
		},
	}
}

func (h *H3I) setDist3D(ix, iy, iz int, sumw, sumw2 float64) {
	i := h.bin(ix, iy, iz)
	h.arr.Data[i] = int32(sumw)
	h.th1.sumw2.Data[i] = sumw2
}

func (h *H3I) entries(height, err float64) int64 {
	if height <= 0 {
		return 0
	}
	v := height / err
	return int64(v*v + 0.5)
}

// AsH3D creates a new hbook.H3D from this ROOT histogram.
func (h *H3I) AsH3D() *hbook.H3D {
	var (
		nx = h.NbinsX()
		ny = h.NbinsY()
		nz = h.NbinsZ()
		hh = hbook.NewH3D(
			nx, h.XAxis().XMin(), h.XAxis().XMax(),
			ny, h.YAxis().XMin(), h.YAxis().XMax(),
			nz, h.ZAxis().XMin(), h.ZAxis().XMax(),
		)
	)
	hh.Ann = hbook.Annotation{
		"name":  h.Name(),
		"title": h.Title(),
	}

	for sx := -1; sx <= +1; sx++ {
		for sy := -1; sy <= +1; sy++ {
			for sz := -1; sz <= +1; sz++ {
				if sx == 0 && sy == 0 && sz == 0 {
					continue
				}
				hh.Binning.Outflows[hbook.Outflow3D(sx, sy, sz)] = h.dist3D(
					h3cell(sx, nx), h3cell(sy, ny), h3cell(sz, nz),
				)
			}
		}
	}

	hh.Binning.Dist = hbook.Dist3D{
		X: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     int64(h.Entries()),
				SumW:  float64(h.SumW()),
				SumW2: float64(h.SumW2()),
			},
		},
		Y: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     int64(h.Entries()),
				SumW:  float64(h.SumW()),
				SumW2: float64(h.SumW2()),
			},
		},
		Z: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     int64(h.Entries()),
				SumW:  float64(h.SumW()),
				SumW2: float64(h.SumW2()),
			},
		},
	}
	hh.Binning.Dist.X.Stats.SumWX = float64(h.SumWX())
	hh.Binning.Dist.X.Stats.SumWX2 = float64(h.SumWX2())
	hh.Binning.Dist.Y.Stats.SumWX = float64(h.SumWY())
	hh.Binning.Dist.Y.Stats.SumWX2 = float64(h.SumWY2())
	hh.Binning.Dist.Z.Stats.SumWX = float64(h.SumWZ())
	hh.Binning.Dist.Z.Stats.SumWX2 = float64(h.SumWZ2())
	hh.Binning.Dist.Stats.SumWXY = h.SumWXY()
	hh.Binning.Dist.Stats.SumWXZ = h.SumWXZ()
	hh.Binning.Dist.Stats.SumWYZ = h.SumWYZ()

	for ix := range nx {
		for iy := range ny {
			for iz := range nz {
				var (
					i    = (iz*ny+iy)*nx + ix
					xmin = h.XBinLowEdge(ix + 1)
					xmax = h.XBinWidth(ix+1) + xmin
					ymin = h.YBinLowEdge(iy + 1)
					ymax = h.YBinWidth(iy+1) + ymin
					zmin = h.ZBinLowEdge(iz + 1)
					zmax = h.ZBinWidth(iz+1) + zmin
					bin  = &hh.Binning.Bins[i]
				)
				bin.XRange.Min = xmin
				bin.XRange.Max = xmax
				bin.YRange.Min = ymin
				bin.YRange.Max = ymax
				bin.ZRange.Min = zmin
				bin.ZRange.Max = zmax
				bin.Dist = h.dist3D(ix+1, iy+1, iz+1)
			}
		}
	}

	return hh
}

func (h *H3I) ROOTMerge(src root.Object) error {
	hsrc, ok := src.(*H3I)
	if !ok {
		return fmt.Errorf("rhist: object %q is not a *rhist.H3I (%T)", src.(root.Named).Name(), src)
	}

	var (
		h1   = h.AsH3D()
		h2   = hsrc.AsH3D()
		hadd = hbook.AddH3D(h1, h2)
	)

	*h = *NewH3IFrom(hadd)
	return nil
}

func (h *H3I) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(h.Class(), h.RVersion())
	w.WriteObject(&h.th3)
	w.WriteObject(&h.arr)

	return w.SetHeader(hdr)
}

func (h *H3I) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(h.Class(), h.RVersion())
	if hdr.Vers < 1 {
		return fmt.Errorf("rhist: TH3I version too old (%d<1)", hdr.Vers)
	}

	r.ReadObject(&h.th3)
	r.ReadObject(&h.arr)

	r.CheckHeader(hdr)
	return r.Err()
}

func (h *H3I) RMembers() (mbrs []rbytes.Member) {
	mbrs = append(mbrs, h.th3.RMembers()...)
	mbrs = append(mbrs, rbytes.Member{
		Name: "fArray", Value: &h.arr.Data,
	})
	return mbrs
}

func init() {
	f := func() reflect.Value {
		o := newH3I()
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TH3I", f)
}

var (
	_ root.Object        = (*H3I)(nil)
	_ root.Merger        = (*H3I)(nil)
	_ root.Named         = (*H3I)(nil)
	_ H3                 = (*H3I)(nil)
	_ rbytes.Marshaler   = (*H3I)(nil)
	_ rbytes.Unmarshaler = (*H3I)(nil)
	_ rbytes.RSlicer     = (*H3I)(nil)
)

// H3F implements ROOT TH3F
type H3F struct {
	th3
	arr rcont.ArrayF
}

func newH3F() *H3F {
	return &H3F{
		th3: *newH3(),
	}
}

// NewH3FFrom creates a new H3F from an hbook 3-dim histogram.
func NewH3FFrom(h *hbook.H3D) *H3F {
	var (
		hroot  = newH3F()
		bins   = h.Binning.Bins
		nxbins = h.Binning.Nx
		nybins = h.Binning.Ny
		nzbins = h.Binning.Nz
		xedges = make([]float64, 0, nxbins+1)
		yedges = make([]float64, 0, nybins+1)
		zedges = make([]float64, 0, nzbins+1)
	)

	hroot.th3.th1.entries = float64(h.Entries())
	hroot.th3.th1.tsumw = h.SumW()
	hroot.th3.th1.tsumw2 = h.SumW2()
	hroot.th3.th1.tsumwx = h.SumWX()
	hroot.th3.th1.tsumwx2 = h.SumWX2()
	hroot.th3.tsumwy = h.SumWY()
	hroot.th3.tsumwy2 = h.SumWY2()
	hroot.th3.tsumwxy = h.SumWXY()
	hroot.th3.tsumwz = h.SumWZ()
	hroot.th3.tsumwz2 = h.SumWZ2()
	hroot.th3.tsumwxz = h.SumWXZ()
	hroot.th3.tsumwyz = h.SumWYZ()

	ncells := (nxbins + 2) * (nybins + 2) * (nzbins + 2)
	hroot.th3.th1.ncells = ncells

	hroot.th3.th1.xaxis.nbins = nxbins
	hroot.th3.th1.xaxis.xmin = h.XMin()
	hroot.th3.th1.xaxis.xmax = h.XMax()

	hroot.th3.th1.yaxis.nbins = nybins
	hroot.th3.th1.yaxis.xmin = h.YMin()
	hroot.th3.th1.yaxis.xmax = h.YMax()

	hroot.th3.th1.zaxis.nbins = nzbins
	hroot.th3.th1.zaxis.xmin = h.ZMin()
	hroot.th3.th1.zaxis.xmax = h.ZMax()

	hroot.arr.Data = make([]float32, ncells)
	hroot.th3.th1.sumw2.Data = make([]float64, ncells)

	ibin := func(ix, iy, iz int) int { return (iz*nybins+iy)*nxbins + ix }

	for ix := range nxbins {
		for iy := range nybins {
			for iz := range nzbins {
				bin := bins[ibin(ix, iy, iz)]
				if iy == 0 && iz == 0 {
					xedges = append(xedges, bin.XMin())
				}
				if ix == 0 && iz == 0 {
					yedges = append(yedges, bin.YMin())
				}
				if ix == 0 && iy == 0 {
					zedges = append(zedges, bin.ZMin())
				}
				hroot.setDist3D(ix+1, iy+1, iz+1, bin.Dist.SumW(), bin.Dist.SumW2())
			}
		}
	}

	// the 26 ways of missing a 3-dim binning, each landing in the slice of
	// cells ROOT keeps for it.
	for sx := -1; sx <= +1; sx++ {
		for sy := -1; sy <= +1; sy++ {
			for sz := -1; sz <= +1; sz++ {
				if sx == 0 && sy == 0 && sz == 0 {
					continue
				}
				o := h.Binning.Outflows[hbook.Outflow3D(sx, sy, sz)]
				hroot.setDist3D(
					h3cell(sx, nxbins), h3cell(sy, nybins), h3cell(sz, nzbins),
					o.SumW(), o.SumW2(),
				)
			}
		}
	}

	xedges = append(xedges, bins[ibin(nxbins-1, 0, 0)].XMax())
	yedges = append(yedges, bins[ibin(0, nybins-1, 0)].YMax())
	zedges = append(zedges, bins[ibin(0, 0, nzbins-1)].ZMax())

	hroot.th3.th1.SetName(h.Name())
	if v, ok := h.Annotation()["title"]; ok && v != nil {
		hroot.th3.th1.SetTitle(v.(string))
	}
	hroot.th3.th1.xaxis.xbins.Data = xedges
	hroot.th3.th1.yaxis.xbins.Data = yedges
	hroot.th3.th1.zaxis.xbins.Data = zedges

	return hroot
}

// NewH3F creates a 3-dim histogram, as "new TH3F(name, title,
// nx, xmin, xmax, ny, ymin, ymax, nz, zmin, zmax)" does in C++.
func NewH3F(name, title string, nx int, xmin, xmax float64, ny int, ymin, ymax float64, nz int, zmin, zmax float64) *H3F {
	h := newH3F()
	h.th3.th1.SetName(name)
	h.th3.th1.SetTitle(title)
	h.th3.th1.xaxis.setRange(nx, xmin, xmax)
	h.th3.th1.yaxis.setRange(ny, ymin, ymax)
	h.th3.th1.zaxis.setRange(nz, zmin, zmax)
	h.reset()
	return h
}

// reset sizes the cells to the axes and empties them.
func (h *H3F) reset() {
	n := (h.th1.xaxis.nbins + 2) * (h.th1.yaxis.nbins + 2) * (h.th1.zaxis.nbins + 2)
	h.th3.th1.ncells = n
	h.arr.Data = make([]float32, n)
	h.th3.th1.sumw2.Data = make([]float64, n)
	h.th3.th1.entries = 0
	h.th3.th1.tsumw = 0
	h.th3.th1.tsumw2 = 0
	h.th3.th1.tsumwx = 0
	h.th3.th1.tsumwx2 = 0
	h.th3.tsumwy = 0
	h.th3.tsumwy2 = 0
	h.th3.tsumwxy = 0
	h.th3.tsumwz = 0
	h.th3.tsumwz2 = 0
	h.th3.tsumwxz = 0
	h.th3.tsumwyz = 0
}

// Reset empties the histogram, keeping its binning.
func (h *H3F) Reset() { h.reset() }

// FindBin returns the cell (x,y,z) falls in, as an index into the flat array
// the histogram keeps.
func (h *H3F) FindBin(x, y, z float64) int {
	return h.bin(
		h.th1.xaxis.FindBin(x),
		h.th1.yaxis.FindBin(y),
		h.th1.zaxis.FindBin(z),
	)
}

// Fill adds an entry of weight w at (x,y,z).
func (h *H3F) Fill(x, y, z, w float64) {
	var (
		ix = h.th1.xaxis.FindBin(x)
		iy = h.th1.yaxis.FindBin(y)
		iz = h.th1.zaxis.FindBin(z)
		i  = h.bin(ix, iy, iz)
	)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}

	h.arr.Data[i] += float32(w)
	if len(h.th1.sumw2.Data) > i {
		h.th1.sumw2.Data[i] += w * w
	}

	h.th3.th1.entries++

	if ix > 0 && ix <= h.th1.xaxis.nbins &&
		iy > 0 && iy <= h.th1.yaxis.nbins &&
		iz > 0 && iz <= h.th1.zaxis.nbins {
		h.th3.th1.tsumw += w
		h.th3.th1.tsumw2 += w * w
		h.th3.th1.tsumwx += w * x
		h.th3.th1.tsumwx2 += w * x * x
		h.th3.tsumwy += w * y
		h.th3.tsumwy2 += w * y * y
		h.th3.tsumwxy += w * x * y
		h.th3.tsumwz += w * z
		h.th3.tsumwz2 += w * z * z
		h.th3.tsumwxz += w * x * z
		h.th3.tsumwyz += w * y * z
	}
}

// FillN adds an entry for each (x,y,z) with the matching weight, or of weight
// one when ws is nil.
func (h *H3F) FillN(xs, ys, zs, ws []float64) {
	if len(ys) != len(xs) || len(zs) != len(xs) || (ws != nil && len(ws) != len(xs)) {
		panic(fmt.Errorf("rhist: lengths mismatch"))
	}
	for i := range xs {
		w := 1.0
		if ws != nil {
			w = ws[i]
		}
		h.Fill(xs[i], ys[i], zs[i], w)
	}
}

// BinContent returns the content of the cell at (ix,iy,iz).
func (h *H3F) BinContent(ix, iy, iz int) float64 {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	return float64(h.arr.Data[i])
}

// SetBinContent sets the content of the cell at (ix,iy,iz).
func (h *H3F) SetBinContent(ix, iy, iz int, v float64) {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}
	h.arr.Data[i] = float32(v)
}

// BinError returns the uncertainty on the cell at (ix,iy,iz).
func (h *H3F) BinError(ix, iy, iz int) float64 {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	if len(h.th1.sumw2.Data) > i {
		return math.Sqrt(h.th1.sumw2.Data[i])
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// SetBinError sets the uncertainty on the cell at (ix,iy,iz).
func (h *H3F) SetBinError(ix, iy, iz int, e float64) {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.th1.sumw2.Data) {
		return
	}
	h.th1.sumw2.Data[i] = e * e
}

// Scale multiplies every cell by f, and the uncertainties with them.
func (h *H3F) Scale(f float64) {
	for i := range h.arr.Data {
		h.arr.Data[i] = float32(float64(h.arr.Data[i]) * f)
	}
	for i := range h.th1.sumw2.Data {
		h.th1.sumw2.Data[i] *= f * f
	}
	h.th3.th1.tsumw *= f
	h.th3.th1.tsumw2 *= f * f
	h.th3.th1.tsumwx *= f
	h.th3.th1.tsumwx2 *= f
	h.th3.tsumwy *= f
	h.th3.tsumwy2 *= f
	h.th3.tsumwxy *= f
	h.th3.tsumwz *= f
	h.th3.tsumwz2 *= f
	h.th3.tsumwxz *= f
	h.th3.tsumwyz *= f
}

// Integral returns the sum of the bins proper, leaving out the under- and
// overflow.
func (h *H3F) Integral() float64 {
	var sum float64
	for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
		for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
			for iz := 1; iz <= h.th1.zaxis.nbins; iz++ {
				sum += h.BinContent(ix, iy, iz)
			}
		}
	}
	return sum
}

// ProjectionZ sums the histogram over x and y, as TH3::ProjectionZ does.
func (h *H3F) ProjectionZ(name string) *H1D {
	o := NewH1D(name, h.Title(), h.th1.zaxis.nbins, h.th1.zaxis.xmin, h.th1.zaxis.xmax)

	for iz := 1; iz <= h.th1.zaxis.nbins; iz++ {
		var sum, err2 float64
		for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
			for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
				sum += h.BinContent(ix, iy, iz)
				e := h.BinError(ix, iy, iz)
				err2 += e * e
			}
		}
		o.SetBinContent(iz, sum)
		o.SetBinError(iz, math.Sqrt(err2))
	}

	o.th1.entries = h.th1.entries
	o.th1.tsumw = h.th3.th1.tsumw
	o.th1.tsumw2 = h.th3.th1.tsumw2
	o.th1.tsumwx = h.th3.tsumwz
	o.th1.tsumwx2 = h.th3.tsumwz2
	return o
}

// ProjectionXY sums the histogram over z, as TH3::Project3D("xy") does.
func (h *H3F) ProjectionXY(name string) *H2D {
	o := NewH2D(name, h.Title(),
		h.th1.xaxis.nbins, h.th1.xaxis.xmin, h.th1.xaxis.xmax,
		h.th1.yaxis.nbins, h.th1.yaxis.xmin, h.th1.yaxis.xmax,
	)

	for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
		for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
			var sum, err2 float64
			for iz := 1; iz <= h.th1.zaxis.nbins; iz++ {
				sum += h.BinContent(ix, iy, iz)
				e := h.BinError(ix, iy, iz)
				err2 += e * e
			}
			o.SetBinContent(ix, iy, sum)
			o.SetBinError(ix, iy, math.Sqrt(err2))
		}
	}

	o.th2.th1.entries = h.th1.entries
	o.th2.th1.tsumw = h.th3.th1.tsumw
	o.th2.th1.tsumw2 = h.th3.th1.tsumw2
	o.th2.th1.tsumwx = h.th3.th1.tsumwx
	o.th2.th1.tsumwx2 = h.th3.th1.tsumwx2
	o.th2.tsumwy = h.th3.tsumwy
	o.th2.tsumwy2 = h.th3.tsumwy2
	o.th2.tsumwxy = h.th3.tsumwxy
	return o
}

// MeanX, MeanY and MeanZ return the means of the entries along each axis.
func (h *H3F) MeanX() float64 {
	if h.th3.th1.tsumw == 0 {
		return 0
	}
	return h.th3.th1.tsumwx / h.th3.th1.tsumw
}

// MeanY returns the mean along y.
func (h *H3F) MeanY() float64 {
	if h.th3.th1.tsumw == 0 {
		return 0
	}
	return h.th3.tsumwy / h.th3.th1.tsumw
}

// MeanZ returns the mean along z.
func (h *H3F) MeanZ() float64 {
	if h.th3.th1.tsumw == 0 {
		return 0
	}
	return h.th3.tsumwz / h.th3.th1.tsumw
}

func (*H3F) RVersion() int16 {
	return rvers.H3F
}

func (*H3F) isH3() {}

// Class returns the ROOT class name.
func (*H3F) Class() string {
	return "TH3F"
}

func (h *H3F) Array() rcont.ArrayF {
	return h.arr
}

// Rank returns the number of dimensions of this histogram.
func (h *H3F) Rank() int {
	return 3
}

// NbinsX returns the number of bins in X.
func (h *H3F) NbinsX() int {
	return h.th1.xaxis.nbins
}

// XAxis returns the axis along X.
func (h *H3F) XAxis() Axis {
	return &h.th1.xaxis
}

// XBinCenter returns the bin center value in X.
func (h *H3F) XBinCenter(i int) float64 {
	return float64(h.th1.xaxis.BinCenter(i))
}

// XBinContent returns the bin content value in X.
func (h *H3F) XBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// XBinError returns the bin error in X.
func (h *H3F) XBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// XBinLowEdge returns the bin lower edge value in X.
func (h *H3F) XBinLowEdge(i int) float64 {
	return h.th1.xaxis.BinLowEdge(i)
}

// XBinWidth returns the bin width in X.
func (h *H3F) XBinWidth(i int) float64 {
	return h.th1.xaxis.BinWidth(i)
}

// NbinsY returns the number of bins in Y.
func (h *H3F) NbinsY() int {
	return h.th1.yaxis.nbins
}

// YAxis returns the axis along Y.
func (h *H3F) YAxis() Axis {
	return &h.th1.yaxis
}

// YBinCenter returns the bin center value in Y.
func (h *H3F) YBinCenter(i int) float64 {
	return float64(h.th1.yaxis.BinCenter(i))
}

// YBinContent returns the bin content value in Y.
func (h *H3F) YBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// YBinError returns the bin error in Y.
func (h *H3F) YBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// YBinLowEdge returns the bin lower edge value in Y.
func (h *H3F) YBinLowEdge(i int) float64 {
	return h.th1.yaxis.BinLowEdge(i)
}

// YBinWidth returns the bin width in Y.
func (h *H3F) YBinWidth(i int) float64 {
	return h.th1.yaxis.BinWidth(i)
}

// NbinsZ returns the number of bins in Z.
func (h *H3F) NbinsZ() int {
	return h.th1.zaxis.nbins
}

// ZAxis returns the axis along Z.
func (h *H3F) ZAxis() Axis {
	return &h.th1.zaxis
}

// ZBinCenter returns the bin center value in Z.
func (h *H3F) ZBinCenter(i int) float64 {
	return float64(h.th1.zaxis.BinCenter(i))
}

// ZBinContent returns the bin content value in Z.
func (h *H3F) ZBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// ZBinError returns the bin error in Z.
func (h *H3F) ZBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// ZBinLowEdge returns the bin lower edge value in Z.
func (h *H3F) ZBinLowEdge(i int) float64 {
	return h.th1.zaxis.BinLowEdge(i)
}

// ZBinWidth returns the bin width in Z.
func (h *H3F) ZBinWidth(i int) float64 {
	return h.th1.zaxis.BinWidth(i)
}

// bin returns the regularized bin number given an (x,y,z) bin index triple.
func (h *H3F) bin(ix, iy, iz int) int {
	nx := h.th1.xaxis.nbins + 1 // overflow bin
	ny := h.th1.yaxis.nbins + 1 // overflow bin
	nz := h.th1.zaxis.nbins + 1 // overflow bin
	switch {
	case ix < 0:
		ix = 0
	case ix > nx:
		ix = nx
	}
	switch {
	case iy < 0:
		iy = 0
	case iy > ny:
		iy = ny
	}
	switch {
	case iz < 0:
		iz = 0
	case iz > nz:
		iz = nz
	}
	return ix + (nx+1)*(iy+(ny+1)*iz)
}

func (h *H3F) dist3D(ix, iy, iz int) hbook.Dist3D {
	i := h.bin(ix, iy, iz)
	vx := h.XBinContent(i)
	xerr := h.XBinError(i)
	nx := h.entries(vx, xerr)
	vy := h.YBinContent(i)
	yerr := h.YBinError(i)
	ny := h.entries(vy, yerr)
	vz := h.ZBinContent(i)
	zerr := h.ZBinError(i)
	nz := h.entries(vz, zerr)

	sumw := h.arr.Data[i]
	sumw2 := 0.0
	if len(h.th1.sumw2.Data) > 0 {
		sumw2 = h.th1.sumw2.Data[i]
	}
	return hbook.Dist3D{
		X: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     nx,
				SumW:  float64(sumw),
				SumW2: float64(sumw2),
			},
		},
		Y: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     ny,
				SumW:  float64(sumw),
				SumW2: float64(sumw2),
			},
		},
		Z: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     nz,
				SumW:  float64(sumw),
				SumW2: float64(sumw2),
			},
		},
	}
}

func (h *H3F) setDist3D(ix, iy, iz int, sumw, sumw2 float64) {
	i := h.bin(ix, iy, iz)
	h.arr.Data[i] = float32(sumw)
	h.th1.sumw2.Data[i] = sumw2
}

func (h *H3F) entries(height, err float64) int64 {
	if height <= 0 {
		return 0
	}
	v := height / err
	return int64(v*v + 0.5)
}

// AsH3D creates a new hbook.H3D from this ROOT histogram.
func (h *H3F) AsH3D() *hbook.H3D {
	var (
		nx = h.NbinsX()
		ny = h.NbinsY()
		nz = h.NbinsZ()
		hh = hbook.NewH3D(
			nx, h.XAxis().XMin(), h.XAxis().XMax(),
			ny, h.YAxis().XMin(), h.YAxis().XMax(),
			nz, h.ZAxis().XMin(), h.ZAxis().XMax(),
		)
	)
	hh.Ann = hbook.Annotation{
		"name":  h.Name(),
		"title": h.Title(),
	}

	for sx := -1; sx <= +1; sx++ {
		for sy := -1; sy <= +1; sy++ {
			for sz := -1; sz <= +1; sz++ {
				if sx == 0 && sy == 0 && sz == 0 {
					continue
				}
				hh.Binning.Outflows[hbook.Outflow3D(sx, sy, sz)] = h.dist3D(
					h3cell(sx, nx), h3cell(sy, ny), h3cell(sz, nz),
				)
			}
		}
	}

	hh.Binning.Dist = hbook.Dist3D{
		X: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     int64(h.Entries()),
				SumW:  float64(h.SumW()),
				SumW2: float64(h.SumW2()),
			},
		},
		Y: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     int64(h.Entries()),
				SumW:  float64(h.SumW()),
				SumW2: float64(h.SumW2()),
			},
		},
		Z: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     int64(h.Entries()),
				SumW:  float64(h.SumW()),
				SumW2: float64(h.SumW2()),
			},
		},
	}
	hh.Binning.Dist.X.Stats.SumWX = float64(h.SumWX())
	hh.Binning.Dist.X.Stats.SumWX2 = float64(h.SumWX2())
	hh.Binning.Dist.Y.Stats.SumWX = float64(h.SumWY())
	hh.Binning.Dist.Y.Stats.SumWX2 = float64(h.SumWY2())
	hh.Binning.Dist.Z.Stats.SumWX = float64(h.SumWZ())
	hh.Binning.Dist.Z.Stats.SumWX2 = float64(h.SumWZ2())
	hh.Binning.Dist.Stats.SumWXY = h.SumWXY()
	hh.Binning.Dist.Stats.SumWXZ = h.SumWXZ()
	hh.Binning.Dist.Stats.SumWYZ = h.SumWYZ()

	for ix := range nx {
		for iy := range ny {
			for iz := range nz {
				var (
					i    = (iz*ny+iy)*nx + ix
					xmin = h.XBinLowEdge(ix + 1)
					xmax = h.XBinWidth(ix+1) + xmin
					ymin = h.YBinLowEdge(iy + 1)
					ymax = h.YBinWidth(iy+1) + ymin
					zmin = h.ZBinLowEdge(iz + 1)
					zmax = h.ZBinWidth(iz+1) + zmin
					bin  = &hh.Binning.Bins[i]
				)
				bin.XRange.Min = xmin
				bin.XRange.Max = xmax
				bin.YRange.Min = ymin
				bin.YRange.Max = ymax
				bin.ZRange.Min = zmin
				bin.ZRange.Max = zmax
				bin.Dist = h.dist3D(ix+1, iy+1, iz+1)
			}
		}
	}

	return hh
}

func (h *H3F) ROOTMerge(src root.Object) error {
	hsrc, ok := src.(*H3F)
	if !ok {
		return fmt.Errorf("rhist: object %q is not a *rhist.H3F (%T)", src.(root.Named).Name(), src)
	}

	var (
		h1   = h.AsH3D()
		h2   = hsrc.AsH3D()
		hadd = hbook.AddH3D(h1, h2)
	)

	*h = *NewH3FFrom(hadd)
	return nil
}

func (h *H3F) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(h.Class(), h.RVersion())
	w.WriteObject(&h.th3)
	w.WriteObject(&h.arr)

	return w.SetHeader(hdr)
}

func (h *H3F) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(h.Class(), h.RVersion())
	if hdr.Vers < 1 {
		return fmt.Errorf("rhist: TH3F version too old (%d<1)", hdr.Vers)
	}

	r.ReadObject(&h.th3)
	r.ReadObject(&h.arr)

	r.CheckHeader(hdr)
	return r.Err()
}

func (h *H3F) RMembers() (mbrs []rbytes.Member) {
	mbrs = append(mbrs, h.th3.RMembers()...)
	mbrs = append(mbrs, rbytes.Member{
		Name: "fArray", Value: &h.arr.Data,
	})
	return mbrs
}

func init() {
	f := func() reflect.Value {
		o := newH3F()
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TH3F", f)
}

var (
	_ root.Object        = (*H3F)(nil)
	_ root.Merger        = (*H3F)(nil)
	_ root.Named         = (*H3F)(nil)
	_ H3                 = (*H3F)(nil)
	_ rbytes.Marshaler   = (*H3F)(nil)
	_ rbytes.Unmarshaler = (*H3F)(nil)
	_ rbytes.RSlicer     = (*H3F)(nil)
)

// H3D implements ROOT TH3D
type H3D struct {
	th3
	arr rcont.ArrayD
}

func newH3D() *H3D {
	return &H3D{
		th3: *newH3(),
	}
}

// NewH3DFrom creates a new H3D from an hbook 3-dim histogram.
func NewH3DFrom(h *hbook.H3D) *H3D {
	var (
		hroot  = newH3D()
		bins   = h.Binning.Bins
		nxbins = h.Binning.Nx
		nybins = h.Binning.Ny
		nzbins = h.Binning.Nz
		xedges = make([]float64, 0, nxbins+1)
		yedges = make([]float64, 0, nybins+1)
		zedges = make([]float64, 0, nzbins+1)
	)

	hroot.th3.th1.entries = float64(h.Entries())
	hroot.th3.th1.tsumw = h.SumW()
	hroot.th3.th1.tsumw2 = h.SumW2()
	hroot.th3.th1.tsumwx = h.SumWX()
	hroot.th3.th1.tsumwx2 = h.SumWX2()
	hroot.th3.tsumwy = h.SumWY()
	hroot.th3.tsumwy2 = h.SumWY2()
	hroot.th3.tsumwxy = h.SumWXY()
	hroot.th3.tsumwz = h.SumWZ()
	hroot.th3.tsumwz2 = h.SumWZ2()
	hroot.th3.tsumwxz = h.SumWXZ()
	hroot.th3.tsumwyz = h.SumWYZ()

	ncells := (nxbins + 2) * (nybins + 2) * (nzbins + 2)
	hroot.th3.th1.ncells = ncells

	hroot.th3.th1.xaxis.nbins = nxbins
	hroot.th3.th1.xaxis.xmin = h.XMin()
	hroot.th3.th1.xaxis.xmax = h.XMax()

	hroot.th3.th1.yaxis.nbins = nybins
	hroot.th3.th1.yaxis.xmin = h.YMin()
	hroot.th3.th1.yaxis.xmax = h.YMax()

	hroot.th3.th1.zaxis.nbins = nzbins
	hroot.th3.th1.zaxis.xmin = h.ZMin()
	hroot.th3.th1.zaxis.xmax = h.ZMax()

	hroot.arr.Data = make([]float64, ncells)
	hroot.th3.th1.sumw2.Data = make([]float64, ncells)

	ibin := func(ix, iy, iz int) int { return (iz*nybins+iy)*nxbins + ix }

	for ix := range nxbins {
		for iy := range nybins {
			for iz := range nzbins {
				bin := bins[ibin(ix, iy, iz)]
				if iy == 0 && iz == 0 {
					xedges = append(xedges, bin.XMin())
				}
				if ix == 0 && iz == 0 {
					yedges = append(yedges, bin.YMin())
				}
				if ix == 0 && iy == 0 {
					zedges = append(zedges, bin.ZMin())
				}
				hroot.setDist3D(ix+1, iy+1, iz+1, bin.Dist.SumW(), bin.Dist.SumW2())
			}
		}
	}

	// the 26 ways of missing a 3-dim binning, each landing in the slice of
	// cells ROOT keeps for it.
	for sx := -1; sx <= +1; sx++ {
		for sy := -1; sy <= +1; sy++ {
			for sz := -1; sz <= +1; sz++ {
				if sx == 0 && sy == 0 && sz == 0 {
					continue
				}
				o := h.Binning.Outflows[hbook.Outflow3D(sx, sy, sz)]
				hroot.setDist3D(
					h3cell(sx, nxbins), h3cell(sy, nybins), h3cell(sz, nzbins),
					o.SumW(), o.SumW2(),
				)
			}
		}
	}

	xedges = append(xedges, bins[ibin(nxbins-1, 0, 0)].XMax())
	yedges = append(yedges, bins[ibin(0, nybins-1, 0)].YMax())
	zedges = append(zedges, bins[ibin(0, 0, nzbins-1)].ZMax())

	hroot.th3.th1.SetName(h.Name())
	if v, ok := h.Annotation()["title"]; ok && v != nil {
		hroot.th3.th1.SetTitle(v.(string))
	}
	hroot.th3.th1.xaxis.xbins.Data = xedges
	hroot.th3.th1.yaxis.xbins.Data = yedges
	hroot.th3.th1.zaxis.xbins.Data = zedges

	return hroot
}

// NewH3D creates a 3-dim histogram, as "new TH3D(name, title,
// nx, xmin, xmax, ny, ymin, ymax, nz, zmin, zmax)" does in C++.
func NewH3D(name, title string, nx int, xmin, xmax float64, ny int, ymin, ymax float64, nz int, zmin, zmax float64) *H3D {
	h := newH3D()
	h.th3.th1.SetName(name)
	h.th3.th1.SetTitle(title)
	h.th3.th1.xaxis.setRange(nx, xmin, xmax)
	h.th3.th1.yaxis.setRange(ny, ymin, ymax)
	h.th3.th1.zaxis.setRange(nz, zmin, zmax)
	h.reset()
	return h
}

// reset sizes the cells to the axes and empties them.
func (h *H3D) reset() {
	n := (h.th1.xaxis.nbins + 2) * (h.th1.yaxis.nbins + 2) * (h.th1.zaxis.nbins + 2)
	h.th3.th1.ncells = n
	h.arr.Data = make([]float64, n)
	h.th3.th1.sumw2.Data = make([]float64, n)
	h.th3.th1.entries = 0
	h.th3.th1.tsumw = 0
	h.th3.th1.tsumw2 = 0
	h.th3.th1.tsumwx = 0
	h.th3.th1.tsumwx2 = 0
	h.th3.tsumwy = 0
	h.th3.tsumwy2 = 0
	h.th3.tsumwxy = 0
	h.th3.tsumwz = 0
	h.th3.tsumwz2 = 0
	h.th3.tsumwxz = 0
	h.th3.tsumwyz = 0
}

// Reset empties the histogram, keeping its binning.
func (h *H3D) Reset() { h.reset() }

// FindBin returns the cell (x,y,z) falls in, as an index into the flat array
// the histogram keeps.
func (h *H3D) FindBin(x, y, z float64) int {
	return h.bin(
		h.th1.xaxis.FindBin(x),
		h.th1.yaxis.FindBin(y),
		h.th1.zaxis.FindBin(z),
	)
}

// Fill adds an entry of weight w at (x,y,z).
func (h *H3D) Fill(x, y, z, w float64) {
	var (
		ix = h.th1.xaxis.FindBin(x)
		iy = h.th1.yaxis.FindBin(y)
		iz = h.th1.zaxis.FindBin(z)
		i  = h.bin(ix, iy, iz)
	)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}

	h.arr.Data[i] += float64(w)
	if len(h.th1.sumw2.Data) > i {
		h.th1.sumw2.Data[i] += w * w
	}

	h.th3.th1.entries++

	if ix > 0 && ix <= h.th1.xaxis.nbins &&
		iy > 0 && iy <= h.th1.yaxis.nbins &&
		iz > 0 && iz <= h.th1.zaxis.nbins {
		h.th3.th1.tsumw += w
		h.th3.th1.tsumw2 += w * w
		h.th3.th1.tsumwx += w * x
		h.th3.th1.tsumwx2 += w * x * x
		h.th3.tsumwy += w * y
		h.th3.tsumwy2 += w * y * y
		h.th3.tsumwxy += w * x * y
		h.th3.tsumwz += w * z
		h.th3.tsumwz2 += w * z * z
		h.th3.tsumwxz += w * x * z
		h.th3.tsumwyz += w * y * z
	}
}

// FillN adds an entry for each (x,y,z) with the matching weight, or of weight
// one when ws is nil.
func (h *H3D) FillN(xs, ys, zs, ws []float64) {
	if len(ys) != len(xs) || len(zs) != len(xs) || (ws != nil && len(ws) != len(xs)) {
		panic(fmt.Errorf("rhist: lengths mismatch"))
	}
	for i := range xs {
		w := 1.0
		if ws != nil {
			w = ws[i]
		}
		h.Fill(xs[i], ys[i], zs[i], w)
	}
}

// BinContent returns the content of the cell at (ix,iy,iz).
func (h *H3D) BinContent(ix, iy, iz int) float64 {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	return float64(h.arr.Data[i])
}

// SetBinContent sets the content of the cell at (ix,iy,iz).
func (h *H3D) SetBinContent(ix, iy, iz int, v float64) {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}
	h.arr.Data[i] = float64(v)
}

// BinError returns the uncertainty on the cell at (ix,iy,iz).
func (h *H3D) BinError(ix, iy, iz int) float64 {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	if len(h.th1.sumw2.Data) > i {
		return math.Sqrt(h.th1.sumw2.Data[i])
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// SetBinError sets the uncertainty on the cell at (ix,iy,iz).
func (h *H3D) SetBinError(ix, iy, iz int, e float64) {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.th1.sumw2.Data) {
		return
	}
	h.th1.sumw2.Data[i] = e * e
}

// Scale multiplies every cell by f, and the uncertainties with them.
func (h *H3D) Scale(f float64) {
	for i := range h.arr.Data {
		h.arr.Data[i] = float64(float64(h.arr.Data[i]) * f)
	}
	for i := range h.th1.sumw2.Data {
		h.th1.sumw2.Data[i] *= f * f
	}
	h.th3.th1.tsumw *= f
	h.th3.th1.tsumw2 *= f * f
	h.th3.th1.tsumwx *= f
	h.th3.th1.tsumwx2 *= f
	h.th3.tsumwy *= f
	h.th3.tsumwy2 *= f
	h.th3.tsumwxy *= f
	h.th3.tsumwz *= f
	h.th3.tsumwz2 *= f
	h.th3.tsumwxz *= f
	h.th3.tsumwyz *= f
}

// Integral returns the sum of the bins proper, leaving out the under- and
// overflow.
func (h *H3D) Integral() float64 {
	var sum float64
	for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
		for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
			for iz := 1; iz <= h.th1.zaxis.nbins; iz++ {
				sum += h.BinContent(ix, iy, iz)
			}
		}
	}
	return sum
}

// ProjectionZ sums the histogram over x and y, as TH3::ProjectionZ does.
func (h *H3D) ProjectionZ(name string) *H1D {
	o := NewH1D(name, h.Title(), h.th1.zaxis.nbins, h.th1.zaxis.xmin, h.th1.zaxis.xmax)

	for iz := 1; iz <= h.th1.zaxis.nbins; iz++ {
		var sum, err2 float64
		for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
			for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
				sum += h.BinContent(ix, iy, iz)
				e := h.BinError(ix, iy, iz)
				err2 += e * e
			}
		}
		o.SetBinContent(iz, sum)
		o.SetBinError(iz, math.Sqrt(err2))
	}

	o.th1.entries = h.th1.entries
	o.th1.tsumw = h.th3.th1.tsumw
	o.th1.tsumw2 = h.th3.th1.tsumw2
	o.th1.tsumwx = h.th3.tsumwz
	o.th1.tsumwx2 = h.th3.tsumwz2
	return o
}

// ProjectionXY sums the histogram over z, as TH3::Project3D("xy") does.
func (h *H3D) ProjectionXY(name string) *H2D {
	o := NewH2D(name, h.Title(),
		h.th1.xaxis.nbins, h.th1.xaxis.xmin, h.th1.xaxis.xmax,
		h.th1.yaxis.nbins, h.th1.yaxis.xmin, h.th1.yaxis.xmax,
	)

	for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
		for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
			var sum, err2 float64
			for iz := 1; iz <= h.th1.zaxis.nbins; iz++ {
				sum += h.BinContent(ix, iy, iz)
				e := h.BinError(ix, iy, iz)
				err2 += e * e
			}
			o.SetBinContent(ix, iy, sum)
			o.SetBinError(ix, iy, math.Sqrt(err2))
		}
	}

	o.th2.th1.entries = h.th1.entries
	o.th2.th1.tsumw = h.th3.th1.tsumw
	o.th2.th1.tsumw2 = h.th3.th1.tsumw2
	o.th2.th1.tsumwx = h.th3.th1.tsumwx
	o.th2.th1.tsumwx2 = h.th3.th1.tsumwx2
	o.th2.tsumwy = h.th3.tsumwy
	o.th2.tsumwy2 = h.th3.tsumwy2
	o.th2.tsumwxy = h.th3.tsumwxy
	return o
}

// MeanX, MeanY and MeanZ return the means of the entries along each axis.
func (h *H3D) MeanX() float64 {
	if h.th3.th1.tsumw == 0 {
		return 0
	}
	return h.th3.th1.tsumwx / h.th3.th1.tsumw
}

// MeanY returns the mean along y.
func (h *H3D) MeanY() float64 {
	if h.th3.th1.tsumw == 0 {
		return 0
	}
	return h.th3.tsumwy / h.th3.th1.tsumw
}

// MeanZ returns the mean along z.
func (h *H3D) MeanZ() float64 {
	if h.th3.th1.tsumw == 0 {
		return 0
	}
	return h.th3.tsumwz / h.th3.th1.tsumw
}

func (*H3D) RVersion() int16 {
	return rvers.H3D
}

func (*H3D) isH3() {}

// Class returns the ROOT class name.
func (*H3D) Class() string {
	return "TH3D"
}

func (h *H3D) Array() rcont.ArrayD {
	return h.arr
}

// Rank returns the number of dimensions of this histogram.
func (h *H3D) Rank() int {
	return 3
}

// NbinsX returns the number of bins in X.
func (h *H3D) NbinsX() int {
	return h.th1.xaxis.nbins
}

// XAxis returns the axis along X.
func (h *H3D) XAxis() Axis {
	return &h.th1.xaxis
}

// XBinCenter returns the bin center value in X.
func (h *H3D) XBinCenter(i int) float64 {
	return float64(h.th1.xaxis.BinCenter(i))
}

// XBinContent returns the bin content value in X.
func (h *H3D) XBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// XBinError returns the bin error in X.
func (h *H3D) XBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// XBinLowEdge returns the bin lower edge value in X.
func (h *H3D) XBinLowEdge(i int) float64 {
	return h.th1.xaxis.BinLowEdge(i)
}

// XBinWidth returns the bin width in X.
func (h *H3D) XBinWidth(i int) float64 {
	return h.th1.xaxis.BinWidth(i)
}

// NbinsY returns the number of bins in Y.
func (h *H3D) NbinsY() int {
	return h.th1.yaxis.nbins
}

// YAxis returns the axis along Y.
func (h *H3D) YAxis() Axis {
	return &h.th1.yaxis
}

// YBinCenter returns the bin center value in Y.
func (h *H3D) YBinCenter(i int) float64 {
	return float64(h.th1.yaxis.BinCenter(i))
}

// YBinContent returns the bin content value in Y.
func (h *H3D) YBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// YBinError returns the bin error in Y.
func (h *H3D) YBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// YBinLowEdge returns the bin lower edge value in Y.
func (h *H3D) YBinLowEdge(i int) float64 {
	return h.th1.yaxis.BinLowEdge(i)
}

// YBinWidth returns the bin width in Y.
func (h *H3D) YBinWidth(i int) float64 {
	return h.th1.yaxis.BinWidth(i)
}

// NbinsZ returns the number of bins in Z.
func (h *H3D) NbinsZ() int {
	return h.th1.zaxis.nbins
}

// ZAxis returns the axis along Z.
func (h *H3D) ZAxis() Axis {
	return &h.th1.zaxis
}

// ZBinCenter returns the bin center value in Z.
func (h *H3D) ZBinCenter(i int) float64 {
	return float64(h.th1.zaxis.BinCenter(i))
}

// ZBinContent returns the bin content value in Z.
func (h *H3D) ZBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// ZBinError returns the bin error in Z.
func (h *H3D) ZBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// ZBinLowEdge returns the bin lower edge value in Z.
func (h *H3D) ZBinLowEdge(i int) float64 {
	return h.th1.zaxis.BinLowEdge(i)
}

// ZBinWidth returns the bin width in Z.
func (h *H3D) ZBinWidth(i int) float64 {
	return h.th1.zaxis.BinWidth(i)
}

// bin returns the regularized bin number given an (x,y,z) bin index triple.
func (h *H3D) bin(ix, iy, iz int) int {
	nx := h.th1.xaxis.nbins + 1 // overflow bin
	ny := h.th1.yaxis.nbins + 1 // overflow bin
	nz := h.th1.zaxis.nbins + 1 // overflow bin
	switch {
	case ix < 0:
		ix = 0
	case ix > nx:
		ix = nx
	}
	switch {
	case iy < 0:
		iy = 0
	case iy > ny:
		iy = ny
	}
	switch {
	case iz < 0:
		iz = 0
	case iz > nz:
		iz = nz
	}
	return ix + (nx+1)*(iy+(ny+1)*iz)
}

func (h *H3D) dist3D(ix, iy, iz int) hbook.Dist3D {
	i := h.bin(ix, iy, iz)
	vx := h.XBinContent(i)
	xerr := h.XBinError(i)
	nx := h.entries(vx, xerr)
	vy := h.YBinContent(i)
	yerr := h.YBinError(i)
	ny := h.entries(vy, yerr)
	vz := h.ZBinContent(i)
	zerr := h.ZBinError(i)
	nz := h.entries(vz, zerr)

	sumw := h.arr.Data[i]
	sumw2 := 0.0
	if len(h.th1.sumw2.Data) > 0 {
		sumw2 = h.th1.sumw2.Data[i]
	}
	return hbook.Dist3D{
		X: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     nx,
				SumW:  float64(sumw),
				SumW2: float64(sumw2),
			},
		},
		Y: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     ny,
				SumW:  float64(sumw),
				SumW2: float64(sumw2),
			},
		},
		Z: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     nz,
				SumW:  float64(sumw),
				SumW2: float64(sumw2),
			},
		},
	}
}

func (h *H3D) setDist3D(ix, iy, iz int, sumw, sumw2 float64) {
	i := h.bin(ix, iy, iz)
	h.arr.Data[i] = float64(sumw)
	h.th1.sumw2.Data[i] = sumw2
}

func (h *H3D) entries(height, err float64) int64 {
	if height <= 0 {
		return 0
	}
	v := height / err
	return int64(v*v + 0.5)
}

// AsH3D creates a new hbook.H3D from this ROOT histogram.
func (h *H3D) AsH3D() *hbook.H3D {
	var (
		nx = h.NbinsX()
		ny = h.NbinsY()
		nz = h.NbinsZ()
		hh = hbook.NewH3D(
			nx, h.XAxis().XMin(), h.XAxis().XMax(),
			ny, h.YAxis().XMin(), h.YAxis().XMax(),
			nz, h.ZAxis().XMin(), h.ZAxis().XMax(),
		)
	)
	hh.Ann = hbook.Annotation{
		"name":  h.Name(),
		"title": h.Title(),
	}

	for sx := -1; sx <= +1; sx++ {
		for sy := -1; sy <= +1; sy++ {
			for sz := -1; sz <= +1; sz++ {
				if sx == 0 && sy == 0 && sz == 0 {
					continue
				}
				hh.Binning.Outflows[hbook.Outflow3D(sx, sy, sz)] = h.dist3D(
					h3cell(sx, nx), h3cell(sy, ny), h3cell(sz, nz),
				)
			}
		}
	}

	hh.Binning.Dist = hbook.Dist3D{
		X: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     int64(h.Entries()),
				SumW:  float64(h.SumW()),
				SumW2: float64(h.SumW2()),
			},
		},
		Y: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     int64(h.Entries()),
				SumW:  float64(h.SumW()),
				SumW2: float64(h.SumW2()),
			},
		},
		Z: hbook.Dist1D{
			Dist: hbook.Dist0D{
				N:     int64(h.Entries()),
				SumW:  float64(h.SumW()),
				SumW2: float64(h.SumW2()),
			},
		},
	}
	hh.Binning.Dist.X.Stats.SumWX = float64(h.SumWX())
	hh.Binning.Dist.X.Stats.SumWX2 = float64(h.SumWX2())
	hh.Binning.Dist.Y.Stats.SumWX = float64(h.SumWY())
	hh.Binning.Dist.Y.Stats.SumWX2 = float64(h.SumWY2())
	hh.Binning.Dist.Z.Stats.SumWX = float64(h.SumWZ())
	hh.Binning.Dist.Z.Stats.SumWX2 = float64(h.SumWZ2())
	hh.Binning.Dist.Stats.SumWXY = h.SumWXY()
	hh.Binning.Dist.Stats.SumWXZ = h.SumWXZ()
	hh.Binning.Dist.Stats.SumWYZ = h.SumWYZ()

	for ix := range nx {
		for iy := range ny {
			for iz := range nz {
				var (
					i    = (iz*ny+iy)*nx + ix
					xmin = h.XBinLowEdge(ix + 1)
					xmax = h.XBinWidth(ix+1) + xmin
					ymin = h.YBinLowEdge(iy + 1)
					ymax = h.YBinWidth(iy+1) + ymin
					zmin = h.ZBinLowEdge(iz + 1)
					zmax = h.ZBinWidth(iz+1) + zmin
					bin  = &hh.Binning.Bins[i]
				)
				bin.XRange.Min = xmin
				bin.XRange.Max = xmax
				bin.YRange.Min = ymin
				bin.YRange.Max = ymax
				bin.ZRange.Min = zmin
				bin.ZRange.Max = zmax
				bin.Dist = h.dist3D(ix+1, iy+1, iz+1)
			}
		}
	}

	return hh
}

func (h *H3D) ROOTMerge(src root.Object) error {
	hsrc, ok := src.(*H3D)
	if !ok {
		return fmt.Errorf("rhist: object %q is not a *rhist.H3D (%T)", src.(root.Named).Name(), src)
	}

	var (
		h1   = h.AsH3D()
		h2   = hsrc.AsH3D()
		hadd = hbook.AddH3D(h1, h2)
	)

	*h = *NewH3DFrom(hadd)
	return nil
}

func (h *H3D) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(h.Class(), h.RVersion())
	w.WriteObject(&h.th3)
	w.WriteObject(&h.arr)

	return w.SetHeader(hdr)
}

func (h *H3D) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(h.Class(), h.RVersion())
	if hdr.Vers < 1 {
		return fmt.Errorf("rhist: TH3D version too old (%d<1)", hdr.Vers)
	}

	r.ReadObject(&h.th3)
	r.ReadObject(&h.arr)

	r.CheckHeader(hdr)
	return r.Err()
}

func (h *H3D) RMembers() (mbrs []rbytes.Member) {
	mbrs = append(mbrs, h.th3.RMembers()...)
	mbrs = append(mbrs, rbytes.Member{
		Name: "fArray", Value: &h.arr.Data,
	})
	return mbrs
}

func init() {
	f := func() reflect.Value {
		o := newH3D()
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TH3D", f)
}

var (
	_ root.Object        = (*H3D)(nil)
	_ root.Merger        = (*H3D)(nil)
	_ root.Named         = (*H3D)(nil)
	_ H3                 = (*H3D)(nil)
	_ rbytes.Marshaler   = (*H3D)(nil)
	_ rbytes.Unmarshaler = (*H3D)(nil)
	_ rbytes.RSlicer     = (*H3D)(nil)
)
