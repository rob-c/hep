// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rdatatest

import (
	"reflect"

	"go-hep.org/x/hep/groot/rbase"
	"go-hep.org/x/hep/groot/rbytes"
	"go-hep.org/x/hep/groot/root"
	"go-hep.org/x/hep/groot/rtypes"
)

const MyObjStringVersion = 1

// MyObjString is a copy of TObjString, to exercize version skew.
type MyObjString struct {
	obj rbase.Object
	str string
}

// NewMyObjString creates a new ObjString.
func NewMyObjString(s string) *MyObjString {
	return &MyObjString{
		obj: *rbase.NewObject(),
		str: s,
	}
}

func (*MyObjString) RVersion() int16 {
	return MyObjStringVersion
}

func (*MyObjString) Class() string {
	return "TMyObjString"
}

func (obj *MyObjString) UID() uint32 {
	return obj.obj.UID()
}

func (obj *MyObjString) Name() string {
	return obj.str
}

func (*MyObjString) Title() string {
	return "Collectable string class"
}

func (obj *MyObjString) String() string {
	return obj.str
}

// ROOTUnmarshaler is the interface implemented by an object that can
// unmarshal itself from a ROOT buffer
func (obj *MyObjString) UnmarshalROOT(r *rbytes.RBuffer) error {
	hdr := r.ReadHeader(obj.Class(), obj.RVersion())
	r.ReadObject(&obj.obj)
	obj.str = r.ReadString()

	r.CheckHeader(hdr)
	return r.Err()
}

func (obj *MyObjString) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(obj.Class(), obj.RVersion())
	w.WriteObject(&obj.obj)
	w.WriteString(obj.str)
	return w.SetHeader(hdr)
}

func init() {
	f := func() reflect.Value {
		o := &MyObjString{}
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TMyObjString", f)
}

var (
	_ root.Object        = (*MyObjString)(nil)
	_ root.UIDer         = (*MyObjString)(nil)
	_ root.Named         = (*MyObjString)(nil)
	_ root.ObjString     = (*MyObjString)(nil)
	_ rbytes.Marshaler   = (*MyObjString)(nil)
	_ rbytes.Unmarshaler = (*MyObjString)(nil)
)

// MyObjRope inherits from MyObjString, to exercize version skew.
type MyObjRope struct {
	base MyObjString
}

// NewObjRope creates a new MyObjRope.
func NewMyObjRope(s string) *MyObjRope {
	return &MyObjRope{
		base: *NewMyObjString(s),
	}
}

func (*MyObjRope) RVersion() int16 {
	return 1
}

func (*MyObjRope) Class() string {
	return "TMyObjRope"
}

func (obj *MyObjRope) UID() uint32 {
	return obj.base.UID()
}

func (obj *MyObjRope) Name() string {
	return obj.base.Name()
}

func (*MyObjRope) Title() string {
	return "Collectable rope class"
}

func (obj *MyObjRope) String() string {
	return obj.base.String()
}

// ROOTUnmarshaler is the interface implemented by an object that can
// unmarshal itself from a ROOT buffer
func (obj *MyObjRope) UnmarshalROOT(r *rbytes.RBuffer) error {
	hdr := r.ReadHeader(obj.Class(), obj.RVersion())
	err := obj.base.UnmarshalROOT(r)
	if err != nil {
		return err
	}

	r.CheckHeader(hdr)
	return r.Err()
}

func (obj *MyObjRope) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(obj.Class(), obj.RVersion())
	n, err := obj.base.MarshalROOT(w)
	if err != nil {
		return n, err
	}
	return w.SetHeader(hdr)
}

func init() {
	f := func() reflect.Value {
		o := &MyObjRope{}
		return reflect.ValueOf(o)
	}
	rtypes.Factory.Add("TMyObjRope", f)
}

var (
	_ root.Object        = (*MyObjRope)(nil)
	_ root.UIDer         = (*MyObjRope)(nil)
	_ root.Named         = (*MyObjRope)(nil)
	_ root.ObjString     = (*MyObjRope)(nil)
	_ rbytes.Marshaler   = (*MyObjRope)(nil)
	_ rbytes.Unmarshaler = (*MyObjRope)(nil)
)

// MyObjStringSrc is the ROOT/C++ code corresponding to rdatatest.MyObjString.
//
// Please keep both in sync.
const MyObjStringSrc = `
#include "TObject.h"
#include "TString.h"

class TMyObjString : public TObject {
private:
	TString fString; // wrapped TString.
public:
	TMyObjString(const char *s = "") : fString(s) {}
	~TMyObjString();
	Int_t       Compare(const TObject *obj) const override;
	TString     CopyString() const { return fString; }
	const char *GetName() const override { return fString; }
	ULong_t     Hash() const override { return fString.Hash(); }
	void        FillBuffer(char *&buffer) { fString.FillBuffer(buffer); }
	void        Print(Option_t *) const override { Printf("TMyObjString = %%s", (const char*)fString); }
	Bool_t      IsSortable() const override { return kTRUE; }
	Bool_t      IsEqual(const TObject *obj) const override;
	void        ReadBuffer(char *&buffer) { fString.ReadBuffer(buffer); }
	void        SetString(const char *s) { fString = s; }
	const TString &GetString() const { return fString; }
	Int_t       Sizeof() const { return fString.Sizeof(); }
	TString    &String() { return fString; }

	ClassDefOverride(TMyObjString, %d)  //Collectable string class
};

// implementation.

#include "TROOT.h"

TMyObjString::~TMyObjString()
{
   // Required since we overload TObject::Hash.
   ROOT::CallRecursiveRemoveIfNeeded(*this);
}

////////////////////////////////////////////////////////////////////////////////
/// String compare the argument with this object.

Int_t TMyObjString::Compare(const TObject *obj) const
{
   if (this == obj) return 0;
   if (TMyObjString::Class() != obj->IsA()) return -1;
   return fString.CompareTo(((TMyObjString*)obj)->fString);
}

////////////////////////////////////////////////////////////////////////////////
/// Return kTRUE if the argument has the same content as this object.

Bool_t TMyObjString::IsEqual(const TObject *obj) const
{
   if (this == obj) return kTRUE;
   if (TMyObjString::Class() != obj->IsA()) return kFALSE;
   return fString == ((TMyObjString*)obj)->fString;
}

class TMyObjRope : public TMyObjString {
public:
	TMyObjRope(const char *s = "") : TMyObjString(s) {}
	~TMyObjRope();

	ClassDefOverride(TMyObjRope, 1)  //Collectable rope class
};

TMyObjRope::~TMyObjRope()
{
   // Required since we overload TObject::Hash.
   ROOT::CallRecursiveRemoveIfNeeded(*this);
}


#include "TFile.h"
#include "TH1F.h"

void gentmyobjstr(const char *fname) {
	auto f = TFile::Open(fname, "RECREATE");
	auto h = new TH1F("h", "h", 10, 0, 10);
	h->FillRandom("gaus", 5);
	auto o = new TMyObjString("my-%[1]d");
	h->GetListOfFunctions()->Add(o);
	auto u = new TMyObjRope("my-%[1]d");
	h->GetListOfFunctions()->Add(u);

	f->WriteObject(h, "h");

	f->Write();
	f->Close();

	exit(0);
}`
