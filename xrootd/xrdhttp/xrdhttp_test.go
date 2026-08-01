// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Dial applies Hardened, so a client built with no options retries a 503 or a
// refused connection five times before reporting it. That is right for a
// caller and wrong for a test: almost every test here means to observe one
// failure, and asking for it five times measures the backoff schedule instead
// of the behaviour under test — and takes about seven seconds to do it.
//
// So a test that is not about the schedule dials with Unbounded, and the
// schedule itself is pinned where it belongs, in conformance_retry_test.go.

package xrdhttp

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// memFS is a tiny in-memory HTTP file server supporting GET (with Range),
// HEAD, PUT and DELETE, enough to exercise the client.
type memFS struct {
	files map[string][]byte
}

func (m *memFS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodHead:
		b, ok := m.files[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		b, ok := m.files[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		http.ServeContent(w, r, r.URL.Path, time.Time{}, bytes.NewReader(b))
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		if m.files == nil {
			m.files = map[string][]byte{}
		}
		m.files[r.URL.Path] = body
		w.WriteHeader(http.StatusCreated)
	case http.MethodDelete:
		delete(m.files, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func TestClientRoundTrip(t *testing.T) {
	fs := &memFS{files: map[string][]byte{"/data/hello.txt": []byte("hello, xrootd over http")}}
	srv := httptest.NewServer(fs)
	defer srv.Close()

	c, err := Dial(srv.URL, Unbounded())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	ctx := context.Background()

	// Stat existing + missing.
	fi, err := c.Stat(ctx, "/data/hello.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !fi.Exists || fi.Size != 23 {
		t.Fatalf("Stat: %+v", fi)
	}
	if miss, err := c.Stat(ctx, "/data/nope.txt"); err != nil || miss.Exists {
		t.Fatalf("Stat missing: info=%+v err=%v", miss, err)
	}

	// ReadAll.
	got, err := c.ReadAll(ctx, "/data/hello.txt")
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "hello, xrootd over http" {
		t.Fatalf("ReadAll: %q", got)
	}

	// Ranged ReadAt.
	buf := make([]byte, 5)
	n, err := c.ReadAt(ctx, buf, "/data/hello.txt", 7)
	if err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if n != 5 || string(buf) != "xroot" {
		t.Fatalf("ReadAt: n=%d %q", n, buf)
	}

	// ReadAt near EOF returns io.EOF with the short bytes.
	tail := make([]byte, 100)
	n, err = c.ReadAt(ctx, tail, "/data/hello.txt", 20)
	if err != io.EOF {
		t.Fatalf("ReadAt tail err: got=%v want=EOF", err)
	}
	if string(tail[:n]) != "ttp" {
		t.Fatalf("ReadAt tail: %q", tail[:n])
	}

	// Create + read back + Remove.
	if err := c.Create(ctx, "/data/new.txt", strings.NewReader("brand new"), int64(len("brand new"))); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err = c.ReadAll(ctx, "/data/new.txt")
	if err != nil || string(got) != "brand new" {
		t.Fatalf("read back: %q err=%v", got, err)
	}
	if err := c.Remove(ctx, "/data/new.txt"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if fi, _ := c.Stat(ctx, "/data/new.txt"); fi.Exists {
		t.Fatal("file still exists after Remove")
	}
}

func TestDialRejectsNonHTTP(t *testing.T) {
	if _, err := Dial("s3://bucket/key", Unbounded()); err == nil {
		t.Fatal("expected error for non-http scheme")
	}
}
