// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package readv contains the structures describing the request and response
// for the readv request (kXR_readv), the scatter-gather read.
//
// The request payload is one 16-byte readahead_list entry per segment. The
// reply is not a plain concatenation of the data: it interleaves a 16-byte
// echo header per segment — carrying the length that was *actually* read —
// with that segment's bytes, so the reply must be walked rather than copied.
package readv // import "go-hep.org/x/hep/xrootd/xrdproto/readv"

import (
	"fmt"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

// RequestID is the id of the request, it is sent as part of message.
// See xrootd protocol specification for details: http://xrootd.org/doc/dev45/XRdv310.pdf, 2.3 Client Request Format.
const RequestID uint16 = 3025

// SegmentLength is the size, in bytes, of one readahead_list entry: the file
// handle, the length and the offset.
const SegmentLength = 16

// Segment is one element of the vector: the file to read from, where to start
// and how many bytes to read.
type Segment struct {
	Handle xrdfs.FileHandle
	Length int32
	Offset int64
}

// MarshalXrd implements xrdproto.Marshaler.
func (o Segment) MarshalXrd(w *xrdenc.WBuffer) error {
	w.WriteBytes(o.Handle[:])
	w.WriteI32(o.Length)
	w.WriteI64(o.Offset)
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (o *Segment) UnmarshalXrd(r *xrdenc.RBuffer) error {
	if r.Len() < SegmentLength {
		return fmt.Errorf("xrootd: readv segment needs %d bytes, only %d are left", SegmentLength, r.Len())
	}
	r.ReadBytes(o.Handle[:])
	o.Length = r.ReadI32()
	o.Offset = r.ReadI64()
	return r.Err()
}

// Request holds the readv request parameters.
type Request struct {
	Segments []Segment
}

// ReqID implements xrdproto.Request.ReqID.
func (req *Request) ReqID() uint16 { return RequestID }

// ShouldSign implements xrdproto.Request.ShouldSign.
func (req *Request) ShouldSign() bool { return false }

// MaxResponseLength implements xrdproto.ResponseLimiter: the reply is one
// 16-byte echo header per segment plus at most the bytes that were asked for.
func (req *Request) MaxResponseLength() int64 {
	var n int64
	for _, seg := range req.Segments {
		if seg.Length > 0 {
			n += int64(seg.Length)
		}
	}
	return int64(len(req.Segments))*SegmentLength + n
}

// Validate reports whether the request is within the bounds a client applies
// before touching the wire.
func (req *Request) Validate() error {
	var total int64
	for _, seg := range req.Segments {
		if seg.Length < 0 {
			total = -1
			break
		}
		total += int64(seg.Length)
	}
	return xrdproto.ValidateVector(len(req.Segments), total, "readv")
}

// MarshalXrd implements xrdproto.Marshaler.
func (o Request) MarshalXrd(w *xrdenc.WBuffer) error {
	w.Next(15) // reserved
	w.WriteU8(0)
	w.WriteLen(len(o.Segments) * SegmentLength)
	for _, seg := range o.Segments {
		if err := seg.MarshalXrd(w); err != nil {
			return err
		}
	}
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (o *Request) UnmarshalXrd(r *xrdenc.RBuffer) error {
	r.Skip(16)
	dlen := r.ReadLen()
	if dlen < 0 || dlen%SegmentLength != 0 {
		return fmt.Errorf("xrootd: readv payload of %d bytes is not a whole number of %d-byte segments", dlen, SegmentLength)
	}
	if dlen > r.Len() {
		return fmt.Errorf("xrootd: readv payload of %d bytes exceeds the %d bytes received", dlen, r.Len())
	}
	o.Segments = make([]Segment, dlen/SegmentLength)
	for i := range o.Segments {
		if err := o.Segments[i].UnmarshalXrd(r); err != nil {
			return err
		}
	}
	return r.Err()
}

// Chunk is one segment of a readv reply: the echoed file handle and offset,
// and the bytes that were read.
type Chunk struct {
	Handle xrdfs.FileHandle
	Offset int64
	Data   []byte
}

// Response is a response for the readv request, which contains the data of
// every segment that was read.
type Response struct {
	Chunks []Chunk
}

// RespID implements xrdproto.Response.RespID.
func (resp *Response) RespID() uint16 { return RequestID }

// MarshalXrd implements xrdproto.Marshaler.
func (resp Response) MarshalXrd(w *xrdenc.WBuffer) error {
	for _, c := range resp.Chunks {
		w.WriteBytes(c.Handle[:])
		w.WriteLen(len(c.Data))
		w.WriteI64(c.Offset)
		w.WriteBytes(c.Data)
	}
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
//
// The echo header's length field is the number of bytes that follow it, so a
// reply that promises more than it carries is refused rather than decoded up
// to the point where it runs out: the remainder would silently become the next
// segment's header.
func (resp *Response) UnmarshalXrd(r *xrdenc.RBuffer) error {
	resp.Chunks = nil
	for r.Len() > 0 {
		if r.Len() < SegmentLength {
			return fmt.Errorf("xrootd: readv reply ends with a %d-byte fragment of a %d-byte segment header", r.Len(), SegmentLength)
		}
		var c Chunk
		r.ReadBytes(c.Handle[:])
		n := r.ReadI32()
		c.Offset = r.ReadI64()
		switch {
		case n < 0:
			return fmt.Errorf("xrootd: readv reply segment has a negative length of %d", n)
		case int64(n) > int64(r.Len()):
			return fmt.Errorf("xrootd: readv reply segment of %d bytes exceeds the %d bytes left", n, r.Len())
		}
		c.Data = make([]byte, n)
		r.ReadBytes(c.Data)
		resp.Chunks = append(resp.Chunks, c)
	}
	return r.Err()
}

var (
	_ xrdproto.Request         = (*Request)(nil)
	_ xrdproto.ResponseLimiter = (*Request)(nil)
	_ xrdproto.Response        = (*Response)(nil)
)
