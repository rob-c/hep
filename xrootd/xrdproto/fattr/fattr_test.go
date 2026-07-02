// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fattr

import (
	"bytes"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
)

func TestGetRequestWire(t *testing.T) {
	req := GetRequest("/a/f.root", "user.tag")
	var w xrdenc.WBuffer
	if err := req.MarshalXrd(&w); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := w.Bytes()
	// params: fhandle[4]=0 subcode=Get numattr=1 options=0 reserved[9]
	wantParams := append([]byte{0, 0, 0, 0, Get, 1, 0}, make([]byte, 9)...)
	if !bytes.Equal(got[:16], wantParams) {
		t.Fatalf("params mismatch:\ngot= % x\nwant=% x", got[:16], wantParams)
	}
	// body: "/a/f.root\0" + [u16 rc=0] + "user.tag\0"
	wantBody := append([]byte("/a/f.root\x00"), 0, 0)
	wantBody = append(wantBody, []byte("user.tag\x00")...)
	if !bytes.Equal(got[20:], wantBody) {
		t.Fatalf("body mismatch:\ngot= % x\nwant=% x", got[20:], wantBody)
	}
}

func TestSetRequestWire(t *testing.T) {
	req := SetRequest("/a", "user.k", []byte("v1"), true)
	if req.Subcode != Set || req.Options != IsNew || req.NumAttr != 1 {
		t.Fatalf("set request fields: %+v", req)
	}
	// body tail: vvec = [i32 BE 2]["v1"]
	wantTail := []byte{0, 0, 0, 2, 'v', '1'}
	if !bytes.HasSuffix(req.Body, wantTail) {
		t.Fatalf("vvec tail mismatch: % x", req.Body)
	}
}

func TestResponseAttr(t *testing.T) {
	// errcount=0 numattr=1, nvec [rc=0]["user.tag\0"], vvec [len=3]["abc"]
	raw := []byte{0, 1, 0, 0}
	raw = append(raw, []byte("user.tag\x00")...)
	raw = append(raw, 0, 0, 0, 3, 'a', 'b', 'c')
	resp := Response{Raw: raw}
	name, rc, value, err := resp.Attr()
	if err != nil {
		t.Fatalf("Attr: %v", err)
	}
	if name != "user.tag" || rc != 0 || string(value) != "abc" {
		t.Fatalf("Attr: name=%q rc=%d value=%q", name, rc, value)
	}
}

func TestResponseNames(t *testing.T) {
	resp := Response{Raw: []byte("user.a\x00user.b\x00")}
	names, err := resp.Names()
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	if len(names) != 2 || names[0] != "user.a" || names[1] != "user.b" {
		t.Fatalf("Names: %v", names)
	}
}
