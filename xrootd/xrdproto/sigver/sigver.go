// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sigver contains the structures describing sigver request.
package sigver // import "go-hep.org/x/hep/xrootd/xrdproto/sigver"

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto/pgwrite"
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

// An Encrypter encrypts a signature hash with the session cipher the security
// provider agreed with the server while authenticating. It is the encryption
// half of the stock XrdSecProtect secver-0 scheme: gsi supplies AES-128-CBC
// with a zero IV (the unsigned-DH path). A provider that agrees no cipher
// supplies no Encrypter, and such a session cannot sign.
type Encrypter func(hash []byte) ([]byte, error)

// NewRequest builds the kXR_sigver that authenticates the marshalled request
// data, whose request id is requestID, as the seqID'th request of the session.
//
// It implements the stock XrdSecProtect secver-0 scheme: a SHA-256 hash is
// taken over the sequence number, the 24-byte request header, and the payload
// the frame declares, and that hash is then encrypted with the session cipher
// (encrypt). The hash is what binds the request; the encryption is what makes
// it evidence, because the covered bytes are all on the wire in the clear and
// only the two ends of the authenticated exchange hold the cipher key. A
// plain — HMAC or bare — digest of those bytes is one any observer can
// recompute and put in front of a request of their own, which is why the
// signature must be the encrypted form and why a caller with no Encrypter has
// nothing to authenticate with and must not send the request at all.
//
// The payload is excluded from the hash — and the kXR_nodata flag set — for
// kXR_write and kXR_pgwrite, whose payloads may be large, unless the server
// asked for it with kXR_secOData (secOData). Every other request covers the
// bytes its length field declares; data streamed past that point (kXR_writev
// segments, a checkpoint's write) is not part of the request the server reads
// and so is not signed.
func NewRequest(encrypt Encrypter, requestID uint16, seqID int64, secOData bool, data []byte) (Request, error) {
	h := sha256.New()

	var s [8]byte
	binary.BigEndian.PutUint64(s[:], uint64(seqID))
	_, _ = h.Write(s[:])

	nodata := (requestID == write.RequestID || requestID == pgwrite.RequestID) && !secOData
	switch {
	case len(data) < requestFrameLength:
		_, _ = h.Write(data)
	case nodata:
		_, _ = h.Write(data[:requestFrameLength])
	default:
		_, _ = h.Write(data[:signedLength(data)])
	}

	signature, err := encrypt(h.Sum(nil))
	if err != nil {
		return Request{}, fmt.Errorf("xrootd: could not encrypt the kXR_sigver signature: %w", err)
	}

	var f Flags
	if nodata {
		f |= NoData
	}

	return Request{ID: requestID, SeqID: seqID, Crypto: 0x01, Signature: signature, Flags: f}, nil
}
