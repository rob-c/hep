// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package protocol contains the structures describing request and response
// for protocol request (see XRootD specification).
//
// A response consists of 3 parts:
//
// 1) A general response that is always returned and specifies protocol version and flags describing server type.
//
// 2) A response part that is added to the general response
// if `ReturnSecurityRequirements` is provided and server supports it.
// It contains the security version, the security options, the security level,
// and the number of following security overrides, if any.
//
// 3) A list of SecurityOverride - alterations needed to the specified predefined security level.
package protocol // import "go-hep.org/x/hep/xrootd/xrdproto/protocol"

import (
	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

// RequestID is the id of the request, it is sent as part of message.
// See xrootd protocol specification for details: http://xrootd.org/doc/dev45/XRdv310.pdf, 2.3 Client Request Format.
const RequestID uint16 = 3006

// Flags are the Flags that define xrootd server type. See xrootd protocol specification for further info.
type Flags int32

const (
	IsServer     Flags = 0x00000001 // IsServer indicates whether this server has server role.
	IsManager    Flags = 0x00000002 // IsManager indicates whether this server has manager role.
	IsMeta       Flags = 0x00000100 // IsMeta indicates whether this server has meta attribute.
	IsProxy      Flags = 0x00000200 // IsProxy indicates whether this server has proxy attribute.
	IsSupervisor Flags = 0x00000400 // IsSupervisor indicates whether this server has supervisor attribute.
)

// SecurityOptions are the security-related options.
// See specification for details: http://xrootd.org/doc/dev45/XRdv310.pdf, p. 72.
type SecurityOptions byte

const (
	// ForceSecurity specifies that signing is required even if the authentication
	// protocol does not support generic encryption.
	ForceSecurity SecurityOptions = 0x02
)

// RequestOptions specifies what should be returned as part of response.
type RequestOptions byte

const (
	// RequestOptionsNone specifies that only general response should be returned.
	RequestOptionsNone RequestOptions = 0
	// ReturnSecurityRequirements specifies that security requirements should be returned
	// if that's supported by the server. Wire value kXR_secreqs.
	ReturnSecurityRequirements RequestOptions = 0x01
	// AbleTLS advertises that the client is capable of in-protocol TLS. Wire value kXR_ableTLS.
	AbleTLS RequestOptions = 0x02
	// WantTLS requests that the connection be switched to TLS. Wire value kXR_wantTLS.
	WantTLS RequestOptions = 0x04
)

// TLS-related bits carried in the protocol response Flags word.
// They are declared through uint32 (not Flags) because kXR_haveTLS occupies the
// sign bit of the wire-level int32; the Response accessors below test them via
// uint32 to avoid sign-extension surprises.
// See XProtocol.hh and http://xrootd.org/doc/dev45/XRdv310.pdf.
const (
	flagHaveTLS  uint32 = 0x80000000 // server supports in-protocol TLS (kXR_haveTLS)
	flagGotoTLS  uint32 = 0x40000000 // server requires switching to TLS now (kXR_gotoTLS)
	flagTLSGPF   uint32 = 0x01000000 // TLS required for gpfile (kXR_tlsGPF)
	flagTLSData  uint32 = 0x02000000 // TLS required for the data stream (kXR_tlsData)
	flagTLSLogin uint32 = 0x04000000 // TLS required for login (kXR_tlsLogin)
	flagTLSSess  uint32 = 0x08000000 // TLS required for the session (kXR_tlsSess)
	flagTLSTPC   uint32 = 0x10000000 // TLS required for third-party copy (kXR_tlsTPC)
	flagTLSGPFA  uint32 = 0x20000000 // TLS required for anonymous gpfile (kXR_tlsGPFA)

	// flagTLSAny is every bit that names a phase or a request kind needing TLS
	// (kXR_tlsAny). It deliberately leaves out kXR_haveTLS and kXR_gotoTLS,
	// which say what the connection can and must do rather than what the work
	// on it requires.
	flagTLSAny uint32 = 0x1F000000
)

// Capability bits carried in the protocol response Flags word.
const (
	flagSupGPF  uint32 = 0x00400000 // server supports Grouped Parallel Fetch (kXR_supgpf)
	flagAnonGPF uint32 = 0x00800000 // GPF is open to unauthenticated clients (kXR_anongpf)
)

// Request holds protocol request parameters.
type Request struct {
	ClientProtocolVersion int32
	Options               RequestOptions
	_                     [11]byte
	_                     int32
}

// NewRequest forms a Request according to provided parameters.
func NewRequest(protocolVersion int32, withSecurityRequirements bool) *Request {
	var options = RequestOptionsNone
	if withSecurityRequirements {
		options |= ReturnSecurityRequirements
	}
	return &Request{ClientProtocolVersion: protocolVersion, Options: options}
}

// NewRequestTLS forms a protocol Request that advertises TLS capability.
//
// withSecurityRequirements requests the security-requirements trailer (needed to
// learn the signing level). ableTLS advertises that the client can speak TLS;
// wantTLS additionally asks the server to switch the connection to TLS (set for
// roots:// URLs). A server may still mandate TLS via kXR_gotoTLS even when
// wantTLS is false.
func NewRequestTLS(protocolVersion int32, withSecurityRequirements, ableTLS, wantTLS bool) *Request {
	var options = RequestOptionsNone
	if withSecurityRequirements {
		options |= ReturnSecurityRequirements
	}
	if ableTLS {
		options |= AbleTLS
	}
	if wantTLS {
		options |= WantTLS
	}
	return &Request{ClientProtocolVersion: protocolVersion, Options: options}
}

// ReqID implements xrdproto.Request.ReqID.
func (req *Request) ReqID() uint16 { return RequestID }

// MarshalXrd implements xrdproto.Marshaler.
func (o Request) MarshalXrd(wBuffer *xrdenc.WBuffer) error {
	wBuffer.WriteI32(o.ClientProtocolVersion)
	wBuffer.WriteU8(byte(o.Options))
	wBuffer.Next(15)
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (o *Request) UnmarshalXrd(rBuffer *xrdenc.RBuffer) error {
	o.ClientProtocolVersion = rBuffer.ReadI32()
	o.Options = RequestOptions(rBuffer.ReadU8())
	rBuffer.Skip(15)
	return rBuffer.Err()
}

// Response is a response for the `Protocol` request. See details in the xrootd protocol specification.
type Response struct {
	BinaryProtocolVersion int32
	Flags                 Flags
	HasSecurityInfo       bool
	_                     byte
	_                     byte
	SecurityVersion       byte
	SecurityOptions       SecurityOptions
	SecurityLevel         xrdproto.SecurityLevel
	SecurityOverrides     []xrdproto.SecurityOverride
}

// IsManager indicates whether this server has manager role.
func (resp *Response) IsManager() bool {
	return resp.Flags&IsManager != 0
}

// IsServer indicates whether this server has server role.
func (resp *Response) IsServer() bool {
	return resp.Flags&IsServer != 0
}

// IsMeta indicates whether this server has meta attribute.
func (resp *Response) IsMeta() bool {
	return resp.Flags&IsMeta != 0
}

// IsProxy indicates whether this server has proxy attribute.
func (resp *Response) IsProxy() bool {
	return resp.Flags&IsProxy != 0
}

// IsSupervisor indicates whether this server has supervisor attribute.
func (resp *Response) IsSupervisor() bool {
	return resp.Flags&IsSupervisor != 0
}

// ForceSecurity indicates whether signing is required even if the authentication
// protocol does not support generic encryption.
func (resp *Response) ForceSecurity() bool {
	return resp.SecurityOptions&ForceSecurity != 0
}

func (resp *Response) flagBits() uint32 { return uint32(resp.Flags) }

// HasTLS reports whether the server supports in-protocol TLS (kXR_haveTLS).
func (resp *Response) HasTLS() bool { return resp.flagBits()&flagHaveTLS != 0 }

// GotoTLS reports whether the server requires switching to TLS now (kXR_gotoTLS).
func (resp *Response) GotoTLS() bool { return resp.flagBits()&flagGotoTLS != 0 }

// TLSForData reports whether TLS is required for the data stream (kXR_tlsData).
func (resp *Response) TLSForData() bool { return resp.flagBits()&flagTLSData != 0 }

// TLSForLogin reports whether TLS is required for login (kXR_tlsLogin).
func (resp *Response) TLSForLogin() bool { return resp.flagBits()&flagTLSLogin != 0 }

// TLSForSession reports whether TLS is required for the whole session (kXR_tlsSess).
func (resp *Response) TLSForSession() bool { return resp.flagBits()&flagTLSSess != 0 }

// TLSForTPC reports whether TLS is required for third-party copy (kXR_tlsTPC).
func (resp *Response) TLSForTPC() bool { return resp.flagBits()&flagTLSTPC != 0 }

// TLSForGPFile reports whether TLS is required for gpfile requests (kXR_tlsGPF).
func (resp *Response) TLSForGPFile() bool { return resp.flagBits()&flagTLSGPF != 0 }

// TLSForAnonGPFile reports whether TLS is required for gpfile requests made by
// unauthenticated clients (kXR_tlsGPFA).
func (resp *Response) TLSForAnonGPFile() bool { return resp.flagBits()&flagTLSGPFA != 0 }

// TLSForAnything reports whether the server asks for TLS around any phase or
// request kind at all (kXR_tlsAny). A client that speaks no TLS learns from
// this in one test that it is going to be turned away eventually, rather than
// finding out one request at a time.
func (resp *Response) TLSForAnything() bool { return resp.flagBits()&flagTLSAny != 0 }

// SupportsGPFile reports whether the server offers Grouped Parallel Fetch
// (kXR_supgpf).
//
// The request itself, kXR_gpfile, was retired in XRootD v5 and this client does
// not send it; see [go-hep.org/x/hep/xrootd/xrdproto/gpfile]. The flag is still
// read because a server that sets it is telling the truth about itself, and
// because a caller comparing this client's view of a server against another's
// needs to see the same bits.
func (resp *Response) SupportsGPFile() bool { return resp.flagBits()&flagSupGPF != 0 }

// AllowsAnonGPFile reports whether Grouped Parallel Fetch is open to
// unauthenticated clients (kXR_anongpf). It is meaningless without
// [Response.SupportsGPFile].
func (resp *Response) AllowsAnonGPFile() bool { return resp.flagBits()&flagAnonGPF != 0 }

// NeedsTLS reports whether the client must upgrade the connection to TLS before
// login. This is true when the server mandates it (kXR_gotoTLS) or when the
// client wanted TLS and the server supports it (kXR_haveTLS).
func (resp *Response) NeedsTLS(wantTLS bool) bool {
	return resp.GotoTLS() || (wantTLS && resp.HasTLS())
}

// MarshalXrd implements xrdproto.Marshaler.
func (o Response) MarshalXrd(wBuffer *xrdenc.WBuffer) error {
	wBuffer.WriteI32(o.BinaryProtocolVersion)
	wBuffer.WriteI32(int32(o.Flags))
	if !o.HasSecurityInfo {
		return nil
	}
	wBuffer.WriteU8('S')
	wBuffer.Next(1)
	wBuffer.WriteU8(o.SecurityVersion)
	wBuffer.WriteU8(byte(o.SecurityOptions))
	wBuffer.WriteU8(byte(o.SecurityLevel))
	wBuffer.WriteU8(uint8(len(o.SecurityOverrides)))
	for _, x := range o.SecurityOverrides {
		err := x.MarshalXrd(wBuffer)
		if err != nil {
			return err
		}
	}
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (o *Response) UnmarshalXrd(rBuffer *xrdenc.RBuffer) error {
	o.BinaryProtocolVersion = rBuffer.ReadI32()
	o.Flags = Flags(rBuffer.ReadI32())
	if rBuffer.Len() == 0 {
		return rBuffer.Err()
	}
	o.HasSecurityInfo = true
	rBuffer.Skip(1)
	rBuffer.Skip(1)
	o.SecurityVersion = rBuffer.ReadU8()
	o.SecurityOptions = SecurityOptions(rBuffer.ReadU8())
	o.SecurityLevel = xrdproto.SecurityLevel(rBuffer.ReadU8())
	o.SecurityOverrides = make([]xrdproto.SecurityOverride, rBuffer.ReadU8())
	for i := range o.SecurityOverrides {
		err := o.SecurityOverrides[i].UnmarshalXrd(rBuffer)
		if err != nil {
			return err
		}
	}
	return rBuffer.Err()
}

// RespID implements xrdproto.Response.RespID.
func (resp *Response) RespID() uint16 { return RequestID }

// ShouldSign implements xrdproto.Request.ShouldSign.
func (req *Request) ShouldSign() bool { return false }
