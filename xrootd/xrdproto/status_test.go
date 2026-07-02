// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdproto

import (
	"encoding/binary"
	"testing"
)

func TestStatusFrameRoundTrip(t *testing.T) {
	info := make([]byte, 8)
	binary.BigEndian.PutUint64(info, 4096) // pg file offset
	body := StatusBody{
		StreamID:   StreamID{0, 7},
		RequestID:  30, // kXR_pgread - 3000
		RespType:   PartialResult,
		DataLength: 4100,
	}
	frame := StatusFrame(body, info)
	if len(frame) != StatusBodyLength+len(info) {
		t.Fatalf("frame length: got=%d want=%d", len(frame), StatusBodyLength+len(info))
	}

	var got StatusBody
	if err := got.UnmarshalVerifyXrd(frame); err != nil {
		t.Fatalf("UnmarshalVerifyXrd: %v", err)
	}
	if got.StreamID != body.StreamID || got.RespType != PartialResult || got.DataLength != 4100 {
		t.Fatalf("decoded body mismatch: %+v", got)
	}

	// Corrupt one info byte: the CRC covers it, so verification must fail.
	frame[len(frame)-1] ^= 0xff
	if err := got.UnmarshalVerifyXrd(frame); err == nil {
		t.Fatal("UnmarshalVerifyXrd accepted a corrupted frame")
	}
}

func TestStatusBodyShortFrame(t *testing.T) {
	var b StatusBody
	if err := b.UnmarshalVerifyXrd(make([]byte, StatusBodyLength-1)); err == nil {
		t.Fatal("expected error for a short status frame")
	}
}
