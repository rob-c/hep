// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package readlink_test

import (
	"reflect"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto/readlink"
)

func TestRequest(t *testing.T) {
	want := readlink.Request{Path: "/data/by-tag/latest.root"}

	var (
		w   xrdenc.WBuffer
		got readlink.Request
	)
	if err := want.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal request: %v", err)
	}
	if err := got.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
		t.Fatalf("could not unmarshal request: %v", err)
	}
	if got != want {
		t.Fatalf("round trip:\ngot  = %#v\nwant = %#v", got, want)
	}
	if got, want := got.ReqID(), readlink.RequestID; got != want {
		t.Fatalf("ReqID = %d, want %d", got, want)
	}
	if got.ShouldSign() {
		t.Fatal("a readlink request asks to be signed")
	}
}

func TestRequestOpaque(t *testing.T) {
	// A link is named like any other path, so the authorization token an open
	// travels with is carried the same way and is not part of the name.
	r := readlink.Request{Path: "/data/latest.root?authz=token"}
	if got, want := r.Opaque(), "authz=token"; got != want {
		t.Fatalf("Opaque = %q, want %q", got, want)
	}

	r = readlink.Request{Path: "/data/latest.root"}
	r.SetOpaque("authz=other")
	if got, want := r.Path, "/data/latest.root?authz=other"; got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

func TestResponse(t *testing.T) {
	want := readlink.Response{Data: []byte("/data/raw/run42.root")}

	var (
		w   xrdenc.WBuffer
		got readlink.Response
	)
	if err := want.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal response: %v", err)
	}
	if err := got.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
		t.Fatalf("could not unmarshal response: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip:\ngot  = %#v\nwant = %#v", got, want)
	}
	if got, want := got.RespID(), readlink.RequestID; got != want {
		t.Fatalf("RespID = %d, want %d", got, want)
	}
}
