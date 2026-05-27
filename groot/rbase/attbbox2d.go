// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rbase

import (
	"reflect"

	"go-hep.org/x/hep/groot/rbytes"
	"go-hep.org/x/hep/groot/root"
	"go-hep.org/x/hep/groot/rtypes"
	"go-hep.org/x/hep/groot/rvers"
)

type AttBBox2D struct{}

func NewAttBBox2D() *AttBBox2D {
	return &AttBBox2D{}
}

func (*AttBBox2D) Class() string {
	return "TAttBBox2D"
}

func (*AttBBox2D) RVersion() int16 {
	return rvers.AttBBox2D
}

func (box *AttBBox2D) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(box.Class(), box.RVersion())
	return w.SetHeader(hdr)
}

func (box *AttBBox2D) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(box.Class(), box.RVersion())
	r.CheckHeader(hdr)
	return r.Err()
}

func (box *AttBBox2D) RMembers() []rbytes.Member {
	return []rbytes.Member{}
}

func init() {
	f := func() reflect.Value {
		o := NewAttBBox2D()
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TAttBBox2D", f)
}

var (
	_ root.Object        = (*AttBBox2D)(nil)
	_ rbytes.Marshaler   = (*AttBBox2D)(nil)
	_ rbytes.Unmarshaler = (*AttBBox2D)(nil)
)
