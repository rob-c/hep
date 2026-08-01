// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"context"
	"fmt"
	"strings"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto/locate"
	"go-hep.org/x/hep/xrootd/xrdproto/prepare"
	"go-hep.org/x/hep/xrootd/xrdproto/query"
)

// Locate asks where the replicas of path live (kXR_locate).
//
// A path is answered by whichever endpoint the client is talking to, and on a
// federation that is usually a manager which holds no data at all. Locate is
// how a caller learns which data servers actually hold a file — to read from
// the nearest one, to check that a replica exists before a transfer is
// scheduled, or to see that the only copy is still pending on tape.
func (fs *fileSystem) Locate(ctx context.Context, path string, opts xrdfs.LocateOptions) ([]xrdfs.Location, error) {
	var resp locate.Response
	req := locate.Request{Options: uint16(opts), Path: path}
	if _, err := fs.c.Send(ctx, &resp, &req); err != nil {
		return nil, err
	}
	locs, err := xrdfs.ParseLocations(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("xrootd: could not locate %q: %w", path, err)
	}
	return locs, nil
}

// Prepare asks the server to prepare paths for later access (kXR_prepare) and
// returns the handle it assigned the request.
//
// Staging a file from tape takes minutes to hours, so a job that opens it
// without warning waits for all of that inside its first read. Prepare is the
// warning. The handle is what a later cancellation names, which is why it is
// returned rather than discarded: a request that cannot be cancelled keeps a
// tape system busy on behalf of a job that has already given up.
func (fs *fileSystem) Prepare(ctx context.Context, paths []string, opts xrdfs.PrepareOptions, prio uint8) (string, error) {
	var resp prepare.Response
	req := prepare.Request{Options: byte(opts), Priority: prio, Paths: paths}
	if _, err := fs.c.Send(ctx, &resp, &req); err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.TrimRight(string(resp.Data), "\x00")), nil
}

// Query asks the server one question (kXR_query) and returns its answer.
//
// The answer is text, with the trailing NULs the protocol pads it with removed.
// What the text means depends on the code: xrdfs.QuerySpace answers with an
// oss.cgroup line, xrdfs.QueryStats with an XML document, and the opaque codes
// with whatever the site's plugin decided.
func (fs *fileSystem) Query(ctx context.Context, code xrdfs.QueryCode, args string) (string, error) {
	var resp query.Response
	req := query.Request{Query: uint16(code), Args: []byte(args)}
	if _, err := fs.c.Send(ctx, &resp, &req); err != nil {
		return "", err
	}
	return strings.TrimRight(string(resp.Data), "\x00"), nil
}

// QueryConfig looks up server configuration values (kXR_query/kXR_Qconfig).
//
// With no names it asks for "version", which is the one value every server
// answers and the cheapest way to ask a server what it is.
func (fs *fileSystem) QueryConfig(ctx context.Context, names ...string) (map[string]string, error) {
	if len(names) == 0 {
		names = []string{"version"}
	}
	var resp query.Response
	req := query.Request{Query: query.Config, Args: []byte(strings.Join(names, "\n"))}
	if _, err := fs.c.Send(ctx, &resp, &req); err != nil {
		return nil, err
	}
	return xrdfs.ParseConfig(names, resp.Data), nil
}

var (
	_ xrdfs.LocateFS  = (*fileSystem)(nil)
	_ xrdfs.PrepareFS = (*fileSystem)(nil)
	_ xrdfs.QueryFS   = (*fileSystem)(nil)
)
