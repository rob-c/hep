// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the two questions a caller asks before reading something
// large: can I stream it, and can I read it in pieces. Fetch exists so a
// download does not cost the file's size in memory; Ranges exists because a
// server that ignores Range still answers every read correctly — just with
// the whole file prefix in front of it, a cost the caller wants to discover
// before choosing scattered reads over one download.

package xrdhttp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConformance_FetchStreamsTheBody(t *testing.T) {
	const content = "not held in memory all at once"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, content)
	}))
	defer srv.Close()

	// The token proves Fetch goes through the same credential path as every
	// other request: a stream that forgot the Authorization header would be a
	// download that works anonymously and fails for exactly the files that
	// needed protecting.
	c, err := Dial(srv.URL, Unbounded(), WithBearerToken("tok"), WithInsecureBearerToken())
	if err != nil {
		t.Fatalf("could not dial: %+v", err)
	}

	body, err := c.Fetch(context.Background(), "/data.bin")
	if err != nil {
		t.Fatalf("could not fetch: %+v", err)
	}
	defer body.Close()

	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("could not read the stream: %+v", err)
	}
	if string(got) != content {
		t.Fatalf("got %q, want %q", got, content)
	}
}

func TestConformance_FetchReportsTheStatus(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	c, err := Dial(srv.URL, Unbounded())
	if err != nil {
		t.Fatalf("could not dial: %+v", err)
	}
	if _, err := c.Fetch(context.Background(), "/missing.bin"); err == nil {
		t.Fatal("fetching a missing object reported success")
	}
}

func TestConformance_RangesTellsTheServersApart(t *testing.T) {
	for _, tc := range []struct {
		name string
		h    http.HandlerFunc
		want bool
	}{
		{
			// http.ServeContent implements ranges; this is what any real
			// storage endpoint looks like.
			name: "a server that honours ranges",
			h: func(w http.ResponseWriter, r *http.Request) {
				http.ServeContent(w, r, "f", (time.Time{}), strings.NewReader("0123456789"))
			},
			want: true,
		},
		{
			name: "a server that ignores them",
			h: func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, "0123456789")
			},
			want: false,
		},
		{
			// 200 with a Content-Range means the range was honoured after
			// all, whatever the status says.
			name: "a server that honours them under a 200",
			h: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Range", "bytes 0-0/10")
				_, _ = io.WriteString(w, "0")
			},
			want: true,
		},
		{
			// An empty object has no byte 0 to serve, but the server
			// understood the question — reading the object in pieces will
			// work the moment it has pieces.
			name: "an empty object on a range-capable server",
			h: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			},
			want: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.h)
			defer srv.Close()

			c, err := Dial(srv.URL, Unbounded())
			if err != nil {
				t.Fatalf("could not dial: %+v", err)
			}
			got, err := c.Ranges(context.Background(), "/f")
			if err != nil {
				t.Fatalf("could not probe: %+v", err)
			}
			if got != tc.want {
				t.Errorf("got ranges=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestConformance_RangesReportsARealFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c, err := Dial(srv.URL, Unbounded())
	if err != nil {
		t.Fatalf("could not dial: %+v", err)
	}
	if _, err := c.Ranges(context.Background(), "/f"); err == nil {
		t.Fatal("a 403 was reported as an answer about ranges")
	}
}
