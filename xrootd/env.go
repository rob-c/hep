// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// The environment variables this client honours, spelled as the C++ client
// spells them so that a process already configured for XrdCl needs no second
// configuration for this one.
const (
	// EnvUsername names the user asserted at login, when the caller passed none.
	EnvUsername = "XRD_USERNAME"
	// EnvRequireTLS asks for TLS on every connection.
	EnvRequireTLS = "XRD_REQUIRETLS"
	// EnvTLSNoCertVerify accepts any server certificate.
	EnvTLSNoCertVerify = "XRD_TLSNOCERTVERIFY"
	// EnvConnectionWindow bounds, in seconds, how long a connection may take.
	EnvConnectionWindow = "XRD_CONNECTIONWINDOW"
	// EnvRequestTimeout bounds, in seconds, how long a server may park a
	// request with kXR_wait.
	EnvRequestTimeout = "XRD_REQUESTTIMEOUT"
	// EnvRedirectLimit bounds how many redirections are followed in a row.
	EnvRedirectLimit = "XRD_REDIRECTLIMIT"
)

// WithRedirectLimit sets how many redirections in a row the client follows
// before giving up.
//
// The limit is what tells a federation that keeps sending a client round in a
// circle from one that is merely deep, and it is a policy rather than a
// protocol constant: a client with a limit of zero cannot use a manager at all.
func WithRedirectLimit(n int) Option {
	return func(client *Client) error {
		if n < 0 {
			return fmt.Errorf("xrootd: redirect limit %d is negative", n)
		}
		client.maxRedirections = n
		return nil
	}
}

// WithConnectionWindow sets how long the client waits for a connection to be
// established. A zero or negative duration leaves the wait to the operating
// system, which is where it is by default.
//
// This bounds the connection alone. A server that accepts the connection and
// then says nothing is the business of the caller's context.
func WithConnectionWindow(d time.Duration) Option {
	return func(client *Client) error {
		client.dialTimeout = max(d, 0)
		return nil
	}
}

// WithRequestTimeout sets the longest a single kXR_wait may park a request.
//
// The delay is chosen by the server, and the protocol puts no ceiling on it: a
// server that asks for a wait longer than this is answered by giving up rather
// than by holding the caller. It does not bound the request as a whole — a
// server that asks repeatedly for short waits can still take as long as the
// caller's context allows.
func WithRequestTimeout(d time.Duration) Option {
	return func(client *Client) error {
		if d <= 0 {
			return fmt.Errorf("xrootd: request timeout %v is not positive", d)
		}
		client.waitCap = d
		return nil
	}
}

// WithUsername sets the user name asserted at login, overriding the one passed
// to NewClient.
func WithUsername(name string) Option {
	return func(client *Client) error {
		client.username = name
		return nil
	}
}

// envOptions returns the configuration the environment asks for.
//
// They are applied before the caller's own options, so a program that
// configures a client explicitly is not overridden by the shell it happens to
// have been started from. A variable that is set but unreadable is an error
// rather than a silent default: an unnoticed XRD_REQUESTTIMEOUT=30s (the C
// client counts seconds, and takes no unit) is a setting the user believes is
// in force.
func envOptions() []Option {
	var opts []Option

	if name := os.Getenv(EnvUsername); name != "" {
		opts = append(opts, func(client *Client) error {
			// Only when the caller named nobody: an explicit user name is a
			// decision, and the environment does not get to overrule it.
			if client.username == "" {
				client.username = name
			}
			return nil
		})
	}
	if envBool(EnvRequireTLS) {
		opts = append(opts, WithTLS())
	}
	if envBool(EnvTLSNoCertVerify) {
		opts = append(opts, WithInsecureTLS())
	}

	opts = append(opts, envSeconds(EnvConnectionWindow, WithConnectionWindow))
	opts = append(opts, envSeconds(EnvRequestTimeout, WithRequestTimeout))

	if v, ok := os.LookupEnv(EnvRedirectLimit); ok && strings.TrimSpace(v) != "" {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			opts = append(opts, envError(EnvRedirectLimit, v))
		} else {
			opts = append(opts, WithRedirectLimit(n))
		}
	}

	return opts
}

// envBool reports whether the named variable asks for something. The spellings
// are the C++ client's, and anything else — including "0" and "false" — is a no.
func envBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// envSeconds turns a variable holding a whole number of seconds into the option
// it configures, or into the error that says why it could not.
func envSeconds(name string, opt func(time.Duration) Option) Option {
	v, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(v) == "" {
		return nil
	}
	secs, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return envError(name, v)
	}
	return opt(time.Duration(secs) * time.Second)
}

func envError(name, value string) Option {
	return func(*Client) error {
		return fmt.Errorf("xrootd: %s=%q is not a number", name, value)
	}
}
