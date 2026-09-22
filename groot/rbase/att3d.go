// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rbase

import (
	"reflect"

	"go-hep.org/x/hep/groot/rbytes"
	"go-hep.org/x/hep/groot/rtypes"
	"go-hep.org/x/hep/groot/rvers"
)

// Att3D describes the 3D attributes of a ROOT object.
//
// Like its C++ counterpart it holds nothing: TAtt3D is a marker a class
// inherits from to say it can be drawn in three dimensions. It still streams
// a header of its own, which is what makes it worth a type here.
type Att3D struct{}

func NewAtt3D() *Att3D {
	return &Att3D{}
}

func (*Att3D) Class() string {
	return "TAtt3D"
}

func (*Att3D) RVersion() int16 {
	return rvers.Att3D
}

func (att *Att3D) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(att.Class(), att.RVersion())
	return w.SetHeader(hdr)
}

func (att *Att3D) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(att.Class(), att.RVersion())
	r.CheckHeader(hdr)
	return r.Err()
}

func (att *Att3D) RMembers() []rbytes.Member {
	return []rbytes.Member{}
}

func init() {
	f := func() reflect.Value {
		o := NewAtt3D()
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TAtt3D", f)
}

var (
	_ rbytes.Marshaler   = (*Att3D)(nil)
	_ rbytes.Unmarshaler = (*Att3D)(nil)
	_ rbytes.RSlicer     = (*Att3D)(nil)
)
