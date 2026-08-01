// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdhttp // import "go-hep.org/x/hep/xrootd/xrdhttp"

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// A request to grid storage over HTTP crosses more machinery than the native
// protocol does — proxies, redirectors, load balancers, gateways in front of
// the storage itself — and each of them has its own way of saying "not now": a
// connection reset before the response, a 503 while a backend is drained, a 429
// from a rate limiter. None of those mean the file is unavailable, and a client
// that reports them as failures makes a transient inconvenience look like a
// missing dataset.
//
// What can be repeated safely is narrower than it looks. A request may only be
// resent when repeating it cannot change the server's state in a way the caller
// did not ask for twice, and when its body can be produced again.

// The retry schedule.
const (
	// retryBackoffBase is the pause before the second attempt.
	retryBackoffBase = 500 * time.Millisecond
	// retryBackoffMax caps the doubling.
	retryBackoffMax = 10 * time.Second
	// retryBackoffJitter is how much of each pause is randomised, as a
	// fraction, so a fleet of clients that hit the same overloaded gateway does
	// not come back in step.
	retryBackoffJitter = 0.25
	// retryAfterMax caps how long a server's own Retry-After is honoured. The
	// header is advice, and a gateway that asks for an hour is asking for
	// longer than any caller meant to wait.
	retryAfterMax = time.Minute
	// hardenedAttempts is how many attempts Hardened allows in total.
	hardenedAttempts = 5
	// hardenedTimeout bounds a single attempt, so a stalled response is retried
	// rather than waited on. It has to cover a whole ranged read, which is why
	// it is minutes and not seconds.
	hardenedTimeout = 5 * time.Minute
)

// retryPolicy is how many times a request may be sent in total. Zero and one
// both mean "once".
type retryPolicy struct {
	attempts int
}

// WithRetry sets how many times a request is sent in total before its failure
// is reported: n = 1, the default, sends it once.
//
// Only a request that can be repeated without changing what the server did is
// resent — GET, HEAD, PROPFIND, OPTIONS, PUT and DELETE, and only when its body
// can be produced a second time. A COPY, which is how a third-party transfer is
// started, is never resent: the first one may well be running.
//
// Resent are transport failures that happened before a response was read, and
// the statuses that mean the server was not able to answer this time — 429, and
// 500, 502, 503 and 504. A 404 or a 403 is an answer, and is returned as one.
func WithRetry(n int) Option {
	return func(c *config) {
		if n < 1 {
			c.err = fmt.Errorf("xrdhttp: retry attempt count %d is less than one", n)
			return
		}
		c.retry.attempts = n
	}
}

// Hardened configures a client for a network that cannot be trusted to fail
// loudly: a per-request timeout, and retries of the requests that can be
// repeated safely.
//
// It is off by default because both settings decide how long a caller's program
// is willing to wait, and that is not a decision a library should make for
// every program that links it. Hardened is the answer for the one caller that
// knows: a program reading from grid storage over the wide area.
//
// It is an ordinary option, so anything applied after it wins:
//
//	cli, err := xrdhttp.Dial(url,
//		xrdhttp.Hardened(),
//		xrdhttp.WithTimeout(30*time.Minute), // whole-file reads of large objects
//	)
func Hardened() Option {
	return func(c *config) {
		WithRetry(hardenedAttempts)(c)
		WithTimeout(hardenedTimeout)(c)
	}
}

// retryableMethods are the methods that may be sent a second time. They are the
// idempotent ones: repeating any of them leaves the server in the state a
// single one would have left it in, so a retry after a lost response cannot
// produce a second effect.
//
// POST and COPY are absent for the same reason. A COPY in particular starts a
// third-party transfer whose first attempt may still be running, and a second
// one would have two servers writing the same file.
var retryableMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
	http.MethodPut:     true,
	http.MethodDelete:  true,
	"PROPFIND":         true,
}

// replayable reports whether req may be sent again: the method has to be one
// where that is harmless, and the body has to be one that can be produced a
// second time.
//
// A body the caller handed over as a stream is gone once it has been read, and
// resending the request would upload whatever is left of it. net/http records
// GetBody for the bodies it can rewind itself, which covers every request this
// package builds from a buffer.
func replayable(req *http.Request) bool {
	if !retryableMethods[req.Method] {
		return false
	}
	return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
}

// retryableStatus reports whether a status says the server could not answer
// this time, as opposed to answering that it will not.
func retryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// retryDelay returns how long to wait before attempt n+1, counting the first
// attempt as 0. A server that named its own delay in a Retry-After header is
// preferred to the backoff, up to retryAfterMax.
func retryDelay(n int, resp *http.Response) time.Duration {
	if d, ok := retryAfter(resp); ok {
		return min(d, retryAfterMax)
	}
	d := retryBackoffBase
	for range n {
		d *= 2
		if d >= retryBackoffMax {
			d = retryBackoffMax
			break
		}
	}
	jitter := retryBackoffJitter * float64(d) * (2*rand.Float64() - 1)
	return max(time.Duration(float64(d)+jitter), 0)
}

// retryAfter reads a Retry-After header, which RFC 9110 §10.2.3 spells either
// as a whole number of seconds or as an HTTP date.
func retryAfter(resp *http.Response) (time.Duration, bool) {
	if resp == nil {
		return 0, false
	}
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	t, err := http.ParseTime(v)
	if err != nil {
		return 0, false
	}
	// A date in the past means the wait is already over.
	return max(time.Until(t), 0), true
}

// send sends req, resending it while the policy allows and the failure is one
// that a second attempt could get past.
func (c *Client) send(req *http.Request) (*http.Response, error) {
	if c.retry.attempts <= 1 || !replayable(req) {
		return c.roundTrip(req)
	}

	ctx := req.Context()
	var (
		resp *http.Response
		err  error
	)
	for i := range c.retry.attempts {
		if i > 0 {
			if werr := sleepCtx(ctx, retryDelay(i-1, resp)); werr != nil {
				break
			}
			// The response that asked for the retry has been read for its
			// Retry-After and is finished with. Draining it lets the
			// connection be reused instead of being dropped and redialled.
			drain(resp)
			resp = nil
		}

		attempt := req.Clone(ctx)
		if req.GetBody != nil {
			body, berr := req.GetBody()
			if berr != nil {
				return nil, berr
			}
			attempt.Body = body
		}

		resp, err = c.roundTrip(attempt)
		switch {
		case err != nil:
			if ctx.Err() != nil {
				// The caller gave up. Trying again would fail the same way.
				return nil, err
			}
		case !retryableStatus(resp.StatusCode):
			return resp, nil
		}
	}
	if resp != nil {
		// The last attempt was answered, just not with anything better. The
		// caller gets the answer, and decides what to say about it.
		return resp, nil
	}
	return nil, fmt.Errorf("xrdhttp: %s %q failed after %d attempts: %w", req.Method, safeURL(req.URL), c.retry.attempts, err)
}

// roundTrip sends req once, with the URL taken out of whatever error comes
// back.
//
// net/http records the URL it was given in the *url.Error it builds for a
// transport failure, verbatim and including the query string. That is where an
// XRootD or WebDAV endpoint carries its authorization, so the error for a
// connection that could not be made is an error that carries a live credential
// into the caller's log.
func (c *Client) roundTrip(req *http.Request) (*http.Response, error) {
	resp, err := c.http.Do(req)
	return resp, scrub(err)
}

// scrub replaces the URL a *url.Error carries with one that cannot name a
// credential. The error has just been built and is not shared, so rewriting it
// in place is safe.
func scrub(err error) error {
	var uerr *url.Error
	if !errors.As(err, &uerr) {
		return err
	}
	u, perr := url.Parse(uerr.URL)
	if perr != nil {
		// Unparseable, so there is no telling which part of it is the
		// credential. Nothing is safer than a guess.
		uerr.URL = ""
		return err
	}
	uerr.URL = safeURL(u)
	return err
}

// safeURL renders u without anything that could be a credential.
//
// XRootD and WebDAV endpoints carry authorization in the query string —
// ?authz=Bearer%20…, a macaroon, a signed-URL signature — and an error naming
// the URL is an error that ends up in a log file. url.Redacted only hides the
// password in the userinfo, which is the one place these never are.
func safeURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	clean := *u
	clean.RawQuery = ""
	clean.Fragment = ""
	if clean.User != nil {
		clean.User = url.User(clean.User.Username())
	}
	return clean.String()
}

// sleepCtx waits for d, or until ctx is done.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// drain reads and closes a response body that is being thrown away, so the
// connection underneath it goes back to the pool rather than being closed.
func drain(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	// Bounded: a body being discarded is not worth reading forever, and an
	// endpoint that answers 503 with an endless one would otherwise hold this
	// goroutine while it did.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
}
