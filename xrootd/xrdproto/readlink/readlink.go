// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package readlink contains the structures describing the readlink request.
//
// kXR_readlink is a vendor extension rather than part of the protocol
// specification: a server that has not been built with it answers
// kXR_Unsupported, which is the only way to find out whether it is there.
package readlink // import "go-hep.org/x/hep/xrootd/xrdproto/readlink"

import (
	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

// RequestID is the id of the request, it is sent as part of message.
const RequestID uint16 = 3502

// Request holds the readlink request parameters, such as the link path.
type Request struct {
	_    [16]byte
	Path string
}

// MarshalXrd implements xrdproto.Marshaler.
func (req Request) MarshalXrd(w *xrdenc.WBuffer) error {
	w.Next(16)
	w.WriteStr(req.Path)
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (req *Request) UnmarshalXrd(r *xrdenc.RBuffer) error {
	r.Skip(16)
	req.Path = r.ReadStr()
	return r.Err()
}

// ReqID implements xrdproto.Request.ReqID.
func (req *Request) ReqID() uint16 { return RequestID }

// ShouldSign implements xrdproto.Request.ShouldSign.
func (req *Request) ShouldSign() bool { return false }

// Opaque implements xrdproto.FilepathRequest.Opaque.
func (req *Request) Opaque() string { return xrdproto.Opaque(req.Path) }

// SetOpaque implements xrdproto.FilepathRequest.SetOpaque.
func (req *Request) SetOpaque(opaque string) { xrdproto.SetOpaque(&req.Path, opaque) }

// Response is the response issued by the server to a readlink request: the
// target of the link, as plain text.
type Response struct {
	Data []byte
}

// RespID implements xrdproto.Response.RespID.
func (resp *Response) RespID() uint16 { return RequestID }

// MarshalXrd implements xrdproto.Marshaler.
func (resp Response) MarshalXrd(w *xrdenc.WBuffer) error {
	w.WriteBytes(resp.Data)
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (resp *Response) UnmarshalXrd(r *xrdenc.RBuffer) error {
	resp.Data = make([]byte, r.Len())
	r.ReadBytes(resp.Data)
	return r.Err()
}
