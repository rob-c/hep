// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/auth"
	"go-hep.org/x/hep/xrootd/xrdproto/login"
	"go-hep.org/x/hep/xrootd/xrdproto/protocol"
	"go-hep.org/x/hep/xrootd/xrdproto/rm"
	"go-hep.org/x/hep/xrootd/xrdproto/sigver"
)

// keyedAuther is a security provider that completes in one round and agrees a
// session key with the server. GSI is the only real one that does; a test needs
// one it can drive without a Diffie-Hellman exchange and a proxy certificate.
type keyedAuther struct{ key []byte }

func (*keyedAuther) Provider() string { return "fake" }

func (*keyedAuther) Request([]string) (*auth.Request, error) {
	return &auth.Request{Type: [4]byte{'f', 'a', 'k', 'e'}, Credentials: "fake\x00"}, nil
}

// SessionKey implements auth.SessionKeyer.
func (a *keyedAuther) SessionKey() []byte { return a.key }

// bootAuth answers the one authentication round keyedAuther takes.
func bootAuth(t *testing.T, conn net.Conn) {
	t.Helper()

	hdr, _ := readBootstrapRequest(conn)
	if hdr.RequestID != auth.RequestID {
		t.Errorf("the client sent request %d where kXR_auth was expected", hdr.RequestID)
		return
	}
	_, _ = conn.Write(confRespHdr(hdr.StreamID, uint16(xrdproto.Ok), 0))
}

// readRawRequest reads one request and returns it whole — header, parameters,
// length and payload — which is what a signature covers and what
// readBootstrapRequest, which starts at the parameters, cannot give back.
func readRawRequest(conn net.Conn) []byte {
	data, err := xrdproto.ReadRequest(conn)
	if err != nil {
		return nil
	}
	return data
}

func TestConformance_ASignatureIsKeyedWithTheSessionKey(t *testing.T) {
	// A signature is meant to prove that the request came from the party that
	// authenticated. Every byte it covers travels on the wire in the clear, so
	// a plain digest of them proves nothing: anyone who saw the connection can
	// recompute it and put it in front of a request of their own. The key
	// agreed during authentication is the only thing in the computation an
	// observer does not have, and the server verifies with the same key.
	key := []byte("0123456789abcdef")

	type signature struct {
		sig sigver.Request
		raw []byte // the signed request, exactly as the server read it
	}
	got := make(chan signature, 1)

	addr := bootServer(t, func(conn net.Conn) {
		bootHandshake(t, conn)
		bootProtocol(t, conn, protocol.Response{
			BinaryProtocolVersion: 0x310,
			Flags:                 protocol.IsServer,
			HasSecurityInfo:       true,
			SecurityVersion:       1,
			SecurityLevel:         xrdproto.Pedantic,
		})
		bootLogin(t, conn, login.Response{SecurityInformation: []byte("&P=fake")})
		bootAuth(t, conn)

		sigHdr, sigBody := readBootstrapRequest(conn)
		if sigHdr.RequestID != sigver.RequestID {
			t.Errorf("kXR_rm went out as request %d, unsigned", sigHdr.RequestID)
			close(got)
			return
		}
		var sig sigver.Request
		if err := sig.UnmarshalXrd(xrdenc.NewRBuffer(sigBody)); err != nil {
			t.Errorf("could not decode the signature: %v", err)
			close(got)
			return
		}
		raw := readRawRequest(conn)
		got <- signature{sig: sig, raw: raw}

		if len(raw) >= xrdproto.RequestHeaderLength {
			var hdr xrdproto.RequestHeader
			_ = hdr.UnmarshalXrd(xrdenc.NewRBuffer(raw[:xrdproto.RequestHeaderLength]))
			_, _ = conn.Write(confRespHdr(hdr.StreamID, uint16(xrdproto.Ok), 0))
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := NewClient(ctx, addr, "gopher", WithAuth(&keyedAuther{key: key}))
	if err != nil {
		t.Fatalf("could not create a client: %v", err)
	}
	defer client.Close()

	if err := client.FS().RemoveFile(ctx, "/tmp/doomed"); err != nil {
		t.Fatalf("could not remove the file: %v", err)
	}

	sent, ok := <-got
	if !ok {
		t.Fatal("no signature was sent")
	}
	if sent.sig.ID != rm.RequestID {
		t.Fatalf("the signature covers request %d, want kXR_rm (%d)", sent.sig.ID, rm.RequestID)
	}

	// What the server computes: the sequence number, then the request as it
	// read it, keyed with the session key.
	mac := hmac.New(sha256.New, key)
	var seq [8]byte
	binary.BigEndian.PutUint64(seq[:], uint64(sent.sig.SeqID))
	_, _ = mac.Write(seq[:])
	_, _ = mac.Write(sent.raw)
	want := mac.Sum(nil)

	if !bytes.Equal(sent.sig.Signature, want) {
		t.Fatalf("the server cannot verify the signature:\ngot  = %x\nwant = %x", sent.sig.Signature, want)
	}

	unkeyed := sha256.New()
	_, _ = unkeyed.Write(seq[:])
	_, _ = unkeyed.Write(sent.raw)
	if bytes.Equal(sent.sig.Signature, unkeyed.Sum(nil)) {
		t.Fatal("the signature is a plain hash of what an observer already has")
	}
}

func TestConformance_ASessionWithNoKeyWillNotPretendToSign(t *testing.T) {
	// unix, host, sss and ztn agree no secret with the server, so a session
	// they authenticated cannot produce a signature the server would accept.
	// Sending one anyway costs a round trip and comes back as a bare
	// authorization failure; refusing here says which of the two ends is
	// missing what.
	addr := bootServer(t, func(conn net.Conn) {
		bootHandshake(t, conn)
		bootProtocol(t, conn, protocol.Response{
			BinaryProtocolVersion: 0x310,
			Flags:                 protocol.IsServer,
			HasSecurityInfo:       true,
			SecurityVersion:       1,
			SecurityLevel:         xrdproto.Pedantic,
		})
		bootLogin(t, conn, login.Response{})

		// Nothing should arrive: the request is refused before it is written.
		if hdr, _ := readBootstrapRequest(conn); hdr.RequestID != 0 {
			t.Errorf("request %d was sent by a session that cannot sign it", hdr.RequestID)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := NewClient(ctx, addr, "gopher")
	if err != nil {
		t.Fatalf("could not create a client: %v", err)
	}
	defer client.Close()

	err = client.FS().RemoveFile(ctx, "/tmp/doomed")
	if err == nil {
		t.Fatal("a request that had to be signed was sent unsigned")
	}
	if !strings.Contains(err.Error(), "signing key") {
		t.Fatalf("the failure does not say what is missing: %v", err)
	}
}

func TestConformance_ARedirectIsNoAnswerAboutThisConnection(t *testing.T) {
	// kXR_login, kXR_protocol, kXR_bind and kXR_ping all ask about the
	// connection they travel on. A redirect answers a different question, and
	// the response struct is left as it was: a login whose reply never arrived
	// reads as a session with no security requirements at all, and a bind that
	// never happened reads as path 0 — the connection the bind was meant to
	// establish.
	t.Run("a redirected login stops the connection", func(t *testing.T) {
		addr := bootServer(t, func(conn net.Conn) {
			bootHandshake(t, conn)
			bootProtocol(t, conn, protocol.Response{BinaryProtocolVersion: 0x310, Flags: protocol.IsServer})

			hdr, _ := readBootstrapRequest(conn)
			_ = xrdproto.WriteResponse(conn, hdr.StreamID, xrdproto.Redirect,
				rawBody(redirectBody(t, "127.0.0.1:1094", "")))
			// Hold the connection open until the client hangs up.
			_, _ = readFull(conn, make([]byte, 1))
		})

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, err := NewClient(ctx, addr, "gopher")
		if err == nil {
			t.Fatal("a client logged in to a server that never answered the login")
		}
		if !strings.Contains(err.Error(), "redirect") {
			t.Fatalf("the failure does not say what the server answered: %v", err)
		}
	})

	t.Run("a redirected ping is not a pong", func(t *testing.T) {
		addr := bootServer(t, func(conn net.Conn) {
			bootHandshake(t, conn)
			bootProtocol(t, conn, protocol.Response{BinaryProtocolVersion: 0x310, Flags: protocol.IsServer})
			bootLogin(t, conn, login.Response{})

			hdr, _ := readBootstrapRequest(conn)
			_ = xrdproto.WriteResponse(conn, hdr.StreamID, xrdproto.Redirect,
				rawBody(redirectBody(t, "127.0.0.1:1094", "")))
			_, _ = readFull(conn, make([]byte, 1))
		})

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		client, err := NewClient(ctx, addr, "gopher")
		if err != nil {
			t.Fatalf("could not create a client: %v", err)
		}
		defer client.Close()

		if err := client.sessions[client.initialSessionID].Ping(ctx); err == nil {
			t.Fatal("a server that answered a ping with a redirect was taken as alive")
		}
	})
}
