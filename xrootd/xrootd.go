// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package xrootd implements the XRootD protocol from
//
//	http://xrootd.org
//
// Package xrootd provides a Client and a Server.
//
// The NewClient function connects to a server:
//
//	ctx := context.Background()
//
//	client, err := xrootd.NewClient(ctx, addr, username)
//	if err != nil {
//		// handle error
//	}
//
//	// ...
//
//	if err := client.Close(); err != nil {
//		// handle error
//	}
//
// The NewServer function creates a server:
//
//	srv := xrootd.NewServer(xrootd.Default(), nil)
//	err := srv.Serve(listener)
//
// # Reading and listing
//
// [Client.FS] returns an [go-hep.org/x/hep/xrootd/xrdfs.FileSystem]: Dirlist
// for one directory, Statx for a batch of paths in one request, and
// [go-hep.org/x/hep/xrootd/xrdfs.Walk] and
// [go-hep.org/x/hep/xrootd/xrdfs.Glob] for a subtree. The same interface is
// implemented over WebDAV by [go-hep.org/x/hep/xrootd/xrdhttp], so a program
// that takes a URL can serve root:// and davs:// with one code path.
//
// When a URL is what the program has rather than an address and a path,
// [go-hep.org/x/hep/xrootd/xrdio.Open] takes the whole thing and picks the
// transport from its scheme.
//
// # Wide-area networks
//
// Across a wide-area link the failure to plan for is not a refused connection,
// which is reported at once, but a path that stops forwarding while both ends
// still believe the connection is up. A read on that connection then blocks
// until TCP gives up, which on Linux is the better part of an hour.
//
// [NewClient] bounds it without being asked. The settings are [Hardened] — a
// stream timeout, a connection window, retried connections and TCP keepalives —
// and each is an option in its own right, [WithStreamTimeout],
// [WithConnectionWindow], [WithConnectionRetry] and [WithKeepAlive], that a
// caller can name to move it. Each also reads its XRD_* environment variable,
// so a batch system can tune a program it did not write; an option passed in
// code outranks the environment, and the environment outranks the defaults.
//
// [Unbounded] removes all of it, for a caller whose own context is the only
// deadline it wants.
//
// # Examples
//
// go-hep.org/x/hep/xrootd/example holds thirty short programs covering reading
// and writing over root://, listing a remote namespace, and token-authenticated
// access over WebDAV.
package xrootd // import "go-hep.org/x/hep/xrootd"
