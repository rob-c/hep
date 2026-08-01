// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for what a HEAD reply is turned into.
//
// Over HTTP a stat is a HEAD, and the modification time is whatever the server
// put in Last-Modified — a header that is optional, that some storage elements
// omit for objects they synthesise, and whose format is not the one Go's time
// package parses by default. Neither absence nor garbage is a reason to fail a
// stat: the size is what callers act on. But neither is a reason to invent a
// time either, because a zero ModTime is what "unknown" looks like and an
// incremental transfer that compares timestamps must see it as such.

package xrdhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConformance_AStatReadsTheModificationTimeWhenThereIsOne(t *testing.T) {
	for _, tc := range []struct {
		name     string
		modified string
		want     time.Time
	}{
		{"an RFC 1123 date", "Mon, 02 Jan 2006 15:04:05 GMT", time.Date(2006, time.January, 2, 15, 4, 5, 0, time.UTC)},
		{"no header at all", "", time.Time{}},
		{"a date the server made up", "yesterday afternoon", time.Time{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodHead {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				w.Header().Set("Content-Length", "6")
				if tc.modified != "" {
					w.Header().Set("Last-Modified", tc.modified)
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			c, err := Dial(srv.URL, Unbounded())
			if err != nil {
				t.Fatalf("could not build a client: %v", err)
			}

			fi, err := c.Stat(context.Background(), "/data.txt")
			if err != nil {
				t.Fatalf("could not stat the file: %v", err)
			}
			if !fi.Exists {
				t.Fatal("a file the server answered for is reported as absent")
			}
			if fi.Size != 6 {
				t.Fatalf("the file is %d bytes, want 6", fi.Size)
			}
			if !fi.ModTime.Equal(tc.want) {
				t.Fatalf("the file is dated %v, want %v", fi.ModTime, tc.want)
			}
		})
	}
}
