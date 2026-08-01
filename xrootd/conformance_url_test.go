// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the URL, which is the first thing a client gets wrong.
//
// An XRootD URL is not a net/url URL, and the difference is the part that
// matters: "root://host//store/f.bin" carries a *double* slash, the first
// separating the authority from the path and the second belonging to the path
// itself. A parser that treats it as an ordinary URL sends kXR_open a path with
// one slash too few, and the server answers kXR_NotFound for a file that is
// plainly there. Everything below is a decision the parser makes on the caller's
// behalf, checked against what actually goes on the wire.

package xrootd

import (
	"strings"
	"testing"
)

func TestConformance_TheDoubleSlashIsWhereThePathBegins(t *testing.T) {
	// The whole reason this package parses URLs itself. In the XRootD idiom the
	// authority ends at the first slash and the path starts at the second, so
	// "//store/f.bin" is the absolute path "/store/f.bin" — not "//store".
	for _, tc := range []struct {
		name string
		want string
	}{
		{"root://example.org//store/f.bin", "/store/f.bin"},
		{"root://example.org/store/f.bin", "/store/f.bin"},
		{"root://example.org//", "/"},
		{"root://example.org/", "/"},
		{"root://example.org//a//b", "/a//b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, err := ParseURL(tc.name)
			if err != nil {
				t.Fatalf("could not parse %q: %v", tc.name, err)
			}
			if u.Path != tc.want {
				t.Fatalf("the path is %q, want %q", u.Path, tc.want)
			}
		})
	}
}

func TestConformance_AURLWithNoPathIsJustAServer(t *testing.T) {
	// "root://host" names a server to talk to, not a file to open. It has to
	// parse — that is how a client is pointed at a redirector — and it must not
	// invent a path, because an empty path and "/" are different requests.
	u, err := ParseURL("root://example.org:1094")
	if err != nil {
		t.Fatalf("could not parse a bare server URL: %v", err)
	}
	if u.Addr != "example.org:1094" {
		t.Fatalf("the address is %q", u.Addr)
	}
	if u.Path != "" {
		t.Fatalf("a bare server URL invented the path %q", u.Path)
	}
}

func TestConformance_TheSchemeIsCaseInsensitiveAndThePathIsNot(t *testing.T) {
	// Schemes are case-insensitive per RFC 3986, so "ROOT://" has to dispatch
	// the same way as "root://". A path does not: object stores are
	// case-sensitive and folding one silently addresses a different file.
	u, err := ParseURL("ROOT://Example.ORG//Store/File.ROOT")
	if err != nil {
		t.Fatalf("could not parse the URL: %v", err)
	}
	if u.Scheme != "root" {
		t.Fatalf("the scheme is %q, want it folded to %q", u.Scheme, "root")
	}
	if u.Path != "/Store/File.ROOT" {
		t.Fatalf("the path is %q, want it left alone", u.Path)
	}
}

func TestConformance_TheSchemeDecidesWhetherTLSIsMandatory(t *testing.T) {
	// This is a security decision, not a preference: a "roots://" URL that
	// parses as plain "root://" is a silent downgrade to cleartext, which is
	// exactly what the scheme exists to prevent.
	for _, tc := range []struct {
		url string
		tls bool
	}{
		{"root://example.org//f.bin", false},
		{"xroot://example.org//f.bin", false},
		{"roots://example.org//f.bin", true},
		{"xroots://example.org//f.bin", true},
		{"ROOTS://example.org//f.bin", true}, // folded first, then classified
		{"https://example.org/f.bin", false}, // TLS, but not *in-protocol* TLS
		{"http://example.org/f.bin", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			u, err := ParseURL(tc.url)
			if err != nil {
				t.Fatalf("could not parse %q: %v", tc.url, err)
			}
			if got := u.TLS(); got != tc.tls {
				t.Fatalf("%q reports TLS=%v, want %v", tc.url, got, tc.tls)
			}
		})
	}
}

func TestConformance_ALoginNameCanRideOnTheURL(t *testing.T) {
	// The user info is a login name for kXR_login, not HTTP credentials. A
	// password after the colon is not something this protocol can send, so it
	// is dropped rather than smuggled into the login name — a login as
	// "gopher:secret" is refused by the server with nothing explaining why.
	for _, tc := range []struct {
		url  string
		user string
		addr string
	}{
		{"root://gopher@example.org//f.bin", "gopher", "example.org"},
		{"root://gopher:secret@example.org//f.bin", "gopher", "example.org"},
		{"root://@example.org//f.bin", "", "example.org"},
		{"root://example.org//f.bin", "", "example.org"},
		{"root://gopher@example.org:1094//f.bin", "gopher", "example.org:1094"},
	} {
		t.Run(tc.url, func(t *testing.T) {
			u, err := ParseURL(tc.url)
			if err != nil {
				t.Fatalf("could not parse %q: %v", tc.url, err)
			}
			if u.User != tc.user {
				t.Fatalf("the login name is %q, want %q", u.User, tc.user)
			}
			if u.Addr != tc.addr {
				t.Fatalf("the address is %q, want %q", u.Addr, tc.addr)
			}
		})
	}
}

func TestConformance_AnIPv6LiteralKeepsItsBrackets(t *testing.T) {
	// The brackets are what separate the address from the port, so an address
	// that loses them cannot be dialled and one that gains a split at the wrong
	// colon is dialled somewhere else entirely.
	for _, tc := range []struct {
		url  string
		addr string
		path string
	}{
		{"root://[::1]//f.bin", "[::1]", "/f.bin"},
		{"root://[::1]:1094//f.bin", "[::1]:1094", "/f.bin"},
		{"root://[fe80::1%25eth0]:1094//f.bin", "[fe80::1%25eth0]:1094", "/f.bin"},
		{"root://gopher@[::1]:1094//f.bin", "[::1]:1094", "/f.bin"},
	} {
		t.Run(tc.url, func(t *testing.T) {
			u, err := ParseURL(tc.url)
			if err != nil {
				t.Fatalf("could not parse %q: %v", tc.url, err)
			}
			if u.Addr != tc.addr {
				t.Fatalf("the address is %q, want %q", u.Addr, tc.addr)
			}
			if u.Path != tc.path {
				t.Fatalf("the path is %q, want %q", u.Path, tc.path)
			}
		})
	}
}

func TestConformance_AnAuthorityThatCannotBeSplitIsRejected(t *testing.T) {
	// Better to fail at parse than to dial something that is not the host the
	// caller wrote. The message has to name the URL: this error reaches a user
	// who typed it, not a programmer reading a stack.
	for _, url := range []string{
		"root://example.org:1:2//f.bin",
		"root://[::1//f.bin",
	} {
		t.Run(url, func(t *testing.T) {
			u, err := ParseURL(url)
			if err == nil {
				t.Fatalf("%q parsed to %+v", url, u)
			}
			if !strings.Contains(err.Error(), url) {
				t.Fatalf("the failure does not quote the URL: %v", err)
			}
			if u != (URL{}) {
				t.Fatalf("a rejected URL still yielded %+v", u)
			}
		})
	}
}

func TestConformance_APathWithNoSchemeIsALocalFile(t *testing.T) {
	// This is how every command in this tree tells "copy from the server" from
	// "copy from disk": the absence of a scheme, not a guess at the shape.
	for _, tc := range []struct {
		name string
		path string
	}{
		{"/store/f.bin", "/store/f.bin"},
		{"f.bin", "f.bin"},
		{"./sub/f.bin", "./sub/f.bin"},
		{"", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, err := ParseURL(tc.name)
			if err != nil {
				t.Fatalf("could not parse %q: %v", tc.name, err)
			}
			if u.Scheme != "" || u.Addr != "" {
				t.Fatalf("a local path parsed as remote: %+v", u)
			}
			if u.Path != tc.path {
				t.Fatalf("the path is %q, want %q", u.Path, tc.path)
			}
		})
	}
}

func TestConformance_OpaqueDataStaysOnThePath(t *testing.T) {
	// The CGI is a credential attached to the path, and kXR_open receives it as
	// part of the same field. Splitting it off here would mean every caller had
	// to remember to put it back, and the one that forgets sends an
	// unauthenticated open that fails as a permission error.
	const url = "root://example.org//store/f.bin?authz=Bearer%20tok&scitag.flow=17"
	u, err := ParseURL(url)
	if err != nil {
		t.Fatalf("could not parse %q: %v", url, err)
	}
	if want := "/store/f.bin?authz=Bearer%20tok&scitag.flow=17"; u.Path != want {
		t.Fatalf("the path is %q, want %q", u.Path, want)
	}
}

func TestConformance_AQuestionMarkDoesNotMoveThePathBoundary(t *testing.T) {
	// The authority ends at the first slash whatever comes after it; a parser
	// that looks for the CGI first can cut the URL in the wrong place.
	u, err := ParseURL("root://example.org//a?b=//c")
	if err != nil {
		t.Fatalf("could not parse the URL: %v", err)
	}
	if u.Addr != "example.org" {
		t.Fatalf("the address is %q", u.Addr)
	}
	if want := "/a?b=//c"; u.Path != want {
		t.Fatalf("the path is %q, want %q", u.Path, want)
	}
}
