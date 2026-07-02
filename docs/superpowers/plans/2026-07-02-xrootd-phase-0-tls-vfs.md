# XRootD Phase 0 — TLS + VFS Foundations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add in-protocol TLS (`roots://`/`xroots://`) to go-hep's pure-Go XRootD client and introduce a protocol-neutral `Backend`/`Dial` VFS layer that later phases (copy engine, HTTP/S3/WebDAV, expanded auth) build on.

**Architecture:** TLS is negotiated exactly as the reference `libxrdc` C client does it: the client sends `kXR_protocol` (advertising `kXR_ableTLS`, and `kXR_wantTLS` for `roots://`) immediately after the initial handshake and *before* `kXR_login`; if the server replies with `kXR_gotoTLS` (or the client wanted TLS and the server has it via `kXR_haveTLS`), the live TCP socket is upgraded to a `crypto/tls` session before login/auth/data flow over it. This requires reordering go-hep's current bootstrap (handshake → login → auth → protocol) so the protocol exchange and the optional TLS upgrade both happen synchronously, before the `consume()` read-loop goroutine takes ownership of the socket. A new `Backend` interface plus a scheme-aware `Dial` function give phases 2–4 a single, protocol-neutral entry point.

**Tech Stack:** Go 1.24, standard library only for TLS (`crypto/tls`, `crypto/x509`) — **no cgo, no OpenSSL**. Existing packages: `go-hep.org/x/hep/xrootd`, `.../xrootd/xrdio`, `.../xrootd/xrdproto`, `.../xrootd/xrdproto/protocol`, `.../xrootd/internal/mux`, `.../xrootd/internal/xrdenc`.

## Global Constraints

- Module path: `go-hep.org/x/hep`. Target package import path comments must be preserved (e.g. `package xrootd // import "go-hep.org/x/hep/xrootd"`).
- Go version floor: `go 1.24.0` (from `go.mod`). Do not add third-party dependencies.
- TLS implementation is **pure Go** (`crypto/tls`). No cgo, no OpenSSL bindings.
- New source files begin with the license header:
  ```go
  // Copyright ©2026 The go-hep Authors. All rights reserved.
  // Use of this source code is governed by a BSD-style
  // license that can be found in the LICENSE file.
  ```
- Every exported identifier gets a complete Go doc comment starting with its name. Package doc comments explain purpose. Comments state non-obvious protocol constraints (e.g. why a TLS flag lives in the high bit), not narration.
- All I/O takes a `context.Context`; wrap errors with `%w` and the `xrootd:` prefix used elsewhere in the package.
- Exact XRootD protocol constants (verified against `/usr/include/xrootd/XProtocol/XProtocol.hh`, the same values `libxrdc` compiles against):
  - Protocol request Options byte: `kXR_secreqs=0x01`, `kXR_ableTLS=0x02`, `kXR_wantTLS=0x04`.
  - Protocol response flags (32-bit): `kXR_haveTLS=0x80000000`, `kXR_gotoTLS=0x40000000`, `kXR_tlsData=0x01000000`, `kXR_tlsGPF=0x02000000`, `kXR_tlsLogin=0x04000000`, `kXR_tlsSess=0x08000000`, `kXR_tlsTPC=0x10000000`, `kXR_tlsGPFA=0x20000000`, `kXR_tlsAny=0x1f000000` (mask).
- After each task: `gofmt -l` reports no files, `go vet ./xrootd/...` is clean, and `go test ./xrootd/...` passes.
- Verification bar for the whole phase: parity is checked against two oracles — the official XRootD server (interop tests) and `libxrdc` (behavioral equivalence). Interop tests skip cleanly when no server is configured; they must not fail on developer machines that lack a server.

---

### Task 1: URL scheme parsing (`xrdio`)

Teach `xrdio.URL` about the URL scheme so the router in Task 6 can decide TLS from `root://` vs `roots://`. Today `Parse` discards the scheme entirely.

**Files:**
- Modify: `xrootd/xrdio/parse.go`
- Test: `xrootd/xrdio/parse_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `xrdio.URL` gains field `Scheme string` (lower-cased scheme without `://`; empty when the input had no scheme, i.e. a bare local path).
  - `func (u URL) TLS() bool` — reports whether the scheme mandates in-protocol TLS (`roots`, `xroots`).

- [x] **Step 1: Write the failing test**

Add to `xrootd/xrdio/parse_test.go` (inside the existing test file; add the import of `reflect` only if absent):

```go
func TestParseScheme(t *testing.T) {
	for _, tc := range []struct {
		name   string
		scheme string
		tls    bool
	}{
		{name: "root://example.org//file.root", scheme: "root", tls: false},
		{name: "xroot://example.org//file.root", scheme: "xroot", tls: false},
		{name: "roots://example.org//file.root", scheme: "roots", tls: true},
		{name: "xroots://example.org//file.root", scheme: "xroots", tls: true},
		{name: "/tmp/local/file.root", scheme: "", tls: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.name)
			if err != nil {
				t.Fatalf("could not parse %q: %v", tc.name, err)
			}
			if got.Scheme != tc.scheme {
				t.Fatalf("scheme mismatch for %q: got=%q want=%q", tc.name, got.Scheme, tc.scheme)
			}
			if got.TLS() != tc.tls {
				t.Fatalf("TLS() mismatch for %q: got=%v want=%v", tc.name, got.TLS(), tc.tls)
			}
		})
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./xrootd/xrdio/ -run TestParseScheme -v`
Expected: FAIL — `got.Scheme undefined (type URL has no field or method Scheme)`.

- [x] **Step 3: Add the `Scheme` field and `TLS` method, and populate the scheme in `Parse`**

In `xrootd/xrdio/parse.go`, extend the struct:

```go
// URL stores an absolute reference to a XRootD path.
type URL struct {
	Scheme string // URL scheme without "://" (e.g. "root", "roots"); empty for a bare local path
	Addr   string // address (host [:port]) of the server
	User   string // user name to use to log in
	Path   string // path to the remote file or directory
}

// TLS reports whether the URL scheme mandates in-protocol TLS.
// The secure schemes are "roots" and "xroots".
func (u URL) TLS() bool {
	switch u.Scheme {
	case "roots", "xroots":
		return true
	default:
		return false
	}
}
```

In `Parse`, capture the scheme and set it on the returned value:

```go
func Parse(name string) (URL, error) {
	var (
		scheme string
		user   string
		addr   string
		path   string
		err    error
	)

	idx := strings.Index(name, "://")
	switch idx {
	case -1:
		path = name
	default:
		scheme = strings.ToLower(name[:idx])
		uri := name[idx+len("://"):]
		tok := strings.SplitN(uri, "/", 2)
		user, addr, err = parseUA(tok[0])
		if err != nil {
			return URL{}, fmt.Errorf("could not parse URI %q: %w", name, err)
		}
		path = "/" + tok[1]
	}

	if strings.HasPrefix(path, "//") {
		path = path[1:]
	}

	return URL{Scheme: scheme, Addr: addr, User: user, Path: path}, nil
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./xrootd/xrdio/ -run TestParseScheme -v`
Expected: PASS. Also run `go test ./xrootd/xrdio/` — all existing parse tests still pass (they compare only `Addr`/`User`/`Path` or construct `URL` with those fields; `Scheme` defaults to `""`, so struct-literal comparisons that omit it still match).

> If any existing test builds a `URL{...}` literal and compares with `reflect.DeepEqual`, update that literal to include the expected `Scheme` value. Check `parse_test.go` for such cases and fix them in this step.

- [x] **Step 5: Commit**

```bash
gofmt -w xrootd/xrdio/parse.go xrootd/xrdio/parse_test.go
git add xrootd/xrdio/parse.go xrootd/xrdio/parse_test.go
git commit -m "xrootd/xrdio: parse and expose URL scheme (root/roots/xroot/xroots)"
```

---

### Task 2: Protocol TLS request options and response flags

Add the TLS negotiation constants and accessors to `xrdproto/protocol`, plus a constructor that advertises TLS capability. The response `Flags` field is wire-encoded as a signed `int32`, but `kXR_haveTLS` (`0x80000000`) and `kXR_gotoTLS` (`0x40000000`) occupy the high bits — the accessors must do bit tests through `uint32` to avoid sign issues.

**Files:**
- Modify: `xrootd/xrdproto/protocol/protocol.go`
- Test: `xrootd/xrdproto/protocol/protocol_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces (all in package `protocol`):
  - Request option constants: `AbleTLS RequestOptions = 0x02`, `WantTLS RequestOptions = 0x04` (the existing `ReturnSecurityRequirements = 1` already equals `kXR_secreqs`).
  - `func NewRequestTLS(protocolVersion int32, withSecurityRequirements, ableTLS, wantTLS bool) *Request`.
  - Response accessors: `func (resp *Response) HasTLS() bool`, `GotoTLS() bool`, `TLSForData() bool`, `TLSForLogin() bool`, `TLSForSession() bool`, `TLSForTPC() bool`.
  - `func (resp *Response) NeedsTLS(wantTLS bool) bool` — true when the client must upgrade before login: `GotoTLS() || (wantTLS && HasTLS())`.

- [x] **Step 1: Write the failing test**

Add to `xrootd/xrdproto/protocol/protocol_test.go`:

```go
func TestNewRequestTLSOptions(t *testing.T) {
	req := NewRequestTLS(0x310, true, true, true)
	want := ReturnSecurityRequirements | AbleTLS | WantTLS
	if req.Options != want {
		t.Fatalf("options mismatch: got=%#x want=%#x", req.Options, want)
	}

	req = NewRequestTLS(0x310, true, true, false)
	want = ReturnSecurityRequirements | AbleTLS
	if req.Options != want {
		t.Fatalf("options mismatch (no wantTLS): got=%#x want=%#x", req.Options, want)
	}
}

func TestResponseTLSAccessors(t *testing.T) {
	const (
		kXRhaveTLS  = 0x80000000
		kXRgotoTLS  = 0x40000000
		kXRtlsLogin = 0x04000000
		kXRtlsData  = 0x01000000
	)
	resp := &Response{Flags: Flags(int32(kXRhaveTLS | kXRgotoTLS | kXRtlsLogin | kXRtlsData))}

	if !resp.HasTLS() {
		t.Fatal("HasTLS() = false, want true")
	}
	if !resp.GotoTLS() {
		t.Fatal("GotoTLS() = false, want true")
	}
	if !resp.TLSForLogin() {
		t.Fatal("TLSForLogin() = false, want true")
	}
	if !resp.TLSForData() {
		t.Fatal("TLSForData() = false, want true")
	}
	if resp.TLSForTPC() {
		t.Fatal("TLSForTPC() = true, want false")
	}

	// NeedsTLS: gotoTLS forces upgrade regardless of client preference.
	if !resp.NeedsTLS(false) {
		t.Fatal("NeedsTLS(false) = false with gotoTLS set, want true")
	}

	// Server that only advertises haveTLS (no gotoTLS): upgrade only if the client wanted TLS.
	only := &Response{Flags: Flags(int32(kXRhaveTLS))}
	if only.NeedsTLS(false) {
		t.Fatal("NeedsTLS(false) = true with only haveTLS, want false")
	}
	if !only.NeedsTLS(true) {
		t.Fatal("NeedsTLS(true) = false with haveTLS, want true")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./xrootd/xrdproto/protocol/ -run 'TestNewRequestTLSOptions|TestResponseTLSAccessors' -v`
Expected: FAIL — `undefined: NewRequestTLS`, `undefined: AbleTLS`, etc.

- [x] **Step 3: Add the constants, constructor, and accessors**

In `xrootd/xrdproto/protocol/protocol.go`, extend the `RequestOptions` block:

```go
const (
	// RequestOptionsNone specifies that only general response should be returned.
	RequestOptionsNone RequestOptions = 0
	// ReturnSecurityRequirements specifies that security requirements should be returned
	// if that's supported by the server. Wire value kXR_secreqs.
	ReturnSecurityRequirements RequestOptions = 0x01
	// AbleTLS advertises that the client is capable of in-protocol TLS. Wire value kXR_ableTLS.
	AbleTLS RequestOptions = 0x02
	// WantTLS requests that the connection be switched to TLS. Wire value kXR_wantTLS.
	WantTLS RequestOptions = 0x04
)
```

Add TLS response-flag constants near the `Flags` type. They are declared as untyped
constants (not `Flags`) because `kXR_haveTLS` overflows the signed `Flags`/`int32` range;
the accessors below apply them through `uint32`:

```go
// TLS-related bits carried in the protocol response Flags word.
// See http://xrootd.org/doc/dev45/XRdv310.pdf and XProtocol.hh.
const (
	flagHaveTLS  uint32 = 0x80000000 // server supports in-protocol TLS
	flagGotoTLS  uint32 = 0x40000000 // server requires switching to TLS now
	flagTLSData  uint32 = 0x01000000 // TLS required for the data stream
	flagTLSGPF   uint32 = 0x02000000 // TLS required for gpfile
	flagTLSLogin uint32 = 0x04000000 // TLS required for login
	flagTLSSess  uint32 = 0x08000000 // TLS required for the session
	flagTLSTPC   uint32 = 0x10000000 // TLS required for third-party copy
	flagTLSGPFA  uint32 = 0x20000000 // TLS required for anonymous gpfile
)
```

Add the constructor:

```go
// NewRequestTLS forms a protocol Request that advertises TLS capability.
//
// withSecurityRequirements requests the security-requirements trailer (needed to
// learn the signing level). ableTLS advertises that the client can speak TLS;
// wantTLS additionally asks the server to switch the connection to TLS (set for
// roots:// URLs). A server may still mandate TLS via kXR_gotoTLS even when
// wantTLS is false.
func NewRequestTLS(protocolVersion int32, withSecurityRequirements, ableTLS, wantTLS bool) *Request {
	var options = RequestOptionsNone
	if withSecurityRequirements {
		options |= ReturnSecurityRequirements
	}
	if ableTLS {
		options |= AbleTLS
	}
	if wantTLS {
		options |= WantTLS
	}
	return &Request{ClientProtocolVersion: protocolVersion, Options: options}
}
```

Add the accessors (place them with the other `Response` methods):

```go
func (resp *Response) flagBits() uint32 { return uint32(resp.Flags) }

// HasTLS reports whether the server supports in-protocol TLS (kXR_haveTLS).
func (resp *Response) HasTLS() bool { return resp.flagBits()&flagHaveTLS != 0 }

// GotoTLS reports whether the server requires switching to TLS now (kXR_gotoTLS).
func (resp *Response) GotoTLS() bool { return resp.flagBits()&flagGotoTLS != 0 }

// TLSForData reports whether TLS is required for the data stream (kXR_tlsData).
func (resp *Response) TLSForData() bool { return resp.flagBits()&flagTLSData != 0 }

// TLSForLogin reports whether TLS is required for login (kXR_tlsLogin).
func (resp *Response) TLSForLogin() bool { return resp.flagBits()&flagTLSLogin != 0 }

// TLSForSession reports whether TLS is required for the whole session (kXR_tlsSess).
func (resp *Response) TLSForSession() bool { return resp.flagBits()&flagTLSSess != 0 }

// TLSForTPC reports whether TLS is required for third-party copy (kXR_tlsTPC).
func (resp *Response) TLSForTPC() bool { return resp.flagBits()&flagTLSTPC != 0 }

// NeedsTLS reports whether the client must upgrade the connection to TLS before
// login. This is true when the server mandates it (kXR_gotoTLS) or when the
// client wanted TLS and the server supports it (kXR_haveTLS).
func (resp *Response) NeedsTLS(wantTLS bool) bool {
	return resp.GotoTLS() || (wantTLS && resp.HasTLS())
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./xrootd/xrdproto/protocol/ -run 'TestNewRequestTLSOptions|TestResponseTLSAccessors' -v`
Expected: PASS. Run the whole package: `go test ./xrootd/xrdproto/protocol/` — existing marshal tests still pass (the `Flags` wire encoding is unchanged; the new constants are untyped `uint32` and don't alter `MarshalXrd`).

- [x] **Step 5: Commit**

```bash
gofmt -w xrootd/xrdproto/protocol/protocol.go xrootd/xrdproto/protocol/protocol_test.go
git add xrootd/xrdproto/protocol/protocol.go xrootd/xrdproto/protocol/protocol_test.go
git commit -m "xrootd/xrdproto/protocol: add TLS negotiation options, flags, accessors"
```

---

### Task 3: Synchronous bootstrap — move protocol before login

Reorder session bootstrap so the `kXR_protocol` exchange happens synchronously right after the handshake and *before* `Login`, and so both run before the `consume()` read-loop goroutine takes ownership of the socket. This is the structural prerequisite for slotting the TLS upgrade (Task 4) between protocol and login. Behavior for cleartext `root://` must be unchanged end-to-end.

**Files:**
- Modify: `xrootd/session.go`
- Modify: `xrootd/handshake.go`
- Modify: `xrootd/protocol.go`
- Test: `xrootd/session_bootstrap_test.go` (create)

**Interfaces:**
- Consumes: `protocol.NewRequestTLS` (Task 2), `xrdproto.ReadResponseWithReuse`, `handshake.NewRequest`/`handshake.Response`, `protocol.Request`/`protocol.Response`.
- Produces (unexported, in package `xrootd`):
  - `func (sess *cliSession) bootstrapExchange(ctx context.Context, streamID xrdproto.StreamID, req xrdproto.Request, resp xrdproto.Response) error` — writes one request and synchronously reads exactly one response frame directly off `sess.conn`, without using the mux or `consume()`. Used only during bootstrap, before `go sess.consume()`.
  - `sess.wantTLS bool` and `sess.protocolInfo protocol.Response` fields on `cliSession`.

- [x] **Step 1: Write the failing test**

Create `xrootd/session_bootstrap_test.go`. It stands up a raw TCP server that speaks the bootstrap by hand and asserts the client sends handshake, then a `kXR_protocol` request advertising `AbleTLS`, then `kXR_login` — in that order.

```go
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
```

Add a small test-only helpers file `xrootd/bootstrap_testutil_test.go` with `readFull`, `writeBootstrapResponse`, and `readBootstrapRequest`:

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"io"
	"net"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

func readFull(r io.Reader, p []byte) (int, error) { return io.ReadFull(r, p) }

// readBootstrapRequest reads one request (header + body) synchronously.
func readBootstrapRequest(conn net.Conn) (xrdproto.RequestHeader, []byte) {
	data, err := xrdproto.ReadRequest(conn)
	if err != nil {
		return xrdproto.RequestHeader{}, nil
	}
	var hdr xrdproto.RequestHeader
	rBuf := xrdenc.NewRBuffer(data[:xrdproto.RequestHeaderLength])
	_ = hdr.UnmarshalXrd(rBuf)
	return hdr, data[xrdproto.RequestHeaderLength:]
}

// writeBootstrapResponse marshals resp and writes it as an Ok response on streamID.
func writeBootstrapResponse(conn net.Conn, streamID xrdproto.StreamID, resp xrdproto.Marshaler) {
	_ = xrdproto.WriteResponse(conn, streamID, xrdproto.Ok, resp)
}
```

> Before writing these helpers, confirm the exact names/signatures of `xrdproto.ReadRequest`, `xrdproto.RequestHeaderLength`, `xrdproto.WriteResponse`, and `xrdproto.RequestHeader.UnmarshalXrd` by reading `xrootd/xrdproto/xrdproto.go`. Adjust the helper bodies to match (the header-length constant and `WriteResponse` signature are used elsewhere in `*_mock_test.go` — mirror those call sites).

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./xrootd/ -run TestBootstrapProtocolBeforeLogin -v`
Expected: FAIL — the current bootstrap sends `login` before `protocol`, so the first recorded `reqID` is `login.RequestID`, not `protocol.RequestID`. (Compilation may also fail until `bootstrapExchange` exists; that is fine — it still counts as red.)

- [x] **Step 3: Add `bootstrapExchange` and reorder `newSession`**

In `xrootd/session.go`, add the synchronous exchange helper:

```go
// bootstrapExchange writes a single request and synchronously reads exactly one
// response frame directly off sess.conn. It is used only during session
// bootstrap (handshake, protocol, and — for TLS — the pre-login exchanges),
// before the consume() read-loop goroutine takes ownership of the socket.
func (sess *cliSession) bootstrapExchange(ctx context.Context, streamID xrdproto.StreamID, req xrdproto.Request, resp xrdproto.Response) error {
	var wBuffer xrdenc.WBuffer
	header := xrdproto.RequestHeader{StreamID: streamID, RequestID: req.ReqID()}
	if err := header.MarshalXrd(&wBuffer); err != nil {
		return err
	}
	if err := req.MarshalXrd(&wBuffer); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := sess.conn.SetDeadline(deadline); err != nil {
			return err
		}
		defer sess.conn.SetDeadline(time.Time{})
	}
	if _, err := sess.conn.Write(wBuffer.Bytes()); err != nil {
		return fmt.Errorf("xrootd: could not send bootstrap request: %w", err)
	}

	var respHeader xrdproto.ResponseHeader
	headerBytes := make([]byte, xrdproto.ResponseHeaderLength)
	data, err := xrdproto.ReadResponseWithReuse(sess.conn, headerBytes, &respHeader)
	if err != nil {
		return fmt.Errorf("xrootd: could not read bootstrap response: %w", err)
	}
	if respHeader.Status == xrdproto.Error {
		return respHeader.Error(data)
	}
	if resp == nil {
		return nil
	}
	return resp.UnmarshalXrd(xrdenc.NewRBuffer(data))
}
```

Add the two fields to the `cliSession` struct:

```go
	wantTLS      bool             // client requested TLS (roots:// or WithTLS)
	protocolInfo protocol.Response // cached kXR_protocol response from bootstrap
```

Rework `newSession` so handshake + protocol run synchronously before `go sess.consume()`, and login/auth run after. Replace the body from the `go sess.consume()` line through the end of the function:

```go
	// Bootstrap runs synchronously so that consume() does not race the socket
	// during the handshake, protocol, and (Task 4) TLS upgrade.
	if err := sess.handshakeBootstrap(ctx); err != nil {
		sess.Close()
		return nil, err
	}

	protocolInfo, err := sess.protocolBootstrap(ctx)
	if err != nil {
		sess.Close()
		return nil, err
	}
	sess.protocolInfo = protocolInfo
	sess.signRequirements = signing.New(protocolInfo.SecurityLevel, protocolInfo.SecurityOverrides)

	// Task 4 inserts the TLS upgrade decision here, before consume() and login.

	go sess.consume()

	securityInfo, err := sess.Login(ctx, username, token)
	if err != nil {
		sess.Close()
		return nil, err
	}
	sess.loginID = securityInfo.SessionID

	if len(securityInfo.SecurityInformation) > 0 {
		if err := sess.auth(ctx, securityInfo.SecurityInformation); err != nil {
			sess.Close()
			return nil, err
		}
	}

	return sess, nil
```

> Note the `consume()` call moved to *after* handshake + protocol. Remove the old `go sess.consume()` that ran before handshake, and remove the old trailing `sess.Protocol(ctx)` call and its `signRequirements` assignment (now done in bootstrap).

In `xrootd/handshake.go`, add a synchronous bootstrap handshake alongside (or replacing) the mux-based one. The mux-based `handshake` is no longer used by `newSession` but may still be used by `newSubSession`; keep it and add:

```go
// handshakeBootstrap performs the initial handshake synchronously, before the
// consume() read loop starts. See cliSession.bootstrapExchange.
func (sess *cliSession) handshakeBootstrap(ctx context.Context) error {
	var result handshake.Response
	err := sess.bootstrapExchange(ctx, xrdproto.StreamID{0, 0}, handshake.NewRequest(), &result)
	if err != nil {
		return err
	}
	sess.protocolVersion = result.ProtocolVersion
	return nil
}
```

> `handshake.NewRequest()` returns a `handshake.Request` (value type) that implements `MarshalXrd` but check it also satisfies `xrdproto.Request` (needs `ReqID()`). The handshake has no request ID in the usual sense; the mux path used `sess.send` with raw bytes rather than `req.ReqID()`. If `handshake.Request` does **not** implement `ReqID()`, do not route it through `bootstrapExchange` (which writes a `RequestHeader`). Instead write the 20 raw bytes directly and read one response. Implement `handshakeBootstrap` as:
> ```go
> func (sess *cliSession) handshakeBootstrap(ctx context.Context) error {
> 	var wBuffer xrdenc.WBuffer
> 	if err := handshake.NewRequest().MarshalXrd(&wBuffer); err != nil {
> 		return err
> 	}
> 	if deadline, ok := ctx.Deadline(); ok {
> 		_ = sess.conn.SetDeadline(deadline)
> 		defer sess.conn.SetDeadline(time.Time{})
> 	}
> 	if _, err := sess.conn.Write(wBuffer.Bytes()); err != nil {
> 		return fmt.Errorf("xrootd: could not send handshake: %w", err)
> 	}
> 	var respHeader xrdproto.ResponseHeader
> 	headerBytes := make([]byte, xrdproto.ResponseHeaderLength)
> 	data, err := xrdproto.ReadResponseWithReuse(sess.conn, headerBytes, &respHeader)
> 	if err != nil {
> 		return fmt.Errorf("xrootd: could not read handshake response: %w", err)
> 	}
> 	var result handshake.Response
> 	if err := result.UnmarshalXrd(xrdenc.NewRBuffer(data)); err != nil {
> 		return err
> 	}
> 	sess.protocolVersion = result.ProtocolVersion
> 	return nil
> }
> ```
> Choose whichever matches the actual `handshake.Request` type. Verify by reading `xrootd/xrdproto/handshake/handshake.go` (already known: `Request [5]int32` with `MarshalXrd`, and there is **no** `ReqID()` — so use the raw-bytes form above).

In `xrootd/protocol.go`, add the synchronous protocol bootstrap that advertises TLS according to `sess.wantTLS`:

```go
// protocolBootstrap issues the kXR_protocol request synchronously during
// bootstrap, advertising TLS capability (and, when sess.wantTLS, requesting a
// switch to TLS). It runs before login so the caller can upgrade the connection
// to TLS before any credentials are sent.
func (sess *cliSession) protocolBootstrap(ctx context.Context) (protocol.Response, error) {
	var resp protocol.Response
	req := protocol.NewRequestTLS(sess.protocolVersion, true, true, sess.wantTLS)
	err := sess.bootstrapExchange(ctx, xrdproto.StreamID{0, 1}, req, &resp)
	return resp, err
}
```

> Use stream ID `{0, 1}` for the protocol bootstrap (the C client reserves streamid 1 for it; `{0,0}` is the handshake). Confirm `xrdproto.StreamID` is a `[2]byte` (it is, per `session.go`).
>
> Add imports as needed: `time` and `fmt` are already imported in `session.go`; `xrootd/protocol.go` needs `xrdproto`, `xrdenc`, and `protocol` (already imports `protocol`); add the others.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./xrootd/ -run TestBootstrapProtocolBeforeLogin -v`
Expected: PASS.

Then run the **full** package to prove the reorder didn't regress cleartext behavior:
Run: `go test ./xrootd/...`
Expected: PASS. Pay special attention to `TestSession_Protocol_WithSecurityInfo` and any login/auth mock tests — they exercise the mux-based `Protocol`/`Login`; the mux-based `Protocol` method may now be unused by `newSession` but is still referenced by that test, so keep it.

> If the mux-based `Protocol` method becomes entirely unused (vet/staticcheck flags it), keep it only if a test uses it; otherwise the test should call `protocolBootstrap`. Decide based on what the build reports; do not delete code a test depends on.

- [x] **Step 5: Commit**

```bash
gofmt -w xrootd/session.go xrootd/handshake.go xrootd/protocol.go xrootd/session_bootstrap_test.go xrootd/bootstrap_testutil_test.go
go vet ./xrootd/...
git add xrootd/session.go xrootd/handshake.go xrootd/protocol.go xrootd/session_bootstrap_test.go xrootd/bootstrap_testutil_test.go
git commit -m "xrootd: run protocol negotiation synchronously before login"
```

---

### Task 4: In-protocol TLS upgrade

Wrap the live TCP socket in a `crypto/tls` client session between the protocol response and login, exactly when `protocol.Response.NeedsTLS(sess.wantTLS)` is true. Add client options to carry a `*tls.Config`, force TLS, or relax verification, and thread `wantTLS`/config from `Client` into each `cliSession`.

**Files:**
- Create: `xrootd/tls.go`
- Modify: `xrootd/client.go`
- Modify: `xrootd/session.go`
- Test: `xrootd/tls_test.go` (create)

**Interfaces:**
- Consumes: `protocol.Response.NeedsTLS` (Task 2); `sess.wantTLS`, `sess.protocolInfo` (Task 3).
- Produces:
  - `func WithTLSConfig(cfg *tls.Config) Option` — supply a custom TLS config.
  - `func WithTLS() Option` — request TLS even for a `root://` address (sets wantTLS).
  - `func WithInsecureTLS() Option` — skip server-certificate verification (test/dev only; documented as insecure).
  - `func (sess *cliSession) upgradeTLS() error` — replace `sess.conn` with a completed `*tls.Conn`.
  - `Client` fields: `tlsConfig *tls.Config`, `wantTLS bool`, `insecureTLS bool`.

- [x] **Step 1: Write the failing test**

Create `xrootd/tls_test.go`. It runs a bootstrap server that, unlike Task 3, sets `kXR_gotoTLS` in the protocol response and then completes a TLS handshake using a self-signed cert; the client must upgrade and send login *inside* TLS. Verification is relaxed with `WithInsecureTLS`.

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"crypto/tls"
	"net"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/handshake"
	"go-hep.org/x/hep/xrootd/xrdproto/login"
	"go-hep.org/x/hep/xrootd/xrdproto/protocol"
)

func TestTLSUpgradeOnGotoTLS(t *testing.T) {
	cert := selfSignedCert(t) // helper below

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	loginInTLS := make(chan bool, 1)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Cleartext: handshake + protocol(gotoTLS).
		buf := make([]byte, handshake.RequestLength)
		if _, err := readFull(conn, buf); err != nil {
			return
		}
		writeBootstrapResponse(conn, xrdproto.StreamID{0, 0}, handshake.Response{ProtocolVersion: 0x310, ServerType: xrdproto.DataServer})

		hdr, _ := readBootstrapRequest(conn)
		const kXRgotoTLS = 0x40000000
		writeBootstrapResponse(conn, hdr.StreamID, protocol.Response{
			BinaryProtocolVersion: 0x310,
			Flags:                 protocol.IsServer | protocol.Flags(int32(kXRgotoTLS)),
		})

		// Upgrade the server side to TLS, then expect login inside TLS.
		tconn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
		if err := tconn.Handshake(); err != nil {
			loginInTLS <- false
			return
		}
		hdr, _ = readBootstrapReqTLS(tconn) // reads via the tls.Conn
		loginInTLS <- (hdr.RequestID == login.RequestID)
		writeBootstrapResponseW(tconn, hdr.StreamID, login.Response{})
		_ = tconn.SetReadDeadline(time.Now().Add(time.Second))
		io := make([]byte, 256)
		for {
			if _, err := tconn.Read(io); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := NewClient(ctx, ln.Addr().String(), "gopher", WithInsecureTLS())
	if err != nil {
		t.Fatalf("NewClient with TLS upgrade: %v", err)
	}
	defer client.Close()

	if ok := <-loginInTLS; !ok {
		t.Fatal("login was not received inside the TLS session")
	}
}
```

Add TLS test helpers to `xrootd/tls_test.go` (`selfSignedCert`, `readBootstrapReqTLS`, `writeBootstrapResponseW`). `readBootstrapReqTLS`/`writeBootstrapResponseW` are identical in body to the plain helpers but take an `io.ReadWriter`/`net.Conn` interface so they work over `*tls.Conn`:

```go
func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	// Generate an in-memory ECDSA self-signed cert valid for localhost.
	// (Use crypto/ecdsa, crypto/x509, crypto/x509/pkix, math/big, encoding/pem.)
	// Return tls.X509KeyPair(certPEM, keyPEM).
	// See the standard library "generate_cert.go" example for the exact recipe.
	// ... implement fully; no placeholder in the committed file ...
	panic("implement selfSignedCert with an in-memory ECDSA cert")
}

func readBootstrapReqTLS(conn net.Conn) (xrdproto.RequestHeader, []byte) {
	data, err := xrdproto.ReadRequest(conn)
	if err != nil {
		return xrdproto.RequestHeader{}, nil
	}
	var hdr xrdproto.RequestHeader
	_ = hdr.UnmarshalXrd(xrdenc.NewRBuffer(data[:xrdproto.RequestHeaderLength]))
	return hdr, data[xrdproto.RequestHeaderLength:]
}

func writeBootstrapResponseW(conn net.Conn, streamID xrdproto.StreamID, resp xrdproto.Marshaler) {
	_ = xrdproto.WriteResponse(conn, streamID, xrdproto.Ok, resp)
}
```

> `selfSignedCert` MUST be fully implemented (no `panic` placeholder) in the committed file. Implement it with `ecdsa.GenerateKey(elliptic.P256(), rand.Reader)`, an `x509.Certificate` template with `DNSNames: []string{"localhost"}` and `IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}`, `x509.CreateCertificate`, then `tls.X509KeyPair` over the PEM-encoded cert+key. The standard library's `crypto/tls` example `generate_cert.go` is the reference recipe.

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./xrootd/ -run TestTLSUpgradeOnGotoTLS -v`
Expected: FAIL — `undefined: WithInsecureTLS` (and the client never upgrades, so the server's `loginInTLS` receives `false` or the TLS handshake times out).

- [x] **Step 3: Implement the TLS upgrade and options**

Create `xrootd/tls.go`:

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"crypto/tls"
	"fmt"
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

// WithInsecureTLS disables server-certificate verification. It is intended for
// testing against self-signed certificates and MUST NOT be used in production.
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
	cfg := sess.client.tlsConfigFor(host)
	tconn := tls.Client(sess.conn, cfg)
	if err := tconn.HandshakeContext(sess.ctx); err != nil {
		return fmt.Errorf("xrootd: TLS handshake failed: %w", err)
	}
	sess.conn = tconn
	return nil
}
```

Add `splitHostPortForTLS` in `xrootd/tls.go` (host without port, tolerant of a
missing port):

```go
// splitHostPortForTLS returns the host portion of addr for use as the TLS
// ServerName. If addr has no port, it is returned unchanged.
func splitHostPortForTLS(addr string) (host, port string, err error) {
	host, port, err = net.SplitHostPort(addr)
	if err != nil {
		return addr, "", nil
	}
	return host, port, nil
}
```

> Add `"net"` to the imports of `xrootd/tls.go`.

In `xrootd/client.go`, add the fields to `Client`:

```go
	tlsConfig   *tls.Config
	wantTLS     bool
	insecureTLS bool
```

> Add `"crypto/tls"` to `client.go` imports.

In `xrootd/session.go`, thread `wantTLS` into the session and perform the upgrade
at the marker left in Task 3. In `newSession`, set `sess.wantTLS` before bootstrap:

```go
	sess := &cliSession{
		// ... existing fields ...
		wantTLS: client.wantTLS,
	}
```

And replace the Task-3 marker comment with the actual upgrade, *before* `go sess.consume()`:

```go
	if sess.protocolInfo.NeedsTLS(sess.wantTLS) {
		if err := sess.upgradeTLS(); err != nil {
			sess.Close()
			return nil, err
		}
	} else if sess.wantTLS && !sess.protocolInfo.HasTLS() {
		sess.Close()
		return nil, fmt.Errorf("xrootd: TLS requested but server %q offers no TLS", sess.addr)
	}
```

> This mirrors `libxrdc` conn.c: upgrade on `gotoTLS || (wantTLS && haveTLS)`; refuse (no silent downgrade) when the client wanted TLS but the server lacks it.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./xrootd/ -run TestTLSUpgradeOnGotoTLS -v`
Expected: PASS.
Run: `go test ./xrootd/...`
Expected: PASS (cleartext bootstrap unaffected: `NeedsTLS(false)` is false when no TLS flags are set).

- [x] **Step 5: Commit**

```bash
gofmt -w xrootd/tls.go xrootd/client.go xrootd/session.go xrootd/tls_test.go
go vet ./xrootd/...
git add xrootd/tls.go xrootd/client.go xrootd/session.go xrootd/tls_test.go
git commit -m "xrootd: add in-protocol TLS upgrade (roots://) with client options"
```

---

### Task 5: Wire scheme-driven TLS through `NewClient` address handling

`NewClient` takes a bare `address` (host:port) today; `roots://` intent lives in the URL, which is parsed by callers. Make `NewClient` accept a full URL (or bare address) and derive `wantTLS` from a `roots://`/`xroots://` scheme, without breaking existing bare-address callers.

**Files:**
- Modify: `xrootd/client.go`
- Modify: `xrootd/port.go`
- Test: `xrootd/client_tls_test.go` (create)

**Interfaces:**
- Consumes: `xrdio.Parse`/`URL.TLS` (Task 1); `Client.wantTLS` (Task 4).
- Produces:
  - `func addressAndTLS(address string) (addr string, tls bool, err error)` — splits a possibly-schemed address into a dial target and a TLS flag. A bare `host[:port]` yields `tls=false`; `roots://host` yields `tls=true`.
  - `NewClient` honours a `roots://`/`xroots://` scheme by setting `client.wantTLS` before dialing.

- [x] **Step 1: Write the failing test**

Create `xrootd/client_tls_test.go`:

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import "testing"

func TestAddressAndTLS(t *testing.T) {
	for _, tc := range []struct {
		in    string
		addr  string
		wants bool
	}{
		{in: "example.org:1094", addr: "example.org:1094", wants: false},
		{in: "root://example.org:1094", addr: "example.org:1094", wants: false},
		{in: "roots://example.org:1094", addr: "example.org:1094", wants: true},
		{in: "xroots://example.org", addr: "example.org", wants: true},
	} {
		t.Run(tc.in, func(t *testing.T) {
			addr, tls, err := addressAndTLS(tc.in)
			if err != nil {
				t.Fatalf("addressAndTLS(%q): %v", tc.in, err)
			}
			if addr != tc.addr {
				t.Fatalf("addr mismatch: got=%q want=%q", addr, tc.addr)
			}
			if tls != tc.wants {
				t.Fatalf("tls mismatch: got=%v want=%v", tls, tc.wants)
			}
		})
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./xrootd/ -run TestAddressAndTLS -v`
Expected: FAIL — `undefined: addressAndTLS`.

- [x] **Step 3: Implement `addressAndTLS` and use it in `NewClient`**

In `xrootd/port.go`, add:

```go
// addressAndTLS splits a client address into a dial target and a TLS flag.
// The input may be a bare host[:port] or a full URL; a roots://xroots:// scheme
// sets tls=true. The returned addr never carries a scheme.
func addressAndTLS(address string) (addr string, tls bool, err error) {
	if !strings.Contains(address, "://") {
		return address, false, nil
	}
	u, err := xrdio.Parse(address)
	if err != nil {
		return "", false, err
	}
	return u.Addr, u.TLS(), nil
}
```

> Add imports `"strings"` and `"go-hep.org/x/hep/xrootd/xrdio"` to `port.go`.

In `xrootd/client.go`, near the top of `NewClient`, translate the address before dialing:

```go
	dialAddr, schemeTLS, err := addressAndTLS(address)
	if err != nil {
		return nil, err
	}
	address = dialAddr
```

Then, after the `client.initSecurityProviders()` / options loop but before
`client.getSession`, fold in the scheme's TLS intent:

```go
	if schemeTLS {
		client.wantTLS = true
	}
```

> Options run first (so an explicit `WithInsecureTLS()` still applies), and a `roots://` scheme additively requests TLS. `getSession` already receives the scheme-less `address`.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./xrootd/ -run TestAddressAndTLS -v`
Expected: PASS.
Run: `go test ./xrootd/...`
Expected: PASS (bare-address callers unaffected: `addressAndTLS` returns the input unchanged and `tls=false`).

- [x] **Step 5: Commit**

```bash
gofmt -w xrootd/port.go xrootd/client.go xrootd/client_tls_test.go
go vet ./xrootd/...
git add xrootd/port.go xrootd/client.go xrootd/client_tls_test.go
git commit -m "xrootd: derive TLS intent from roots:// scheme in NewClient"
```

---

### Task 6: Protocol-neutral `Backend` interface and `Dial` router

Introduce the VFS entry point that phases 2–4 target: a `Backend` interface and a scheme-aware `Dial`. Wire `root://`/`roots://`/`xroot://`/`xroots://` to the existing client; return a clear "not implemented" error for `http`/`https`/`s3`/`dav` so later phases have a defined extension point.

**Files:**
- Create: `xrootd/backend.go`
- Test: `xrootd/backend_test.go` (create)

**Interfaces:**
- Consumes: `NewClient`/`Client` (Tasks 4–5); `Client.FS()` — verify the existing accessor that returns an `xrdfs.FileSystem` (read `xrootd/xrootd.go`/`filesystem.go`; if the client exposes the filesystem differently, adapt the `xrootdBackend` methods accordingly).
- Produces (package `xrootd`):
  - `type Backend interface { FS() xrdfs.FileSystem; Client() *Client; Close() error }` — the protocol-neutral handle. `FS()` returns the filesystem view; `Client()` returns the underlying XRootD client (nil for non-XRootD backends, populated in this phase).
  - `func Dial(ctx context.Context, rawurl, username string, opts ...Option) (Backend, error)` — parses the scheme and constructs the appropriate backend.
  - `var ErrUnsupportedScheme = errors.New("xrootd: unsupported URL scheme")`.

- [x] **Step 1: Write the failing test**

Create `xrootd/backend_test.go`:

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"errors"
	"testing"
)

func TestDialUnsupportedScheme(t *testing.T) {
	for _, scheme := range []string{"http", "https", "s3", "dav"} {
		t.Run(scheme, func(t *testing.T) {
			_, err := Dial(context.Background(), scheme+"://example.org/some/path", "gopher")
			if !errors.Is(err, ErrUnsupportedScheme) {
				t.Fatalf("Dial(%q): got err=%v, want ErrUnsupportedScheme", scheme, err)
			}
		})
	}
}

func TestDialRootScheme(t *testing.T) {
	// Uses the existing mock-server harness so a root:// Dial produces a working Backend.
	serverFunc := func(cancel func(), conn net.Conn) { /* handled by testClientWithMockServer */ }
	_ = serverFunc
	t.Skip("covered by TestDialRootBackend once the mock harness is wired; see Step 3 note")
}
```

> The `TestDialRootScheme` body above is a placeholder-free skip with a rationale: the root:// happy path is exercised by the interop test in Task 7 and by reusing the mock harness. If `testClientWithMockServer` (see `xrootd/main_mock_test.go`) can be adapted to hand back the dialed address, replace the skip with a real assertion that `Dial("root://<mockaddr>", ...)` returns a non-nil `Backend` whose `Client()` is non-nil. Do this only if the harness exposes the listener address; otherwise leave the documented skip.

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./xrootd/ -run TestDialUnsupportedScheme -v`
Expected: FAIL — `undefined: Dial`, `undefined: ErrUnsupportedScheme`.

- [x] **Step 3: Implement `Backend` and `Dial`**

Create `xrootd/backend.go`:

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"context"
	"errors"
	"fmt"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdio"
)

// ErrUnsupportedScheme is returned by Dial for a URL scheme that has no backend
// implementation. Alternative-protocol backends (http, https, s3, dav) are
// added in a later phase.
var ErrUnsupportedScheme = errors.New("xrootd: unsupported URL scheme")

// Backend is a protocol-neutral handle to a storage endpoint. It gives later
// phases (copy engine, alternative protocols) a single entry point independent
// of whether the underlying transport is native XRootD, HTTP, S3, or WebDAV.
type Backend interface {
	// FS returns a filesystem view of the endpoint.
	FS() xrdfs.FileSystem
	// Client returns the underlying XRootD client, or nil for non-XRootD backends.
	Client() *Client
	// Close releases the backend's resources.
	Close() error
}

// xrootdBackend adapts a native XRootD Client to the Backend interface.
type xrootdBackend struct {
	client *Client
}

func (b *xrootdBackend) FS() xrdfs.FileSystem { return b.client.FS() }
func (b *xrootdBackend) Client() *Client      { return b.client }
func (b *xrootdBackend) Close() error         { return b.client.Close() }

// Dial connects to rawurl and returns a protocol-neutral Backend. The scheme
// selects the transport: root/roots/xroot/xroots use native XRootD (roots and
// xroots negotiate TLS). Other schemes return ErrUnsupportedScheme.
func Dial(ctx context.Context, rawurl, username string, opts ...Option) (Backend, error) {
	u, err := xrdio.Parse(rawurl)
	if err != nil {
		return nil, err
	}

	switch u.Scheme {
	case "", "root", "roots", "xroot", "xroots":
		client, err := NewClient(ctx, rawurl, username, opts...)
		if err != nil {
			return nil, err
		}
		return &xrootdBackend{client: client}, nil
	case "http", "https", "s3", "dav":
		return nil, fmt.Errorf("%w: %q (added in a later phase)", ErrUnsupportedScheme, u.Scheme)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedScheme, u.Scheme)
	}
}
```

> Verify the accessor name: read `xrootd/xrootd.go` and `xrootd/filesystem.go` to confirm the method that returns an `xrdfs.FileSystem` for a `*Client`. If it is `FS()`, the code above is correct. If it is named differently (e.g. `Filesystem()`), rename `xrootdBackend.FS` to call the correct method. If there is **no** such accessor, add one to the client in this task:
> ```go
> // FS returns a filesystem view backed by this client.
> func (client *Client) FS() xrdfs.FileSystem { return &fileSystem{c: client} }
> ```
> matching the existing filesystem constructor in `xrootd/filesystem.go` (read it first to use the right unexported type/name).
>
> `Dial` passes the full `rawurl` (scheme included) to `NewClient`; Task 5 made `NewClient` scheme-aware, so `roots://` correctly enables TLS.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./xrootd/ -run 'TestDialUnsupportedScheme|TestDialRootScheme' -v`
Expected: PASS (the unsupported-scheme cases assert `ErrUnsupportedScheme`; the root case is the documented skip or a real assertion if the harness allows).
Run: `go test ./xrootd/...`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
gofmt -w xrootd/backend.go xrootd/backend_test.go
go vet ./xrootd/...
git add xrootd/backend.go xrootd/backend_test.go
git commit -m "xrootd: add protocol-neutral Backend interface and scheme-aware Dial"
```

---

### Task 7: Dual-oracle interop test — TLS against a real XRootD server

Add a build-tag-gated interop test that verifies the go-hep client negotiates TLS and performs a stat against a real XRootD server (the official implementation oracle), and documents the `libxrdc` cross-check. The test must skip cleanly when no server is configured so it never fails on machines without one.

**Files:**
- Create: `xrootd/tls_interop_test.go`
- Create: `docs/superpowers/testing/xrootd-phase-0-parity.md`

**Interfaces:**
- Consumes: `Dial` (Task 6), `WithInsecureTLS` (Task 4).
- Produces: no new source API; a gated test plus a parity-verification runbook.

- [x] **Step 1: Write the (skipping) interop test**

Create `xrootd/tls_interop_test.go`:

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestTLSInterop connects to a real XRootD server over roots:// and stats a
// path. It is skipped unless XROOTD_TLS_SERVER is set, e.g.
//
//	XROOTD_TLS_SERVER=roots://xrootd.example.org:1094 \
//	XROOTD_TLS_PATH=//store/test/file.root \
//	go test ./xrootd/ -run TestTLSInterop -v
//
// Set XROOTD_TLS_INSECURE=1 to accept a self-signed server certificate.
func TestTLSInterop(t *testing.T) {
	server := os.Getenv("XROOTD_TLS_SERVER")
	if server == "" {
		t.Skip("set XROOTD_TLS_SERVER to run the TLS interop test")
	}
	path := os.Getenv("XROOTD_TLS_PATH")
	if path == "" {
		t.Skip("set XROOTD_TLS_PATH to a stat-able path on the server")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var opts []Option
	if os.Getenv("XROOTD_TLS_INSECURE") == "1" {
		opts = append(opts, WithInsecureTLS())
	}

	be, err := Dial(ctx, server, os.Getenv("USER"), opts...)
	if err != nil {
		t.Fatalf("Dial(%q) over TLS: %v", server, err)
	}
	defer be.Close()

	fi, err := be.FS().Stat(ctx, path)
	if err != nil {
		t.Fatalf("Stat(%q): %v", path, err)
	}
	t.Logf("stat ok: name=%s size=%d dir=%v", fi.Name(), fi.Size(), fi.IsDir())
}
```

> Verify the `FS().Stat` signature by reading `xrootd/filesystem.go` / `xrdfs`: confirm it is `Stat(ctx, path) (xrdfs.EntryStat, error)` and that `EntryStat` has `Name()`, `Size()`, `IsDir()` (it does, per `xrdfs`). Adjust the log line if the accessors differ.

- [x] **Step 2: Run the test to verify it skips cleanly**

Run: `go test ./xrootd/ -run TestTLSInterop -v`
Expected: PASS with `--- SKIP` (no `XROOTD_TLS_SERVER` set). This proves the test is well-formed and non-breaking on developer machines.

- [x] **Step 3: Write the parity runbook**

Create `docs/superpowers/testing/xrootd-phase-0-parity.md`:

```markdown
# Phase 0 Parity Verification (TLS + VFS)

Two oracles must agree with the go-hep client.

## Oracle 1 — official XRootD server (interop)

Prerequisite: an XRootD server with TLS enabled (config `xrd.tls` cert/key and
`xrd.tlsca`), reachable at `roots://HOST:1094`.

```
export XROOTD_TLS_SERVER=roots://HOST:1094
export XROOTD_TLS_PATH=//store/test/file.root
# For a self-signed test cert:
export XROOTD_TLS_INSECURE=1
go test ./xrootd/ -run TestTLSInterop -v
```

Expected: the stat succeeds over a TLS-upgraded connection. Capture a packet trace
(`tcpdump`) if you need to confirm the bytes after the `kXR_protocol` reply are
TLS records, not cleartext XRootD frames.

## Oracle 2 — libxrdc C client (behavioral cross-check)

Run the same operation with the reference C client and confirm identical results:

```
# Native C client (roots:// forces TLS, same negotiation path go-hep now uses):
/home/rcurrie/HEP-x/nginx-xrootd/client/bin/xrdfs roots://HOST:1094 stat /store/test/file.root
```

Compare: file size, and (once Phase 1 lands) checksum values must match between the
go-hep client and `xrdfs`/`xrdcp`. For Phase 0 the cross-check is: both clients
successfully negotiate TLS and return the same stat metadata for the same path.

## Cleartext regression

`root://HOST:1094` (no TLS) must continue to work unchanged:

```
go test ./xrootd/...   # full suite, all green
```
```

- [x] **Step 4: Run the full suite and vet**

Run: `go test ./xrootd/...`
Expected: PASS (all tasks' tests green, interop test skipped).
Run: `go vet ./xrootd/...` and `gofmt -l xrootd/`
Expected: no vet findings, no unformatted files.

- [x] **Step 5: Commit**

```bash
gofmt -w xrootd/tls_interop_test.go
git add xrootd/tls_interop_test.go docs/superpowers/testing/xrootd-phase-0-parity.md
git commit -m "xrootd: add TLS interop test and Phase 0 parity runbook"
```

---

## Self-Review

**Spec coverage (Phase 0 scope from the roadmap):**
- "TLS / `xroots://`: TLS handshake + `kXR_protocol` TLS negotiation, prerequisite blocker" → Tasks 2 (constants/flags), 3 (protocol-before-login reorder), 4 (TLS upgrade), 5 (scheme-driven TLS). ✓
- "URL/scheme router (`root`, `roots`, `xroot`, `xroots`, later `http(s)`, `s3`, `dav`)" → Tasks 1 (scheme parse) + 6 (`Dial` router with those cases stubbed). ✓
- "`Backend`/VFS interface the copy engine and CLIs target" → Task 6. ✓
- Dual-oracle testing (official XRootD + libxrdc) → Task 7 (interop test + runbook naming both oracles); every task also carries mock/unit tests. ✓
- Code-quality bar (gofmt/vet clean, doc comments on exported IDs, idiomatic context/errors) → enforced per-task in Step 4/5 and in Global Constraints. ✓

**Placeholder scan:** The only `panic("implement...")`/`t.Skip` items are explicitly called out as MUST-implement (`selfSignedCert`) or documented, rationale-bearing skips (`TestDialRootScheme`, `TestTLSInterop`), not silent TODOs. No "add error handling"/"similar to Task N" placeholders. ✓

**Type consistency:** `NewRequestTLS` signature, `Response` TLS accessors, `bootstrapExchange`, `upgradeTLS`, `addressAndTLS`, `Backend`/`Dial`/`ErrUnsupportedScheme` names are used identically wherever referenced across tasks. Protocol constant values match `XProtocol.hh` verbatim (Global Constraints). `StreamID` is `[2]byte`; `Flags` stays `int32` on the wire with `uint32` bit-test accessors. ✓

**Verification-dependent assumptions flagged inline** (must be confirmed by reading the named files during implementation, not guessed): `handshake.Request` has no `ReqID()` (raw-bytes handshake path); the `*Client` filesystem accessor name (`FS()` vs other); exact signatures of `xrdproto.ReadRequest`/`WriteResponse`/`RequestHeaderLength`; `xrdfs.EntryStat` accessor names. Each is called out at the point it is used.
