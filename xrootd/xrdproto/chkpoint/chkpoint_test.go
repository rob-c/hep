// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package chkpoint_test

import (
	"reflect"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/chkpoint"
	"go-hep.org/x/hep/xrootd/xrdproto/pgwrite"
	"go-hep.org/x/hep/xrootd/xrdproto/read"
	"go-hep.org/x/hep/xrootd/xrdproto/sync"
	"go-hep.org/x/hep/xrootd/xrdproto/truncate"
	"go-hep.org/x/hep/xrootd/xrdproto/write"
)

func TestRequest(t *testing.T) {
	handle := xrdfs.FileHandle{1, 2, 3, 4}

	for _, want := range []chkpoint.Request{
		{Handle: handle, SubCode: chkpoint.Begin},
		{Handle: handle, SubCode: chkpoint.Commit},
		{Handle: handle, SubCode: chkpoint.Query},
		{Handle: handle, SubCode: chkpoint.Rollback},
	} {
		var (
			w   xrdenc.WBuffer
			got chkpoint.Request
		)
		if err := want.MarshalXrd(&w); err != nil {
			t.Fatalf("could not marshal request: %v", err)
		}
		if err := got.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
			t.Fatalf("could not unmarshal request: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round trip:\ngot = %#v\nwant = %#v", got, want)
		}
		if got.ReqID() != chkpoint.RequestID {
			t.Fatalf("request id: got = %d, want = %d", got.ReqID(), chkpoint.RequestID)
		}
		if !got.ShouldSign() {
			t.Fatalf("a checkpoint asked not to be signed")
		}
	}
}

func TestNewXeq(t *testing.T) {
	handle := xrdfs.FileHandle{1, 2, 3, 4}

	for _, tc := range []struct {
		name string
		req  xrdproto.Request
	}{
		{"write", &write.Request{Handle: handle, Offset: 8, Data: []byte("data")}},
		{"pgwrite", &pgwrite.Request{Handle: handle, Offset: 0, Data: []byte("data")}},
		{"truncate", &truncate.Request{Handle: handle, Size: 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := chkpoint.NewXeq(handle, tc.req)
			if err != nil {
				t.Fatalf("could not enclose a %s: %v", tc.name, err)
			}
			if req.SubCode != chkpoint.Xeq {
				t.Fatalf("sub-code: got = %d, want = %d", req.SubCode, chkpoint.Xeq)
			}
			if len(req.Header) != 24 {
				t.Fatalf("the enclosed header is %d bytes, want 24", len(req.Header))
			}
			// The enclosed header names the request it carries, and carries no
			// stream id: the answer comes back on the outer frame's.
			if got := uint16(req.Header[2])<<8 | uint16(req.Header[3]); got != tc.req.ReqID() {
				t.Fatalf("the enclosed header says request %d, want %d", got, tc.req.ReqID())
			}
			if req.Header[0] != 0 || req.Header[1] != 0 {
				t.Fatalf("the enclosed header carries a stream id: %v", req.Header[:2])
			}
		})
	}
}

func TestNewXeqRejectsWhatCannotBeUndone(t *testing.T) {
	// A checkpoint can only undo a modification it knows how to reverse.
	// Enclosing anything else would be answered with an error at best, and at
	// worst run outside the checkpoint — a write believed to be protected that
	// a rollback leaves in place.
	handle := xrdfs.FileHandle{1, 2, 3, 4}

	for _, req := range []xrdproto.Request{
		&read.Request{Handle: handle, Length: 8},
		&sync.Request{Handle: handle},
	} {
		_, err := chkpoint.NewXeq(handle, req)
		if err == nil {
			t.Fatalf("request %d was enclosed in a checkpoint", req.ReqID())
		}
		if !strings.Contains(err.Error(), "checkpoint") {
			t.Fatalf("the error does not say what refused it: %v", err)
		}
	}
}

func TestResponse(t *testing.T) {
	var (
		w    xrdenc.WBuffer
		got  chkpoint.Response
		want = chkpoint.Response{Capacity: 1 << 20, Used: 512}
	)
	if err := want.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal response: %v", err)
	}
	if n := len(w.Bytes()); n != 8 {
		t.Fatalf("a checkpoint query answer is %d bytes, want 8", n)
	}
	if err := got.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
		t.Fatalf("could not unmarshal response: %v", err)
	}
	if got != want {
		t.Fatalf("round trip:\ngot = %+v\nwant = %+v", got, want)
	}
	if got.RespID() != chkpoint.RequestID {
		t.Fatalf("response id: got = %d, want = %d", got.RespID(), chkpoint.RequestID)
	}
}
