// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrd

import (
	"errors"
	"fmt"
	"io/fs"

	"go-hep.org/x/hep/xrootd"
)

// errNotRemote guards the one path a caller cannot reach: every exported
// function deals with a local name itself before the remote machinery is
// asked for anything.
var errNotRemote = errors.New("not a remote URL")

// Error is what every function in this package returns when something goes
// wrong, whether the file was on a server or on this machine. It says which
// operation failed, on what, and why — and, for the handful of failures that
// have a usual cause, what that cause usually is.
//
// The underlying error is kept, so the standard tests still work:
//
//	if errors.Is(err, fs.ErrNotExist) { ... }
type Error struct {
	Op   string // the operation: "read", "list", "connect to", …
	Name string // the file or directory it was attempted on
	Err  error  // what went wrong

	// here records that the name was a path on this machine, which changes
	// what is worth suggesting: nothing about servers, proxies or tokens can
	// be the cause of a local failure.
	here bool
}

// wrap gives an error from the local filesystem the same shape as one from a
// server, so that a program moved between the two keeps reporting failures the
// same way. A *fs.PathError is unwrapped first: it repeats the name, which
// Error already has.
func wrap(op, name string, err error) error {
	if err == nil {
		return nil
	}
	var perr *fs.PathError
	if errors.As(err, &perr) {
		err = perr.Err
	}
	return &Error{Op: op, Name: name, Err: err, here: true}
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("xrd: could not %s %q: %v", e.Op, e.Name, e.Err)
	if hint := hint(e); hint != "" {
		msg += " (" + hint + ")"
	}
	return msg
}

func (e *Error) Unwrap() error { return e.Err }

// fail is wrap for an operation whose name may be on either side — a copy, in
// practice, where the same call covers both.
func fail(op, name string, err error) error {
	if err == nil {
		return nil
	}
	if isLocal, _, e := local(name); e == nil && isLocal {
		return wrap(op, name, err)
	}
	return &Error{Op: op, Name: name, Err: err}
}

// hint is the sentence a physicist meeting this failure for the first time
// would otherwise have to ask a colleague for. It is deliberately empty for
// anything whose cause is not usually the same.
func hint(e *Error) string {
	if e.here {
		switch {
		case errors.Is(e.Err, fs.ErrNotExist):
			return "there is nothing at that path on this machine: xrd.List of the directory above it shows what is there"

		case errors.Is(e.Err, fs.ErrPermission):
			return "this account is not allowed to read or write that path"

		case errors.Is(e.Err, fs.ErrExist):
			return "the file is already there: xrd.WriteFile replaces one, xrd.Remove deletes it"
		}
		return ""
	}

	switch {
	case e.Op == "connect to":
		if errors.Is(e.Err, xrootd.ErrUnsupportedScheme) {
			return schemeHint
		}
		return "check the server name and port — 1094 is the XRootD default, 443 the HTTPS one — and whether this machine is allowed to reach it"

	case errors.Is(e.Err, fs.ErrNotExist):
		return "the server has no such path: xrd.List of the directory above it shows what it does have"

	case errors.Is(e.Err, fs.ErrPermission):
		return "the server refused the credentials it was offered: check your proxy with voms-proxy-info, or your token in $BEARER_TOKEN or $BEARER_TOKEN_FILE"

	case errors.Is(e.Err, fs.ErrExist):
		return "the file is already there: xrd.WriteFile replaces one, xrd.Remove deletes it"

	case errors.Is(e.Err, xrootd.ErrUnsupportedScheme):
		return schemeHint
	}
	return ""
}

const schemeHint = "the schemes are root, roots, xroot, xroots, https, http, davs and dav; a name with no scheme at all is a file on this machine"
