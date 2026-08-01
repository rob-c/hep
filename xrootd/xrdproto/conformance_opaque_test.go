// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for adding opaque data to a path.
//
// The opaque part of an XRootD path is everything after the first '?', and it
// is where authorisation tokens, checksum algorithms and site-specific hints
// travel. A client that appends its own key has to join it to whatever is
// already there: a second '?' turns the token the caller was given into part of
// a filename, and a redirector answers "no such file" for a path the user can
// see is right.

package xrdproto

import "testing"

func TestConformance_OpaqueDataIsJoinedToWhatIsAlreadyThere(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		kv   string
		want string
	}{
		{"a path with no opaque data", "/a/b", "cks.type=md5", "/a/b?cks.type=md5"},
		{"a path with opaque data", "/a/b?authz=tok", "cks.type=md5", "/a/b?authz=tok&cks.type=md5"},
		{"a path ending in a question mark", "/a/b?", "cks.type=md5", "/a/b?cks.type=md5"},
		{"a path ending in an ampersand", "/a/b?authz=tok&", "cks.type=md5", "/a/b?authz=tok&cks.type=md5"},
		{"a path with several keys", "/a/b?x=1&y=2", "z=3", "/a/b?x=1&y=2&z=3"},
		{"nothing to add", "/a/b", "", "/a/b?"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := WithOpaque(tc.path, tc.kv); got != tc.want {
				t.Fatalf("WithOpaque(%q, %q) = %q, want %q", tc.path, tc.kv, got, tc.want)
			}
		})
	}
}

func TestConformance_AddedOpaqueDataIsReadBack(t *testing.T) {
	// Whatever WithOpaque produces has to be something Opaque and Path can take
	// apart again, or the client has written a path only the server can read.
	const (
		path = "/a/b"
		kv   = "cks.type=sha256"
	)
	full := WithOpaque(WithOpaque(path, "authz=tok"), kv)

	if got, _ := SplitPath(full); got != path {
		t.Fatalf("SplitPath(%q) = %q, want %q", full, got, path)
	}
	if got, want := Opaque(full), "authz=tok&"+kv; got != want {
		t.Fatalf("Opaque(%q) = %q, want %q", full, got, want)
	}
}
