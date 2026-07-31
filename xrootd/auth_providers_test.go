// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"testing"

	"go-hep.org/x/hep/xrootd/xrdproto/auth"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/gsi"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/sss"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/token"
)

func TestDefaultProvidersIncludeTokenAndSSS(t *testing.T) {
	// The provider set is krb5, gsi, ztn, sss, unix, host. The length is a
	// deterministic guard against the gsi/ztn/sss wiring silently regressing,
	// independent of whether ambient discovery made their Defaults non-nil.
	if got, want := len(defaultProviders), 6; got != want {
		t.Fatalf("defaultProviders has %d entries, want %d", got, want)
	}

	// When discovery produced a non-nil Default, assert it is present by
	// identity. (nil entries are skipped by initSecurityProviders, so their
	// membership cannot be checked reliably here.)
	contains := func(target auth.Auther) bool {
		for _, p := range defaultProviders {
			if p != nil && p == target {
				return true
			}
		}
		return false
	}
	if token.Default != nil && !contains(token.Default) {
		t.Fatal("token.Default is set but not in defaultProviders")
	}
	if sss.Default != nil && !contains(sss.Default) {
		t.Fatal("sss.Default is set but not in defaultProviders")
	}
	// An X.509 proxy sitting in the conventional place is what a stock client
	// authenticates with; a gsi implementation the chain never offers is
	// indistinguishable from no implementation at all.
	if gsi.Default != nil && !contains(gsi.Default) {
		t.Fatal("gsi.Default is set but not in defaultProviders")
	}

	// The provider names are deterministic regardless of discovery.
	if got := (&token.Auth{Token: "x"}).Provider(); got != "ztn" {
		t.Fatalf("token provider name: got=%q want=%q", got, "ztn")
	}
	if got := (&sss.Auth{}).Provider(); got != "sss" {
		t.Fatalf("sss provider name: got=%q want=%q", got, "sss")
	}
	if got := (&gsi.Auth{}).Provider(); got != "gsi" {
		t.Fatalf("gsi provider name: got=%q want=%q", got, "gsi")
	}
}
