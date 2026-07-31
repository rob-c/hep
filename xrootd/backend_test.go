// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/handshake"
	"go-hep.org/x/hep/xrootd/xrdproto/login"
	"go-hep.org/x/hep/xrootd/xrdproto/protocol"
)

func TestDialUnsupportedScheme(t *testing.T) {
	for _, scheme := range []string{"s3", "ftp", "gopher"} {
		t.Run(scheme, func(t *testing.T) {
			_, err := Dial(context.Background(), scheme+"://example.org/some/path", "gopher")
			if !errors.Is(err, ErrUnsupportedScheme) {
				t.Fatalf("Dial(%q): got err=%v, want ErrUnsupportedScheme", scheme, err)
			}
		})
	}
}

// startBootstrapMockServer serves a single cleartext bootstrap (handshake,
// protocol, login) and then drains the connection. It returns the address
// to dial.
func startBootstrapMockServer(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, handshake.RequestLength)
		if _, err := readFull(conn, buf); err != nil {
			return
		}
		writeBootstrapResponse(conn, xrdproto.StreamID{0, 0}, handshake.Response{ProtocolVersion: 0x310, ServerType: xrdproto.DataServer})

		hdr, _ := readBootstrapRequest(conn)
		writeBootstrapResponse(conn, hdr.StreamID, protocol.Response{BinaryProtocolVersion: 0x310, Flags: protocol.IsServer})

		hdr, _ = readBootstrapRequest(conn)
		writeBootstrapResponse(conn, hdr.StreamID, login.Response{})

		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		drain := make([]byte, 256)
		for {
			if _, err := conn.Read(drain); err != nil {
				return
			}
		}
	}()

	return ln.Addr().String()
}

func TestDialRootScheme(t *testing.T) {
	addr := startBootstrapMockServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	be, err := Dial(ctx, "root://"+addr+"//some/path", "gopher")
	if err != nil {
		t.Fatalf("Dial(root://%s): %v", addr, err)
	}
	defer be.Close()

	if be.Client() == nil {
		t.Fatal("Backend.Client() = nil, want the underlying XRootD client")
	}
	if be.FS() == nil {
		t.Fatal("Backend.FS() = nil, want a filesystem view")
	}
}
