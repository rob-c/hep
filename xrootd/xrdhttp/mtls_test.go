// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdhttp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// certPair is a generated certificate plus its parsed form and TLS keypair.
type certPair struct {
	tlsCert tls.Certificate
	x509    *x509.Certificate
}

func mkCert(t *testing.T, cn string, ca *certPair, isCA bool, dns []string, ips []net.IP) certPair {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:     dns,
		IPAddresses:  ips,
	}
	if isCA {
		tmpl.IsCA = true
		tmpl.KeyUsage |= x509.KeyUsageCertSign
		tmpl.BasicConstraintsValid = true
	}
	parent, parentKey := tmpl, any(key)
	if ca != nil {
		parent, parentKey = ca.x509, ca.tlsCert.PrivateKey
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	crt, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	return certPair{
		tlsCert: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: crt},
		x509:    crt,
	}
}

// TestMutualTLS proves the client authenticates with an X.509 client
// certificate and that the server sees the expected subject — the xrdhttps+x509
// access path.
func TestMutualTLS(t *testing.T) {
	ca := mkCert(t, "Test CA", nil, true, nil, nil)
	server := mkCert(t, "localhost", &ca, false, []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})
	client := mkCert(t, "gopher-user", &ca, false, nil, nil)

	caPool := x509.NewCertPool()
	caPool.AddCert(ca.x509)

	var gotSubject string
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.TLS.PeerCertificates) > 0 {
			gotSubject = r.TLS.PeerCertificates[0].Subject.CommonName
		}
		w.Write([]byte("authenticated"))
	}))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{server.tlsCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
	}
	srv.StartTLS()
	defer srv.Close()

	c, err := Dial(srv.URL, Unbounded(),
		WithRootCAs(caPool),
		WithClientCertificate(client.tlsCert),
	)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	got, err := c.ReadAll(context.Background(), "/")
	if err != nil {
		t.Fatalf("ReadAll over mTLS: %v", err)
	}
	if string(got) != "authenticated" {
		t.Fatalf("body: %q", got)
	}
	if gotSubject != "gopher-user" {
		t.Fatalf("server saw client CN=%q, want %q", gotSubject, "gopher-user")
	}
}

// TestServerVerificationFailsWithoutCA confirms the client rejects a server
// whose certificate does not chain to a trusted CA.
func TestServerVerificationFailsWithoutCA(t *testing.T) {
	ca := mkCert(t, "Test CA", nil, true, nil, nil)
	server := mkCert(t, "localhost", &ca, false, []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{server.tlsCert}}
	srv.StartTLS()
	defer srv.Close()

	c, err := Dial(srv.URL, Unbounded()) // no RootCAs: system pool won't trust our test CA
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if _, err := c.ReadAll(context.Background(), "/"); err == nil {
		t.Fatal("expected TLS verification failure without trusted CA")
	}
}
