// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrds3

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/internal/s3cred"
)

func TestEmptyPayloadHashConstant(t *testing.T) {
	// Known answer: SHA-256 of the empty string.
	if got := sha256hex(nil); got != emptyPayloadHash {
		t.Fatalf("empty payload hash: got=%s want=%s", got, emptyPayloadHash)
	}
}

func TestSignHeaderShape(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://s3.example.org/bucket/key.txt", nil)
	sign(req, "AKIDEXAMPLE", "secretkey", "us-east-1", "s3", emptyPayloadHash,
		time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC))

	auth := req.Header.Get("Authorization")
	for _, want := range []string{
		"AWS4-HMAC-SHA256 ",
		"Credential=AKIDEXAMPLE/20260702/us-east-1/s3/aws4_request",
		"SignedHeaders=host;x-amz-content-sha256;x-amz-date",
		"Signature=",
	} {
		if !strings.Contains(auth, want) {
			t.Fatalf("Authorization %q missing %q", auth, want)
		}
	}
	if req.Header.Get("x-amz-date") != "20260702T120000Z" {
		t.Fatalf("x-amz-date=%q", req.Header.Get("x-amz-date"))
	}

	// Signing is deterministic for a fixed clock.
	req2, _ := http.NewRequest(http.MethodGet, "https://s3.example.org/bucket/key.txt", nil)
	sign(req2, "AKIDEXAMPLE", "secretkey", "us-east-1", "s3", emptyPayloadHash,
		time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC))
	if req.Header.Get("Authorization") != req2.Header.Get("Authorization") {
		t.Fatal("signature is not deterministic for a fixed clock")
	}
}

// fakeS3 stores objects in memory and requires a SigV4 Authorization header.
type fakeS3 struct{ objs map[string][]byte }

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if r.Header.Get("x-amz-content-sha256") == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	key := r.URL.Path
	switch r.Method {
	case http.MethodHead:
		b, ok := f.objs[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", itoa(len(b)))
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		b, ok := f.objs[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		http.ServeContent(w, r, key, time.Time{}, bytes.NewReader(b))
	case http.MethodPut:
		body := new(bytes.Buffer)
		body.ReadFrom(r.Body)
		f.objs[key] = body.Bytes()
		w.WriteHeader(http.StatusOK)
	case http.MethodDelete:
		delete(f.objs, key)
		w.WriteHeader(http.StatusNoContent)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func TestClientRoundTrip(t *testing.T) {
	fake := &fakeS3{objs: map[string][]byte{}}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	c := New(srv.URL, "bucket", s3cred.Credentials{AccessKey: "AK", Secret: "SK"})
	ctx := context.Background()

	if err := c.Put(ctx, "hello.txt", strings.NewReader("hello s3"), int64(len("hello s3"))); err != nil {
		t.Fatalf("Put: %v", err)
	}
	size, exists, err := c.Stat(ctx, "hello.txt")
	if err != nil || !exists || size != 8 {
		t.Fatalf("Stat: size=%d exists=%v err=%v", size, exists, err)
	}
	got, err := c.ReadAll(ctx, "hello.txt")
	if err != nil || string(got) != "hello s3" {
		t.Fatalf("ReadAll: %q err=%v", got, err)
	}
	buf := make([]byte, 2)
	n, err := c.ReadAt(ctx, buf, "hello.txt", 6)
	if err != nil || string(buf[:n]) != "s3" {
		t.Fatalf("ReadAt: %q err=%v", buf[:n], err)
	}
	if err := c.Remove(ctx, "hello.txt"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, exists, _ := c.Stat(ctx, "hello.txt"); exists {
		t.Fatal("object still present after Remove")
	}
}
