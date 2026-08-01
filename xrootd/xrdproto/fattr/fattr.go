// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fattr contains the structures describing the fattr request
// (kXR_fattr): extended-attribute get/set/delete/list on a path. The wire
// vectors follow XProtocol.hh: an nvec entry is [u16 rc][name\0] and a vvec
// entry is [i32 vlen][value], both big-endian; a path-based request body is
// "path\0" + nvec + (set only) vvec.
package fattr // import "go-hep.org/x/hep/xrootd/xrdproto/fattr"

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
)

// RequestID is the id of the request, it is sent as part of message.
const RequestID uint16 = 3020

// Subcodes selecting the fattr operation.
const (
	// Del deletes attributes.
	Del uint8 = 0
	// Get retrieves attribute values.
	Get uint8 = 1
	// List enumerates attribute names.
	List uint8 = 2
	// Set creates or replaces attributes.
	Set uint8 = 3
)

// MaxVars is the maximum number of attributes per request (kXR_faMaxVars).
const MaxVars = 16

// Request options.
const (
	// IsNew requires that the attribute does not already exist (set only).
	IsNew uint8 = 0x01
	// AData asks list to also return attribute values.
	AData uint8 = 0x10
)

// Request holds the fattr request parameters. Body carries the path and
// attribute vectors; the builders below assemble it.
type Request struct {
	Handle  xrdfs.FileHandle
	Subcode uint8
	NumAttr uint8
	Options uint8
	Body    []byte
}

// ReqID implements xrdproto.Request.ReqID.
func (req *Request) ReqID() uint16 { return RequestID }

// ShouldSign implements xrdproto.Request.ShouldSign.
func (req *Request) ShouldSign() bool { return true }

// SetHandle implements xrdproto.FilehandleRequest.SetHandle: it points the
// request at the file handle handle, which is what the file is open as at
// the server the request is about to be sent to.
func (req *Request) SetHandle(handle xrdfs.FileHandle) { req.Handle = handle }

// MarshalXrd implements xrdproto.Marshaler.
func (o Request) MarshalXrd(w *xrdenc.WBuffer) error {
	w.WriteBytes(o.Handle[:])
	w.WriteU8(o.Subcode)
	w.WriteU8(o.NumAttr)
	w.WriteU8(o.Options)
	w.Next(9)
	w.WriteI32(int32(len(o.Body)))
	w.WriteBytes(o.Body)
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (o *Request) UnmarshalXrd(r *xrdenc.RBuffer) error {
	r.ReadBytes(o.Handle[:])
	o.Subcode = r.ReadU8()
	o.NumAttr = r.ReadU8()
	o.Options = r.ReadU8()
	r.Skip(9)
	o.Body = r.ReadLenBytes()
	return r.Err()
}

func pathNameBody(path, name string) []byte {
	body := append([]byte(path), 0)
	body = append(body, 0, 0) // nvec rc placeholder
	body = append(body, []byte(name)...)
	return append(body, 0)
}

// GetRequest builds a path-based fattr get for a single attribute.
func GetRequest(path, name string) *Request {
	return &Request{Subcode: Get, NumAttr: 1, Body: pathNameBody(path, name)}
}

// DelRequest builds a path-based fattr delete for a single attribute.
func DelRequest(path, name string) *Request {
	return &Request{Subcode: Del, NumAttr: 1, Body: pathNameBody(path, name)}
}

// SetRequest builds a path-based fattr set for a single attribute; isNew
// requires the attribute to not exist yet.
func SetRequest(path, name string, value []byte, isNew bool) *Request {
	body := pathNameBody(path, name)
	var vlen [4]byte
	binary.BigEndian.PutUint32(vlen[:], uint32(len(value)))
	body = append(body, vlen[:]...)
	body = append(body, value...)
	req := &Request{Subcode: Set, NumAttr: 1, Body: body}
	if isNew {
		req.Options = IsNew
	}
	return req
}

// ListRequest builds a path-based fattr list request.
func ListRequest(path string) *Request {
	return &Request{Subcode: List, Body: append([]byte(path), 0)}
}

// Response is a response for the fattr request. Raw holds the payload;
// Attr and Names decode it for get/set/del and list respectively.
type Response struct {
	ErrCount uint8
	NumAttr  uint8
	Raw      []byte
}

// RespID implements xrdproto.Response.RespID.
func (resp *Response) RespID() uint16 { return RequestID }

// MarshalXrd implements xrdproto.Marshaler: it writes the raw payload; it
// exists for servers and tests.
func (resp Response) MarshalXrd(w *xrdenc.WBuffer) error {
	w.WriteBytes(resp.Raw)
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (resp *Response) UnmarshalXrd(r *xrdenc.RBuffer) error {
	resp.Raw = make([]byte, r.Len())
	r.ReadBytes(resp.Raw)
	if len(resp.Raw) >= 2 {
		resp.ErrCount = resp.Raw[0]
		resp.NumAttr = resp.Raw[1]
	}
	return r.Err()
}

// Attr decodes a get/set/del reply for a single attribute: the per-attribute
// status code rc (0 on success, a kXR error code otherwise) and, for get,
// the attribute value.
func (resp *Response) Attr() (name string, rc uint16, value []byte, err error) {
	raw := resp.Raw
	if len(raw) < 2+2+1 {
		return "", 0, nil, fmt.Errorf("xrootd: fattr response too short: %d bytes", len(raw))
	}
	raw = raw[2:] // errcount, numattr
	rc = binary.BigEndian.Uint16(raw[:2])
	raw = raw[2:]
	i := bytes.IndexByte(raw, 0)
	if i < 0 {
		return "", 0, nil, fmt.Errorf("xrootd: fattr response: unterminated attribute name")
	}
	name = string(raw[:i])
	raw = raw[i+1:]
	if len(raw) >= 4 {
		n := int(binary.BigEndian.Uint32(raw[:4]))
		raw = raw[4:]
		if n > len(raw) {
			return "", 0, nil, fmt.Errorf("xrootd: fattr value length %d exceeds %d remaining bytes", n, len(raw))
		}
		value = raw[:n]
	}
	return name, rc, value, nil
}

// Names decodes a list reply: NUL-separated attribute names.
func (resp *Response) Names() ([]string, error) {
	raw := strings.TrimRight(string(resp.Raw), "\x00")
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\x00"), nil
}
