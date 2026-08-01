// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"context"

	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/protocol"
)

// Protocol obtains the protocol version number, type of the server and security information, such as:
// the security version, the security options, the security level, and the list of alterations
// needed to the specified predefined security level.
func (sess *cliSession) Protocol(ctx context.Context) (protocol.Response, error) {
	var resp protocol.Response
	err := sess.sendHere(ctx, &resp, protocol.NewRequest(sess.protocolVersion, true))
	return resp, err
}

// protocolBootstrap issues the kXR_protocol request synchronously during
// bootstrap, advertising TLS capability (and, when sess.wantTLS, requesting a
// switch to TLS). It runs before login so the caller can upgrade the
// connection to TLS before any credentials are sent. Stream ID {0, 1} is
// reserved for this exchange, mirroring the reference C client; regular
// mux-assigned streams start only once consume() is running.
func (sess *cliSession) protocolBootstrap(ctx context.Context) (protocol.Response, error) {
	var resp protocol.Response
	req := protocol.NewRequestTLS(sess.protocolVersion, true, true, sess.wantTLS)
	err := sess.bootstrapExchange(ctx, xrdproto.StreamID{0, 1}, req, &resp)
	return resp, err
}
