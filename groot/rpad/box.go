// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rpad

import (
	"reflect"

	"go-hep.org/x/hep/groot/rbase"
	"go-hep.org/x/hep/groot/rbytes"
	"go-hep.org/x/hep/groot/root"
	"go-hep.org/x/hep/groot/rtypes"
	"go-hep.org/x/hep/groot/rvers"
)

// Box is a ROOT TBox, defined by :
//
// - Its bottom left coordinates x1,y1
// - Its top right coordinates x2,y2
type Box struct {
	obj     rbase.Object
	attline rbase.AttLine
	attfill rbase.AttFill
	attbbox rbase.AttBBox2D

	x1 float64 // X of 1st point
	y1 float64 // Y of 1st point
	x2 float64 // X of 2nd point
	y2 float64 // Y of 2nd point
}

func NewBox() *Box {
	return &Box{}
}

func (*Box) Class() string {
	return "TBox"
}

func (*Box) RVersion() int16 {
	return rvers.Box
}

func (box *Box) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(box.Class(), box.RVersion())
	w.WriteObject(&box.obj)
	w.WriteObject(&box.attline)
	w.WriteObject(&box.attfill)
	w.WriteObject(&box.attbbox)

	w.WriteF64(box.x1)
	w.WriteF64(box.y1)
	w.WriteF64(box.x2)
	w.WriteF64(box.y2)
	return w.SetHeader(hdr)
}

func (box *Box) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(box.Class(), box.RVersion())
	if hdr.Vers <= 1 {
		box.x1 = float64(r.ReadF32())
		box.y1 = float64(r.ReadF32())
		box.x2 = float64(r.ReadF32())
		box.y2 = float64(r.ReadF32())
	} else {
		r.ReadObject(&box.obj)
		r.ReadObject(&box.attline)
		r.ReadObject(&box.attfill)
		r.ReadObject(&box.attbbox)

		box.x1 = r.ReadF64()
		box.y1 = r.ReadF64()
		box.x2 = r.ReadF64()
		box.y2 = r.ReadF64()
	}

	r.CheckHeader(hdr)
	return r.Err()
}

func (box *Box) RMembers() []rbytes.Member {
	o := box.obj.RMembers()
	o = append(o, box.attline.RMembers()...)
	o = append(o, box.attfill.RMembers()...)
	o = append(o, box.attbbox.RMembers()...)
	return append(o, []rbytes.Member{
		{Name: "fX1", Value: &box.x1},
		{Name: "fY1", Value: &box.y1},
		{Name: "fX2", Value: &box.x2},
		{Name: "fY2", Value: &box.y2},
	}...)
}

func init() {
	f := func() reflect.Value {
		o := NewBox()
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TBox", f)
}

var (
	_ root.Object        = (*Box)(nil)
	_ rbytes.Marshaler   = (*Box)(nil)
	_ rbytes.Unmarshaler = (*Box)(nil)
)
