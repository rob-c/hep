// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build ignore

package main

import (
	"fmt"
	"log"
	"os"
	"text/template"

	"go-hep.org/x/hep/groot/internal/genroot"
)

func main() {
	genH1()
	genH2()
	genH3()
}

func genH1() {
	fname := "./rhist/h1_gen.go"
	year := genroot.ExtractYear(fname)
	f, err := os.Create(fname)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	genroot.GenImports(year, "rhist", f,
		"fmt", "math", "reflect",
		"",
		"go-hep.org/x/hep/hbook",
		"go-hep.org/x/hep/groot/root",
		"go-hep.org/x/hep/groot/rcont",
		"go-hep.org/x/hep/groot/rbytes",
		"go-hep.org/x/hep/groot/rtypes",
		"go-hep.org/x/hep/groot/rvers",
	)

	for i, typ := range []struct {
		Name string
		Type string
		Elem string
	}{
		{
			Name: "H1C",
			Type: "rcont.ArrayC",
			Elem: "int8",
		},
		{
			Name: "H1S",
			Type: "rcont.ArrayS",
			Elem: "int16",
		},
		{
			Name: "H1F",
			Type: "rcont.ArrayF",
			Elem: "float32",
		},
		{
			Name: "H1D",
			Type: "rcont.ArrayD",
			Elem: "float64",
		},
		{
			Name: "H1I",
			Type: "rcont.ArrayI",
			Elem: "int32",
		},
	} {
		if i > 0 {
			fmt.Fprintf(f, "\n")
		}
		tmpl := template.Must(template.New(typ.Name).Parse(h1Tmpl))
		err = tmpl.Execute(f, typ)
		if err != nil {
			log.Fatalf("error executing template for %q: %v\n", typ.Name, err)
		}
	}

	err = f.Close()
	if err != nil {
		log.Fatal(err)
	}
	genroot.GoFmt(f)
}

func genH2() {
	fname := "./rhist/h2_gen.go"
	year := genroot.ExtractYear(fname)
	f, err := os.Create(fname)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	genroot.GenImports(year, "rhist", f,
		"fmt", "math", "reflect",
		"",
		"go-hep.org/x/hep/hbook",
		"go-hep.org/x/hep/groot/root",
		"go-hep.org/x/hep/groot/rcont",
		"go-hep.org/x/hep/groot/rbytes",
		"go-hep.org/x/hep/groot/rtypes",
		"go-hep.org/x/hep/groot/rvers",
	)

	for i, typ := range []struct {
		Name string
		Type string
		Elem string
	}{
		{
			Name: "H2C",
			Type: "rcont.ArrayC",
			Elem: "int8",
		},
		{
			Name: "H2S",
			Type: "rcont.ArrayS",
			Elem: "int16",
		},
		{
			Name: "H2F",
			Type: "rcont.ArrayF",
			Elem: "float32",
		},
		{
			Name: "H2D",
			Type: "rcont.ArrayD",
			Elem: "float64",
		},
		{
			Name: "H2I",
			Type: "rcont.ArrayI",
			Elem: "int32",
		},
	} {
		if i > 0 {
			fmt.Fprintf(f, "\n")
		}
		tmpl := template.Must(template.New(typ.Name).Parse(h2Tmpl))
		err = tmpl.Execute(f, typ)
		if err != nil {
			log.Fatalf("error executing template for %q: %v\n", typ.Name, err)
		}
	}

	err = f.Close()
	if err != nil {
		log.Fatal(err)
	}
	genroot.GoFmt(f)
}

func genH3() {
	fname := "./rhist/h3_gen.go"
	year := genroot.ExtractYear(fname)
	f, err := os.Create(fname)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	genroot.GenImports(year, "rhist", f,
		"fmt", "math", "reflect",
		"",
		"go-hep.org/x/hep/hbook",
		"go-hep.org/x/hep/groot/root",
		"go-hep.org/x/hep/groot/rcont",
		"go-hep.org/x/hep/groot/rbytes",
		"go-hep.org/x/hep/groot/rtypes",
		"go-hep.org/x/hep/groot/rvers",
	)

	for i, typ := range []struct {
		Name string
		Type string
		Elem string
	}{
		{
			Name: "H3C",
			Type: "rcont.ArrayC",
			Elem: "int8",
		},
		{
			Name: "H3S",
			Type: "rcont.ArrayS",
			Elem: "int16",
		},
		{
			Name: "H3I",
			Type: "rcont.ArrayI",
			Elem: "int32",
		},
		{
			Name: "H3F",
			Type: "rcont.ArrayF",
			Elem: "float32",
		},
		{
			Name: "H3D",
			Type: "rcont.ArrayD",
			Elem: "float64",
		},
	} {
		if i > 0 {
			fmt.Fprintf(f, "\n")
		}
		tmpl := template.Must(template.New(typ.Name).Parse(h3Tmpl))
		err = tmpl.Execute(f, typ)
		if err != nil {
			log.Fatalf("error executing template for %q: %v\n", typ.Name, err)
		}
	}

	err = f.Close()
	if err != nil {
		log.Fatal(err)
	}
	genroot.GoFmt(f)
}

const h1Tmpl = `// {{.Name}} implements ROOT T{{.Name}}
type {{.Name}} struct {
	th1
	arr {{.Type}}
}

func new{{.Name}}() *{{.Name}} {
	return &{{.Name}}{
		th1:   *newH1(),
	}
}

// New{{.Name}}From creates a new 1-dim histogram from hbook.
func New{{.Name}}From(h *hbook.H1D) *{{.Name}} {
	var (
		hroot = new{{.Name}}()
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
	hroot.th1.ncells = nbins+2

	hroot.th1.xaxis.nbins = nbins
	hroot.th1.xaxis.xmin = h.XMin()
	hroot.th1.xaxis.xmax = h.XMax()

	hroot.arr.Data = make([]{{.Elem}}, nbins+2)
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

// New{{.Name}} creates a 1-dim histogram with n equal bins between xmin and
// xmax, as "new T{{.Name}}(name, title, n, xmin, xmax)" does in C++.
func New{{.Name}}(name, title string, n int, xmin, xmax float64) *{{.Name}} {
	h := new{{.Name}}()
	h.th1.SetName(name)
	h.th1.SetTitle(title)
	h.th1.xaxis.setRange(n, xmin, xmax)
	h.reset()
	return h
}

// New{{.Name}}FromEdges creates a 1-dim histogram whose bins are the ones the
// edges describe, for a binning that is not uniform.
func New{{.Name}}FromEdges(name, title string, edges []float64) *{{.Name}} {
	h := new{{.Name}}()
	h.th1.SetName(name)
	h.th1.SetTitle(title)
	h.th1.xaxis.setEdges(edges)
	h.reset()
	return h
}

// reset sizes the cells to the axis and empties them.
func (h *{{.Name}}) reset() {
	n := h.th1.xaxis.nbins + 2 // and the under- and overflow
	h.th1.ncells = n
	h.arr.Data = make([]{{.Elem}}, n)
	h.th1.sumw2.Data = make([]float64, n)
	h.th1.entries = 0
	h.th1.tsumw = 0
	h.th1.tsumw2 = 0
	h.th1.tsumwx = 0
	h.th1.tsumwx2 = 0
}

// Reset empties the histogram, keeping its binning.
func (h *{{.Name}}) Reset() { h.reset() }

// FindBin returns the bin x falls in: 0 for the underflow, 1 to NbinsX for
// the bins proper, NbinsX+1 for the overflow.
func (h *{{.Name}}) FindBin(x float64) int {
	return h.th1.xaxis.FindBin(x)
}

// Fill adds an entry of weight w at x.
func (h *{{.Name}}) Fill(x, w float64) {
	i := h.FindBin(x)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}

	h.arr.Data[i] += {{.Elem}}(w)
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
func (h *{{.Name}}) FillN(xs, ws []float64) {
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
func (h *{{.Name}}) BinContent(i int) float64 {
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	return float64(h.arr.Data[i])
}

// SetBinContent sets the content of the i-th cell.
func (h *{{.Name}}) SetBinContent(i int, v float64) {
	if i < 0 || i >= len(h.arr.Data) {
		return
	}
	h.arr.Data[i] = {{.Elem}}(v)
}

// BinError returns the uncertainty on the i-th cell: the square root of its
// sum of squared weights, or of its content where no such sum is kept.
func (h *{{.Name}}) BinError(i int) float64 {
	return h.XBinError(i)
}

// SetBinError sets the uncertainty on the i-th cell.
func (h *{{.Name}}) SetBinError(i int, e float64) {
	if i < 0 || i >= len(h.th1.sumw2.Data) {
		return
	}
	h.th1.sumw2.Data[i] = e * e
}

// Scale multiplies every cell by f, and the uncertainties with them.
func (h *{{.Name}}) Scale(f float64) {
	for i := range h.arr.Data {
		h.arr.Data[i] = {{.Elem}}(float64(h.arr.Data[i]) * f)
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
func (h *{{.Name}}) Integral() float64 {
	return h.IntegralRange(1, h.th1.xaxis.nbins)
}

// IntegralRange returns the sum of the cells from lo to hi, both included.
func (h *{{.Name}}) IntegralRange(lo, hi int) float64 {
	var sum float64
	for i := max(0, lo); i <= min(hi, len(h.arr.Data)-1); i++ {
		sum += float64(h.arr.Data[i])
	}
	return sum
}

// Maximum returns the largest content of the bins proper, and MaximumBin
// which bin holds it.
func (h *{{.Name}}) Maximum() float64 {
	_, v := h.maxBin()
	return v
}

// MaximumBin returns the bin holding the largest content.
func (h *{{.Name}}) MaximumBin() int {
	i, _ := h.maxBin()
	return i
}

func (h *{{.Name}}) maxBin() (int, float64) {
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
func (h *{{.Name}}) Minimum() float64 {
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
func (h *{{.Name}}) Mean() float64 {
	if h.th1.tsumw == 0 {
		return 0
	}
	return h.th1.tsumwx / h.th1.tsumw
}

// StdDev returns the standard deviation of the entries.
func (h *{{.Name}}) StdDev() float64 {
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

func (*{{.Name}}) RVersion() int16 {
	return rvers.{{.Name}}
}

func (*{{.Name}}) isH1() {}

// Class returns the ROOT class name.
func (*{{.Name}}) Class() string {
	return "T{{.Name}}"
}

func (h *{{.Name}}) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(h.Class(), h.RVersion())

	w.WriteObject(&h.th1)
	w.WriteObject(&h.arr)

	return w.SetHeader(hdr)
}

func (h *{{.Name}}) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(h.Class(), h.RVersion())

	r.ReadObject(&h.th1)
	r.ReadObject(&h.arr)

	r.CheckHeader(hdr)
	return r.Err()
}

func (h *{{.Name}}) RMembers() (mbrs []rbytes.Member) {
	mbrs = append(mbrs, h.th1.RMembers()...)
	mbrs = append(mbrs, rbytes.Member{
		Name: "fArray", Value: &h.arr.Data,
	})
	return mbrs
}

func (h *{{.Name}}) Array() {{.Type}} {
	return h.arr
}

// Rank returns the number of dimensions of this histogram.
func (h *{{.Name}}) Rank() int {
	return 1
}

// NbinsX returns the number of bins in X.
func (h *{{.Name}}) NbinsX() int {
	return h.th1.xaxis.nbins
}

// XAxis returns the axis along X.
func (h*{{.Name}}) XAxis() Axis {
	return &h.th1.xaxis
}

// bin returns the regularized bin number given an x bin pair.
func (h *{{.Name}}) bin(ix int) int {
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
func (h *{{.Name}}) XBinCenter(i int) float64 {
	return float64(h.th1.xaxis.BinCenter(i))
}

// XBinContent returns the bin content value in X.
func (h *{{.Name}}) XBinContent(i int) float64 {
	ibin := h.bin(i)
	return float64(h.arr.Data[ibin])
}

// XBinError returns the bin error in X.
func (h *{{.Name}}) XBinError(i int) float64 {
	ibin := h.bin(i)
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[ibin]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[ibin])))
}

// XBinLowEdge returns the bin lower edge value in X.
func (h *{{.Name}}) XBinLowEdge(i int) float64 {
	return h.th1.xaxis.BinLowEdge(i)
}

// XBinWidth returns the bin width in X.
func (h *{{.Name}}) XBinWidth(i int) float64 {
	return h.th1.xaxis.BinWidth(i)
}

func (h *{{.Name}}) dist1D(i int) hbook.Dist1D {
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

func (h *{{.Name}}) setDist1D(i int, sumw, sumw2 float64) {
	h.arr.Data[i] = {{.Elem}}(sumw)
	h.th1.sumw2.Data[i] = sumw2
}

func (h *{{.Name}}) entries(height, err float64) int64 {
	if height <= 0 {
		return 0
	}
	v := height / err
	return int64(v*v+0.5)
}

// AsH1D creates a new hbook.H1D from this ROOT histogram.
func (h *{{.Name}}) AsH1D() *hbook.H1D {
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
func (h *{{.Name}}) MarshalYODA() ([]byte, error) {
	return h.AsH1D().MarshalYODA()
}

// UnmarshalYODA implements the YODAUnmarshaler interface.
func (h *{{.Name}}) UnmarshalYODA(raw []byte) error {
	var hh hbook.H1D
	err := hh.UnmarshalYODA(raw)
	if err != nil {
		return err
	}

	*h = *New{{.Name}}From(&hh)
	return nil
}

func (h *{{.Name}}) ROOTMerge(src root.Object) error {
	hsrc, ok := src.(*{{.Name}})
	if !ok {
		return fmt.Errorf("rhist: object %q is not a *rhist.{{.Name}} (%T)", src.(root.Named).Name(), src)
	}

	var (
		h1   = h.AsH1D()
		h2   = hsrc.AsH1D()
		hadd = hbook.AddH1D(h1, h2)
	)

	*h = *New{{.Name}}From(hadd)
	return nil
}

func init() {
	f := func() reflect.Value {
		o := new{{.Name}}()
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("T{{.Name}}", f)
}

var (
	_ root.Object        = (*{{.Name}})(nil)
	_ root.Merger        = (*{{.Name}})(nil)
	_ root.Named         = (*{{.Name}})(nil)
	_ H1                 = (*{{.Name}})(nil)
	_ rbytes.Marshaler   = (*{{.Name}})(nil)
	_ rbytes.Unmarshaler = (*{{.Name}})(nil)
	_ rbytes.RSlicer     = (*{{.Name}})(nil)
)
`

const h2Tmpl = `// {{.Name}} implements ROOT T{{.Name}}
type {{.Name}} struct {
	th2
	arr {{.Type}}
}

func new{{.Name}}() *{{.Name}} {
	return &{{.Name}}{
		th2:   *newH2(),
	}
}

// New{{.Name}}From creates a new {{.Name}} from hbook 2-dim histogram.
func New{{.Name}}From(h *hbook.H2D) *{{.Name}} {
	var (
		hroot  = new{{.Name}}()
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

	hroot.arr.Data = make([]{{.Elem}}, ncells)
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
	for i, v := range []struct{ix,iy int}{
		{0, 0},
		{0, 1},
		{0, nybins+1},
		{nxbins + 1, 0},
		{nxbins + 1, 1},
		{nxbins + 1, nybins + 1},
		{1, 0},
		{1, nybins + 1},
	}{
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

// New{{.Name}} creates a 2-dim histogram, as "new T{{.Name}}(name, title,
// nx, xmin, xmax, ny, ymin, ymax)" does in C++.
func New{{.Name}}(name, title string, nx int, xmin, xmax float64, ny int, ymin, ymax float64) *{{.Name}} {
	h := new{{.Name}}()
	h.th2.th1.SetName(name)
	h.th2.th1.SetTitle(title)
	h.th2.th1.xaxis.setRange(nx, xmin, xmax)
	h.th2.th1.yaxis.setRange(ny, ymin, ymax)
	h.reset()
	return h
}

// reset sizes the cells to the axes and empties them.
func (h *{{.Name}}) reset() {
	n := (h.th1.xaxis.nbins + 2) * (h.th1.yaxis.nbins + 2)
	h.th2.th1.ncells = n
	h.arr.Data = make([]{{.Elem}}, n)
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
func (h *{{.Name}}) Reset() { h.reset() }

// FindBin returns the cell (x,y) falls in, as an index into the flat array
// the histogram keeps.
func (h *{{.Name}}) FindBin(x, y float64) int {
	return h.bin(h.th1.xaxis.FindBin(x), h.th1.yaxis.FindBin(y))
}

// Fill adds an entry of weight w at (x,y).
func (h *{{.Name}}) Fill(x, y, w float64) {
	var (
		ix = h.th1.xaxis.FindBin(x)
		iy = h.th1.yaxis.FindBin(y)
		i  = h.bin(ix, iy)
	)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}

	h.arr.Data[i] += {{.Elem}}(w)
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
func (h *{{.Name}}) FillN(xs, ys, ws []float64) {
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
func (h *{{.Name}}) BinContent(ix, iy int) float64 {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	return float64(h.arr.Data[i])
}

// SetBinContent sets the content of the cell at (ix,iy).
func (h *{{.Name}}) SetBinContent(ix, iy int, v float64) {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}
	h.arr.Data[i] = {{.Elem}}(v)
}

// BinError returns the uncertainty on the cell at (ix,iy).
func (h *{{.Name}}) BinError(ix, iy int) float64 {
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
func (h *{{.Name}}) SetBinError(ix, iy int, e float64) {
	i := h.bin(ix, iy)
	if i < 0 || i >= len(h.th1.sumw2.Data) {
		return
	}
	h.th1.sumw2.Data[i] = e * e
}

// Scale multiplies every cell by f, and the uncertainties with them.
func (h *{{.Name}}) Scale(f float64) {
	for i := range h.arr.Data {
		h.arr.Data[i] = {{.Elem}}(float64(h.arr.Data[i]) * f)
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
func (h *{{.Name}}) Integral() float64 {
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
func (h *{{.Name}}) ProjectionX(name string) *H1D {
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
func (h *{{.Name}}) ProjectionY(name string) *H1D {
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
func (h *{{.Name}}) MeanX() float64 {
	if h.th2.th1.tsumw == 0 {
		return 0
	}
	return h.th2.th1.tsumwx / h.th2.th1.tsumw
}

// MeanY returns the mean along y.
func (h *{{.Name}}) MeanY() float64 {
	if h.th2.th1.tsumw == 0 {
		return 0
	}
	return h.th2.tsumwy / h.th2.th1.tsumw
}

func (*{{.Name}}) RVersion() int16 {
	return rvers.{{.Name}}
}

func (*{{.Name}}) isH2() {}

// Class returns the ROOT class name.
func (*{{.Name}}) Class() string {
	return "T{{.Name}}"
}

func (h *{{.Name}}) Array() {{.Type}} {
	return h.arr
}

// Rank returns the number of dimensions of this histogram.
func (h *{{.Name}}) Rank() int {
	return 2
}

// NbinsX returns the number of bins in X.
func (h *{{.Name}}) NbinsX() int {
	return h.th1.xaxis.nbins
}

// XAxis returns the axis along X.
func (h*{{.Name}}) XAxis() Axis {
	return &h.th1.xaxis
}

// XBinCenter returns the bin center value in X.
func (h *{{.Name}}) XBinCenter(i int) float64 {
	return float64(h.th1.xaxis.BinCenter(i))
}

// XBinContent returns the bin content value in X.
func (h *{{.Name}}) XBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// XBinError returns the bin error in X.
func (h *{{.Name}}) XBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// XBinLowEdge returns the bin lower edge value in X.
func (h *{{.Name}}) XBinLowEdge(i int) float64 {
	return h.th1.xaxis.BinLowEdge(i)
}

// XBinWidth returns the bin width in X.
func (h *{{.Name}}) XBinWidth(i int) float64 {
	return h.th1.xaxis.BinWidth(i)
}

// NbinsY returns the number of bins in Y.
func (h *{{.Name}}) NbinsY() int {
	return h.th1.yaxis.nbins
}

// YAxis returns the axis along Y.
func (h*{{.Name}}) YAxis() Axis {
	return &h.th1.yaxis
}

// YBinCenter returns the bin center value in Y.
func (h *{{.Name}}) YBinCenter(i int) float64 {
	return float64(h.th1.yaxis.BinCenter(i))
}

// YBinContent returns the bin content value in Y.
func (h *{{.Name}}) YBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// YBinError returns the bin error in Y.
func (h *{{.Name}}) YBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// YBinLowEdge returns the bin lower edge value in Y.
func (h *{{.Name}}) YBinLowEdge(i int) float64 {
	return h.th1.yaxis.BinLowEdge(i)
}

// YBinWidth returns the bin width in Y.
func (h *{{.Name}}) YBinWidth(i int) float64 {
	return h.th1.yaxis.BinWidth(i)
}

// bin returns the regularized bin number given an (x,y) bin index pair.
func (h *{{.Name}}) bin(ix, iy int) int {
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

func (h *{{.Name}}) dist2D(ix, iy int) hbook.Dist2D {
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

func (h *{{.Name}}) setDist2D(ix, iy int, sumw, sumw2 float64) {
	i := h.bin(ix, iy)
	h.arr.Data[i] = {{.Elem}}(sumw)
	h.th1.sumw2.Data[i] = sumw2
}

func (h *{{.Name}}) entries(height, err float64) int64 {
	if height <= 0 {
		return 0
	}
	v := height / err
	return int64(v*v + 0.5)
}

// AsH2D creates a new hbook.H2D from this ROOT histogram.
func (h *{{.Name}}) AsH2D() *hbook.H2D {
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
func (h *{{.Name}}) MarshalYODA() ([]byte, error) {
	return h.AsH2D().MarshalYODA()
}

// UnmarshalYODA implements the YODAUnmarshaler interface.
func (h *{{.Name}}) UnmarshalYODA(raw []byte) error {
	var hh hbook.H2D
	err := hh.UnmarshalYODA(raw)
	if err != nil {
		return err
	}

	*h = *New{{.Name}}From(&hh)
	return nil
}

func (h *{{.Name}}) ROOTMerge(src root.Object) error {
	hsrc, ok := src.(*{{.Name}})
	if !ok {
		return fmt.Errorf("rhist: object %q is not a *rhist.{{.Name}} (%T)", src.(root.Named).Name(), src)
	}

	var (
		h1   = h.AsH2D()
		h2   = hsrc.AsH2D()
		hadd = hbook.AddH2D(h1, h2)
	)

	*h = *New{{.Name}}From(hadd)
	return nil
}

func (h *{{.Name}}) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(h.Class(), h.RVersion())
	w.WriteObject(&h.th2)
	w.WriteObject(&h.arr)

	return w.SetHeader(hdr)
}

func (h *{{.Name}}) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(h.Class(), h.RVersion())
	if hdr.Vers < 1 {
		return fmt.Errorf("rhist: T{{.Name}} version too old (%d<1)", hdr.Vers)
	}

	r.ReadObject(&h.th2)
	r.ReadObject(&h.arr)

	r.CheckHeader(hdr)
	return r.Err()
}

func (h *{{.Name}}) RMembers() (mbrs []rbytes.Member) {
	mbrs = append(mbrs, h.th2.RMembers()...)
	mbrs = append(mbrs, rbytes.Member{
		Name: "fArray", Value: &h.arr.Data,
	})
	return mbrs
}

func init() {
	f := func() reflect.Value {
		o := new{{.Name}}()
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("T{{.Name}}", f)
}

var (
	_ root.Object        = (*{{.Name}})(nil)
	_ root.Merger        = (*{{.Name}})(nil)
	_ root.Named         = (*{{.Name}})(nil)
	_ H2                 = (*{{.Name}})(nil)
	_ rbytes.Marshaler   = (*{{.Name}})(nil)
	_ rbytes.Unmarshaler = (*{{.Name}})(nil)
	_ rbytes.RSlicer     = (*{{.Name}})(nil)
)
`

const h3Tmpl = `// {{.Name}} implements ROOT T{{.Name}}
type {{.Name}} struct {
	th3
	arr {{.Type}}
}

func new{{.Name}}() *{{.Name}} {
	return &{{.Name}}{
		th3:   *newH3(),
	}
}

// New{{.Name}}From creates a new {{.Name}} from an hbook 3-dim histogram.
func New{{.Name}}From(h *hbook.H3D) *{{.Name}} {
	var (
		hroot  = new{{.Name}}()
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

	hroot.arr.Data = make([]{{.Elem}}, ncells)
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

// New{{.Name}} creates a 3-dim histogram, as "new T{{.Name}}(name, title,
// nx, xmin, xmax, ny, ymin, ymax, nz, zmin, zmax)" does in C++.
func New{{.Name}}(name, title string, nx int, xmin, xmax float64, ny int, ymin, ymax float64, nz int, zmin, zmax float64) *{{.Name}} {
	h := new{{.Name}}()
	h.th3.th1.SetName(name)
	h.th3.th1.SetTitle(title)
	h.th3.th1.xaxis.setRange(nx, xmin, xmax)
	h.th3.th1.yaxis.setRange(ny, ymin, ymax)
	h.th3.th1.zaxis.setRange(nz, zmin, zmax)
	h.reset()
	return h
}

// reset sizes the cells to the axes and empties them.
func (h *{{.Name}}) reset() {
	n := (h.th1.xaxis.nbins + 2) * (h.th1.yaxis.nbins + 2) * (h.th1.zaxis.nbins + 2)
	h.th3.th1.ncells = n
	h.arr.Data = make([]{{.Elem}}, n)
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
func (h *{{.Name}}) Reset() { h.reset() }

// FindBin returns the cell (x,y,z) falls in, as an index into the flat array
// the histogram keeps.
func (h *{{.Name}}) FindBin(x, y, z float64) int {
	return h.bin(
		h.th1.xaxis.FindBin(x),
		h.th1.yaxis.FindBin(y),
		h.th1.zaxis.FindBin(z),
	)
}

// Fill adds an entry of weight w at (x,y,z).
func (h *{{.Name}}) Fill(x, y, z, w float64) {
	var (
		ix = h.th1.xaxis.FindBin(x)
		iy = h.th1.yaxis.FindBin(y)
		iz = h.th1.zaxis.FindBin(z)
		i  = h.bin(ix, iy, iz)
	)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}

	h.arr.Data[i] += {{.Elem}}(w)
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
func (h *{{.Name}}) FillN(xs, ys, zs, ws []float64) {
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
func (h *{{.Name}}) BinContent(ix, iy, iz int) float64 {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.arr.Data) {
		return 0
	}
	return float64(h.arr.Data[i])
}

// SetBinContent sets the content of the cell at (ix,iy,iz).
func (h *{{.Name}}) SetBinContent(ix, iy, iz int, v float64) {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.arr.Data) {
		return
	}
	h.arr.Data[i] = {{.Elem}}(v)
}

// BinError returns the uncertainty on the cell at (ix,iy,iz).
func (h *{{.Name}}) BinError(ix, iy, iz int) float64 {
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
func (h *{{.Name}}) SetBinError(ix, iy, iz int, e float64) {
	i := h.bin(ix, iy, iz)
	if i < 0 || i >= len(h.th1.sumw2.Data) {
		return
	}
	h.th1.sumw2.Data[i] = e * e
}

// Scale multiplies every cell by f, and the uncertainties with them.
func (h *{{.Name}}) Scale(f float64) {
	for i := range h.arr.Data {
		h.arr.Data[i] = {{.Elem}}(float64(h.arr.Data[i]) * f)
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
func (h *{{.Name}}) Integral() float64 {
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
func (h *{{.Name}}) ProjectionZ(name string) *H1D {
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
func (h *{{.Name}}) ProjectionXY(name string) *H2D {
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
func (h *{{.Name}}) MeanX() float64 {
	if h.th3.th1.tsumw == 0 {
		return 0
	}
	return h.th3.th1.tsumwx / h.th3.th1.tsumw
}

// MeanY returns the mean along y.
func (h *{{.Name}}) MeanY() float64 {
	if h.th3.th1.tsumw == 0 {
		return 0
	}
	return h.th3.tsumwy / h.th3.th1.tsumw
}

// MeanZ returns the mean along z.
func (h *{{.Name}}) MeanZ() float64 {
	if h.th3.th1.tsumw == 0 {
		return 0
	}
	return h.th3.tsumwz / h.th3.th1.tsumw
}

func (*{{.Name}}) RVersion() int16 {
	return rvers.{{.Name}}
}

func (*{{.Name}}) isH3() {}

// Class returns the ROOT class name.
func (*{{.Name}}) Class() string {
	return "T{{.Name}}"
}

func (h *{{.Name}}) Array() {{.Type}} {
	return h.arr
}

// Rank returns the number of dimensions of this histogram.
func (h *{{.Name}}) Rank() int {
	return 3
}

// NbinsX returns the number of bins in X.
func (h *{{.Name}}) NbinsX() int {
	return h.th1.xaxis.nbins
}

// XAxis returns the axis along X.
func (h*{{.Name}}) XAxis() Axis {
	return &h.th1.xaxis
}

// XBinCenter returns the bin center value in X.
func (h *{{.Name}}) XBinCenter(i int) float64 {
	return float64(h.th1.xaxis.BinCenter(i))
}

// XBinContent returns the bin content value in X.
func (h *{{.Name}}) XBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// XBinError returns the bin error in X.
func (h *{{.Name}}) XBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// XBinLowEdge returns the bin lower edge value in X.
func (h *{{.Name}}) XBinLowEdge(i int) float64 {
	return h.th1.xaxis.BinLowEdge(i)
}

// XBinWidth returns the bin width in X.
func (h *{{.Name}}) XBinWidth(i int) float64 {
	return h.th1.xaxis.BinWidth(i)
}

// NbinsY returns the number of bins in Y.
func (h *{{.Name}}) NbinsY() int {
	return h.th1.yaxis.nbins
}

// YAxis returns the axis along Y.
func (h*{{.Name}}) YAxis() Axis {
	return &h.th1.yaxis
}

// YBinCenter returns the bin center value in Y.
func (h *{{.Name}}) YBinCenter(i int) float64 {
	return float64(h.th1.yaxis.BinCenter(i))
}

// YBinContent returns the bin content value in Y.
func (h *{{.Name}}) YBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// YBinError returns the bin error in Y.
func (h *{{.Name}}) YBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// YBinLowEdge returns the bin lower edge value in Y.
func (h *{{.Name}}) YBinLowEdge(i int) float64 {
	return h.th1.yaxis.BinLowEdge(i)
}

// YBinWidth returns the bin width in Y.
func (h *{{.Name}}) YBinWidth(i int) float64 {
	return h.th1.yaxis.BinWidth(i)
}

// NbinsZ returns the number of bins in Z.
func (h *{{.Name}}) NbinsZ() int {
	return h.th1.zaxis.nbins
}

// ZAxis returns the axis along Z.
func (h*{{.Name}}) ZAxis() Axis {
	return &h.th1.zaxis
}

// ZBinCenter returns the bin center value in Z.
func (h *{{.Name}}) ZBinCenter(i int) float64 {
	return float64(h.th1.zaxis.BinCenter(i))
}

// ZBinContent returns the bin content value in Z.
func (h *{{.Name}}) ZBinContent(i int) float64 {
	return float64(h.arr.Data[i])
}

// ZBinError returns the bin error in Z.
func (h *{{.Name}}) ZBinError(i int) float64 {
	if len(h.th1.sumw2.Data) > 0 {
		return math.Sqrt(float64(h.th1.sumw2.Data[i]))
	}
	return math.Sqrt(math.Abs(float64(h.arr.Data[i])))
}

// ZBinLowEdge returns the bin lower edge value in Z.
func (h *{{.Name}}) ZBinLowEdge(i int) float64 {
	return h.th1.zaxis.BinLowEdge(i)
}

// ZBinWidth returns the bin width in Z.
func (h *{{.Name}}) ZBinWidth(i int) float64 {
	return h.th1.zaxis.BinWidth(i)
}

// bin returns the regularized bin number given an (x,y,z) bin index triple.
func (h *{{.Name}}) bin(ix, iy, iz int) int {
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

func (h *{{.Name}}) dist3D(ix, iy, iz int) hbook.Dist3D {
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

func (h *{{.Name}}) setDist3D(ix, iy, iz int, sumw, sumw2 float64) {
	i := h.bin(ix, iy, iz)
	h.arr.Data[i] = {{.Elem}}(sumw)
	h.th1.sumw2.Data[i] = sumw2
}

func (h *{{.Name}}) entries(height, err float64) int64 {
	if height <= 0 {
		return 0
	}
	v := height / err
	return int64(v*v + 0.5)
}

// AsH3D creates a new hbook.H3D from this ROOT histogram.
func (h *{{.Name}}) AsH3D() *hbook.H3D {
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

func (h *{{.Name}}) ROOTMerge(src root.Object) error {
	hsrc, ok := src.(*{{.Name}})
	if !ok {
		return fmt.Errorf("rhist: object %q is not a *rhist.{{.Name}} (%T)", src.(root.Named).Name(), src)
	}

	var (
		h1   = h.AsH3D()
		h2   = hsrc.AsH3D()
		hadd = hbook.AddH3D(h1, h2)
	)

	*h = *New{{.Name}}From(hadd)
	return nil
}

func (h *{{.Name}}) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(h.Class(), h.RVersion())
	w.WriteObject(&h.th3)
	w.WriteObject(&h.arr)

	return w.SetHeader(hdr)
}

func (h *{{.Name}}) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(h.Class(), h.RVersion())
	if hdr.Vers < 1 {
		return fmt.Errorf("rhist: T{{.Name}} version too old (%d<1)", hdr.Vers)
	}

	r.ReadObject(&h.th3)
	r.ReadObject(&h.arr)

	r.CheckHeader(hdr)
	return r.Err()
}

func (h *{{.Name}}) RMembers() (mbrs []rbytes.Member) {
	mbrs = append(mbrs, h.th3.RMembers()...)
	mbrs = append(mbrs, rbytes.Member{
		Name: "fArray", Value: &h.arr.Data,
	})
	return mbrs
}

func init() {
	f := func() reflect.Value {
		o := new{{.Name}}()
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("T{{.Name}}", f)
}

var (
	_ root.Object        = (*{{.Name}})(nil)
	_ root.Merger        = (*{{.Name}})(nil)
	_ root.Named         = (*{{.Name}})(nil)
	_ H3                 = (*{{.Name}})(nil)
	_ rbytes.Marshaler   = (*{{.Name}})(nil)
	_ rbytes.Unmarshaler = (*{{.Name}})(nil)
	_ rbytes.RSlicer     = (*{{.Name}})(nil)
)
`
