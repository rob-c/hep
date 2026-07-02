// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gsi

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"encoding/asn1"
	"encoding/pem"
	"math/big"
	"testing"
)

// testDHParamsPEM builds a PEM "DH PARAMETERS" block for a small-but-valid
// safe-ish group used only to exercise the codec and agreement logic.
func testDHParamsPEM(t *testing.T) ([]byte, dhParams) {
	t.Helper()
	// A 512-bit prime is enough to test the math quickly; production uses
	// ffdhe2048 supplied by the server.
	p, ok := new(big.Int).SetString(
		"00c90fdaa22168c234c4c6628b80dc1cd129024e088a67cc74020bbea63b139b"+
			"22514a08798e3404ddef9519b3cd3a431b302b0a6df25f14374fe1356d6d51c245e485b576625e7ec6f44c42e9a63a3620ffffffffffffffff", 16)
	if !ok {
		t.Fatal("bad test prime")
	}
	params := dhParams{P: p, G: big.NewInt(2)}
	der, err := asn1.Marshal(struct {
		P *big.Int
		G *big.Int
	}{p, big.NewInt(2)})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "DH PARAMETERS", Bytes: der})
	return pemBytes, params
}

func TestDHAgreement(t *testing.T) {
	pemBytes, params := testDHParamsPEM(t)

	// Server side: generate a key and encode its public blob.
	server, err := generateKey(params)
	if err != nil {
		t.Fatalf("server key: %v", err)
	}
	blob := encodePublicBlob(pemBytes, server.pub)

	// Client side: parse the blob, generate a key, agree the session key.
	peer, err := parsePeerBlob(blob)
	if err != nil {
		t.Fatalf("parsePeerBlob: %v", err)
	}
	if peer.params.P.Cmp(params.P) != 0 || peer.params.G.Cmp(params.G) != 0 {
		t.Fatal("parsed params mismatch")
	}
	client, err := generateKey(peer.params)
	if err != nil {
		t.Fatalf("client key: %v", err)
	}

	clientKey, err := client.sessionKey(peer.pub, 16)
	if err != nil {
		t.Fatalf("client session key: %v", err)
	}
	serverKey, err := server.sessionKey(client.pub, 16)
	if err != nil {
		t.Fatalf("server session key: %v", err)
	}
	if !bytes.Equal(clientKey, serverKey) {
		t.Fatalf("session keys disagree:\nclient=%x\nserver=%x", clientKey, serverKey)
	}
}

func TestPublicBlobRoundTrip(t *testing.T) {
	pemBytes, params := testDHParamsPEM(t)
	k, err := generateKey(params)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	blob := encodePublicBlob(pemBytes, k.pub)

	// The blob must carry the verbatim PEM params and uppercase hex.
	if !bytes.HasPrefix(blob, pemBytes) {
		t.Fatal("blob does not start with the PEM params")
	}
	peer, err := parsePeerBlob(blob)
	if err != nil {
		t.Fatalf("parsePeerBlob: %v", err)
	}
	if peer.pub.Cmp(k.pub) != 0 {
		t.Fatal("round-tripped public value mismatch")
	}
}

func TestAESCBCEncryptMatchesStdlib(t *testing.T) {
	key := bytes.Repeat([]byte{0x11}, 16)
	plaintext := []byte("gsi inner buffer contents")
	enc, err := aesCBCEncrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// Decrypt independently and strip PKCS#7.
	block, _ := aes.NewCipher(key)
	dec := make([]byte, len(enc))
	cipher.NewCBCDecrypter(block, make([]byte, 16)).CryptBlocks(dec, enc)
	n := int(dec[len(dec)-1])
	if n < 1 || n > 16 || n > len(dec) {
		t.Fatalf("bad padding byte %d", n)
	}
	if !bytes.Equal(dec[:len(dec)-n], plaintext) {
		t.Fatalf("roundtrip mismatch: %q", dec[:len(dec)-n])
	}
}

func TestSignRTagVerifies(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa: %v", err)
	}
	tag := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	sig, err := signRTag(key, tag)
	if err != nil {
		t.Fatalf("signRTag: %v", err)
	}
	// The server recovers with the public key: PKCS#1 v1.5, no digest.
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.Hash(0), tag, sig); err != nil {
		t.Fatalf("verify: %v", err)
	}
}
