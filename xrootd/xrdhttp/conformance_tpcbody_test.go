// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the shape of the third-party-copy marker stream.
//
// The body of an HTTP-TPC response is a line protocol written by whatever the
// far endpoint happens to be, and endpoints differ: some pad the stream with
// blank lines to keep the connection alive, some emit banner lines before the
// first marker, some write keys with no value. None of that is an error, and a
// parser that treats it as one turns a successful transfer into a failed copy.
// What is an error is a stream that cannot be read to its end — because the
// outcome is announced at the end, and a truncated stream means nobody knows
// whether the bytes arrived.

package xrdhttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// errReader fails partway through, the way a connection dropped mid-transfer
// does.
type errReader struct {
	head string
	err  error
}

func (r *errReader) Read(p []byte) (int, error) {
	if r.head != "" {
		n := copy(p, r.head)
		r.head = r.head[n:]
		return n, nil
	}
	return 0, r.err
}

func TestConformance_ANoisyMarkerStreamIsStillReadable(t *testing.T) {
	// Every line here is one a real endpoint has been seen to write and none
	// of them says anything about the outcome. The success at the end does.
	const body = "\n" + // keep-alive padding
		"   \n" + // padding that is not empty until it is trimmed
		"Stripe Index: 0\n" + // a marker field outside any marker block
		"Perf Marker\n" +
		"a line with no colon\n" + // a field that is not a field
		"Timestamp: not-a-number\n" + // a field whose value does not parse
		"Stripe Index: 0\n" +
		"Stripe Bytes Transferred: 1024\n" +
		"Total Stripe Count: 1\n" +
		"End\n" +
		"success: all done\n"

	var markers []TPCMarker
	if err := parseTPCBody(strings.NewReader(body), func(m TPCMarker) { markers = append(markers, m) }); err != nil {
		t.Fatalf("a well-formed transfer was reported as failed: %v", err)
	}
	if len(markers) != 1 {
		t.Fatalf("got %d progress markers, want 1", len(markers))
	}
	if got := markers[0].BytesTransferred; got != 1024 {
		t.Fatalf("the marker reports %d bytes, want 1024", got)
	}
	if !markers[0].Timestamp.IsZero() {
		t.Fatalf("an unparseable timestamp became %v, want it left alone", markers[0].Timestamp)
	}
}

func TestConformance_AMarkerStreamThatBreaksIsNotASuccess(t *testing.T) {
	// The outcome comes last, so a stream that stops early has not reported
	// one. Reporting nil here is how a transfer that died halfway becomes a
	// file the catalogue believes in.
	boom := errors.New("connection reset by peer")
	err := parseTPCBody(&errReader{head: "Perf Marker\nStripe Index: 0\n", err: boom}, nil)
	if err == nil {
		t.Fatal("a truncated marker stream was accepted as a completed copy")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("the failure is %v, want it to carry the read error", err)
	}
}

func TestConformance_AnEndpointThatDoesNotAnswerCOPYIsAFailedTPC(t *testing.T) {
	// The endpoint accepted the connection long enough to be dialled and is
	// gone by the time the COPY is sent. There is no body to read an outcome
	// from, so the outcome is the transport failure.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	c, err := Dial(url, Unbounded())
	if err != nil {
		t.Fatalf("could not build a client: %v", err)
	}

	err = c.Push(context.Background(), "/data.txt", "https://elsewhere.example.org/data.txt", TPCOptions{})
	if err == nil {
		t.Fatal("a COPY nobody answered reported success")
	}
	if !strings.Contains(err.Error(), "COPY") {
		t.Fatalf("the failure says %q, want it to name the request that failed", err)
	}
}

func TestConformance_ACopyThatEndsWithoutAnOutcomeIsNotADoneCopy(t *testing.T) {
	// A 2xx on the COPY means "accepted", and this endpoint says nothing more.
	// Accepted is not done.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "Perf Marker\nStripe Index: 0\nEnd\n")
	}))
	defer srv.Close()

	c, err := Dial(srv.URL, Unbounded())
	if err != nil {
		t.Fatalf("could not build a client: %v", err)
	}

	err = c.Pull(context.Background(), "/data.txt", "https://elsewhere.example.org/data.txt", TPCOptions{})
	if !errors.Is(err, ErrTPCNoOutcome) {
		t.Fatalf("the copy reported %v, want it to say no outcome was announced", err)
	}
}
