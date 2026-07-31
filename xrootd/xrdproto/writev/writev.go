// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package writev contains the structures describing the writev request
// (kXR_writev), the scatter-gather write.
//
// The framing is the one detail worth knowing before reading the code: the
// request's dlen covers *only* the 16-byte write_list descriptors, and the
// concatenated segment data streams after the frame. Stock servers enforce
// dlen%16 == 0 and answer kXR_ArgInvalid ("Write vector is invalid") to a
// request that counts the data inside dlen.
package writev // import "go-hep.org/x/hep/xrootd/xrdproto/writev"

import (
	"fmt"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

// RequestID is the id of the request, it is sent as part of message.
// See xrootd protocol specification for details: http://xrootd.org/doc/dev45/XRdv310.pdf, 2.3 Client Request Format.
const RequestID uint16 = 3031

// SegmentLength is the size, in bytes, of one write_list entry: the file
// handle, the length and the offset.
const SegmentLength = 16

// OptionSync asks the server to commit every file the vector touched before
// answering, as an fsync would.
const OptionSync uint8 = 0x01

// Segment is one element of the vector: the file to write to, where to start
// and the bytes to write.
type Segment struct {
	Handle xrdfs.FileHandle
	Offset int64
	Data   []byte
}

// Request holds the writev request parameters.
//
// A vector write is all-or-nothing: the server either applies every segment or
// none of them, so a partial result is never reported.
type Request struct {
	Options  uint8
	Segments []Segment
}

// ReqID implements xrdproto.Request.ReqID.
func (req *Request) ReqID() uint16 { return RequestID }

// ShouldSign implements xrdproto.Request.ShouldSign.
//
// It is false, and kXR_writev is deliberately absent from the signing
// requirements: a kXR_sigver hash covers the request frame and its dlen bytes,
// and a vector write's data is outside dlen. Signing one would produce a hash
// the server cannot reproduce.
func (req *Request) ShouldSign() bool { return false }

// Validate reports whether the request is within the bounds a client applies
// before touching the wire.
func (req *Request) Validate() error {
	var total int64
	for _, seg := range req.Segments {
		total += int64(len(seg.Data))
	}
	return xrdproto.ValidateVector(len(req.Segments), total, "writev")
}

// MarshalXrd implements xrdproto.Marshaler.
func (req Request) MarshalXrd(w *xrdenc.WBuffer) error {
	w.WriteU8(req.Options)
	w.Next(15) // reserved
	w.WriteLen(len(req.Segments) * SegmentLength)
	for _, seg := range req.Segments {
		w.WriteBytes(seg.Handle[:])
		w.WriteLen(len(seg.Data))
		w.WriteI64(seg.Offset)
	}
	// The data is not covered by dlen: it follows the frame on the wire.
	for _, seg := range req.Segments {
		w.WriteBytes(seg.Data)
	}
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
//
// It recovers the descriptors, which is all a framing reader has: the segment
// data lies past the dlen bytes the frame declares, and is read from the
// connection separately. The decoded segments carry their lengths in Data, as
// zero-filled slices, so the caller knows how much to read for each.
func (req *Request) UnmarshalXrd(r *xrdenc.RBuffer) error {
	if r.Len() < 20 {
		return fmt.Errorf("xrootd: writev request needs 20 bytes of header, only %d are left", r.Len())
	}
	req.Options = r.ReadU8()
	r.Skip(15)
	dlen := r.ReadLen()
	if dlen < 0 || dlen%SegmentLength != 0 {
		return fmt.Errorf("xrootd: writev payload of %d bytes is not a whole number of %d-byte segments", dlen, SegmentLength)
	}
	if dlen > r.Len() {
		return fmt.Errorf("xrootd: writev payload of %d bytes exceeds the %d bytes received", dlen, r.Len())
	}
	nsegs := dlen / SegmentLength
	lengths := make([]int32, nsegs)
	var total int64
	for i := range lengths {
		var seg Segment
		r.ReadBytes(seg.Handle[:])
		lengths[i] = r.ReadI32()
		seg.Offset = r.ReadI64()
		if lengths[i] < 0 {
			return fmt.Errorf("xrootd: writev segment has a negative length of %d", lengths[i])
		}
		total += int64(lengths[i])
		req.Segments = append(req.Segments, seg)
	}
	// The declared lengths decide how much is read from the connection next,
	// so they are bounded before anything is reserved for them.
	if err := xrdproto.ValidateVector(nsegs, total, "writev"); err != nil {
		return err
	}
	for i := range req.Segments {
		req.Segments[i].Data = make([]byte, lengths[i])
	}
	return nil
}

var _ xrdproto.Request = (*Request)(nil)
