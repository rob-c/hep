// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdproto // import "go-hep.org/x/hep/xrootd/xrdproto"

import (
	"encoding/binary"
	"fmt"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdsum"
)

// StatusBodyLength is the fixed length of the StatusBody in bytes; an
// operation-specific info tail may follow it inside the response header's
// data length (pg operations append an 8-byte file offset).
const StatusBodyLength = 16

// Response types carried in StatusBody.RespType.
const (
	// FinalResult marks the last kXR_status frame of a response.
	FinalResult uint8 = 0x00
	// PartialResult marks an intermediate frame; more frames follow.
	PartialResult uint8 = 0x01
	// ProgressInfo is a keep-alive progress frame.
	ProgressInfo uint8 = 0x02
)

// StatusBody is the fixed part of a kXR_status response frame. The CRC32C
// field covers every byte of the frame after itself (the body remainder and
// the info tail), per RFC 7143.
type StatusBody struct {
	CRC32C     uint32
	StreamID   StreamID
	RequestID  uint8 // request code minus 3000 (kXR_pgread -> 30)
	RespType   uint8 // FinalResult, PartialResult or ProgressInfo
	Reserved   [4]byte
	DataLength int32 // trailing data bytes that follow the frame on the wire
}

// MarshalXrd implements Marshaler. The stored CRC32C is written as-is;
// use StatusFrame to build a frame with a correctly stamped CRC.
func (o StatusBody) MarshalXrd(wBuffer *xrdenc.WBuffer) error {
	wBuffer.WriteU32(o.CRC32C)
	wBuffer.WriteBytes(o.StreamID[:])
	wBuffer.WriteU8(o.RequestID)
	wBuffer.WriteU8(o.RespType)
	wBuffer.WriteBytes(o.Reserved[:])
	wBuffer.WriteI32(o.DataLength)
	return nil
}

// UnmarshalVerifyXrd decodes the fixed 16-byte body from frame (the complete
// response-header payload, including any info tail) after verifying that the
// leading CRC-32C matches the remainder of the frame.
func (o *StatusBody) UnmarshalVerifyXrd(frame []byte) error {
	if len(frame) < StatusBodyLength {
		return fmt.Errorf("xrootd: kXR_status frame too short: %d bytes (min %d)", len(frame), StatusBodyLength)
	}
	want := binary.BigEndian.Uint32(frame[:4])
	if got := xrdsum.CRC32C(frame[4:]); got != want {
		return fmt.Errorf("xrootd: kXR_status frame CRC mismatch: got=%#08x want=%#08x", got, want)
	}
	o.CRC32C = want
	copy(o.StreamID[:], frame[4:6])
	o.RequestID = frame[6]
	o.RespType = frame[7]
	copy(o.Reserved[:], frame[8:12])
	o.DataLength = int32(binary.BigEndian.Uint32(frame[12:16]))
	return nil
}

// StatusFrame assembles a kXR_status frame from body and the operation
// specific info tail, stamping the CRC-32C over everything after the CRC
// field itself. It is intended for servers and tests.
func StatusFrame(body StatusBody, info []byte) []byte {
	frame := make([]byte, StatusBodyLength+len(info))
	copy(frame[4:6], body.StreamID[:])
	frame[6] = body.RequestID
	frame[7] = body.RespType
	copy(frame[8:12], body.Reserved[:])
	binary.BigEndian.PutUint32(frame[12:16], uint32(body.DataLength))
	copy(frame[16:], info)
	binary.BigEndian.PutUint32(frame[:4], xrdsum.CRC32C(frame[4:]))
	return frame
}
