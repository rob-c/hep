// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrds3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/s3cred"
)

// TestConformance_CanonicalQueryIsOrderedAndEscaped pins the one part of a
// SigV4 signature the caller cannot see. The canonical query string is hashed,
// not sent: if the client orders or escapes it differently from the service,
// the request is rejected with a signature mismatch and nothing in the request
// says which byte disagreed.
//
// The rules (AWS SigV4, "Create a canonical request") are: parameters sorted by
// name, repeated parameters sorted by value, every name and value
// percent-encoded, joined with "&" — and an empty query is the empty string,
// not "?".
func TestConformance_CanonicalQueryIsOrderedAndEscaped(t *testing.T) {
	for _, tc := range []struct {
		name string
		q    url.Values
		want string
	}{
		{
			name: "no parameters",
			q:    url.Values{},
			want: "",
		},
		{
			name: "sorted by name, not by insertion",
			q:    url.Values{"zeta": {"1"}, "alpha": {"2"}, "mu": {"3"}},
			want: "alpha=2&mu=3&zeta=1",
		},
		{
			name: "repeated names sorted by value",
			q:    url.Values{"k": {"b", "a", "c"}},
			want: "k=a&k=b&k=c",
		},
		{
			name: "reserved characters are escaped",
			q:    url.Values{"prefix": {"a/b c+d"}},
			want: "prefix=a%2Fb+c%2Bd",
		},
		{
			name: "an empty value keeps its equals sign",
			q:    url.Values{"marker": {""}},
			want: "marker=",
		},
		{
			name: "the delimiters of a listing request",
			q:    url.Values{"list-type": {"2"}, "delimiter": {"/"}, "prefix": {"data/"}},
			want: "delimiter=%2F&list-type=2&prefix=data%2F",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := canonicalQuery(tc.q); got != tc.want {
				t.Errorf("canonical query is %q, want %q", got, tc.want)
			}
		})
	}
}

// TestConformance_TheRegionOptionReachesTheSignature checks that WithRegion is
// not decoration: the region is part of the credential scope, so signing with
// the default while talking to a bucket in another region fails
// authentication rather than being redirected.
func TestConformance_TheRegionOptionReachesTheSignature(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Length", "3")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	creds := s3cred.Credentials{AccessKey: "AKIAEXAMPLE", Secret: "secret"}

	for _, tc := range []struct {
		name   string
		opts   []Option
		wantIn string
	}{
		{name: "the default region", wantIn: "/us-east-1/s3/aws4_request"},
		{name: "an explicit region", opts: []Option{WithRegion("eu-west-2")}, wantIn: "/eu-west-2/s3/aws4_request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth = ""
			// WithHTTPClient is what makes the request reach the test server
			// at all: a client that ignored it would dial the real endpoint.
			opts := append([]Option{WithHTTPClient(srv.Client())}, tc.opts...)
			c := New(srv.URL, "bucket", creds, opts...)

			if _, _, err := c.Stat(context.Background(), "key"); err != nil {
				t.Fatalf("Stat: %v", err)
			}
			if !strings.Contains(auth, tc.wantIn) {
				t.Errorf("the credential scope is %q, want it to contain %q", auth, tc.wantIn)
			}
			if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") {
				t.Errorf("the Authorization header is %q, want a SigV4 header", auth)
			}
		})
	}
}
