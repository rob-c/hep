// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/handshake"
	"go-hep.org/x/hep/xrootd/xrdproto/login"
	"go-hep.org/x/hep/xrootd/xrdproto/protocol"
)

// selfSignedCert generates an in-memory ECDSA certificate valid for
// localhost/127.0.0.1, for use by mock TLS servers.
func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("could not generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{Organization: []string{"go-hep test"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("could not create certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("could not marshal key: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("could not load key pair: %v", err)
	}
	return cert
}

// TestTLSUpgradeOnGotoTLS asserts that a kXR_gotoTLS protocol response makes
// the client upgrade the connection to TLS before sending kXR_login.
func TestTLSUpgradeOnGotoTLS(t *testing.T) {
	cert := selfSignedCert(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	loginInTLS := make(chan bool, 1)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Cleartext: handshake + protocol(gotoTLS).
		buf := make([]byte, handshake.RequestLength)
		if _, err := readFull(conn, buf); err != nil {
			return
		}
		writeBootstrapResponse(conn, xrdproto.StreamID{0, 0}, handshake.Response{ProtocolVersion: 0x310, ServerType: xrdproto.DataServer})

		hdr, _ := readBootstrapRequest(conn)
		const kXRgotoTLS = 0x40000000 // fits in int32; only kXR_haveTLS needs uint32 handling
		writeBootstrapResponse(conn, hdr.StreamID, protocol.Response{
			BinaryProtocolVersion: 0x310,
			Flags:                 protocol.IsServer | protocol.Flags(kXRgotoTLS),
		})

		// Upgrade the server side to TLS, then expect login inside TLS.
		tconn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
		if err := tconn.Handshake(); err != nil {
			loginInTLS <- false
			return
		}
		hdr, _ = readBootstrapRequest(tconn)
		loginInTLS <- (hdr.RequestID == login.RequestID)
		writeBootstrapResponse(tconn, hdr.StreamID, login.Response{})
		_ = tconn.SetReadDeadline(time.Now().Add(time.Second))
		drain := make([]byte, 256)
		for {
			if _, err := tconn.Read(drain); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := NewClient(ctx, ln.Addr().String(), "gopher", WithInsecureTLS())
	if err != nil {
		t.Fatalf("NewClient with TLS upgrade: %v", err)
	}
	defer client.Close()

	if ok := <-loginInTLS; !ok {
		t.Fatal("login was not received inside the TLS session")
	}
}
