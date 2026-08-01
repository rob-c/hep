// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdhttp"
)

// startHTTPFileServer serves a single file over HEAD and GET and returns the
// server's host:port.
func startHTTPFileServer(t *testing.T, name string, data []byte) string {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != name {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if r.Method == http.MethodHead {
			return
		}
		w.Write(data)
	}))
	t.Cleanup(srv.Close)

	return strings.TrimPrefix(srv.URL, "http://")
}

// TestDialHTTPSchemes checks each HTTP-family scheme reaches a working
// filesystem view, including the dav and davs aliases that must be rewritten
// before they are dialled.
func TestDialHTTPSchemes(t *testing.T) {
	const name = "/some/file"
	data := []byte("hello from http")
	addr := startHTTPFileServer(t, name, data)

	for _, scheme := range []string{"http", "dav"} {
		t.Run(scheme, func(t *testing.T) {
			be, err := Dial(context.Background(), scheme+"://"+addr+name, "gopher")
			if err != nil {
				t.Fatalf("Dial(%s://): %v", scheme, err)
			}
			defer be.Close()

			// There is no XRootD session behind an HTTP endpoint, and callers
			// are told so rather than handed a stub.
			if be.Client() != nil {
				t.Fatal("Backend.Client() is non-nil for an HTTP endpoint")
			}
			hb, ok := be.(HTTPBackend)
			if !ok {
				t.Fatal("an HTTP backend does not implement HTTPBackend")
			}
			if hb.HTTPClient() == nil {
				t.Fatal("HTTPClient() = nil")
			}

			st, err := be.FS().Stat(context.Background(), name)
			if err != nil {
				t.Fatalf("Stat: %v", err)
			}
			if st.EntrySize != int64(len(data)) {
				t.Fatalf("got size %d, want %d", st.EntrySize, len(data))
			}
		})
	}
}

// TestDialHTTPRewritesDavSchemes checks davs maps to https rather than being
// dialled as-is; the TLS handshake against a cleartext server is what proves
// the rewrite happened.
func TestDialHTTPRewritesDavSchemes(t *testing.T) {
	addr := startHTTPFileServer(t, "/f", []byte("x"))

	// Unbounded: the failure below is the point of the test, and the default
	// retry schedule would ask for it five times before reporting it.
	be, err := DialHTTP("davs://"+addr+"/f", xrdhttp.Unbounded())
	if err != nil {
		t.Fatalf("DialHTTP(davs://): %v", err)
	}
	defer be.Close()

	// The server speaks cleartext HTTP, so an https request must fail at the
	// TLS layer. Succeeding would mean davs had been dialled as plain http.
	if _, err := be.FS().Stat(context.Background(), "/f"); err == nil {
		t.Fatal("davs:// was dialled without TLS")
	}
}

func TestDialHTTPUnsupportedScheme(t *testing.T) {
	if _, err := DialHTTP("root://example.org:1094//f"); !errors.Is(err, ErrUnsupportedScheme) {
		t.Fatalf("got %v, want ErrUnsupportedScheme", err)
	}
}
