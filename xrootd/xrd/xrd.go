// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package xrd is the short way to use grid storage.
//
// It is meant for people whose job is physics rather than networking. There is
// one function per thing you might want to do, each takes a name and returns
// what you asked for, and nothing has to be set up first:
//
//	data, err := xrd.ReadFile("root://storage.example.org//store/user/gopher/run.dat")
//	files, err := xrd.Glob("root://storage.example.org//store/user/gopher/*.root")
//	here, err := xrd.DownloadAll(files, "data")
//
// A name is either a URL or a plain local path, and every function accepts
// both, so the same program runs against a file on your laptop and a file on
// the grid. The URL scheme picks the protocol: root and xroot are the native
// XRootD ones, roots and xroots are the same with the connection encrypted,
// and https, http, davs and dav go over WebDAV. Local names have no scheme.
//
// # What is taken care of for you
//
// Connections. You never open or close one: the first call to a server dials
// it, and the rest reuse it. A connection that has died is noticed and
// replaced. Call Close at the end of a long-running program if you want the
// sockets back sooner.
//
// Credentials. The same ones the command-line tools find are found here — a
// bearer token in $BEARER_TOKEN or the file the pilot wrote, an X.509 proxy,
// a keytab — and are offered to encrypted endpoints only, because anyone who
// can watch a cleartext connection can steal a token off it.
//
// Timeouts and retries. Grid storage does not usually fail by refusing a
// connection; it fails by going quiet, and an unguarded read then blocks for
// most of an hour. Every connection here is opened with stream timeouts,
// bounded retries and keep-alives already applied.
//
// # When it does not work
//
// Start with [Check]: given the directory you mean to use, it says whether
// this machine can reach the server, whether the credentials were accepted and
// whether the path is there — and it is worth calling before a long job, so
// that an expired proxy is found now rather than in an hour.
//
// Every error from this package is an [*Error]. It names the operation and the
// file, keeps the underlying error so errors.Is(err, fs.ErrNotExist) still
// works, and adds the sentence you would otherwise have to ask a colleague
// for. Printing it is enough:
//
//	xrd: could not read "root://…//store/x.root": file does not exist
//	(the server has no such path: xrd.List of the directory above it shows
//	what it does have)
//
// # When to use something else
//
// This package trades control for brevity. It uses one sensible setting where
// the underlying packages offer a choice, and it has no context.Context, so an
// operation cannot be cancelled from the outside. When that matters — a
// long-lived service, a custom credential, a deadline of your own — use
// [go-hep.org/x/hep/xrootd] and [go-hep.org/x/hep/xrootd/xrdio] directly; this
// package is written on top of them and adds nothing they cannot do.
//
// To read or write a ROOT file, use groot, which takes the same URLs:
//
//	import _ "go-hep.org/x/hep/groot/riofs/plugin/xrootd"
//
//	f, err := groot.Open("root://storage.example.org//store/user/gopher/data.root")
package xrd // import "go-hep.org/x/hep/xrootd/xrd"

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdhttp"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/token"
)

// sessions holds one connection per endpoint, so that a loop over a thousand
// files is a thousand requests rather than a thousand logins.
var sessions = struct {
	sync.Mutex
	db map[string]xrootd.Backend
}{
	db: make(map[string]xrootd.Backend),
}

// Close closes every connection this package has opened. It is not needed
// before exiting — the operating system closes sockets — but a long-running
// program that has finished with storage for a while can call it to let the
// servers go.
//
// Files still open on those connections stop working. Close returns the first
// error it met, having tried to close all of them.
func Close() error {
	sessions.Lock()
	defer sessions.Unlock()

	var err error
	for key, be := range sessions.db {
		if e := be.Close(); e != nil && err == nil {
			err = e
		}
		delete(sessions.db, key)
	}
	return err
}

// Check reports whether this program can reach a name and be allowed at it. It
// is the first thing to run when something is not working, and the thing to
// run before a long job starts, so that a missing credential is found now
// rather than after an hour of processing:
//
//	if err := xrd.Check(dir); err != nil {
//		log.Fatal(err)
//	}
//
// A nil error means the connection was made, the credentials were accepted and
// the path is there. Anything else is the error the attempt produced, with the
// usual cause named: a wrong port, an expired proxy, a path that does not
// exist.
//
// Give Check the file or directory you mean to work on rather than the server
// on its own. Named a server alone, it can only report that the connection and
// the login worked — and an https or davs endpoint is not contacted at all
// until something is asked of it, so there it reports nothing useful.
func Check(name string) error {
	isLocal, u, err := local(name)
	if err != nil {
		return &Error{Op: "check", Name: name, Err: err}
	}
	if isLocal {
		_, err := os.Stat(name)
		return wrap("check", name, err)
	}

	if u.Path == "" || u.Path == "/" {
		if _, _, err := connect(u, name); err != nil {
			return &Error{Op: "connect to", Name: name, Err: err}
		}
		return nil
	}

	_, err = Stat(name)
	return err
}

// endpoint is the connection cache key: one login per user per server per
// protocol.
func endpoint(u xrootd.URL) string {
	return u.Scheme + "://" + u.User + "@" + u.Addr
}

// connect returns the connection to the endpoint named by u, dialling it if
// this is the first time it is asked for. The bool reports whether the
// connection was already there, which is what tells a stale session from a
// server that is genuinely refusing the request.
func connect(u xrootd.URL, name string) (be xrootd.Backend, cached bool, err error) {
	key := endpoint(u)

	sessions.Lock()
	defer sessions.Unlock()

	if be, ok := sessions.db[key]; ok {
		return be, true, nil
	}

	be, err = dial(u, name)
	if err != nil {
		return nil, false, err
	}
	sessions.db[key] = be
	return be, false, nil
}

// drop forgets the connection to u, after it has stopped working.
func drop(u xrootd.URL) {
	key := endpoint(u)

	sessions.Lock()
	defer sessions.Unlock()

	if be, ok := sessions.db[key]; ok {
		delete(sessions.db, key)
		_ = be.Close()
	}
}

// dial opens a connection to the endpoint named by u, with the credentials the
// scheme may be trusted with.
func dial(u xrootd.URL, name string) (xrootd.Backend, error) {
	switch u.Scheme {
	case "http", "https", "dav", "davs":
		return xrootd.DialHTTP(name, credentials(u.Scheme)...)
	default:
		return xrootd.Dial(context.Background(), name, user(u))
	}
}

// credentials returns the ambient credential the scheme may be trusted with:
// the discovered bearer token for an encrypted endpoint, nothing for a
// cleartext one, and nothing when there is no token to find. Anonymous access
// is a working configuration, not an error.
func credentials(scheme string) []xrdhttp.Option {
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

// user is the login name to present: the one in the URL, then $USER, then the
// name every site accepts for anonymous access.
func user(u xrootd.URL) string {
	if u.User != "" {
		return u.User
	}
	if v := os.Getenv("USER"); v != "" {
		return v
	}
	return "nobody"
}

// local reports whether name is a plain path on this machine rather than a URL.
func local(name string) (bool, xrootd.URL, error) {
	u, err := xrootd.ParseURL(name)
	if err != nil {
		return false, xrootd.URL{}, err
	}
	// A bare path is local here, where the lower-level packages read it as an
	// XRootD path with the server left out: a program that says "data.root"
	// means the file next to it, and should not be told about hosts. A name
	// that does carry a scheme is remote even if the server part of it is
	// nonsense — silently reading it off the local disk instead would be a
	// worse answer than the connection error.
	return u.Scheme == "", u, nil
}

// urlOf is the name to hand back to a caller for a path on the endpoint u:
// what they gave us, in the form they can pass to any of these functions.
func urlOf(u xrootd.URL, path string) string {
	host := u.Addr
	if u.User != "" {
		host = u.User + "@" + u.Addr
	}
	switch u.Scheme {
	case "root", "roots", "xroot", "xroots":
		// The doubled slash separates the endpoint from an absolute path on
		// it, and is not optional in the native form.
		return fmt.Sprintf("%s://%s/%s", u.Scheme, host, path)
	default:
		return fmt.Sprintf("%s://%s%s", u.Scheme, host, path)
	}
}

// run performs op on the filesystem holding name, and is where connection
// reuse and its one hazard are handled: a cached connection may have died
// since it was last used, in which case the work is done again on a new one.
// A connection that was just dialled gets no second try — a server saying no
// twice is still no.
func run[T any](op, name string, fn func(ctx context.Context, fsys xrdfs.FileSystem, path string) (T, error)) (T, error) {
	var zero T

	isLocal, u, err := local(name)
	switch {
	case err != nil:
		return zero, &Error{Op: op, Name: name, Err: err}
	case isLocal:
		return zero, &Error{Op: op, Name: name, Err: errNotRemote}
	}

	ctx := context.Background()

	be, cached, err := connect(u, name)
	if err != nil {
		return zero, &Error{Op: "connect to", Name: name, Err: err}
	}

	v, err := fn(ctx, be.FS(), u.Path)
	if err == nil {
		return v, nil
	}
	if !cached || answered(err) {
		return zero, &Error{Op: op, Name: name, Err: err}
	}

	// The connection was one we had lying around and the failure is not one
	// the server chose to give: assume it died while nobody was looking.
	drop(u)
	be, _, err = connect(u, name)
	if err != nil {
		return zero, &Error{Op: "connect to", Name: name, Err: err}
	}
	v, err = fn(ctx, be.FS(), u.Path)
	if err != nil {
		return zero, &Error{Op: op, Name: name, Err: err}
	}
	return v, nil
}

// do is run for an operation with nothing to return.
func do(op, name string, fn func(ctx context.Context, fsys xrdfs.FileSystem, path string) error) error {
	_, err := run(op, name, func(ctx context.Context, fsys xrdfs.FileSystem, path string) (struct{}, error) {
		return struct{}{}, fn(ctx, fsys, path)
	})
	return err
}

// answered reports whether err is an answer the server gave rather than a
// failure of the path to it. Those are the errors worth believing the first
// time.
func answered(err error) bool {
	switch {
	case errors.Is(err, fs.ErrNotExist),
		errors.Is(err, fs.ErrExist),
		errors.Is(err, fs.ErrPermission),
		errors.Is(err, fs.ErrInvalid):
		return true
	}
	return false
}
