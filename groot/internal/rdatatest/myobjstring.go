// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rdatatest

import (
	"reflect"

	"go-hep.org/x/hep/groot/rbase"
	"go-hep.org/x/hep/groot/rbytes"
	"go-hep.org/x/hep/groot/rdict"
	"go-hep.org/x/hep/groot/rmeta"
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

// MyObjStringVers is a TMyObjString at an arbitrary class version.
//
// Only MyObjStringVersion is a class version groot knows how to read: the
// point of MyObjStringVers is to write files holding a TMyObjString at a
// class version groot doesn't handle, and thus to exercize version skew
// without a C++ ROOT installation.
//
// MyObjStringVers is deliberately not registered with rtypes.Factory: reading
// back a TMyObjString should always go through MyObjString.
type MyObjStringVers struct {
	obj  rbase.Object
	str  string
	vers int16
}

// NewMyObjStringVers creates a new MyObjStringVers at class version vers.
func NewMyObjStringVers(s string, vers int) *MyObjStringVers {
	return &MyObjStringVers{
		obj:  *rbase.NewObject(),
		str:  s,
		vers: int16(vers),
	}
}

func (obj *MyObjStringVers) RVersion() int16 {
	return obj.vers
}

func (*MyObjStringVers) Class() string {
	return "TMyObjString"
}

func (obj *MyObjStringVers) UID() uint32 {
	return obj.obj.UID()
}

func (obj *MyObjStringVers) Name() string {
	return obj.str
}

func (*MyObjStringVers) Title() string {
	return "Collectable string class"
}

func (obj *MyObjStringVers) String() string {
	return obj.str
}

func (obj *MyObjStringVers) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(obj.Class(), obj.RVersion())
	w.WriteObject(&obj.obj)
	w.WriteString(obj.str)
	return w.SetHeader(hdr)
}

var (
	_ root.Object      = (*MyObjStringVers)(nil)
	_ root.UIDer       = (*MyObjStringVers)(nil)
	_ root.Named       = (*MyObjStringVers)(nil)
	_ root.ObjString   = (*MyObjStringVers)(nil)
	_ rbytes.Marshaler = (*MyObjStringVers)(nil)
)

// MyObjRopeVers is a TMyObjRope whose TMyObjString base class sits at an
// arbitrary class version. Like MyObjStringVers, it only ever writes.
//
// TMyObjRope itself keeps its class version: it is the base class that skews,
// just like in the C++ code of MyObjStringSrc.
type MyObjRopeVers struct {
	base MyObjStringVers
}

// NewMyObjRopeVers creates a new MyObjRopeVers whose base class sits at
// class version vers.
func NewMyObjRopeVers(s string, vers int) *MyObjRopeVers {
	return &MyObjRopeVers{
		base: *NewMyObjStringVers(s, vers),
	}
}

func (*MyObjRopeVers) RVersion() int16 {
	return ((*MyObjRope)(nil)).RVersion()
}

func (*MyObjRopeVers) Class() string {
	return "TMyObjRope"
}

func (obj *MyObjRopeVers) UID() uint32 {
	return obj.base.UID()
}

func (obj *MyObjRopeVers) Name() string {
	return obj.base.Name()
}

func (*MyObjRopeVers) Title() string {
	return "Collectable rope class"
}

func (obj *MyObjRopeVers) String() string {
	return obj.base.String()
}

func (obj *MyObjRopeVers) MarshalROOT(w *rbytes.WBuffer) (int, error) {
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

var (
	_ root.Object      = (*MyObjRopeVers)(nil)
	_ root.UIDer       = (*MyObjRopeVers)(nil)
	_ root.Named       = (*MyObjRopeVers)(nil)
	_ root.ObjString   = (*MyObjRopeVers)(nil)
	_ rbytes.Marshaler = (*MyObjRopeVers)(nil)
)

// MyObjStringStreamer returns the StreamerInfo of TMyObjString at class
// version vers, as C++ ROOT would have written it for MyObjStringSrc.
//
// The class checksum is left at zero: it is what C++ ROOT compares two
// dictionaries with, and there is no second dictionary here to compare to.
//
// Please keep it in sync with MyObjString and MyObjStringSrc.
func MyObjStringStreamer(vers int) *rdict.StreamerInfo {
	return rdict.NewCxxStreamerInfo("TMyObjString", int32(vers), 0, []rbytes.StreamerElement{
		rdict.NewStreamerBase(rdict.Element{
			Name:   *rbase.NewNamed("TObject", "Basic ROOT object"),
			Type:   rmeta.Base,
			MaxIdx: [5]int32{0, tobjectCheckSum, 0, 0, 0},
			EName:  "BASE",
		}.New(), 1),
		&rdict.StreamerString{StreamerElement: rdict.Element{
			Name:  *rbase.NewNamed("fString", "wrapped TString"),
			Type:  rmeta.TString,
			Size:  24,
			EName: "TString",
		}.New()},
	})
}

// MyObjRopeStreamer returns the StreamerInfo of TMyObjRope whose TMyObjString
// base class sits at class version base, as C++ ROOT would have written it
// for MyObjStringSrc.
//
// Please keep it in sync with MyObjRope and MyObjStringSrc.
func MyObjRopeStreamer(base int) *rdict.StreamerInfo {
	rope := (*MyObjRope)(nil)
	return rdict.NewCxxStreamerInfo("TMyObjRope", int32(rope.RVersion()), 0, []rbytes.StreamerElement{
		rdict.NewStreamerBase(rdict.Element{
			Name:  *rbase.NewNamed("TMyObjString", "Collectable string class"),
			Type:  rmeta.Base,
			EName: "BASE",
		}.New(), int32(base)),
	})
}

// tobjectCheckSum is the C++ ROOT checksum of TObject, as found in the
// TStreamerBase of every class deriving from it.
const tobjectCheckSum = -1877229523

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
