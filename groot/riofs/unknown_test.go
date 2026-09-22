// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package riofs_test

import (
	"path/filepath"
	"strings"
	"testing"

	"go-hep.org/x/hep/groot"
	"go-hep.org/x/hep/groot/rbase"
	"go-hep.org/x/hep/groot/rbytes"
	"go-hep.org/x/hep/groot/rdict"
	"go-hep.org/x/hep/groot/rmeta"
	"go-hep.org/x/hep/groot/root"
)

// TestReadClassWithoutAGoType checks a class groot has no Go type for can
// still be read, through the streamer the file carries for it.
//
// The file is written with a Go type registered under a made-up class name,
// and then read in a state where that registration has been taken away
// again, which is the situation somebody else's file puts groot in.
func TestReadClassWithoutAGoType(t *testing.T) {
	const class = "TUserClass"

	// a streamer describing two doubles and a string, and a Go type to match.
	si := rdict.NewCxxStreamerInfo(class, 1, 0, []rbytes.StreamerElement{
		rdict.NewStreamerBase(rdict.Element{
			Name:   *rbase.NewNamed("TObject", "Basic ROOT object"),
			Type:   rmeta.Base,
			MaxIdx: [5]int32{0, -1877229523, 0, 0, 0},
			EName:  "BASE",
		}.New(), 1),
		&rdict.StreamerBasicType{StreamerElement: rdict.Element{
			Name:  *rbase.NewNamed("fX", "an x"),
			Type:  rmeta.Double,
			Size:  8,
			EName: "double",
		}.New()},
		&rdict.StreamerBasicType{StreamerElement: rdict.Element{
			Name:  *rbase.NewNamed("fN", "a count"),
			Type:  rmeta.Int,
			Size:  4,
			EName: "int",
		}.New()},
	})
	rdict.StreamerInfos.Add(si)

	fname := filepath.Join(t.TempDir(), "user.root")

	// Write one with a Go type standing in for the C++ class. The type is
	// never registered with rtypes.Factory, which is only consulted on the
	// way in: so reading it back lands in exactly the state somebody else's
	// file puts groot in.
	func() {
		f, err := groot.Create(fname)
		if err != nil {
			t.Fatalf("could not create ROOT file: %+v", err)
		}
		err = f.Put("obj", &userClass{x: 42.5, n: 7})
		if err != nil {
			t.Fatalf("could not write the object: %+v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("could not close ROOT file: %+v", err)
		}
	}()

	// and read it back with no Go type registered for it at all.
	r, err := groot.Open(fname)
	if err != nil {
		t.Fatalf("could not open ROOT file: %+v", err)
	}
	defer r.Close()

	obj, err := r.Get("obj")
	if err != nil {
		t.Fatalf("could not read the object: %+v", err)
	}

	if got, want := obj.Class(), class; got != want {
		t.Fatalf("class: got=%q, want=%q", got, want)
	}

	gen, ok := obj.(*rdict.Object)
	if !ok {
		t.Fatalf("got a %T, want a *rdict.Object", obj)
	}

	// the members must have come through.
	str := gen.String()
	for _, want := range []string{"42.5", "7"} {
		if !strings.Contains(str, want) {
			t.Errorf("the value %q is missing from %q", want, str)
		}
	}
}

// userClass stands in for a C++ class groot has no Go type for.
type userClass struct {
	obj rbase.Object
	x   float64
	n   int32
}

func (*userClass) Class() string   { return "TUserClass" }
func (*userClass) RVersion() int16 { return 1 }

func (u *userClass) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}
	hdr := w.WriteHeader(u.Class(), u.RVersion())
	w.WriteObject(&u.obj)
	w.WriteF64(u.x)
	w.WriteI32(u.n)
	return w.SetHeader(hdr)
}

func (u *userClass) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}
	hdr := r.ReadHeader(u.Class(), u.RVersion())
	r.ReadObject(&u.obj)
	u.x = r.ReadF64()
	u.n = r.ReadI32()
	r.CheckHeader(hdr)
	return r.Err()
}

var (
	_ root.Object        = (*userClass)(nil)
	_ rbytes.Marshaler   = (*userClass)(nil)
	_ rbytes.Unmarshaler = (*userClass)(nil)
)
