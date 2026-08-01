// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package chkpoint contains the structures describing the checkpoint request
// (kXR_chkpoint), which makes a group of writes to an open file undoable.
//
// A checkpoint is opened with Begin, and the writes made inside it are either
// made permanent with Commit or undone with Rollback. Query reports how much
// the server is prepared to undo — a bound on the *undo*, not on the file: a
// transaction that overwrites more than that is one the server could no longer
// roll back, and it refuses the write rather than lose the ability.
//
// The writes themselves travel inside Xeq requests, which carry a whole
// kXR_write, kXR_pgwrite or kXR_truncate to be run within the checkpoint.
package chkpoint // import "go-hep.org/x/hep/xrootd/xrdproto/chkpoint"

import (
	"fmt"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/pgwrite"
	"go-hep.org/x/hep/xrootd/xrdproto/truncate"
	"go-hep.org/x/hep/xrootd/xrdproto/write"
)

// RequestID is the id of the request, it is sent as part of message.
const RequestID uint16 = 3012

// Checkpoint sub-codes.
const (
	Begin    uint8 = 0 // Begin opens a checkpoint on the handle.
	Commit   uint8 = 1 // Commit makes the checkpointed writes permanent.
	Query    uint8 = 2 // Query asks how much a checkpoint on this file may hold.
	Rollback uint8 = 3 // Rollback undoes the checkpointed writes.
	Xeq      uint8 = 4 // Xeq runs the enclosed request inside the checkpoint.
)

// requestHeaderLength is the length of an XRootD request header, which is what
// an Xeq request declares as its data length.
const requestHeaderLength = 24

// Request holds the checkpoint request parameters.
type Request struct {
	Handle xrdfs.FileHandle
	// SubCode is one of Begin, Commit, Query, Rollback or Xeq.
	SubCode uint8
	// Header is the enclosed request's 24-byte header, for Xeq.
	Header []byte
	// Data is the enclosed request's payload, which follows the frame rather
	// than being counted in its data length.
	Data []byte
}

// NewXeq returns a checkpoint request that runs req inside the open
// checkpoint on handle.
//
// Only a write, a paged write or a truncate may be enclosed: those are the
// three operations a server knows how to undo, and one it does not is a
// silently unprotected write rather than an error at commit time.
func NewXeq(handle xrdfs.FileHandle, req xrdproto.Request) (*Request, error) {
	switch req.ReqID() {
	case write.RequestID, pgwrite.RequestID, truncate.RequestID:
	default:
		return nil, fmt.Errorf("xrootd: a checkpoint can only execute a write or a truncate, not request %d", req.ReqID())
	}

	var w xrdenc.WBuffer
	hdr := xrdproto.RequestHeader{RequestID: req.ReqID()}
	if err := hdr.MarshalXrd(&w); err != nil {
		return nil, err
	}
	if err := req.MarshalXrd(&w); err != nil {
		return nil, err
	}
	raw := w.Bytes()
	if len(raw) < requestHeaderLength {
		return nil, fmt.Errorf("xrootd: request %d marshalled to %d bytes, too short to enclose", req.ReqID(), len(raw))
	}
	// The enclosed request carries no stream id of its own: the answer comes
	// back on the outer frame's.
	return &Request{
		Handle:  handle,
		SubCode: Xeq,
		Header:  raw[:requestHeaderLength],
		Data:    raw[requestHeaderLength:],
	}, nil
}

// MarshalXrd implements xrdproto.Marshaler.
//
// The sub-code goes in the *last* byte of the parameter area rather than the
// first, and the enclosed request's payload streams after the frame without
// being counted in the data length — a server that read the declared length
// and stopped would otherwise take the first payload byte for the start of the
// next request.
func (req Request) MarshalXrd(w *xrdenc.WBuffer) error {
	w.WriteBytes(req.Handle[:])
	w.Next(11)
	w.WriteU8(req.SubCode)
	w.WriteLen(len(req.Header))
	w.WriteBytes(req.Header)
	w.WriteBytes(req.Data)
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
//
// Only the frame is decoded: the enclosed request's payload is not part of it,
// and a reader that wants it reads on for as many bytes as that request's own
// header declares.
func (req *Request) UnmarshalXrd(r *xrdenc.RBuffer) error {
	r.ReadBytes(req.Handle[:])
	r.Skip(11)
	req.SubCode = r.ReadU8()
	req.Header = r.ReadLenBytes()
	return r.Err()
}

// ReqID implements xrdproto.Request.ReqID.
func (req *Request) ReqID() uint16 { return RequestID }

// ShouldSign implements xrdproto.Request.ShouldSign.
func (req *Request) ShouldSign() bool { return true }

// Response is the response issued by the server to a checkpoint query.
type Response struct {
	// Capacity is how many bytes of undo the server will hold for a checkpoint
	// on this file.
	Capacity int32
	// Used is how much of that the open checkpoint already holds, and zero when
	// none is open.
	Used int32
}

// RespID implements xrdproto.Response.RespID.
func (*Response) RespID() uint16 { return RequestID }

// MarshalXrd implements xrdproto.Marshaler.
func (o Response) MarshalXrd(w *xrdenc.WBuffer) error {
	w.WriteI32(o.Capacity)
	w.WriteI32(o.Used)
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (o *Response) UnmarshalXrd(r *xrdenc.RBuffer) error {
	o.Capacity = r.ReadI32()
	o.Used = r.ReadI32()
	return r.Err()
}
