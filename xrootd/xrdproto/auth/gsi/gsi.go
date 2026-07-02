// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package gsi implements the wire codec for XRootD's GSI (X.509) security
// protocol: the XrdSutBuffer message framing and the buckets exchanged during
// the GSI handshake.
//
// A GSI message is the NUL-terminated protocol name "gsi\0", a big-endian
// uint32 step code, a sequence of buckets, and a terminator bucket. Each
// bucket is a big-endian uint32 type, a big-endian uint32 length, and that many
// data bytes; the terminator is the single uint32 value BucketNone.
//
// This package covers the message layer and the first client round
// (BuildCertReq), which involve no cryptography. The round-two crypto kernel
// (Diffie-Hellman agreement, AES session cipher, RSA proof-of-possession and
// X.509 proxy assembly) is intentionally not part of this package.
package gsi // import "go-hep.org/x/hep/xrootd/xrdproto/auth/gsi"

import (
	"encoding/binary"
	"fmt"
)

// GSI handshake step codes. Server steps are in the kXGS_* range, client steps
// in the kXGC_* range; the step is the uint32 following the "gsi\0" name.
const (
	StepServerInit    uint32 = 2000 // kXGS_init:    server → client initial exchange
	StepServerCert    uint32 = 2001 // kXGS_cert:    server → client cert + DH
	StepServerPxyReq  uint32 = 2002 // kXGS_pxyreq:  server → client proxy request
	StepClientCertReq uint32 = 1000 // kXGC_certreq: client → server cert request
	StepClientCert    uint32 = 1001 // kXGC_cert:    client → server cert + DH
	StepClientSigPxy  uint32 = 1002 // kXGC_sigpxy:  client → server signed proxy
)

// XrdSutBucket type codes.
const (
	BucketNone       uint32 = 0    // kXRS_none:        terminator
	BucketCryptoMod  uint32 = 3000 // kXRS_cryptomod:   crypto module name ("ssl")
	BucketMain       uint32 = 3001 // kXRS_main:        inner/encrypted main buffer
	BucketPuk        uint32 = 3004 // kXRS_puk:         server DH public key blob
	BucketCipher     uint32 = 3005 // kXRS_cipher:      DH public params / ciphertext
	BucketRTag       uint32 = 3006 // kXRS_rtag:        random challenge tag
	BucketSignedRTag uint32 = 3007 // kXRS_signed_rtag: signed random tag
	BucketUser       uint32 = 3008 // kXRS_user:        username string
	BucketVersion    uint32 = 3014 // kXRS_version:     protocol version (int32)
	BucketClntOpts   uint32 = 3019 // kXRS_clnt_opts:   client option flags (int32)
	BucketX509       uint32 = 3022 // kXRS_x509:        X.509 certificate (PEM)
	BucketIssuerHash uint32 = 3023 // kXRS_issuer_hash: CA subject name hash
	BucketX509Req    uint32 = 3024 // kXRS_x509_req:    X.509 certificate request
	BucketCipherAlg  uint32 = 3025 // kXRS_cipher_alg:  supported cipher algorithms
	BucketMDAlg      uint32 = 3026 // kXRS_md_alg:      supported digest algorithms
)

// Version is the GSI protocol version reported in a version bucket (2.01.00).
const Version uint32 = 20100

const protoName = "gsi\x00"

// Bucket is one type-length-value element of a GSI message.
type Bucket struct {
	Type uint32
	Data []byte
}

// EncodeMessage serialises a GSI message: the protocol name, the step code, the
// buckets in order, and the terminator.
func EncodeMessage(step uint32, buckets []Bucket) []byte {
	out := make([]byte, 0, 16+len(buckets)*16)
	out = append(out, protoName...)
	out = appendU32(out, step)
	for _, b := range buckets {
		out = appendU32(out, b.Type)
		out = appendU32(out, uint32(len(b.Data)))
		out = append(out, b.Data...)
	}
	out = appendU32(out, BucketNone)
	return out
}

// DecodeMessage parses a GSI message into its step code and buckets. Parsing
// stops at the terminator bucket or the end of the buffer.
func DecodeMessage(data []byte) (step uint32, buckets []Bucket, err error) {
	i := 0
	for i < len(data) && data[i] != 0 { // protocol name up to and incl. the NUL
		i++
	}
	i++ // skip the NUL
	if i+4 > len(data) {
		return 0, nil, fmt.Errorf("gsi: message too short for step code")
	}
	step = binary.BigEndian.Uint32(data[i:])
	i += 4

	for i+4 <= len(data) {
		btype := binary.BigEndian.Uint32(data[i:])
		i += 4
		if btype == BucketNone {
			break
		}
		if i+4 > len(data) {
			return 0, nil, fmt.Errorf("gsi: truncated bucket length for type %d", btype)
		}
		blen := int(binary.BigEndian.Uint32(data[i:]))
		i += 4
		if blen < 0 || i+blen > len(data) {
			return 0, nil, fmt.Errorf("gsi: bucket type %d announces %d bytes, %d available", btype, blen, len(data)-i)
		}
		buckets = append(buckets, Bucket{Type: btype, Data: append([]byte(nil), data[i:i+blen]...)})
		i += blen
	}
	return step, buckets, nil
}

// FindBucket returns the data of the first bucket of the given type in a GSI
// message, mirroring the reference client's xrootd_gsi_find_bucket.
func FindBucket(data []byte, typ uint32) ([]byte, bool) {
	_, buckets, err := DecodeMessage(data)
	if err != nil {
		return nil, false
	}
	for _, b := range buckets {
		if b.Type == typ {
			return b.Data, true
		}
	}
	return nil, false
}

// BuildCertReq builds the first client message (kXGC_certreq): the outer
// message carries the crypto module, protocol version, CA issuer hash, client
// options and a nested "main" buffer holding the random challenge tag the
// server will sign. It involves no cryptography.
func BuildCertReq(cryptomod string, version uint32, issuerHash string, clntOpts uint32, rtag []byte) []byte {
	if cryptomod == "" {
		cryptomod = "ssl"
	}
	inner := EncodeMessage(StepClientCertReq, []Bucket{
		{Type: BucketRTag, Data: rtag},
	})
	var verBE, optsBE [4]byte
	binary.BigEndian.PutUint32(verBE[:], version)
	binary.BigEndian.PutUint32(optsBE[:], clntOpts)
	return EncodeMessage(StepClientCertReq, []Bucket{
		{Type: BucketCryptoMod, Data: []byte(cryptomod)},
		{Type: BucketVersion, Data: verBE[:]},
		{Type: BucketIssuerHash, Data: []byte(issuerHash)},
		{Type: BucketClntOpts, Data: optsBE[:]},
		{Type: BucketMain, Data: inner},
	})
}

func appendU32(b []byte, v uint32) []byte {
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], v)
	return append(b, tmp[:]...)
}
