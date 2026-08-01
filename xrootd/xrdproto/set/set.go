// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package set contains the structures describing the set request.
// See xrootd protocol specification (http://xrootd.org/doc/dev45/XRdv310.pdf, p. 111) for details.
package set // import "go-hep.org/x/hep/xrootd/xrdproto/set"

import (
	"go-hep.org/x/hep/xrootd/internal/xrdenc"
)

// RequestID is the id of the request, it is sent as part of message.
// See xrootd protocol specification for details: http://xrootd.org/doc/dev45/XRdv310.pdf, 2.3 Client Request Format.
const RequestID uint16 = 3018

// AppIDPrefix begins the one directive every client sends. What follows it
// labels the connection in the server's monitoring stream, so that an operator
// looking at the server can tell whose traffic it is.
const AppIDPrefix = "appid "

// Request holds the set request parameters: the directive text.
type Request struct {
	_    [16]byte
	Data string
}

// MarshalXrd implements xrdproto.Marshaler.
func (req Request) MarshalXrd(w *xrdenc.WBuffer) error {
	w.Next(16)
	w.WriteStr(req.Data)
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (req *Request) UnmarshalXrd(r *xrdenc.RBuffer) error {
	r.Skip(16)
	req.Data = r.ReadStr()
	return r.Err()
}

// ReqID implements xrdproto.Request.ReqID.
func (req *Request) ReqID() uint16 { return RequestID }

// ShouldSign implements xrdproto.Request.ShouldSign.
func (req *Request) ShouldSign() bool { return false }

// Response is the response issued by the server to a set request. A server
// that has nothing to say about the directive answers with an empty body.
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
