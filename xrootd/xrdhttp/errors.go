// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdhttp // import "go-hep.org/x/hep/xrootd/xrdhttp"

import (
	"fmt"
	"io/fs"
	"net/http"
)

// StatusError is a request the server refused, carrying the status it refused
// it with. It exists so a caller can tell the ordinary cases apart — a missing
// file, a denied authorization — without matching on the text of an error.
type StatusError struct {
	Op     string // the HTTP method, e.g. "GET" or "PROPFIND"
	Name   string // the path that was addressed
	Code   int    // the HTTP status code
	Status string // the status line, e.g. "404 Not Found"
}

func (err *StatusError) Error() string {
	return fmt.Sprintf("xrdhttp: %s %q: unexpected status %s", err.Op, err.Name, err.Status)
}

// Is maps the status onto the standard library's error values, so that a
// failure means the same thing whichever transport it arrived on: the native
// protocol answers a missing file with kXR_NotFound and an HTTP endpoint
// answers it with 404, and a caller should be able to write
// errors.Is(err, fs.ErrNotExist) for both.
func (err *StatusError) Is(target error) bool {
	switch target {
	case fs.ErrNotExist:
		switch err.Code {
		case http.StatusNotFound, http.StatusGone:
			return true
		case http.StatusConflict:
			// RFC 4918 §9.3.1: a MKCOL is answered with 409 when a parent
			// collection does not exist. The path that is missing is not the
			// one addressed, but it is still a missing path.
			return err.Op == "MKCOL"
		}
	case fs.ErrExist:
		// RFC 4918 §9.3.1: MKCOL on something that is already there.
		return err.Code == http.StatusMethodNotAllowed && err.Op == "MKCOL"
	case fs.ErrPermission:
		switch err.Code {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusProxyAuthRequired:
			return true
		}
	case ErrNotSupported:
		switch err.Code {
		case http.StatusNotImplemented:
			return true
		case http.StatusMethodNotAllowed:
			// A server that does not speak WebDAV refuses PROPFIND and MKCOL
			// with 405. For MKCOL that is "it already exists" (above); for the
			// rest it is the verb itself being unavailable.
			return err.Op != "MKCOL"
		}
	}
	return false
}

// statusError builds a StatusError from a response the client cannot act on.
func statusError(op, name string, resp *http.Response) error {
	return &StatusError{Op: op, Name: name, Code: resp.StatusCode, Status: resp.Status}
}
