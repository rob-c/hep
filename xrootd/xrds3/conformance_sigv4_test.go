// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the pieces of AWS Signature Version 4 that the S3 backend
// gets wrong silently.
//
// A signature is either right or the request is refused, so most mistakes are
// loud. The two below are not. Percent-encoding of the object key happens
// twice — once by net/http when the request goes out, once here when the
// canonical request is built — and the two must agree byte for byte, or every
// key containing a space, a plus, or a non-ASCII character fails with
// SignatureDoesNotMatch while the plain-ASCII keys next to them work. And the
// signing key is derived through four chained HMACs whose order carries the
// scope; getting it wrong produces a well-formed signature for the wrong
// credential scope, which S3 reports the same way.

package xrds3

import (
	"encoding/hex"
	"testing"
)

func TestConformance_TheObjectKeyIsEncodedTheWayS3Signs(t *testing.T) {
	// SigV4 encodes every byte outside the unreserved set, keeps '/' as a path
	// separator, and uses upper-case hex digits. url.PathEscape does not: it
	// leaves sub-delimiters alone and would produce a canonical request the
	// server does not reproduce.
	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{"nothing to encode", "/bucket/run3/file.root", "/bucket/run3/file.root"},
		{"the unreserved set survives", "/-_.~/AZaz09", "/-_.~/AZaz09"},
		{"a space", "/b/two words.root", "/b/two%20words.root"},
		{"a plus is not a space", "/b/a+b.root", "/b/a%2Bb.root"},
		{"sub-delimiters are encoded", "/b/a=1&c,d;e.root", "/b/a%3D1%26c%2Cd%3Be.root"},
		{"hex digits are upper case", "/b/\x0f\xff", "/b/%0F%FF"},
		{"utf-8 is encoded byte by byte", "/b/é.root", "/b/%C3%A9.root"},
		{"an empty path", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := uriEncodePath(tc.path); got != tc.want {
				t.Fatalf("uriEncodePath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestConformance_TheSigningKeyMatchesTheAWSVector(t *testing.T) {
	// The published derivation vector from the AWS SigV4 documentation. It
	// pins the order of the four HMACs and the "AWS4" prefix on the secret:
	// any permutation of those still yields 32 plausible-looking bytes.
	const (
		secret = "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"
		want   = "f4780e2d9f65fa895f9c67b32ce1baf0b0d8a43505a000a1a9e090d414db404d"
	)

	got := hex.EncodeToString(signingKey(secret, "20120215", "us-east-1", "iam"))
	if got != want {
		t.Fatalf("signingKey = %s, want %s", got, want)
	}
}

func TestConformance_TheSigningKeyIsScopedToEveryPartOfTheScope(t *testing.T) {
	// Each input has to reach the key. A derivation that dropped one — the
	// region is the usual casualty, since most deployments only ever use one —
	// would let a signature minted for one scope be replayed into another.
	base := signingKey("s3cr3t", "20260731", "us-east-1", "s3")

	for _, tc := range []struct {
		name                          string
		secret, date, region, service string
	}{
		{"the secret", "other", "20260731", "us-east-1", "s3"},
		{"the date", "s3cr3t", "20260801", "us-east-1", "s3"},
		{"the region", "s3cr3t", "20260731", "eu-west-2", "s3"},
		{"the service", "s3cr3t", "20260731", "us-east-1", "iam"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := signingKey(tc.secret, tc.date, tc.region, tc.service)
			if string(got) == string(base) {
				t.Fatalf("changing %s left the signing key unchanged", tc.name)
			}
		})
	}
}
