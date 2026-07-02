// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gsi // import "go-hep.org/x/hep/xrootd/xrdproto/auth/gsi"

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"encoding/asn1"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
)

// The GSI DH public blob is the PEM-encoded DH parameters immediately followed
// by the delimiters "---BPUB---", the uppercase hex of the public value, and
// "---EPUB---" (the trailing delimiter is written without its final dash by the
// reference encoder, so parsing matches on the 9-byte "---EPUB--").
const (
	bpub = "---BPUB---"
	epub = "---EPUB--"
)

// dhParams holds the finite-field Diffie-Hellman group (prime and generator).
type dhParams struct {
	P *big.Int
	G *big.Int
}

// dhKey is an ephemeral DH key pair in a given group.
type dhKey struct {
	params dhParams
	priv   *big.Int
	pub    *big.Int
}

// parseDHParamsPEM decodes the prime and generator from a PEM "DH PARAMETERS"
// block (ASN.1 DHParameter: SEQUENCE { prime INTEGER, base INTEGER, ... }).
func parseDHParamsPEM(pemBytes []byte) (dhParams, error) {
	blk, _ := pem.Decode(pemBytes)
	if blk == nil {
		return dhParams{}, fmt.Errorf("gsi: no PEM block in DH parameters")
	}
	var seq struct {
		P *big.Int
		G *big.Int
		L int `asn1:"optional"`
	}
	if _, err := asn1.Unmarshal(blk.Bytes, &seq); err != nil {
		return dhParams{}, fmt.Errorf("gsi: could not parse DH parameters: %w", err)
	}
	if seq.P == nil || seq.G == nil {
		return dhParams{}, fmt.Errorf("gsi: incomplete DH parameters")
	}
	return dhParams{P: seq.P, G: seq.G}, nil
}

// peerPublic is the parsed server DH public blob: the raw PEM parameter bytes
// (echoed verbatim in the client's reply), the group, and the peer public value.
type peerPublic struct {
	paramsPEM []byte
	params    dhParams
	pub       *big.Int
}

// parsePeerBlob splits a GSI DH public blob into its PEM parameters and the
// hex-encoded public value.
func parsePeerBlob(blob []byte) (peerPublic, error) {
	i := bytes.Index(blob, []byte(bpub))
	j := bytes.Index(blob, []byte(epub))
	if i < 0 || j < 0 || j <= i+len(bpub) {
		return peerPublic{}, fmt.Errorf("gsi: malformed DH public blob")
	}
	paramsPEM := blob[:i]
	hexPub := string(blob[i+len(bpub) : j])
	pub, ok := new(big.Int).SetString(strings.TrimSpace(hexPub), 16)
	if !ok {
		return peerPublic{}, fmt.Errorf("gsi: could not parse DH public hex")
	}
	params, err := parseDHParamsPEM(paramsPEM)
	if err != nil {
		return peerPublic{}, err
	}
	return peerPublic{paramsPEM: append([]byte(nil), paramsPEM...), params: params, pub: pub}, nil
}

// generateKey creates an ephemeral DH key pair in the given group.
func generateKey(params dhParams) (dhKey, error) {
	// Private exponent in [2, p-2].
	max := new(big.Int).Sub(params.P, big.NewInt(2))
	priv, err := rand.Int(rand.Reader, max)
	if err != nil {
		return dhKey{}, fmt.Errorf("gsi: DH key generation failed: %w", err)
	}
	priv.Add(priv, big.NewInt(2))
	pub := new(big.Int).Exp(params.G, priv, params.P)
	return dhKey{params: params, priv: priv, pub: pub}, nil
}

// sessionKey computes the DH shared secret with peerPub and returns its first
// keyLen bytes, matching XrdSecgsi's unsigned-DH (HasPad=0) key derivation:
// the shared secret is the minimal big-endian representation (leading zeros
// stripped) and the session key is its leading keyLen bytes.
func (k dhKey) sessionKey(peerPub *big.Int, keyLen int) ([]byte, error) {
	secret := new(big.Int).Exp(peerPub, k.priv, k.params.P)
	b := secret.Bytes()
	if len(b) < keyLen {
		return nil, fmt.Errorf("gsi: DH shared secret too short (%d < %d)", len(b), keyLen)
	}
	return b[:keyLen], nil
}

// encodePublicBlob builds the client's DH public blob: the peer's PEM
// parameters (echoed verbatim) followed by the uppercase hex of pub between the
// BPUB/EPUB delimiters, as the reference encoder writes them.
func encodePublicBlob(paramsPEM []byte, pub *big.Int) []byte {
	var out bytes.Buffer
	out.Write(paramsPEM)
	out.WriteString(bpub)
	out.WriteString(strings.ToUpper(hex.EncodeToString(pub.Bytes())))
	out.WriteString("---EPUB---")
	return out.Bytes()
}

// signRTag signs the raw tag with the proxy RSA key using PKCS#1 v1.5 and no
// message digest, matching XrdCryptosslRSA::EncryptPrivate (the GSI
// proof-of-possession). The tag is far smaller than the modulus, so it is a
// single block.
func signRTag(key *rsa.PrivateKey, tag []byte) ([]byte, error) {
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.Hash(0), tag)
	if err != nil {
		return nil, fmt.Errorf("gsi: signing the server tag: %w", err)
	}
	return sig, nil
}

// aesCBCEncrypt encrypts plaintext with AES-CBC using a zero IV and PKCS#7
// padding (the unsigned-DH path, where no IV is prepended).
func aesCBCEncrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("gsi: AES cipher: %w", err)
	}
	padded := pkcs7Pad(plaintext, block.BlockSize())
	iv := make([]byte, block.BlockSize())
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	return out, nil
}

func pkcs7Pad(b []byte, blockSize int) []byte {
	n := blockSize - len(b)%blockSize
	pad := bytes.Repeat([]byte{byte(n)}, n)
	return append(append([]byte(nil), b...), pad...)
}
