// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for how the S3 client reads a response it did not ask for.
//
// S3 endpoints in the wild are not all Amazon's: MinIO, Ceph RGW and the
// gateways in front of them each have their own idea of which 2xx to send and
// which 4xx means "not there". The client has to sort those into three
// outcomes and only three — success, absence, and failure — because everything
// above it (the xrdhttp bridge, the copy engine) branches on exactly that.

package xrds3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/s3cred"
)

// statusS3 answers every request with a fixed status and body.
type statusS3 struct {
	status int
	body   []byte
	hdr    map[string]string

	methods []string
}

func (s *statusS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.methods = append(s.methods, r.Method)
	for k, v := range s.hdr {
		w.Header().Set(k, v)
	}
	w.WriteHeader(s.status)
	w.Write(s.body)
}

func statusClient(t *testing.T, srv *statusS3) *Client {
	t.Helper()
	h := httptest.NewServer(srv)
	t.Cleanup(h.Close)
	return New(h.URL, "bucket", s3cred.Credentials{AccessKey: "AK", Secret: "SK"})
}

func TestConformance_AnUnexpectedStatusNamesTheVerbAndTheKey(t *testing.T) {
	// The caller sees one error string and has to be able to tell which of the
	// four operations produced it, on which object, and what the endpoint
	// actually said.
	for _, tc := range []struct {
		name string
		call func(*Client) error
		verb string
	}{
		{
			name: "stat",
			verb: "HEAD",
			call: func(c *Client) error { _, _, err := c.Stat(context.Background(), "obj.bin"); return err },
		},
		{
			name: "read-all",
			verb: "GET",
			call: func(c *Client) error { _, err := c.ReadAll(context.Background(), "obj.bin"); return err },
		},
		{
			name: "read-at",
			verb: "GET",
			call: func(c *Client) error {
				_, err := c.ReadAt(context.Background(), make([]byte, 4), "obj.bin", 0)
				return err
			},
		},
		{
			name: "put",
			verb: "PUT",
			call: func(c *Client) error {
				return c.Put(context.Background(), "obj.bin", strings.NewReader("x"), 1)
			},
		},
		{
			name: "remove",
			verb: "DELETE",
			call: func(c *Client) error { return c.Remove(context.Background(), "obj.bin") },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call(statusClient(t, &statusS3{status: http.StatusInternalServerError}))
			if err == nil {
				t.Fatal("a 500 was reported as success")
			}
			for _, want := range []string{"xrds3:", tc.verb, "obj.bin", "500"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("the failure does not mention %q: %v", want, err)
				}
			}
		})
	}
}

func TestConformance_AMissingObjectIsAbsenceNotFailure(t *testing.T) {
	// Stat is how the layers above ask "is it there?", so a 404 has to come
	// back as a clean negative answer rather than an error they would have to
	// pattern-match on.
	c := statusClient(t, &statusS3{status: http.StatusNotFound})

	size, exists, err := c.Stat(context.Background(), "obj.bin")
	if err != nil {
		t.Fatalf("a missing object was reported as a failure: %v", err)
	}
	if exists {
		t.Fatal("a 404 was reported as an existing object")
	}
	if size != 0 {
		t.Fatalf("a missing object was reported with size %d", size)
	}
}

func TestConformance_DeletingSomethingAlreadyGoneSucceeds(t *testing.T) {
	// Remove is idempotent: the caller wants the object absent, and it is.
	// This is what makes a retried or resumed cleanup safe.
	for _, status := range []int{http.StatusOK, http.StatusNoContent, http.StatusNotFound} {
		c := statusClient(t, &statusS3{status: status})
		if err := c.Remove(context.Background(), "obj.bin"); err != nil {
			t.Fatalf("a delete answered with %d was reported as a failure: %v", status, err)
		}
	}
}

func TestConformance_EverySuccessfulStatusIsAccepted(t *testing.T) {
	// Endpoints disagree on which 2xx a PUT gets: S3 says 200, some gateways
	// say 201 or 204. All of them mean the object was stored.
	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent} {
		c := statusClient(t, &statusS3{status: status})
		if err := c.Put(context.Background(), "obj.bin", strings.NewReader("x"), 1); err != nil {
			t.Fatalf("a put answered with %d was reported as a failure: %v", status, err)
		}
	}
}

func TestConformance_AReadPastTheEndOfAnObjectIsEOF(t *testing.T) {
	// ReadAt sits under an io.ReaderAt, whose callers stop on io.EOF and
	// report anything else. A range the endpoint refuses, and a range it
	// answers only partly, are both ends of file.
	for _, tc := range []struct {
		name string
		srv  *statusS3
		want int
	}{
		{
			name: "the endpoint refuses the range",
			srv:  &statusS3{status: http.StatusRequestedRangeNotSatisfiable},
			want: 0,
		},
		{
			name: "the endpoint returns a short range",
			srv:  &statusS3{status: http.StatusPartialContent, body: []byte("ab")},
			want: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := statusClient(t, tc.srv)
			n, err := c.ReadAt(context.Background(), make([]byte, 8), "obj.bin", 0)
			if !errors.Is(err, io.EOF) {
				t.Fatalf("a short read reported %v, want io.EOF", err)
			}
			if n != tc.want {
				t.Fatalf("the short read returned %d bytes, want %d", n, tc.want)
			}
		})
	}
}

func TestConformance_AWholeObjectRangeIsAcceptedFromAnEndpointThatIgnoresIt(t *testing.T) {
	// Some gateways answer a Range request with the whole object and a plain
	// 200. That is still a usable answer as long as the bytes asked for are
	// there.
	srv := &statusS3{status: http.StatusOK, body: []byte("go-hep s3")}
	c := statusClient(t, srv)

	p := make([]byte, 5)
	n, err := c.ReadAt(context.Background(), p, "obj.bin", 0)
	if err != nil {
		t.Fatalf("a 200 answer to a range request failed: %v", err)
	}
	if n != len(p) || string(p) != "go-he" {
		t.Fatalf("the range read returned %d bytes %q", n, p)
	}
}

func TestConformance_AnEmptyReadTouchesTheNetworkNotAtAll(t *testing.T) {
	// io.ReaderAt callers pass a zero-length buffer often enough that a round
	// trip per call would be a real cost, and there is nothing to ask for.
	srv := &statusS3{status: http.StatusInternalServerError}
	c := statusClient(t, srv)

	n, err := c.ReadAt(context.Background(), nil, "obj.bin", 0)
	if err != nil || n != 0 {
		t.Fatalf("an empty read returned (%d, %v), want (0, nil)", n, err)
	}
	if len(srv.methods) != 0 {
		t.Fatalf("an empty read made %d requests: %v", len(srv.methods), srv.methods)
	}
}

func TestConformance_AnUnreachableEndpointNamesTheVerbItWasAttempting(t *testing.T) {
	// A transport failure carries no status, so the operation has to supply
	// the context itself — otherwise the caller gets a bare dial error with no
	// clue which of five calls produced it.
	h := httptest.NewServer(&statusS3{status: http.StatusOK})
	url := h.URL
	h.Close()

	c := New(url, "bucket", s3cred.Credentials{AccessKey: "AK", Secret: "SK"})
	ctx := context.Background()

	for _, tc := range []struct {
		verb string
		call func() error
	}{
		{"HEAD", func() error { _, _, err := c.Stat(ctx, "obj.bin"); return err }},
		{"GET", func() error { _, err := c.ReadAll(ctx, "obj.bin"); return err }},
		{"GET", func() error { _, err := c.ReadAt(ctx, make([]byte, 4), "obj.bin", 0); return err }},
		{"PUT", func() error { return c.Put(ctx, "obj.bin", bytes.NewReader([]byte("x")), 1) }},
		{"DELETE", func() error { return c.Remove(ctx, "obj.bin") }},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("a request to a closed endpoint reported success")
			}
			if !strings.Contains(err.Error(), tc.verb) {
				t.Fatalf("the failure does not name the operation: %v", err)
			}
		})
	}
}

func TestConformance_APutOfUnknownLengthIsStillSigned(t *testing.T) {
	// A negative size means "the caller does not know", which is why the
	// payload is signed as UNSIGNED-PAYLOAD: the signature cannot depend on
	// bytes that have not been read yet.
	var hash string
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hash = r.Header.Get("x-amz-content-sha256")
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer h.Close()

	c := New(h.URL, "bucket", s3cred.Credentials{AccessKey: "AK", Secret: "SK"})
	if err := c.Put(context.Background(), "obj.bin", strings.NewReader("go-hep"), -1); err != nil {
		t.Fatalf("could not put an object of unknown length: %v", err)
	}
	if hash != unsignedPayload {
		t.Fatalf("the payload was signed as %q, want %q", hash, unsignedPayload)
	}
}

func TestConformance_TheKeyIsPathStyleWhateverTheCallerWrites(t *testing.T) {
	// The bridge above builds keys from XRootD paths, which start with "/".
	// Path-style addressing puts the bucket first, so a doubled slash would
	// address a differently-named object on a real endpoint.
	c := New("https://s3.example.org/", "bucket", s3cred.Credentials{})

	const want = "https://s3.example.org/bucket/dir/obj.bin"
	for _, key := range []string{"dir/obj.bin", "/dir/obj.bin"} {
		if got := c.urlFor(key); got != want {
			t.Fatalf("key %q addresses %q, want %q", key, got, want)
		}
	}
}
