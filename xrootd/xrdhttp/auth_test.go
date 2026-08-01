// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestDialRefusesTokenOverCleartext(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
		opts []Option
		want bool // want an error
	}{
		{
			name: "token over http is refused",
			url:  "http://example.org/base",
			opts: []Option{WithBearerToken("tok")},
			want: true,
		},
		{
			name: "token over https is fine",
			url:  "https://example.org/base",
			opts: []Option{WithBearerToken("tok")},
		},
		{
			name: "the refusal can be overridden",
			url:  "http://example.org/base",
			opts: []Option{WithBearerToken("tok"), WithInsecureBearerToken()},
		},
		{
			name: "no token, no refusal",
			url:  "http://example.org/base",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Dial(tc.url, append([]Option{Unbounded()}, tc.opts...)...)
			if got := err != nil; got != tc.want {
				t.Fatalf("Dial(%q) error = %v, want an error: %v", tc.url, err, tc.want)
			}
		})
	}
}

// TestDialReportsFailedOptions checks a credential the caller asked for and did
// not get is an error, not a silently unauthenticated client.
func TestDialReportsFailedOptions(t *testing.T) {
	boom := errors.New("no credential here")
	_, err := Dial("https://example.org/", Unbounded(), func(c *config) { c.err = boom })
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want %v", err, boom)
	}
}

func TestWithDiscoveredBearerToken(t *testing.T) {
	t.Setenv("BEARER_TOKEN", "discovered-tok")

	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
	}))
	defer srv.Close()

	c, err := Dial(srv.URL, Unbounded(), WithDiscoveredBearerToken(), WithInsecureBearerToken())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if _, err := c.Stat(context.Background(), "/file"); err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if want := "Bearer discovered-tok"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestEveryRequestCarriesTheToken is the point of routing every request through
// do: a credential must not be present on some methods and forgotten on others.
func TestEveryRequestCarriesTheToken(t *testing.T) {
	var (
		mu   sync.Mutex
		seen = map[string]string{}
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.Method] = r.Header.Get("Authorization")
		mu.Unlock()
		switch r.Method {
		case "PROPFIND":
			w.WriteHeader(http.StatusMultiStatus)
			w.Write([]byte(`<multistatus xmlns="DAV:"></multistatus>`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	c, err := Dial(srv.URL, Unbounded(), WithBearerToken("tok"), WithInsecureBearerToken())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	ctx := context.Background()
	calls := []struct {
		method string
		run    func() error
	}{
		{"HEAD", func() error { _, err := c.Stat(ctx, "/f"); return err }},
		{"GET", func() error { _, err := c.ReadAll(ctx, "/f"); return err }},
		{"PUT", func() error { return c.Create(ctx, "/f", strings.NewReader("x"), 1) }},
		{"DELETE", func() error { return c.Remove(ctx, "/f") }},
		{"PROPFIND", func() error { _, err := c.Dirlist(ctx, "/d"); return err }},
		{"MKCOL", func() error { return c.mkcol(ctx, "/d") }},
		{"MOVE", func() error { return c.move(ctx, "/a", "/b") }},
		{"COPY", func() error {
			// The body carries no outcome; only the header matters here.
			err := c.Pull(ctx, "/f", "https://src.example.org/in", TPCOptions{})
			if errors.Is(err, ErrTPCNoOutcome) {
				return nil
			}
			return err
		}},
	}
	for _, call := range calls {
		if err := call.run(); err != nil {
			t.Fatalf("%s: %v", call.method, err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	for _, call := range calls {
		if got := seen[call.method]; got != "Bearer tok" {
			t.Fatalf("%s carried Authorization %q, want %q", call.method, got, "Bearer tok")
		}
	}
}
