// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for retrying a request that the network, rather than the server,
// refused to complete.
//
// The distinction is the whole of it. A 404 is an answer and retrying it is
// three round trips to be told the same thing; a 503 from a gateway whose
// backend is draining is not an answer at all. And a request may only be
// resent when repeating it cannot do something the caller asked for once —
// which rules out a third-party copy whose first attempt may still be running,
// and any request whose body has already been read off a stream.

package xrdhttp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// countingServer answers with the given statuses in order, repeating the last
// one once they run out, and counts the requests it received.
func countingServer(t *testing.T, hdr http.Header, codes ...int) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(n.Add(1)) - 1
		code := codes[min(i, len(codes)-1)]
		for k, vs := range hdr {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(code)
		if code == http.StatusOK {
			w.Write([]byte("go-hep"))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func TestConformance_ARetryableStatusIsSentAgain(t *testing.T) {
	srv, n := countingServer(t, nil, http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusOK)

	c, err := Dial(srv.URL, WithRetry(3))
	if err != nil {
		t.Fatalf("could not build a client: %v", err)
	}

	got, err := c.ReadAll(context.Background(), "/a.txt")
	if err != nil {
		t.Fatalf("a request that eventually succeeded was reported as failed: %v", err)
	}
	if want := "go-hep"; string(got) != want {
		t.Fatalf("the file holds %q, want %q", got, want)
	}
	if got, want := n.Load(), int64(3); got != want {
		t.Fatalf("the server saw %d requests, want %d", got, want)
	}
}

func TestConformance_AnAnswerIsNotRetried(t *testing.T) {
	// A 404 and a 403 are the server saying what it knows. Retrying them costs
	// round trips and delays an error the caller could have acted on at once.
	for _, code := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusRequestedRangeNotSatisfiable} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			srv, n := countingServer(t, nil, code)

			c, err := Dial(srv.URL, WithRetry(5))
			if err != nil {
				t.Fatalf("could not build a client: %v", err)
			}
			if _, err := c.ReadAll(context.Background(), "/a.txt"); err == nil {
				t.Fatal("a refused request succeeded")
			}
			if got, want := n.Load(), int64(1); got != want {
				t.Fatalf("the server saw %d requests, want %d", got, want)
			}
		})
	}
}

func TestConformance_TheLastAnswerIsWhatTheCallerIsTold(t *testing.T) {
	// Every attempt failed the same way. The caller gets the server's own
	// status, not a retry-shaped error that hides it: a 503 that persists is
	// still worth distinguishing from a 404.
	srv, n := countingServer(t, nil, http.StatusServiceUnavailable)

	c, err := Dial(srv.URL, WithRetry(2))
	if err != nil {
		t.Fatalf("could not build a client: %v", err)
	}

	_, err = c.ReadAll(context.Background(), "/a.txt")
	if err == nil {
		t.Fatal("a request refused by every attempt succeeded")
	}
	var serr *StatusError
	if !errors.As(err, &serr) {
		t.Fatalf("the failure is a %T, want a *StatusError", err)
	}
	if got, want := serr.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("the failure is coded %d, want %d", got, want)
	}
	if got, want := n.Load(), int64(2); got != want {
		t.Fatalf("the server saw %d requests, want %d", got, want)
	}
}

func TestConformance_ARequestWithABodyItCannotReplayIsNotRetried(t *testing.T) {
	// Create uploads from whatever reader the caller owns. When that reader is
	// a pipe, a socket, or anything else that cannot be wound back, there is no
	// second copy of the body: resending the request would upload whatever was
	// left of it — a truncated file written over a good one.
	//
	// A buffer is different, and net/http knows it: an upload from bytes,
	// strings or a file is rewindable and is retried like any other request.
	srv, n := countingServer(t, nil, http.StatusServiceUnavailable)

	c, err := Dial(srv.URL, WithRetry(3))
	if err != nil {
		t.Fatalf("could not build a client: %v", err)
	}

	body := io.LimitReader(bytes.NewReader([]byte("go-hep")), 6) // not rewindable
	if err := c.Create(context.Background(), "/a.txt", body, 6); err == nil {
		t.Fatal("an upload the server refused succeeded")
	}
	if got, want := n.Load(), int64(1); got != want {
		t.Fatalf("the server saw %d requests, want %d", got, want)
	}

	n.Store(0)
	if err := c.Create(context.Background(), "/a.txt", bytes.NewReader([]byte("go-hep")), 6); err == nil {
		t.Fatal("an upload the server refused succeeded")
	}
	if got, want := n.Load(), int64(3); got != want {
		t.Fatalf("a rewindable upload was sent %d times, want %d", got, want)
	}
}

func TestConformance_AThirdPartyCopyIsNeverRetried(t *testing.T) {
	// A COPY hands the transfer to the server, which may still be running it
	// when the answer is lost. A second one has two servers writing the same
	// file, and the loser's bytes are what the caller is left holding.
	req, err := http.NewRequest("COPY", "https://example.com/a.txt", nil)
	if err != nil {
		t.Fatalf("could not build a request: %v", err)
	}
	if replayable(req) {
		t.Fatal("a third-party copy is treated as safe to repeat")
	}

	for _, method := range []string{http.MethodPost, "PATCH", "MKCOL"} {
		req, err := http.NewRequest(method, "https://example.com/a.txt", nil)
		if err != nil {
			t.Fatalf("could not build a request: %v", err)
		}
		if replayable(req) {
			t.Fatalf("a %s is treated as safe to repeat", method)
		}
	}

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodDelete, "PROPFIND"} {
		req, err := http.NewRequest(method, "https://example.com/a.txt", nil)
		if err != nil {
			t.Fatalf("could not build a request: %v", err)
		}
		if !replayable(req) {
			t.Fatalf("a %s is treated as unsafe to repeat", method)
		}
	}
}

func TestConformance_ATransportFailureIsRetried(t *testing.T) {
	// The connection died before a status line arrived. There is nothing to
	// map onto an error the caller could act on, and it is the case a retry
	// exists for.
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			// Hijack and close: the client sees the connection go away with no
			// response on it, which no status code can express.
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("could not hijack the connection: %v", err)
				return
			}
			conn.Close()
			return
		}
		w.Write([]byte("go-hep"))
	}))
	defer srv.Close()

	c, err := Dial(srv.URL, WithRetry(3))
	if err != nil {
		t.Fatalf("could not build a client: %v", err)
	}

	got, err := c.ReadAll(context.Background(), "/a.txt")
	if err != nil {
		t.Fatalf("a request that eventually succeeded was reported as failed: %v", err)
	}
	if want := "go-hep"; string(got) != want {
		t.Fatalf("the file holds %q, want %q", got, want)
	}
	if got, want := n.Load(), int64(2); got != want {
		t.Fatalf("the server saw %d requests, want %d", got, want)
	}
}

func TestConformance_ARetryGivesUpWhenTheCallerDoes(t *testing.T) {
	// A caller that has stopped waiting is not helped by more attempts, and a
	// retry loop that ignored its context would keep a dead endpoint busy long
	// after the request it was for had been abandoned.
	srv, _ := countingServer(t, nil, http.StatusServiceUnavailable)

	c, err := Dial(srv.URL, WithRetry(100))
	if err != nil {
		t.Fatalf("could not build a client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := c.ReadAll(ctx, "/a.txt"); err == nil {
		t.Fatal("a request refused by every attempt succeeded")
	}
	if got := time.Since(start); got > 30*time.Second {
		t.Fatalf("the attempts took %v after the caller gave up", got)
	}
}

func TestConformance_TheDefaultIsASingleAttempt(t *testing.T) {
	// Retrying decides how long a caller's program is willing to wait, and a
	// library that decided that for every program linking it would be deciding
	// something that is not its to decide.
	srv, n := countingServer(t, nil, http.StatusServiceUnavailable)

	c, err := Dial(srv.URL)
	if err != nil {
		t.Fatalf("could not build a client: %v", err)
	}
	if _, err := c.ReadAll(context.Background(), "/a.txt"); err == nil {
		t.Fatal("a refused request succeeded")
	}
	if got, want := n.Load(), int64(1); got != want {
		t.Fatalf("the server saw %d requests, want %d", got, want)
	}
}

func TestConformance_ARetryCountBelowOneIsRefused(t *testing.T) {
	// Zero attempts is a request that is never sent. Reading it as "the
	// default" would leave a caller who miscounted with a client that quietly
	// does not retry.
	if _, err := Dial("https://example.com", WithRetry(0)); err == nil {
		t.Fatal("a client that sends nothing was built")
	}
}

func TestConformance_RetryAfterIsHonoured(t *testing.T) {
	// A rate limiter that names its own delay knows something the backoff does
	// not, and a client that ignores it comes back too early and is refused
	// again.
	for _, tc := range []struct {
		name string
		hdr  string
		want time.Duration
		ok   bool
	}{
		{"no header at all", "", 0, false},
		{"a whole number of seconds", "12", 12 * time.Second, true},
		{"no delay", "0", 0, true},
		{"a negative delay", "-5", 0, false},
		{"something that is not a delay", "soon", 0, false},
		{"a date in the past", "Mon, 02 Jan 2006 15:04:05 GMT", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{Header: make(http.Header)}
			if tc.hdr != "" {
				resp.Header.Set("Retry-After", tc.hdr)
			}
			got, ok := retryAfter(resp)
			if ok != tc.ok {
				t.Fatalf("Retry-After %q was read as (%v, %v), want ok=%v", tc.hdr, got, ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Fatalf("Retry-After %q is %v, want %v", tc.hdr, got, tc.want)
			}
		})
	}

	// A gateway asking for an hour is asking for longer than any caller meant
	// to wait, so the header is advice and not an instruction.
	resp := &http.Response{Header: http.Header{"Retry-After": []string{"3600"}}}
	if got := retryDelay(0, resp); got > retryAfterMax {
		t.Fatalf("a Retry-After of an hour produced a pause of %v, want no more than %v", got, retryAfterMax)
	}

	// And it is preferred to the backoff when it is short enough to honour.
	resp.Header.Set("Retry-After", "0")
	if got := retryDelay(4, resp); got != 0 {
		t.Fatalf("a Retry-After of no delay produced a pause of %v, want none", got)
	}

	// Without one, the backoff grows and is capped: a server that says nothing
	// about when to come back gets an increasing pause, not an unbounded one.
	const jitterMax = 1 + retryBackoffJitter
	for n := range 10 {
		got := retryDelay(n, nil)
		if got < 0 {
			t.Fatalf("backoff %d is %v, want a non-negative pause", n, got)
		}
		if max := time.Duration(jitterMax * float64(retryBackoffMax)); got > max {
			t.Fatalf("backoff %d is %v, want no more than %v", n, got, max)
		}
	}
	if got, min := retryDelay(9, nil), time.Duration((1-retryBackoffJitter)*float64(retryBackoffMax)); got < min {
		t.Fatalf("the tenth backoff is %v, want at least %v", got, min)
	}
}

func TestConformance_RetryAfterPacesTheAttempts(t *testing.T) {
	srv, n := countingServer(t,
		http.Header{"Retry-After": []string{"0"}},
		http.StatusTooManyRequests, http.StatusOK,
	)

	c, err := Dial(srv.URL, WithRetry(2))
	if err != nil {
		t.Fatalf("could not build a client: %v", err)
	}

	start := time.Now()
	if _, err := c.ReadAll(context.Background(), "/a.txt"); err != nil {
		t.Fatalf("a request that eventually succeeded was reported as failed: %v", err)
	}
	if got, want := n.Load(), int64(2); got != want {
		t.Fatalf("the server saw %d requests, want %d", got, want)
	}
	// The server said to come back at once, so the backoff must not have been
	// applied on top of it.
	if got := time.Since(start); got > retryBackoffBase {
		t.Fatalf("the retry took %v, want the delay the server named", got)
	}
}

func TestConformance_ARetriedListingStillWorks(t *testing.T) {
	// PROPFIND carries a body, and a retry that did not rewind it would send a
	// second request with an empty one — which a server answers with 400, and
	// the caller reads as a directory that cannot be listed.
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 1024)
		nb, _ := r.Body.Read(body)
		if nb == 0 {
			t.Errorf("attempt %d arrived with an empty body", n.Load()+1)
		}
		if n.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusMultiStatus)
		w.Write([]byte(`<?xml version="1.0"?><D:multistatus xmlns:D="DAV:">` +
			`<D:response><D:href>/dir/a.txt</D:href><D:propstat><D:prop>` +
			`<D:getcontentlength>6</D:getcontentlength></D:prop>` +
			`<D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>` +
			`</D:multistatus>`))
	}))
	defer srv.Close()

	c, err := Dial(srv.URL, WithRetry(2))
	if err != nil {
		t.Fatalf("could not build a client: %v", err)
	}

	entries, err := c.Dirlist(context.Background(), "/dir")
	if err != nil {
		t.Fatalf("could not list the collection: %v", err)
	}
	if got, want := len(entries), 1; got != want {
		t.Fatalf("the listing holds %d entries, want %d", got, want)
	}
	if got, want := n.Load(), int64(2); got != want {
		t.Fatalf("the server saw %d requests, want %d", got, want)
	}
}

func TestConformance_HardenedRetriesAndBoundsEachAttempt(t *testing.T) {
	srv, n := countingServer(t, nil, http.StatusServiceUnavailable, http.StatusOK)

	c, err := Dial(srv.URL, Hardened())
	if err != nil {
		t.Fatalf("could not build a client: %v", err)
	}
	if got, want := c.retry.attempts, hardenedAttempts; got != want {
		t.Fatalf("a hardened client makes %d attempts, want %d", got, want)
	}
	if c.http.Timeout <= 0 {
		t.Fatal("a hardened client waits indefinitely for an answer")
	}

	if _, err := c.ReadAll(context.Background(), "/a.txt"); err != nil {
		t.Fatalf("a request that eventually succeeded was reported as failed: %v", err)
	}
	if got, want := n.Load(), int64(2); got != want {
		t.Fatalf("the server saw %d requests, want %d", got, want)
	}
}

func TestConformance_ARetriedRequestKeepsItsCredential(t *testing.T) {
	// The credential is attached before the retry loop, and the loop clones the
	// request. A clone that dropped the header would send the second attempt
	// unauthenticated, which a server answers with 401 — a token that looks
	// expired, on a network that merely hiccuped.
	var seen atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer s3cr3t" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if seen.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("go-hep"))
	}))
	defer srv.Close()

	c, err := Dial(srv.URL, WithBearerToken("s3cr3t"), WithInsecureBearerToken(), WithRetry(3))
	if err != nil {
		t.Fatalf("could not build a client: %v", err)
	}

	got, err := c.ReadAll(context.Background(), "/a.txt")
	if err != nil {
		t.Fatalf("a request that eventually succeeded was reported as failed: %v", err)
	}
	if want := "go-hep"; string(got) != want {
		t.Fatalf("the file holds %q, want %q", got, want)
	}
	if got, want := seen.Load(), int64(2); got != want {
		t.Fatalf("the server saw %d authenticated requests, want %d", got, want)
	}
}

func TestConformance_ARetriedFailureNamesNoCredential(t *testing.T) {
	// The error a retry loop builds carries the URL, and a token in a query
	// string is a credential in a log file.
	c, err := Dial("https://example.invalid/a.txt?authz=s3cr3t", WithRetry(2))
	if err != nil {
		t.Fatalf("could not build a client: %v", err)
	}

	_, err = c.Stat(context.Background(), "")
	if err == nil {
		t.Fatal("a request to a host that does not resolve succeeded")
	}
	if strings.Contains(err.Error(), "s3cr3t") {
		t.Fatalf("the failure quotes the credential: %v", err)
	}
	if !errors.Is(err, fs.ErrNotExist) && !strings.Contains(err.Error(), "attempts") {
		t.Fatalf("the failure says %q, want it to say how many attempts were made", err)
	}
}

func TestConformance_AnErrorNamesNoCredential(t *testing.T) {
	// Every shape of URL a credential can hide in: the query string, where
	// XRootD and WebDAV put their authorization; the fragment; and the userinfo
	// password, which is the only one url.Redacted covers.
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"a query string", "https://host/a.txt?authz=s3cr3t", "https://host/a.txt"},
		{"a signed URL", "https://host/a.txt?X-Amz-Signature=s3cr3t&x=1", "https://host/a.txt"},
		{"a fragment", "https://host/a.txt#s3cr3t", "https://host/a.txt"},
		{"a password", "https://user:s3cr3t@host/a.txt", "https://user@host/a.txt"},
		{"nothing to hide", "https://host/a.txt", "https://host/a.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, err := url.Parse(tc.raw)
			if err != nil {
				t.Fatalf("could not parse %q: %v", tc.raw, err)
			}
			if got := safeURL(u); got != tc.want {
				t.Fatalf("safeURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}

	if got := safeURL(nil); got != "" {
		t.Fatalf("safeURL(nil) = %q, want nothing", got)
	}
}

func TestConformance_ScrubLeavesOtherErrorsAlone(t *testing.T) {
	// It rewrites the one error type that carries a URL, and nothing else: an
	// error it did not recognise has to come back as it went in, or a caller
	// matching on it stops matching.
	want := errors.New("something else entirely")
	if got := scrub(want); got != want {
		t.Fatalf("scrub returned %v, want the error it was given", got)
	}
	if got := scrub(nil); got != nil {
		t.Fatalf("scrub(nil) = %v, want nothing", got)
	}

	// A URL that cannot be parsed is one where there is no telling which part
	// is the credential, so none of it is kept.
	uerr := &url.Error{Op: "Get", URL: "https://host/\x7f?authz=s3cr3t", Err: errors.New("boom")}
	scrub(uerr)
	if strings.Contains(uerr.URL, "s3cr3t") {
		t.Fatalf("an unparseable URL kept its credential: %q", uerr.URL)
	}
}
