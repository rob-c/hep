// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdcred

import (
	"context"
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

	"go-hep.org/x/hep/xrootd"
)

// proxyFile writes a proxy that expires at expiry.
func proxyFile(t *testing.T, expiry time.Time) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("could not generate a key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "proxy"},
		NotBefore:    expiry.Add(-12 * time.Hour),
		NotAfter:     expiry,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("could not create a certificate: %v", err)
	}

	var raw []byte
	raw = append(raw, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	raw = append(raw, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})...)

	name := filepath.Join(t.TempDir(), "x509up")
	if err := os.WriteFile(name, raw, 0600); err != nil {
		t.Fatalf("could not write the proxy: %v", err)
	}
	return name
}

func TestConformance_AProxyTheUserNamesSaysWhenItExpires(t *testing.T) {
	// A proxy file is opaque, and the mistake it usually hides — this is
	// yesterday's proxy, renewed on a machine the job never ran on — is visible
	// only in the expiry. A prompt that accepts one silently has answered the
	// question the user could not answer with another one.
	expiry := time.Now().Add(11 * time.Hour).Truncate(time.Second)
	path := proxyFile(t, expiry)

	term, out := scripted(t, path)
	a, err := loadProxyFile(context.Background(), term, xrootd.CredentialRequest{Provider: "gsi"})
	if err != nil {
		t.Fatalf("a valid proxy was not accepted: %v", err)
	}
	if got := a.Provider(); got != "gsi" {
		t.Fatalf("the credential is for %q, want %q", got, "gsi")
	}
	if got, want := out.String(), expiry.Local().Format("2006-01-02 15:04:05"); !strings.Contains(got, want) {
		t.Fatalf("the terminal does not say the proxy expires at %s:\n%s", want, got)
	}
	if !strings.Contains(out.String(), path) {
		t.Fatalf("the terminal does not say which proxy was used:\n%s", out)
	}
}
