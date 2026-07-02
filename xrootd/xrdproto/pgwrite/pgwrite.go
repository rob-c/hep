// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package pgwrite contains the structures describing the pgwrite request and
// response (kXR_pgwrite), the paged write with per-page CRC-32C integrity.
// Phase 1 keeps pathid=0 (data on the control socket) and does not implement
// the kXR_pgRetry recovery flow: a server-detected CRC error is an error.
package pgwrite // import "go-hep.org/x/hep/xrootd/xrdproto/pgwrite"

import (
	"encoding/binary"
	"fmt"

	"go-hep.org/x/hep/xrootd/internal/pgbuf"
	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

// RequestID is the id of the request, it is sent as part of message.
const RequestID uint16 = 3026

// Request holds the pgwrite request parameters. Data is the plain file
// content; the page-unit framing (with per-page CRC-32C) is applied during
// marshaling.
type Request struct {
	Handle xrdfs.FileHandle
	Offset int64
	Data   []byte
}

// ReqID implements xrdproto.Request.ReqID.
func (req *Request) ReqID() uint16 { return RequestID }

// ShouldSign implements xrdproto.Request.ShouldSign.
func (req *Request) ShouldSign() bool { return true }

// MarshalXrd implements xrdproto.Marshaler.
func (o Request) MarshalXrd(w *xrdenc.WBuffer) error {
	pages := pgbuf.Encode(o.Offset, o.Data)
	w.WriteBytes(o.Handle[:])
	w.WriteI64(o.Offset)
	w.WriteU8(0) // pathid: control socket
	w.WriteU8(0) // reqflags: no retry
	w.Next(2)    // reserved
	w.WriteI32(int32(len(pages)))
	w.WriteBytes(pages)
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (o *Request) UnmarshalXrd(r *xrdenc.RBuffer) error {
	r.ReadBytes(o.Handle[:])
	o.Offset = r.ReadI64()
	r.Skip(4) // pathid, reqflags, reserved
	n := int(r.ReadI32())
	pages := make([]byte, n)
	r.ReadBytes(pages)
	data, err := pgbuf.Decode(o.Offset, pages)
	if err != nil {
		return err
	}
	o.Data = data
	return nil
}

// Response is a response for the pgwrite request: the file offset the
// server acknowledged, carried in a CRC-protected kXR_status frame.
type Response struct {
	Offset int64
}

// RespID implements xrdproto.Response.RespID.
func (resp *Response) RespID() uint16 { return RequestID }

// MarshalXrd implements xrdproto.Marshaler. It emits a final kXR_status
// acknowledgment frame; it exists for servers and tests.
func (resp Response) MarshalXrd(w *xrdenc.WBuffer) error {
	info := make([]byte, 8)
	binary.BigEndian.PutUint64(info, uint64(resp.Offset))
	frame := xrdproto.StatusFrame(xrdproto.StatusBody{
		RequestID: uint8(RequestID - 3000),
		RespType:  xrdproto.FinalResult,
	}, info)
	w.WriteBytes(frame)
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (resp *Response) UnmarshalXrd(r *xrdenc.RBuffer) error {
	frame := r.Bytes()
	if len(frame) < xrdproto.StatusBodyLength+8 {
		return fmt.Errorf("xrootd: truncated pgwrite status frame: %d bytes", len(frame))
	}
	var body xrdproto.StatusBody
	if err := body.UnmarshalVerifyXrd(frame[:xrdproto.StatusBodyLength+8]); err != nil {
		return err
	}
	resp.Offset = int64(binary.BigEndian.Uint64(frame[xrdproto.StatusBodyLength : xrdproto.StatusBodyLength+8]))
	return nil
}
