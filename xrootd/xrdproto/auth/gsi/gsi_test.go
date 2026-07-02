// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gsi

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestMessageRoundTrip(t *testing.T) {
	buckets := []Bucket{
		{Type: BucketCryptoMod, Data: []byte("ssl")},
		{Type: BucketRTag, Data: []byte{1, 2, 3, 4, 5, 6, 7, 8}},
	}
	wire := EncodeMessage(StepClientCertReq, buckets)

	// The message starts with "gsi\0" then the big-endian step.
	if !bytes.HasPrefix(wire, []byte("gsi\x00")) {
		t.Fatalf("missing gsi name prefix: % x", wire[:4])
	}
	if got := binary.BigEndian.Uint32(wire[4:8]); got != StepClientCertReq {
		t.Fatalf("step: got=%d want=%d", got, StepClientCertReq)
	}
	// Ends with the terminator (uint32 BucketNone).
	if got := binary.BigEndian.Uint32(wire[len(wire)-4:]); got != BucketNone {
		t.Fatalf("terminator: got=%d want=%d", got, BucketNone)
	}

	step, got, err := DecodeMessage(wire)
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}
	if step != StepClientCertReq {
		t.Fatalf("decoded step: got=%d want=%d", step, StepClientCertReq)
	}
	if len(got) != 2 || got[0].Type != BucketCryptoMod || string(got[0].Data) != "ssl" {
		t.Fatalf("decoded buckets mismatch: %+v", got)
	}
	if got[1].Type != BucketRTag || !bytes.Equal(got[1].Data, buckets[1].Data) {
		t.Fatalf("decoded rtag bucket mismatch: %+v", got[1])
	}
}

func TestFindBucket(t *testing.T) {
	wire := EncodeMessage(StepServerCert, []Bucket{
		{Type: BucketPuk, Data: []byte("dh-public")},
		{Type: BucketX509, Data: []byte("-----BEGIN CERTIFICATE-----")},
	})
	if data, ok := FindBucket(wire, BucketX509); !ok || string(data) != "-----BEGIN CERTIFICATE-----" {
		t.Fatalf("FindBucket x509: ok=%v data=%q", ok, data)
	}
	if _, ok := FindBucket(wire, BucketSignedRTag); ok {
		t.Fatal("FindBucket found a bucket that is not present")
	}
}

func TestBuildCertReq(t *testing.T) {
	rtag := []byte{0xde, 0xad, 0xbe, 0xef, 0x01, 0x02, 0x03, 0x04}
	wire := BuildCertReq("ssl", Version, "abcd1234", 0x80, rtag)

	step, buckets, err := DecodeMessage(wire)
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}
	if step != StepClientCertReq {
		t.Fatalf("step: got=%d want=%d", step, StepClientCertReq)
	}

	byType := map[uint32][]byte{}
	for _, b := range buckets {
		byType[b.Type] = b.Data
	}
	if string(byType[BucketCryptoMod]) != "ssl" {
		t.Fatalf("cryptomod: %q", byType[BucketCryptoMod])
	}
	if v := binary.BigEndian.Uint32(byType[BucketVersion]); v != Version {
		t.Fatalf("version: got=%d want=%d", v, Version)
	}
	if string(byType[BucketIssuerHash]) != "abcd1234" {
		t.Fatalf("issuer hash: %q", byType[BucketIssuerHash])
	}
	if o := binary.BigEndian.Uint32(byType[BucketClntOpts]); o != 0x80 {
		t.Fatalf("clnt opts: got=%#x want=0x80", o)
	}

	// The nested main buffer carries the rtag under a kXGC_certreq step.
	main, ok := byType[BucketMain]
	if !ok {
		t.Fatal("missing main bucket")
	}
	innerStep, inner, err := DecodeMessage(main)
	if err != nil {
		t.Fatalf("decode inner main: %v", err)
	}
	if innerStep != StepClientCertReq {
		t.Fatalf("inner step: got=%d want=%d", innerStep, StepClientCertReq)
	}
	if len(inner) != 1 || inner[0].Type != BucketRTag || !bytes.Equal(inner[0].Data, rtag) {
		t.Fatalf("inner rtag bucket mismatch: %+v", inner)
	}
}

func TestDecodeTruncated(t *testing.T) {
	// A bucket claiming more data than present must error.
	bad := append([]byte("gsi\x00"), make([]byte, 4)...) // step
	bad = appendU32(bad, BucketRTag)
	bad = appendU32(bad, 100) // claims 100 bytes
	bad = append(bad, 1, 2, 3)
	if _, _, err := DecodeMessage(bad); err == nil {
		t.Fatal("expected error for truncated bucket")
	}
}
