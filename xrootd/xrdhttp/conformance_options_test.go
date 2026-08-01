// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the options a client is built from.
//
// An option that fails has to fail at Dial, before a single request goes out.
// The alternative — recording the problem and carrying on — produces a client
// that looks configured and is not: it sends unauthenticated requests to a
// server that will refuse them, and the error the caller finally sees names the
// operation rather than the credential that was never attached.
//
// The TLS options compose, which is the other thing to pin: each one has to
// fill in the shared *tls.Config rather than replace it, or the last one
// written wins and quietly discards the rest.

package xrdhttp

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestConformance_ATokenThatCannotBeDiscoveredFailsAtDial(t *testing.T) {
	// The option exists so a job can say "use whatever token the pilot left me".
	// When there is no such token, the client must not be built: a request that
	// goes out without one is refused by the server as anonymous, and nothing in
	// that error says a token was expected.
	if p := "/tmp/bt_u" + strconv.Itoa(os.Geteuid()); fileExists(p) {
		t.Skipf("%s exists on this machine and discovery would find it", p)
	}
	for _, env := range []string{"BEARER_TOKEN", "BEARER_TOKEN_FILE", "XDG_RUNTIME_DIR"} {
		t.Setenv(env, "")
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	c, err := Dial("https://example.org/", WithDiscoveredBearerToken())
	if err == nil {
		t.Fatalf("a client was built with a bearer token that does not exist: %+v", c)
	}
	if !strings.Contains(err.Error(), "no bearer token found") {
		t.Fatalf("the failure does not say what is missing: %v", err)
	}
}

func TestConformance_ADiscoveredTokenReachesTheClient(t *testing.T) {
	// The positive control: with a token where the specification says to look,
	// the same option succeeds and the token is the one that was found.
	for _, env := range []string{"BEARER_TOKEN_FILE", "XDG_RUNTIME_DIR"} {
		t.Setenv(env, "")
	}
	t.Setenv("BEARER_TOKEN", "the.jwt.token")

	c, err := Dial("https://example.org/", WithDiscoveredBearerToken())
	if err != nil {
		t.Fatalf("could not build a client: %v", err)
	}
	if c.token != "the.jwt.token" {
		t.Fatalf("the client carries the token %q", c.token)
	}
}

func TestConformance_TheTLSOptionsCompose(t *testing.T) {
	// A proxy certificate and a private CA bundle are the normal WLCG
	// combination, and they arrive as two separate options. If either replaced
	// the config rather than adding to it, the client would fail the handshake
	// for a reason — no client certificate, or an unknown authority — that has
	// nothing to do with what the caller configured.
	cert := tls.Certificate{Certificate: [][]byte{{0x30, 0x00}}}
	pool := x509.NewCertPool()

	for _, tc := range []struct {
		name string
		opts []Option
	}{
		{"the certificate first", []Option{WithClientCertificate(cert), WithRootCAs(pool)}},
		{"the pool first", []Option{WithRootCAs(pool), WithClientCertificate(cert)}},
		{"onto a config the caller supplied", []Option{
			WithTLSConfig(&tls.Config{ServerName: "example.org"}),
			WithClientCertificate(cert),
			WithRootCAs(pool),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg config
			for _, opt := range tc.opts {
				opt(&cfg)
			}

			if cfg.tls == nil {
				t.Fatal("no TLS configuration was built")
			}
			if len(cfg.tls.Certificates) != 1 {
				t.Fatalf("the configuration holds %d certificates, want 1", len(cfg.tls.Certificates))
			}
			if cfg.tls.RootCAs != pool {
				t.Fatal("the certificate pool was dropped")
			}
		})
	}
}

func TestConformance_AClientCertificateSurvivesIntoTheTransport(t *testing.T) {
	// The end of the same path: whatever the options built has to be what the
	// HTTP transport dials with. A configuration assembled correctly and then
	// left behind is the same failure, one layer down.
	cert := tls.Certificate{Certificate: [][]byte{{0x30, 0x00}}}

	c, err := Dial("https://example.org/", WithClientCertificate(cert), WithInsecureTLS())
	if err != nil {
		t.Fatalf("could not build a client: %v", err)
	}

	tr, ok := c.http.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("the client dials through a %T, not an *http.Transport", c.http.Transport)
	}
	if tr.TLSClientConfig == nil {
		t.Fatal("the transport has no TLS configuration")
	}
	if len(tr.TLSClientConfig.Certificates) != 1 {
		t.Fatalf("the transport carries %d certificates, want 1", len(tr.TLSClientConfig.Certificates))
	}
	if !tr.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("the insecure option did not reach the transport")
	}
}

// fileExists reports whether path is there.
func fileExists(path string) bool {
	_, err := os.Stat(filepath.Clean(path))
	return err == nil
}
