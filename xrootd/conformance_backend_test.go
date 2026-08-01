// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the protocol-neutral entry point, which picks a transport
// from the URL scheme.
//
// Dial is what lets a job move from root:// to https:// by changing a
// configuration string, so the scheme is the only thing standing between a
// caller and the wrong transport. A URL it cannot parse must not fall through
// to a default — dialing native XRootD for an address the caller wrote as HTTPS
// is a connection to the right host on the wrong port, and the diagnosis lands
// on the storage element rather than on the typo.

package xrootd_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdhttp"
)

func TestConformance_AnAddressThatIsNotAURLSelectsNoTransport(t *testing.T) {
	// A host:port:port is not an address, on either entry point. Both parse
	// before they dial, so neither should reach the network.
	const bad = "root://example.org:1094:2094//store/file.root"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := xrootd.Dial(ctx, bad, "gopher"); err == nil {
		t.Fatal("a malformed address was dialed natively")
	}
	if _, err := xrootd.DialHTTP("https://example.org:443:8443/store/file.root"); err == nil {
		t.Fatal("a malformed address was dialed over HTTP")
	}
}

func TestConformance_ASchemeWithNoTransportIsRefusedByName(t *testing.T) {
	// The scheme is reported back because it is almost always a typo, and a
	// bare "unsupported scheme" leaves the caller re-reading their own config.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, tc := range []struct {
		name string
		dial func() error
	}{
		{"an unknown scheme", func() error {
			_, err := xrootd.Dial(ctx, "ftp://example.org/store/file.root", "gopher")
			return err
		}},
		{"a native scheme handed to the HTTP transport", func() error {
			_, err := xrootd.DialHTTP("root://example.org//store/file.root")
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.dial()
			if !errors.Is(err, xrootd.ErrUnsupportedScheme) {
				t.Fatalf("the failure is %v, want it to be an unsupported scheme", err)
			}
		})
	}
}

func TestConformance_AnHTTPBackendCarriesItsOptionsFailures(t *testing.T) {
	// The options are what configure credentials, and one of them refuses to
	// send a bearer token in the clear. Dropping that refusal on the way
	// through DialHTTP would put the token on the wire anyway.
	_, err := xrootd.DialHTTP("http://example.org/store/file.root", xrdhttp.WithBearerToken("secret"))
	if err == nil {
		t.Fatal("a bearer token was accepted for a cleartext endpoint")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("the failure quotes the token: %v", err)
	}

	// The control: the same option on the transport that can carry it.
	if _, err := xrootd.DialHTTP("https://example.org/store/file.root", xrdhttp.WithBearerToken("secret")); err != nil {
		t.Fatalf("a bearer token was refused for an HTTPS endpoint: %v", err)
	}
}

func TestConformance_ADavAddressIsAnHTTPBackend(t *testing.T) {
	// dav:// and davs:// are spellings of http:// and https://, and the
	// rewrite happens before the client is built. A backend that kept the
	// dav scheme would fail at the first request, having already reported a
	// successful connection.
	for _, addr := range []string{
		"dav://example.org/store/file.root",
		"davs://example.org/store/file.root",
	} {
		backend, err := xrootd.DialHTTP(addr)
		if err != nil {
			t.Fatalf("could not dial %q: %v", addr, err)
		}
		hb, ok := backend.(xrootd.HTTPBackend)
		if !ok {
			t.Fatalf("%q produced a %T, not an HTTP backend", addr, backend)
		}
		if hb.HTTPClient() == nil {
			t.Fatalf("%q produced an HTTP backend with no client", addr)
		}
		if err := backend.Close(); err != nil {
			t.Fatalf("could not close the backend for %q: %v", addr, err)
		}
	}
}
