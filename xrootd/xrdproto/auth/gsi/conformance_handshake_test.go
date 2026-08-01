// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the two client rounds of the GSI handshake, driven against a
// server side written here rather than against the client's own helpers.
//
// The unit tests next to this one check each piece against itself: the message
// codec round-trips, the DH agreement agrees, the signature verifies. What that
// cannot catch is the client assembling correct pieces into a message the
// server cannot use — the proxy encrypted under a key derived from the wrong
// public value, the tag signed being the client's own rather than the server's,
// the session cipher agreed at the wrong length. So the server here does what
// XrdSecgsi does: it derives the session key from what the client actually
// sent, decrypts with it, and verifies the proof of possession against the
// certificate it was handed.

package gsi

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rsa"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"strings"
	"testing"
)

// gsiProxy loads a freshly generated proxy the way a client finds one on disk.
func gsiProxy(t *testing.T) *Auth {
	t.Helper()
	a, err := LoadProxy(writeProxy(t, t.TempDir(), false))
	if err != nil {
		t.Fatalf("could not load the proxy: %v", err)
	}
	return a
}

// gsiServerCert builds a kXGS_cert challenge the way an unsigned-DH server
// does: its own DH public value, and a random tag inside the main buffer for
// the client to sign. It returns the challenge and the server's DH key.
func gsiServerCert(t *testing.T, rtag []byte) ([]byte, dhKey) {
	t.Helper()

	paramsPEM, params := testDHParamsPEM(t)
	key, err := generateKey(params)
	if err != nil {
		t.Fatalf("could not generate the server DH key: %v", err)
	}

	main := EncodeMessage(StepServerCert, []Bucket{{Type: BucketRTag, Data: rtag}})
	msg := EncodeMessage(StepServerCert, []Bucket{
		{Type: BucketCryptoMod, Data: []byte("ssl")},
		{Type: BucketPuk, Data: encodePublicBlob(paramsPEM, key.pub)},
		{Type: BucketMain, Data: main},
	})
	return msg, key
}

// gsiDecrypt undoes the client's session cipher: AES-CBC under a zero IV, with
// PKCS#7 padding, which is what the unsigned-DH path specifies.
func gsiDecrypt(t *testing.T, key, ciphertext []byte) []byte {
	t.Helper()

	blk, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("could not build the session cipher: %v", err)
	}
	if len(ciphertext) == 0 || len(ciphertext)%blk.BlockSize() != 0 {
		t.Fatalf("the encrypted buffer is %d bytes, not a whole number of blocks", len(ciphertext))
	}

	out := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(blk, make([]byte, blk.BlockSize())).CryptBlocks(out, ciphertext)

	pad := int(out[len(out)-1])
	if pad <= 0 || pad > blk.BlockSize() || pad > len(out) {
		t.Fatalf("the padding byte is %d, which no PKCS#7 padding can be", pad)
	}
	return out[:len(out)-pad]
}

func TestConformance_AGSIProviderNamesItselfTheWayTheServerAdvertisesIt(t *testing.T) {
	a := gsiProxy(t)
	if got := a.Provider(); got != "gsi" {
		t.Fatalf("the provider calls itself %q, want %q", got, "gsi")
	}
	// The credential type is four bytes with a trailing NUL, not "gsi " —
	// the server compares all four.
	if Type != [4]byte{'g', 's', 'i', 0} {
		t.Fatalf("the credential type is %q", Type)
	}
}

func TestConformance_AProviderWithNoProxyDoesNotStartAHandshake(t *testing.T) {
	// The zero Auth is what a caller gets from a proxy that failed to load.
	// Offering gsi anyway would fail the login instead of falling through to
	// the next protocol the server advertised.
	for _, tc := range []struct {
		name string
		auth *Auth
	}{
		{"nothing at all", &Auth{}},
		{"a chain with no key", &Auth{ProxyPEM: []byte("-----BEGIN CERTIFICATE-----\n")}},
		{"a key with no chain", &Auth{ProxyKey: gsiProxy(t).ProxyKey}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.auth.Request(nil)
			if err == nil {
				t.Fatal("a provider with no credential started a handshake")
			}
			if !strings.Contains(err.Error(), "no proxy credential") {
				t.Fatalf("the refusal does not say what is missing: %v", err)
			}
		})
	}
}

func TestConformance_TheCertificateRequestRepeatsTheServersChoices(t *testing.T) {
	// The server advertises its crypto module and CA hash in the protocol
	// parameters; the certreq has to echo them back, because the server uses
	// them to select which of its own credentials to answer with.
	a := gsiProxy(t)

	req, err := a.Request([]string{"c:ssl", "ca:8d4b1c9f", "v:10400"})
	if err != nil {
		t.Fatalf("could not build the certificate request: %v", err)
	}
	if req.Type != Type {
		t.Fatalf("the request is typed %q, want %q", req.Type, Type)
	}

	msg := []byte(req.Credentials)
	step, _, err := DecodeMessage(msg)
	if err != nil {
		t.Fatalf("the client built a message it cannot decode itself: %v", err)
	}
	if step != StepClientCertReq {
		t.Fatalf("the first client message is step %d, want kXGC_certreq (%d)", step, StepClientCertReq)
	}

	for _, tc := range []struct {
		name   string
		bucket uint32
		want   string
	}{
		{"the crypto module", BucketCryptoMod, "ssl"},
		{"the issuer hash", BucketIssuerHash, "8d4b1c9f"},
	} {
		got, ok := FindBucket(msg, tc.bucket)
		if !ok {
			t.Fatalf("the request carries no bucket for %s", tc.name)
		}
		if strings.TrimRight(string(got), "\x00") != tc.want {
			t.Fatalf("%s is %q, want %q", tc.name, got, tc.want)
		}
	}

	// The version selects the unsigned-DH path. Advertising the version the
	// server offered (10400) instead would put it on the signed path, which
	// this client cannot answer.
	v, ok := FindBucket(msg, BucketVersion)
	if !ok {
		t.Fatal("the request carries no version")
	}
	if got := binary.BigEndian.Uint32(v); got != unsignedVersion {
		t.Fatalf("the request advertises version %d, want %d", got, unsignedVersion)
	}
}

func TestConformance_ACertificateRequestWithoutParametersStillPicksACryptoModule(t *testing.T) {
	// Some servers advertise gsi with no parameters at all. "ssl" is the only
	// module XrdSecgsi ships, so it is the right default; an empty one would
	// make the server reject the message.
	a := gsiProxy(t)

	for _, params := range [][]string{nil, {"c:"}} {
		req, err := a.Request(params)
		if err != nil {
			t.Fatalf("could not build the certificate request: %v", err)
		}
		got, ok := FindBucket([]byte(req.Credentials), BucketCryptoMod)
		if !ok {
			t.Fatal("the request names no crypto module")
		}
		if want := "ssl"; strings.TrimRight(string(got), "\x00") != want {
			t.Fatalf("the crypto module is %q, want %q", got, want)
		}
	}
}

func TestConformance_TheCertificateResponseIsUsableByTheServerThatChallengedIt(t *testing.T) {
	// The whole round, checked from the server's side: agree the key from the
	// client's public value, decrypt the main buffer with it, and verify the
	// signature over the tag the server itself chose against the certificate
	// the client just handed over.
	a := gsiProxy(t)
	if _, err := a.Request([]string{"c:ssl"}); err != nil {
		t.Fatalf("could not build the certificate request: %v", err)
	}

	rtag := []byte("12345678")
	challenge, server := gsiServerCert(t, rtag)

	resp, err := a.More(challenge)
	if err != nil {
		t.Fatalf("could not answer the server challenge: %v", err)
	}
	msg := []byte(resp.Credentials)

	step, _, err := DecodeMessage(msg)
	if err != nil {
		t.Fatalf("the client built a message it cannot decode itself: %v", err)
	}
	if step != StepClientCert {
		t.Fatalf("the answer is step %d, want kXGC_cert (%d)", step, StepClientCert)
	}

	// The session cipher and digest have to be named, or the server does not
	// know how to read the main buffer it is about to be handed.
	for _, tc := range []struct {
		bucket uint32
		want   string
	}{
		{BucketCryptoMod, "ssl"},
		{BucketCipherAlg, "aes-128-cbc"},
		{BucketMDAlg, "sha256"},
	} {
		got, ok := FindBucket(msg, tc.bucket)
		if !ok {
			t.Fatalf("the answer carries no bucket %d", tc.bucket)
		}
		if strings.TrimRight(string(got), "\x00") != tc.want {
			t.Fatalf("bucket %d is %q, want %q", tc.bucket, got, tc.want)
		}
	}

	blob, ok := FindBucket(msg, BucketPuk)
	if !ok {
		t.Fatal("the answer carries no client DH public value")
	}
	peer, err := parsePeerBlob(blob)
	if err != nil {
		t.Fatalf("the server cannot parse the client public value: %v", err)
	}
	sessionKey, err := server.sessionKey(peer.pub, 16)
	if err != nil {
		t.Fatalf("the server cannot agree a session key: %v", err)
	}

	enc, ok := FindBucket(msg, BucketMain)
	if !ok {
		t.Fatal("the answer carries no main buffer")
	}
	inner := gsiDecrypt(t, sessionKey, enc)

	if _, _, err := DecodeMessage(inner); err != nil {
		t.Fatalf("the decrypted main buffer is not a GSI message: %v", err)
	}

	chain, ok := FindBucket(inner, BucketX509)
	if !ok {
		t.Fatal("the encrypted buffer carries no certificate chain")
	}
	if !bytes.Equal(chain, a.ProxyPEM) {
		t.Fatal("the chain sent is not the proxy that was loaded")
	}

	blk, _ := pem.Decode(chain)
	if blk == nil {
		t.Fatal("the chain is not PEM")
	}
	cert, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		t.Fatalf("the server cannot parse the proxy certificate: %v", err)
	}

	sig, ok := FindBucket(inner, BucketSignedRTag)
	if !ok {
		t.Fatal("the client did not prove possession of the proxy key")
	}
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		t.Fatalf("the proxy certificate holds a %T public key", cert.PublicKey)
	}
	// The tag signed must be the server's, not one the client made up: a
	// client that signs its own tag proves nothing about this exchange.
	if err := rsa.VerifyPKCS1v15(pub, crypto.Hash(0), rtag, sig); err != nil {
		t.Fatalf("the proof of possession does not verify against the server tag: %v", err)
	}

	// And the client poses its own challenge in return.
	newTag, ok := FindBucket(inner, BucketRTag)
	if !ok {
		t.Fatal("the answer carries no client tag")
	}
	if bytes.Equal(newTag, rtag) {
		t.Fatal("the client echoed the server tag instead of choosing its own")
	}
}

func TestConformance_AChallengeWithNoTagIsStillAnswered(t *testing.T) {
	// Proof of possession is only asked for when the server sends a tag. With
	// none, the exchange still has to produce a usable certificate response
	// rather than a signature over nothing.
	a := gsiProxy(t)

	paramsPEM, params := testDHParamsPEM(t)
	key, err := generateKey(params)
	if err != nil {
		t.Fatalf("could not generate the server DH key: %v", err)
	}
	challenge := EncodeMessage(StepServerCert, []Bucket{
		{Type: BucketPuk, Data: encodePublicBlob(paramsPEM, key.pub)},
	})

	resp, err := a.More(challenge)
	if err != nil {
		t.Fatalf("could not answer a challenge with no tag: %v", err)
	}

	msg := []byte(resp.Credentials)
	blob, ok := FindBucket(msg, BucketPuk)
	if !ok {
		t.Fatal("the answer carries no client DH public value")
	}
	peer, err := parsePeerBlob(blob)
	if err != nil {
		t.Fatalf("the server cannot parse the client public value: %v", err)
	}
	sessionKey, err := key.sessionKey(peer.pub, 16)
	if err != nil {
		t.Fatalf("the server cannot agree a session key: %v", err)
	}
	enc, ok := FindBucket(msg, BucketMain)
	if !ok {
		t.Fatal("the answer carries no main buffer")
	}
	inner := gsiDecrypt(t, sessionKey, enc)

	if _, ok := FindBucket(inner, BucketX509); !ok {
		t.Fatal("the encrypted buffer carries no certificate chain")
	}
	if _, ok := FindBucket(inner, BucketSignedRTag); ok {
		t.Fatal("the client signed a tag the server never sent")
	}
}

func TestConformance_AChallengeThisClientCannotAnswerIsRefused(t *testing.T) {
	// Every one of these is a server doing something legitimate that this
	// client does not implement. Each has to fail with a message naming what
	// was asked for — the alternative is a handshake that stalls or, worse,
	// one that sends the proxy under a key the server did not choose.
	a := gsiProxy(t)
	paramsPEM, params := testDHParamsPEM(t)
	key, err := generateKey(params)
	if err != nil {
		t.Fatalf("could not generate the server DH key: %v", err)
	}

	for _, tc := range []struct {
		name      string
		challenge []byte
		want      string
	}{
		{
			name:      "a truncated message",
			challenge: []byte{'g', 's', 'i'},
			want:      "bad server challenge",
		},
		{
			name:      "a request to delegate the proxy",
			challenge: EncodeMessage(StepServerPxyReq, nil),
			want:      "delegation",
		},
		{
			name:      "a step from another phase of the handshake",
			challenge: EncodeMessage(StepServerInit, nil),
			want:      "unexpected server step",
		},
		{
			name:      "a challenge with no DH public value",
			challenge: EncodeMessage(StepServerCert, []Bucket{{Type: BucketCryptoMod, Data: []byte("ssl")}}),
			want:      "kXRS_puk",
		},
		{
			name: "a server on the signed-DH path",
			challenge: EncodeMessage(StepServerCert, []Bucket{
				{Type: BucketCipher, Data: encodePublicBlob(paramsPEM, key.pub)},
			}),
			want: "signed-DH",
		},
		{
			name: "a DH public value that is not one",
			challenge: EncodeMessage(StepServerCert, []Bucket{
				{Type: BucketPuk, Data: []byte("not a PEM parameter block")},
			}),
			want: "gsi",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := a.More(tc.challenge)
			if err == nil {
				t.Fatal("the client answered a challenge it cannot support")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the refusal does not mention %q: %v", tc.want, err)
			}
		})
	}
}
