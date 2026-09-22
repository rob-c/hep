// Copyright ©2018 The go-hep Authors. All rights reserved.
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

// H2C implements ROOT TH2C
type H2C struct {
	th2
	arr rcont.ArrayC
}

func newH2C() *H2C {
	return &H2C{
		th2: *newH2(),
	}
}

// NewH2CFrom creates a new H2C from hbook 2-dim histogram.
func NewH2CFrom(h *hbook.H2D) *H2C {
	var (
		hroot  = newH2C()
		bins   = h.Binning.Bins
		nxbins = h.Binning.Nx
		nybins = h.Binning.Ny
		xedges = make([]float64, 0, nxbins+1)
		yedges = make([]float64, 0, nybins+1)
	)

	hroot.th2.th1.entries = float64(h.Entries())
	hroot.th2.th1.tsumw = h.SumW()
	hroot.th2.th1.tsumw2 = h.SumW2()
	hroot.th2.th1.tsumwx = h.SumWX()
	hroot.th2.th1.tsumwx2 = h.SumWX2()
	hroot.th2.tsumwy = h.SumWY()
	hroot.th2.tsumwy2 = h.SumWY2()
	hroot.th2.tsumwxy = h.SumWXY()

	ncells := (nxbins + 2) * (nybins + 2)
	hroot.th2.th1.ncells = ncells

	hroot.th2.th1.xaxis.nbins = nxbins
	hroot.th2.th1.xaxis.xmin = h.XMin()
	hroot.th2.th1.xaxis.xmax = h.XMax()

	hroot.th2.th1.yaxis.nbins = nybins
	hroot.th2.th1.yaxis.xmin = h.YMin()
	hroot.th2.th1.yaxis.xmax = h.YMax()

	hroot.arr.Data = make([]int8, ncells)
	hroot.th2.th1.sumw2.Data = make([]float64, ncells)

	ibin := func(ix, iy int) int { return iy*nxbins + ix }

	for ix := range h.Binning.Nx {
		for iy := range h.Binning.Ny {
			i := ibin(ix, iy)
			bin := bins[i]
			if ix == 0 {
				yedges = append(yedges, bin.YMin())
			}
			if iy == 0 {
				xedges = append(xedges, bin.XMin())
			}
			hroot.setDist2D(ix+1, iy+1, bin.Dist.SumW(), bin.Dist.SumW2())
		}
	}

	oflows := h.Binning.Outflows[:]
	for i, v := range []struct{ ix, iy int }{
		{0, 0},
		{0, 1},
		{0, nybins + 1},
		{nxbins + 1, 0},
		{nxbins + 1, 1},
		{nxbins + 1, nybins + 1},
		{1, 0},
		{1, nybins + 1},
	} {
		hroot.setDist2D(v.ix, v.iy, oflows[i].SumW(), oflows[i].SumW2())
	}

	xedges = append(xedges, bins[ibin(h.Binning.Nx-1, 0)].XMax())
	yedges = append(yedges, bins[ibin(0, h.Binning.Ny-1)].YMax())

	hroot.th2.th1.SetName(h.Name())
	if v, ok := h.Annotation()["title"]; ok && v != nil {
		hroot.th2.th1.SetTitle(v.(string))
	}
	hroot.th2.th1.xaxis.xbins.Data = xedges
	hroot.th2.th1.yaxis.xbins.Data = yedges

	return hroot
}

// NewH2C creates a 2-dim histogram, as "new TH2C(name, title,
// nx, xmin, xmax, ny, ymin, ymax)" does in C++.
func NewH2C(name, title string, nx int, xmin, xmax float64, ny int, ymin, ymax float64) *H2C {
	h := newH2C()
	h.th2.th1.SetName(name)
	h.th2.th1.SetTitle(title)
	h.th2.th1.xaxis.setRange(nx, xmin, xmax)
	h.th2.th1.yaxis.setRange(ny, ymin, ymax)
	h.reset()
	return h
}

// reset sizes the cells to the axes and empties them.
func (h *H2C) reset() {
	n := (h.th1.xaxis.nbins + 2) * (h.th1.yaxis.nbins + 2)
	h.th2.th1.ncells = n
	h.arr.Data = make([]int8, n)
	h.th2.th1.sumw2.Data = make([]float64, n)
	h.th2.th1.entries = 0
	h.th2.th1.tsumw = 0
	h.th2.th1.tsumw2 = 0
	h.th2.th1.tsumwx = 0
	h.th2.th1.tsumwx2 = 0
	h.th2.tsumwy = 0
	h.th2.tsumwy2 = 0
	h.th2.tsumwxy = 0
}

// Reset empties the histogram, keeping its binning.
func (h *H2C) Reset() { h.reset() }

// FindBin returns the cell (x,y) falls in, as an index into the flat array
// the histogram keeps.
func (h *H2C) FindBin(x, y float64) int {
	return h.bin(h.th1.xaxis.FindBin(x), h.th1.yaxis.FindBin(y))
}

// Fill adds an entry of weight w at (x,y).
func (h *H2C) Fill(x, y, w float64) {
	var (
		ix = h.th1.xaxis.FindBin(x)
		iy = h.th1.yaxis.FindBin(y)
		i  = h.bin(ix, iy)
	)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}

	h.arr.Data[i] += int8(w)
	if len(h.th1.sumw2.Data) > i {
		h.th1.sumw2.Data[i] += w * w
	}

	h.th2.th1.entries++

	if ix > 0 && ix <= h.th1.xaxis.nbins && iy > 0 && iy <= h.th1.yaxis.nbins {
		h.th2.th1.tsumw += w
		h.th2.th1.tsumw2 += w * w
		h.th2.th1.tsumwx += w * x
		h.th2.th1.tsumwx2 += w * x * x
		h.th2.tsumwy += w * y
		h.th2.tsumwy2 += w * y * y
		h.th2.tsumwxy += w * x * y
	}
}

// FillN adds an entry for each (x,y) with the matching weight, or of weight
// one when ws is nil.
func (h *H2C) FillN(xs, ys, ws []float64) {
	if len(ys) != len(xs) || (ws != nil && len(ws) != len(xs)) {
		panic(fmt.Errorf("rhist: lengths mismatch"))
	}
	for i := range xs {
		w := 1.0
		if ws != nil {
			w = ws[i]
		}
		h.Fill(xs[i], ys[i], w)
	}
}

// BinContent returns the content of the cell at (ix,iy).
func (h *H2C) BinContent(ix, iy int) float64 {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	return float64(h.arr.Data[i])
}

// SetBinContent sets the content of the cell at (ix,iy).
func (h *H2C) SetBinContent(ix, iy int, v float64) {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}
	h.arr.Data[i] = int8(v)
}

// BinError returns the uncertainty on the cell at (ix,iy).
func (h *H2C) BinError(ix, iy int) float64 {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	if len(h.th1.sumw2.Data) > i {
		return math.Sqrt(h.th1.sumw2.Data[i])
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// SetBinError sets the uncertainty on the cell at (ix,iy).
func (h *H2C) SetBinError(ix, iy int, e float64) {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.th1.sumw2.Data) {
		return
	}
	h.th1.sumw2.Data[i] = e * e
}

// Scale multiplies every cell by f, and the uncertainties with them.
func (h *H2C) Scale(f float64) {
	for i := range h.arr.Data {
		h.arr.Data[i] = int8(float64(h.arr.Data[i]) * f)
	}
	for i := range h.th1.sumw2.Data {
		h.th1.sumw2.Data[i] *= f * f
	}
	h.th2.th1.tsumw *= f
	h.th2.th1.tsumw2 *= f * f
	h.th2.th1.tsumwx *= f
	h.th2.th1.tsumwx2 *= f
	h.th2.tsumwy *= f
	h.th2.tsumwy2 *= f
	h.th2.tsumwxy *= f
}

// Integral returns the sum of the bins proper, leaving out the under- and
// overflow.
func (h *H2C) Integral() float64 {
	var sum float64
	for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
		for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
			sum += h.BinContent(ix, iy)
		}
	}
	return sum
}

// ProjectionX sums the histogram over y and returns the 1-dim histogram that
// leaves, as TH2::ProjectionX does.
func (h *H2C) ProjectionX(name string) *H1D {
	o := NewH1D(name, h.Title(), h.th1.xaxis.nbins, h.th1.xaxis.xmin, h.th1.xaxis.xmax)
	if edges := h.th1.xaxis.xbins.Data; len(edges) == h.th1.xaxis.nbins+1 {
		o = NewH1DFromEdges(name, h.Title(), edges)
	}

	for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
		var sum, err2 float64
		for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
			sum += h.BinContent(ix, iy)
			e := h.BinError(ix, iy)
			err2 += e * e
		}
		o.SetBinContent(ix, sum)
		o.SetBinError(ix, math.Sqrt(err2))
	}

	o.th1.entries = h.th1.entries
	o.th1.tsumw = h.th2.th1.tsumw
	o.th1.tsumw2 = h.th2.th1.tsumw2
	o.th1.tsumwx = h.th2.th1.tsumwx
	o.th1.tsumwx2 = h.th2.th1.tsumwx2
	return o
}

// ProjectionY sums the histogram over x, as TH2::ProjectionY does.
func (h *H2C) ProjectionY(name string) *H1D {
	o := NewH1D(name, h.Title(), h.th1.yaxis.nbins, h.th1.yaxis.xmin, h.th1.yaxis.xmax)

	for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
		var sum, err2 float64
		for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
			sum += h.BinContent(ix, iy)
			e := h.BinError(ix, iy)
			err2 += e * e
		}
		o.SetBinContent(iy, sum)
		o.SetBinError(iy, math.Sqrt(err2))
	}

	o.th1.entries = h.th1.entries
	o.th1.tsumw = h.th2.th1.tsumw
	o.th1.tsumw2 = h.th2.th1.tsumw2
	o.th1.tsumwx = h.th2.tsumwy
	o.th1.tsumwx2 = h.th2.tsumwy2
	return o
}

// MeanX and MeanY return the means of the entries along each axis.
func (h *H2C) MeanX() float64 {
	if h.th2.th1.tsumw == 0 {
		return 0
	}
	return h.th2.th1.tsumwx / h.th2.th1.tsumw
}

// MeanY returns the mean along y.
func (h *H2C) MeanY() float64 {
	if h.th2.th1.tsumw == 0 {
		return 0
	}
	return h.th2.tsumwy / h.th2.th1.tsumw
}

func (*H2C) RVersion() int16 {
	return rvers.H2C
}

func (*H2C) isH2() {}

// Class returns the ROOT class name.
func (*H2C) Class() string {
	return "TH2C"
}

func (h *H2C) Array() rcont.ArrayC {
	return h.arr
}

// Rank returns the number of dimensions of this histogram.
func (h *H2C) Rank() int {
	return 2
}

// NbinsX returns the number of bins in X.
func (h *H2C) NbinsX() int {
	return h.th1.xaxis.nbins
}

// XAxis returns the axis along X.
func (h *H2C) XAxis() Axis {
	return &h.th1.xaxis
}

// XBinCenter returns the bin center value in X.
func (h *H2C) XBinCenter(i int) float64 {
	return float64(h.th1.xaxis.BinCenter(i))
}

// XBinContent returns the bin content value in X.
func (h *H2C) XBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// XBinError returns the bin error in X.
func (h *H2C) XBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// XBinLowEdge returns the bin lower edge value in X.
func (h *H2C) XBinLowEdge(i int) float64 {
	return h.th1.xaxis.BinLowEdge(i)
}

// XBinWidth returns the bin width in X.
func (h *H2C) XBinWidth(i int) float64 {
	return h.th1.xaxis.BinWidth(i)
}

// NbinsY returns the number of bins in Y.
func (h *H2C) NbinsY() int {
	return h.th1.yaxis.nbins
}

// YAxis returns the axis along Y.
func (h *H2C) YAxis() Axis {
	return &h.th1.yaxis
}

// YBinCenter returns the bin center value in Y.
func (h *H2C) YBinCenter(i int) float64 {
	return float64(h.th1.yaxis.BinCenter(i))
}

// YBinContent returns the bin content value in Y.
func (h *H2C) YBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// YBinError returns the bin error in Y.
func (h *H2C) YBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// YBinLowEdge returns the bin lower edge value in Y.
func (h *H2C) YBinLowEdge(i int) float64 {
	return h.th1.yaxis.BinLowEdge(i)
}

// YBinWidth returns the bin width in Y.
func (h *H2C) YBinWidth(i int) float64 {
	return h.th1.yaxis.BinWidth(i)
}

// bin returns the regularized bin number given an (x,y) bin index pair.
func (h *H2C) bin(ix, iy int) int {
	nx := h.th1.xaxis.nbins + 1 // overflow bin
	ny := h.th1.yaxis.nbins + 1 // overflow bin
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
	return ix + (nx+1)*iy
}

func (h *H2C) dist2D(ix, iy int) hbook.Dist2D {
	i := h.bin(ix, iy)
	vx := h.XBinContent(i)
	xerr := h.XBinError(i)
	nx := h.entries(vx, xerr)
	vy := h.YBinContent(i)
	yerr := h.YBinError(i)
	ny := h.entries(vy, yerr)

	sumw := h.arr.Data[i]
	sumw2 := 0.0
	if len(h.th1.sumw2.Data) > 0 {
		sumw2 = h.th1.sumw2.Data[i]
	}
	return hbook.Dist2D{
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
	}
}

func (h *H2C) setDist2D(ix, iy int, sumw, sumw2 float64) {
	i := h.bin(ix, iy)
	h.arr.Data[i] = int8(sumw)
	h.th1.sumw2.Data[i] = sumw2
}

func (h *H2C) entries(height, err float64) int64 {
	if height <= 0 {
		return 0
	}
	v := height / err
	return int64(v*v + 0.5)
}

// AsH2D creates a new hbook.H2D from this ROOT histogram.
func (h *H2C) AsH2D() *hbook.H2D {
	var (
		nx = h.NbinsX()
		ny = h.NbinsY()
		hh = hbook.NewH2D(
			nx, h.XAxis().XMin(), h.XAxis().XMax(),
			ny, h.YAxis().XMin(), h.YAxis().XMax(),
		)
		xinrange = 1
		yinrange = 1
	)
	hh.Ann = hbook.Annotation{
		"name":  h.Name(),
		"title": h.Title(),
	}
	hh.Binning.Outflows = [8]hbook.Dist2D{
		h.dist2D(0, 0),
		h.dist2D(0, yinrange),
		h.dist2D(0, ny+1),
		h.dist2D(nx+1, 0),
		h.dist2D(nx+1, yinrange),
		h.dist2D(nx+1, ny+1),
		h.dist2D(xinrange, 0),
		h.dist2D(xinrange, ny+1),
	}

	hh.Binning.Dist = hbook.Dist2D{
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
	}
	hh.Binning.Dist.X.Stats.SumWX = float64(h.SumWX())
	hh.Binning.Dist.X.Stats.SumWX2 = float64(h.SumWX2())
	hh.Binning.Dist.Y.Stats.SumWX = float64(h.SumWY())
	hh.Binning.Dist.Y.Stats.SumWX2 = float64(h.SumWY2())
	hh.Binning.Dist.Stats.SumWXY = h.SumWXY()

	for ix := range nx {
		for iy := range ny {
			var (
				i    = iy*nx + ix
				xmin = h.XBinLowEdge(ix + 1)
				xmax = h.XBinWidth(ix+1) + xmin
				ymin = h.YBinLowEdge(iy + 1)
				ymax = h.YBinWidth(iy+1) + ymin
				bin  = &hh.Binning.Bins[i]
			)
			bin.XRange.Min = xmin
			bin.XRange.Max = xmax
			bin.YRange.Min = ymin
			bin.YRange.Max = ymax
			bin.Dist = h.dist2D(ix+1, iy+1)
		}
	}

	return hh
}

// MarshalYODA implements the YODAMarshaler interface.
func (h *H2C) MarshalYODA() ([]byte, error) {
	return h.AsH2D().MarshalYODA()
}

// UnmarshalYODA implements the YODAUnmarshaler interface.
func (h *H2C) UnmarshalYODA(raw []byte) error {
	var hh hbook.H2D
	err := hh.UnmarshalYODA(raw)
	if err != nil {
		return err
	}

	*h = *NewH2CFrom(&hh)
	return nil
}

func (h *H2C) ROOTMerge(src root.Object) error {
	hsrc, ok := src.(*H2C)
	if !ok {
		return fmt.Errorf("rhist: object %q is not a *rhist.H2C (%T)", src.(root.Named).Name(), src)
	}

	var (
		h1   = h.AsH2D()
		h2   = hsrc.AsH2D()
		hadd = hbook.AddH2D(h1, h2)
	)

	*h = *NewH2CFrom(hadd)
	return nil
}

func (h *H2C) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(h.Class(), h.RVersion())
	w.WriteObject(&h.th2)
	w.WriteObject(&h.arr)

	return w.SetHeader(hdr)
}

func (h *H2C) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(h.Class(), h.RVersion())
	if hdr.Vers < 1 {
		return fmt.Errorf("rhist: TH2C version too old (%d<1)", hdr.Vers)
	}

	r.ReadObject(&h.th2)
	r.ReadObject(&h.arr)

	r.CheckHeader(hdr)
	return r.Err()
}

func (h *H2C) RMembers() (mbrs []rbytes.Member) {
	mbrs = append(mbrs, h.th2.RMembers()...)
	mbrs = append(mbrs, rbytes.Member{
		Name: "fArray", Value: &h.arr.Data,
	})
	return mbrs
}

func init() {
	f := func() reflect.Value {
		o := newH2C()
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TH2C", f)
}

var (
	_ root.Object        = (*H2C)(nil)
	_ root.Merger        = (*H2C)(nil)
	_ root.Named         = (*H2C)(nil)
	_ H2                 = (*H2C)(nil)
	_ rbytes.Marshaler   = (*H2C)(nil)
	_ rbytes.Unmarshaler = (*H2C)(nil)
	_ rbytes.RSlicer     = (*H2C)(nil)
)

// H2S implements ROOT TH2S
type H2S struct {
	th2
	arr rcont.ArrayS
}

func newH2S() *H2S {
	return &H2S{
		th2: *newH2(),
	}
}

// NewH2SFrom creates a new H2S from hbook 2-dim histogram.
func NewH2SFrom(h *hbook.H2D) *H2S {
	var (
		hroot  = newH2S()
		bins   = h.Binning.Bins
		nxbins = h.Binning.Nx
		nybins = h.Binning.Ny
		xedges = make([]float64, 0, nxbins+1)
		yedges = make([]float64, 0, nybins+1)
	)

	hroot.th2.th1.entries = float64(h.Entries())
	hroot.th2.th1.tsumw = h.SumW()
	hroot.th2.th1.tsumw2 = h.SumW2()
	hroot.th2.th1.tsumwx = h.SumWX()
	hroot.th2.th1.tsumwx2 = h.SumWX2()
	hroot.th2.tsumwy = h.SumWY()
	hroot.th2.tsumwy2 = h.SumWY2()
	hroot.th2.tsumwxy = h.SumWXY()

	ncells := (nxbins + 2) * (nybins + 2)
	hroot.th2.th1.ncells = ncells

	hroot.th2.th1.xaxis.nbins = nxbins
	hroot.th2.th1.xaxis.xmin = h.XMin()
	hroot.th2.th1.xaxis.xmax = h.XMax()

	hroot.th2.th1.yaxis.nbins = nybins
	hroot.th2.th1.yaxis.xmin = h.YMin()
	hroot.th2.th1.yaxis.xmax = h.YMax()

	hroot.arr.Data = make([]int16, ncells)
	hroot.th2.th1.sumw2.Data = make([]float64, ncells)

	ibin := func(ix, iy int) int { return iy*nxbins + ix }

	for ix := range h.Binning.Nx {
		for iy := range h.Binning.Ny {
			i := ibin(ix, iy)
			bin := bins[i]
			if ix == 0 {
				yedges = append(yedges, bin.YMin())
			}
			if iy == 0 {
				xedges = append(xedges, bin.XMin())
			}
			hroot.setDist2D(ix+1, iy+1, bin.Dist.SumW(), bin.Dist.SumW2())
		}
	}

	oflows := h.Binning.Outflows[:]
	for i, v := range []struct{ ix, iy int }{
		{0, 0},
		{0, 1},
		{0, nybins + 1},
		{nxbins + 1, 0},
		{nxbins + 1, 1},
		{nxbins + 1, nybins + 1},
		{1, 0},
		{1, nybins + 1},
	} {
		hroot.setDist2D(v.ix, v.iy, oflows[i].SumW(), oflows[i].SumW2())
	}

	xedges = append(xedges, bins[ibin(h.Binning.Nx-1, 0)].XMax())
	yedges = append(yedges, bins[ibin(0, h.Binning.Ny-1)].YMax())

	hroot.th2.th1.SetName(h.Name())
	if v, ok := h.Annotation()["title"]; ok && v != nil {
		hroot.th2.th1.SetTitle(v.(string))
	}
	hroot.th2.th1.xaxis.xbins.Data = xedges
	hroot.th2.th1.yaxis.xbins.Data = yedges

	return hroot
}

// NewH2S creates a 2-dim histogram, as "new TH2S(name, title,
// nx, xmin, xmax, ny, ymin, ymax)" does in C++.
func NewH2S(name, title string, nx int, xmin, xmax float64, ny int, ymin, ymax float64) *H2S {
	h := newH2S()
	h.th2.th1.SetName(name)
	h.th2.th1.SetTitle(title)
	h.th2.th1.xaxis.setRange(nx, xmin, xmax)
	h.th2.th1.yaxis.setRange(ny, ymin, ymax)
	h.reset()
	return h
}

// reset sizes the cells to the axes and empties them.
func (h *H2S) reset() {
	n := (h.th1.xaxis.nbins + 2) * (h.th1.yaxis.nbins + 2)
	h.th2.th1.ncells = n
	h.arr.Data = make([]int16, n)
	h.th2.th1.sumw2.Data = make([]float64, n)
	h.th2.th1.entries = 0
	h.th2.th1.tsumw = 0
	h.th2.th1.tsumw2 = 0
	h.th2.th1.tsumwx = 0
	h.th2.th1.tsumwx2 = 0
	h.th2.tsumwy = 0
	h.th2.tsumwy2 = 0
	h.th2.tsumwxy = 0
}

// Reset empties the histogram, keeping its binning.
func (h *H2S) Reset() { h.reset() }

// FindBin returns the cell (x,y) falls in, as an index into the flat array
// the histogram keeps.
func (h *H2S) FindBin(x, y float64) int {
	return h.bin(h.th1.xaxis.FindBin(x), h.th1.yaxis.FindBin(y))
}

// Fill adds an entry of weight w at (x,y).
func (h *H2S) Fill(x, y, w float64) {
	var (
		ix = h.th1.xaxis.FindBin(x)
		iy = h.th1.yaxis.FindBin(y)
		i  = h.bin(ix, iy)
	)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}

	h.arr.Data[i] += int16(w)
	if len(h.th1.sumw2.Data) > i {
		h.th1.sumw2.Data[i] += w * w
	}

	h.th2.th1.entries++

	if ix > 0 && ix <= h.th1.xaxis.nbins && iy > 0 && iy <= h.th1.yaxis.nbins {
		h.th2.th1.tsumw += w
		h.th2.th1.tsumw2 += w * w
		h.th2.th1.tsumwx += w * x
		h.th2.th1.tsumwx2 += w * x * x
		h.th2.tsumwy += w * y
		h.th2.tsumwy2 += w * y * y
		h.th2.tsumwxy += w * x * y
	}
}

// FillN adds an entry for each (x,y) with the matching weight, or of weight
// one when ws is nil.
func (h *H2S) FillN(xs, ys, ws []float64) {
	if len(ys) != len(xs) || (ws != nil && len(ws) != len(xs)) {
		panic(fmt.Errorf("rhist: lengths mismatch"))
	}
	for i := range xs {
		w := 1.0
		if ws != nil {
			w = ws[i]
		}
		h.Fill(xs[i], ys[i], w)
	}
}

// BinContent returns the content of the cell at (ix,iy).
func (h *H2S) BinContent(ix, iy int) float64 {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	return float64(h.arr.Data[i])
}

// SetBinContent sets the content of the cell at (ix,iy).
func (h *H2S) SetBinContent(ix, iy int, v float64) {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}
	h.arr.Data[i] = int16(v)
}

// BinError returns the uncertainty on the cell at (ix,iy).
func (h *H2S) BinError(ix, iy int) float64 {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	if len(h.th1.sumw2.Data) > i {
		return math.Sqrt(h.th1.sumw2.Data[i])
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// SetBinError sets the uncertainty on the cell at (ix,iy).
func (h *H2S) SetBinError(ix, iy int, e float64) {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.th1.sumw2.Data) {
		return
	}
	h.th1.sumw2.Data[i] = e * e
}

// Scale multiplies every cell by f, and the uncertainties with them.
func (h *H2S) Scale(f float64) {
	for i := range h.arr.Data {
		h.arr.Data[i] = int16(float64(h.arr.Data[i]) * f)
	}
	for i := range h.th1.sumw2.Data {
		h.th1.sumw2.Data[i] *= f * f
	}
	h.th2.th1.tsumw *= f
	h.th2.th1.tsumw2 *= f * f
	h.th2.th1.tsumwx *= f
	h.th2.th1.tsumwx2 *= f
	h.th2.tsumwy *= f
	h.th2.tsumwy2 *= f
	h.th2.tsumwxy *= f
}

// Integral returns the sum of the bins proper, leaving out the under- and
// overflow.
func (h *H2S) Integral() float64 {
	var sum float64
	for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
		for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
			sum += h.BinContent(ix, iy)
		}
	}
	return sum
}

// ProjectionX sums the histogram over y and returns the 1-dim histogram that
// leaves, as TH2::ProjectionX does.
func (h *H2S) ProjectionX(name string) *H1D {
	o := NewH1D(name, h.Title(), h.th1.xaxis.nbins, h.th1.xaxis.xmin, h.th1.xaxis.xmax)
	if edges := h.th1.xaxis.xbins.Data; len(edges) == h.th1.xaxis.nbins+1 {
		o = NewH1DFromEdges(name, h.Title(), edges)
	}

	for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
		var sum, err2 float64
		for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
			sum += h.BinContent(ix, iy)
			e := h.BinError(ix, iy)
			err2 += e * e
		}
		o.SetBinContent(ix, sum)
		o.SetBinError(ix, math.Sqrt(err2))
	}

	o.th1.entries = h.th1.entries
	o.th1.tsumw = h.th2.th1.tsumw
	o.th1.tsumw2 = h.th2.th1.tsumw2
	o.th1.tsumwx = h.th2.th1.tsumwx
	o.th1.tsumwx2 = h.th2.th1.tsumwx2
	return o
}

// ProjectionY sums the histogram over x, as TH2::ProjectionY does.
func (h *H2S) ProjectionY(name string) *H1D {
	o := NewH1D(name, h.Title(), h.th1.yaxis.nbins, h.th1.yaxis.xmin, h.th1.yaxis.xmax)

	for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
		var sum, err2 float64
		for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
			sum += h.BinContent(ix, iy)
			e := h.BinError(ix, iy)
			err2 += e * e
		}
		o.SetBinContent(iy, sum)
		o.SetBinError(iy, math.Sqrt(err2))
	}

	o.th1.entries = h.th1.entries
	o.th1.tsumw = h.th2.th1.tsumw
	o.th1.tsumw2 = h.th2.th1.tsumw2
	o.th1.tsumwx = h.th2.tsumwy
	o.th1.tsumwx2 = h.th2.tsumwy2
	return o
}

// MeanX and MeanY return the means of the entries along each axis.
func (h *H2S) MeanX() float64 {
	if h.th2.th1.tsumw == 0 {
		return 0
	}
	return h.th2.th1.tsumwx / h.th2.th1.tsumw
}

// MeanY returns the mean along y.
func (h *H2S) MeanY() float64 {
	if h.th2.th1.tsumw == 0 {
		return 0
	}
	return h.th2.tsumwy / h.th2.th1.tsumw
}

func (*H2S) RVersion() int16 {
	return rvers.H2S
}

func (*H2S) isH2() {}

// Class returns the ROOT class name.
func (*H2S) Class() string {
	return "TH2S"
}

func (h *H2S) Array() rcont.ArrayS {
	return h.arr
}

// Rank returns the number of dimensions of this histogram.
func (h *H2S) Rank() int {
	return 2
}

// NbinsX returns the number of bins in X.
func (h *H2S) NbinsX() int {
	return h.th1.xaxis.nbins
}

// XAxis returns the axis along X.
func (h *H2S) XAxis() Axis {
	return &h.th1.xaxis
}

// XBinCenter returns the bin center value in X.
func (h *H2S) XBinCenter(i int) float64 {
	return float64(h.th1.xaxis.BinCenter(i))
}

// XBinContent returns the bin content value in X.
func (h *H2S) XBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// XBinError returns the bin error in X.
func (h *H2S) XBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// XBinLowEdge returns the bin lower edge value in X.
func (h *H2S) XBinLowEdge(i int) float64 {
	return h.th1.xaxis.BinLowEdge(i)
}

// XBinWidth returns the bin width in X.
func (h *H2S) XBinWidth(i int) float64 {
	return h.th1.xaxis.BinWidth(i)
}

// NbinsY returns the number of bins in Y.
func (h *H2S) NbinsY() int {
	return h.th1.yaxis.nbins
}

// YAxis returns the axis along Y.
func (h *H2S) YAxis() Axis {
	return &h.th1.yaxis
}

// YBinCenter returns the bin center value in Y.
func (h *H2S) YBinCenter(i int) float64 {
	return float64(h.th1.yaxis.BinCenter(i))
}

// YBinContent returns the bin content value in Y.
func (h *H2S) YBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// YBinError returns the bin error in Y.
func (h *H2S) YBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// YBinLowEdge returns the bin lower edge value in Y.
func (h *H2S) YBinLowEdge(i int) float64 {
	return h.th1.yaxis.BinLowEdge(i)
}

// YBinWidth returns the bin width in Y.
func (h *H2S) YBinWidth(i int) float64 {
	return h.th1.yaxis.BinWidth(i)
}

// bin returns the regularized bin number given an (x,y) bin index pair.
func (h *H2S) bin(ix, iy int) int {
	nx := h.th1.xaxis.nbins + 1 // overflow bin
	ny := h.th1.yaxis.nbins + 1 // overflow bin
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
	return ix + (nx+1)*iy
}

func (h *H2S) dist2D(ix, iy int) hbook.Dist2D {
	i := h.bin(ix, iy)
	vx := h.XBinContent(i)
	xerr := h.XBinError(i)
	nx := h.entries(vx, xerr)
	vy := h.YBinContent(i)
	yerr := h.YBinError(i)
	ny := h.entries(vy, yerr)

	sumw := h.arr.Data[i]
	sumw2 := 0.0
	if len(h.th1.sumw2.Data) > 0 {
		sumw2 = h.th1.sumw2.Data[i]
	}
	return hbook.Dist2D{
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
	}
}

func (h *H2S) setDist2D(ix, iy int, sumw, sumw2 float64) {
	i := h.bin(ix, iy)
	h.arr.Data[i] = int16(sumw)
	h.th1.sumw2.Data[i] = sumw2
}

func (h *H2S) entries(height, err float64) int64 {
	if height <= 0 {
		return 0
	}
	v := height / err
	return int64(v*v + 0.5)
}

// AsH2D creates a new hbook.H2D from this ROOT histogram.
func (h *H2S) AsH2D() *hbook.H2D {
	var (
		nx = h.NbinsX()
		ny = h.NbinsY()
		hh = hbook.NewH2D(
			nx, h.XAxis().XMin(), h.XAxis().XMax(),
			ny, h.YAxis().XMin(), h.YAxis().XMax(),
		)
		xinrange = 1
		yinrange = 1
	)
	hh.Ann = hbook.Annotation{
		"name":  h.Name(),
		"title": h.Title(),
	}
	hh.Binning.Outflows = [8]hbook.Dist2D{
		h.dist2D(0, 0),
		h.dist2D(0, yinrange),
		h.dist2D(0, ny+1),
		h.dist2D(nx+1, 0),
		h.dist2D(nx+1, yinrange),
		h.dist2D(nx+1, ny+1),
		h.dist2D(xinrange, 0),
		h.dist2D(xinrange, ny+1),
	}

	hh.Binning.Dist = hbook.Dist2D{
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
	}
	hh.Binning.Dist.X.Stats.SumWX = float64(h.SumWX())
	hh.Binning.Dist.X.Stats.SumWX2 = float64(h.SumWX2())
	hh.Binning.Dist.Y.Stats.SumWX = float64(h.SumWY())
	hh.Binning.Dist.Y.Stats.SumWX2 = float64(h.SumWY2())
	hh.Binning.Dist.Stats.SumWXY = h.SumWXY()

	for ix := range nx {
		for iy := range ny {
			var (
				i    = iy*nx + ix
				xmin = h.XBinLowEdge(ix + 1)
				xmax = h.XBinWidth(ix+1) + xmin
				ymin = h.YBinLowEdge(iy + 1)
				ymax = h.YBinWidth(iy+1) + ymin
				bin  = &hh.Binning.Bins[i]
			)
			bin.XRange.Min = xmin
			bin.XRange.Max = xmax
			bin.YRange.Min = ymin
			bin.YRange.Max = ymax
			bin.Dist = h.dist2D(ix+1, iy+1)
		}
	}

	return hh
}

// MarshalYODA implements the YODAMarshaler interface.
func (h *H2S) MarshalYODA() ([]byte, error) {
	return h.AsH2D().MarshalYODA()
}

// UnmarshalYODA implements the YODAUnmarshaler interface.
func (h *H2S) UnmarshalYODA(raw []byte) error {
	var hh hbook.H2D
	err := hh.UnmarshalYODA(raw)
	if err != nil {
		return err
	}

	*h = *NewH2SFrom(&hh)
	return nil
}

func (h *H2S) ROOTMerge(src root.Object) error {
	hsrc, ok := src.(*H2S)
	if !ok {
		return fmt.Errorf("rhist: object %q is not a *rhist.H2S (%T)", src.(root.Named).Name(), src)
	}

	var (
		h1   = h.AsH2D()
		h2   = hsrc.AsH2D()
		hadd = hbook.AddH2D(h1, h2)
	)

	*h = *NewH2SFrom(hadd)
	return nil
}

func (h *H2S) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(h.Class(), h.RVersion())
	w.WriteObject(&h.th2)
	w.WriteObject(&h.arr)

	return w.SetHeader(hdr)
}

func (h *H2S) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(h.Class(), h.RVersion())
	if hdr.Vers < 1 {
		return fmt.Errorf("rhist: TH2S version too old (%d<1)", hdr.Vers)
	}

	r.ReadObject(&h.th2)
	r.ReadObject(&h.arr)

	r.CheckHeader(hdr)
	return r.Err()
}

func (h *H2S) RMembers() (mbrs []rbytes.Member) {
	mbrs = append(mbrs, h.th2.RMembers()...)
	mbrs = append(mbrs, rbytes.Member{
		Name: "fArray", Value: &h.arr.Data,
	})
	return mbrs
}

func init() {
	f := func() reflect.Value {
		o := newH2S()
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TH2S", f)
}

var (
	_ root.Object        = (*H2S)(nil)
	_ root.Merger        = (*H2S)(nil)
	_ root.Named         = (*H2S)(nil)
	_ H2                 = (*H2S)(nil)
	_ rbytes.Marshaler   = (*H2S)(nil)
	_ rbytes.Unmarshaler = (*H2S)(nil)
	_ rbytes.RSlicer     = (*H2S)(nil)
)

// H2F implements ROOT TH2F
type H2F struct {
	th2
	arr rcont.ArrayF
}

func newH2F() *H2F {
	return &H2F{
		th2: *newH2(),
	}
}

// NewH2FFrom creates a new H2F from hbook 2-dim histogram.
func NewH2FFrom(h *hbook.H2D) *H2F {
	var (
		hroot  = newH2F()
		bins   = h.Binning.Bins
		nxbins = h.Binning.Nx
		nybins = h.Binning.Ny
		xedges = make([]float64, 0, nxbins+1)
		yedges = make([]float64, 0, nybins+1)
	)

	hroot.th2.th1.entries = float64(h.Entries())
	hroot.th2.th1.tsumw = h.SumW()
	hroot.th2.th1.tsumw2 = h.SumW2()
	hroot.th2.th1.tsumwx = h.SumWX()
	hroot.th2.th1.tsumwx2 = h.SumWX2()
	hroot.th2.tsumwy = h.SumWY()
	hroot.th2.tsumwy2 = h.SumWY2()
	hroot.th2.tsumwxy = h.SumWXY()

	ncells := (nxbins + 2) * (nybins + 2)
	hroot.th2.th1.ncells = ncells

	hroot.th2.th1.xaxis.nbins = nxbins
	hroot.th2.th1.xaxis.xmin = h.XMin()
	hroot.th2.th1.xaxis.xmax = h.XMax()

	hroot.th2.th1.yaxis.nbins = nybins
	hroot.th2.th1.yaxis.xmin = h.YMin()
	hroot.th2.th1.yaxis.xmax = h.YMax()

	hroot.arr.Data = make([]float32, ncells)
	hroot.th2.th1.sumw2.Data = make([]float64, ncells)

	ibin := func(ix, iy int) int { return iy*nxbins + ix }

	for ix := range h.Binning.Nx {
		for iy := range h.Binning.Ny {
			i := ibin(ix, iy)
			bin := bins[i]
			if ix == 0 {
				yedges = append(yedges, bin.YMin())
			}
			if iy == 0 {
				xedges = append(xedges, bin.XMin())
			}
			hroot.setDist2D(ix+1, iy+1, bin.Dist.SumW(), bin.Dist.SumW2())
		}
	}

	oflows := h.Binning.Outflows[:]
	for i, v := range []struct{ ix, iy int }{
		{0, 0},
		{0, 1},
		{0, nybins + 1},
		{nxbins + 1, 0},
		{nxbins + 1, 1},
		{nxbins + 1, nybins + 1},
		{1, 0},
		{1, nybins + 1},
	} {
		hroot.setDist2D(v.ix, v.iy, oflows[i].SumW(), oflows[i].SumW2())
	}

	xedges = append(xedges, bins[ibin(h.Binning.Nx-1, 0)].XMax())
	yedges = append(yedges, bins[ibin(0, h.Binning.Ny-1)].YMax())

	hroot.th2.th1.SetName(h.Name())
	if v, ok := h.Annotation()["title"]; ok && v != nil {
		hroot.th2.th1.SetTitle(v.(string))
	}
	hroot.th2.th1.xaxis.xbins.Data = xedges
	hroot.th2.th1.yaxis.xbins.Data = yedges

	return hroot
}

// NewH2F creates a 2-dim histogram, as "new TH2F(name, title,
// nx, xmin, xmax, ny, ymin, ymax)" does in C++.
func NewH2F(name, title string, nx int, xmin, xmax float64, ny int, ymin, ymax float64) *H2F {
	h := newH2F()
	h.th2.th1.SetName(name)
	h.th2.th1.SetTitle(title)
	h.th2.th1.xaxis.setRange(nx, xmin, xmax)
	h.th2.th1.yaxis.setRange(ny, ymin, ymax)
	h.reset()
	return h
}

// reset sizes the cells to the axes and empties them.
func (h *H2F) reset() {
	n := (h.th1.xaxis.nbins + 2) * (h.th1.yaxis.nbins + 2)
	h.th2.th1.ncells = n
	h.arr.Data = make([]float32, n)
	h.th2.th1.sumw2.Data = make([]float64, n)
	h.th2.th1.entries = 0
	h.th2.th1.tsumw = 0
	h.th2.th1.tsumw2 = 0
	h.th2.th1.tsumwx = 0
	h.th2.th1.tsumwx2 = 0
	h.th2.tsumwy = 0
	h.th2.tsumwy2 = 0
	h.th2.tsumwxy = 0
}

// Reset empties the histogram, keeping its binning.
func (h *H2F) Reset() { h.reset() }

// FindBin returns the cell (x,y) falls in, as an index into the flat array
// the histogram keeps.
func (h *H2F) FindBin(x, y float64) int {
	return h.bin(h.th1.xaxis.FindBin(x), h.th1.yaxis.FindBin(y))
}

// Fill adds an entry of weight w at (x,y).
func (h *H2F) Fill(x, y, w float64) {
	var (
		ix = h.th1.xaxis.FindBin(x)
		iy = h.th1.yaxis.FindBin(y)
		i  = h.bin(ix, iy)
	)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}

	h.arr.Data[i] += float32(w)
	if len(h.th1.sumw2.Data) > i {
		h.th1.sumw2.Data[i] += w * w
	}

	h.th2.th1.entries++

	if ix > 0 && ix <= h.th1.xaxis.nbins && iy > 0 && iy <= h.th1.yaxis.nbins {
		h.th2.th1.tsumw += w
		h.th2.th1.tsumw2 += w * w
		h.th2.th1.tsumwx += w * x
		h.th2.th1.tsumwx2 += w * x * x
		h.th2.tsumwy += w * y
		h.th2.tsumwy2 += w * y * y
		h.th2.tsumwxy += w * x * y
	}
}

// FillN adds an entry for each (x,y) with the matching weight, or of weight
// one when ws is nil.
func (h *H2F) FillN(xs, ys, ws []float64) {
	if len(ys) != len(xs) || (ws != nil && len(ws) != len(xs)) {
		panic(fmt.Errorf("rhist: lengths mismatch"))
	}
	for i := range xs {
		w := 1.0
		if ws != nil {
			w = ws[i]
		}
		h.Fill(xs[i], ys[i], w)
	}
}

// BinContent returns the content of the cell at (ix,iy).
func (h *H2F) BinContent(ix, iy int) float64 {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	return float64(h.arr.Data[i])
}

// SetBinContent sets the content of the cell at (ix,iy).
func (h *H2F) SetBinContent(ix, iy int, v float64) {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}
	h.arr.Data[i] = float32(v)
}

// BinError returns the uncertainty on the cell at (ix,iy).
func (h *H2F) BinError(ix, iy int) float64 {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	if len(h.th1.sumw2.Data) > i {
		return math.Sqrt(h.th1.sumw2.Data[i])
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// SetBinError sets the uncertainty on the cell at (ix,iy).
func (h *H2F) SetBinError(ix, iy int, e float64) {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.th1.sumw2.Data) {
		return
	}
	h.th1.sumw2.Data[i] = e * e
}

// Scale multiplies every cell by f, and the uncertainties with them.
func (h *H2F) Scale(f float64) {
	for i := range h.arr.Data {
		h.arr.Data[i] = float32(float64(h.arr.Data[i]) * f)
	}
	for i := range h.th1.sumw2.Data {
		h.th1.sumw2.Data[i] *= f * f
	}
	h.th2.th1.tsumw *= f
	h.th2.th1.tsumw2 *= f * f
	h.th2.th1.tsumwx *= f
	h.th2.th1.tsumwx2 *= f
	h.th2.tsumwy *= f
	h.th2.tsumwy2 *= f
	h.th2.tsumwxy *= f
}

// Integral returns the sum of the bins proper, leaving out the under- and
// overflow.
func (h *H2F) Integral() float64 {
	var sum float64
	for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
		for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
			sum += h.BinContent(ix, iy)
		}
	}
	return sum
}

// ProjectionX sums the histogram over y and returns the 1-dim histogram that
// leaves, as TH2::ProjectionX does.
func (h *H2F) ProjectionX(name string) *H1D {
	o := NewH1D(name, h.Title(), h.th1.xaxis.nbins, h.th1.xaxis.xmin, h.th1.xaxis.xmax)
	if edges := h.th1.xaxis.xbins.Data; len(edges) == h.th1.xaxis.nbins+1 {
		o = NewH1DFromEdges(name, h.Title(), edges)
	}

	for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
		var sum, err2 float64
		for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
			sum += h.BinContent(ix, iy)
			e := h.BinError(ix, iy)
			err2 += e * e
		}
		o.SetBinContent(ix, sum)
		o.SetBinError(ix, math.Sqrt(err2))
	}

	o.th1.entries = h.th1.entries
	o.th1.tsumw = h.th2.th1.tsumw
	o.th1.tsumw2 = h.th2.th1.tsumw2
	o.th1.tsumwx = h.th2.th1.tsumwx
	o.th1.tsumwx2 = h.th2.th1.tsumwx2
	return o
}

// ProjectionY sums the histogram over x, as TH2::ProjectionY does.
func (h *H2F) ProjectionY(name string) *H1D {
	o := NewH1D(name, h.Title(), h.th1.yaxis.nbins, h.th1.yaxis.xmin, h.th1.yaxis.xmax)

	for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
		var sum, err2 float64
		for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
			sum += h.BinContent(ix, iy)
			e := h.BinError(ix, iy)
			err2 += e * e
		}
		o.SetBinContent(iy, sum)
		o.SetBinError(iy, math.Sqrt(err2))
	}

	o.th1.entries = h.th1.entries
	o.th1.tsumw = h.th2.th1.tsumw
	o.th1.tsumw2 = h.th2.th1.tsumw2
	o.th1.tsumwx = h.th2.tsumwy
	o.th1.tsumwx2 = h.th2.tsumwy2
	return o
}

// MeanX and MeanY return the means of the entries along each axis.
func (h *H2F) MeanX() float64 {
	if h.th2.th1.tsumw == 0 {
		return 0
	}
	return h.th2.th1.tsumwx / h.th2.th1.tsumw
}

// MeanY returns the mean along y.
func (h *H2F) MeanY() float64 {
	if h.th2.th1.tsumw == 0 {
		return 0
	}
	return h.th2.tsumwy / h.th2.th1.tsumw
}

func (*H2F) RVersion() int16 {
	return rvers.H2F
}

func (*H2F) isH2() {}

// Class returns the ROOT class name.
func (*H2F) Class() string {
	return "TH2F"
}

func (h *H2F) Array() rcont.ArrayF {
	return h.arr
}

// Rank returns the number of dimensions of this histogram.
func (h *H2F) Rank() int {
	return 2
}

// NbinsX returns the number of bins in X.
func (h *H2F) NbinsX() int {
	return h.th1.xaxis.nbins
}

// XAxis returns the axis along X.
func (h *H2F) XAxis() Axis {
	return &h.th1.xaxis
}

// XBinCenter returns the bin center value in X.
func (h *H2F) XBinCenter(i int) float64 {
	return float64(h.th1.xaxis.BinCenter(i))
}

// XBinContent returns the bin content value in X.
func (h *H2F) XBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// XBinError returns the bin error in X.
func (h *H2F) XBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// XBinLowEdge returns the bin lower edge value in X.
func (h *H2F) XBinLowEdge(i int) float64 {
	return h.th1.xaxis.BinLowEdge(i)
}

// XBinWidth returns the bin width in X.
func (h *H2F) XBinWidth(i int) float64 {
	return h.th1.xaxis.BinWidth(i)
}

// NbinsY returns the number of bins in Y.
func (h *H2F) NbinsY() int {
	return h.th1.yaxis.nbins
}

// YAxis returns the axis along Y.
func (h *H2F) YAxis() Axis {
	return &h.th1.yaxis
}

// YBinCenter returns the bin center value in Y.
func (h *H2F) YBinCenter(i int) float64 {
	return float64(h.th1.yaxis.BinCenter(i))
}

// YBinContent returns the bin content value in Y.
func (h *H2F) YBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// YBinError returns the bin error in Y.
func (h *H2F) YBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// YBinLowEdge returns the bin lower edge value in Y.
func (h *H2F) YBinLowEdge(i int) float64 {
	return h.th1.yaxis.BinLowEdge(i)
}

// YBinWidth returns the bin width in Y.
func (h *H2F) YBinWidth(i int) float64 {
	return h.th1.yaxis.BinWidth(i)
}

// bin returns the regularized bin number given an (x,y) bin index pair.
func (h *H2F) bin(ix, iy int) int {
	nx := h.th1.xaxis.nbins + 1 // overflow bin
	ny := h.th1.yaxis.nbins + 1 // overflow bin
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
	return ix + (nx+1)*iy
}

func (h *H2F) dist2D(ix, iy int) hbook.Dist2D {
	i := h.bin(ix, iy)
	vx := h.XBinContent(i)
	xerr := h.XBinError(i)
	nx := h.entries(vx, xerr)
	vy := h.YBinContent(i)
	yerr := h.YBinError(i)
	ny := h.entries(vy, yerr)

	sumw := h.arr.Data[i]
	sumw2 := 0.0
	if len(h.th1.sumw2.Data) > 0 {
		sumw2 = h.th1.sumw2.Data[i]
	}
	return hbook.Dist2D{
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
	}
}

func (h *H2F) setDist2D(ix, iy int, sumw, sumw2 float64) {
	i := h.bin(ix, iy)
	h.arr.Data[i] = float32(sumw)
	h.th1.sumw2.Data[i] = sumw2
}

func (h *H2F) entries(height, err float64) int64 {
	if height <= 0 {
		return 0
	}
	v := height / err
	return int64(v*v + 0.5)
}

// AsH2D creates a new hbook.H2D from this ROOT histogram.
func (h *H2F) AsH2D() *hbook.H2D {
	var (
		nx = h.NbinsX()
		ny = h.NbinsY()
		hh = hbook.NewH2D(
			nx, h.XAxis().XMin(), h.XAxis().XMax(),
			ny, h.YAxis().XMin(), h.YAxis().XMax(),
		)
		xinrange = 1
		yinrange = 1
	)
	hh.Ann = hbook.Annotation{
		"name":  h.Name(),
		"title": h.Title(),
	}
	hh.Binning.Outflows = [8]hbook.Dist2D{
		h.dist2D(0, 0),
		h.dist2D(0, yinrange),
		h.dist2D(0, ny+1),
		h.dist2D(nx+1, 0),
		h.dist2D(nx+1, yinrange),
		h.dist2D(nx+1, ny+1),
		h.dist2D(xinrange, 0),
		h.dist2D(xinrange, ny+1),
	}

	hh.Binning.Dist = hbook.Dist2D{
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
	}
	hh.Binning.Dist.X.Stats.SumWX = float64(h.SumWX())
	hh.Binning.Dist.X.Stats.SumWX2 = float64(h.SumWX2())
	hh.Binning.Dist.Y.Stats.SumWX = float64(h.SumWY())
	hh.Binning.Dist.Y.Stats.SumWX2 = float64(h.SumWY2())
	hh.Binning.Dist.Stats.SumWXY = h.SumWXY()

	for ix := range nx {
		for iy := range ny {
			var (
				i    = iy*nx + ix
				xmin = h.XBinLowEdge(ix + 1)
				xmax = h.XBinWidth(ix+1) + xmin
				ymin = h.YBinLowEdge(iy + 1)
				ymax = h.YBinWidth(iy+1) + ymin
				bin  = &hh.Binning.Bins[i]
			)
			bin.XRange.Min = xmin
			bin.XRange.Max = xmax
			bin.YRange.Min = ymin
			bin.YRange.Max = ymax
			bin.Dist = h.dist2D(ix+1, iy+1)
		}
	}

	return hh
}

// MarshalYODA implements the YODAMarshaler interface.
func (h *H2F) MarshalYODA() ([]byte, error) {
	return h.AsH2D().MarshalYODA()
}

// UnmarshalYODA implements the YODAUnmarshaler interface.
func (h *H2F) UnmarshalYODA(raw []byte) error {
	var hh hbook.H2D
	err := hh.UnmarshalYODA(raw)
	if err != nil {
		return err
	}

	*h = *NewH2FFrom(&hh)
	return nil
}

func (h *H2F) ROOTMerge(src root.Object) error {
	hsrc, ok := src.(*H2F)
	if !ok {
		return fmt.Errorf("rhist: object %q is not a *rhist.H2F (%T)", src.(root.Named).Name(), src)
	}

	var (
		h1   = h.AsH2D()
		h2   = hsrc.AsH2D()
		hadd = hbook.AddH2D(h1, h2)
	)

	*h = *NewH2FFrom(hadd)
	return nil
}

func (h *H2F) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(h.Class(), h.RVersion())
	w.WriteObject(&h.th2)
	w.WriteObject(&h.arr)

	return w.SetHeader(hdr)
}

func (h *H2F) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(h.Class(), h.RVersion())
	if hdr.Vers < 1 {
		return fmt.Errorf("rhist: TH2F version too old (%d<1)", hdr.Vers)
	}

	r.ReadObject(&h.th2)
	r.ReadObject(&h.arr)

	r.CheckHeader(hdr)
	return r.Err()
}

func (h *H2F) RMembers() (mbrs []rbytes.Member) {
	mbrs = append(mbrs, h.th2.RMembers()...)
	mbrs = append(mbrs, rbytes.Member{
		Name: "fArray", Value: &h.arr.Data,
	})
	return mbrs
}

func init() {
	f := func() reflect.Value {
		o := newH2F()
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TH2F", f)
}

var (
	_ root.Object        = (*H2F)(nil)
	_ root.Merger        = (*H2F)(nil)
	_ root.Named         = (*H2F)(nil)
	_ H2                 = (*H2F)(nil)
	_ rbytes.Marshaler   = (*H2F)(nil)
	_ rbytes.Unmarshaler = (*H2F)(nil)
	_ rbytes.RSlicer     = (*H2F)(nil)
)

// H2D implements ROOT TH2D
type H2D struct {
	th2
	arr rcont.ArrayD
}

func newH2D() *H2D {
	return &H2D{
		th2: *newH2(),
	}
}

// NewH2DFrom creates a new H2D from hbook 2-dim histogram.
func NewH2DFrom(h *hbook.H2D) *H2D {
	var (
		hroot  = newH2D()
		bins   = h.Binning.Bins
		nxbins = h.Binning.Nx
		nybins = h.Binning.Ny
		xedges = make([]float64, 0, nxbins+1)
		yedges = make([]float64, 0, nybins+1)
	)

	hroot.th2.th1.entries = float64(h.Entries())
	hroot.th2.th1.tsumw = h.SumW()
	hroot.th2.th1.tsumw2 = h.SumW2()
	hroot.th2.th1.tsumwx = h.SumWX()
	hroot.th2.th1.tsumwx2 = h.SumWX2()
	hroot.th2.tsumwy = h.SumWY()
	hroot.th2.tsumwy2 = h.SumWY2()
	hroot.th2.tsumwxy = h.SumWXY()

	ncells := (nxbins + 2) * (nybins + 2)
	hroot.th2.th1.ncells = ncells

	hroot.th2.th1.xaxis.nbins = nxbins
	hroot.th2.th1.xaxis.xmin = h.XMin()
	hroot.th2.th1.xaxis.xmax = h.XMax()

	hroot.th2.th1.yaxis.nbins = nybins
	hroot.th2.th1.yaxis.xmin = h.YMin()
	hroot.th2.th1.yaxis.xmax = h.YMax()

	hroot.arr.Data = make([]float64, ncells)
	hroot.th2.th1.sumw2.Data = make([]float64, ncells)

	ibin := func(ix, iy int) int { return iy*nxbins + ix }

	for ix := range h.Binning.Nx {
		for iy := range h.Binning.Ny {
			i := ibin(ix, iy)
			bin := bins[i]
			if ix == 0 {
				yedges = append(yedges, bin.YMin())
			}
			if iy == 0 {
				xedges = append(xedges, bin.XMin())
			}
			hroot.setDist2D(ix+1, iy+1, bin.Dist.SumW(), bin.Dist.SumW2())
		}
	}

	oflows := h.Binning.Outflows[:]
	for i, v := range []struct{ ix, iy int }{
		{0, 0},
		{0, 1},
		{0, nybins + 1},
		{nxbins + 1, 0},
		{nxbins + 1, 1},
		{nxbins + 1, nybins + 1},
		{1, 0},
		{1, nybins + 1},
	} {
		hroot.setDist2D(v.ix, v.iy, oflows[i].SumW(), oflows[i].SumW2())
	}

	xedges = append(xedges, bins[ibin(h.Binning.Nx-1, 0)].XMax())
	yedges = append(yedges, bins[ibin(0, h.Binning.Ny-1)].YMax())

	hroot.th2.th1.SetName(h.Name())
	if v, ok := h.Annotation()["title"]; ok && v != nil {
		hroot.th2.th1.SetTitle(v.(string))
	}
	hroot.th2.th1.xaxis.xbins.Data = xedges
	hroot.th2.th1.yaxis.xbins.Data = yedges

	return hroot
}

// NewH2D creates a 2-dim histogram, as "new TH2D(name, title,
// nx, xmin, xmax, ny, ymin, ymax)" does in C++.
func NewH2D(name, title string, nx int, xmin, xmax float64, ny int, ymin, ymax float64) *H2D {
	h := newH2D()
	h.th2.th1.SetName(name)
	h.th2.th1.SetTitle(title)
	h.th2.th1.xaxis.setRange(nx, xmin, xmax)
	h.th2.th1.yaxis.setRange(ny, ymin, ymax)
	h.reset()
	return h
}

// reset sizes the cells to the axes and empties them.
func (h *H2D) reset() {
	n := (h.th1.xaxis.nbins + 2) * (h.th1.yaxis.nbins + 2)
	h.th2.th1.ncells = n
	h.arr.Data = make([]float64, n)
	h.th2.th1.sumw2.Data = make([]float64, n)
	h.th2.th1.entries = 0
	h.th2.th1.tsumw = 0
	h.th2.th1.tsumw2 = 0
	h.th2.th1.tsumwx = 0
	h.th2.th1.tsumwx2 = 0
	h.th2.tsumwy = 0
	h.th2.tsumwy2 = 0
	h.th2.tsumwxy = 0
}

// Reset empties the histogram, keeping its binning.
func (h *H2D) Reset() { h.reset() }

// FindBin returns the cell (x,y) falls in, as an index into the flat array
// the histogram keeps.
func (h *H2D) FindBin(x, y float64) int {
	return h.bin(h.th1.xaxis.FindBin(x), h.th1.yaxis.FindBin(y))
}

// Fill adds an entry of weight w at (x,y).
func (h *H2D) Fill(x, y, w float64) {
	var (
		ix = h.th1.xaxis.FindBin(x)
		iy = h.th1.yaxis.FindBin(y)
		i  = h.bin(ix, iy)
	)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}

	h.arr.Data[i] += float64(w)
	if len(h.th1.sumw2.Data) > i {
		h.th1.sumw2.Data[i] += w * w
	}

	h.th2.th1.entries++

	if ix > 0 && ix <= h.th1.xaxis.nbins && iy > 0 && iy <= h.th1.yaxis.nbins {
		h.th2.th1.tsumw += w
		h.th2.th1.tsumw2 += w * w
		h.th2.th1.tsumwx += w * x
		h.th2.th1.tsumwx2 += w * x * x
		h.th2.tsumwy += w * y
		h.th2.tsumwy2 += w * y * y
		h.th2.tsumwxy += w * x * y
	}
}

// FillN adds an entry for each (x,y) with the matching weight, or of weight
// one when ws is nil.
func (h *H2D) FillN(xs, ys, ws []float64) {
	if len(ys) != len(xs) || (ws != nil && len(ws) != len(xs)) {
		panic(fmt.Errorf("rhist: lengths mismatch"))
	}
	for i := range xs {
		w := 1.0
		if ws != nil {
			w = ws[i]
		}
		h.Fill(xs[i], ys[i], w)
	}
}

// BinContent returns the content of the cell at (ix,iy).
func (h *H2D) BinContent(ix, iy int) float64 {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	return float64(h.arr.Data[i])
}

// SetBinContent sets the content of the cell at (ix,iy).
func (h *H2D) SetBinContent(ix, iy int, v float64) {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}
	h.arr.Data[i] = float64(v)
}

// BinError returns the uncertainty on the cell at (ix,iy).
func (h *H2D) BinError(ix, iy int) float64 {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	if len(h.th1.sumw2.Data) > i {
		return math.Sqrt(h.th1.sumw2.Data[i])
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// SetBinError sets the uncertainty on the cell at (ix,iy).
func (h *H2D) SetBinError(ix, iy int, e float64) {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.th1.sumw2.Data) {
		return
	}
	h.th1.sumw2.Data[i] = e * e
}

// Scale multiplies every cell by f, and the uncertainties with them.
func (h *H2D) Scale(f float64) {
	for i := range h.arr.Data {
		h.arr.Data[i] = float64(float64(h.arr.Data[i]) * f)
	}
	for i := range h.th1.sumw2.Data {
		h.th1.sumw2.Data[i] *= f * f
	}
	h.th2.th1.tsumw *= f
	h.th2.th1.tsumw2 *= f * f
	h.th2.th1.tsumwx *= f
	h.th2.th1.tsumwx2 *= f
	h.th2.tsumwy *= f
	h.th2.tsumwy2 *= f
	h.th2.tsumwxy *= f
}

// Integral returns the sum of the bins proper, leaving out the under- and
// overflow.
func (h *H2D) Integral() float64 {
	var sum float64
	for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
		for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
			sum += h.BinContent(ix, iy)
		}
	}
	return sum
}

// ProjectionX sums the histogram over y and returns the 1-dim histogram that
// leaves, as TH2::ProjectionX does.
func (h *H2D) ProjectionX(name string) *H1D {
	o := NewH1D(name, h.Title(), h.th1.xaxis.nbins, h.th1.xaxis.xmin, h.th1.xaxis.xmax)
	if edges := h.th1.xaxis.xbins.Data; len(edges) == h.th1.xaxis.nbins+1 {
		o = NewH1DFromEdges(name, h.Title(), edges)
	}

	for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
		var sum, err2 float64
		for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
			sum += h.BinContent(ix, iy)
			e := h.BinError(ix, iy)
			err2 += e * e
		}
		o.SetBinContent(ix, sum)
		o.SetBinError(ix, math.Sqrt(err2))
	}

	o.th1.entries = h.th1.entries
	o.th1.tsumw = h.th2.th1.tsumw
	o.th1.tsumw2 = h.th2.th1.tsumw2
	o.th1.tsumwx = h.th2.th1.tsumwx
	o.th1.tsumwx2 = h.th2.th1.tsumwx2
	return o
}

// ProjectionY sums the histogram over x, as TH2::ProjectionY does.
func (h *H2D) ProjectionY(name string) *H1D {
	o := NewH1D(name, h.Title(), h.th1.yaxis.nbins, h.th1.yaxis.xmin, h.th1.yaxis.xmax)

	for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
		var sum, err2 float64
		for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
			sum += h.BinContent(ix, iy)
			e := h.BinError(ix, iy)
			err2 += e * e
		}
		o.SetBinContent(iy, sum)
		o.SetBinError(iy, math.Sqrt(err2))
	}

	o.th1.entries = h.th1.entries
	o.th1.tsumw = h.th2.th1.tsumw
	o.th1.tsumw2 = h.th2.th1.tsumw2
	o.th1.tsumwx = h.th2.tsumwy
	o.th1.tsumwx2 = h.th2.tsumwy2
	return o
}

// MeanX and MeanY return the means of the entries along each axis.
func (h *H2D) MeanX() float64 {
	if h.th2.th1.tsumw == 0 {
		return 0
	}
	return h.th2.th1.tsumwx / h.th2.th1.tsumw
}

// MeanY returns the mean along y.
func (h *H2D) MeanY() float64 {
	if h.th2.th1.tsumw == 0 {
		return 0
	}
	return h.th2.tsumwy / h.th2.th1.tsumw
}

func (*H2D) RVersion() int16 {
	return rvers.H2D
}

func (*H2D) isH2() {}

// Class returns the ROOT class name.
func (*H2D) Class() string {
	return "TH2D"
}

func (h *H2D) Array() rcont.ArrayD {
	return h.arr
}

// Rank returns the number of dimensions of this histogram.
func (h *H2D) Rank() int {
	return 2
}

// NbinsX returns the number of bins in X.
func (h *H2D) NbinsX() int {
	return h.th1.xaxis.nbins
}

// XAxis returns the axis along X.
func (h *H2D) XAxis() Axis {
	return &h.th1.xaxis
}

// XBinCenter returns the bin center value in X.
func (h *H2D) XBinCenter(i int) float64 {
	return float64(h.th1.xaxis.BinCenter(i))
}

// XBinContent returns the bin content value in X.
func (h *H2D) XBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// XBinError returns the bin error in X.
func (h *H2D) XBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// XBinLowEdge returns the bin lower edge value in X.
func (h *H2D) XBinLowEdge(i int) float64 {
	return h.th1.xaxis.BinLowEdge(i)
}

// XBinWidth returns the bin width in X.
func (h *H2D) XBinWidth(i int) float64 {
	return h.th1.xaxis.BinWidth(i)
}

// NbinsY returns the number of bins in Y.
func (h *H2D) NbinsY() int {
	return h.th1.yaxis.nbins
}

// YAxis returns the axis along Y.
func (h *H2D) YAxis() Axis {
	return &h.th1.yaxis
}

// YBinCenter returns the bin center value in Y.
func (h *H2D) YBinCenter(i int) float64 {
	return float64(h.th1.yaxis.BinCenter(i))
}

// YBinContent returns the bin content value in Y.
func (h *H2D) YBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// YBinError returns the bin error in Y.
func (h *H2D) YBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// YBinLowEdge returns the bin lower edge value in Y.
func (h *H2D) YBinLowEdge(i int) float64 {
	return h.th1.yaxis.BinLowEdge(i)
}

// YBinWidth returns the bin width in Y.
func (h *H2D) YBinWidth(i int) float64 {
	return h.th1.yaxis.BinWidth(i)
}

// bin returns the regularized bin number given an (x,y) bin index pair.
func (h *H2D) bin(ix, iy int) int {
	nx := h.th1.xaxis.nbins + 1 // overflow bin
	ny := h.th1.yaxis.nbins + 1 // overflow bin
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
	return ix + (nx+1)*iy
}

func (h *H2D) dist2D(ix, iy int) hbook.Dist2D {
	i := h.bin(ix, iy)
	vx := h.XBinContent(i)
	xerr := h.XBinError(i)
	nx := h.entries(vx, xerr)
	vy := h.YBinContent(i)
	yerr := h.YBinError(i)
	ny := h.entries(vy, yerr)

	sumw := h.arr.Data[i]
	sumw2 := 0.0
	if len(h.th1.sumw2.Data) > 0 {
		sumw2 = h.th1.sumw2.Data[i]
	}
	return hbook.Dist2D{
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
	}
}

func (h *H2D) setDist2D(ix, iy int, sumw, sumw2 float64) {
	i := h.bin(ix, iy)
	h.arr.Data[i] = float64(sumw)
	h.th1.sumw2.Data[i] = sumw2
}

func (h *H2D) entries(height, err float64) int64 {
	if height <= 0 {
		return 0
	}
	v := height / err
	return int64(v*v + 0.5)
}

// AsH2D creates a new hbook.H2D from this ROOT histogram.
func (h *H2D) AsH2D() *hbook.H2D {
	var (
		nx = h.NbinsX()
		ny = h.NbinsY()
		hh = hbook.NewH2D(
			nx, h.XAxis().XMin(), h.XAxis().XMax(),
			ny, h.YAxis().XMin(), h.YAxis().XMax(),
		)
		xinrange = 1
		yinrange = 1
	)
	hh.Ann = hbook.Annotation{
		"name":  h.Name(),
		"title": h.Title(),
	}
	hh.Binning.Outflows = [8]hbook.Dist2D{
		h.dist2D(0, 0),
		h.dist2D(0, yinrange),
		h.dist2D(0, ny+1),
		h.dist2D(nx+1, 0),
		h.dist2D(nx+1, yinrange),
		h.dist2D(nx+1, ny+1),
		h.dist2D(xinrange, 0),
		h.dist2D(xinrange, ny+1),
	}

	hh.Binning.Dist = hbook.Dist2D{
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
	}
	hh.Binning.Dist.X.Stats.SumWX = float64(h.SumWX())
	hh.Binning.Dist.X.Stats.SumWX2 = float64(h.SumWX2())
	hh.Binning.Dist.Y.Stats.SumWX = float64(h.SumWY())
	hh.Binning.Dist.Y.Stats.SumWX2 = float64(h.SumWY2())
	hh.Binning.Dist.Stats.SumWXY = h.SumWXY()

	for ix := range nx {
		for iy := range ny {
			var (
				i    = iy*nx + ix
				xmin = h.XBinLowEdge(ix + 1)
				xmax = h.XBinWidth(ix+1) + xmin
				ymin = h.YBinLowEdge(iy + 1)
				ymax = h.YBinWidth(iy+1) + ymin
				bin  = &hh.Binning.Bins[i]
			)
			bin.XRange.Min = xmin
			bin.XRange.Max = xmax
			bin.YRange.Min = ymin
			bin.YRange.Max = ymax
			bin.Dist = h.dist2D(ix+1, iy+1)
		}
	}

	return hh
}

// MarshalYODA implements the YODAMarshaler interface.
func (h *H2D) MarshalYODA() ([]byte, error) {
	return h.AsH2D().MarshalYODA()
}

// UnmarshalYODA implements the YODAUnmarshaler interface.
func (h *H2D) UnmarshalYODA(raw []byte) error {
	var hh hbook.H2D
	err := hh.UnmarshalYODA(raw)
	if err != nil {
		return err
	}

	*h = *NewH2DFrom(&hh)
	return nil
}

func (h *H2D) ROOTMerge(src root.Object) error {
	hsrc, ok := src.(*H2D)
	if !ok {
		return fmt.Errorf("rhist: object %q is not a *rhist.H2D (%T)", src.(root.Named).Name(), src)
	}

	var (
		h1   = h.AsH2D()
		h2   = hsrc.AsH2D()
		hadd = hbook.AddH2D(h1, h2)
	)

	*h = *NewH2DFrom(hadd)
	return nil
}

func (h *H2D) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(h.Class(), h.RVersion())
	w.WriteObject(&h.th2)
	w.WriteObject(&h.arr)

	return w.SetHeader(hdr)
}

func (h *H2D) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(h.Class(), h.RVersion())
	if hdr.Vers < 1 {
		return fmt.Errorf("rhist: TH2D version too old (%d<1)", hdr.Vers)
	}

	r.ReadObject(&h.th2)
	r.ReadObject(&h.arr)

	r.CheckHeader(hdr)
	return r.Err()
}

func (h *H2D) RMembers() (mbrs []rbytes.Member) {
	mbrs = append(mbrs, h.th2.RMembers()...)
	mbrs = append(mbrs, rbytes.Member{
		Name: "fArray", Value: &h.arr.Data,
	})
	return mbrs
}

func init() {
	f := func() reflect.Value {
		o := newH2D()
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TH2D", f)
}

var (
	_ root.Object        = (*H2D)(nil)
	_ root.Merger        = (*H2D)(nil)
	_ root.Named         = (*H2D)(nil)
	_ H2                 = (*H2D)(nil)
	_ rbytes.Marshaler   = (*H2D)(nil)
	_ rbytes.Unmarshaler = (*H2D)(nil)
	_ rbytes.RSlicer     = (*H2D)(nil)
)

// H2I implements ROOT TH2I
type H2I struct {
	th2
	arr rcont.ArrayI
}

func newH2I() *H2I {
	return &H2I{
		th2: *newH2(),
	}
}

// NewH2IFrom creates a new H2I from hbook 2-dim histogram.
func NewH2IFrom(h *hbook.H2D) *H2I {
	var (
		hroot  = newH2I()
		bins   = h.Binning.Bins
		nxbins = h.Binning.Nx
		nybins = h.Binning.Ny
		xedges = make([]float64, 0, nxbins+1)
		yedges = make([]float64, 0, nybins+1)
	)

	hroot.th2.th1.entries = float64(h.Entries())
	hroot.th2.th1.tsumw = h.SumW()
	hroot.th2.th1.tsumw2 = h.SumW2()
	hroot.th2.th1.tsumwx = h.SumWX()
	hroot.th2.th1.tsumwx2 = h.SumWX2()
	hroot.th2.tsumwy = h.SumWY()
	hroot.th2.tsumwy2 = h.SumWY2()
	hroot.th2.tsumwxy = h.SumWXY()

	ncells := (nxbins + 2) * (nybins + 2)
	hroot.th2.th1.ncells = ncells

	hroot.th2.th1.xaxis.nbins = nxbins
	hroot.th2.th1.xaxis.xmin = h.XMin()
	hroot.th2.th1.xaxis.xmax = h.XMax()

	hroot.th2.th1.yaxis.nbins = nybins
	hroot.th2.th1.yaxis.xmin = h.YMin()
	hroot.th2.th1.yaxis.xmax = h.YMax()

	hroot.arr.Data = make([]int32, ncells)
	hroot.th2.th1.sumw2.Data = make([]float64, ncells)

	ibin := func(ix, iy int) int { return iy*nxbins + ix }

	for ix := range h.Binning.Nx {
		for iy := range h.Binning.Ny {
			i := ibin(ix, iy)
			bin := bins[i]
			if ix == 0 {
				yedges = append(yedges, bin.YMin())
			}
			if iy == 0 {
				xedges = append(xedges, bin.XMin())
			}
			hroot.setDist2D(ix+1, iy+1, bin.Dist.SumW(), bin.Dist.SumW2())
		}
	}

	oflows := h.Binning.Outflows[:]
	for i, v := range []struct{ ix, iy int }{
		{0, 0},
		{0, 1},
		{0, nybins + 1},
		{nxbins + 1, 0},
		{nxbins + 1, 1},
		{nxbins + 1, nybins + 1},
		{1, 0},
		{1, nybins + 1},
	} {
		hroot.setDist2D(v.ix, v.iy, oflows[i].SumW(), oflows[i].SumW2())
	}

	xedges = append(xedges, bins[ibin(h.Binning.Nx-1, 0)].XMax())
	yedges = append(yedges, bins[ibin(0, h.Binning.Ny-1)].YMax())

	hroot.th2.th1.SetName(h.Name())
	if v, ok := h.Annotation()["title"]; ok && v != nil {
		hroot.th2.th1.SetTitle(v.(string))
	}
	hroot.th2.th1.xaxis.xbins.Data = xedges
	hroot.th2.th1.yaxis.xbins.Data = yedges

	return hroot
}

// NewH2I creates a 2-dim histogram, as "new TH2I(name, title,
// nx, xmin, xmax, ny, ymin, ymax)" does in C++.
func NewH2I(name, title string, nx int, xmin, xmax float64, ny int, ymin, ymax float64) *H2I {
	h := newH2I()
	h.th2.th1.SetName(name)
	h.th2.th1.SetTitle(title)
	h.th2.th1.xaxis.setRange(nx, xmin, xmax)
	h.th2.th1.yaxis.setRange(ny, ymin, ymax)
	h.reset()
	return h
}

// reset sizes the cells to the axes and empties them.
func (h *H2I) reset() {
	n := (h.th1.xaxis.nbins + 2) * (h.th1.yaxis.nbins + 2)
	h.th2.th1.ncells = n
	h.arr.Data = make([]int32, n)
	h.th2.th1.sumw2.Data = make([]float64, n)
	h.th2.th1.entries = 0
	h.th2.th1.tsumw = 0
	h.th2.th1.tsumw2 = 0
	h.th2.th1.tsumwx = 0
	h.th2.th1.tsumwx2 = 0
	h.th2.tsumwy = 0
	h.th2.tsumwy2 = 0
	h.th2.tsumwxy = 0
}

// Reset empties the histogram, keeping its binning.
func (h *H2I) Reset() { h.reset() }

// FindBin returns the cell (x,y) falls in, as an index into the flat array
// the histogram keeps.
func (h *H2I) FindBin(x, y float64) int {
	return h.bin(h.th1.xaxis.FindBin(x), h.th1.yaxis.FindBin(y))
}

// Fill adds an entry of weight w at (x,y).
func (h *H2I) Fill(x, y, w float64) {
	var (
		ix = h.th1.xaxis.FindBin(x)
		iy = h.th1.yaxis.FindBin(y)
		i  = h.bin(ix, iy)
	)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}

	h.arr.Data[i] += int32(w)
	if len(h.th1.sumw2.Data) > i {
		h.th1.sumw2.Data[i] += w * w
	}

	h.th2.th1.entries++

	if ix > 0 && ix <= h.th1.xaxis.nbins && iy > 0 && iy <= h.th1.yaxis.nbins {
		h.th2.th1.tsumw += w
		h.th2.th1.tsumw2 += w * w
		h.th2.th1.tsumwx += w * x
		h.th2.th1.tsumwx2 += w * x * x
		h.th2.tsumwy += w * y
		h.th2.tsumwy2 += w * y * y
		h.th2.tsumwxy += w * x * y
	}
}

// FillN adds an entry for each (x,y) with the matching weight, or of weight
// one when ws is nil.
func (h *H2I) FillN(xs, ys, ws []float64) {
	if len(ys) != len(xs) || (ws != nil && len(ws) != len(xs)) {
		panic(fmt.Errorf("rhist: lengths mismatch"))
	}
	for i := range xs {
		w := 1.0
		if ws != nil {
			w = ws[i]
		}
		h.Fill(xs[i], ys[i], w)
	}
}

// BinContent returns the content of the cell at (ix,iy).
func (h *H2I) BinContent(ix, iy int) float64 {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	return float64(h.arr.Data[i])
}

// SetBinContent sets the content of the cell at (ix,iy).
func (h *H2I) SetBinContent(ix, iy int, v float64) {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}
	h.arr.Data[i] = int32(v)
}

// BinError returns the uncertainty on the cell at (ix,iy).
func (h *H2I) BinError(ix, iy int) float64 {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	if len(h.th1.sumw2.Data) > i {
		return math.Sqrt(h.th1.sumw2.Data[i])
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// SetBinError sets the uncertainty on the cell at (ix,iy).
func (h *H2I) SetBinError(ix, iy int, e float64) {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.th1.sumw2.Data) {
		return
	}
	h.th1.sumw2.Data[i] = e * e
}

// Scale multiplies every cell by f, and the uncertainties with them.
func (h *H2I) Scale(f float64) {
	for i := range h.arr.Data {
		h.arr.Data[i] = int32(float64(h.arr.Data[i]) * f)
	}
	for i := range h.th1.sumw2.Data {
		h.th1.sumw2.Data[i] *= f * f
	}
	h.th2.th1.tsumw *= f
	h.th2.th1.tsumw2 *= f * f
	h.th2.th1.tsumwx *= f
	h.th2.th1.tsumwx2 *= f
	h.th2.tsumwy *= f
	h.th2.tsumwy2 *= f
	h.th2.tsumwxy *= f
}

// Integral returns the sum of the bins proper, leaving out the under- and
// overflow.
func (h *H2I) Integral() float64 {
	var sum float64
	for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
		for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
			sum += h.BinContent(ix, iy)
		}
	}
	return sum
}

// ProjectionX sums the histogram over y and returns the 1-dim histogram that
// leaves, as TH2::ProjectionX does.
func (h *H2I) ProjectionX(name string) *H1D {
	o := NewH1D(name, h.Title(), h.th1.xaxis.nbins, h.th1.xaxis.xmin, h.th1.xaxis.xmax)
	if edges := h.th1.xaxis.xbins.Data; len(edges) == h.th1.xaxis.nbins+1 {
		o = NewH1DFromEdges(name, h.Title(), edges)
	}

	for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
		var sum, err2 float64
		for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
			sum += h.BinContent(ix, iy)
			e := h.BinError(ix, iy)
			err2 += e * e
		}
		o.SetBinContent(ix, sum)
		o.SetBinError(ix, math.Sqrt(err2))
	}

	o.th1.entries = h.th1.entries
	o.th1.tsumw = h.th2.th1.tsumw
	o.th1.tsumw2 = h.th2.th1.tsumw2
	o.th1.tsumwx = h.th2.th1.tsumwx
	o.th1.tsumwx2 = h.th2.th1.tsumwx2
	return o
}

// ProjectionY sums the histogram over x, as TH2::ProjectionY does.
func (h *H2I) ProjectionY(name string) *H1D {
	o := NewH1D(name, h.Title(), h.th1.yaxis.nbins, h.th1.yaxis.xmin, h.th1.yaxis.xmax)

	for iy := 1; iy <= h.th1.yaxis.nbins; iy++ {
		var sum, err2 float64
		for ix := 1; ix <= h.th1.xaxis.nbins; ix++ {
			sum += h.BinContent(ix, iy)
			e := h.BinError(ix, iy)
			err2 += e * e
		}
		o.SetBinContent(iy, sum)
		o.SetBinError(iy, math.Sqrt(err2))
	}

	o.th1.entries = h.th1.entries
	o.th1.tsumw = h.th2.th1.tsumw
	o.th1.tsumw2 = h.th2.th1.tsumw2
	o.th1.tsumwx = h.th2.tsumwy
	o.th1.tsumwx2 = h.th2.tsumwy2
	return o
}

// MeanX and MeanY return the means of the entries along each axis.
func (h *H2I) MeanX() float64 {
	if h.th2.th1.tsumw == 0 {
		return 0
	}
	return h.th2.th1.tsumwx / h.th2.th1.tsumw
}

// MeanY returns the mean along y.
func (h *H2I) MeanY() float64 {
	if h.th2.th1.tsumw == 0 {
		return 0
	}
	return h.th2.tsumwy / h.th2.th1.tsumw
}

func (*H2I) RVersion() int16 {
	return rvers.H2I
}

func (*H2I) isH2() {}

// Class returns the ROOT class name.
func (*H2I) Class() string {
	return "TH2I"
}

func (h *H2I) Array() rcont.ArrayI {
	return h.arr
}

// Rank returns the number of dimensions of this histogram.
func (h *H2I) Rank() int {
	return 2
}

// NbinsX returns the number of bins in X.
func (h *H2I) NbinsX() int {
	return h.th1.xaxis.nbins
}

// XAxis returns the axis along X.
func (h *H2I) XAxis() Axis {
	return &h.th1.xaxis
}

// XBinCenter returns the bin center value in X.
func (h *H2I) XBinCenter(i int) float64 {
	return float64(h.th1.xaxis.BinCenter(i))
}

// XBinContent returns the bin content value in X.
func (h *H2I) XBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// XBinError returns the bin error in X.
func (h *H2I) XBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// XBinLowEdge returns the bin lower edge value in X.
func (h *H2I) XBinLowEdge(i int) float64 {
	return h.th1.xaxis.BinLowEdge(i)
}

// XBinWidth returns the bin width in X.
func (h *H2I) XBinWidth(i int) float64 {
	return h.th1.xaxis.BinWidth(i)
}

// NbinsY returns the number of bins in Y.
func (h *H2I) NbinsY() int {
	return h.th1.yaxis.nbins
}

// YAxis returns the axis along Y.
func (h *H2I) YAxis() Axis {
	return &h.th1.yaxis
}

// YBinCenter returns the bin center value in Y.
func (h *H2I) YBinCenter(i int) float64 {
	return float64(h.th1.yaxis.BinCenter(i))
}

// YBinContent returns the bin content value in Y.
func (h *H2I) YBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// YBinError returns the bin error in Y.
func (h *H2I) YBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// YBinLowEdge returns the bin lower edge value in Y.
func (h *H2I) YBinLowEdge(i int) float64 {
	return h.th1.yaxis.BinLowEdge(i)
}

// YBinWidth returns the bin width in Y.
func (h *H2I) YBinWidth(i int) float64 {
	return h.th1.yaxis.BinWidth(i)
}

// bin returns the regularized bin number given an (x,y) bin index pair.
func (h *H2I) bin(ix, iy int) int {
	nx := h.th1.xaxis.nbins + 1 // overflow bin
	ny := h.th1.yaxis.nbins + 1 // overflow bin
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
	return ix + (nx+1)*iy
}

func (h *H2I) dist2D(ix, iy int) hbook.Dist2D {
	i := h.bin(ix, iy)
	vx := h.XBinContent(i)
	xerr := h.XBinError(i)
	nx := h.entries(vx, xerr)
	vy := h.YBinContent(i)
	yerr := h.YBinError(i)
	ny := h.entries(vy, yerr)

	sumw := h.arr.Data[i]
	sumw2 := 0.0
	if len(h.th1.sumw2.Data) > 0 {
		sumw2 = h.th1.sumw2.Data[i]
	}
	return hbook.Dist2D{
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
	}
}

func (h *H2I) setDist2D(ix, iy int, sumw, sumw2 float64) {
	i := h.bin(ix, iy)
	h.arr.Data[i] = int32(sumw)
	h.th1.sumw2.Data[i] = sumw2
}

func (h *H2I) entries(height, err float64) int64 {
	if height <= 0 {
		return 0
	}
	v := height / err
	return int64(v*v + 0.5)
}

// AsH2D creates a new hbook.H2D from this ROOT histogram.
func (h *H2I) AsH2D() *hbook.H2D {
	var (
		nx = h.NbinsX()
		ny = h.NbinsY()
		hh = hbook.NewH2D(
			nx, h.XAxis().XMin(), h.XAxis().XMax(),
			ny, h.YAxis().XMin(), h.YAxis().XMax(),
		)
		xinrange = 1
		yinrange = 1
	)
	hh.Ann = hbook.Annotation{
		"name":  h.Name(),
		"title": h.Title(),
	}
	hh.Binning.Outflows = [8]hbook.Dist2D{
		h.dist2D(0, 0),
		h.dist2D(0, yinrange),
		h.dist2D(0, ny+1),
		h.dist2D(nx+1, 0),
		h.dist2D(nx+1, yinrange),
		h.dist2D(nx+1, ny+1),
		h.dist2D(xinrange, 0),
		h.dist2D(xinrange, ny+1),
	}

	hh.Binning.Dist = hbook.Dist2D{
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
	}
	hh.Binning.Dist.X.Stats.SumWX = float64(h.SumWX())
	hh.Binning.Dist.X.Stats.SumWX2 = float64(h.SumWX2())
	hh.Binning.Dist.Y.Stats.SumWX = float64(h.SumWY())
	hh.Binning.Dist.Y.Stats.SumWX2 = float64(h.SumWY2())
	hh.Binning.Dist.Stats.SumWXY = h.SumWXY()

	for ix := range nx {
		for iy := range ny {
			var (
				i    = iy*nx + ix
				xmin = h.XBinLowEdge(ix + 1)
				xmax = h.XBinWidth(ix+1) + xmin
				ymin = h.YBinLowEdge(iy + 1)
				ymax = h.YBinWidth(iy+1) + ymin
				bin  = &hh.Binning.Bins[i]
			)
			bin.XRange.Min = xmin
			bin.XRange.Max = xmax
			bin.YRange.Min = ymin
			bin.YRange.Max = ymax
			bin.Dist = h.dist2D(ix+1, iy+1)
		}
	}

	return hh
}

// MarshalYODA implements the YODAMarshaler interface.
func (h *H2I) MarshalYODA() ([]byte, error) {
	return h.AsH2D().MarshalYODA()
}

// UnmarshalYODA implements the YODAUnmarshaler interface.
func (h *H2I) UnmarshalYODA(raw []byte) error {
	var hh hbook.H2D
	err := hh.UnmarshalYODA(raw)
	if err != nil {
		return err
	}

	*h = *NewH2IFrom(&hh)
	return nil
}

func (h *H2I) ROOTMerge(src root.Object) error {
	hsrc, ok := src.(*H2I)
	if !ok {
		return fmt.Errorf("rhist: object %q is not a *rhist.H2I (%T)", src.(root.Named).Name(), src)
	}

	var (
		h1   = h.AsH2D()
		h2   = hsrc.AsH2D()
		hadd = hbook.AddH2D(h1, h2)
	)

	*h = *NewH2IFrom(hadd)
	return nil
}

func (h *H2I) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(h.Class(), h.RVersion())
	w.WriteObject(&h.th2)
	w.WriteObject(&h.arr)

	return w.SetHeader(hdr)
}

func (h *H2I) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(h.Class(), h.RVersion())
	if hdr.Vers < 1 {
		return fmt.Errorf("rhist: TH2I version too old (%d<1)", hdr.Vers)
	}

	r.ReadObject(&h.th2)
	r.ReadObject(&h.arr)

	r.CheckHeader(hdr)
	return r.Err()
}

func (h *H2I) RMembers() (mbrs []rbytes.Member) {
	mbrs = append(mbrs, h.th2.RMembers()...)
	mbrs = append(mbrs, rbytes.Member{
		Name: "fArray", Value: &h.arr.Data,
	})
	return mbrs
}

func init() {
	f := func() reflect.Value {
		o := newH2I()
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TH2I", f)
}

var (
	_ root.Object        = (*H2I)(nil)
	_ root.Merger        = (*H2I)(nil)
	_ root.Named         = (*H2I)(nil)
	_ H2                 = (*H2I)(nil)
	_ rbytes.Marshaler   = (*H2I)(nil)
	_ rbytes.Unmarshaler = (*H2I)(nil)
	_ rbytes.RSlicer     = (*H2I)(nil)
)
