// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package token_test

import (
	"os"
	"path/filepath"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdproto/auth/token"
)

func TestProviderAndRequest(t *testing.T) {
	a := token.Auth{Token: "header.payload.sig"}
	if got, want := a.Provider(), "ztn"; got != want {
		t.Fatalf("provider: got=%q want=%q", got, want)
	}
	req, err := a.Request(nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if req.Type != token.Type {
		t.Fatalf("type: got=%v want=%v", req.Type, token.Type)
	}
	if want := "ztn\x00header.payload.sig"; req.Credentials != want {
		t.Fatalf("credentials: got=%q want=%q", req.Credentials, want)
	}
}

func TestEmptyTokenErrors(t *testing.T) {
	a := token.Auth{}
	if _, err := a.Request(nil); err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestDiscoverBearerToken(t *testing.T) {
	t.Setenv("BEARER_TOKEN", "  tok-from-env\n")
	t.Setenv("BEARER_TOKEN_FILE", "")
	got, err := token.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got != "tok-from-env" {
		t.Fatalf("discover env: got=%q want=%q", got, "tok-from-env")
	}
}

func TestDiscoverBearerTokenFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tok")
	if err := os.WriteFile(p, []byte("tok-from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEARER_TOKEN", "")
	t.Setenv("BEARER_TOKEN_FILE", p)
	got, err := token.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got != "tok-from-file" {
		t.Fatalf("discover file: got=%q want=%q", got, "tok-from-file")
	}
}
