// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for how a provider says it has no credential.
//
// "not found" is the answer that sends a user to read the client's source. What
// they need is the list of places that were consulted — because the usual cause
// is that their credential is in a different one — and, when something was found
// and could not be used, which of the two situations they are in. An expired
// proxy and an absent proxy are fixed by different commands.

package auth_test

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdproto/auth"
)

func TestConformance_AMissingCredentialSaysWhereItLooked(t *testing.T) {
	miss := &auth.Missing{
		Provider: "ztn",
		What:     "bearer token",
		Searched: []string{"$BEARER_TOKEN", "/run/user/1000/bt_u1000", "/tmp/bt_u1000"},
		Hint:     "obtain a WLCG token",
	}
	got := miss.Error()
	for _, want := range []string{"auth/ztn", "bearer token", "$BEARER_TOKEN", "/tmp/bt_u1000"} {
		if !strings.Contains(got, want) {
			t.Errorf("the message does not mention %q: %v", want, got)
		}
	}
	if miss.Unwrap() != nil {
		t.Errorf("a credential that was never there unwraps to %v, want nothing", miss.Unwrap())
	}
}

func TestConformance_ACredentialThatWasFoundAndIsUnusableSaysSo(t *testing.T) {
	// The distinction is the whole point: this user has a proxy, and no amount
	// of looking in another directory will help them.
	cause := errors.New("the proxy expired at 2026-07-30T09:00:00Z")
	miss := &auth.Missing{
		Provider: "gsi",
		What:     "X.509 proxy",
		Searched: []string{"/tmp/x509up_u1000"},
		Err:      cause,
	}
	if !strings.Contains(miss.Error(), cause.Error()) {
		t.Errorf("the message does not say what was wrong with what it found: %v", miss)
	}
	if !errors.Is(miss, cause) {
		t.Errorf("the underlying failure is not reachable from %v", miss)
	}
}

func TestConformance_AMissingCredentialIsFoundThroughWrapping(t *testing.T) {
	// The client wraps this error several times on its way out — once per
	// protocol it tried — and still has to be able to pull the hint back out.
	miss := &auth.Missing{Provider: "gsi", What: "X.509 proxy", Hint: "voms-proxy-init"}
	wrapped := errors.Join(errors.New("could not authorize using gsi"), miss)

	got := auth.AsMissing(wrapped)
	if got != miss {
		t.Fatalf("AsMissing returned %v, want the wrapped Missing", got)
	}
	if auth.AsMissing(fs.ErrNotExist) != nil {
		t.Fatal("an unrelated error reported a missing credential")
	}
	if auth.AsMissing(nil) != nil {
		t.Fatal("no error at all reported a missing credential")
	}
}
