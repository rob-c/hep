// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for kXR_redirect. A redirect is the one reply that makes the
// client open a second connection to an address the first server chose, carry
// opaque data onto the re-issued request and a token into the new login. None
// of that is observable from a single-server test, so these run two real TCP
// servers: a redirector that answers every request with kXR_redirect, and a
// target that records what actually arrived.

package xrootd

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/handshake"
	"go-hep.org/x/hep/xrootd/xrdproto/login"
	"go-hep.org/x/hep/xrootd/xrdproto/protocol"
	"go-hep.org/x/hep/xrootd/xrdproto/stat"
)

// redirBootstrap answers the handshake, kXR_protocol and kXR_login that open
// every session, and reports the token the client logged in with.
func redirBootstrap(conn net.Conn) (token string, ok bool) {
	buf := make([]byte, handshake.RequestLength)
	if _, err := readFull(conn, buf); err != nil {
		return "", false
	}
	writeBootstrapResponse(conn, xrdproto.StreamID{0, 0}, handshake.Response{
		ProtocolVersion: 0x310,
		ServerType:      xrdproto.DataServer,
	})

	hdr, _ := readBootstrapRequest(conn)
	if hdr.RequestID != protocol.RequestID {
		return "", false
	}
	writeBootstrapResponse(conn, hdr.StreamID, protocol.Response{
		BinaryProtocolVersion: 0x310,
		Flags:                 protocol.IsServer,
	})

	hdr, body := readBootstrapRequest(conn)
	if hdr.RequestID != login.RequestID {
		return "", false
	}
	var req login.Request
	_ = req.UnmarshalXrd(xrdenc.NewRBuffer(body))
	writeBootstrapResponse(conn, hdr.StreamID, login.Response{})

	return string(req.Token), true
}

// redirTarget is the server a redirect points at. It answers kXR_stat and
// records the path it was asked for and the token it was logged into with, so
// a test can tell what the client carried across the redirect.
type redirTarget struct {
	addr string

	mu     sync.Mutex
	paths  []string
	tokens []string
}

func newRedirTarget(t *testing.T) *redirTarget {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	tgt := &redirTarget{addr: ln.Addr().String()}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go tgt.serve(conn)
		}
	}()
	return tgt
}

func (tgt *redirTarget) serve(conn net.Conn) {
	defer conn.Close()

	token, ok := redirBootstrap(conn)
	if !ok {
		return
	}
	tgt.mu.Lock()
	tgt.tokens = append(tgt.tokens, token)
	tgt.mu.Unlock()

	for {
		hdr, body := readBootstrapRequest(conn)
		if body == nil {
			return
		}
		switch hdr.RequestID {
		case stat.RequestID:
			var req stat.Request
			_ = req.UnmarshalXrd(xrdenc.NewRBuffer(body))
			tgt.mu.Lock()
			tgt.paths = append(tgt.paths, req.Path)
			tgt.mu.Unlock()
			writeBootstrapResponse(conn, hdr.StreamID, &stat.DefaultResponse{
				EntryStat: xrdfs.EntryStat{HasStatInfo: true, EntrySize: 42},
			})
		default:
			writeBootstrapResponse(conn, hdr.StreamID, rawBody(nil))
		}
	}
}

func (tgt *redirTarget) seen() (paths, tokens []string) {
	tgt.mu.Lock()
	defer tgt.mu.Unlock()
	return append([]string(nil), tgt.paths...), append([]string(nil), tgt.tokens...)
}

// redirector is a server that completes the bootstrap and then answers every
// request with kXR_redirect. Its target is set after it is listening, so two
// redirectors can be pointed at each other.
type redirector struct {
	addr string

	mu   sync.Mutex
	body []byte
	n    int
}

func newRedirector(t *testing.T) *redirector {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	r := &redirector{addr: ln.Addr().String()}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go r.serve(conn)
		}
	}()
	return r
}

// pointAt aims the redirector at addr, tagging every redirect with the given
// opaque data and login token.
func (r *redirector) pointAt(t *testing.T, addr, opaque, token string) *redirector {
	t.Helper()

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("could not split %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("could not parse the port in %q: %v", addr, err)
	}

	// The redirect body is a 4-byte port followed by "host?opaque?token",
	// with the trailing fields left off when they are empty: a stray "?"
	// would otherwise be read as an empty opaque.
	body := make([]byte, 4, 64)
	binary.BigEndian.PutUint32(body, uint32(port))
	url := host
	switch {
	case token != "":
		url += "?" + opaque + "?" + token
	case opaque != "":
		url += "?" + opaque
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.body = append(body, url...)
	return r
}

func (r *redirector) serve(conn net.Conn) {
	defer conn.Close()

	if _, ok := redirBootstrap(conn); !ok {
		return
	}
	for {
		hdr, b := readBootstrapRequest(conn)
		if b == nil {
			return
		}
		r.mu.Lock()
		r.n++
		body := r.body
		r.mu.Unlock()
		if err := xrdproto.WriteResponse(conn, hdr.StreamID, xrdproto.Redirect, rawBody(body)); err != nil {
			return
		}
	}
}

func (r *redirector) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

// redirectorTo starts a redirector already aimed at addr.
func redirectorTo(t *testing.T, addr, opaque, token string) *redirector {
	t.Helper()
	return newRedirector(t).pointAt(t, addr, opaque, token)
}

func redirClient(t *testing.T, addr string) (*Client, context.Context) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	// Unbounded: a redirect here may point at a dead address, and a test that
	// means to see that reported once should not wait out five redials.
	cli, err := NewClient(ctx, addr, "gopher", Unbounded())
	if err != nil {
		t.Fatalf("could not create the client: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	return cli, ctx
}

// TestConformance_RedirectIsFollowed: the request has to be re-issued against
// the server the redirect named, and its answer — not the redirect — is what
// the caller gets back.
func TestConformance_RedirectIsFollowed(t *testing.T) {
	tgt := newRedirTarget(t)
	redir := redirectorTo(t, tgt.addr, "", "")

	cli, ctx := redirClient(t, redir.addr)
	got, err := cli.FS().Stat(ctx, "/some/file.txt")
	if err != nil {
		t.Fatalf("could not stat across the redirect: %v", err)
	}
	if got.EntrySize != 42 {
		t.Fatalf("stat came back with size %d, want 42 — the redirect target answered it", got.EntrySize)
	}
	if n := redir.count(); n != 1 {
		t.Fatalf("the redirector handed out %d redirects, want 1", n)
	}

	paths, _ := tgt.seen()
	if len(paths) != 1 || paths[0] != "/some/file.txt" {
		t.Fatalf("the target was asked for %q, want [\"/some/file.txt\"]", paths)
	}
}

// TestConformance_RedirectCarriesOpaqueData: opaque data in the redirect is
// meant for the new server, and it travels appended to the path rather than in
// a field of its own. A client that drops it looks like it works — until the
// target needs the token in it to authorize the open.
func TestConformance_RedirectCarriesOpaqueData(t *testing.T) {
	tgt := newRedirTarget(t)
	redir := redirectorTo(t, tgt.addr, "authz=abc&exp=1", "")

	cli, ctx := redirClient(t, redir.addr)
	if _, err := cli.FS().Stat(ctx, "/some/file.txt"); err != nil {
		t.Fatalf("could not stat across the redirect: %v", err)
	}

	paths, _ := tgt.seen()
	want := "/some/file.txt?authz=abc&exp=1"
	if len(paths) != 1 || paths[0] != want {
		t.Fatalf("the target was asked for %q, want [%q]", paths, want)
	}
}

// TestConformance_RedirectKeepsTheCallersOpaqueData: the redirect's opaque data
// is added to the file name, not substituted for it. A caller that opened a
// path carrying its own authorization token has to still be carrying it after
// a namespace server redirects the request, or the redirect turns a working
// open into a permission error on the data server.
func TestConformance_RedirectKeepsTheCallersOpaqueData(t *testing.T) {
	tgt := newRedirTarget(t)
	redir := redirectorTo(t, tgt.addr, "site=b", "")

	cli, ctx := redirClient(t, redir.addr)
	if _, err := cli.FS().Stat(ctx, "/some/file.txt?authz=mine"); err != nil {
		t.Fatalf("could not stat across the redirect: %v", err)
	}

	paths, _ := tgt.seen()
	want := "/some/file.txt?authz=mine&site=b"
	if len(paths) != 1 || paths[0] != want {
		t.Fatalf("the target was asked for %q, want [%q]", paths, want)
	}
}

// TestConformance_RedirectCarriesLoginToken: the token belongs to the login on
// the new connection, not to the request. It is the one part of a redirect the
// re-issued request cannot carry, so it is checked where it lands.
func TestConformance_RedirectCarriesLoginToken(t *testing.T) {
	tgt := newRedirTarget(t)
	redir := redirectorTo(t, tgt.addr, "authz=abc", "tok-1234")

	cli, ctx := redirClient(t, redir.addr)
	if _, err := cli.FS().Stat(ctx, "/some/file.txt"); err != nil {
		t.Fatalf("could not stat across the redirect: %v", err)
	}

	paths, tokens := tgt.seen()
	if len(tokens) != 1 || tokens[0] != "tok-1234" {
		t.Fatalf("the target was logged into with %q, want [\"tok-1234\"]", tokens)
	}
	if want := "/some/file.txt?authz=abc"; len(paths) != 1 || paths[0] != want {
		t.Fatalf("the target was asked for %q, want [%q]", paths, want)
	}
}

// TestConformance_RedirectLoopIsBounded: a server that redirects to itself is
// the classic way to hang a client. The chain must end with an error, and it
// must end after the client's own limit rather than the server's patience.
func TestConformance_RedirectLoopIsBounded(t *testing.T) {
	// Two redirectors pointing at each other cycle just as well as one
	// pointing at itself, and they can be aimed once both are listening.
	first, second := newRedirector(t), newRedirector(t)
	first.pointAt(t, second.addr, "", "")
	second.pointAt(t, first.addr, "", "")

	cli, ctx := redirClient(t, first.addr)
	_, err := cli.FS().Stat(ctx, "/some/file.txt")
	if err == nil {
		t.Fatal("an endless redirect chain reported success")
	}
	if !strings.Contains(err.Error(), "redirection") {
		t.Fatalf("the chain failed with %v, want a redirection-limit error", err)
	}

	// The client's own limit is what has to stop this: neither server ever
	// stops redirecting.
	if n := first.count() + second.count(); n > 32 {
		t.Fatalf("the client followed %d redirects before giving up", n)
	}
}

// TestConformance_RedirectToADeadServerIsReported: following a redirect means
// dialling an address the client did not choose, and that dial can fail. It
// must be reported rather than retried forever or reported as an empty stat.
func TestConformance_RedirectToADeadServerIsReported(t *testing.T) {
	redir := redirectorTo(t, "127.0.0.1:1", "", "")

	cli, ctx := redirClient(t, redir.addr)
	if _, err := cli.FS().Stat(ctx, "/some/file.txt"); err == nil {
		t.Fatal("a redirect to a dead server reported success")
	}
}

// TestConformance_RedirectSessionIsReused: two requests redirected to the same
// address must share one session. A client that dials again per redirect turns
// a redirecting namespace server into a connection storm.
func TestConformance_RedirectSessionIsReused(t *testing.T) {
	tgt := newRedirTarget(t)
	redir := redirectorTo(t, tgt.addr, "", "")

	cli, ctx := redirClient(t, redir.addr)
	for i := range 3 {
		if _, err := cli.FS().Stat(ctx, fmt.Sprintf("/file-%d.txt", i)); err != nil {
			t.Fatalf("could not stat across the redirect (%d): %v", i, err)
		}
	}

	paths, tokens := tgt.seen()
	if len(tokens) != 1 {
		t.Fatalf("the target saw %d logins, want 1: %q", len(tokens), tokens)
	}
	if len(paths) != 3 {
		t.Fatalf("the target saw %d stats, want 3: %q", len(paths), paths)
	}
}
