// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"context"
	"errors"
	"fmt"

	"go-hep.org/x/hep/xrootd/xrdfs"
)

// ErrUnsupportedScheme is returned by Dial for a URL scheme that has no
// backend implementation. Alternative-protocol backends (http, https, s3,
// dav) are added in a later phase.
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

// Dial connects to rawurl and returns a protocol-neutral Backend. The scheme
// selects the transport: root, roots, xroot and xroots use native XRootD
// (roots and xroots negotiate in-protocol TLS). Other schemes return
// ErrUnsupportedScheme.
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
	case "http", "https", "s3", "dav":
		return nil, fmt.Errorf("%w: %q (added in a later phase)", ErrUnsupportedScheme, u.Scheme)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedScheme, u.Scheme)
	}
}
