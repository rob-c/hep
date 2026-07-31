// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Fail-closed conformance: a server that stalls, over-answers or corrupts must
// never be reported as success. Each case drives the strict server from
// conformance_server_test.go into one specific misbehaviour and asserts the
// client fails with a diagnosable error instead of returning plausible bytes.

package xrootd

import (
	"context"
	"strings"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/pgread"
	"go-hep.org/x/hep/xrootd/xrdproto/pgwrite"
	"go-hep.org/x/hep/xrootd/xrdproto/read"
	"go-hep.org/x/hep/xrootd/xrdproto/sync"
	"go-hep.org/x/hep/xrootd/xrdproto/xrdclose"
)

// wantErr fails t unless err is non-nil and mentions want.
func wantErr(t *testing.T, err error, what, want string) {
	t.Helper()
	switch {
	case err == nil:
		t.Fatalf("%s: got no error, want one mentioning %q", what, want)
	case !strings.Contains(err.Error(), want):
		t.Fatalf("%s: got error %q, want one mentioning %q", what, err, want)
	}
}

func TestConformance_Read_OverAnsweredIsRefused(t *testing.T) {
	// The server returns more bytes than were asked for. Silently keeping them
	// would let a peer decide how much memory the client spends, and would make
	// ReadAt report a count its caller's buffer never received.
	srv := confClient(t, confContent, func(srv *confServer, f *file) {
		srv.set(func(srv *confServer) { srv.overAnswer = 512 })

		p := make([]byte, 1024)
		_, err := f.ReadAt(p, 0)
		wantErr(t, err, "ReadAt against an over-answering server", "exceeds")
	})
	srv.check(t)
}

func TestConformance_Read_OversizedBodyIsRefusedBeforeAllocation(t *testing.T) {
	// The server announces a body past MaxResponseLength and then hangs up
	// behind the lie: a client that trusts the header allocates the whole
	// claimed size before it discovers there are no bytes coming.
	confClient(t, confContent, func(srv *confServer, f *file) {
		srv.set(func(srv *confServer) { srv.hugeDlen = true })

		p := make([]byte, 64)
		_, err := f.ReadAt(p, 0)
		if err == nil {
			t.Fatal("ReadAt: got no error for a body past the response limit")
		}
	})
	// No srv.check: the connection is torn down mid-frame by design.
}

func TestConformance_Read_StalledServerIsBoundedByTheContext(t *testing.T) {
	// The server accepts the request and never answers. Nothing in the protocol
	// bounds that, so the caller's context is the only backstop.
	confClient(t, confContent, func(srv *confServer, f *file) {
		srv.set(func(srv *confServer) { srv.stall = true })

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		start := time.Now()
		_, err := f.ReadAtContext(ctx, make([]byte, 64), 0)
		if err == nil {
			t.Fatal("ReadAtContext: got no error from a server that never answered")
		}
		if !isDeadline(err) {
			t.Fatalf("ReadAtContext: got %v, want a deadline error", err)
		}
		if d := time.Since(start); d > 5*time.Second {
			t.Fatalf("ReadAtContext blocked for %v past its 100ms deadline", d)
		}
	})
}

func isDeadline(err error) bool {
	return strings.Contains(err.Error(), context.DeadlineExceeded.Error())
}

func TestConformance_PgRead_CorruptPageIsAnErrorNotData(t *testing.T) {
	// A page whose CRC-32C does not match is exactly the case kXR_pgread
	// exists to catch: it must surface as an error, never as bytes.
	srv := confClient(t, confContent, func(srv *confServer, f *file) {
		srv.set(func(srv *confServer) { srv.corruptPage = true })

		p := make([]byte, 2*confPage)
		n, err := f.PgReadAt(context.Background(), p, 0)
		wantErr(t, err, "PgReadAt over a corrupted page", "CRC mismatch")
		if n != 0 {
			t.Fatalf("PgReadAt returned %d bytes alongside a checksum failure", n)
		}
	})
	srv.check(t)
}

func TestConformance_PgRead_TruncatedPageUnitIsRefused(t *testing.T) {
	// A page unit cut off mid-CRC carries no verifiable data. Decoding what is
	// there anyway would hand the caller unchecked bytes.
	srv := confClient(t, confContent, func(srv *confServer, f *file) {
		srv.set(func(srv *confServer) { srv.shortPgUnit = true })

		n, err := f.PgReadAt(context.Background(), make([]byte, confPage), 0)
		if err == nil {
			t.Fatalf("PgReadAt: got no error for a truncated page unit (%d bytes returned)", n)
		}
		if n != 0 {
			t.Fatalf("PgReadAt returned %d bytes from a truncated page unit", n)
		}
	})
	srv.check(t)
}

func TestConformance_PgWrite_GivesUpAfterTheRetryBudget(t *testing.T) {
	// The server reports the same page corrupt forever. The client must retry a
	// bounded number of times and then fail: reporting success would leave the
	// caller believing bytes the server itself knows are damaged.
	payload := confBytes(2 * confPage)
	srv := confClient(t, nil, func(srv *confServer, f *file) {
		srv.set(func(srv *confServer) { srv.badAlways[int64(confPage)] = true })

		err := f.PgWriteAt(context.Background(), payload, 0)
		wantErr(t, err, "PgWriteAt against a permanently failing page", "still corrupt")
	})
	srv.check(t)

	if got, want := srv.opCount(pgwrite.RequestID), 1+maxPgRetries; got != want {
		t.Fatalf("got %d kXR_pgwrite requests, want %d (the initial write plus %d retries)", got, want, maxPgRetries)
	}
}

func TestConformance_SyncAndCloseSurfaceServerErrors(t *testing.T) {
	// A failed sync or close means the file is not durable. Swallowing either
	// turns data loss into a silent success.
	srv := confClient(t, confContent, func(srv *confServer, f *file) {
		srv.set(func(srv *confServer) { srv.failSync = true })
		wantErr(t, f.Sync(context.Background()), "Sync", "sync failed")

		srv.set(func(srv *confServer) { srv.failSync, srv.failClose = false, true })
		wantErr(t, f.Close(context.Background()), "Close", "close failed")
	})
	srv.check(t)

	if got, want := srv.opSeq(), []uint16{sync.RequestID, xrdclose.RequestID}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got op sequence %v, want %v", got, want)
	}
}

// TestConformance_ServerCatchesBreaches checks the oracle itself: the strict
// server must actually detect the misbehaviour the other tests assert is
// absent. Without this, every srv.check(t) above could be passing vacuously.
func TestConformance_ServerCatchesBreaches(t *testing.T) {
	t.Run("wrong fhandle", func(t *testing.T) {
		srv := confClient(t, confContent, func(srv *confServer, f *file) {
			bad := &file{fs: f.fs, handle: xrdfs.FileHandle{0, 0, 0, 9}, sessionID: f.sessionID}
			if _, err := bad.ReadAt(make([]byte, 16), 0); err != nil {
				t.Fatalf("ReadAt: %v", err)
			}
		})
		if got := srv.breaches(); len(got) != 1 || !strings.Contains(got[0], "unknown fhandle") {
			t.Fatalf("got violations %q, want one about an unknown fhandle", got)
		}
	})

	t.Run("bad page crc", func(t *testing.T) {
		srv := newConfServer(nil)
		payload := append(be32(0xdeadbeef), confBytes(confPage)...)
		if _, ok := srv.pgWritePages(0, payload); ok {
			t.Fatal("pgWritePages accepted a page whose CRC-32C does not match")
		}
		if got := srv.breaches(); len(got) != 1 || !strings.Contains(got[0], "CRC-32C mismatch") {
			t.Fatalf("got violations %q, want one about a CRC-32C mismatch", got)
		}
	})

	t.Run("a well-formed unaligned page is not flagged", func(t *testing.T) {
		srv := newConfServer(nil)
		if _, ok := srv.pgWritePages(1, append(be32(confCRC(confBytes(8))), confBytes(8)...)); !ok {
			t.Fatal("pgWritePages rejected a well-formed unaligned page")
		}
		if got := srv.breaches(); len(got) != 0 {
			t.Fatalf("got violations %q on a well-formed payload, want none", got)
		}
	})
}

// TestConformance_ResponseLimitsAreDerivedFromTheRequest pins the bounds the
// session enforces: they must be big enough for a legitimate reply and small
// enough to stop a server from driving the client's heap.
func TestConformance_ResponseLimitsAreDerivedFromTheRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  xrdproto.Request
		min  int64
	}{
		{"read", &read.Request{Handle: confHandle, Length: 8192}, 8192},
		{"pgread", &pgread.Request{Handle: confHandle, ReadLength: 8192}, 8192},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lim, ok := tc.req.(xrdproto.ResponseLimiter)
			if !ok {
				t.Fatalf("%T does not implement xrdproto.ResponseLimiter", tc.req)
			}
			switch got := lim.MaxResponseLength(); {
			case got < tc.min:
				t.Fatalf("MaxResponseLength = %d, too small for a %d-byte reply", got, tc.min)
			case got > xrdproto.MaxResponseLength:
				t.Fatalf("MaxResponseLength = %d, above the %d-byte frame cap", got, xrdproto.MaxResponseLength)
			}
		})
	}
}
