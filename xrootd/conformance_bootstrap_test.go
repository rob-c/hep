// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for everything that happens before a session is usable, and for
// each way that sequence can fail.
//
// The bootstrap is handshake, then kXR_protocol, then — if either side asked
// for it — a TLS upgrade, then kXR_login, then authentication. The order is not
// decorative: the protocol response is what says whether the server can speak
// TLS, and the upgrade has to land between it and the login, or the credentials
// go out in the clear. Every step below is made to fail on its own, because a
// bootstrap that reports success after a step it could not complete hands back a
// client that will fail later, somewhere else, for a reason that names the wrong
// thing.
//
// The servers here are scripted byte by byte: the point is to send what a real
// server sends when it is refusing, and what a broken one sends when it is
// confused.

package xrootd

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/handshake"
	"go-hep.org/x/hep/xrootd/xrdproto/login"
	"go-hep.org/x/hep/xrootd/xrdproto/protocol"
	"go-hep.org/x/hep/xrootd/xrdproto/rm"
	"go-hep.org/x/hep/xrootd/xrdproto/sigver"
)

// bootGotoTLS is kXR_gotoTLS: the server orders the client into TLS now.
const bootGotoTLS = protocol.Flags(0x40000000)

// bootServer plays script against the first connection made to a fresh
// loopback listener, and returns the address to dial.
func bootServer(t *testing.T, script func(conn net.Conn)) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}

	done := make(chan struct{})
	// Registered first, so it is the last cleanup to run: the listener is
	// closed before the script is waited on, and a test that never dials
	// unblocks instead of hanging.
	t.Cleanup(func() { <-done })
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		defer close(done)

		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		script(conn)
	}()

	return ln.Addr().String()
}

// bootHandshake reads the 20-byte client init and answers it.
func bootHandshake(t *testing.T, conn net.Conn) {
	t.Helper()

	if _, err := readFull(conn, make([]byte, handshake.RequestLength)); err != nil {
		return
	}
	writeBootstrapResponse(conn, xrdproto.StreamID{0, 0}, handshake.Response{
		ProtocolVersion: 0x310,
		ServerType:      xrdproto.DataServer,
	})
}

// bootProtocol reads the kXR_protocol request and answers it with resp.
func bootProtocol(t *testing.T, conn net.Conn, resp protocol.Response) {
	t.Helper()

	hdr, _ := readBootstrapRequest(conn)
	if hdr.RequestID != protocol.RequestID {
		t.Errorf("the client sent request %d where kXR_protocol was expected", hdr.RequestID)
		return
	}
	writeBootstrapResponse(conn, hdr.StreamID, resp)
}

// bootLogin reads the kXR_login request and answers it with resp.
func bootLogin(t *testing.T, conn net.Conn, resp login.Response) {
	t.Helper()

	hdr, _ := readBootstrapRequest(conn)
	if hdr.RequestID != login.RequestID {
		t.Errorf("the client sent request %d where kXR_login was expected", hdr.RequestID)
		return
	}
	writeBootstrapResponse(conn, hdr.StreamID, resp)
}

// bootErrorFrame writes a kXR_error response carrying msg.
func bootErrorFrame(conn net.Conn, streamID xrdproto.StreamID, msg string) {
	_ = xrdproto.WriteResponse(conn, streamID, xrdproto.Error, xrdproto.ServerError{
		Code:    xrdproto.InvalidRequest,
		Message: msg,
	})
}

// bootDial connects to addr, expecting the attempt to fail.
func bootRefused(t *testing.T, addr string, opts ...Option) error {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := NewClient(ctx, addr, "gopher", opts...)
	if err == nil {
		client.Close()
		t.Fatal("a session was built on a bootstrap that could not complete")
	}
	return err
}

func TestConformance_AHandshakeTheServerRefusesIsNotASession(t *testing.T) {
	// The handshake is what tells the client it is talking to an XRootD server
	// at all. Every failure of it has to end the attempt: a client that carried
	// on would send kXR_protocol to something that is not a server, and report
	// whatever came back as a protocol error.
	for _, tc := range []struct {
		name  string
		reply func(conn net.Conn)
		want  string
	}{
		{
			"the server hangs up",
			func(conn net.Conn) {},
			"could not read handshake response",
		},
		{
			"the server answers with an error",
			func(conn net.Conn) { bootErrorFrame(conn, xrdproto.StreamID{0, 0}, "not today") },
			"not today",
		},
		{
			"the server answers with a truncated handshake",
			func(conn net.Conn) {
				// A handshake response is two 32-bit words; three bytes
				// decodes into neither of them.
				_, _ = conn.Write(append(confRespHdr(xrdproto.StreamID{0, 0}, uint16(xrdproto.Ok), 3), 1, 2, 3))
			},
			"", // the decoder's own message, whatever it says
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			addr := bootServer(t, func(conn net.Conn) {
				if _, err := readFull(conn, make([]byte, handshake.RequestLength)); err != nil {
					return
				}
				tc.reply(conn)
			})

			err := bootRefused(t, addr)
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the failure says %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestConformance_AProtocolExchangeTheServerRefusesIsNotASession(t *testing.T) {
	// kXR_protocol is where the client learns the security level it must sign
	// at and whether TLS is available. Guessing either of those is worse than
	// failing: unsigned requests are refused one by one, and a missed
	// kXR_gotoTLS means the login goes out in the clear.
	for _, tc := range []struct {
		name  string
		reply func(conn net.Conn, streamID xrdproto.StreamID)
		want  string
	}{
		{
			"the server hangs up after the handshake",
			func(conn net.Conn, streamID xrdproto.StreamID) {},
			"could not read bootstrap response",
		},
		{
			"the server refuses to negotiate",
			func(conn net.Conn, streamID xrdproto.StreamID) { bootErrorFrame(conn, streamID, "no protocol for you") },
			"no protocol for you",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			addr := bootServer(t, func(conn net.Conn) {
				bootHandshake(t, conn)
				hdr, _ := readBootstrapRequest(conn)
				tc.reply(conn, hdr.StreamID)
			})

			err := bootRefused(t, addr)
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the failure says %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestConformance_TLSThatCannotBeHadIsRefusedRatherThanDowngraded(t *testing.T) {
	// A roots:// URL is a promise that nothing leaves the machine in the
	// clear. When the server cannot keep it, the only correct answer is to
	// fail: continuing on the cleartext socket would send the login — and the
	// credential in it — exactly where the caller said not to.
	t.Run("the server offers no TLS", func(t *testing.T) {
		addr := bootServer(t, func(conn net.Conn) {
			bootHandshake(t, conn)
			bootProtocol(t, conn, protocol.Response{BinaryProtocolVersion: 0x310, Flags: protocol.IsServer})
		})

		err := bootRefused(t, "roots://"+addr)
		if !strings.Contains(err.Error(), "TLS requested but server") {
			t.Fatalf("the failure does not say TLS was unavailable: %v", err)
		}
	})

	t.Run("the server mandates TLS it cannot speak", func(t *testing.T) {
		// kXR_gotoTLS with nothing behind it. The upgrade is attempted
		// because the server ordered it, and its failure is the session's.
		addr := bootServer(t, func(conn net.Conn) {
			bootHandshake(t, conn)
			bootProtocol(t, conn, protocol.Response{
				BinaryProtocolVersion: 0x310,
				Flags:                 protocol.IsServer | bootGotoTLS,
			})
			// Hang up rather than complete a TLS handshake.
		})

		err := bootRefused(t, addr)
		if !strings.Contains(err.Error(), "TLS handshake failed") {
			t.Fatalf("the failure does not name the TLS handshake: %v", err)
		}
	})
}

func TestConformance_ALoginTheServerRefusesIsNotASession(t *testing.T) {
	// A refused login is the ordinary answer to an unknown user or a full
	// server. It has to reach the caller as the reason the client could not be
	// created, not as a nil client with an error on the first request.
	addr := bootServer(t, func(conn net.Conn) {
		bootHandshake(t, conn)
		bootProtocol(t, conn, protocol.Response{BinaryProtocolVersion: 0x310, Flags: protocol.IsServer})
		hdr, _ := readBootstrapRequest(conn)
		bootErrorFrame(conn, hdr.StreamID, "who are you")
	})

	err := bootRefused(t, addr)
	if !strings.Contains(err.Error(), "who are you") {
		t.Fatalf("the failure lost the server's reason: %v", err)
	}
}

func TestConformance_AnAuthenticationMethodTheClientDoesNotHaveIsAnError(t *testing.T) {
	// The login response lists the security protocols the server will accept.
	// When none of them is one this client implements, the session is not
	// authenticated — and an unauthenticated session is refused on its first
	// request, far from the cause. It fails here instead, naming the protocol.
	addr := bootServer(t, func(conn net.Conn) {
		bootHandshake(t, conn)
		bootProtocol(t, conn, protocol.Response{BinaryProtocolVersion: 0x310, Flags: protocol.IsServer})
		bootLogin(t, conn, login.Response{SecurityInformation: []byte("&P=nosuchsec,v:1")})
		// Hold the connection open: the client must refuse on its own, not
		// because the socket went away underneath it.
		_, _ = readFull(conn, make([]byte, 1))
	})

	err := bootRefused(t, addr)
	if !strings.Contains(err.Error(), "nosuchsec") {
		t.Fatalf("the failure does not name the protocol that was demanded: %v", err)
	}
}

func TestConformance_ARequestTheSecurityLevelCoversGoesOutSigned(t *testing.T) {
	// The security level the server announced in kXR_protocol decides which
	// requests must carry a kXR_sigver ahead of them. kXR_rm is signed from
	// "compatible" upwards; a client that ignored the level would have every
	// destructive request refused as unsigned, on a connection that had just
	// negotiated the requirement.
	type sighdr struct {
		reqID uint16
		seqID int64
	}
	signed := make(chan sighdr, 1)

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

		// The signature travels as its own request, immediately ahead of the
		// one it covers and on the same stream.
		sigHdr, sigBody := readBootstrapRequest(conn)
		if sigHdr.RequestID != sigver.RequestID {
			t.Errorf("kXR_rm went out as request %d, unsigned", sigHdr.RequestID)
			close(signed)
			return
		}
		var sig sigver.Request
		if err := sig.UnmarshalXrd(xrdenc.NewRBuffer(sigBody)); err != nil {
			t.Errorf("could not decode the signature: %v", err)
			close(signed)
			return
		}
		signed <- sighdr{reqID: sig.ID, seqID: sig.SeqID}

		rmHdr, _ := readBootstrapRequest(conn)
		if rmHdr.RequestID != rm.RequestID {
			t.Errorf("the signed request is %d, want kXR_rm (%d)", rmHdr.RequestID, rm.RequestID)
			return
		}
		if rmHdr.StreamID != sigHdr.StreamID {
			t.Errorf("the signature went out on stream %v and the request on %v", sigHdr.StreamID, rmHdr.StreamID)
		}
		// kXR_rm answers with an empty kXR_ok.
		_, _ = conn.Write(confRespHdr(rmHdr.StreamID, uint16(xrdproto.Ok), 0))
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := NewClient(ctx, addr, "gopher")
	if err != nil {
		t.Fatalf("could not create a client: %v", err)
	}
	defer client.Close()

	_ = client.FS().RemoveFile(ctx, "/tmp/doomed")

	got, ok := <-signed
	if !ok {
		t.Fatal("no signature was sent")
	}
	if got.reqID != rm.RequestID {
		t.Fatalf("the signature covers request %d, want kXR_rm (%d)", got.reqID, rm.RequestID)
	}
	if got.seqID == 0 {
		t.Fatal("the signature carries no sequence number, so it can be replayed")
	}
}

func TestConformance_AClientIsBuiltFromItsOptionsOrNotAtAll(t *testing.T) {
	// Options are applied before anything is dialled, so a bad one costs no
	// connection — and, more to the point, never yields a half-configured
	// client that talks to a server without the credential it was given.
	ctx := context.Background()

	t.Run("an option that fails stops the client", func(t *testing.T) {
		boom := errors.New("this option cannot be satisfied")
		_, err := NewClient(ctx, "127.0.0.1:1", "gopher", func(*Client) error { return boom })
		if !errors.Is(err, boom) {
			t.Fatalf("the failure is %v, want the option's own error", err)
		}
	})

	t.Run("a nil option is not an option", func(t *testing.T) {
		// Callers build option slices conditionally; a nil hole in one is not
		// a reason to refuse to connect.
		addr := bootServer(t, func(conn net.Conn) {
			bootHandshake(t, conn)
			bootProtocol(t, conn, protocol.Response{BinaryProtocolVersion: 0x310, Flags: protocol.IsServer})
			bootLogin(t, conn, login.Response{})
			// Hold the connection open until the client closes it.
			_, _ = readFull(conn, make([]byte, 1))
		})

		client, err := NewClient(ctx, addr, "gopher", nil)
		if err != nil {
			t.Fatalf("a nil option stopped the client: %v", err)
		}
		client.Close()
	})

	t.Run("an address that is not one is refused before dialling", func(t *testing.T) {
		_, err := NewClient(ctx, "root://[::1//f.bin", "gopher")
		if err == nil {
			t.Fatal("a malformed address was accepted")
		}
	})
}

func TestConformance_ASendWithNoSessionIsAnErrorNotAPanic(t *testing.T) {
	// Both are reachable from ordinary code: a Client left nil by a failed
	// constructor, and a session ID that named a server the client has since
	// dropped. Neither may take the process down.
	var nilClient *Client
	if _, err := nilClient.Send(context.Background(), nil, &rm.Request{Path: "/tmp/x"}); err == nil {
		t.Fatal("sending through a nil client succeeded")
	}

	client := &Client{sessions: make(map[string]*cliSession)}
	_, err := client.sendSession(context.Background(), "elsewhere.example.org:1094", nil, &rm.Request{Path: "/tmp/x"})
	if err == nil {
		t.Fatal("sending to a session that does not exist succeeded")
	}
	if !strings.Contains(err.Error(), "elsewhere.example.org:1094") {
		t.Fatalf("the failure does not name the session: %v", err)
	}
}

func TestConformance_AnAddressWithoutAPortIsStillATLSServerName(t *testing.T) {
	// The TLS ServerName has to be the host alone, or verification fails
	// against a certificate that is perfectly valid for it. An address that
	// carries no port at all is passed through rather than rejected.
	for _, tc := range []struct {
		addr string
		host string
		port string
	}{
		{"xrootd.example.org:1094", "xrootd.example.org", "1094"},
		{"xrootd.example.org", "xrootd.example.org", ""},
		{"[::1]:1094", "::1", "1094"},
	} {
		t.Run(tc.addr, func(t *testing.T) {
			host, port, err := splitHostPortForTLS(tc.addr)
			if err != nil {
				t.Fatalf("could not split %q: %v", tc.addr, err)
			}
			if host != tc.host || port != tc.port {
				t.Fatalf("split %q into %q/%q, want %q/%q", tc.addr, host, port, tc.host, tc.port)
			}
		})
	}
}
