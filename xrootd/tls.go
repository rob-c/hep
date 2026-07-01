// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"crypto/tls"
	"fmt"
	"net"
)

// WithTLSConfig sets the TLS configuration used when the connection is upgraded
// to in-protocol TLS (roots:// or WithTLS). A nil config uses a default that
// verifies the server certificate against the host's root CAs.
func WithTLSConfig(cfg *tls.Config) Option {
	return func(client *Client) error {
		client.tlsConfig = cfg
		return nil
	}
}

// WithTLS requests in-protocol TLS even when the address uses the cleartext
// root:// scheme. The server must support TLS (kXR_haveTLS) or the connection
// fails rather than silently downgrading.
func WithTLS() Option {
	return func(client *Client) error {
		client.wantTLS = true
		return nil
	}
}

// WithInsecureTLS requests TLS but disables server-certificate verification.
// It is intended for testing against self-signed certificates and MUST NOT be
// used in production.
func WithInsecureTLS() Option {
	return func(client *Client) error {
		client.wantTLS = true
		client.insecureTLS = true
		return nil
	}
}

// tlsConfigFor returns the effective TLS config for a session dialing serverName.
func (client *Client) tlsConfigFor(serverName string) *tls.Config {
	var cfg *tls.Config
	if client.tlsConfig != nil {
		cfg = client.tlsConfig.Clone()
	} else {
		cfg = &tls.Config{}
	}
	if cfg.ServerName == "" {
		cfg.ServerName = serverName
	}
	if client.insecureTLS {
		cfg.InsecureSkipVerify = true
	}
	return cfg
}

// upgradeTLS replaces the session's cleartext connection with a completed TLS
// client session. It must be called during bootstrap, after the protocol
// response and before login, so credentials never travel in the clear.
func (sess *cliSession) upgradeTLS() error {
	host, _, err := splitHostPortForTLS(sess.addr)
	if err != nil {
		return err
	}
	var cfg *tls.Config
	if sess.client != nil {
		cfg = sess.client.tlsConfigFor(host)
	} else {
		cfg = &tls.Config{ServerName: host}
	}
	tconn := tls.Client(sess.conn, cfg)
	if err := tconn.HandshakeContext(sess.ctx); err != nil {
		return fmt.Errorf("xrootd: TLS handshake failed: %w", err)
	}
	sess.conn = tconn
	return nil
}

// splitHostPortForTLS returns the host portion of addr for use as the TLS
// ServerName. If addr has no port, it is returned unchanged.
func splitHostPortForTLS(addr string) (host, port string, err error) {
	host, port, err = net.SplitHostPort(addr)
	if err != nil {
		return addr, "", nil
	}
	return host, port, nil
}
