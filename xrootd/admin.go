// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto/hardlink"
	"go-hep.org/x/hep/xrootd/xrdproto/locate"
	"go-hep.org/x/hep/xrootd/xrdproto/prepare"
	"go-hep.org/x/hep/xrootd/xrdproto/query"
	"go-hep.org/x/hep/xrootd/xrdproto/readlink"
	"go-hep.org/x/hep/xrootd/xrdproto/set"
	"go-hep.org/x/hep/xrootd/xrdproto/symlink"
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

// DeepLocate resolves path down to the data servers that hold it, asking every
// manager the answer names in turn (kXR_locate).
//
// A locate against the top of a federation answers with the managers one tier
// down, not with anything that holds a byte. Walking that tree is the only way
// to learn the real replicas, and it is done breadth-first so that the nearest
// tier is resolved before the far ones.
//
// An endpoint is asked once. A supervisor answers as a manager to the tier
// above it and as a server to the tier below, so when the same address comes
// back both ways the server answer is the one kept: dropping it would lose a
// node that does hold the file. Only data servers are returned; a manager that
// answers for nobody simply contributes nothing.
func (fs *fileSystem) DeepLocate(ctx context.Context, path string, opts xrdfs.LocateOptions) ([]xrdfs.Location, error) {
	roots, err := fs.Locate(ctx, path, opts)
	if err != nil {
		return nil, err
	}

	var (
		seen    = make(map[string]xrdfs.Location, len(roots))
		order   []string
		pending = slices.Clone(roots)
	)
	for len(pending) > 0 {
		loc := pending[0]
		pending = pending[1:]

		if known, ok := seen[loc.Addr]; ok {
			if known.IsManager() && !loc.IsManager() {
				seen[loc.Addr] = loc
			}
			continue
		}
		seen[loc.Addr] = loc
		order = append(order, loc.Addr)

		if !loc.IsManager() {
			continue
		}
		// A manager knows where the file is but not what it contains, so the
		// question has to be put again to the endpoint it named. An endpoint
		// that cannot be reached or refuses the question is skipped rather
		// than fatal: one unreachable subtree should not hide the replicas the
		// rest of the federation reported.
		if _, err := fs.c.getSession(ctx, loc.Addr, ""); err != nil {
			continue
		}
		var resp locate.Response
		if _, err := fs.c.sendSession(ctx, loc.Addr, &resp, &locate.Request{Options: uint16(opts), Path: path}); err != nil {
			continue
		}
		kids, err := xrdfs.ParseLocations(resp.Data)
		if err != nil {
			continue
		}
		pending = append(pending, kids...)
	}

	var out []xrdfs.Location
	for _, addr := range order {
		if loc := seen[addr]; !loc.IsManager() {
			out = append(out, loc)
		}
	}
	return out, nil
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

// Stage brings paths online (kXR_prepare with kXR_stage) and returns the
// handle the server assigned the request.
func (fs *fileSystem) Stage(ctx context.Context, paths []string, prio uint8) (string, error) {
	return fs.Prepare(ctx, paths, xrdfs.PrepareStage, prio)
}

// Evict releases the disk copies of paths (kXR_prepare with kXR_evict).
//
// Evicting is the reverse of staging, and the only prepare option that lives
// in the extended half-word rather than the options byte: by the time it was
// given a name of its own, the byte the older options share was full.
func (fs *fileSystem) Evict(ctx context.Context, paths []string) error {
	req := prepare.Request{OptionsX: prepare.Evict, Paths: paths}
	_, err := fs.c.Send(ctx, &prepare.Response{}, &req)
	return err
}

// CancelPrepare withdraws an earlier prepare request, named by the handle it
// returned (kXR_prepare with kXR_cancel).
func (fs *fileSystem) CancelPrepare(ctx context.Context, handle string) error {
	req := prepare.Request{Options: byte(xrdfs.PrepareCancel), Paths: []string{handle}}
	_, err := fs.c.Send(ctx, &prepare.Response{}, &req)
	return err
}

// SetProperty sets a server-side property of this connection (kXR_set).
func (fs *fileSystem) SetProperty(ctx context.Context, directive string) error {
	_, err := fs.c.Send(ctx, &set.Response{}, &set.Request{Data: directive})
	return err
}

// SetAppID labels this connection in the server's monitoring stream
// (kXR_set with the "appid" directive).
//
// Every other client sends one, and a site that cannot tell whose traffic a
// connection carries cannot answer the question its monitoring exists to
// answer. The name is what appears in the server's records, so it names the
// job or the tool rather than the library.
func (fs *fileSystem) SetAppID(ctx context.Context, name string) error {
	return fs.SetProperty(ctx, set.AppIDPrefix+name)
}

// Symlink creates link pointing at target (kXR_symlink).
//
// This is a vendor extension: a server built without it answers
// kXR_Unsupported, which is the only way to find out that it is missing.
func (fs *fileSystem) Symlink(ctx context.Context, target, link string) error {
	_, err := fs.c.Send(ctx, nil, &symlink.Request{Target: target, Link: link})
	return err
}

// Hardlink adds newpath as another name for oldpath (kXR_link).
//
// This is a vendor extension: a server built without it answers
// kXR_Unsupported, which is the only way to find out that it is missing.
func (fs *fileSystem) Hardlink(ctx context.Context, oldpath, newpath string) error {
	_, err := fs.c.Send(ctx, nil, &hardlink.Request{OldPath: oldpath, NewPath: newpath})
	return err
}

// Readlink returns what a symbolic link points at (kXR_readlink).
//
// This is a vendor extension: a server built without it answers
// kXR_Unsupported, which is the only way to find out that it is missing.
func (fs *fileSystem) Readlink(ctx context.Context, path string) (string, error) {
	var resp readlink.Response
	if _, err := fs.c.Send(ctx, &resp, &readlink.Request{Path: path}); err != nil {
		return "", err
	}
	return strings.TrimRight(string(resp.Data), "\x00"), nil
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
	_ xrdfs.LocateFS   = (*fileSystem)(nil)
	_ xrdfs.PrepareFS  = (*fileSystem)(nil)
	_ xrdfs.QueryFS    = (*fileSystem)(nil)
	_ xrdfs.LinkFS     = (*fileSystem)(nil)
	_ xrdfs.PropertyFS = (*fileSystem)(nil)
)
