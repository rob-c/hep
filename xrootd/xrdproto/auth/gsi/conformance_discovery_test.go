// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for finding the ambient X.509 proxy and deciding whether it is
// worth offering.
//
// A proxy is short lived by design — twelve hours is normal — so the credential
// a client finds in /tmp is as likely to be yesterday's as today's. Offering an
// expired one produces a server-side authorization failure that names the user's
// identity and not the clock, which is the least useful place for that failure
// to appear. The decision belongs here, where the expiry is in hand.

package gsi

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/xrdproto/auth"
)

// writeProxyValid writes a proxy whose certificate is valid over the given
// window, and returns its path.
func writeProxyValid(t *testing.T, dir string, notBefore, notAfter time.Time) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("could not generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "proxy"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("could not create certificate: %v", err)
	}

	var raw []byte
	raw = append(raw, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	raw = append(raw, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})...)

	name := filepath.Join(dir, "x509up")
	if err := os.WriteFile(name, raw, 0600); err != nil {
		t.Fatalf("could not write proxy: %v", err)
	}
	return name
}

func TestConformance_ALiveProxyIsDiscovered(t *testing.T) {
	now := time.Now()
	t.Setenv("X509_USER_PROXY", writeProxyValid(t, t.TempDir(), now.Add(-time.Hour), now.Add(12*time.Hour)))

	a, err := Discover()
	if err != nil {
		t.Fatalf("a valid proxy was not discovered: %v", err)
	}
	if got := a.Provider(); got != "gsi" {
		t.Fatalf("the discovered provider is %q, want %q", got, "gsi")
	}
}

func TestConformance_AnExpiredProxyIsNotOffered(t *testing.T) {
	now := time.Now()
	path := writeProxyValid(t, t.TempDir(), now.Add(-24*time.Hour), now.Add(-time.Hour))
	t.Setenv("X509_USER_PROXY", path)

	a, err := Discover()
	if a != nil {
		t.Fatal("an expired proxy was offered to the server")
	}
	miss := auth.AsMissing(err)
	if miss == nil {
		t.Fatalf("the failure is %v, want a missing credential", err)
	}
	switch {
	case miss.Err == nil:
		t.Error("an expired proxy was reported as if no proxy was there at all")
	case !strings.Contains(miss.Err.Error(), "expired"):
		t.Errorf("the reason is %v, want the expiry", miss.Err)
	}
	if len(miss.Searched) != 1 || miss.Searched[0] != path {
		t.Errorf("the proxy that was rejected is reported as %q, want %q", miss.Searched, path)
	}
	if miss.Hint == "" {
		t.Error("a user whose proxy expired is not told what to run")
	}
}

func TestConformance_AProxyThatIsNotThereIsReportedAsAbsent(t *testing.T) {
	// Nothing to explain beyond where it looked: Err distinguishes "you have no
	// proxy" from "your proxy is no good", and only the second has a cause.
	t.Setenv("X509_USER_PROXY", filepath.Join(t.TempDir(), "nosuchproxy"))

	_, err := Discover()
	miss := auth.AsMissing(err)
	if miss == nil {
		t.Fatalf("the failure is %v, want a missing credential", err)
	}
	if miss.Err != nil {
		t.Errorf("an absent proxy was reported as a broken one: %v", miss.Err)
	}
}

func TestConformance_AProxyThatCannotBeReadIsReportedWithItsCause(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x509up")
	if err := os.WriteFile(path, []byte("this is not a proxy\n"), 0600); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}
	t.Setenv("X509_USER_PROXY", path)

	_, err := Discover()
	miss := auth.AsMissing(err)
	if miss == nil {
		t.Fatalf("the failure is %v, want a missing credential", err)
	}
	if miss.Err == nil {
		t.Fatal("a proxy that was there and unusable was reported as absent")
	}
}

func TestConformance_TheExpiryComesFromTheProxyCertificateItself(t *testing.T) {
	// The proxy certificate is the first block and the shortest-lived part of
	// the chain: reading the expiry off the issuer would report the user's
	// year-long certificate and never notice the proxy running out.
	want := time.Now().Add(7 * time.Hour).Truncate(time.Second)
	a, err := LoadProxy(writeProxyValid(t, t.TempDir(), time.Now().Add(-time.Hour), want))
	if err != nil {
		t.Fatalf("could not load the proxy: %v", err)
	}

	got, err := a.NotAfter()
	if err != nil {
		t.Fatalf("could not read the expiry: %v", err)
	}
	if !got.Equal(want.UTC()) {
		t.Fatalf("the expiry is %v, want %v", got, want.UTC())
	}
}

func TestConformance_AProxyWithNoCertificateHasNoExpiry(t *testing.T) {
	for _, tc := range []struct {
		name string
		pem  []byte
	}{
		{"nothing at all", nil},
		{"not PEM", []byte("-----BEGIN NOT A BLOCK-----\n")},
		{"a block that is not a certificate", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("junk")})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Auth{ProxyPEM: tc.pem}
			if _, err := a.NotAfter(); err == nil {
				t.Fatal("an expiry was read from something that is not a certificate")
			}
		})
	}
}
