// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdhttp // import "go-hep.org/x/hep/xrootd/xrdhttp"

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// MethodCopy is the WebDAV/HTTP-TPC COPY method.
const MethodCopy = "COPY"

// TPCOptions configures a third-party copy.
type TPCOptions struct {
	// RemoteToken is the bearer token the *remote* endpoint must accept. It
	// travels in TransferHeaderAuthorization, which the active endpoint strips
	// the prefix from and replays as Authorization against its peer. The
	// client's own credential (WithBearerToken) authenticates the active
	// endpoint and is not reused for the remote one.
	RemoteToken string

	// Overwrite allows replacing an existing destination.
	Overwrite bool

	// RequireChecksumVerification asks the endpoints to compare checksums and
	// fail the transfer if they differ.
	RequireChecksumVerification bool

	// Progress, when non-nil, is called for every performance marker the
	// active endpoint emits while the copy runs.
	Progress func(TPCMarker)
}

// TPCMarker is one performance marker from an in-progress third-party copy.
type TPCMarker struct {
	Timestamp        time.Time
	StripeIndex      int
	BytesTransferred int64
	TotalStripes     int
}

// TPCError reports a third-party copy that the endpoint accepted and then
// failed. It is distinct from a transport error: the HTTP exchange succeeded,
// and the failure was announced in the response body.
type TPCError struct {
	Reason string
}

func (e *TPCError) Error() string { return "xrdhttp: third-party copy failed: " + e.Reason }

// ErrTPCNoOutcome reports a third-party copy whose response ended without
// announcing either success or failure. The transfer's true state is unknown,
// so it must not be treated as either.
var ErrTPCNoOutcome = errors.New("xrdhttp: third-party copy ended without reporting an outcome")

// Push performs a third-party copy of the client's name to the remote URL dst,
// driving it from the source: a COPY sent to name carrying a Destination
// header. The bytes never pass through this process.
func (c *Client) Push(ctx context.Context, name, dst string, opts TPCOptions) error {
	return c.tpc(ctx, name, "Destination", dst, opts)
}

// Pull performs a third-party copy of the remote URL src into the client's
// name, driving it from the destination: a COPY sent to name carrying a Source
// header. The bytes never pass through this process.
//
// Pull is the mode to prefer where both work: the endpoint doing the writing is
// the one that reports whether the write succeeded.
func (c *Client) Pull(ctx context.Context, name, src string, opts TPCOptions) error {
	return c.tpc(ctx, name, "Source", src, opts)
}

func (c *Client) tpc(ctx context.Context, name, remoteHdr, remoteURL string, opts TPCOptions) error {
	req, err := http.NewRequestWithContext(ctx, MethodCopy, c.urlFor(name), nil)
	if err != nil {
		return err
	}
	req.Header.Set(remoteHdr, remoteURL)
	req.Header.Set("Overwrite", boolFlag(opts.Overwrite))
	// Ask for the marker stream rather than a single terminal status: without
	// it a long copy looks indistinguishable from a hung connection.
	req.Header.Set("X-Number-Of-Streams", "1")
	if opts.RequireChecksumVerification {
		req.Header.Set("RequireChecksumVerification", "true")
	}
	switch {
	case opts.RemoteToken != "":
		req.Header.Set("TransferHeaderAuthorization", "Bearer "+opts.RemoteToken)
		// Tell the endpoint not to look for a delegated X.509 proxy: the
		// remote credential is the token above.
		req.Header.Set("Credential", "none")
	case c.token != "":
		// No distinct remote credential was given. Reusing the client's own
		// token is what stock clients do for same-issuer transfers.
		req.Header.Set("TransferHeaderAuthorization", "Bearer "+c.token)
		req.Header.Set("Credential", "none")
	}

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("xrdhttp: COPY %q: %w", name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("xrdhttp: COPY %q: unexpected status %s", name, resp.Status)
	}

	// A 2xx here means the endpoint ACCEPTED the copy, not that it completed:
	// HTTP-TPC reports the actual outcome in the body, after the transfer has
	// run. Returning success on the status code alone silently turns a failed
	// transfer into a successful one.
	return parseTPCBody(resp.Body, opts.Progress)
}

// parseTPCBody consumes the performance-marker stream and returns the outcome
// the endpoint announced.
func parseTPCBody(r io.Reader, progress func(TPCMarker)) error {
	var (
		sc  = bufio.NewScanner(r)
		cur TPCMarker
		in  bool
	)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		switch {
		case strings.EqualFold(line, "Perf Marker"):
			cur, in = TPCMarker{}, true
			continue
		case strings.EqualFold(line, "End"):
			if in && progress != nil {
				progress(cur)
			}
			in = false
			continue
		}

		if lower := strings.ToLower(line); strings.HasPrefix(lower, "success") {
			return nil
		} else if strings.HasPrefix(lower, "failure") {
			return &TPCError{Reason: strings.TrimSpace(strings.TrimPrefix(line[len("failure"):], ":"))}
		}

		if !in {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "timestamp":
			if v, err := strconv.ParseInt(val, 10, 64); err == nil {
				cur.Timestamp = time.Unix(v, 0)
			}
		case "stripe index":
			if v, err := strconv.Atoi(val); err == nil {
				cur.StripeIndex = v
			}
		case "stripe bytes transferred":
			if v, err := strconv.ParseInt(val, 10, 64); err == nil {
				cur.BytesTransferred = v
			}
		case "total stripe count":
			if v, err := strconv.Atoi(val); err == nil {
				cur.TotalStripes = v
			}
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("xrdhttp: reading third-party copy progress: %w", err)
	}
	// The stream ended with neither "success" nor "failure". The transfer may
	// have completed, may have been cut off mid-copy; nothing here says which,
	// so it cannot be reported as done.
	return ErrTPCNoOutcome
}

func boolFlag(v bool) string {
	if v {
		return "T"
	}
	return "F"
}
