// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// URL stores an absolute reference to a XRootD path.
type URL struct {
	Scheme string // URL scheme without "://" (e.g. "root", "roots"); empty for a bare local path
	Addr   string // address (host [:port]) of the server
	User   string // user name to use to log in
	Path   string // path to the remote file or directory
}

// TLS reports whether the URL scheme mandates in-protocol TLS.
// The secure schemes are "roots" and "xroots".
func (u URL) TLS() bool {
	switch u.Scheme {
	case "roots", "xroots":
		return true
	default:
		return false
	}
}

// ParseURL parses name into an xrootd URL structure.
func ParseURL(name string) (URL, error) {
	var (
		scheme string
		user   string
		addr   string
		path   string
		err    error
	)

	idx := strings.Index(name, "://")
	switch idx {
	case -1:
		path = name
	default:
		scheme = strings.ToLower(name[:idx])
		uri := name[idx+len("://"):]
		tok := strings.SplitN(uri, "/", 2)
		user, addr, err = parseUA(tok[0])
		if err != nil {
			return URL{}, fmt.Errorf("could not parse URI %q: %w", name, err)
		}
		// A URL may name just the server (e.g. "roots://example.org:1094")
		// with no path component.
		if len(tok) == 2 {
			path = "/" + tok[1]
		}
	}

	if strings.HasPrefix(path, "//") {
		path = path[1:]
	}

	return URL{Scheme: scheme, Addr: addr, User: user, Path: path}, nil
}

func parseUA(s string) (user, addr string, err error) {
	switch {
	case strings.Contains(s, "@"):
		toks := strings.SplitN(s, "@", 2)
		user = parseUser(toks[0])
		addr = toks[1]
	default:
		addr = s
	}

	switch {
	case strings.HasPrefix(addr, "["): // IPv6 literal
		idx := strings.LastIndex(addr, "]")
		col := strings.Index(addr[idx+1:], ":")
		if col >= 0 {
			_, _, err = net.SplitHostPort(addr)
		}
	case strings.Contains(addr, ":"):
		_, _, err = net.SplitHostPort(addr)
	}

	if err != nil {
		return "", "", fmt.Errorf("could not extract host+port from URI: %w", err)
	}

	return user, addr, nil
}

func parseUser(s string) string {
	usr, _, ok := strings.Cut(s, ":")
	if !ok {
		return s
	}
	return usr
}

// Username is the login name to present to a server: the one the caller asked
// for, then the one the URL carried, then $USER, then the name sites accept
// for anonymous access.
//
// It exists so that the client, the copy engine and the command-line tools all
// answer "who am I?" the same way, and so that a caller with nothing to say
// about it can pass an empty string and get the sensible answer rather than an
// empty login name.
func Username(want, fromURL string) string {
	switch {
	case want != "":
		return want
	case fromURL != "":
		return fromURL
	}
	if v := os.Getenv("USER"); v != "" {
		return v
	}
	return "nobody"
}
