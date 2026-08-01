// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the parts of the krb5 provider that do not need a KDC.
//
// Everything past the credential cache needs a live realm, but the steps
// before it are exactly where a client silently authenticates as nobody:
// finding the ticket cache the way the rest of the Kerberos world finds it,
// and refusing a handshake that named no service instead of asking the KDC for
// a ticket to "".

package krb5

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdproto/auth"
)

func TestConformance_TheTicketCacheIsFoundTheWayKerberosFindsIt(t *testing.T) {
	// KRB5CCNAME is how every other Kerberos tool is pointed at a cache, and
	// it is set with a type prefix as often as not. A client that takes
	// "FILE:/tmp/ccache" literally looks for a file with a colon in its name,
	// finds nothing, and falls back to unauthenticated.
	dir := t.TempDir()
	path := filepath.Join(dir, "ccache")

	for _, tc := range []struct {
		name string
		env  string
		want string
	}{
		{"a bare path", path, path},
		{"a FILE: type prefix", "FILE:" + path, path},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("KRB5CCNAME", tc.env)
			if got := cachePath(); got != tc.want {
				t.Fatalf("the cache was looked for at %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConformance_WithNoEnvironmentTheCacheIsTheConventionalOne(t *testing.T) {
	// Without KRB5CCNAME the cache is the one kinit writes by default, named
	// after the numeric uid — not the login name, which is not unique across
	// the NFS-mounted homes these clients run on.
	t.Setenv("KRB5CCNAME", "")

	usr, err := user.Current()
	if err != nil {
		t.Skipf("could not resolve the current user: %v", err)
	}

	want := filepath.Join(os.TempDir(), fmt.Sprintf("krb5cc_%s", usr.Uid))
	if got := cachePath(); got != want {
		t.Fatalf("the cache was looked for at %q, want %q", got, want)
	}
}

func TestConformance_AKerberosProviderNamesItselfOnTheWire(t *testing.T) {
	// The server advertises protocols by name in the security information and
	// the client picks by matching it; the four-byte Type is what goes into
	// the request header.
	a := WithClient(nil)
	if got := a.Provider(); got != "krb5" {
		t.Fatalf("the provider calls itself %q, want %q", got, "krb5")
	}
	if got, want := string(Type[:]), "krb5"; got != want {
		t.Fatalf("the request type is %q, want %q", got, want)
	}
	var _ auth.Auther = a
}

func TestConformance_AKerberosHandshakeWithNoServiceNameIsRefused(t *testing.T) {
	// The server names the service principal it wants a ticket for. With no
	// parameters there is nothing to ask the KDC for, and the client has to
	// say so rather than start a round trip it cannot finish.
	a := WithClient(nil)

	for _, params := range [][]string{nil, {}} {
		_, err := a.Request(params)
		if err == nil {
			t.Fatal("a handshake with no service name was accepted")
		}
		if !strings.Contains(err.Error(), "want at least 1 parameter") {
			t.Fatalf("the refusal does not say what was missing: %v", err)
		}
	}
}

func TestConformance_ACacheThatIsNotThereIsAFailureNotADefault(t *testing.T) {
	// A missing or unreadable ticket cache must not produce a usable-looking
	// Auth: the caller would then offer krb5 to the server and fail the
	// handshake instead of falling through to the next protocol.
	t.Setenv("KRB5CCNAME", filepath.Join(t.TempDir(), "absent"))

	a, err := WithCredCache()
	if err == nil {
		t.Fatal("a missing credential cache produced a provider")
	}
	if a != nil {
		t.Fatal("a failed provider was returned alongside its error")
	}
	if !strings.Contains(err.Error(), "auth/krb5") {
		t.Fatalf("the failure does not say which provider failed: %v", err)
	}
}
