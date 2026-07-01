// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"net"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/handshake"
	"go-hep.org/x/hep/xrootd/xrdproto/login"
	"go-hep.org/x/hep/xrootd/xrdproto/protocol"
)

// TestBootstrapProtocolBeforeLogin asserts the bootstrap wire order the TLS
// upgrade depends on: handshake, then kXR_protocol (advertising AbleTLS), then
// kXR_login. TLS must slot between protocol and login so credentials never
// travel in the clear.
func TestBootstrapProtocolBeforeLogin(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	type step struct {
		reqID uint16
	}
	seen := make(chan step, 8)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// 1) handshake: 20-byte init, reply with handshake.Response on stream {0,0}.
		buf := make([]byte, handshake.RequestLength)
		if _, err := readFull(conn, buf); err != nil {
			return
		}
		writeBootstrapResponse(conn, xrdproto.StreamID{0, 0}, handshake.Response{ProtocolVersion: 0x310, ServerType: xrdproto.DataServer})

		// 2) protocol: read request, record it, reply with a TLS-free response.
		hdr, body := readBootstrapRequest(conn)
		seen <- step{reqID: hdr.RequestID}
		var preq protocol.Request
		_ = preq.UnmarshalXrd(xrdenc.NewRBuffer(body))
		if preq.Options&protocol.AbleTLS == 0 {
			t.Errorf("protocol request did not advertise AbleTLS: options=%#x", preq.Options)
		}
		writeBootstrapResponse(conn, hdr.StreamID, protocol.Response{BinaryProtocolVersion: 0x310, Flags: protocol.IsServer})

		// 3) login: read request, record it, reply with an empty security info.
		hdr, _ = readBootstrapRequest(conn)
		seen <- step{reqID: hdr.RequestID}
		writeBootstrapResponse(conn, hdr.StreamID, login.Response{})

		// Drain until the client disconnects.
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		io := make([]byte, 256)
		for {
			if _, err := conn.Read(io); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := NewClient(ctx, ln.Addr().String(), "gopher")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	got1 := <-seen
	if got1.reqID != protocol.RequestID {
		t.Fatalf("first post-handshake request = %d, want protocol (%d)", got1.reqID, protocol.RequestID)
	}
	got2 := <-seen
	if got2.reqID != login.RequestID {
		t.Fatalf("second post-handshake request = %d, want login (%d)", got2.reqID, login.RequestID)
	}
}
