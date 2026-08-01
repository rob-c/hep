// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package clone contains the structures describing the request and the response
// for the clone request (kXR_clone), which copies byte ranges between two files
// the server already has open.
//
// The point of it is that the data does not move: a client that wants the first
// megabyte of one file and the last megabyte of another written into a third
// would otherwise read both across the network and write them back, and the
// server can do it with copy_file_range(2) instead. Several ranges travel in one
// request, so a scatter of extents costs one round trip rather than one each.
//
// The destination is the file the request is sent on. Every source is another
// file open on the same connection, named by its own handle, and the server
// checks that the destination was opened for writing and each source for
// reading.
package clone // import "go-hep.org/x/hep/xrootd/xrdproto/clone"

import (
	"fmt"
	"math"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
)

// RequestID is the id of the request, it is sent as part of message.
// See xrootd protocol specification for details: http://xrootd.org/doc/dev45/XRdv310.pdf, 2.3 Client Request Format.
const RequestID uint16 = 3032

const (
	// ItemLength is the size on the wire of one [Item].
	ItemLength = 32

	// MaxItems is the number of items a server accepts in one request
	// (maxClonesz). A longer list is refused whole, so a caller with more
	// ranges than this sends them in several requests.
	MaxItems = 1024
)

// Item is one range to copy: SrcLength bytes taken from SrcOffset in the file
// open as Src, written at DstOffset in the file the request was sent on.
//
// An item of zero length copies nothing and is not an error; servers skip it.
type Item struct {
	Src       xrdfs.FileHandle // Src is the handle of the file to copy from.
	_         [4]byte
	SrcOffset int64 // SrcOffset is where the range starts in the source file.
	SrcLength int64 // SrcLength is how many bytes to copy.
	DstOffset int64 // DstOffset is where the range lands in the destination file.
}

// MarshalXrd implements xrdproto.Marshaler.
func (o Item) MarshalXrd(wBuffer *xrdenc.WBuffer) error {
	wBuffer.WriteBytes(o.Src[:])
	wBuffer.Next(4)
	wBuffer.WriteI64(o.SrcOffset)
	wBuffer.WriteI64(o.SrcLength)
	wBuffer.WriteI64(o.DstOffset)
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (o *Item) UnmarshalXrd(rBuffer *xrdenc.RBuffer) error {
	rBuffer.ReadBytes(o.Src[:])
	rBuffer.Skip(4)
	o.SrcOffset = rBuffer.ReadI64()
	o.SrcLength = rBuffer.ReadI64()
	o.DstOffset = rBuffer.ReadI64()
	return rBuffer.Err()
}

// Validate reports whether the item names a range a server will accept.
//
// The offsets and the length travel as unsigned 64-bit fields and are used as
// signed ones, so a value with its high bit set, or a range whose end does not
// fit, is refused rather than handed to a kernel that would answer EINVAL from
// somewhere the caller cannot see.
func (o Item) Validate() error {
	switch {
	case o.SrcOffset < 0 || o.DstOffset < 0 || o.SrcLength < 0:
		return fmt.Errorf("xrootd: clone offset/length out of range")
	case o.SrcOffset > math.MaxInt64-o.SrcLength,
		o.DstOffset > math.MaxInt64-o.SrcLength:
		return fmt.Errorf("xrootd: clone offset/length out of range")
	}
	return nil
}

// Request holds the clone request parameters. Dst is the file the ranges are
// copied into; it is the handle the request is sent on.
type Request struct {
	Dst   xrdfs.FileHandle
	_     [12]byte
	Items []Item
}

// NewRequest forms a Request that copies items into the file open as dst.
func NewRequest(dst xrdfs.FileHandle, items []Item) *Request {
	return &Request{Dst: dst, Items: items}
}

// ReqID implements xrdproto.Request.ReqID.
func (req *Request) ReqID() uint16 { return RequestID }

// ShouldSign implements xrdproto.Request.ShouldSign.
//
// As for the other requests that write, the answer is left to the security
// level: a clone is signed wherever a write is, and package signing says so
// once for both rather than each request deciding for itself.
func (req *Request) ShouldSign() bool { return false }

// SetHandle implements xrdproto.FilehandleRequest.SetHandle: it points the
// request at the file handle handle, which is what the destination file is open
// as at the server the request is about to be sent to.
//
// The source handles are not touched. They are handles of the same session, so
// a request that was redirected has to be built again against the new server
// rather than repointed.
func (req *Request) SetHandle(handle xrdfs.FileHandle) { req.Dst = handle }

// MarshalXrd implements xrdproto.Marshaler.
func (o Request) MarshalXrd(wBuffer *xrdenc.WBuffer) error {
	if len(o.Items) > MaxItems {
		return fmt.Errorf("xrootd: too many clone items: %d, at most %d are accepted", len(o.Items), MaxItems)
	}
	for i, item := range o.Items {
		if err := item.Validate(); err != nil {
			return fmt.Errorf("xrootd: clone item %d: %w", i, err)
		}
	}

	wBuffer.WriteBytes(o.Dst[:])
	wBuffer.Next(12)
	wBuffer.WriteLen(len(o.Items) * ItemLength)
	for _, item := range o.Items {
		if err := item.MarshalXrd(wBuffer); err != nil {
			return err
		}
	}
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (o *Request) UnmarshalXrd(rBuffer *xrdenc.RBuffer) error {
	rBuffer.ReadBytes(o.Dst[:])
	rBuffer.Skip(12)
	n := rBuffer.ReadLen()
	if err := rBuffer.Err(); err != nil {
		return err
	}
	if n%ItemLength != 0 {
		return fmt.Errorf("xrootd: malformed clone list: %d bytes is not a whole number of %d-byte items", n, ItemLength)
	}

	o.Items = make([]Item, n/ItemLength)
	for i := range o.Items {
		if err := o.Items[i].UnmarshalXrd(rBuffer); err != nil {
			return err
		}
	}
	return rBuffer.Err()
}

// Response is the response for the clone request. A server that copied the
// ranges says so and nothing else: the request said how many bytes it wanted
// moved, and anything short of all of them was an error.
type Response struct{}

// RespID implements xrdproto.Response.RespID.
func (resp *Response) RespID() uint16 { return RequestID }

// MarshalXrd implements xrdproto.Marshaler.
func (o Response) MarshalXrd(wBuffer *xrdenc.WBuffer) error { return nil }

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (o *Response) UnmarshalXrd(rBuffer *xrdenc.RBuffer) error { return rBuffer.Err() }
