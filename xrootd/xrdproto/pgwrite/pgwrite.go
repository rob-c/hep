// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package pgwrite contains the structures describing the pgwrite request and
// response (kXR_pgwrite), the paged write with per-page CRC-32C integrity.
// It keeps pathid=0 (data on the control socket).
//
// A pgwrite reply may carry a checksum-error trailer naming the pages whose
// CRC-32C did not survive the wire. The server has already stored those pages;
// they are corrupt on disk until the client resends each one with the Retry
// flag set. See Response.Corrupt.
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

// Retry is the kXR_pgRetry request flag: this request retransmits a single
// page that the server reported as corrupt in an earlier reply.
const Retry uint8 = 0x01

// CSEHeaderLength is the length of the fixed part of a checksum-error
// trailer: cseCRC[4] + dlFirst[2] + dlLast[2], followed by one big-endian
// int64 file offset per corrupt page.
const CSEHeaderLength = 8

// Request holds the pgwrite request parameters. Data is the plain file
// content; the page-unit framing (with per-page CRC-32C) is applied during
// marshaling.
type Request struct {
	Handle xrdfs.FileHandle
	Offset int64
	Data   []byte
	Flags  uint8 // request flags; Retry for a corrupt-page retransmission
}

// ReqID implements xrdproto.Request.ReqID.
func (req *Request) ReqID() uint16 { return RequestID }

// ShouldSign implements xrdproto.Request.ShouldSign.
func (req *Request) ShouldSign() bool { return true }

// SetHandle implements xrdproto.FilehandleRequest.SetHandle: it points the
// request at the file handle handle, which is what the file is open as at
// the server the request is about to be sent to.
func (req *Request) SetHandle(handle xrdfs.FileHandle) { req.Handle = handle }

// MaxResponseLength implements xrdproto.ResponseLimiter: a kXR_status frame
// plus a checksum-error trailer that, in the worst case, names every page the
// request wrote.
func (req *Request) MaxResponseLength() int64 {
	pages := int64(len(req.Data))/pgbuf.PageSize + 2
	return xrdproto.StatusBodyLength + 8 + CSEHeaderLength + 8*pages
}

// MarshalXrd implements xrdproto.Marshaler.
func (o Request) MarshalXrd(w *xrdenc.WBuffer) error {
	pages := pgbuf.Encode(o.Offset, o.Data)
	w.WriteBytes(o.Handle[:])
	w.WriteI64(o.Offset)
	w.WriteU8(0) // pathid: control socket
	w.WriteU8(o.Flags)
	w.Next(2) // reserved
	w.WriteI32(int32(len(pages)))
	w.WriteBytes(pages)
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (o *Request) UnmarshalXrd(r *xrdenc.RBuffer) error {
	r.ReadBytes(o.Handle[:])
	o.Offset = r.ReadI64()
	r.Skip(1) // pathid
	o.Flags = r.ReadU8()
	r.Skip(2) // reserved
	pages := r.ReadLenBytes()
	if err := r.Err(); err != nil {
		return err
	}
	data, err := pgbuf.Decode(o.Offset, pages)
	if err != nil {
		return err
	}
	o.Data = data
	return r.Err()
}

// Response is a response for the pgwrite request: the file offset the
// server acknowledged, carried in a CRC-protected kXR_status frame, and the
// file offsets of any page the server received with a broken CRC-32C.
type Response struct {
	Offset int64

	// Corrupt holds the file offset of every page whose CRC-32C did not
	// match on arrival. The server stored those pages anyway, so the file
	// holds corrupt data until each one is retransmitted with the Retry
	// flag. An empty slice means the server took every page.
	Corrupt []int64
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

	// Whatever follows the info tail is the checksum-error trailer, appended
	// by the session from the trailing data the status body announced.
	cse := frame[xrdproto.StatusBodyLength+8:]
	if len(cse) == 0 {
		return r.Err()
	}
	corrupt, err := ParseCSE(cse)
	if err != nil {
		return err
	}
	resp.Corrupt = corrupt
	return r.Err()
}

// ParseCSE decodes a pgwrite checksum-error trailer: an 8-byte header
// (cseCRC[4] + dlFirst[2] + dlLast[2]) followed by one big-endian int64 file
// offset per page whose CRC-32C did not match on arrival.
func ParseCSE(cse []byte) ([]int64, error) {
	if len(cse) < CSEHeaderLength || (len(cse)-CSEHeaderLength)%8 != 0 {
		return nil, fmt.Errorf("xrootd: malformed pgwrite checksum-error trailer (%d bytes)", len(cse))
	}
	n := (len(cse) - CSEHeaderLength) / 8
	if n == 0 {
		return nil, nil
	}
	offsets := make([]int64, n)
	for i := range offsets {
		p := CSEHeaderLength + 8*i
		offsets[i] = int64(binary.BigEndian.Uint64(cse[p : p+8]))
	}
	return offsets, nil
}
