// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package token implements the XRootD "ztn" security provider: a WLCG bearer
// token (JWT) sent as the kXR_auth payload in a single round. See the XRootD
// XrdSecztn protocol.
package token // import "go-hep.org/x/hep/xrootd/xrdproto/auth/token"

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"go-hep.org/x/hep/xrootd/xrdproto/auth"
)

// Type is the 4-byte credential type for the ztn provider.
var Type = [4]byte{'z', 't', 'n', 0}

// Default is the token provider discovered from the environment, or nil when
// no bearer token could be found.
var Default auth.Auther

func init() {
	if tok, err := Discover(); err == nil {
		Default = &Auth{Token: tok}
	}
}

// Auth is the "ztn" security provider carrying a bearer token.
type Auth struct {
	Token string // the JWT to present
}

// Provider returns the name of the security provider.
func (*Auth) Provider() string { return "ztn" }

// Request forms a kXR_auth request carrying the bearer token. The payload is
// the 4-byte "ztn\0" tag followed by the raw token.
func (a *Auth) Request(params []string) (*auth.Request, error) {
	if a.Token == "" {
		return nil, fmt.Errorf("auth/token: empty bearer token")
	}
	return &auth.Request{Type: Type, Credentials: "ztn\x00" + a.Token}, nil
}

// Discover locates a bearer token, trying $BEARER_TOKEN (a literal token),
// $BEARER_TOKEN_FILE, $XDG_RUNTIME_DIR/bt_u<uid> and /tmp/bt_u<uid> in order.
// Trailing whitespace and NUL bytes are trimmed.
func Discover() (string, error) {
	if v := strings.TrimRight(os.Getenv("BEARER_TOKEN"), " \t\r\n\x00"); v != "" {
		return v, nil
	}
	var paths []string
	if p := os.Getenv("BEARER_TOKEN_FILE"); p != "" {
		paths = append(paths, p)
	}
	uid := strconv.Itoa(os.Geteuid())
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		paths = append(paths, xdg+"/bt_u"+uid)
	}
	paths = append(paths, "/tmp/bt_u"+uid)

	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if tok := strings.TrimRight(string(raw), " \t\r\n\x00"); tok != "" {
			return tok, nil
		}
	}
	return "", fmt.Errorf("auth/token: no bearer token found")
}

var _ auth.Auther = (*Auth)(nil)
