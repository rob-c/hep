// Copyright ©2019 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package http is a plugin for riofs.Open and riofs.Create, to read and write
// ROOT files over http, https, dav and davs.
//
// Requests go through the hardened xrdhttp transport: bounded, jittered
// retries for the failures a wide-area network manufactures, and transport
// errors scrubbed of the query string where WebDAV endpoints carry
// authorization.
//
// An encrypted endpoint (https, davs) is offered the ambient bearer token
// found by the WLCG discovery sequence ($BEARER_TOKEN, $BEARER_TOKEN_FILE,
// then the bt_u<uid> files), when there is one — the same decision xrdcp and
// gfal2 make, taken here because riofs.Open gives the caller nowhere to make
// it themselves. A cleartext endpoint is never offered a token: anyone who
// can observe a bearer token can replay it.
//
// Writing is a different proposition from reading. HTTP has no ranged write
// that servers can be relied on to implement, so a file being written is held
// in memory and PUT whole when it is closed or synced — a ROOT file written
// this way must fit in RAM. Where that is not acceptable, write over the
// native protocol (root://, roots://), which addresses each write by offset
// and streams.
package http

import (
	"context"
	"io"
	"os"
	"runtime"

	"go-hep.org/x/hep/groot/riofs"
	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdhttp"
	"go-hep.org/x/hep/xrootd/xrdio"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/token"
)

func init() {
	riofs.Register("http", openFile)
	riofs.Register("https", openFile)
	riofs.Register("dav", openFile)
	riofs.Register("davs", openFile)

	riofs.RegisterWriter("http", createFile)
	riofs.RegisterWriter("https", createFile)
	riofs.RegisterWriter("dav", createFile)
	riofs.RegisterWriter("davs", createFile)
}

// createFile creates path for writing, and hands back a Writer that owns the
// backend it writes through.
func createFile(path string) (riofs.Writer, error) {
	urn, err := xrdio.Parse(path)
	if err != nil {
		return nil, err
	}

	be, err := xrootd.DialHTTP(path, credentialOptions(urn.Scheme)...)
	if err != nil {
		return nil, err
	}

	f, err := xrdio.CreateFrom(be.FS(), urn.Path)
	if err != nil {
		_ = be.Close()
		return nil, err
	}
	return &remoteFile{File: f, be: be}, nil
}

func openFile(path string) (riofs.Reader, error) {
	urn, err := xrdio.Parse(path)
	if err != nil {
		return nil, err
	}

	be, err := xrootd.DialHTTP(path, credentialOptions(urn.Scheme)...)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	cli := be.(xrootd.HTTPBackend).HTTPClient()

	// A ROOT file is read as scattered spans — header, streamer info, then
	// baskets on demand — which is only affordable when the server answers
	// range requests. One that does not still answers every span correctly,
	// but with the whole file prefix in front of it, so fetch the file once
	// instead.
	ranged, err := cli.Ranges(ctx, urn.Path)
	if err != nil {
		_ = be.Close()
		return nil, err
	}
	if !ranged {
		defer be.Close()
		return tmpFileFrom(ctx, cli, urn.Path)
	}

	f, err := xrdio.OpenFrom(be.FS(), urn.Path)
	if err != nil {
		_ = be.Close()
		return nil, err
	}
	rc, err := rcacheOf(&preader{r: &remoteFile{File: f, be: be}, n: runtime.NumCPU()})
	if err != nil {
		_ = f.Close()
		_ = be.Close()
		return nil, err
	}
	return rc, nil
}

// credentialOptions returns the ambient credential the scheme may be trusted
// with: the discovered bearer token for an encrypted endpoint, nothing for a
// cleartext one, nothing when discovery finds no token. Anonymous access is a
// working configuration, not an error.
func credentialOptions(scheme string) []xrdhttp.Option {
	switch scheme {
	case "https", "davs":
	default:
		return nil
	}
	tok, err := token.Discover()
	if err != nil {
		return nil
	}
	return []xrdhttp.Option{xrdhttp.WithBearerToken(tok)}
}

// remoteFile owns the backend its file reads through: xrdio.OpenFrom leaves
// the filesystem handle with the caller, and dropping it on the floor would
// leak the transport's idle connections for the life of the process.
type remoteFile struct {
	*xrdio.File
	be xrootd.Backend
}

var _ riofs.Writer = (*remoteFile)(nil)

func (f *remoteFile) Close() error {
	err := f.File.Close()
	if e := f.be.Close(); err == nil {
		err = e
	}
	return err
}

// tmpFileFrom downloads the file through the client — credentials, retries
// and all — into a temporary file that deletes itself on Close.
func tmpFileFrom(ctx context.Context, cli *xrdhttp.Client, name string) (riofs.Reader, error) {
	body, err := cli.Fetch(ctx, name)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	f, err := os.CreateTemp("", "riofs-remote-")
	if err != nil {
		return nil, err
	}
	_, err = io.Copy(f, body)
	if err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, err
	}
	_, err = f.Seek(0, 0)
	if err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, err
	}
	return &tmpFile{f}, nil
}

// tmpFile wraps a regular os.File to automatically remove it when closed.
type tmpFile struct {
	*os.File
}

func (f *tmpFile) Close() error {
	err1 := f.File.Close()
	err2 := os.Remove(f.File.Name())
	if err1 != nil {
		return err1
	}
	return err2
}

var (
	_ riofs.Reader = (*tmpFile)(nil)
	_ riofs.Writer = (*tmpFile)(nil)
	_ riofs.Reader = (*remoteFile)(nil)
)
