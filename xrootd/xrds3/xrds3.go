// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrds3 // import "go-hep.org/x/hep/xrootd/xrds3"

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go-hep.org/x/hep/xrootd/internal/s3cred"
)

// Credentials is a resolved S3 access-key/secret pair, as accepted by [New].
//
// It is an alias for the type the resolver returns, so that a caller outside
// this module can name and build one: the resolver itself lives in an internal
// package and cannot be imported.
type Credentials = s3cred.Credentials

// Provider resolves S3 credentials. AccessKey and Secret, when both set, take
// precedence over the environment and the AWS shared credentials file.
//
// Provider{}.Resolve returns the first complete pair from, in order: the
// explicit fields; $AWS_ACCESS_KEY_ID and $AWS_SECRET_ACCESS_KEY; the [default]
// profile of ~/.aws/credentials.
type Provider = s3cred.Provider

// Client accesses objects in a single S3 bucket using AWS Signature Version 4.
type Client struct {
	endpoint string // scheme://host[:port] of the S3 service
	bucket   string
	region   string
	creds    Credentials
	http     *http.Client
	now      func() time.Time // overridable for tests
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient sets the underlying HTTP client (e.g. one with a custom TLS
// config for an HTTPS endpoint).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithRegion sets the AWS region used in the signature (default "us-east-1").
func WithRegion(region string) Option {
	return func(c *Client) { c.region = region }
}

// New creates an S3 client for bucket at endpoint (e.g.
// "https://s3.example.org") using the resolved credentials.
func New(endpoint, bucket string, creds Credentials, opts ...Option) *Client {
	c := &Client{
		endpoint: strings.TrimRight(endpoint, "/"),
		bucket:   bucket,
		region:   "us-east-1",
		creds:    creds,
		http:     http.DefaultClient,
		now:      time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// urlFor builds the path-style URL for key: endpoint/bucket/key.
func (c *Client) urlFor(key string) string {
	return c.endpoint + "/" + c.bucket + "/" + strings.TrimPrefix(key, "/")
}

func (c *Client) do(req *http.Request, payloadHash string) (*http.Response, error) {
	sign(req, c.creds.AccessKey, c.creds.Secret, c.region, "s3", payloadHash, c.now())
	return c.http.Do(req)
}

// Stat returns the object size via a HEAD request; exists is false on 404.
func (c *Client) Stat(ctx context.Context, key string) (size int64, exists bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, c.urlFor(key), nil)
	if err != nil {
		return 0, false, err
	}
	resp, err := c.do(req, emptyPayloadHash)
	if err != nil {
		return 0, false, fmt.Errorf("xrds3: HEAD %q: %w", key, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	switch resp.StatusCode {
	case http.StatusOK:
		return resp.ContentLength, true, nil
	case http.StatusNotFound:
		return 0, false, nil
	default:
		return 0, false, fmt.Errorf("xrds3: HEAD %q: unexpected status %s", key, resp.Status)
	}
}

// ReadAll downloads the whole object.
func (c *Client) ReadAll(ctx context.Context, key string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.urlFor(key), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req, emptyPayloadHash)
	if err != nil {
		return nil, fmt.Errorf("xrds3: GET %q: %w", key, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("xrds3: GET %q: unexpected status %s", key, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// ReadAt reads len(p) bytes into p at offset off with a Range request.
func (c *Client) ReadAt(ctx context.Context, p []byte, key string, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.urlFor(key), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, off+int64(len(p))-1))
	resp, err := c.do(req, emptyPayloadHash)
	if err != nil {
		return 0, fmt.Errorf("xrds3: GET %q range: %w", key, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusPartialContent, http.StatusOK:
	case http.StatusRequestedRangeNotSatisfiable:
		return 0, io.EOF
	default:
		return 0, fmt.Errorf("xrds3: GET %q range: unexpected status %s", key, resp.Status)
	}
	n, err := io.ReadFull(resp.Body, p)
	if err == io.ErrUnexpectedEOF || (err == nil && n < len(p)) {
		return n, io.EOF
	}
	return n, err
}

// Put uploads body (of the given size) to key. The payload is sent as
// UNSIGNED-PAYLOAD, which S3 accepts over any transport.
func (c *Client) Put(ctx context.Context, key string, body io.Reader, size int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.urlFor(key), body)
	if err != nil {
		return err
	}
	if size >= 0 {
		req.ContentLength = size
	}
	resp, err := c.do(req, unsignedPayload)
	if err != nil {
		return fmt.Errorf("xrds3: PUT %q: %w", key, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("xrds3: PUT %q: unexpected status %s", key, resp.Status)
	}
	return nil
}

// Remove deletes the object at key.
func (c *Client) Remove(ctx context.Context, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.urlFor(key), nil)
	if err != nil {
		return err
	}
	resp, err := c.do(req, emptyPayloadHash)
	if err != nil {
		return fmt.Errorf("xrds3: DELETE %q: %w", key, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("xrds3: DELETE %q: unexpected status %s", key, resp.Status)
	}
	return nil
}
