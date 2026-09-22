// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rhist

import (
	"fmt"
	"reflect"

	"go-hep.org/x/hep/groot/rbase"
	"go-hep.org/x/hep/groot/rbytes"
	"go-hep.org/x/hep/groot/rcont"
	"go-hep.org/x/hep/groot/root"
	"go-hep.org/x/hep/groot/rtypes"
	"go-hep.org/x/hep/groot/rvers"
)

// Graph2D implements ROOT TGraph2D: a set of (x,y,z) points.
//
// The members TGraph2D keeps for drawing — the interpolating histogram, the
// Delaunay triangulation and the painter — are transient in ROOT and never
// reach a file, so they are absent here too.
type Graph2D struct {
	named   rbase.Named
	attline rbase.AttLine
	attfill rbase.AttFill
	attmark rbase.AttMarker

	npoints int32 // Number of points in the data set
	npx     int32 // Number of bins along X in the interpolating histogram
	npy     int32 // Number of bins along Y in the interpolating histogram
	maxiter int32 // Maximum number of iterations to find Delaunay triangles

	x []float64 // [fNpoints]
	y []float64 // [fNpoints]
	z []float64 // [fNpoints]

	min    float64 // Minimum value for plotting along z
	max    float64 // Maximum value for plotting along z
	margin float64 // Extra space (in %) around the interpolated area
	zout   float64 // Bin height for points outside the interpolated area

	funcs root.List // Pointer to list of functions (fits and user)

	userHisto bool // True when SetHistogram has been called
}

func newGraph2D(n int) *Graph2D {
	return &Graph2D{
		named:   *rbase.NewNamed("", ""),
		attline: *rbase.NewAttLine(),
		attfill: *rbase.NewAttFill(),
		attmark: *rbase.NewAttMarker(),
		npoints: int32(n),
		npx:     40, // ROOT's defaults for the interpolating histogram
		npy:     40,
		maxiter: 100000,
		x:       make([]float64, n),
		y:       make([]float64, n),
		z:       make([]float64, n),
		min:     -1111,
		max:     -1111,
		margin:  0.1,
		funcs:   rcont.NewList("", nil),
	}
}

// NewGraph2D creates a TGraph2D holding n points, all at the origin.
func NewGraph2D(n int) *Graph2D {
	return newGraph2D(n)
}

// NewGraph2DFrom creates a TGraph2D from the given coordinates.
//
// NewGraph2DFrom returns an error if the three slices do not have the same
// length.
func NewGraph2DFrom(xs, ys, zs []float64) (*Graph2D, error) {
	if len(xs) != len(ys) || len(xs) != len(zs) {
		return nil, fmt.Errorf(
			"rhist: TGraph2D needs as many x, y and z values (got %d, %d, %d)",
			len(xs), len(ys), len(zs),
		)
	}

	g := newGraph2D(len(xs))
	copy(g.x, xs)
	copy(g.y, ys)
	copy(g.z, zs)
	return g, nil
}

func (*Graph2D) RVersion() int16 {
	return rvers.Graph2D
}

func (*Graph2D) Class() string {
	return "TGraph2D"
}

func (g *Graph2D) Name() string {
	return g.named.Name()
}

func (g *Graph2D) Title() string {
	return g.named.Title()
}

func (g *Graph2D) SetName(name string)   { g.named.SetName(name) }
func (g *Graph2D) SetTitle(title string) { g.named.SetTitle(title) }

// Len returns the number of points of this graph.
func (g *Graph2D) Len() int {
	return int(g.npoints)
}

// XYZ returns the (x,y,z) coordinates of the i-th point.
func (g *Graph2D) XYZ(i int) (x, y, z float64) {
	return g.x[i], g.y[i], g.z[i]
}

// XY returns the (x,y) coordinates of the i-th point, which is what makes a
// TGraph2D usable wherever a 2-dim graph is.
func (g *Graph2D) XY(i int) (x, y float64) {
	return g.x[i], g.y[i]
}

// SetXYZ sets the (x,y,z) coordinates of the i-th point.
func (g *Graph2D) SetXYZ(i int, x, y, z float64) {
	g.x[i] = x
	g.y[i] = y
	g.z[i] = z
}

// ZMin returns the minimum value for plotting along z.
func (g *Graph2D) ZMin() float64 { return g.min }

// ZMax returns the maximum value for plotting along z.
func (g *Graph2D) ZMax() float64 { return g.max }

func (g *Graph2D) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(g.Class(), g.RVersion())

	w.WriteObject(&g.named)
	w.WriteObject(&g.attline)
	w.WriteObject(&g.attfill)
	w.WriteObject(&g.attmark)

	w.WriteI32(g.npoints)
	w.WriteI32(g.npx)
	w.WriteI32(g.npy)
	w.WriteI32(g.maxiter)

	w.WriteI8(1)
	w.WriteArrayF64(g.x)
	w.WriteI8(1)
	w.WriteArrayF64(g.y)
	w.WriteI8(1)
	w.WriteArrayF64(g.z)

	w.WriteF64(g.min)
	w.WriteF64(g.max)
	w.WriteF64(g.margin)
	w.WriteF64(g.zout)

	w.WriteObjectAny(g.funcs)

	w.WriteBool(g.userHisto)

	return w.SetHeader(hdr)
}

func (g *Graph2D) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(g.Class(), g.RVersion())

	r.ReadObject(&g.named)
	r.ReadObject(&g.attline)
	r.ReadObject(&g.attfill)
	r.ReadObject(&g.attmark)

	g.npoints = r.ReadI32()
	g.npx = r.ReadI32()
	g.npy = r.ReadI32()
	g.maxiter = r.ReadI32()

	g.x = rbytes.ResizeF64(g.x, int(g.npoints))
	g.y = rbytes.ResizeF64(g.y, int(g.npoints))
	g.z = rbytes.ResizeF64(g.z, int(g.npoints))

	_ = r.ReadI8()
	r.ReadArrayF64(g.x)
	_ = r.ReadI8()
	r.ReadArrayF64(g.y)
	_ = r.ReadI8()
	r.ReadArrayF64(g.z)

	g.min = r.ReadF64()
	g.max = r.ReadF64()
	g.margin = r.ReadF64()
	g.zout = r.ReadF64()

	g.funcs = nil
	if obj := r.ReadObjectAny(); obj != nil {
		g.funcs = obj.(root.List)
	}

	g.userHisto = r.ReadBool()

	r.CheckHeader(hdr)
	return r.Err()
}

func init() {
	f := func() reflect.Value {
		o := newGraph2D(0)
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TGraph2D", f)
}

var (
	_ root.Object        = (*Graph2D)(nil)
	_ root.Named         = (*Graph2D)(nil)
	_ Graph              = (*Graph2D)(nil)
	_ rbytes.Marshaler   = (*Graph2D)(nil)
	_ rbytes.Unmarshaler = (*Graph2D)(nil)
)

// Graph2DErrors implements ROOT TGraph2DErrors: a TGraph2D carrying an
// uncertainty on each of its three coordinates.
type Graph2DErrors struct {
	Graph2D
	xerr []float64 // [fNpoints] array of X errors
	yerr []float64 // [fNpoints] array of Y errors
	zerr []float64 // [fNpoints] array of Z errors
}

func newGraph2DErrors(n int) *Graph2DErrors {
	return &Graph2DErrors{
		Graph2D: *newGraph2D(n),
		xerr:    make([]float64, n),
		yerr:    make([]float64, n),
		zerr:    make([]float64, n),
	}
}

// NewGraph2DErrors creates a TGraph2DErrors holding n points.
func NewGraph2DErrors(n int) *Graph2DErrors {
	return newGraph2DErrors(n)
}

func (*Graph2DErrors) RVersion() int16 {
	return rvers.Graph2DErrors
}

func (*Graph2DErrors) Class() string {
	return "TGraph2DErrors"
}

// XError returns the uncertainty on x of the i-th point, low and high, which
// TGraph2DErrors keeps as a single symmetric value.
func (g *Graph2DErrors) XError(i int) (float64, float64) {
	return g.xerr[i], g.xerr[i]
}

// YError returns the uncertainty on y of the i-th point, low and high.
func (g *Graph2DErrors) YError(i int) (float64, float64) {
	return g.yerr[i], g.yerr[i]
}

// ZError returns the uncertainty on z of the i-th point, low and high.
func (g *Graph2DErrors) ZError(i int) (float64, float64) {
	return g.zerr[i], g.zerr[i]
}

// SetXYZError sets the uncertainties on the i-th point.
func (g *Graph2DErrors) SetXYZError(i int, ex, ey, ez float64) {
	g.xerr[i] = ex
	g.yerr[i] = ey
	g.zerr[i] = ez
}

func (g *Graph2DErrors) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(g.Class(), g.RVersion())

	w.WriteObject(&g.Graph2D)

	w.WriteI8(1)
	w.WriteArrayF64(g.xerr)
	w.WriteI8(1)
	w.WriteArrayF64(g.yerr)
	w.WriteI8(1)
	w.WriteArrayF64(g.zerr)

	return w.SetHeader(hdr)
}

func (g *Graph2DErrors) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(g.Class(), g.RVersion())

	r.ReadObject(&g.Graph2D)

	g.xerr = rbytes.ResizeF64(g.xerr, int(g.npoints))
	g.yerr = rbytes.ResizeF64(g.yerr, int(g.npoints))
	g.zerr = rbytes.ResizeF64(g.zerr, int(g.npoints))

	_ = r.ReadI8()
	r.ReadArrayF64(g.xerr)
	_ = r.ReadI8()
	r.ReadArrayF64(g.yerr)
	_ = r.ReadI8()
	r.ReadArrayF64(g.zerr)

	r.CheckHeader(hdr)
	return r.Err()
}

func init() {
	f := func() reflect.Value {
		o := newGraph2DErrors(0)
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TGraph2DErrors", f)
}

var (
	_ root.Object        = (*Graph2DErrors)(nil)
	_ root.Named         = (*Graph2DErrors)(nil)
	_ Graph              = (*Graph2DErrors)(nil)
	_ rbytes.Marshaler   = (*Graph2DErrors)(nil)
	_ rbytes.Unmarshaler = (*Graph2DErrors)(nil)
)
