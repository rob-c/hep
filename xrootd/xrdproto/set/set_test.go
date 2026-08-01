// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package set_test

import (
	"reflect"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto/set"
)

func TestRequest(t *testing.T) {
	for _, want := range []set.Request{
		{Data: set.AppIDPrefix + "analysis-42"},
		{Data: "monitor 1"},
		{Data: ""},
	} {
		var (
			w   xrdenc.WBuffer
			got set.Request
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
	}
}

func TestRequestLayout(t *testing.T) {
	// The whole parameter area is reserved: everything a set says is in the
	// directive text.
	req := set.Request{Data: set.AppIDPrefix + "analysis-42"}

	var w xrdenc.WBuffer
	if err := req.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal request: %v", err)
	}
	raw := w.Bytes()

	for i, b := range raw[:16] {
		if b != 0 {
			t.Fatalf("reserved parameter byte %d is %d, want 0", i, b)
		}
	}
	if got, want := string(raw[20:]), req.Data; got != want {
		t.Fatalf("payload: got = %q, want = %q", got, want)
	}

	if got, want := req.ReqID(), set.RequestID; got != want {
		t.Fatalf("ReqID = %d, want %d", got, want)
	}
	if req.ShouldSign() {
		// The level table decides that a set is signed from Standard up; the
		// request has no opinion of its own to add.
		t.Fatal("a set request asks to be signed")
	}
}

func TestResponse(t *testing.T) {
	want := set.Response{Data: []byte("ok")}

	var (
		w   xrdenc.WBuffer
		got set.Response
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
	if got, want := got.RespID(), set.RequestID; got != want {
		t.Fatalf("RespID = %d, want %d", got, want)
	}
}
