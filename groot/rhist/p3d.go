// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rhist

import (
	"fmt"
	"reflect"

	"go-hep.org/x/hep/groot/rbytes"
	"go-hep.org/x/hep/groot/rcont"
	"go-hep.org/x/hep/groot/root"
	"go-hep.org/x/hep/groot/rtypes"
	"go-hep.org/x/hep/groot/rvers"
)

// Profile3D is a 3-dim profile histogram.
type Profile3D struct {
	h3d        H3D          // base class
	binEntries rcont.ArrayD // number of entries per bin
	errMode    int32        // Option to compute errors
	tmin       float64      // Lower limit in T (if set)
	tmax       float64      // Upper limit in T (if set)
	sumwt      float64      // Total Sum of weight*T
	sumwt2     float64      // Total Sum of weight*T*T
	binSumw2   rcont.ArrayD // Array of sum of squares of weights per bin
}

func newProfile3D() *Profile3D {
	return &Profile3D{
		h3d: *newH3D(),
	}
}

func (*Profile3D) Class() string {
	return "TProfile3D"
}

func (*Profile3D) RVersion() int16 {
	return rvers.Profile3D
}

func (p3d *Profile3D) Name() string  { return p3d.h3d.Name() }
func (p3d *Profile3D) Title() string { return p3d.h3d.Title() }

// BinEntries returns the number of entries in each bin.
func (p3d *Profile3D) BinEntries() []float64 {
	return p3d.binEntries.Data
}

// TMin returns the lower limit in T, if one was set.
func (p3d *Profile3D) TMin() float64 { return p3d.tmin }

// TMax returns the upper limit in T, if one was set.
func (p3d *Profile3D) TMax() float64 { return p3d.tmax }

// MarshalROOT implements rbytes.Marshaler
func (p3d *Profile3D) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(p3d.Class(), p3d.RVersion())

	w.WriteObject(&p3d.h3d)
	w.WriteObject(&p3d.binEntries)
	w.WriteI32(p3d.errMode)
	w.WriteF64(p3d.tmin)
	w.WriteF64(p3d.tmax)
	w.WriteF64(p3d.sumwt)
	w.WriteF64(p3d.sumwt2)
	w.WriteObject(&p3d.binSumw2)

	return w.SetHeader(hdr)
}

// UnmarshalROOT implements rbytes.Unmarshaler
func (p3d *Profile3D) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(p3d.Class(), p3d.RVersion())
	if hdr.Vers < 8 {
		return fmt.Errorf("rhist: TProfile3D version too old (%d<8)", hdr.Vers)
	}

	r.ReadObject(&p3d.h3d)
	r.ReadObject(&p3d.binEntries)
	p3d.errMode = r.ReadI32()
	p3d.tmin = r.ReadF64()
	p3d.tmax = r.ReadF64()
	p3d.sumwt = r.ReadF64()
	p3d.sumwt2 = r.ReadF64()
	r.ReadObject(&p3d.binSumw2)

	r.CheckHeader(hdr)
	return r.Err()
}

func init() {
	f := func() reflect.Value {
		p3d := newProfile3D()
		return reflect.ValueOf(p3d)
	}
	rtypes.Factory.Add("TProfile3D", f)
}

var (
	_ root.Object        = (*Profile3D)(nil)
	_ root.Named         = (*Profile3D)(nil)
	_ rbytes.RVersioner  = (*Profile3D)(nil)
	_ rbytes.Marshaler   = (*Profile3D)(nil)
	_ rbytes.Unmarshaler = (*Profile3D)(nil)
)
