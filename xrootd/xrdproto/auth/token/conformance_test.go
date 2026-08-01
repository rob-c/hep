// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for bearer-token discovery.
//
// The WLCG Bearer Token Discovery specification exists so that a job does not
// have to know how its token was delivered: the pilot writes it wherever it
// can, and every client looks in the same places in the same order. A client
// that searches a different order finds a stale token in a location the pilot
// stopped using, and one that stops at the first location that *exists* rather
// than the first that holds a token fails on a site that leaves an empty file
// behind. Neither failure looks like a discovery bug — both look like the
// server rejecting a valid identity.

package token

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// tokenFile writes content to a file under dir and returns its path.
func tokenFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("could not write %q: %v", path, err)
	}
	return path
}

// clearTokenEnv unsets every variable discovery consults, so a test only sees
// what it sets itself.
func clearTokenEnv(t *testing.T) {
	t.Helper()

	for _, env := range []string{"BEARER_TOKEN", "BEARER_TOKEN_FILE", "XDG_RUNTIME_DIR"} {
		t.Setenv(env, "")
	}
}

// skipIfSystemToken skips when the last-resort location holds a token, since
// no test can remove it without touching a path outside its own directories.
func skipIfSystemToken(t *testing.T) {
	t.Helper()

	p := "/tmp/bt_u" + strconv.Itoa(os.Geteuid())
	if _, err := os.Stat(p); err == nil {
		t.Skipf("%s exists on this machine and discovery would find it", p)
	}
}

func TestConformance_DiscoveryFollowsTheWLCGOrder(t *testing.T) {
	// Every location holds a different token, and they are removed one at a
	// time from the most preferred down. A client searching in any other order
	// picks the wrong one at some step here.
	dir := t.TempDir()
	xdg := t.TempDir()
	uid := strconv.Itoa(os.Geteuid())

	file := tokenFile(t, dir, "token.jwt", "from-the-file")
	tokenFile(t, xdg, "bt_u"+uid, "from-the-runtime-dir")

	for _, tc := range []struct {
		name  string
		setup func()
		want  string
	}{
		{"the literal variable wins", func() {
			t.Setenv("BEARER_TOKEN", "from-the-variable")
			t.Setenv("BEARER_TOKEN_FILE", file)
			t.Setenv("XDG_RUNTIME_DIR", xdg)
		}, "from-the-variable"},
		{"then the named file", func() {
			t.Setenv("BEARER_TOKEN", "")
			t.Setenv("BEARER_TOKEN_FILE", file)
			t.Setenv("XDG_RUNTIME_DIR", xdg)
		}, "from-the-file"},
		{"then the runtime directory", func() {
			t.Setenv("BEARER_TOKEN", "")
			t.Setenv("BEARER_TOKEN_FILE", "")
			t.Setenv("XDG_RUNTIME_DIR", xdg)
		}, "from-the-runtime-dir"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearTokenEnv(t)
			tc.setup()

			got, err := Discover()
			if err != nil {
				t.Fatalf("could not discover a token: %v", err)
			}
			if got != tc.want {
				t.Fatalf("discovery found %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConformance_AnEmptyLocationIsNotAToken(t *testing.T) {
	// A pilot that failed part-way leaves an empty file behind. Treating it as
	// a token sends an empty credential and stops the search at the location
	// that had nothing, skipping the one that does.
	dir := t.TempDir()
	xdg := t.TempDir()
	uid := strconv.Itoa(os.Geteuid())

	empty := tokenFile(t, dir, "empty.jwt", "")
	tokenFile(t, xdg, "bt_u"+uid, "from-the-runtime-dir")

	clearTokenEnv(t)
	t.Setenv("BEARER_TOKEN", "   \n")
	t.Setenv("BEARER_TOKEN_FILE", empty)
	t.Setenv("XDG_RUNTIME_DIR", xdg)

	got, err := Discover()
	if err != nil {
		t.Fatalf("could not discover a token: %v", err)
	}
	if got != "from-the-runtime-dir" {
		t.Fatalf("discovery stopped at an empty location and found %q", got)
	}
}

func TestConformance_AMissingLocationIsSkippedNotFatal(t *testing.T) {
	// $BEARER_TOKEN_FILE routinely outlives the file it names; that is a reason
	// to look further, not to give up.
	xdg := t.TempDir()
	uid := strconv.Itoa(os.Geteuid())
	tokenFile(t, xdg, "bt_u"+uid, "from-the-runtime-dir")

	clearTokenEnv(t)
	t.Setenv("BEARER_TOKEN_FILE", filepath.Join(t.TempDir(), "absent.jwt"))
	t.Setenv("XDG_RUNTIME_DIR", xdg)

	got, err := Discover()
	if err != nil {
		t.Fatalf("could not discover a token: %v", err)
	}
	if got != "from-the-runtime-dir" {
		t.Fatalf("discovery found %q", got)
	}
}

func TestConformance_ATokenIsTrimmedOfWhatEditorsAdd(t *testing.T) {
	// A token file is written by a shell redirect as often as by a library, so
	// it arrives with a trailing newline — and a JWT with a newline appended is
	// not the same string, so the signature check fails at the server with no
	// hint that the bytes were ever different.
	dir := t.TempDir()

	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"a trailing newline", "the.jwt.token\n"},
		{"a CRLF", "the.jwt.token\r\n"},
		{"trailing NULs", "the.jwt.token\x00\x00"},
		{"trailing spaces and tabs", "the.jwt.token \t"},
		{"all of them", "the.jwt.token \t\r\n\x00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearTokenEnv(t)
			t.Setenv("BEARER_TOKEN_FILE", tokenFile(t, dir, "token.jwt", tc.raw))

			got, err := Discover()
			if err != nil {
				t.Fatalf("could not discover a token: %v", err)
			}
			if got != "the.jwt.token" {
				t.Fatalf("the token is %q, want it trimmed", got)
			}
		})
	}

	// The same trimming applies to the literal variable, which a shell can
	// just as easily set from a file.
	t.Run("the literal variable too", func(t *testing.T) {
		clearTokenEnv(t)
		t.Setenv("BEARER_TOKEN", "the.jwt.token\n")

		got, err := Discover()
		if err != nil {
			t.Fatalf("could not discover a token: %v", err)
		}
		if got != "the.jwt.token" {
			t.Fatalf("the token is %q, want it trimmed", got)
		}
	})
}

func TestConformance_ATokenIsNotTrimmedInTheMiddle(t *testing.T) {
	// Only the trailing bytes are noise. A JWT is three base64url segments
	// separated by dots and never contains whitespace, but a client that
	// trimmed or folded anything else would corrupt a token from a site using
	// a different format.
	clearTokenEnv(t)
	const raw = "  leading.space.matters"
	t.Setenv("BEARER_TOKEN", raw)

	got, err := Discover()
	if err != nil {
		t.Fatalf("could not discover a token: %v", err)
	}
	if got != raw {
		t.Fatalf("the token is %q, want %q untouched", got, raw)
	}
}

func TestConformance_NoTokenAnywhereIsAFailureNotAnEmptyToken(t *testing.T) {
	// An empty token would be sent as a credential and refused by the server
	// as bad authentication; the caller needs to know there was nothing to
	// send, so it can fall back to another mechanism.
	skipIfSystemToken(t)
	clearTokenEnv(t)
	t.Setenv("BEARER_TOKEN_FILE", filepath.Join(t.TempDir(), "absent.jwt"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	tok, err := Discover()
	if err == nil {
		t.Fatalf("discovery invented the token %q", tok)
	}
	if tok != "" {
		t.Fatalf("a failed discovery still returned %q", tok)
	}
	if !strings.Contains(err.Error(), "no bearer token") {
		t.Fatalf("the failure does not say what is missing: %v", err)
	}
}

func TestConformance_ADiscoveredTokenIsUsableAsACredential(t *testing.T) {
	// Discovery and the credential are separate steps, and the seam is where a
	// token gets dropped: the ztn payload is the 4-byte tag and then the token
	// exactly as found, with no separator and no terminator.
	clearTokenEnv(t)
	t.Setenv("BEARER_TOKEN", "the.jwt.token")

	tok, err := Discover()
	if err != nil {
		t.Fatalf("could not discover a token: %v", err)
	}

	a := &Auth{Token: tok}
	if got := a.Provider(); got != "ztn" {
		t.Fatalf("the provider calls itself %q, want %q", got, "ztn")
	}

	req, err := a.Request(nil)
	if err != nil {
		t.Fatalf("could not build a credential: %v", err)
	}
	if req.Type != Type {
		t.Fatalf("the request is typed %q, want %q", req.Type, Type)
	}
	if want := "ztn\x00the.jwt.token"; req.Credentials != want {
		t.Fatalf("the credential is %q, want %q", req.Credentials, want)
	}
}
