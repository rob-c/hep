// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdfs

import "context"

// LinkFS is implemented by filesystems that expose the link vendor extensions
// (kXR_symlink, kXR_link and kXR_readlink).
//
// These are not part of the protocol specification. A storage system that has
// them uses them to publish one file under several names without copying it —
// a dataset re-catalogued under a new production tag, say — and a server built
// without them answers kXR_Unsupported, which is the only way to find out that
// they are missing.
type LinkFS interface {
	// Symlink creates link pointing at target.
	Symlink(ctx context.Context, target, link string) error

	// Hardlink adds newpath as another name for oldpath.
	Hardlink(ctx context.Context, oldpath, newpath string) error

	// Readlink returns what a symbolic link points at.
	Readlink(ctx context.Context, path string) (string, error)
}

// PropertyFS is implemented by filesystems that can set a server-side property
// of the connection (kXR_set).
type PropertyFS interface {
	// SetProperty sends one directive, such as "appid analysis-42".
	SetProperty(ctx context.Context, directive string) error

	// SetAppID labels this connection in the server's monitoring stream, so
	// that an operator looking at the server can tell whose traffic it is.
	SetAppID(ctx context.Context, name string) error
}
