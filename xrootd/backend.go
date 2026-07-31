// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdhttp"
)

// ErrUnsupportedScheme is returned by Dial for a URL scheme that has no
// backend implementation.
var ErrUnsupportedScheme = errors.New("xrootd: unsupported URL scheme")

// Backend is a protocol-neutral handle to a storage endpoint. It gives
// higher-level components (such as a copy engine or alternative-protocol
// support) a single entry point independent of whether the underlying
// transport is native XRootD, HTTP, S3, or WebDAV.
type Backend interface {
	// FS returns a filesystem view of the endpoint.
	FS() xrdfs.FileSystem
	// Client returns the underlying XRootD client, or nil for non-XRootD backends.
	Client() *Client
	// Close releases the backend's resources.
	Close() error
}

// xrootdBackend adapts a native XRootD Client to the Backend interface.
type xrootdBackend struct {
	client *Client
}

func (b *xrootdBackend) FS() xrdfs.FileSystem { return b.client.FS() }
func (b *xrootdBackend) Client() *Client      { return b.client }
func (b *xrootdBackend) Close() error         { return b.client.Close() }

// httpBackend adapts an xrdhttp.Client to the Backend interface. Client
// returns nil: there is no native XRootD session behind an HTTP endpoint, and
// callers must handle that rather than be handed a stub that fails later.
type httpBackend struct {
	client *xrdhttp.Client
}

func (b *httpBackend) FS() xrdfs.FileSystem { return b.client.FS() }
func (b *httpBackend) Client() *Client      { return nil }
func (b *httpBackend) Close() error         { return nil }

// HTTPClient returns the underlying HTTP client, giving access to the
// operations that have no xrdfs equivalent (third-party copy, in particular).
func (b *httpBackend) HTTPClient() *xrdhttp.Client { return b.client }

// HTTPBackend is implemented by backends that speak HTTP, letting a caller
// reach HTTP-only operations such as third-party copy.
type HTTPBackend interface {
	HTTPClient() *xrdhttp.Client
}

// Dial connects to rawurl and returns a protocol-neutral Backend. The scheme
// selects the transport:
//
//   - root, roots, xroot, xroots: native XRootD (roots and xroots negotiate
//     in-protocol TLS);
//   - http, https: XRootD HTTP data access, with the WebDAV extensions used
//     for directory operations;
//   - dav, davs: WebDAV, mapped onto http and https respectively.
//
// Any other scheme returns ErrUnsupportedScheme.
//
// opts apply to the native XRootD transport only; use DialHTTP to configure an
// HTTP endpoint's TLS and credentials.
func Dial(ctx context.Context, rawurl, username string, opts ...Option) (Backend, error) {
	u, err := ParseURL(rawurl)
	if err != nil {
		return nil, err
	}

	switch u.Scheme {
	case "", "root", "roots", "xroot", "xroots":
		client, err := NewClient(ctx, rawurl, username, opts...)
		if err != nil {
			return nil, err
		}
		return &xrootdBackend{client: client}, nil
	case "http", "https", "dav", "davs":
		return DialHTTP(rawurl)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedScheme, u.Scheme)
	}
}

// DialHTTP connects to an http, https, dav or davs URL and returns an HTTP
// Backend. The dav and davs schemes are rewritten to http and https, which is
// what they denote; opts configure TLS and credentials.
func DialHTTP(rawurl string, opts ...xrdhttp.Option) (Backend, error) {
	u, err := ParseURL(rawurl)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "http", "https":
	case "dav":
		rawurl = "http://" + strings.TrimPrefix(rawurl, "dav://")
	case "davs":
		rawurl = "https://" + strings.TrimPrefix(rawurl, "davs://")
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedScheme, u.Scheme)
	}

	client, err := xrdhttp.Dial(rawurl, opts...)
	if err != nil {
		return nil, err
	}
	return &httpBackend{client: client}, nil
}
