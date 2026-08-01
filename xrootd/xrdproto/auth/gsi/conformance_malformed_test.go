// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the GSI cryptographic primitives when the input is not what
// it claims to be.
//
// Every byte these functions parse comes from the network or from a proxy file
// on disk, and all of it is attacker-reachable before anything has been
// authenticated: the DH group, the peer's public value and the proxy key are
// read before the handshake has proved anything about who is on the other end.
// A parser that returns a zero value on malformed input hands the rest of the
// handshake a degenerate group, an all-zero session key, or a nil signing key —
// each of which produces a handshake that appears to succeed while encrypting
// the credential under something the peer chose.

package gsi

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
)

func TestConformance_DHParametersThatCannotBeParsedAreRejected(t *testing.T) {
	valid, _ := testDHParamsPEM(t)

	for _, tc := range []struct {
		name  string
		input []byte
		want  string
	}{
		{"nothing at all", nil, "no PEM block"},
		{"not PEM", []byte("P=23, G=5\n"), "no PEM block"},
		{"a PEM block that is not ASN.1", pem.EncodeToMemory(&pem.Block{
			Type: "DH PARAMETERS", Bytes: []byte("not DER at all"),
		}), "could not parse DH parameters"},
		{"the parameters of a different object", pem.EncodeToMemory(&pem.Block{
			Type: "DH PARAMETERS", Bytes: mustDER(t, struct{ Name string }{"ffdhe2048"}),
		}), "could not parse DH parameters"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseDHParamsPEM(tc.input)
			if err == nil {
				t.Fatal("malformed DH parameters were accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the failure says %q, want it to mention %q", err, tc.want)
			}
		})
	}

	// The control: the same function on the same input the handshake sends.
	if _, err := parseDHParamsPEM(valid); err != nil {
		t.Fatalf("well-formed DH parameters were rejected: %v", err)
	}
}

func TestConformance_APeerPublicBlobIsCheckedBeforeItIsUsed(t *testing.T) {
	pemBytes, params := testDHParamsPEM(t)

	for _, tc := range []struct {
		name string
		blob []byte
		want string
	}{
		{"no markers", []byte("just some bytes"), "malformed DH public blob"},
		{"an empty public value", append(append([]byte{}, pemBytes...), (bpub + epub + "-")...), "malformed DH public blob"},
		{"a public value that is not hex", append(append([]byte{}, pemBytes...), (bpub + "not-a-number" + epub + "-")...), "could not parse DH public hex"},
		{"parameters that are not PEM", []byte(bpub + "0F" + epub + "-"), "no PEM block"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parsePeerBlob(tc.blob)
			if err == nil {
				t.Fatal("a malformed public blob was accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the failure says %q, want it to mention %q", err, tc.want)
			}
		})
	}

	// The control: what the server actually sends.
	k, err := generateKey(params)
	if err != nil {
		t.Fatalf("could not generate a key: %v", err)
	}
	if _, err := parsePeerBlob(encodePublicBlob(pemBytes, k.pub)); err != nil {
		t.Fatalf("a well-formed public blob was rejected: %v", err)
	}
}

func TestConformance_ASharedSecretTooSmallToKeyAESIsNotStretched(t *testing.T) {
	// A server that offers a tiny group produces a shared secret of a few
	// bytes. Padding it out to 16 would key AES with mostly zeros — a session
	// key an eavesdropper can search — so the agreement has to fail instead.
	params := dhParams{P: big.NewInt(2357), G: big.NewInt(2)}
	k, err := generateKey(params)
	if err != nil {
		t.Fatalf("could not generate a key: %v", err)
	}

	_, err = k.sessionKey(big.NewInt(5), 16)
	if err == nil {
		t.Fatal("a two-byte shared secret was accepted as an AES-128 key")
	}
	if !strings.Contains(err.Error(), "too short") {
		t.Fatalf("the failure says %q, want it to say the secret is too short", err)
	}
}

func TestConformance_AKeyOfTheWrongSizeIsNotAnAESKey(t *testing.T) {
	// aesCBCEncrypt is fed the session key, whose length depends on what the
	// peer negotiated. AES accepts 16, 24 and 32 bytes and nothing else.
	for _, n := range []int{0, 7, 15, 17, 31} {
		if _, err := aesCBCEncrypt(make([]byte, n), []byte("payload")); err == nil {
			t.Errorf("a %d-byte AES key was accepted", n)
		}
	}
	if _, err := aesCBCEncrypt(make([]byte, 16), []byte("payload")); err != nil {
		t.Errorf("a 16-byte AES key was rejected: %v", err)
	}
}

func TestConformance_ATagTooLargeToSignIsAnErrorNotASignature(t *testing.T) {
	// The random tag comes from the server. PKCS#1 v1.5 without a digest can
	// only sign a message shorter than the modulus, so a server that sends an
	// oversized tag must get an error rather than a truncated signature.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("could not generate a key: %v", err)
	}

	if _, err := signRTag(key, make([]byte, 4096)); err == nil {
		t.Fatal("a tag larger than the modulus was signed")
	}

	// The control: the 8-byte tag the protocol actually carries.
	if _, err := signRTag(key, make([]byte, 8)); err != nil {
		t.Fatalf("could not sign an 8-byte tag: %v", err)
	}
}

func TestConformance_AProxyKeyThatIsNotAnRSAKeyIsRejected(t *testing.T) {
	// The proxy file is read from disk and its key is what proves possession.
	// Accepting a key of another kind, or a block that does not decode at all,
	// would leave the handshake with a nil signer and a panic somewhere later.
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("could not generate an EC key: %v", err)
	}
	ecDER, err := x509.MarshalPKCS8PrivateKey(ec)
	if err != nil {
		t.Fatalf("could not encode the EC key: %v", err)
	}

	for _, tc := range []struct {
		name string
		blk  *pem.Block
		want string
	}{
		{"not a key at all", &pem.Block{Type: "PRIVATE KEY", Bytes: []byte("nope")}, "could not parse proxy key"},
		{"an elliptic-curve key", &pem.Block{Type: "PRIVATE KEY", Bytes: ecDER}, "not RSA"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseRSAKey(tc.blk)
			if err == nil {
				t.Fatal("a proxy key that cannot sign was accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the failure says %q, want it to mention %q", err, tc.want)
			}
		})
	}

	// The control, in both encodings a proxy file may use.
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("could not generate an RSA key: %v", err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	if err != nil {
		t.Fatalf("could not encode the RSA key: %v", err)
	}
	for _, blk := range []*pem.Block{
		{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rsaKey)},
		{Type: "PRIVATE KEY", Bytes: pkcs8},
	} {
		got, err := parseRSAKey(blk)
		if err != nil {
			t.Fatalf("a %q block was rejected: %v", blk.Type, err)
		}
		if !got.Equal(rsaKey) {
			t.Fatalf("the %q block decoded to a different key", blk.Type)
		}
	}
}

// mustDER encodes v as ASN.1 DER.
func mustDER(t *testing.T, v any) []byte {
	t.Helper()

	der, err := asn1.Marshal(v)
	if err != nil {
		t.Fatalf("could not encode the test object: %v", err)
	}
	return der
}
