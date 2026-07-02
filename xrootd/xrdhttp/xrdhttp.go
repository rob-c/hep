// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package xrdhttp implements an HTTP/HTTPS client for XRootD's HTTP data
// access (the "xrdhttp" protocol), including ranged reads, uploads, and
// X.509 client-certificate (mutual TLS) authentication. It provides the
// alternative-protocol backend that the copy engine and CLIs target through a
// protocol-neutral interface.
package xrdhttp // import "go-hep.org/x/hep/xrootd/xrdhttp"

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client accesses files over HTTP or HTTPS against an XRootD (or any
// standards-compliant) HTTP server.
type Client struct {
	base *url.URL
	http *http.Client
}

// Option configures a Client.
type Option func(*config)

type config struct {
	tls     *tls.Config
	timeout time.Duration
	rt      http.RoundTripper
}

// WithTLSConfig sets the TLS configuration (server CAs, client certificate).
func WithTLSConfig(cfg *tls.Config) Option {
	return func(c *config) { c.tls = cfg }
}

// WithClientCertificate configures X.509 mutual-TLS authentication with the
// given certificate (an X.509 proxy or user certificate).
func WithClientCertificate(cert tls.Certificate) Option {
	return func(c *config) {
		if c.tls == nil {
			c.tls = &tls.Config{}
		}
		c.tls.Certificates = append(c.tls.Certificates, cert)
	}
}

// WithRootCAs sets the certificate pool used to verify the server certificate.
func WithRootCAs(pool *x509.CertPool) Option {
	return func(c *config) {
		if c.tls == nil {
			c.tls = &tls.Config{}
		}
		c.tls.RootCAs = pool
	}
}

// WithInsecureTLS disables server-certificate verification (testing only).
func WithInsecureTLS() Option {
	return func(c *config) {
		if c.tls == nil {
			c.tls = &tls.Config{}
		}
		c.tls.InsecureSkipVerify = true
	}
}

// WithTimeout sets the per-request timeout of the underlying HTTP client.
func WithTimeout(d time.Duration) Option {
	return func(c *config) { c.timeout = d }
}

// Dial parses rawurl (an http:// or https:// URL naming the server, optionally
// with a base path) and returns a Client. The path of rawurl becomes the base
// against which relative paths passed to the Client's methods are resolved.
func Dial(rawurl string, opts ...Option) (*Client, error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, fmt.Errorf("xrdhttp: could not parse %q: %w", rawurl, err)
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf("xrdhttp: unsupported scheme %q", u.Scheme)
	}

	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}

	rt := cfg.rt
	if rt == nil {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		if cfg.tls != nil {
			tr.TLSClientConfig = cfg.tls
		}
		rt = tr
	}

	return &Client{
		base: u,
		http: &http.Client{Transport: rt, Timeout: cfg.timeout},
	}, nil
}

// urlFor resolves name against the client's base URL. An absolute path
// replaces the base path; a relative path is joined to it.
func (c *Client) urlFor(name string) string {
	ref := &url.URL{Path: name}
	return c.base.ResolveReference(ref).String()
}

// FileInfo describes a remote file, as reported by a HEAD request.
type FileInfo struct {
	Name    string
	Size    int64
	ModTime time.Time
	// Exists is false when the server reports the object as absent (404).
	Exists bool
}

// Stat issues a HEAD request and returns metadata for the named path.
func (c *Client) Stat(ctx context.Context, name string) (FileInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, c.urlFor(name), nil)
	if err != nil {
		return FileInfo{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return FileInfo{}, fmt.Errorf("xrdhttp: HEAD %q: %w", name, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		return FileInfo{Name: name, Exists: false}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return FileInfo{}, fmt.Errorf("xrdhttp: HEAD %q: unexpected status %s", name, resp.Status)
	}

	fi := FileInfo{Name: name, Size: resp.ContentLength, Exists: true}
	if lm := resp.Header.Get("Last-Modified"); lm != "" {
		if t, err := http.ParseTime(lm); err == nil {
			fi.ModTime = t
		}
	}
	return fi, nil
}

// ReadAll downloads the whole named object.
func (c *Client) ReadAll(ctx context.Context, name string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.urlFor(name), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xrdhttp: GET %q: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("xrdhttp: GET %q: unexpected status %s", name, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// ReadAt reads len(p) bytes into p starting at offset off using an HTTP Range
// request. It returns the number of bytes read; a short read at end of file is
// reported with io.EOF, mirroring io.ReaderAt semantics.
func (c *Client) ReadAt(ctx context.Context, p []byte, name string, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.urlFor(name), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, off+int64(len(p))-1))
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("xrdhttp: GET %q range: %w", name, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusPartialContent, http.StatusOK:
	case http.StatusRequestedRangeNotSatisfiable:
		return 0, io.EOF
	default:
		return 0, fmt.Errorf("xrdhttp: GET %q range: unexpected status %s", name, resp.Status)
	}
	n, err := io.ReadFull(resp.Body, p)
	if err == io.ErrUnexpectedEOF || (err == nil && n < len(p)) {
		return n, io.EOF
	}
	return n, err
}

// Create uploads r to the named path with an HTTP PUT. size may be -1 when the
// length is unknown (chunked transfer encoding is used).
func (c *Client) Create(ctx context.Context, name string, r io.Reader, size int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.urlFor(name), r)
	if err != nil {
		return err
	}
	if size >= 0 {
		req.ContentLength = size
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("xrdhttp: PUT %q: %w", name, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("xrdhttp: PUT %q: unexpected status %s", name, resp.Status)
	}
	return nil
}

// Remove deletes the named path with an HTTP DELETE.
func (c *Client) Remove(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.urlFor(name), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("xrdhttp: DELETE %q: %w", name, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("xrdhttp: DELETE %q: unexpected status %s", name, resp.Status)
	}
	return nil
}

// LoadX509KeyPair loads an X.509 certificate and key from PEM files (a user
// certificate, or a combined proxy file where cert and key are concatenated).
func LoadX509KeyPair(certFile, keyFile string) (tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("xrdhttp: could not load key pair: %w", err)
	}
	return cert, nil
}
