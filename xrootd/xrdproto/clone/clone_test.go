// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clone

import (
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
)

func TestRequestRoundTrip(t *testing.T) {
	want := Request{
		Dst: xrdfs.FileHandle{1, 2, 3, 4},
		Items: []Item{
			{Src: xrdfs.FileHandle{5, 6, 7, 8}, SrcOffset: 4096, SrcLength: 1024, DstOffset: 0},
			{Src: xrdfs.FileHandle{9, 10, 11, 12}, SrcOffset: 0, SrcLength: 16, DstOffset: 1024},
		},
	}

	var w xrdenc.WBuffer
	if err := want.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal the request: %v", err)
	}

	// 4 bytes of handle, 12 reserved, the 4-byte dlen, then the items.
	if got, want := len(w.Bytes()), 20+2*ItemLength; got != want {
		t.Fatalf("the request is %d bytes, want %d", got, want)
	}

	var got Request
	if err := got.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
		t.Fatalf("could not unmarshal the request: %v", err)
	}
	if got.Dst != want.Dst || len(got.Items) != len(want.Items) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want.Items {
		if got.Items[i] != want.Items[i] {
			t.Fatalf("item %d is %+v, want %+v", i, got.Items[i], want.Items[i])
		}
	}
}

func TestRequestInterfaces(t *testing.T) {
	req := NewRequest(xrdfs.FileHandle{1}, nil)
	if got := req.ReqID(); got != RequestID {
		t.Fatalf("ReqID() = %d, want %d", got, RequestID)
	}
	if req.ShouldSign() {
		// A clone is signed by the security level, as a write is, and not by
		// asking the request itself.
		t.Fatal("a clone asks to be signed on its own")
	}
	req.SetHandle(xrdfs.FileHandle{7, 7, 7, 7})
	if got, want := req.Dst, (xrdfs.FileHandle{7, 7, 7, 7}); got != want {
		t.Fatalf("SetHandle left the destination at %v, want %v", got, want)
	}

	var resp Response
	if got := resp.RespID(); got != RequestID {
		t.Fatalf("RespID() = %d, want %d", got, RequestID)
	}
	var w xrdenc.WBuffer
	if err := resp.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal the response: %v", err)
	}
	if got := len(w.Bytes()); got != 0 {
		t.Fatalf("the response is %d bytes, want an empty body", got)
	}
	if err := resp.UnmarshalXrd(xrdenc.NewRBuffer(nil)); err != nil {
		t.Fatalf("could not unmarshal the response: %v", err)
	}
}

func TestRequestMarshalRefusesTooManyItems(t *testing.T) {
	req := Request{Items: make([]Item, MaxItems+1)}
	var w xrdenc.WBuffer
	err := req.MarshalXrd(&w)
	if err == nil {
		t.Fatal("a list longer than the server accepts was encoded")
	}
	if !strings.Contains(err.Error(), "too many clone items") {
		t.Fatalf("the failure says %q, want it to name the limit", err)
	}
}

func TestRequestMarshalRefusesInvalidItems(t *testing.T) {
	req := Request{Items: []Item{{SrcOffset: -1}}}
	var w xrdenc.WBuffer
	err := req.MarshalXrd(&w)
	if err == nil {
		t.Fatal("an item naming a range no file has was encoded")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("the failure says %q, want it to name the range", err)
	}
}

func TestRequestUnmarshalRefusesAMalformedList(t *testing.T) {
	// The payload is a whole number of fixed-size items, and a length that is
	// not one means the sender and the reader disagree about the layout: every
	// item after the first would be read from the middle of its neighbour.
	var w xrdenc.WBuffer
	w.WriteBytes(make([]byte, 16))
	w.WriteLen(ItemLength + 1)
	w.WriteBytes(make([]byte, ItemLength+1))

	var req Request
	err := req.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes()))
	if err == nil {
		t.Fatal("a payload that is not a whole number of items was decoded")
	}
	if !strings.Contains(err.Error(), "malformed clone list") {
		t.Fatalf("the failure says %q, want it to name the list", err)
	}
}

func TestItemValidate(t *testing.T) {
	const maxInt64 = int64(^uint64(0) >> 1)

	for _, tc := range []struct {
		name string
		item Item
		ok   bool
	}{
		{"an empty range", Item{}, true},
		{"an ordinary range", Item{SrcOffset: 1 << 30, SrcLength: 4096, DstOffset: 0}, true},
		{"a range at the end of the address space", Item{SrcOffset: maxInt64 - 1, SrcLength: 1}, true},
		{"a negative source offset", Item{SrcOffset: -1}, false},
		{"a negative destination offset", Item{DstOffset: -1}, false},
		{"a negative length", Item{SrcLength: -1}, false},
		{"a source range that does not fit", Item{SrcOffset: maxInt64, SrcLength: 1}, false},
		{"a destination range that does not fit", Item{DstOffset: maxInt64, SrcLength: 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.item.Validate()
			if got := err == nil; got != tc.ok {
				t.Fatalf("Validate() = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

func TestRequestUnmarshalRefusesATruncatedList(t *testing.T) {
	// A frame that says it carries two items and holds one is a frame that was
	// cut, and the second item would otherwise decode as zeros — a range that
	// copies nothing from a handle nobody issued.
	req := Request{
		Dst:   xrdfs.FileHandle{1, 2, 3, 4},
		Items: []Item{{SrcLength: 8}, {SrcLength: 8, DstOffset: 8}},
	}
	var w xrdenc.WBuffer
	if err := req.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal the request: %v", err)
	}

	for _, n := range []int{4, 16, 20, 20 + ItemLength, len(w.Bytes()) - 1} {
		var got Request
		if err := got.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes()[:n])); err == nil {
			t.Fatalf("a %d-byte prefix of the request was decoded as %+v", n, got)
		}
	}
}
