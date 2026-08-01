// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sigver contains the structures describing sigver request.
package sigver // import "go-hep.org/x/hep/xrootd/xrdproto/sigver"

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto/verifyw"
	"go-hep.org/x/hep/xrootd/xrdproto/write"
)

// RequestID is the id of the request, it is sent as part of message.
// See xrootd protocol specification for details: http://xrootd.org/doc/dev45/XRdv310.pdf, 2.3 Client Request Format.
const RequestID uint16 = 3029

// Flags are the request indicators.
type Flags uint8

const (
	NoData Flags = 1 // NoData indicates whether the data payload is included in the hash.
)

// Request holds the sigver request parameters.
type Request struct {
	ID        uint16 // ID is the requestID of the subsequent request.
	Version   byte   // Version is a version of the signature protocol to be used. Currently only the zero value is supported.
	Flags     Flags  // Flags are the request indicators. Currently only NoData is supported which indicates whether the data payload is included in the hash.
	SeqID     int64  // SeqID is a monotonically increasing sequence number. Each requests should have a sequence number that is greater than a previous one.
	Crypto    byte   // Crypto identifies the cryptography used to construct the signature.
	_         [3]byte
	Signature []byte
}

// ReqID implements xrdproto.Request.ReqID.
func (req *Request) ReqID() uint16 { return RequestID }

// ShouldSign implements xrdproto.Request.ShouldSign.
func (req *Request) ShouldSign() bool { return false }

// MarshalXrd implements xrdproto.Marshaler.
func (o Request) MarshalXrd(wBuffer *xrdenc.WBuffer) error {
	wBuffer.WriteU16(o.ID)
	wBuffer.WriteU8(o.Version)
	wBuffer.WriteU8(uint8(o.Flags))
	wBuffer.WriteI64(o.SeqID)
	wBuffer.WriteU8(o.Crypto)
	wBuffer.Next(3)
	wBuffer.WriteLen(len(o.Signature))
	wBuffer.WriteBytes(o.Signature)
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (o *Request) UnmarshalXrd(rBuffer *xrdenc.RBuffer) error {
	o.ID = rBuffer.ReadU16()
	o.Version = rBuffer.ReadU8()
	o.Flags = Flags(rBuffer.ReadU8())
	o.SeqID = rBuffer.ReadI64()
	o.Crypto = rBuffer.ReadU8()
	rBuffer.Skip(3)
	o.Signature = rBuffer.ReadLenBytes()
	return rBuffer.Err()
}

// requestFrameLength is the fixed part of every request: a 4-byte header, 16
// parameter bytes and the 4-byte data length.
const requestFrameLength = 24

// signedLength returns how much of a marshalled request the signature covers:
// the fixed frame plus exactly the number of payload bytes the frame declares.
//
// For nearly every request that is all of it. It is not for the two that write
// past their own frame — kXR_writev and a kXR_ckpXeq carrying a write — where
// the payload is followed by data the length field does not count. A server
// verifies a request by hashing what it read as the request, and it stops
// reading at the declared length; a client that hashed the trailing data too
// would produce a signature the server can never arrive at, and every signed
// request of that kind would be rejected as forged.
func signedLength(data []byte) int {
	if len(data) < requestFrameLength {
		return len(data)
	}
	dlen := int(binary.BigEndian.Uint32(data[requestFrameLength-4 : requestFrameLength]))
	if n := requestFrameLength + dlen; n >= requestFrameLength && n <= len(data) {
		return n
	}
	return len(data)
}

// NewRequest builds the kXR_sigver that authenticates the marshalled request
// data, whose request id is requestID, as the seqID'th request of the session.
//
// key is the session key agreed with the server while authenticating. It is
// what makes the signature worth anything: the covered bytes are all on the
// wire, so an unkeyed digest of them is one any observer can recompute and put
// in front of a request of their own. A caller with no key has nothing to
// authenticate with and must not send the request at all.
func NewRequest(key []byte, requestID uint16, seqID int64, data []byte) Request {
	mac := hmac.New(sha256.New, key)

	var s [8]byte
	binary.BigEndian.PutUint64(s[:], uint64(seqID))
	_, _ = mac.Write(s[:])

	if requestID == write.RequestID || requestID == verifyw.RequestID {
		_, _ = mac.Write(data[:requestFrameLength])
	} else {
		_, _ = mac.Write(data[:signedLength(data)])
	}
	signature := mac.Sum(nil)

	var f Flags
	if requestID == write.RequestID {
		f |= NoData
	}

	return Request{ID: requestID, SeqID: seqID, Crypto: 0x01, Signature: signature[:], Flags: f}
}
