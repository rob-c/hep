// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pgread

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
	req := Request{Handle: xrdfs.FileHandle{1, 2, 3, 4}, Offset: 8192, ReadLength: 1 << 20}
	var w xrdenc.WBuffer
	if err := req.MarshalXrd(&w); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(w.Bytes()) != 20 { // 16 params + 4 dlen
		t.Fatalf("marshaled length: got=%d want=20", len(w.Bytes()))
	}
	var back Request
	if err := back.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back != req {
		t.Fatalf("roundtrip mismatch: got=%+v want=%+v", back, req)
	}
	if req.ReqID() != RequestID {
		t.Fatalf("ReqID: got=%d want=%d", req.ReqID(), RequestID)
	}
}

// statusFrameFor builds one pg status frame carrying data at off.
func statusFrameFor(off int64, data []byte, resptype uint8) []byte {
	pages := pgbuf.Encode(off, data)
	info := make([]byte, 8)
	binary.BigEndian.PutUint64(info, uint64(off))
	frame := xrdproto.StatusFrame(xrdproto.StatusBody{
		RequestID:  uint8(RequestID - 3000),
		RespType:   resptype,
		DataLength: int32(len(pages)),
	}, info)
	return append(frame, pages...)
}

func TestResponseUnmarshalTwoFrames(t *testing.T) {
	part1 := bytes.Repeat([]byte{0x11}, pgbuf.PageSize)
	part2 := bytes.Repeat([]byte{0x22}, 100)
	wire := append(
		statusFrameFor(0, part1, xrdproto.PartialResult),
		statusFrameFor(int64(len(part1)), part2, xrdproto.FinalResult)...,
	)

	var resp Response
	if err := resp.UnmarshalXrd(xrdenc.NewRBuffer(wire)); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if want := append(append([]byte{}, part1...), part2...); !bytes.Equal(resp.Data, want) {
		t.Fatalf("data mismatch: got %d bytes want %d", len(resp.Data), len(want))
	}
	if resp.Offset != 0 {
		t.Fatalf("offset: got=%d want=0", resp.Offset)
	}
}

func TestResponseCorruptPage(t *testing.T) {
	wire := statusFrameFor(0, bytes.Repeat([]byte{0x33}, 64), xrdproto.FinalResult)
	wire[len(wire)-1] ^= 0xff // corrupt page data
	var resp Response
	if err := resp.UnmarshalXrd(xrdenc.NewRBuffer(wire)); err == nil {
		t.Fatal("accepted corrupt page data")
	}
}
