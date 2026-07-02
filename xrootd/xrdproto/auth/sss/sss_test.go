// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sss

import (
	"bytes"
	"crypto/cipher"
	"encoding/binary"
	"hash/crc32"
	"testing"

	"golang.org/x/crypto/blowfish"
)

func TestBuildCredentialLayout(t *testing.T) {
	key := Key{ID: 0x0102030405060708, Key: []byte("0123456789abcdef0123456789abcdef")}
	var nonce [32]byte
	for i := range nonce {
		nonce[i] = byte(i)
	}
	blob, err := buildCredential(key, "alice", nonce, 42)
	if err != nil {
		t.Fatalf("buildCredential: %v", err)
	}

	// Outer header (16 bytes).
	if !bytes.Equal(blob[:4], []byte{'s', 's', 's', 0}) {
		t.Fatalf("magic: % x", blob[:4])
	}
	if blob[4] != 1 || blob[5] != 0 || blob[6] != 0 || blob[7] != '0' {
		t.Fatalf("header bytes: % x", blob[4:8])
	}
	if got := binary.BigEndian.Uint64(blob[8:16]); got != uint64(key.ID) {
		t.Fatalf("key id: got=%#x want=%#x", got, uint64(key.ID))
	}

	// Decrypt the body with Blowfish-CFB64, zero IV, to check the cleartext.
	bc, err := blowfish.NewCipher(key.Key)
	if err != nil {
		t.Fatalf("blowfish: %v", err)
	}
	body := blob[16:]
	plain := make([]byte, len(body))
	cipher.NewCFBDecrypter(bc, make([]byte, blowfish.BlockSize)).XORKeyStream(plain, body)

	if !bytes.Equal(plain[:32], nonce[:]) {
		t.Fatalf("nonce mismatch")
	}
	if got := binary.BigEndian.Uint32(plain[32:36]); got != 42 {
		t.Fatalf("gen_time: got=%d want=42", got)
	}
	if plain[39] != 0x00 {
		t.Fatalf("opt byte: got=%#x want=0", plain[39])
	}
	// NAME TLV at [40:]: type=1, 0, len, "alice\0".
	if plain[40] != 0x01 || plain[41] != 0x00 {
		t.Fatalf("TLV tag: % x", plain[40:42])
	}
	nlen := int(plain[42])
	if want := len("alice") + 1; nlen != want {
		t.Fatalf("TLV len: got=%d want=%d", nlen, want)
	}
	name := plain[43 : 43+nlen]
	if string(name) != "alice\x00" {
		t.Fatalf("TLV name: %q", name)
	}
	// Trailing IEEE-CRC32 (big-endian) over the cleartext.
	clearLen := 43 + nlen
	crc := binary.BigEndian.Uint32(plain[clearLen : clearLen+4])
	if want := crc32.ChecksumIEEE(plain[:clearLen]); crc != want {
		t.Fatalf("crc: got=%#x want=%#x", crc, want)
	}
}

func TestAuthProviderRequest(t *testing.T) {
	a := Auth{Key: Key{ID: 1, Key: bytes.Repeat([]byte{0xab}, 32)}, User: "bob"}
	if got, want := a.Provider(), "sss"; got != want {
		t.Fatalf("provider: got=%q want=%q", got, want)
	}
	req, err := a.Request(nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if req.Type != Type {
		t.Fatalf("type: %v", req.Type)
	}
	if len(req.Credentials) < 16 || req.Credentials[:4] != "sss\x00" {
		t.Fatalf("credentials prefix: %q", req.Credentials[:4])
	}
}
