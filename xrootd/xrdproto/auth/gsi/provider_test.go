// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

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
	"strconv"
	"testing"
	"time"
)

// writeProxy writes a combined proxy file (certificate then key, as
// grid-proxy-init produces) and returns its path.
func writeProxy(t *testing.T, dir string, pkcs8 bool) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("could not generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "proxy"},
		NotBefore:    time.Unix(0, 0),
		NotAfter:     time.Unix(1<<31-1, 0),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("could not create certificate: %v", err)
	}

	var raw []byte
	raw = append(raw, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	switch {
	case pkcs8:
		blk, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatalf("could not marshal key: %v", err)
		}
		raw = append(raw, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: blk})...)
	default:
		raw = append(raw, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})...)
	}

	name := filepath.Join(dir, "x509up")
	if err := os.WriteFile(name, raw, 0600); err != nil {
		t.Fatalf("could not write proxy: %v", err)
	}
	return name
}

func TestLoadProxy(t *testing.T) {
	for _, pkcs8 := range []bool{false, true} {
		name := "PKCS#1"
		if pkcs8 {
			name = "PKCS#8"
		}
		t.Run(name, func(t *testing.T) {
			a, err := LoadProxy(writeProxy(t, t.TempDir(), pkcs8))
			if err != nil {
				t.Fatalf("LoadProxy: %v", err)
			}
			if a.ProxyKey == nil {
				t.Fatal("the proxy key was not loaded")
			}
			if len(a.ProxyPEM) == 0 {
				t.Fatal("the proxy certificate chain was not loaded")
			}
			if got := a.Provider(); got != "gsi" {
				t.Fatalf("got provider %q, want %q", got, "gsi")
			}
		})
	}
}

func TestLoadProxyRejectsIncompleteFiles(t *testing.T) {
	dir := t.TempDir()

	// A certificate with no key cannot authenticate anything, and neither can
	// a key with no certificate.
	certOnly := filepath.Join(dir, "cert-only")
	if err := os.WriteFile(certOnly, []byte("-----BEGIN CERTIFICATE-----\nZm9v\n-----END CERTIFICATE-----\n"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, name := range []string{certOnly, filepath.Join(dir, "absent"), "/dev/null"} {
		if _, err := LoadProxy(name); err == nil {
			t.Fatalf("LoadProxy(%q) accepted an unusable proxy", name)
		}
	}
}

// TestDefaultProxyPath checks the discovery order a stock client uses.
func TestDefaultProxyPath(t *testing.T) {
	t.Setenv("X509_USER_PROXY", "/somewhere/else")
	if got, want := DefaultProxyPath(), "/somewhere/else"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	t.Setenv("X509_USER_PROXY", "")
	if got, want := DefaultProxyPath(), "/tmp/x509up_u"+strconv.Itoa(os.Geteuid()); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestAmbientProxyIsUsable exercises what the package's init does: a proxy in
// the conventional place yields a usable Auther without the caller wiring it.
func TestAmbientProxyIsUsable(t *testing.T) {
	t.Setenv("X509_USER_PROXY", writeProxy(t, t.TempDir(), false))

	a, err := LoadProxy(DefaultProxyPath())
	if err != nil {
		t.Fatalf("could not load the ambient proxy: %v", err)
	}
	req, err := a.Request([]string{"v:10400", "c:ssl", "g:0", "P:gsi"})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if req == nil {
		t.Fatal("Request returned no authentication request")
	}
}
