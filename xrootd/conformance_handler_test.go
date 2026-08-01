// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the server side of the protocol that does not depend on a
// filesystem: what the default handler answers, and what a Server does with a
// request it cannot parse or does not know.
//
// Default() exists so that an embedder can implement the handful of operations
// it cares about and inherit the rest. What it inherits has to be a refusal the
// client understands — kXR_ArgInvalid with a message naming the operation —
// because a handler that answered kXR_ok with an empty body would leave the
// client believing an unimplemented write had been stored.

package xrootd

import (
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/dirlist"
	"go-hep.org/x/hep/xrootd/xrdproto/handshake"
	"go-hep.org/x/hep/xrootd/xrdproto/login"
	"go-hep.org/x/hep/xrootd/xrdproto/mkdir"
	"go-hep.org/x/hep/xrootd/xrdproto/mv"
	"go-hep.org/x/hep/xrootd/xrdproto/open"
	"go-hep.org/x/hep/xrootd/xrdproto/ping"
	"go-hep.org/x/hep/xrootd/xrdproto/protocol"
	"go-hep.org/x/hep/xrootd/xrdproto/read"
	"go-hep.org/x/hep/xrootd/xrdproto/rm"
	"go-hep.org/x/hep/xrootd/xrdproto/rmdir"
	"go-hep.org/x/hep/xrootd/xrdproto/stat"
	xrdsync "go-hep.org/x/hep/xrootd/xrdproto/sync"
	"go-hep.org/x/hep/xrootd/xrdproto/truncate"
	"go-hep.org/x/hep/xrootd/xrdproto/write"
	"go-hep.org/x/hep/xrootd/xrdproto/xrdclose"
)

var confSessionID = [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

func TestConformance_TheDefaultHandlerRefusesEveryOperationItDoesNotImplement(t *testing.T) {
	h := Default()

	for _, tc := range []struct {
		// op is both the name in the refusal message and the label of the
		// subtest, so a handler that copies a neighbouring message is caught.
		op   string
		call func() (xrdproto.Marshaler, xrdproto.ResponseStatus)
	}{
		{"Dirlist", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Dirlist(confSessionID, &dirlist.Request{Path: "/tmp"})
		}},
		{"Open", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Open(confSessionID, &open.Request{Path: "/tmp/file"})
		}},
		{"Close", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Close(confSessionID, &xrdclose.Request{})
		}},
		{"Read", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Read(confSessionID, &read.Request{Length: 8})
		}},
		{"Write", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Write(confSessionID, &write.Request{Data: []byte("go-hep")})
		}},
		{"Stat", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Stat(confSessionID, &stat.Request{Path: "/tmp/file"})
		}},
		{"Sync", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Sync(confSessionID, &xrdsync.Request{})
		}},
		{"Truncate", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Truncate(confSessionID, &truncate.Request{Path: "/tmp/file", Size: 1})
		}},
		{"Rename", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Rename(confSessionID, &mv.Request{OldPath: "/tmp/a", NewPath: "/tmp/b"})
		}},
		{"Mkdir", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Mkdir(confSessionID, &mkdir.Request{Path: "/tmp/dir"})
		}},
		{"Remove", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Remove(confSessionID, &rm.Request{Path: "/tmp/file"})
		}},
		{"RemoveDir", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.RemoveDir(confSessionID, &rmdir.Request{Path: "/tmp/dir"})
		}},
	} {
		t.Run(tc.op, func(t *testing.T) {
			resp, status := tc.call()
			if status != xrdproto.Error {
				t.Fatalf("%s answered status %v, want %v", tc.op, status, xrdproto.Error)
			}
			srvErr, ok := resp.(xrdproto.ServerError)
			if !ok {
				t.Fatalf("%s answered %T, want a server error", tc.op, resp)
			}
			if srvErr.Code != xrdproto.InvalidRequest {
				t.Fatalf("%s answered code %v, want %v", tc.op, srvErr.Code, xrdproto.InvalidRequest)
			}
			if !strings.Contains(srvErr.Message, tc.op) {
				t.Fatalf("%s answered %q, which does not name the operation", tc.op, srvErr.Message)
			}
		})
	}
}

func TestConformance_TheDefaultHandlerCompletesTheOperationsEveryServerShares(t *testing.T) {
	// Handshake, login, protocol and ping are the same for every server: they
	// establish the session rather than touch storage. An embedder must be
	// able to inherit them and still be reachable by a stock client.
	h := Default()

	t.Run("handshake", func(t *testing.T) {
		resp, status := h.Handshake()
		if status != xrdproto.Ok {
			t.Fatalf("handshake status = %v, want %v", status, xrdproto.Ok)
		}
		hs, ok := resp.(*handshake.Response)
		if !ok {
			t.Fatalf("handshake answered %T, want a handshake response", resp)
		}
		if hs.ProtocolVersion != 0x310 {
			t.Fatalf("handshake announced protocol %#x, want %#x", hs.ProtocolVersion, 0x310)
		}
		if hs.ServerType != xrdproto.DataServer {
			t.Fatalf("handshake announced server type %v, want %v", hs.ServerType, xrdproto.DataServer)
		}
	})

	t.Run("login", func(t *testing.T) {
		// The session id the handler is given is the one it must hand back:
		// a client binds its second connection with exactly these bytes.
		resp, status := h.Login(confSessionID, &login.Request{Username: [8]byte{'g', 'o', 'p', 'h', 'e', 'r'}})
		if status != xrdproto.Ok {
			t.Fatalf("login status = %v, want %v", status, xrdproto.Ok)
		}
		lg, ok := resp.(*login.Response)
		if !ok {
			t.Fatalf("login answered %T, want a login response", resp)
		}
		if lg.SessionID != confSessionID {
			t.Fatalf("login answered session %v, want %v", lg.SessionID, confSessionID)
		}
		if lg.SecurityInformation != nil {
			t.Fatalf("login offered security information %q, want none", lg.SecurityInformation)
		}
	})

	t.Run("protocol", func(t *testing.T) {
		resp, status := h.Protocol(confSessionID, &protocol.Request{})
		if status != xrdproto.Ok {
			t.Fatalf("protocol status = %v, want %v", status, xrdproto.Ok)
		}
		pr, ok := resp.(*protocol.Response)
		if !ok {
			t.Fatalf("protocol answered %T, want a protocol response", resp)
		}
		if !pr.IsServer() {
			t.Fatalf("protocol announced flags %#08x, which do not include the server role", int32(pr.Flags))
		}
		if pr.IsManager() {
			t.Fatal("the default handler announced itself as a manager")
		}
		if pr.HasSecurityInfo {
			t.Fatal("the default handler announced security requirements it does not enforce")
		}
	})

	t.Run("ping", func(t *testing.T) {
		// kXR_ping is answered with kXR_ok and no body at all: the round trip
		// is the whole answer.
		resp, status := h.Ping(confSessionID, &ping.Request{})
		if status != xrdproto.Ok {
			t.Fatalf("ping status = %v, want %v", status, xrdproto.Ok)
		}
		if resp != nil {
			t.Fatalf("ping answered a body %v, want none", resp)
		}
	})

	t.Run("close-session", func(t *testing.T) {
		if err := h.CloseSession(confSessionID); err != nil {
			t.Fatalf("closing a session the default handler never opened: %v", err)
		}
	})
}

func TestConformance_AServerRefusesARequestItCannotParse(t *testing.T) {
	// Every request this server dispatches has a fixed parameter area. A
	// frame that declares one of those request ids and then stops short is
	// not a request the handler may be asked to act on: the fields it would
	// read are whatever was left in memory.
	srv := NewServer(Default(), nil)

	for _, tc := range []struct {
		name string
		id   uint16
	}{
		{"login", login.RequestID},
		{"dirlist", dirlist.RequestID},
		{"open", open.RequestID},
		{"close", xrdclose.RequestID},
		{"read", read.RequestID},
		{"write", write.RequestID},
		{"stat", stat.RequestID},
		{"sync", xrdsync.RequestID},
		{"truncate", truncate.RequestID},
		{"mv", mv.RequestID},
		{"mkdir", mkdir.RequestID},
		{"rm", rm.RequestID},
		{"rmdir", rmdir.RequestID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Two bytes: enough to prove the id was dispatched, far short of
			// the sixteen the parameter area needs.
			resp, status := srv.handleRequest(confSessionID, tc.id, xrdenc.NewRBuffer([]byte{0, 0}))
			if status != xrdproto.Error {
				t.Fatalf("a truncated %s was accepted with status %v", tc.name, status)
			}
			srvErr, ok := resp.(xrdproto.ServerError)
			if !ok {
				t.Fatalf("a truncated %s answered %T, want a server error", tc.name, resp)
			}
			if srvErr.Code != xrdproto.InvalidRequest {
				t.Fatalf("a truncated %s answered code %v, want %v", tc.name, srvErr.Code, xrdproto.InvalidRequest)
			}
			if !strings.Contains(srvErr.Message, "parsing the request") {
				t.Fatalf("a truncated %s answered %q, which does not say the frame was unreadable", tc.name, srvErr.Message)
			}
		})
	}
}

func TestConformance_AServerRefusesARequestItDoesNotKnow(t *testing.T) {
	// A request id outside the dispatch table is answered, not dropped: the
	// client is waiting on that stream id and would otherwise hang until its
	// deadline.
	srv := NewServer(Default(), nil)

	const unknown = 3999
	resp, status := srv.handleRequest(confSessionID, unknown, xrdenc.NewRBuffer(make([]byte, 16)))
	if status != xrdproto.Error {
		t.Fatalf("an unknown request was accepted with status %v", status)
	}
	srvErr, ok := resp.(xrdproto.ServerError)
	if !ok {
		t.Fatalf("an unknown request answered %T, want a server error", resp)
	}
	if srvErr.Code != xrdproto.InvalidRequest {
		t.Fatalf("an unknown request answered code %v, want %v", srvErr.Code, xrdproto.InvalidRequest)
	}
	if !strings.Contains(srvErr.Message, "3999") {
		t.Fatalf("an unknown request answered %q, which does not name the id received", srvErr.Message)
	}
}

func TestConformance_AWellFormedRequestReachesTheHandler(t *testing.T) {
	// The counterpart of the two tests above: a frame that does parse is
	// dispatched, so that "unparseable" is a statement about the frame and
	// not about every request of that kind.
	srv := NewServer(Default(), nil)

	var w xrdenc.WBuffer
	if err := (&ping.Request{}).MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal a ping: %v", err)
	}
	resp, status := srv.handleRequest(confSessionID, ping.RequestID, xrdenc.NewRBuffer(w.Bytes()))
	if status != xrdproto.Ok {
		t.Fatalf("a well-formed ping answered status %v, want %v", status, xrdproto.Ok)
	}
	if resp != nil {
		t.Fatalf("a well-formed ping answered a body %v, want none", resp)
	}
}
