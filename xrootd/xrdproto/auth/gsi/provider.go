// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gsi // import "go-hep.org/x/hep/xrootd/xrdproto/auth/gsi"

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strconv"
	"strings"

	"go-hep.org/x/hep/xrootd/xrdproto/auth"
)

// Type is the 4-byte credential type for the gsi provider.
var Type = [4]byte{'g', 's', 'i', 0}

// unsignedVersion is advertised in the certreq to select XrdSecgsi's simpler
// unsigned-DH path (any version below XrdSecgsiVersDHsigned=10400): the server
// sends a plain kXRS_puk DH public and a zero-IV session cipher.
const unsignedVersion = 10300

// clntOptsDefault matches a stock client's default options (delegated proxy off).
const clntOptsDefault = 0x80

// Default is a GSI provider configured from the ambient X.509 proxy found at
// DefaultProxyPath. If no usable proxy is present, Default is nil and the
// client simply never offers gsi.
//
// Discovery mirrors krb5, ztn and sss: a credential that happens to be sitting
// in the conventional place is usable without the caller wiring it by hand,
// which is what a stock client does.
var Default auth.Auther

func init() {
	if a, err := LoadProxy(DefaultProxyPath()); err == nil {
		Default = a
	}
}

// Auth is the GSI (X.509 proxy) security provider. It drives the two client
// rounds of the handshake: the initial certificate request and, after the
// server's kXGS_cert challenge, the certificate response carrying the proxy
// chain and the DH session material.
//
// Only the unsigned-DH path is implemented (see unsignedVersion); X.509
// delegation (kXGS_pxyreq) is not.
type Auth struct {
	// ProxyPEM is the proxy certificate chain (proxy cert + issuer chain) as
	// concatenated PEM certificate blocks.
	ProxyPEM []byte
	// ProxyKey is the proxy private key used for proof-of-possession.
	ProxyKey *rsa.PrivateKey

	cryptomod  string
	issuerHash string
}

// Provider returns the name of the security provider.
func (*Auth) Provider() string { return "gsi" }

// Request builds the first client message (kXGC_certreq). params are the GSI
// protocol parameters the server advertised (e.g. "c:ssl", "ca:<hash>").
func (a *Auth) Request(params []string) (*auth.Request, error) {
	if len(a.ProxyPEM) == 0 || a.ProxyKey == nil {
		return nil, fmt.Errorf("auth/gsi: no proxy credential")
	}
	a.cryptomod, a.issuerHash = "ssl", ""
	for _, p := range params {
		switch {
		case strings.HasPrefix(p, "c:"):
			a.cryptomod = strings.TrimPrefix(p, "c:")
		case strings.HasPrefix(p, "ca:"):
			a.issuerHash = strings.TrimPrefix(p, "ca:")
		}
	}
	if a.cryptomod == "" {
		a.cryptomod = "ssl"
	}

	var rtag [8]byte
	if _, err := rand.Read(rtag[:]); err != nil {
		return nil, fmt.Errorf("auth/gsi: RNG failed: %w", err)
	}
	msg := BuildCertReq(a.cryptomod, unsignedVersion, a.issuerHash, clntOptsDefault, rtag[:])
	return &auth.Request{Type: Type, Credentials: string(msg)}, nil
}

// More handles a server challenge (kXR_authmore). For a kXGS_cert challenge it
// returns the certificate response (kXGC_cert). A kXGS_pxyreq (delegation)
// challenge is rejected.
func (a *Auth) More(challenge []byte) (*auth.Request, error) {
	step, _, err := DecodeMessage(challenge)
	if err != nil {
		return nil, fmt.Errorf("auth/gsi: bad server challenge: %w", err)
	}
	switch step {
	case StepServerCert:
		resp, err := BuildCertResponse(challenge, a.ProxyPEM, a.ProxyKey)
		if err != nil {
			return nil, err
		}
		return &auth.Request{Type: Type, Credentials: string(resp)}, nil
	case StepServerPxyReq:
		return nil, fmt.Errorf("auth/gsi: server requested X.509 delegation, which is not supported")
	default:
		return nil, fmt.Errorf("auth/gsi: unexpected server step %d", step)
	}
}

// BuildCertResponse parses the server's kXGS_cert message and builds the client
// kXGC_cert response over the unsigned-DH path: it agrees the AES-128-CBC
// session key via Diffie-Hellman, signs the server's random tag with the proxy
// key (proof of possession), and returns the outer message carrying the client
// DH public value and the encrypted proxy chain.
func BuildCertResponse(serverMsg, proxyPEM []byte, proxyKey *rsa.PrivateKey) ([]byte, error) {
	pukBlob, ok := FindBucket(serverMsg, BucketPuk)
	if !ok {
		if _, signed := FindBucket(serverMsg, BucketCipher); signed {
			return nil, fmt.Errorf("auth/gsi: server chose signed-DH; only unsigned-DH is supported")
		}
		return nil, fmt.Errorf("auth/gsi: server DH public (kXRS_puk) missing")
	}
	peer, err := parsePeerBlob(pukBlob)
	if err != nil {
		return nil, err
	}

	const keyLen = 16 // aes-128-cbc
	key, err := generateKey(peer.params)
	if err != nil {
		return nil, err
	}
	sessionKey, err := key.sessionKey(peer.pub, keyLen)
	if err != nil {
		return nil, err
	}

	// Proof of possession: sign the server's random tag (in the server main
	// buffer) with the proxy key.
	var signedRTag []byte
	if xmain, ok := FindBucket(serverMsg, BucketMain); ok {
		if srtag, ok := FindBucket(xmain, BucketRTag); ok {
			signedRTag, err = signRTag(proxyKey, srtag)
			if err != nil {
				return nil, err
			}
		}
	}

	var newRTag [8]byte
	if _, err := rand.Read(newRTag[:]); err != nil {
		return nil, fmt.Errorf("auth/gsi: RNG failed: %w", err)
	}

	innerBuckets := []Bucket{{Type: BucketX509, Data: proxyPEM}}
	if signedRTag != nil {
		innerBuckets = append(innerBuckets, Bucket{Type: BucketSignedRTag, Data: signedRTag})
	}
	innerBuckets = append(innerBuckets, Bucket{Type: BucketRTag, Data: newRTag[:]})
	inner := EncodeMessage(StepClientCert, innerBuckets)

	enc, err := aesCBCEncrypt(sessionKey, inner)
	if err != nil {
		return nil, err
	}

	myBlob := encodePublicBlob(peer.paramsPEM, key.pub)
	return EncodeMessage(StepClientCert, []Bucket{
		{Type: BucketCryptoMod, Data: []byte("ssl")},
		{Type: BucketPuk, Data: myBlob},
		{Type: BucketCipherAlg, Data: []byte("aes-128-cbc")},
		{Type: BucketMDAlg, Data: []byte("sha256")},
		{Type: BucketMain, Data: enc},
	}), nil
}

// LoadProxy loads a GSI proxy from a combined PEM file (proxy certificate,
// private key, and issuer chain). The certificate blocks become ProxyPEM and
// the private key becomes ProxyKey.
func LoadProxy(path string) (*Auth, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("auth/gsi: could not read proxy %q: %w", path, err)
	}
	var certs []byte
	var keyDER *pem.Block
	rest := raw
	for {
		blk, r := pem.Decode(rest)
		if blk == nil {
			break
		}
		rest = r
		switch blk.Type {
		case "CERTIFICATE":
			certs = append(certs, pem.EncodeToMemory(blk)...)
		case "RSA PRIVATE KEY", "PRIVATE KEY":
			if keyDER == nil {
				keyDER = blk
			}
		}
	}
	if len(certs) == 0 || keyDER == nil {
		return nil, fmt.Errorf("auth/gsi: proxy %q missing certificate or key", path)
	}
	key, err := parseRSAKey(keyDER)
	if err != nil {
		return nil, err
	}
	return &Auth{ProxyPEM: certs, ProxyKey: key}, nil
}

func parseRSAKey(blk *pem.Block) (*rsa.PrivateKey, error) {
	if k, err := x509.ParsePKCS1PrivateKey(blk.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, fmt.Errorf("auth/gsi: could not parse proxy key: %w", err)
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("auth/gsi: proxy key is not RSA")
	}
	return rk, nil
}

// DefaultProxyPath resolves the proxy location from $X509_USER_PROXY, else
// /tmp/x509up_u<uid>.
func DefaultProxyPath() string {
	if p := os.Getenv("X509_USER_PROXY"); p != "" {
		return p
	}
	return "/tmp/x509up_u" + strconv.Itoa(os.Geteuid())
}

var (
	_ auth.Auther    = (*Auth)(nil)
	_ auth.Continuer = (*Auth)(nil)
)
