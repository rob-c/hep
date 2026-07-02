// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"context"
	"fmt"
	"time"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/handshake"
)

// handshakeBootstrap performs the initial handshake synchronously, before the
// consume() read loop starts. The 20-byte client init carries no request
// header (no stream/request ID), so it is written raw rather than through
// bootstrapExchange.
func (sess *cliSession) handshakeBootstrap(ctx context.Context) error {
	var wBuffer xrdenc.WBuffer
	if err := handshake.NewRequest().MarshalXrd(&wBuffer); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := sess.conn.SetDeadline(deadline); err != nil {
			return err
		}
		defer sess.conn.SetDeadline(time.Time{})
	}
	if _, err := sess.conn.Write(wBuffer.Bytes()); err != nil {
		return fmt.Errorf("xrootd: could not send handshake: %w", err)
	}

	var respHeader xrdproto.ResponseHeader
	headerBytes := make([]byte, xrdproto.ResponseHeaderLength)
	data, err := xrdproto.ReadResponseWithReuse(sess.conn, headerBytes, &respHeader)
	if err != nil {
		return fmt.Errorf("xrootd: could not read handshake response: %w", err)
	}
	if respHeader.Status == xrdproto.Error {
		return respHeader.Error(data)
	}

	var result handshake.Response
	if err := result.UnmarshalXrd(xrdenc.NewRBuffer(data)); err != nil {
		return err
	}
	sess.protocolVersion = result.ProtocolVersion
	return nil
}

func (sess *cliSession) handshake(ctx context.Context) error {
	streamID := xrdproto.StreamID{0, 0}
	responseChannel, err := sess.mux.ClaimWithID(streamID)
	if err != nil {
		return err
	}

	req := handshake.NewRequest()
	var wBuffer xrdenc.WBuffer
	err = req.MarshalXrd(&wBuffer)
	if err != nil {
		return err
	}

	resp, _, _, err := sess.send(ctx, streamID, responseChannel, wBuffer.Bytes(), nil, 0)
	// TODO: should we react somehow to redirection?
	if err != nil {
		return err
	}

	var result handshake.Response
	if err = result.UnmarshalXrd(xrdenc.NewRBuffer(resp)); err != nil {
		return err
	}

	sess.protocolVersion = result.ProtocolVersion

	return nil
}
