// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rhist

import (
	"fmt"
	"reflect"

	"go-hep.org/x/hep/groot/rbytes"
	"go-hep.org/x/hep/groot/root"
	"go-hep.org/x/hep/groot/rtypes"
	"go-hep.org/x/hep/groot/rvers"
)

// F2 is a ROOT 2-dim function.
type F2 struct {
	F1
	ymin float64 // Lower bound for the range in y
	ymax float64 // Upper bound for the range in y
	npy  int32   // Number of points along y used for the graphical representation
}

func newF2() *F2 {
	return &F2{F1: *newF1()}
}

// NewF2 creates a ROOT TF2 over [xmin,xmax]x[ymin,ymax] from a formula
// expression, the way TF2(name, expr, xmin, xmax, ymin, ymax) does in C++.
func NewF2(name, expr string, xmin, xmax, ymin, ymax float64) (*F2, error) {
	f1, err := NewF1(name, expr, xmin, xmax)
	if err != nil {
		return nil, err
	}
	f1.ndim = 2

	return &F2{
		F1:   *f1,
		ymin: ymin,
		ymax: ymax,
		npy:  100, // ROOT's default number of points for drawing
	}, nil
}

func (*F2) RVersion() int16 {
	return rvers.F2
}

func (*F2) Class() string {
	return "TF2"
}

// YMin returns the lower bound in y of the range this function is defined over.
func (f *F2) YMin() float64 {
	return f.ymin
}

// YMax returns the upper bound in y of the range this function is defined over.
func (f *F2) YMax() float64 {
	return f.ymax
}

// Func compiles this function and returns it as a Go function of (x,y).
func (f *F2) Func() (func(x, y float64) float64, error) {
	if f.formula == nil {
		return nil, fmt.Errorf("rhist: TF2 %q is not defined by a formula", f.Name())
	}

	fct, err := f.formula.Func()
	if err != nil {
		return nil, fmt.Errorf("rhist: could not compile TF2 %q: %w", f.Name(), err)
	}

	return func(x, y float64) float64 { return fct(x, y) }, nil
}

// Eval evaluates this function at (x,y).
//
// Eval compiles the formula on every call: use Func to evaluate a function
// more than once.
func (f *F2) Eval(x, y float64) (float64, error) {
	fct, err := f.Func()
	if err != nil {
		return 0, err
	}
	return fct(x, y), nil
}

func (f *F2) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(f.Class(), f.RVersion())
	w.WriteObject(&f.F1)
	w.WriteF64(f.ymin)
	w.WriteF64(f.ymax)
	w.WriteI32(f.npy)

	return w.SetHeader(hdr)
}

func (f *F2) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(f.Class(), f.RVersion())
	r.ReadObject(&f.F1)
	f.ymin = r.ReadF64()
	f.ymax = r.ReadF64()
	f.npy = r.ReadI32()

	r.CheckHeader(hdr)
	return r.Err()
}

// F3 is a ROOT 3-dim function.
type F3 struct {
	F2
	zmin float64 // Lower bound for the range in z
	zmax float64 // Upper bound for the range in z
	npz  int32   // Number of points along z used for the graphical representation
}

func newF3() *F3 {
	return &F3{F2: *newF2()}
}

// NewF3 creates a ROOT TF3 over the given box from a formula expression, the
// way TF3(name, expr, xmin, xmax, ymin, ymax, zmin, zmax) does in C++.
func NewF3(name, expr string, xmin, xmax, ymin, ymax, zmin, zmax float64) (*F3, error) {
	f2, err := NewF2(name, expr, xmin, xmax, ymin, ymax)
	if err != nil {
		return nil, err
	}
	f2.ndim = 3

	return &F3{
		F2:   *f2,
		zmin: zmin,
		zmax: zmax,
		npz:  100, // ROOT's default number of points for drawing
	}, nil
}

func (*F3) RVersion() int16 {
	return rvers.F3
}

func (*F3) Class() string {
	return "TF3"
}

// ZMin returns the lower bound in z of the range this function is defined over.
func (f *F3) ZMin() float64 {
	return f.zmin
}

// ZMax returns the upper bound in z of the range this function is defined over.
func (f *F3) ZMax() float64 {
	return f.zmax
}

// Func compiles this function and returns it as a Go function of (x,y,z).
func (f *F3) Func() (func(x, y, z float64) float64, error) {
	if f.formula == nil {
		return nil, fmt.Errorf("rhist: TF3 %q is not defined by a formula", f.Name())
	}

	fct, err := f.formula.Func()
	if err != nil {
		return nil, fmt.Errorf("rhist: could not compile TF3 %q: %w", f.Name(), err)
	}

	return func(x, y, z float64) float64 { return fct(x, y, z) }, nil
}

// Eval evaluates this function at (x,y,z).
//
// Eval compiles the formula on every call: use Func to evaluate a function
// more than once.
func (f *F3) Eval(x, y, z float64) (float64, error) {
	fct, err := f.Func()
	if err != nil {
		return 0, err
	}
	return fct(x, y, z), nil
}

func (f *F3) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(f.Class(), f.RVersion())
	w.WriteObject(&f.F2)
	w.WriteF64(f.zmin)
	w.WriteF64(f.zmax)
	w.WriteI32(f.npz)

	return w.SetHeader(hdr)
}

func (f *F3) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(f.Class(), f.RVersion())
	r.ReadObject(&f.F2)
	f.zmin = r.ReadF64()
	f.zmax = r.ReadF64()
	f.npz = r.ReadI32()

	r.CheckHeader(hdr)
	return r.Err()
}

func init() {
	{
		f := func() reflect.Value {
			o := newF2()
			return reflect.ValueOf(o)
		}
		rtypes.Factory.Add("TF2", f)
	}
	{
		f := func() reflect.Value {
			o := newF3()
			return reflect.ValueOf(o)
		}
		rtypes.Factory.Add("TF3", f)
	}
}

var (
	_ root.Object        = (*F2)(nil)
	_ root.Named         = (*F2)(nil)
	_ rbytes.Marshaler   = (*F2)(nil)
	_ rbytes.Unmarshaler = (*F2)(nil)

	_ root.Object        = (*F3)(nil)
	_ root.Named         = (*F3)(nil)
	_ rbytes.Marshaler   = (*F3)(nil)
	_ rbytes.Unmarshaler = (*F3)(nil)
)
