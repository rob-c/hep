// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pgwrite

import (
	"bytes"
	"encoding/binary"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/pgbuf"
	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

func TestRequestMarshalRoundTrip(t *testing.T) {
	data := bytes.Repeat([]byte{0x7e}, pgbuf.PageSize+10)
	req := Request{Handle: xrdfs.FileHandle{9, 8, 7, 6}, Offset: 4096, Data: data}
	var w xrdenc.WBuffer
	if err := req.MarshalXrd(&w); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// 16 params + 4 dlen + encoded page units.
	if want := 20 + pgbuf.EncodedLen(4096, len(data)); len(w.Bytes()) != want {
		t.Fatalf("marshaled length: got=%d want=%d", len(w.Bytes()), want)
	}
	var back Request
	if err := back.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Handle != req.Handle || back.Offset != req.Offset || !bytes.Equal(back.Data, data) {
		t.Fatal("roundtrip mismatch")
	}
}

func TestResponseUnmarshal(t *testing.T) {
	info := make([]byte, 8)
	binary.BigEndian.PutUint64(info, 12345)
	frame := xrdproto.StatusFrame(xrdproto.StatusBody{
		RequestID: uint8(RequestID - 3000),
		RespType:  xrdproto.FinalResult,
	}, info)
	var resp Response
	if err := resp.UnmarshalXrd(xrdenc.NewRBuffer(frame)); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Offset != 12345 {
		t.Fatalf("offset: got=%d want=12345", resp.Offset)
	}
}
