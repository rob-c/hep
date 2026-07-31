// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the options that decide a client's security posture before
// it has said a word to anyone: which authentication providers it will offer,
// whether it will encrypt, and whose certificate it will accept. These
// decisions are made once, at construction, and every session inherits them.

package xrootd

import (
	"crypto/tls"
	"crypto/x509"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdproto/auth"
)

// fakeAuther is a security provider that only has to have a name: the
// registration path does not call Request.
type fakeAuther struct {
	provider string
	id       int
}

func (a fakeAuther) Provider() string { return a.provider }

func (a fakeAuther) Request(params []string) (*auth.Request, error) {
	return &auth.Request{Type: [4]byte{}, Credentials: a.provider}, nil
}

// newOptionClient builds the part of a Client the options touch, without
// dialling: the options run before any connection is made, and that is
// precisely the property under test.
func newOptionClient(t *testing.T, opts ...Option) *Client {
	t.Helper()
	client := &Client{auths: make(map[string]auth.Auther)}
	client.initSecurityProviders()
	for _, opt := range opts {
		if err := opt(client); err != nil {
			t.Fatalf("could not apply an option: %v", err)
		}
	}
	return client
}

// TestConformance_TheDefaultProvidersAreRegisteredUnderTheirProtocolNames
// checks the map the server's offered list is looked up in. The server names a
// provider ("&P=gsi,..."), and a provider registered under any other key is
// simply never found: the client reports "provider was not found" and falls
// back to a weaker identity, which reads as an authorisation failure rather
// than a wiring bug.
func TestConformance_TheDefaultProvidersAreRegisteredUnderTheirProtocolNames(t *testing.T) {
	client := newOptionClient(t)

	for _, provider := range defaultProviders {
		if provider == nil {
			continue // ambient discovery failed; not registered, by design.
		}
		got, ok := client.auths[provider.Provider()]
		if !ok {
			t.Errorf("provider %q is not registered", provider.Provider())
			continue
		}
		if got != provider {
			t.Errorf("%q is registered as %T, want %T", provider.Provider(), got, provider)
		}
	}

	// unix and host need no credentials, so they are always available and
	// their absence would leave a client with nothing at all to offer.
	for _, name := range []string{"unix", "host"} {
		if _, ok := client.auths[name]; !ok {
			t.Errorf("the credential-free provider %q is not registered", name)
		}
	}
}

// TestConformance_WithAuthAddsAndReplacesByProviderName pins the documented
// behaviour of registering a provider: a new name is added, and a name that is
// already there is replaced rather than duplicated or refused. A client that
// kept the default would silently ignore the credentials the caller supplied.
func TestConformance_WithAuthAddsAndReplacesByProviderName(t *testing.T) {
	client := newOptionClient(t)
	before := len(client.auths)

	custom := fakeAuther{provider: "custom", id: 1}
	if err := WithAuth(custom)(client); err != nil {
		t.Fatalf("WithAuth: %v", err)
	}
	if got := client.auths["custom"]; got != auth.Auther(custom) {
		t.Errorf("the custom provider is %v, want %v", got, custom)
	}
	if got, want := len(client.auths), before+1; got != want {
		t.Errorf("the client offers %d providers, want %d", got, want)
	}

	// A second registration under the same name replaces the first.
	replacement := fakeAuther{provider: "custom", id: 2}
	if err := WithAuth(replacement)(client); err != nil {
		t.Fatalf("WithAuth: %v", err)
	}
	if got := client.auths["custom"].(fakeAuther); got.id != 2 {
		t.Errorf("the registered provider is #%d, want the replacement #2", got.id)
	}
	if got, want := len(client.auths), before+1; got != want {
		t.Errorf("replacing a provider left %d providers, want %d", got, want)
	}

	// Overriding a default is the same operation, and is how a caller supplies
	// its own credentials for a provider the client already knows.
	if err := WithAuth(fakeAuther{provider: "unix", id: 3})(client); err != nil {
		t.Fatalf("WithAuth: %v", err)
	}
	if got := client.auths["unix"].(fakeAuther); got.id != 3 {
		t.Error("registering a provider did not override the default of the same name")
	}
}

// TestConformance_TLSIsRequestedOnlyWhenAsked keeps the two ways of asking for
// encryption apart from the one way of weakening it. WithTLS and
// WithInsecureTLS both turn the negotiation on; only the second one stops
// checking who answered.
func TestConformance_TLSIsRequestedOnlyWhenAsked(t *testing.T) {
	for _, tc := range []struct {
		name         string
		opts         []Option
		wantTLS      bool
		wantInsecure bool
	}{
		{name: "a plain client does not negotiate TLS"},
		{name: "WithTLS", opts: []Option{WithTLS()}, wantTLS: true},
		{name: "WithInsecureTLS", opts: []Option{WithInsecureTLS()}, wantTLS: true, wantInsecure: true},
		{
			name:    "a TLS config alone does not request TLS",
			opts:    []Option{WithTLSConfig(&tls.Config{MinVersion: tls.VersionTLS13})},
			wantTLS: false,
		},
		{
			name:         "the two compose in either order",
			opts:         []Option{WithInsecureTLS(), WithTLSConfig(&tls.Config{MinVersion: tls.VersionTLS13})},
			wantTLS:      true,
			wantInsecure: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newOptionClient(t, tc.opts...)
			if got := client.wantTLS; got != tc.wantTLS {
				t.Errorf("the client would negotiate TLS: %v, want %v", got, tc.wantTLS)
			}
			if got := client.insecureTLS; got != tc.wantInsecure {
				t.Errorf("the client would skip verification: %v, want %v", got, tc.wantInsecure)
			}
		})
	}
}

// TestConformance_TheEffectiveTLSConfigNamesTheServerAndIsACopy covers what a
// session actually dials with. Two things matter and neither is visible from
// the option: the server name has to be filled in, or verification checks a
// certificate against nothing; and the caller's config must not be mutated, or
// one insecure session downgrades every later one made from the same client.
func TestConformance_TheEffectiveTLSConfigNamesTheServerAndIsACopy(t *testing.T) {
	t.Run("a default config verifies against the dialled host", func(t *testing.T) {
		client := newOptionClient(t, WithTLS())
		cfg := client.tlsConfigFor("xrd.example.org")
		if got, want := cfg.ServerName, "xrd.example.org"; got != want {
			t.Errorf("the config verifies %q, want %q", got, want)
		}
		if cfg.InsecureSkipVerify {
			t.Error("a client that asked for TLS would skip verification")
		}
	})

	t.Run("an explicit server name is kept", func(t *testing.T) {
		client := newOptionClient(t, WithTLSConfig(&tls.Config{ServerName: "alias.example.org"}))
		if got, want := client.tlsConfigFor("xrd.example.org").ServerName, "alias.example.org"; got != want {
			t.Errorf("the config verifies %q, want the caller's %q", got, want)
		}
	})

	t.Run("the caller's config is not mutated", func(t *testing.T) {
		pool := x509.NewCertPool()
		orig := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13}
		client := newOptionClient(t, WithInsecureTLS(), WithTLSConfig(orig))

		cfg := client.tlsConfigFor("xrd.example.org")
		if cfg == orig {
			t.Fatal("the session dials with the caller's own config, not a copy")
		}
		if !cfg.InsecureSkipVerify {
			t.Error("WithInsecureTLS did not reach the effective config")
		}
		if orig.InsecureSkipVerify {
			t.Error("the caller's config was weakened in place")
		}
		if orig.ServerName != "" {
			t.Error("the caller's config was named in place")
		}
		// What the caller did set has to survive the copy.
		if cfg.MinVersion != tls.VersionTLS13 || cfg.RootCAs != pool {
			t.Errorf("the copy dropped the caller's settings: %+v", cfg)
		}
	})

	t.Run("each session gets its own config", func(t *testing.T) {
		client := newOptionClient(t, WithTLS())
		a := client.tlsConfigFor("a.example.org")
		b := client.tlsConfigFor("b.example.org")
		if a == b {
			t.Fatal("two sessions share one config")
		}
		if a.ServerName == b.ServerName {
			t.Errorf("both sessions verify %q; the second server name overwrote the first", a.ServerName)
		}
	})
}
