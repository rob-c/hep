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

// H1C implements ROOT TH1C
type H1C struct {
	th1
	arr rcont.ArrayC
}

func newH1C() *H1C {
	return &H1C{
		th1: *newH1(),
	}
}

// NewH1CFrom creates a new 1-dim histogram from hbook.
func NewH1CFrom(h *hbook.H1D) *H1C {
	var (
		hroot = newH1C()
		bins  = h.Binning.Bins
		nbins = len(bins)
		edges = make([]float64, 0, nbins+1)
		uflow = h.Binning.Underflow()
		oflow = h.Binning.Overflow()
	)

	hroot.th1.entries = float64(h.Entries())
	hroot.th1.tsumw = h.SumW()
	hroot.th1.tsumw2 = h.SumW2()
	hroot.th1.tsumwx = h.SumWX()
	hroot.th1.tsumwx2 = h.SumWX2()
	hroot.th1.ncells = nbins + 2

	hroot.th1.xaxis.nbins = nbins
	hroot.th1.xaxis.xmin = h.XMin()
	hroot.th1.xaxis.xmax = h.XMax()

	hroot.arr.Data = make([]int8, nbins+2)
	hroot.th1.sumw2.Data = make([]float64, nbins+2)

	for i, bin := range bins {
		if i == 0 {
			edges = append(edges, bin.XMin())
		}
		edges = append(edges, bin.XMax())
		hroot.setDist1D(i+1, bin.Dist.SumW(), bin.Dist.SumW2())
	}
	hroot.setDist1D(0, uflow.SumW(), uflow.SumW2())
	hroot.setDist1D(nbins+1, oflow.SumW(), oflow.SumW2())

	hroot.th1.SetName(h.Name())
	if v, ok := h.Annotation()["title"]; ok && v != nil {
		hroot.th1.SetTitle(v.(string))
	}
	hroot.th1.xaxis.xbins.Data = edges
	return hroot
}

// NewH1C creates a 1-dim histogram with n equal bins between xmin and
// xmax, as "new TH1C(name, title, n, xmin, xmax)" does in C++.
func NewH1C(name, title string, n int, xmin, xmax float64) *H1C {
	h := newH1C()
	h.th1.SetName(name)
	h.th1.SetTitle(title)
	h.th1.xaxis.setRange(n, xmin, xmax)
	h.reset()
	return h
}

// NewH1CFromEdges creates a 1-dim histogram whose bins are the ones the
// edges describe, for a binning that is not uniform.
func NewH1CFromEdges(name, title string, edges []float64) *H1C {
	h := newH1C()
	h.th1.SetName(name)
	h.th1.SetTitle(title)
	h.th1.xaxis.setEdges(edges)
	h.reset()
	return h
}

// reset sizes the cells to the axis and empties them.
func (h *H1C) reset() {
	n := h.th1.xaxis.nbins + 2 // and the under- and overflow
	h.th1.ncells = n
	h.arr.Data = make([]int8, n)
	h.th1.sumw2.Data = make([]float64, n)
	h.th1.entries = 0
	h.th1.tsumw = 0
	h.th1.tsumw2 = 0
	h.th1.tsumwx = 0
	h.th1.tsumwx2 = 0
}

// Reset empties the histogram, keeping its binning.
func (h *H1C) Reset() { h.reset() }

// FindBin returns the bin x falls in: 0 for the underflow, 1 to NbinsX for
// the bins proper, NbinsX+1 for the overflow.
func (h *H1C) FindBin(x float64) int {
	return h.th1.xaxis.FindBin(x)
}

// Fill adds an entry of weight w at x.
func (h *H1C) Fill(x, w float64) {
	i := h.FindBin(x)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}

	h.arr.Data[i] += int8(w)
	if len(h.th1.sumw2.Data) > i {
		h.th1.sumw2.Data[i] += w * w
	}

	h.th1.entries++

	// The sums over the whole histogram, which is where the mean and the
	// width come from. ROOT leaves out what fell outside the axis, since a
	// mean of the overflow is a mean of nothing in particular.
	if i > 0 && i <= h.th1.xaxis.nbins {
		h.th1.tsumw += w
		h.th1.tsumw2 += w * w
		h.th1.tsumwx += w * x
		h.th1.tsumwx2 += w * x * x
	}
}

// FillN adds an entry for each x with the matching weight, or of weight one
// when ws is nil.
//
// FillN panics if the slices are of different lengths.
func (h *H1C) FillN(xs, ws []float64) {
	if ws != nil && len(ws) != len(xs) {
		panic(fmt.Errorf("rhist: %d values and %d weights", len(xs), len(ws)))
	}
	for i, x := range xs {
		w := 1.0
		if ws != nil {
			w = ws[i]
		}
		h.Fill(x, w)
	}
}

// BinContent returns the content of the i-th cell, counting the underflow as
// zero and the overflow as NbinsX+1.
func (h *H1C) BinContent(i int) float64 {
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	return float64(h.arr.Data[i])
}

// SetBinContent sets the content of the i-th cell.
func (h *H1C) SetBinContent(i int, v float64) {
	if i < 0 || i >= len(h.arr.Data) {
		return
	}
	h.arr.Data[i] = int8(v)
}

// BinError returns the uncertainty on the i-th cell: the square root of its
// sum of squared weights, or of its content where no such sum is kept.
func (h *H1C) BinError(i int) float64 {
	return h.XBinError(i)
}

// SetBinError sets the uncertainty on the i-th cell.
func (h *H1C) SetBinError(i int, e float64) {
	if i < 0 || i >= len(h.th1.sumw2.Data) {
		return
	}
	h.th1.sumw2.Data[i] = e * e
}

// Scale multiplies every cell by f, and the uncertainties with them.
func (h *H1C) Scale(f float64) {
	for i := range h.arr.Data {
		h.arr.Data[i] = int8(float64(h.arr.Data[i]) * f)
	}
	for i := range h.th1.sumw2.Data {
		h.th1.sumw2.Data[i] *= f * f
	}
	h.th1.tsumw *= f
	h.th1.tsumw2 *= f * f
	h.th1.tsumwx *= f
	h.th1.tsumwx2 *= f
}

// Integral returns the sum of the bins proper, leaving out the under- and
// overflow.
func (h *H1C) Integral() float64 {
	return h.IntegralRange(1, h.th1.xaxis.nbins)
}

// IntegralRange returns the sum of the cells from lo to hi, both included.
func (h *H1C) IntegralRange(lo, hi int) float64 {
	var sum float64
	for i := max(0, lo); i <= min(hi, len(h.arr.Data)-1); i++ {
		sum += float64(h.arr.Data[i])
	}
	return sum
}

// Maximum returns the largest content of the bins proper, and MaximumBin
// which bin holds it.
func (h *H1C) Maximum() float64 {
	_, v := h.maxBin()
	return v
}

// MaximumBin returns the bin holding the largest content.
func (h *H1C) MaximumBin() int {
	i, _ := h.maxBin()
	return i
}

func (h *H1C) maxBin() (int, float64) {
	var (
		at = 0
		mx = math.Inf(-1)
	)
	for i := 1; i <= h.th1.xaxis.nbins && i < len(h.arr.Data); i++ {
		if v := float64(h.arr.Data[i]); v > mx {
			at, mx = i, v
		}
	}
	if math.IsInf(mx, -1) {
		return 0, 0
	}
	return at, mx
}

// Minimum returns the smallest content of the bins proper.
func (h *H1C) Minimum() float64 {
	mn := math.Inf(+1)
	for i := 1; i <= h.th1.xaxis.nbins && i < len(h.arr.Data); i++ {
		if v := float64(h.arr.Data[i]); v < mn {
			mn = v
		}
	}
	if math.IsInf(mn, +1) {
		return 0
	}
	return mn
}

// Mean returns the mean of the entries, from the running sums rather than
// from the bins, so it is the mean of what was filled and not of where the
// bins are.
func (h *H1C) Mean() float64 {
	if h.th1.tsumw == 0 {
		return 0
	}
	return h.th1.tsumwx / h.th1.tsumw
}

// StdDev returns the standard deviation of the entries.
func (h *H1C) StdDev() float64 {
	if h.th1.tsumw == 0 {
		return 0
	}
	var (
		m = h.Mean()
		v = h.th1.tsumwx2/h.th1.tsumw - m*m
	)
	if v <= 0 {
		return 0
	}
	return math.Sqrt(v)
}

func (*H1C) RVersion() int16 {
	return rvers.H1C
}

func (*H1C) isH1() {}

// Class returns the ROOT class name.
func (*H1C) Class() string {
	return "TH1C"
}

func (h *H1C) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(h.Class(), h.RVersion())

	w.WriteObject(&h.th1)
	w.WriteObject(&h.arr)

	return w.SetHeader(hdr)
}

func (h *H1C) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(h.Class(), h.RVersion())

	r.ReadObject(&h.th1)
	r.ReadObject(&h.arr)

	r.CheckHeader(hdr)
	return r.Err()
}

func (h *H1C) RMembers() (mbrs []rbytes.Member) {
	mbrs = append(mbrs, h.th1.RMembers()...)
	mbrs = append(mbrs, rbytes.Member{
		Name: "fArray", Value: &h.arr.Data,
	})
	return mbrs
}

func (h *H1C) Array() rcont.ArrayC {
	return h.arr
}

// Rank returns the number of dimensions of this histogram.
func (h *H1C) Rank() int {
	return 1
}

// NbinsX returns the number of bins in X.
func (h *H1C) NbinsX() int {
	return h.th1.xaxis.nbins
}

// XAxis returns the axis along X.
func (h *H1C) XAxis() Axis {
	return &h.th1.xaxis
}

// bin returns the regularized bin number given an x bin pair.
func (h *H1C) bin(ix int) int {
	nx := h.th1.xaxis.nbins + 1 // overflow bin
	switch {
	case ix < 0:
		ix = 0
	case ix > nx:
		ix = nx
	}
	return ix
}

// XBinCenter returns the bin center value in X.
func (h *H1C) XBinCenter(i int) float64 {
	return float64(h.th1.xaxis.BinCenter(i))
}

// XBinContent returns the bin content value in X.
func (h *H1C) XBinContent(i int) float64 {
	ibin := h.bin(i)
	return float64(h.arr.Data[ibin])
}

// XBinError returns the bin error in X.
func (h *H1C) XBinError(i int) float64 {
	ibin := h.bin(i)
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[ibin]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[ibin])))
}

// XBinLowEdge returns the bin lower edge value in X.
func (h *H1C) XBinLowEdge(i int) float64 {
	return h.th1.xaxis.BinLowEdge(i)
}

// XBinWidth returns the bin width in X.
func (h *H1C) XBinWidth(i int) float64 {
	return h.th1.xaxis.BinWidth(i)
}

func (h *H1C) dist1D(i int) hbook.Dist1D {
	v := h.XBinContent(i)
	err := h.XBinError(i)
	n := h.entries(v, err)
	sumw := h.arr.Data[i]
	sumw2 := 0.0
	if len(h.th1.sumw2.Data) > 0 {
		sumw2 = h.th1.sumw2.Data[i]
	}
	return hbook.Dist1D{
		Dist: hbook.Dist0D{
			N:     n,
			SumW:  float64(sumw),
			SumW2: float64(sumw2),
		},
	}
}

func (h *H1C) setDist1D(i int, sumw, sumw2 float64) {
	h.arr.Data[i] = int8(sumw)
	h.th1.sumw2.Data[i] = sumw2
}

func (h *H1C) entries(height, err float64) int64 {
	if height <= 0 {
		return 0
	}
	v := height / err
	return int64(v*v + 0.5)
}

// AsH1D creates a new hbook.H1D from this ROOT histogram.
func (h *H1C) AsH1D() *hbook.H1D {
	var (
		nx = h.NbinsX()
		hh = hbook.NewH1D(int(nx), h.XAxis().XMin(), h.XAxis().XMax())
	)
	hh.Ann = hbook.Annotation{
		"name":  h.Name(),
		"title": h.Title(),
	}

	hh.Binning.Dist = hbook.Dist1D{
		Dist: hbook.Dist0D{
			N:     int64(h.Entries()),
			SumW:  float64(h.SumW()),
			SumW2: float64(h.SumW2()),
		},
	}
	hh.Binning.Dist.Stats.SumWX = float64(h.SumWX())
	hh.Binning.Dist.Stats.SumWX2 = float64(h.SumWX2())

	hh.Binning.Outflows = [2]hbook.Dist1D{
		h.dist1D(0),      // underflow
		h.dist1D(nx + 1), // overflow
	}

	for i := range nx {
		bin := &hh.Binning.Bins[i]
		xmin := h.XBinLowEdge(i + 1)
		xmax := h.XBinWidth(i+1) + xmin
		bin.Dist = h.dist1D(i + 1)
		bin.Range.Min = xmin
		bin.Range.Max = xmax
		hh.Binning.Bins[i].Dist = h.dist1D(i + 1)
	}

	return hh
}

// MarshalYODA implements the YODAMarshaler interface.
func (h *H1C) MarshalYODA() ([]byte, error) {
	return h.AsH1D().MarshalYODA()
}

// UnmarshalYODA implements the YODAUnmarshaler interface.
func (h *H1C) UnmarshalYODA(raw []byte) error {
	var hh hbook.H1D
	err := hh.UnmarshalYODA(raw)
	if err != nil {
		return err
	}

	*h = *NewH1CFrom(&hh)
	return nil
}

func (h *H1C) ROOTMerge(src root.Object) error {
	hsrc, ok := src.(*H1C)
	if !ok {
		return fmt.Errorf("rhist: object %q is not a *rhist.H1C (%T)", src.(root.Named).Name(), src)
	}

	var (
		h1   = h.AsH1D()
		h2   = hsrc.AsH1D()
		hadd = hbook.AddH1D(h1, h2)
	)

	*h = *NewH1CFrom(hadd)
	return nil
}

func init() {
	f := func() reflect.Value {
		o := newH1C()
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TH1C", f)
}

var (
	_ root.Object        = (*H1C)(nil)
	_ root.Merger        = (*H1C)(nil)
	_ root.Named         = (*H1C)(nil)
	_ H1                 = (*H1C)(nil)
	_ rbytes.Marshaler   = (*H1C)(nil)
	_ rbytes.Unmarshaler = (*H1C)(nil)
	_ rbytes.RSlicer     = (*H1C)(nil)
)

// H1S implements ROOT TH1S
type H1S struct {
	th1
	arr rcont.ArrayS
}

func newH1S() *H1S {
	return &H1S{
		th1: *newH1(),
	}
}

// NewH1SFrom creates a new 1-dim histogram from hbook.
func NewH1SFrom(h *hbook.H1D) *H1S {
	var (
		hroot = newH1S()
		bins  = h.Binning.Bins
		nbins = len(bins)
		edges = make([]float64, 0, nbins+1)
		uflow = h.Binning.Underflow()
		oflow = h.Binning.Overflow()
	)

	hroot.th1.entries = float64(h.Entries())
	hroot.th1.tsumw = h.SumW()
	hroot.th1.tsumw2 = h.SumW2()
	hroot.th1.tsumwx = h.SumWX()
	hroot.th1.tsumwx2 = h.SumWX2()
	hroot.th1.ncells = nbins + 2

	hroot.th1.xaxis.nbins = nbins
	hroot.th1.xaxis.xmin = h.XMin()
	hroot.th1.xaxis.xmax = h.XMax()

	hroot.arr.Data = make([]int16, nbins+2)
	hroot.th1.sumw2.Data = make([]float64, nbins+2)

	for i, bin := range bins {
		if i == 0 {
			edges = append(edges, bin.XMin())
		}
		edges = append(edges, bin.XMax())
		hroot.setDist1D(i+1, bin.Dist.SumW(), bin.Dist.SumW2())
	}
	hroot.setDist1D(0, uflow.SumW(), uflow.SumW2())
	hroot.setDist1D(nbins+1, oflow.SumW(), oflow.SumW2())

	hroot.th1.SetName(h.Name())
	if v, ok := h.Annotation()["title"]; ok && v != nil {
		hroot.th1.SetTitle(v.(string))
	}
	hroot.th1.xaxis.xbins.Data = edges
	return hroot
}

// NewH1S creates a 1-dim histogram with n equal bins between xmin and
// xmax, as "new TH1S(name, title, n, xmin, xmax)" does in C++.
func NewH1S(name, title string, n int, xmin, xmax float64) *H1S {
	h := newH1S()
	h.th1.SetName(name)
	h.th1.SetTitle(title)
	h.th1.xaxis.setRange(n, xmin, xmax)
	h.reset()
	return h
}

// NewH1SFromEdges creates a 1-dim histogram whose bins are the ones the
// edges describe, for a binning that is not uniform.
func NewH1SFromEdges(name, title string, edges []float64) *H1S {
	h := newH1S()
	h.th1.SetName(name)
	h.th1.SetTitle(title)
	h.th1.xaxis.setEdges(edges)
	h.reset()
	return h
}

// reset sizes the cells to the axis and empties them.
func (h *H1S) reset() {
	n := h.th1.xaxis.nbins + 2 // and the under- and overflow
	h.th1.ncells = n
	h.arr.Data = make([]int16, n)
	h.th1.sumw2.Data = make([]float64, n)
	h.th1.entries = 0
	h.th1.tsumw = 0
	h.th1.tsumw2 = 0
	h.th1.tsumwx = 0
	h.th1.tsumwx2 = 0
}

// Reset empties the histogram, keeping its binning.
func (h *H1S) Reset() { h.reset() }

// FindBin returns the bin x falls in: 0 for the underflow, 1 to NbinsX for
// the bins proper, NbinsX+1 for the overflow.
func (h *H1S) FindBin(x float64) int {
	return h.th1.xaxis.FindBin(x)
}

// Fill adds an entry of weight w at x.
func (h *H1S) Fill(x, w float64) {
	i := h.FindBin(x)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}

	h.arr.Data[i] += int16(w)
	if len(h.th1.sumw2.Data) > i {
		h.th1.sumw2.Data[i] += w * w
	}

	h.th1.entries++

	// The sums over the whole histogram, which is where the mean and the
	// width come from. ROOT leaves out what fell outside the axis, since a
	// mean of the overflow is a mean of nothing in particular.
	if i > 0 && i <= h.th1.xaxis.nbins {
		h.th1.tsumw += w
		h.th1.tsumw2 += w * w
		h.th1.tsumwx += w * x
		h.th1.tsumwx2 += w * x * x
	}
}

// FillN adds an entry for each x with the matching weight, or of weight one
// when ws is nil.
//
// FillN panics if the slices are of different lengths.
func (h *H1S) FillN(xs, ws []float64) {
	if ws != nil && len(ws) != len(xs) {
		panic(fmt.Errorf("rhist: %d values and %d weights", len(xs), len(ws)))
	}
	for i, x := range xs {
		w := 1.0
		if ws != nil {
			w = ws[i]
		}
		h.Fill(x, w)
	}
}

// BinContent returns the content of the i-th cell, counting the underflow as
// zero and the overflow as NbinsX+1.
func (h *H1S) BinContent(i int) float64 {
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	return float64(h.arr.Data[i])
}

// SetBinContent sets the content of the i-th cell.
func (h *H1S) SetBinContent(i int, v float64) {
	if i < 0 || i >= len(h.arr.Data) {
		return
	}
	h.arr.Data[i] = int16(v)
}

// BinError returns the uncertainty on the i-th cell: the square root of its
// sum of squared weights, or of its content where no such sum is kept.
func (h *H1S) BinError(i int) float64 {
	return h.XBinError(i)
}

// SetBinError sets the uncertainty on the i-th cell.
func (h *H1S) SetBinError(i int, e float64) {
	if i < 0 || i >= len(h.th1.sumw2.Data) {
		return
	}
	h.th1.sumw2.Data[i] = e * e
}

// Scale multiplies every cell by f, and the uncertainties with them.
func (h *H1S) Scale(f float64) {
	for i := range h.arr.Data {
		h.arr.Data[i] = int16(float64(h.arr.Data[i]) * f)
	}
	for i := range h.th1.sumw2.Data {
		h.th1.sumw2.Data[i] *= f * f
	}
	h.th1.tsumw *= f
	h.th1.tsumw2 *= f * f
	h.th1.tsumwx *= f
	h.th1.tsumwx2 *= f
}

// Integral returns the sum of the bins proper, leaving out the under- and
// overflow.
func (h *H1S) Integral() float64 {
	return h.IntegralRange(1, h.th1.xaxis.nbins)
}

// IntegralRange returns the sum of the cells from lo to hi, both included.
func (h *H1S) IntegralRange(lo, hi int) float64 {
	var sum float64
	for i := max(0, lo); i <= min(hi, len(h.arr.Data)-1); i++ {
		sum += float64(h.arr.Data[i])
	}
	return sum
}

// Maximum returns the largest content of the bins proper, and MaximumBin
// which bin holds it.
func (h *H1S) Maximum() float64 {
	_, v := h.maxBin()
	return v
}

// MaximumBin returns the bin holding the largest content.
func (h *H1S) MaximumBin() int {
	i, _ := h.maxBin()
	return i
}

func (h *H1S) maxBin() (int, float64) {
	var (
		at = 0
		mx = math.Inf(-1)
	)
	for i := 1; i <= h.th1.xaxis.nbins && i < len(h.arr.Data); i++ {
		if v := float64(h.arr.Data[i]); v > mx {
			at, mx = i, v
		}
	}
	if math.IsInf(mx, -1) {
		return 0, 0
	}
	return at, mx
}

// Minimum returns the smallest content of the bins proper.
func (h *H1S) Minimum() float64 {
	mn := math.Inf(+1)
	for i := 1; i <= h.th1.xaxis.nbins && i < len(h.arr.Data); i++ {
		if v := float64(h.arr.Data[i]); v < mn {
			mn = v
		}
	}
	if math.IsInf(mn, +1) {
		return 0
	}
	return mn
}

// Mean returns the mean of the entries, from the running sums rather than
// from the bins, so it is the mean of what was filled and not of where the
// bins are.
func (h *H1S) Mean() float64 {
	if h.th1.tsumw == 0 {
		return 0
	}
	return h.th1.tsumwx / h.th1.tsumw
}

// StdDev returns the standard deviation of the entries.
func (h *H1S) StdDev() float64 {
	if h.th1.tsumw == 0 {
		return 0
	}
	var (
		m = h.Mean()
		v = h.th1.tsumwx2/h.th1.tsumw - m*m
	)
	if v <= 0 {
		return 0
	}
	return math.Sqrt(v)
}

func (*H1S) RVersion() int16 {
	return rvers.H1S
}

func (*H1S) isH1() {}

// Class returns the ROOT class name.
func (*H1S) Class() string {
	return "TH1S"
}

func (h *H1S) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(h.Class(), h.RVersion())

	w.WriteObject(&h.th1)
	w.WriteObject(&h.arr)

	return w.SetHeader(hdr)
}

func (h *H1S) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(h.Class(), h.RVersion())

	r.ReadObject(&h.th1)
	r.ReadObject(&h.arr)

	r.CheckHeader(hdr)
	return r.Err()
}

func (h *H1S) RMembers() (mbrs []rbytes.Member) {
	mbrs = append(mbrs, h.th1.RMembers()...)
	mbrs = append(mbrs, rbytes.Member{
		Name: "fArray", Value: &h.arr.Data,
	})
	return mbrs
}

func (h *H1S) Array() rcont.ArrayS {
	return h.arr
}

// Rank returns the number of dimensions of this histogram.
func (h *H1S) Rank() int {
	return 1
}

// NbinsX returns the number of bins in X.
func (h *H1S) NbinsX() int {
	return h.th1.xaxis.nbins
}

// XAxis returns the axis along X.
func (h *H1S) XAxis() Axis {
	return &h.th1.xaxis
}

// bin returns the regularized bin number given an x bin pair.
func (h *H1S) bin(ix int) int {
	nx := h.th1.xaxis.nbins + 1 // overflow bin
	switch {
	case ix < 0:
		ix = 0
	case ix > nx:
		ix = nx
	}
	return ix
}

// XBinCenter returns the bin center value in X.
func (h *H1S) XBinCenter(i int) float64 {
	return float64(h.th1.xaxis.BinCenter(i))
}

// XBinContent returns the bin content value in X.
func (h *H1S) XBinContent(i int) float64 {
	ibin := h.bin(i)
	return float64(h.arr.Data[ibin])
}

// XBinError returns the bin error in X.
func (h *H1S) XBinError(i int) float64 {
	ibin := h.bin(i)
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[ibin]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[ibin])))
}

// XBinLowEdge returns the bin lower edge value in X.
func (h *H1S) XBinLowEdge(i int) float64 {
	return h.th1.xaxis.BinLowEdge(i)
}

// XBinWidth returns the bin width in X.
func (h *H1S) XBinWidth(i int) float64 {
	return h.th1.xaxis.BinWidth(i)
}

func (h *H1S) dist1D(i int) hbook.Dist1D {
	v := h.XBinContent(i)
	err := h.XBinError(i)
	n := h.entries(v, err)
	sumw := h.arr.Data[i]
	sumw2 := 0.0
	if len(h.th1.sumw2.Data) > 0 {
		sumw2 = h.th1.sumw2.Data[i]
	}
	return hbook.Dist1D{
		Dist: hbook.Dist0D{
			N:     n,
			SumW:  float64(sumw),
			SumW2: float64(sumw2),
		},
	}
}

func (h *H1S) setDist1D(i int, sumw, sumw2 float64) {
	h.arr.Data[i] = int16(sumw)
	h.th1.sumw2.Data[i] = sumw2
}

func (h *H1S) entries(height, err float64) int64 {
	if height <= 0 {
		return 0
	}
	v := height / err
	return int64(v*v + 0.5)
}

// AsH1D creates a new hbook.H1D from this ROOT histogram.
func (h *H1S) AsH1D() *hbook.H1D {
	var (
		nx = h.NbinsX()
		hh = hbook.NewH1D(int(nx), h.XAxis().XMin(), h.XAxis().XMax())
	)
	hh.Ann = hbook.Annotation{
		"name":  h.Name(),
		"title": h.Title(),
	}

	hh.Binning.Dist = hbook.Dist1D{
		Dist: hbook.Dist0D{
			N:     int64(h.Entries()),
			SumW:  float64(h.SumW()),
			SumW2: float64(h.SumW2()),
		},
	}
	hh.Binning.Dist.Stats.SumWX = float64(h.SumWX())
	hh.Binning.Dist.Stats.SumWX2 = float64(h.SumWX2())

	hh.Binning.Outflows = [2]hbook.Dist1D{
		h.dist1D(0),      // underflow
		h.dist1D(nx + 1), // overflow
	}

	for i := range nx {
		bin := &hh.Binning.Bins[i]
		xmin := h.XBinLowEdge(i + 1)
		xmax := h.XBinWidth(i+1) + xmin
		bin.Dist = h.dist1D(i + 1)
		bin.Range.Min = xmin
		bin.Range.Max = xmax
		hh.Binning.Bins[i].Dist = h.dist1D(i + 1)
	}

	return hh
}

// MarshalYODA implements the YODAMarshaler interface.
func (h *H1S) MarshalYODA() ([]byte, error) {
	return h.AsH1D().MarshalYODA()
}

// UnmarshalYODA implements the YODAUnmarshaler interface.
func (h *H1S) UnmarshalYODA(raw []byte) error {
	var hh hbook.H1D
	err := hh.UnmarshalYODA(raw)
	if err != nil {
		return err
	}

	*h = *NewH1SFrom(&hh)
	return nil
}

func (h *H1S) ROOTMerge(src root.Object) error {
	hsrc, ok := src.(*H1S)
	if !ok {
		return fmt.Errorf("rhist: object %q is not a *rhist.H1S (%T)", src.(root.Named).Name(), src)
	}

	var (
		h1   = h.AsH1D()
		h2   = hsrc.AsH1D()
		hadd = hbook.AddH1D(h1, h2)
	)

	*h = *NewH1SFrom(hadd)
	return nil
}

func init() {
	f := func() reflect.Value {
		o := newH1S()
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TH1S", f)
}

var (
	_ root.Object        = (*H1S)(nil)
	_ root.Merger        = (*H1S)(nil)
	_ root.Named         = (*H1S)(nil)
	_ H1                 = (*H1S)(nil)
	_ rbytes.Marshaler   = (*H1S)(nil)
	_ rbytes.Unmarshaler = (*H1S)(nil)
	_ rbytes.RSlicer     = (*H1S)(nil)
)

// H1F implements ROOT TH1F
type H1F struct {
	th1
	arr rcont.ArrayF
}

func newH1F() *H1F {
	return &H1F{
		th1: *newH1(),
	}
}

// NewH1FFrom creates a new 1-dim histogram from hbook.
func NewH1FFrom(h *hbook.H1D) *H1F {
	var (
		hroot = newH1F()
		bins  = h.Binning.Bins
		nbins = len(bins)
		edges = make([]float64, 0, nbins+1)
		uflow = h.Binning.Underflow()
		oflow = h.Binning.Overflow()
	)

	hroot.th1.entries = float64(h.Entries())
	hroot.th1.tsumw = h.SumW()
	hroot.th1.tsumw2 = h.SumW2()
	hroot.th1.tsumwx = h.SumWX()
	hroot.th1.tsumwx2 = h.SumWX2()
	hroot.th1.ncells = nbins + 2

	hroot.th1.xaxis.nbins = nbins
	hroot.th1.xaxis.xmin = h.XMin()
	hroot.th1.xaxis.xmax = h.XMax()

	hroot.arr.Data = make([]float32, nbins+2)
	hroot.th1.sumw2.Data = make([]float64, nbins+2)

	for i, bin := range bins {
		if i == 0 {
			edges = append(edges, bin.XMin())
		}
		edges = append(edges, bin.XMax())
		hroot.setDist1D(i+1, bin.Dist.SumW(), bin.Dist.SumW2())
	}
	hroot.setDist1D(0, uflow.SumW(), uflow.SumW2())
	hroot.setDist1D(nbins+1, oflow.SumW(), oflow.SumW2())

	hroot.th1.SetName(h.Name())
	if v, ok := h.Annotation()["title"]; ok && v != nil {
		hroot.th1.SetTitle(v.(string))
	}
	hroot.th1.xaxis.xbins.Data = edges
	return hroot
}

// NewH1F creates a 1-dim histogram with n equal bins between xmin and
// xmax, as "new TH1F(name, title, n, xmin, xmax)" does in C++.
func NewH1F(name, title string, n int, xmin, xmax float64) *H1F {
	h := newH1F()
	h.th1.SetName(name)
	h.th1.SetTitle(title)
	h.th1.xaxis.setRange(n, xmin, xmax)
	h.reset()
	return h
}

// NewH1FFromEdges creates a 1-dim histogram whose bins are the ones the
// edges describe, for a binning that is not uniform.
func NewH1FFromEdges(name, title string, edges []float64) *H1F {
	h := newH1F()
	h.th1.SetName(name)
	h.th1.SetTitle(title)
	h.th1.xaxis.setEdges(edges)
	h.reset()
	return h
}

// reset sizes the cells to the axis and empties them.
func (h *H1F) reset() {
	n := h.th1.xaxis.nbins + 2 // and the under- and overflow
	h.th1.ncells = n
	h.arr.Data = make([]float32, n)
	h.th1.sumw2.Data = make([]float64, n)
	h.th1.entries = 0
	h.th1.tsumw = 0
	h.th1.tsumw2 = 0
	h.th1.tsumwx = 0
	h.th1.tsumwx2 = 0
}

// Reset empties the histogram, keeping its binning.
func (h *H1F) Reset() { h.reset() }

// FindBin returns the bin x falls in: 0 for the underflow, 1 to NbinsX for
// the bins proper, NbinsX+1 for the overflow.
func (h *H1F) FindBin(x float64) int {
	return h.th1.xaxis.FindBin(x)
}

// Fill adds an entry of weight w at x.
func (h *H1F) Fill(x, w float64) {
	i := h.FindBin(x)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}

	h.arr.Data[i] += float32(w)
	if len(h.th1.sumw2.Data) > i {
		h.th1.sumw2.Data[i] += w * w
	}

	h.th1.entries++

	// The sums over the whole histogram, which is where the mean and the
	// width come from. ROOT leaves out what fell outside the axis, since a
	// mean of the overflow is a mean of nothing in particular.
	if i > 0 && i <= h.th1.xaxis.nbins {
		h.th1.tsumw += w
		h.th1.tsumw2 += w * w
		h.th1.tsumwx += w * x
		h.th1.tsumwx2 += w * x * x
	}
}

// FillN adds an entry for each x with the matching weight, or of weight one
// when ws is nil.
//
// FillN panics if the slices are of different lengths.
func (h *H1F) FillN(xs, ws []float64) {
	if ws != nil && len(ws) != len(xs) {
		panic(fmt.Errorf("rhist: %d values and %d weights", len(xs), len(ws)))
	}
	for i, x := range xs {
		w := 1.0
		if ws != nil {
			w = ws[i]
		}
		h.Fill(x, w)
	}
}

// BinContent returns the content of the i-th cell, counting the underflow as
// zero and the overflow as NbinsX+1.
func (h *H1F) BinContent(i int) float64 {
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	return float64(h.arr.Data[i])
}

// SetBinContent sets the content of the i-th cell.
func (h *H1F) SetBinContent(i int, v float64) {
	if i < 0 || i >= len(h.arr.Data) {
		return
	}
	h.arr.Data[i] = float32(v)
}

// BinError returns the uncertainty on the i-th cell: the square root of its
// sum of squared weights, or of its content where no such sum is kept.
func (h *H1F) BinError(i int) float64 {
	return h.XBinError(i)
}

// SetBinError sets the uncertainty on the i-th cell.
func (h *H1F) SetBinError(i int, e float64) {
	if i < 0 || i >= len(h.th1.sumw2.Data) {
		return
	}
	h.th1.sumw2.Data[i] = e * e
}

// Scale multiplies every cell by f, and the uncertainties with them.
func (h *H1F) Scale(f float64) {
	for i := range h.arr.Data {
		h.arr.Data[i] = float32(float64(h.arr.Data[i]) * f)
	}
	for i := range h.th1.sumw2.Data {
		h.th1.sumw2.Data[i] *= f * f
	}
	h.th1.tsumw *= f
	h.th1.tsumw2 *= f * f
	h.th1.tsumwx *= f
	h.th1.tsumwx2 *= f
}

// Integral returns the sum of the bins proper, leaving out the under- and
// overflow.
func (h *H1F) Integral() float64 {
	return h.IntegralRange(1, h.th1.xaxis.nbins)
}

// IntegralRange returns the sum of the cells from lo to hi, both included.
func (h *H1F) IntegralRange(lo, hi int) float64 {
	var sum float64
	for i := max(0, lo); i <= min(hi, len(h.arr.Data)-1); i++ {
		sum += float64(h.arr.Data[i])
	}
	return sum
}

// Maximum returns the largest content of the bins proper, and MaximumBin
// which bin holds it.
func (h *H1F) Maximum() float64 {
	_, v := h.maxBin()
	return v
}

// MaximumBin returns the bin holding the largest content.
func (h *H1F) MaximumBin() int {
	i, _ := h.maxBin()
	return i
}

func (h *H1F) maxBin() (int, float64) {
	var (
		at = 0
		mx = math.Inf(-1)
	)
	for i := 1; i <= h.th1.xaxis.nbins && i < len(h.arr.Data); i++ {
		if v := float64(h.arr.Data[i]); v > mx {
			at, mx = i, v
		}
	}
	if math.IsInf(mx, -1) {
		return 0, 0
	}
	return at, mx
}

// Minimum returns the smallest content of the bins proper.
func (h *H1F) Minimum() float64 {
	mn := math.Inf(+1)
	for i := 1; i <= h.th1.xaxis.nbins && i < len(h.arr.Data); i++ {
		if v := float64(h.arr.Data[i]); v < mn {
			mn = v
		}
	}
	if math.IsInf(mn, +1) {
		return 0
	}
	return mn
}

// Mean returns the mean of the entries, from the running sums rather than
// from the bins, so it is the mean of what was filled and not of where the
// bins are.
func (h *H1F) Mean() float64 {
	if h.th1.tsumw == 0 {
		return 0
	}
	return h.th1.tsumwx / h.th1.tsumw
}

// StdDev returns the standard deviation of the entries.
func (h *H1F) StdDev() float64 {
	if h.th1.tsumw == 0 {
		return 0
	}
	var (
		m = h.Mean()
		v = h.th1.tsumwx2/h.th1.tsumw - m*m
	)
	if v <= 0 {
		return 0
	}
	return math.Sqrt(v)
}

func (*H1F) RVersion() int16 {
	return rvers.H1F
}

func (*H1F) isH1() {}

// Class returns the ROOT class name.
func (*H1F) Class() string {
	return "TH1F"
}

func (h *H1F) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(h.Class(), h.RVersion())

	w.WriteObject(&h.th1)
	w.WriteObject(&h.arr)

	return w.SetHeader(hdr)
}

func (h *H1F) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(h.Class(), h.RVersion())

	r.ReadObject(&h.th1)
	r.ReadObject(&h.arr)

	r.CheckHeader(hdr)
	return r.Err()
}

func (h *H1F) RMembers() (mbrs []rbytes.Member) {
	mbrs = append(mbrs, h.th1.RMembers()...)
	mbrs = append(mbrs, rbytes.Member{
		Name: "fArray", Value: &h.arr.Data,
	})
	return mbrs
}

func (h *H1F) Array() rcont.ArrayF {
	return h.arr
}

// Rank returns the number of dimensions of this histogram.
func (h *H1F) Rank() int {
	return 1
}

// NbinsX returns the number of bins in X.
func (h *H1F) NbinsX() int {
	return h.th1.xaxis.nbins
}

// XAxis returns the axis along X.
func (h *H1F) XAxis() Axis {
	return &h.th1.xaxis
}

// bin returns the regularized bin number given an x bin pair.
func (h *H1F) bin(ix int) int {
	nx := h.th1.xaxis.nbins + 1 // overflow bin
	switch {
	case ix < 0:
		ix = 0
	case ix > nx:
		ix = nx
	}
	return ix
}

// XBinCenter returns the bin center value in X.
func (h *H1F) XBinCenter(i int) float64 {
	return float64(h.th1.xaxis.BinCenter(i))
}

// XBinContent returns the bin content value in X.
func (h *H1F) XBinContent(i int) float64 {
	ibin := h.bin(i)
	return float64(h.arr.Data[ibin])
}

// XBinError returns the bin error in X.
func (h *H1F) XBinError(i int) float64 {
	ibin := h.bin(i)
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[ibin]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[ibin])))
}

// XBinLowEdge returns the bin lower edge value in X.
func (h *H1F) XBinLowEdge(i int) float64 {
	return h.th1.xaxis.BinLowEdge(i)
}

// XBinWidth returns the bin width in X.
func (h *H1F) XBinWidth(i int) float64 {
	return h.th1.xaxis.BinWidth(i)
}

func (h *H1F) dist1D(i int) hbook.Dist1D {
	v := h.XBinContent(i)
	err := h.XBinError(i)
	n := h.entries(v, err)
	sumw := h.arr.Data[i]
	sumw2 := 0.0
	if len(h.th1.sumw2.Data) > 0 {
		sumw2 = h.th1.sumw2.Data[i]
	}
	return hbook.Dist1D{
		Dist: hbook.Dist0D{
			N:     n,
			SumW:  float64(sumw),
			SumW2: float64(sumw2),
		},
	}
}

func (h *H1F) setDist1D(i int, sumw, sumw2 float64) {
	h.arr.Data[i] = float32(sumw)
	h.th1.sumw2.Data[i] = sumw2
}

func (h *H1F) entries(height, err float64) int64 {
	if height <= 0 {
		return 0
	}
	v := height / err
	return int64(v*v + 0.5)
}

// AsH1D creates a new hbook.H1D from this ROOT histogram.
func (h *H1F) AsH1D() *hbook.H1D {
	var (
		nx = h.NbinsX()
		hh = hbook.NewH1D(int(nx), h.XAxis().XMin(), h.XAxis().XMax())
	)
	hh.Ann = hbook.Annotation{
		"name":  h.Name(),
		"title": h.Title(),
	}

	hh.Binning.Dist = hbook.Dist1D{
		Dist: hbook.Dist0D{
			N:     int64(h.Entries()),
			SumW:  float64(h.SumW()),
			SumW2: float64(h.SumW2()),
		},
	}
	hh.Binning.Dist.Stats.SumWX = float64(h.SumWX())
	hh.Binning.Dist.Stats.SumWX2 = float64(h.SumWX2())

	hh.Binning.Outflows = [2]hbook.Dist1D{
		h.dist1D(0),      // underflow
		h.dist1D(nx + 1), // overflow
	}

	for i := range nx {
		bin := &hh.Binning.Bins[i]
		xmin := h.XBinLowEdge(i + 1)
		xmax := h.XBinWidth(i+1) + xmin
		bin.Dist = h.dist1D(i + 1)
		bin.Range.Min = xmin
		bin.Range.Max = xmax
		hh.Binning.Bins[i].Dist = h.dist1D(i + 1)
	}

	return hh
}

// MarshalYODA implements the YODAMarshaler interface.
func (h *H1F) MarshalYODA() ([]byte, error) {
	return h.AsH1D().MarshalYODA()
}

// UnmarshalYODA implements the YODAUnmarshaler interface.
func (h *H1F) UnmarshalYODA(raw []byte) error {
	var hh hbook.H1D
	err := hh.UnmarshalYODA(raw)
	if err != nil {
		return err
	}

	*h = *NewH1FFrom(&hh)
	return nil
}

func (h *H1F) ROOTMerge(src root.Object) error {
	hsrc, ok := src.(*H1F)
	if !ok {
		return fmt.Errorf("rhist: object %q is not a *rhist.H1F (%T)", src.(root.Named).Name(), src)
	}

	var (
		h1   = h.AsH1D()
		h2   = hsrc.AsH1D()
		hadd = hbook.AddH1D(h1, h2)
	)

	*h = *NewH1FFrom(hadd)
	return nil
}

func init() {
	f := func() reflect.Value {
		o := newH1F()
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TH1F", f)
}

var (
	_ root.Object        = (*H1F)(nil)
	_ root.Merger        = (*H1F)(nil)
	_ root.Named         = (*H1F)(nil)
	_ H1                 = (*H1F)(nil)
	_ rbytes.Marshaler   = (*H1F)(nil)
	_ rbytes.Unmarshaler = (*H1F)(nil)
	_ rbytes.RSlicer     = (*H1F)(nil)
)

// H1D implements ROOT TH1D
type H1D struct {
	th1
	arr rcont.ArrayD
}

func newH1D() *H1D {
	return &H1D{
		th1: *newH1(),
	}
}

// NewH1DFrom creates a new 1-dim histogram from hbook.
func NewH1DFrom(h *hbook.H1D) *H1D {
	var (
		hroot = newH1D()
		bins  = h.Binning.Bins
		nbins = len(bins)
		edges = make([]float64, 0, nbins+1)
		uflow = h.Binning.Underflow()
		oflow = h.Binning.Overflow()
	)

	hroot.th1.entries = float64(h.Entries())
	hroot.th1.tsumw = h.SumW()
	hroot.th1.tsumw2 = h.SumW2()
	hroot.th1.tsumwx = h.SumWX()
	hroot.th1.tsumwx2 = h.SumWX2()
	hroot.th1.ncells = nbins + 2

	hroot.th1.xaxis.nbins = nbins
	hroot.th1.xaxis.xmin = h.XMin()
	hroot.th1.xaxis.xmax = h.XMax()

	hroot.arr.Data = make([]float64, nbins+2)
	hroot.th1.sumw2.Data = make([]float64, nbins+2)

	for i, bin := range bins {
		if i == 0 {
			edges = append(edges, bin.XMin())
		}
		edges = append(edges, bin.XMax())
		hroot.setDist1D(i+1, bin.Dist.SumW(), bin.Dist.SumW2())
	}
	hroot.setDist1D(0, uflow.SumW(), uflow.SumW2())
	hroot.setDist1D(nbins+1, oflow.SumW(), oflow.SumW2())

	hroot.th1.SetName(h.Name())
	if v, ok := h.Annotation()["title"]; ok && v != nil {
		hroot.th1.SetTitle(v.(string))
	}
	hroot.th1.xaxis.xbins.Data = edges
	return hroot
}

// NewH1D creates a 1-dim histogram with n equal bins between xmin and
// xmax, as "new TH1D(name, title, n, xmin, xmax)" does in C++.
func NewH1D(name, title string, n int, xmin, xmax float64) *H1D {
	h := newH1D()
	h.th1.SetName(name)
	h.th1.SetTitle(title)
	h.th1.xaxis.setRange(n, xmin, xmax)
	h.reset()
	return h
}

// NewH1DFromEdges creates a 1-dim histogram whose bins are the ones the
// edges describe, for a binning that is not uniform.
func NewH1DFromEdges(name, title string, edges []float64) *H1D {
	h := newH1D()
	h.th1.SetName(name)
	h.th1.SetTitle(title)
	h.th1.xaxis.setEdges(edges)
	h.reset()
	return h
}

// reset sizes the cells to the axis and empties them.
func (h *H1D) reset() {
	n := h.th1.xaxis.nbins + 2 // and the under- and overflow
	h.th1.ncells = n
	h.arr.Data = make([]float64, n)
	h.th1.sumw2.Data = make([]float64, n)
	h.th1.entries = 0
	h.th1.tsumw = 0
	h.th1.tsumw2 = 0
	h.th1.tsumwx = 0
	h.th1.tsumwx2 = 0
}

// Reset empties the histogram, keeping its binning.
func (h *H1D) Reset() { h.reset() }

// FindBin returns the bin x falls in: 0 for the underflow, 1 to NbinsX for
// the bins proper, NbinsX+1 for the overflow.
func (h *H1D) FindBin(x float64) int {
	return h.th1.xaxis.FindBin(x)
}

// Fill adds an entry of weight w at x.
func (h *H1D) Fill(x, w float64) {
	i := h.FindBin(x)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}

	h.arr.Data[i] += float64(w)
	if len(h.th1.sumw2.Data) > i {
		h.th1.sumw2.Data[i] += w * w
	}

	h.th1.entries++

	// The sums over the whole histogram, which is where the mean and the
	// width come from. ROOT leaves out what fell outside the axis, since a
	// mean of the overflow is a mean of nothing in particular.
	if i > 0 && i <= h.th1.xaxis.nbins {
		h.th1.tsumw += w
		h.th1.tsumw2 += w * w
		h.th1.tsumwx += w * x
		h.th1.tsumwx2 += w * x * x
	}
}

// FillN adds an entry for each x with the matching weight, or of weight one
// when ws is nil.
//
// FillN panics if the slices are of different lengths.
func (h *H1D) FillN(xs, ws []float64) {
	if ws != nil && len(ws) != len(xs) {
		panic(fmt.Errorf("rhist: %d values and %d weights", len(xs), len(ws)))
	}
	for i, x := range xs {
		w := 1.0
		if ws != nil {
			w = ws[i]
		}
		h.Fill(x, w)
	}
}

// BinContent returns the content of the i-th cell, counting the underflow as
// zero and the overflow as NbinsX+1.
func (h *H1D) BinContent(i int) float64 {
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	return float64(h.arr.Data[i])
}

// SetBinContent sets the content of the i-th cell.
func (h *H1D) SetBinContent(i int, v float64) {
	if i < 0 || i >= len(h.arr.Data) {
		return
	}
	h.arr.Data[i] = float64(v)
}

// BinError returns the uncertainty on the i-th cell: the square root of its
// sum of squared weights, or of its content where no such sum is kept.
func (h *H1D) BinError(i int) float64 {
	return h.XBinError(i)
}

// SetBinError sets the uncertainty on the i-th cell.
func (h *H1D) SetBinError(i int, e float64) {
	if i < 0 || i >= len(h.th1.sumw2.Data) {
		return
	}
	h.th1.sumw2.Data[i] = e * e
}

// Scale multiplies every cell by f, and the uncertainties with them.
func (h *H1D) Scale(f float64) {
	for i := range h.arr.Data {
		h.arr.Data[i] = float64(float64(h.arr.Data[i]) * f)
	}
	for i := range h.th1.sumw2.Data {
		h.th1.sumw2.Data[i] *= f * f
	}
	h.th1.tsumw *= f
	h.th1.tsumw2 *= f * f
	h.th1.tsumwx *= f
	h.th1.tsumwx2 *= f
}

// Integral returns the sum of the bins proper, leaving out the under- and
// overflow.
func (h *H1D) Integral() float64 {
	return h.IntegralRange(1, h.th1.xaxis.nbins)
}

// IntegralRange returns the sum of the cells from lo to hi, both included.
func (h *H1D) IntegralRange(lo, hi int) float64 {
	var sum float64
	for i := max(0, lo); i <= min(hi, len(h.arr.Data)-1); i++ {
		sum += float64(h.arr.Data[i])
	}
	return sum
}

// Maximum returns the largest content of the bins proper, and MaximumBin
// which bin holds it.
func (h *H1D) Maximum() float64 {
	_, v := h.maxBin()
	return v
}

// MaximumBin returns the bin holding the largest content.
func (h *H1D) MaximumBin() int {
	i, _ := h.maxBin()
	return i
}

func (h *H1D) maxBin() (int, float64) {
	var (
		at = 0
		mx = math.Inf(-1)
	)
	for i := 1; i <= h.th1.xaxis.nbins && i < len(h.arr.Data); i++ {
		if v := float64(h.arr.Data[i]); v > mx {
			at, mx = i, v
		}
	}
	if math.IsInf(mx, -1) {
		return 0, 0
	}
	return at, mx
}

// Minimum returns the smallest content of the bins proper.
func (h *H1D) Minimum() float64 {
	mn := math.Inf(+1)
	for i := 1; i <= h.th1.xaxis.nbins && i < len(h.arr.Data); i++ {
		if v := float64(h.arr.Data[i]); v < mn {
			mn = v
		}
	}
	if math.IsInf(mn, +1) {
		return 0
	}
	return mn
}

// Mean returns the mean of the entries, from the running sums rather than
// from the bins, so it is the mean of what was filled and not of where the
// bins are.
func (h *H1D) Mean() float64 {
	if h.th1.tsumw == 0 {
		return 0
	}
	return h.th1.tsumwx / h.th1.tsumw
}

// StdDev returns the standard deviation of the entries.
func (h *H1D) StdDev() float64 {
	if h.th1.tsumw == 0 {
		return 0
	}
	var (
		m = h.Mean()
		v = h.th1.tsumwx2/h.th1.tsumw - m*m
	)
	if v <= 0 {
		return 0
	}
	return math.Sqrt(v)
}

func (*H1D) RVersion() int16 {
	return rvers.H1D
}

func (*H1D) isH1() {}

// Class returns the ROOT class name.
func (*H1D) Class() string {
	return "TH1D"
}

func (h *H1D) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(h.Class(), h.RVersion())

	w.WriteObject(&h.th1)
	w.WriteObject(&h.arr)

	return w.SetHeader(hdr)
}

func (h *H1D) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(h.Class(), h.RVersion())

	r.ReadObject(&h.th1)
	r.ReadObject(&h.arr)

	r.CheckHeader(hdr)
	return r.Err()
}

func (h *H1D) RMembers() (mbrs []rbytes.Member) {
	mbrs = append(mbrs, h.th1.RMembers()...)
	mbrs = append(mbrs, rbytes.Member{
		Name: "fArray", Value: &h.arr.Data,
	})
	return mbrs
}

func (h *H1D) Array() rcont.ArrayD {
	return h.arr
}

// Rank returns the number of dimensions of this histogram.
func (h *H1D) Rank() int {
	return 1
}

// NbinsX returns the number of bins in X.
func (h *H1D) NbinsX() int {
	return h.th1.xaxis.nbins
}

// XAxis returns the axis along X.
func (h *H1D) XAxis() Axis {
	return &h.th1.xaxis
}

// bin returns the regularized bin number given an x bin pair.
func (h *H1D) bin(ix int) int {
	nx := h.th1.xaxis.nbins + 1 // overflow bin
	switch {
	case ix < 0:
		ix = 0
	case ix > nx:
		ix = nx
	}
	return ix
}

// XBinCenter returns the bin center value in X.
func (h *H1D) XBinCenter(i int) float64 {
	return float64(h.th1.xaxis.BinCenter(i))
}

// XBinContent returns the bin content value in X.
func (h *H1D) XBinContent(i int) float64 {
	ibin := h.bin(i)
	return float64(h.arr.Data[ibin])
}

// XBinError returns the bin error in X.
func (h *H1D) XBinError(i int) float64 {
	ibin := h.bin(i)
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[ibin]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[ibin])))
}

// XBinLowEdge returns the bin lower edge value in X.
func (h *H1D) XBinLowEdge(i int) float64 {
	return h.th1.xaxis.BinLowEdge(i)
}

// XBinWidth returns the bin width in X.
func (h *H1D) XBinWidth(i int) float64 {
	return h.th1.xaxis.BinWidth(i)
}

func (h *H1D) dist1D(i int) hbook.Dist1D {
	v := h.XBinContent(i)
	err := h.XBinError(i)
	n := h.entries(v, err)
	sumw := h.arr.Data[i]
	sumw2 := 0.0
	if len(h.th1.sumw2.Data) > 0 {
		sumw2 = h.th1.sumw2.Data[i]
	}
	return hbook.Dist1D{
		Dist: hbook.Dist0D{
			N:     n,
			SumW:  float64(sumw),
			SumW2: float64(sumw2),
		},
	}
}

func (h *H1D) setDist1D(i int, sumw, sumw2 float64) {
	h.arr.Data[i] = float64(sumw)
	h.th1.sumw2.Data[i] = sumw2
}

func (h *H1D) entries(height, err float64) int64 {
	if height <= 0 {
		return 0
	}
	v := height / err
	return int64(v*v + 0.5)
}

// AsH1D creates a new hbook.H1D from this ROOT histogram.
func (h *H1D) AsH1D() *hbook.H1D {
	var (
		nx = h.NbinsX()
		hh = hbook.NewH1D(int(nx), h.XAxis().XMin(), h.XAxis().XMax())
	)
	hh.Ann = hbook.Annotation{
		"name":  h.Name(),
		"title": h.Title(),
	}

	hh.Binning.Dist = hbook.Dist1D{
		Dist: hbook.Dist0D{
			N:     int64(h.Entries()),
			SumW:  float64(h.SumW()),
			SumW2: float64(h.SumW2()),
		},
	}
	hh.Binning.Dist.Stats.SumWX = float64(h.SumWX())
	hh.Binning.Dist.Stats.SumWX2 = float64(h.SumWX2())

	hh.Binning.Outflows = [2]hbook.Dist1D{
		h.dist1D(0),      // underflow
		h.dist1D(nx + 1), // overflow
	}

	for i := range nx {
		bin := &hh.Binning.Bins[i]
		xmin := h.XBinLowEdge(i + 1)
		xmax := h.XBinWidth(i+1) + xmin
		bin.Dist = h.dist1D(i + 1)
		bin.Range.Min = xmin
		bin.Range.Max = xmax
		hh.Binning.Bins[i].Dist = h.dist1D(i + 1)
	}

	return hh
}

// MarshalYODA implements the YODAMarshaler interface.
func (h *H1D) MarshalYODA() ([]byte, error) {
	return h.AsH1D().MarshalYODA()
}

// UnmarshalYODA implements the YODAUnmarshaler interface.
func (h *H1D) UnmarshalYODA(raw []byte) error {
	var hh hbook.H1D
	err := hh.UnmarshalYODA(raw)
	if err != nil {
		return err
	}

	*h = *NewH1DFrom(&hh)
	return nil
}

func (h *H1D) ROOTMerge(src root.Object) error {
	hsrc, ok := src.(*H1D)
	if !ok {
		return fmt.Errorf("rhist: object %q is not a *rhist.H1D (%T)", src.(root.Named).Name(), src)
	}

	var (
		h1   = h.AsH1D()
		h2   = hsrc.AsH1D()
		hadd = hbook.AddH1D(h1, h2)
	)

	*h = *NewH1DFrom(hadd)
	return nil
}

func init() {
	f := func() reflect.Value {
		o := newH1D()
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TH1D", f)
}

var (
	_ root.Object        = (*H1D)(nil)
	_ root.Merger        = (*H1D)(nil)
	_ root.Named         = (*H1D)(nil)
	_ H1                 = (*H1D)(nil)
	_ rbytes.Marshaler   = (*H1D)(nil)
	_ rbytes.Unmarshaler = (*H1D)(nil)
	_ rbytes.RSlicer     = (*H1D)(nil)
)

// H1I implements ROOT TH1I
type H1I struct {
	th1
	arr rcont.ArrayI
}

func newH1I() *H1I {
	return &H1I{
		th1: *newH1(),
	}
}

// NewH1IFrom creates a new 1-dim histogram from hbook.
func NewH1IFrom(h *hbook.H1D) *H1I {
	var (
		hroot = newH1I()
		bins  = h.Binning.Bins
		nbins = len(bins)
		edges = make([]float64, 0, nbins+1)
		uflow = h.Binning.Underflow()
		oflow = h.Binning.Overflow()
	)

	hroot.th1.entries = float64(h.Entries())
	hroot.th1.tsumw = h.SumW()
	hroot.th1.tsumw2 = h.SumW2()
	hroot.th1.tsumwx = h.SumWX()
	hroot.th1.tsumwx2 = h.SumWX2()
	hroot.th1.ncells = nbins + 2

	hroot.th1.xaxis.nbins = nbins
	hroot.th1.xaxis.xmin = h.XMin()
	hroot.th1.xaxis.xmax = h.XMax()

	hroot.arr.Data = make([]int32, nbins+2)
	hroot.th1.sumw2.Data = make([]float64, nbins+2)

	for i, bin := range bins {
		if i == 0 {
			edges = append(edges, bin.XMin())
		}
		edges = append(edges, bin.XMax())
		hroot.setDist1D(i+1, bin.Dist.SumW(), bin.Dist.SumW2())
	}
	hroot.setDist1D(0, uflow.SumW(), uflow.SumW2())
	hroot.setDist1D(nbins+1, oflow.SumW(), oflow.SumW2())

	hroot.th1.SetName(h.Name())
	if v, ok := h.Annotation()["title"]; ok && v != nil {
		hroot.th1.SetTitle(v.(string))
	}
	hroot.th1.xaxis.xbins.Data = edges
	return hroot
}

// NewH1I creates a 1-dim histogram with n equal bins between xmin and
// xmax, as "new TH1I(name, title, n, xmin, xmax)" does in C++.
func NewH1I(name, title string, n int, xmin, xmax float64) *H1I {
	h := newH1I()
	h.th1.SetName(name)
	h.th1.SetTitle(title)
	h.th1.xaxis.setRange(n, xmin, xmax)
	h.reset()
	return h
}

// NewH1IFromEdges creates a 1-dim histogram whose bins are the ones the
// edges describe, for a binning that is not uniform.
func NewH1IFromEdges(name, title string, edges []float64) *H1I {
	h := newH1I()
	h.th1.SetName(name)
	h.th1.SetTitle(title)
	h.th1.xaxis.setEdges(edges)
	h.reset()
	return h
}

// reset sizes the cells to the axis and empties them.
func (h *H1I) reset() {
	n := h.th1.xaxis.nbins + 2 // and the under- and overflow
	h.th1.ncells = n
	h.arr.Data = make([]int32, n)
	h.th1.sumw2.Data = make([]float64, n)
	h.th1.entries = 0
	h.th1.tsumw = 0
	h.th1.tsumw2 = 0
	h.th1.tsumwx = 0
	h.th1.tsumwx2 = 0
}

// Reset empties the histogram, keeping its binning.
func (h *H1I) Reset() { h.reset() }

// FindBin returns the bin x falls in: 0 for the underflow, 1 to NbinsX for
// the bins proper, NbinsX+1 for the overflow.
func (h *H1I) FindBin(x float64) int {
	return h.th1.xaxis.FindBin(x)
}

// Fill adds an entry of weight w at x.
func (h *H1I) Fill(x, w float64) {
	i := h.FindBin(x)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}

	h.arr.Data[i] += int32(w)
	if len(h.th1.sumw2.Data) > i {
		h.th1.sumw2.Data[i] += w * w
	}

	h.th1.entries++

	// The sums over the whole histogram, which is where the mean and the
	// width come from. ROOT leaves out what fell outside the axis, since a
	// mean of the overflow is a mean of nothing in particular.
	if i > 0 && i <= h.th1.xaxis.nbins {
		h.th1.tsumw += w
		h.th1.tsumw2 += w * w
		h.th1.tsumwx += w * x
		h.th1.tsumwx2 += w * x * x
	}
}

// FillN adds an entry for each x with the matching weight, or of weight one
// when ws is nil.
//
// FillN panics if the slices are of different lengths.
func (h *H1I) FillN(xs, ws []float64) {
	if ws != nil && len(ws) != len(xs) {
		panic(fmt.Errorf("rhist: %d values and %d weights", len(xs), len(ws)))
	}
	for i, x := range xs {
		w := 1.0
		if ws != nil {
			w = ws[i]
		}
		h.Fill(x, w)
	}
}

// BinContent returns the content of the i-th cell, counting the underflow as
// zero and the overflow as NbinsX+1.
func (h *H1I) BinContent(i int) float64 {
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	return float64(h.arr.Data[i])
}

// SetBinContent sets the content of the i-th cell.
func (h *H1I) SetBinContent(i int, v float64) {
	if i < 0 || i >= len(h.arr.Data) {
		return
	}
	h.arr.Data[i] = int32(v)
}

// BinError returns the uncertainty on the i-th cell: the square root of its
// sum of squared weights, or of its content where no such sum is kept.
func (h *H1I) BinError(i int) float64 {
	return h.XBinError(i)
}

// SetBinError sets the uncertainty on the i-th cell.
func (h *H1I) SetBinError(i int, e float64) {
	if i < 0 || i >= len(h.th1.sumw2.Data) {
		return
	}
	h.th1.sumw2.Data[i] = e * e
}

// Scale multiplies every cell by f, and the uncertainties with them.
func (h *H1I) Scale(f float64) {
	for i := range h.arr.Data {
		h.arr.Data[i] = int32(float64(h.arr.Data[i]) * f)
	}
	for i := range h.th1.sumw2.Data {
		h.th1.sumw2.Data[i] *= f * f
	}
	h.th1.tsumw *= f
	h.th1.tsumw2 *= f * f
	h.th1.tsumwx *= f
	h.th1.tsumwx2 *= f
}

// Integral returns the sum of the bins proper, leaving out the under- and
// overflow.
func (h *H1I) Integral() float64 {
	return h.IntegralRange(1, h.th1.xaxis.nbins)
}

// IntegralRange returns the sum of the cells from lo to hi, both included.
func (h *H1I) IntegralRange(lo, hi int) float64 {
	var sum float64
	for i := max(0, lo); i <= min(hi, len(h.arr.Data)-1); i++ {
		sum += float64(h.arr.Data[i])
	}
	return sum
}

// Maximum returns the largest content of the bins proper, and MaximumBin
// which bin holds it.
func (h *H1I) Maximum() float64 {
	_, v := h.maxBin()
	return v
}

// MaximumBin returns the bin holding the largest content.
func (h *H1I) MaximumBin() int {
	i, _ := h.maxBin()
	return i
}

func (h *H1I) maxBin() (int, float64) {
	var (
		at = 0
		mx = math.Inf(-1)
	)
	for i := 1; i <= h.th1.xaxis.nbins && i < len(h.arr.Data); i++ {
		if v := float64(h.arr.Data[i]); v > mx {
			at, mx = i, v
		}
	}
	if math.IsInf(mx, -1) {
		return 0, 0
	}
	return at, mx
}

// Minimum returns the smallest content of the bins proper.
func (h *H1I) Minimum() float64 {
	mn := math.Inf(+1)
	for i := 1; i <= h.th1.xaxis.nbins && i < len(h.arr.Data); i++ {
		if v := float64(h.arr.Data[i]); v < mn {
			mn = v
		}
	}
	if math.IsInf(mn, +1) {
		return 0
	}
	return mn
}

// Mean returns the mean of the entries, from the running sums rather than
// from the bins, so it is the mean of what was filled and not of where the
// bins are.
func (h *H1I) Mean() float64 {
	if h.th1.tsumw == 0 {
		return 0
	}
	return h.th1.tsumwx / h.th1.tsumw
}

// StdDev returns the standard deviation of the entries.
func (h *H1I) StdDev() float64 {
	if h.th1.tsumw == 0 {
		return 0
	}
	var (
		m = h.Mean()
		v = h.th1.tsumwx2/h.th1.tsumw - m*m
	)
	if v <= 0 {
		return 0
	}
	return math.Sqrt(v)
}

func (*H1I) RVersion() int16 {
	return rvers.H1I
}

func (*H1I) isH1() {}

// Class returns the ROOT class name.
func (*H1I) Class() string {
	return "TH1I"
}

func (h *H1I) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(h.Class(), h.RVersion())

	w.WriteObject(&h.th1)
	w.WriteObject(&h.arr)

	return w.SetHeader(hdr)
}

func (h *H1I) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(h.Class(), h.RVersion())

	r.ReadObject(&h.th1)
	r.ReadObject(&h.arr)

	r.CheckHeader(hdr)
	return r.Err()
}

func (h *H1I) RMembers() (mbrs []rbytes.Member) {
	mbrs = append(mbrs, h.th1.RMembers()...)
	mbrs = append(mbrs, rbytes.Member{
		Name: "fArray", Value: &h.arr.Data,
	})
	return mbrs
}

func (h *H1I) Array() rcont.ArrayI {
	return h.arr
}

// Rank returns the number of dimensions of this histogram.
func (h *H1I) Rank() int {
	return 1
}

// NbinsX returns the number of bins in X.
func (h *H1I) NbinsX() int {
	return h.th1.xaxis.nbins
}

// XAxis returns the axis along X.
func (h *H1I) XAxis() Axis {
	return &h.th1.xaxis
}

// bin returns the regularized bin number given an x bin pair.
func (h *H1I) bin(ix int) int {
	nx := h.th1.xaxis.nbins + 1 // overflow bin
	switch {
	case ix < 0:
		ix = 0
	case ix > nx:
		ix = nx
	}
	return ix
}

// XBinCenter returns the bin center value in X.
func (h *H1I) XBinCenter(i int) float64 {
	return float64(h.th1.xaxis.BinCenter(i))
}

// XBinContent returns the bin content value in X.
func (h *H1I) XBinContent(i int) float64 {
	ibin := h.bin(i)
	return float64(h.arr.Data[ibin])
}

// XBinError returns the bin error in X.
func (h *H1I) XBinError(i int) float64 {
	ibin := h.bin(i)
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[ibin]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[ibin])))
}

// XBinLowEdge returns the bin lower edge value in X.
func (h *H1I) XBinLowEdge(i int) float64 {
	return h.th1.xaxis.BinLowEdge(i)
}

// XBinWidth returns the bin width in X.
func (h *H1I) XBinWidth(i int) float64 {
	return h.th1.xaxis.BinWidth(i)
}

func (h *H1I) dist1D(i int) hbook.Dist1D {
	v := h.XBinContent(i)
	err := h.XBinError(i)
	n := h.entries(v, err)
	sumw := h.arr.Data[i]
	sumw2 := 0.0
	if len(h.th1.sumw2.Data) > 0 {
		sumw2 = h.th1.sumw2.Data[i]
	}
	return hbook.Dist1D{
		Dist: hbook.Dist0D{
			N:     n,
			SumW:  float64(sumw),
			SumW2: float64(sumw2),
		},
	}
}

func (h *H1I) setDist1D(i int, sumw, sumw2 float64) {
	h.arr.Data[i] = int32(sumw)
	h.th1.sumw2.Data[i] = sumw2
}

func (h *H1I) entries(height, err float64) int64 {
	if height <= 0 {
		return 0
	}
	v := height / err
	return int64(v*v + 0.5)
}

// AsH1D creates a new hbook.H1D from this ROOT histogram.
func (h *H1I) AsH1D() *hbook.H1D {
	var (
		nx = h.NbinsX()
		hh = hbook.NewH1D(int(nx), h.XAxis().XMin(), h.XAxis().XMax())
	)
	hh.Ann = hbook.Annotation{
		"name":  h.Name(),
		"title": h.Title(),
	}

	hh.Binning.Dist = hbook.Dist1D{
		Dist: hbook.Dist0D{
			N:     int64(h.Entries()),
			SumW:  float64(h.SumW()),
			SumW2: float64(h.SumW2()),
		},
	}
	hh.Binning.Dist.Stats.SumWX = float64(h.SumWX())
	hh.Binning.Dist.Stats.SumWX2 = float64(h.SumWX2())

	hh.Binning.Outflows = [2]hbook.Dist1D{
		h.dist1D(0),      // underflow
		h.dist1D(nx + 1), // overflow
	}

	for i := range nx {
		bin := &hh.Binning.Bins[i]
		xmin := h.XBinLowEdge(i + 1)
		xmax := h.XBinWidth(i+1) + xmin
		bin.Dist = h.dist1D(i + 1)
		bin.Range.Min = xmin
		bin.Range.Max = xmax
		hh.Binning.Bins[i].Dist = h.dist1D(i + 1)
	}

	return hh
}

// MarshalYODA implements the YODAMarshaler interface.
func (h *H1I) MarshalYODA() ([]byte, error) {
	return h.AsH1D().MarshalYODA()
}

// UnmarshalYODA implements the YODAUnmarshaler interface.
func (h *H1I) UnmarshalYODA(raw []byte) error {
	var hh hbook.H1D
	err := hh.UnmarshalYODA(raw)
	if err != nil {
		return err
	}

	*h = *NewH1IFrom(&hh)
	return nil
}

func (h *H1I) ROOTMerge(src root.Object) error {
	hsrc, ok := src.(*H1I)
	if !ok {
		return fmt.Errorf("rhist: object %q is not a *rhist.H1I (%T)", src.(root.Named).Name(), src)
	}

	var (
		h1   = h.AsH1D()
		h2   = hsrc.AsH1D()
		hadd = hbook.AddH1D(h1, h2)
	)

	*h = *NewH1IFrom(hadd)
	return nil
}

func init() {
	f := func() reflect.Value {
		o := newH1I()
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TH1I", f)
}

var (
	_ root.Object        = (*H1I)(nil)
	_ root.Merger        = (*H1I)(nil)
	_ root.Named         = (*H1I)(nil)
	_ H1                 = (*H1I)(nil)
	_ rbytes.Marshaler   = (*H1I)(nil)
	_ rbytes.Unmarshaler = (*H1I)(nil)
	_ rbytes.RSlicer     = (*H1I)(nil)
)
