// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package pgread contains the structures describing the pgread request and
// response (kXR_pgread), the paged read with per-page CRC-32C integrity.
// Responses arrive as kXR_status frames; the session layer concatenates the
// complete frames of one request, and Response.UnmarshalXrd walks them.
package pgread // import "go-hep.org/x/hep/xrootd/xrdproto/pgread"

import (
	"encoding/binary"
	"fmt"

	"go-hep.org/x/hep/xrootd/internal/pgbuf"
	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

// RequestID is the id of the request, it is sent as part of message.
const RequestID uint16 = 3030

// Request holds the pgread request parameters.
type Request struct {
	Handle     xrdfs.FileHandle
	Offset     int64
	ReadLength int32
}

// ReqID implements xrdproto.Request.ReqID.
func (req *Request) ReqID() uint16 { return RequestID }

// ShouldSign implements xrdproto.Request.ShouldSign.
func (req *Request) ShouldSign() bool { return false }

// MaxResponseLength implements xrdproto.ResponseLimiter: the requested bytes,
// plus a 4-byte CRC-32C per page, plus a kXR_status header per frame in the
// worst case of one frame per page. Bounding it this way also bounds a server
// that answers with an unending stream of empty partial frames.
func (req *Request) MaxResponseLength() int64 {
	if req.ReadLength <= 0 {
		return 0
	}
	// +2 pages of slack: the first page is short when the offset is not page
	// aligned, and the server may close with an empty final frame.
	pages := int64(req.ReadLength)/pgbuf.PageSize + 3
	return int64(req.ReadLength) + pages*(4+xrdproto.StatusBodyLength+8)
}

// MarshalXrd implements xrdproto.Marshaler.
func (o Request) MarshalXrd(w *xrdenc.WBuffer) error {
	w.WriteBytes(o.Handle[:])
	w.WriteI64(o.Offset)
	w.WriteI32(o.ReadLength)
	w.WriteI32(0) // dlen: no args
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (o *Request) UnmarshalXrd(r *xrdenc.RBuffer) error {
	r.ReadBytes(o.Handle[:])
	o.Offset = r.ReadI64()
	o.ReadLength = r.ReadI32()
	r.Skip(4)
	return nil
}

// Response is a response for the pgread request: the CRC-verified data and
// the file offset of its first byte.
type Response struct {
	Data   []byte
	Offset int64
}

// RespID implements xrdproto.Response.RespID.
func (resp *Response) RespID() uint16 { return RequestID }

// MarshalXrd implements xrdproto.Marshaler. It emits a single final
// kXR_status frame carrying the whole Data as page units; it exists for
// servers and tests.
func (resp Response) MarshalXrd(w *xrdenc.WBuffer) error {
	pages := pgbuf.Encode(resp.Offset, resp.Data)
	info := make([]byte, 8)
	binary.BigEndian.PutUint64(info, uint64(resp.Offset))
	frame := xrdproto.StatusFrame(xrdproto.StatusBody{
		RequestID:  uint8(RequestID - 3000),
		RespType:   xrdproto.FinalResult,
		DataLength: int32(len(pages)),
	}, info)
	w.WriteBytes(frame)
	w.WriteBytes(pages)
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler. It walks the concatenated
// kXR_status frames of a pgread response, verifying and stripping the page
// framing of each.
func (resp *Response) UnmarshalXrd(r *xrdenc.RBuffer) error {
	wire := r.Bytes()
	first := true
	for len(wire) > 0 {
		if len(wire) < xrdproto.StatusBodyLength+8 {
			return fmt.Errorf("xrootd: truncated pgread status frame: %d bytes", len(wire))
		}
		var body xrdproto.StatusBody
		if err := body.UnmarshalVerifyXrd(wire[:xrdproto.StatusBodyLength+8]); err != nil {
			return err
		}
		off := int64(binary.BigEndian.Uint64(wire[xrdproto.StatusBodyLength : xrdproto.StatusBodyLength+8]))
		wire = wire[xrdproto.StatusBodyLength+8:]
		n := int(body.DataLength)
		if n > len(wire) {
			return fmt.Errorf("xrootd: pgread frame announces %d data bytes, %d available", n, len(wire))
		}
		if n > 0 {
			data, err := pgbuf.Decode(off, wire[:n])
			if err != nil {
				return err
			}
			if first {
				resp.Offset = off
				first = false
			}
			resp.Data = append(resp.Data, data...)
		}
		wire = wire[n:]
	}
	return nil
}
