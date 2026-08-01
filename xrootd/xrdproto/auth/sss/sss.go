// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sss // import "go-hep.org/x/hep/xrootd/xrdproto/auth/sss"

import (
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"os/user"
	"time"

	"golang.org/x/crypto/blowfish"

	"go-hep.org/x/hep/xrootd/xrdproto/auth"
)

// Type is the 4-byte credential type for the sss provider.
var Type = [4]byte{'s', 's', 's', 0}

// baseTime is the SSS timestamp epoch (2008-09-23T13:51:20Z); gen_time is
// seconds since this epoch, keeping the uint32 field valid past 2038.
const baseTime = 1222183880

// dataHdrLen is the fixed cleartext prefix: 32-byte nonce + 4-byte gen_time +
// 3 reserved + 1 option byte.
const dataHdrLen = 40

// typeName is the identity TLV tag for the login name.
const typeName = 0x01

// optUseData marks a self-contained credential (identity inline).
const optUseData = 0x00

// buildCredential assembles an SSS credential blob for key: a 16-byte outer
// header followed by the Blowfish-CFB64 encryption of (cleartext + IEEE-CRC32).
func buildCredential(key Key, username string, nonce [32]byte, genTime uint32) ([]byte, error) {
	if len(key.Key) == 0 {
		return nil, fmt.Errorf("auth/sss: empty key")
	}
	if username == "" {
		username = "xrd"
	}
	name := append([]byte(username), 0) // NUL-terminated; len includes the NUL
	if len(name) > 64 {
		name = name[:64]
		name[63] = 0
	}

	// Cleartext: 40-byte data header + NAME TLV.
	clear := make([]byte, 0, dataHdrLen+3+len(name))
	hdr := make([]byte, dataHdrLen)
	copy(hdr[:32], nonce[:])
	binary.BigEndian.PutUint32(hdr[32:36], genTime)
	hdr[39] = optUseData
	clear = append(clear, hdr...)
	clear = append(clear, typeName, 0x00, byte(len(name)))
	clear = append(clear, name...)

	// plain = cleartext + IEEE-CRC32 (big-endian).
	plain := make([]byte, len(clear)+4)
	copy(plain, clear)
	binary.BigEndian.PutUint32(plain[len(clear):], crc32.ChecksumIEEE(clear))

	// Blowfish-CFB64 encrypt with an all-zero 8-byte IV, no padding. This is the
	// mandated SSS wire format (OpenSSL EVP_bf_cfb64), not a new protocol choice.
	bc, err := blowfish.NewCipher(key.Key)
	if err != nil {
		return nil, fmt.Errorf("auth/sss: blowfish: %w", err)
	}
	enc := make([]byte, len(plain))
	cipher.NewCFBEncrypter(bc, make([]byte, blowfish.BlockSize)).XORKeyStream(enc, plain)

	// 16-byte outer header + encrypted body.
	out := make([]byte, 16+len(enc))
	out[0], out[1], out[2], out[3] = 's', 's', 's', 0
	out[4] = 1   // version
	out[5] = 0   // spare
	out[6] = 0   // kn_size: no named key
	out[7] = '0' // enc marker: Blowfish-CFB64
	binary.BigEndian.PutUint64(out[8:16], uint64(key.ID))
	copy(out[16:], enc)
	return out, nil
}

// Auth is the "sss" security provider carrying a shared-secret key.
type Auth struct {
	Key  Key    // the shared key from the keytab
	User string // the login name embedded in the credential
}

// Provider returns the name of the security provider.
func (*Auth) Provider() string { return "sss" }

// Request forms a kXR_auth request carrying a freshly minted SSS credential.
func (a *Auth) Request(params []string) (*auth.Request, error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("auth/sss: could not read nonce: %w", err)
	}
	genTime := uint32(time.Now().Unix() - baseTime)
	blob, err := buildCredential(a.Key, a.User, nonce, genTime)
	if err != nil {
		return nil, err
	}
	return &auth.Request{Type: Type, Credentials: string(blob)}, nil
}

// New builds an Auth from the first live keytab key and the current OS user.
func New() (*Auth, error) {
	keys, err := LoadKeytab()
	if err != nil {
		return nil, err
	}
	return newFromKeys(keys)
}

// NewFromKeytab builds an Auth from the keytab at path, which is what a user
// who names a keytab means: the same rules as an ambient one, applied to a file
// of their choosing.
func NewFromKeytab(path string) (*Auth, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("auth/sss: could not open keytab: %w", err)
	}
	defer f.Close()
	keys, err := ParseKeytab(f)
	if err != nil {
		return nil, err
	}
	return newFromKeys(keys)
}

func newFromKeys(keys []Key) (*Auth, error) {
	k, err := FirstLiveKey(keys, time.Now())
	if err != nil {
		return nil, err
	}
	name := "xrd"
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
	}
	return &Auth{Key: k, User: name}, nil
}

// Default is the sss provider discovered from the ambient keytab, or nil when
// no keytab/live key is available.
var Default auth.Auther

// DefaultErr is why Default is nil: an *auth.Missing naming the keytab
// locations that were consulted. It is nil when Default was discovered.
var DefaultErr error

func init() {
	a, err := New()
	switch err {
	case nil:
		Default = a
	default:
		DefaultErr = &auth.Missing{
			Provider: "sss",
			What:     "shared-secret keytab",
			Searched: KeytabLocations(),
			Err:      err,
		}
	}
}

var _ auth.Auther = (*Auth)(nil)
