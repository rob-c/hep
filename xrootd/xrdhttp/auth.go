// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdhttp // import "go-hep.org/x/hep/xrootd/xrdhttp"

import (
	"fmt"
	"net/http"

	"go-hep.org/x/hep/xrootd/xrdproto/auth/token"
)

// WithBearerToken authenticates every request with an RFC 6750 bearer token
// (a WLCG/SciTokens JWT, or a macaroon).
//
// A bearer token is a credential anyone who observes it can replay, so Dial
// refuses to pair one with a cleartext http:// endpoint unless the caller
// also passes WithInsecureBearerToken.
func WithBearerToken(tok string) Option {
	return func(c *config) { c.token = tok }
}

// WithDiscoveredBearerToken authenticates with the ambient bearer token found
// by the WLCG discovery sequence: $BEARER_TOKEN, $BEARER_TOKEN_FILE,
// $XDG_RUNTIME_DIR/bt_u<uid>, then /tmp/bt_u<uid>.
//
// Discovery is deliberately opt-in rather than automatic: unlike the native
// XRootD protocol, where the server names the security providers it accepts
// before the client offers anything, an HTTP request presents its credential
// unprompted — so sending a discovered token to whatever host was dialled has
// to be the caller's decision.
func WithDiscoveredBearerToken() Option {
	return func(c *config) {
		tok, err := token.Discover()
		if err != nil {
			c.err = fmt.Errorf("xrdhttp: no bearer token found: %w", err)
			return
		}
		c.token = tok
	}
}

// WithInsecureBearerToken permits sending a bearer token over cleartext http
// (testing only). Without it, Dial rejects that combination.
func WithInsecureBearerToken() Option {
	return func(c *config) { c.insecureToken = true }
}

// do sends req with the client's credentials attached, retrying it as far as
// the client's policy allows. Every request the package makes goes through
// here, so a credential cannot be forgotten on one method and present on
// another, and no request is left un-retried by having been built elsewhere.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.send(req)
}
