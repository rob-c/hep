// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance: the happy paths, driven against the strict server in
// conformance_server_test.go. Every case asserts on the bytes the server
// actually holds (or sent) rather than on a status code, and asserts the
// server recorded no protocol violation.

package xrootd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdproto/pgwrite"
	"go-hep.org/x/hep/xrootd/xrdproto/read"
	"go-hep.org/x/hep/xrootd/xrdproto/sync"
	"go-hep.org/x/hep/xrootd/xrdproto/write"
	"go-hep.org/x/hep/xrootd/xrdproto/xrdclose"
)

func TestConformance_Read_Ranges(t *testing.T) {
	srv := confClient(t, confContent, func(srv *confServer, f *file) {
		for _, tc := range []struct {
			name string
			off  int64
			n    int
			want []byte
		}{
			{name: "whole file", off: 0, n: len(confContent), want: confContent},
			{name: "page aligned", off: 4096, n: 1000, want: confContent[4096:5096]},
			{name: "unaligned", off: 4096, n: 500, want: confContent[4096:4596]},
			{name: "last bytes", off: int64(len(confContent) - 10), n: 10, want: confContent[len(confContent)-10:]},
			// A read entirely past EOF returns nothing; it is not an error.
			{name: "past EOF", off: int64(len(confContent)) + 100, n: 16, want: nil},
			// A read straddling EOF is truncated to what exists.
			{name: "straddles EOF", off: int64(len(confContent) - 4), n: 64, want: confContent[len(confContent)-4:]},
		} {
			t.Run(tc.name, func(t *testing.T) {
				p := make([]byte, tc.n)
				n, err := f.ReadAt(p, tc.off)
				// A short read may report io.EOF; anything else is a failure.
				if err != nil && !errors.Is(err, io.EOF) {
					t.Fatalf("ReadAt: %v", err)
				}
				if n != len(tc.want) {
					t.Fatalf("got %d bytes, want %d", n, len(tc.want))
				}
				if !bytes.Equal(p[:n], tc.want) {
					t.Fatal("the bytes read differ from the file content")
				}
			})
		}
	})
	srv.check(t)
}

// TestConformance_Read_ReplyShapes drives the reply shapes a client must
// reassemble: a response split across OkSoFar frames, a kXR_wait the client
// must honour and retry, a deferred kXR_attn/kXR_asynresp completion, and an
// unsolicited frame that must be dropped rather than kill the session.
func TestConformance_Read_ReplyShapes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(srv *confServer)
	}{
		{
			name:  "chunked into OkSoFar frames",
			setup: func(srv *confServer) { srv.readChunk = 7 },
		},
		{
			name:  "kXR_wait then a normal reply",
			setup: func(srv *confServer) { srv.waitOnce = true },
		},
		{
			name:  "deferred via kXR_attn/kXR_asynresp",
			setup: func(srv *confServer) { srv.asyncRead = true },
		},
		{
			name:  "preceded by a frame for no one",
			setup: func(srv *confServer) { srv.unsolicited = true },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := confClient(t, confContent, func(srv *confServer, f *file) {
				srv.set(tc.setup)
				p := make([]byte, 300)
				n, err := f.ReadAt(p, 64)
				if err != nil {
					t.Fatalf("ReadAt: %v", err)
				}
				if !bytes.Equal(p[:n], confContent[64:364]) {
					t.Fatalf("reassembled data mismatch (%d bytes)", n)
				}

				// The session must still be usable afterwards.
				q := make([]byte, 16)
				if _, err := f.ReadAt(q, 0); err != nil {
					t.Fatalf("follow-up ReadAt: %v", err)
				}
				if !bytes.Equal(q, confContent[:16]) {
					t.Fatal("follow-up read returned the wrong bytes")
				}
			})
			srv.check(t)
		})
	}
}

// TestConformance_Write_ServerHoldsExactBytes verifies the write path against
// what the server stored, not against the status it returned.
func TestConformance_Write_ServerHoldsExactBytes(t *testing.T) {
	srv := confClient(t, nil, func(srv *confServer, f *file) {
		ctx := context.Background()

		if _, err := f.WriteAt([]byte("hello world"), 0); err != nil {
			t.Fatalf("WriteAt: %v", err)
		}
		if got := srv.content(); !bytes.Equal(got, []byte("hello world")) {
			t.Fatalf("server holds %q", got)
		}

		// A write past the end extends the file, zero-filling the hole.
		tail := make([]byte, 256)
		for i := range tail {
			tail[i] = byte(i)
		}
		if _, err := f.WriteAt(tail, 1024); err != nil {
			t.Fatalf("WriteAt past EOF: %v", err)
		}
		got := srv.content()
		if len(got) != 1280 {
			t.Fatalf("got file size %d, want 1280", len(got))
		}
		if !bytes.Equal(got[1024:1280], tail) {
			t.Fatal("the extending write did not land at 1024")
		}
		if !bytes.Equal(got[11:1024], make([]byte, 1013)) {
			t.Fatal("the hole was not zero-filled")
		}

		// An overwrite replaces bytes in place without changing the size.
		if _, err := f.WriteAt([]byte{0xf0, 0xf1, 0xf2, 0xf3}, 0); err != nil {
			t.Fatalf("overwrite: %v", err)
		}
		if got := srv.content(); !bytes.Equal(got[:4], []byte{0xf0, 0xf1, 0xf2, 0xf3}) || len(got) != 1280 {
			t.Fatal("the overwrite changed the file size or missed")
		}

		if err := f.Truncate(ctx, 8); err != nil {
			t.Fatalf("Truncate: %v", err)
		}
		if got := srv.content(); len(got) != 8 {
			t.Fatalf("after truncate the file is %d bytes, want 8", len(got))
		}
		if err := f.Sync(ctx); err != nil {
			t.Fatalf("Sync: %v", err)
		}
		if err := f.Close(ctx); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	srv.check(t)

	// The durability signals must be the last two operations, in that order:
	// a sync after the close would not have committed anything.
	ops := srv.opSeq()
	if len(ops) < 2 {
		t.Fatalf("got %d operations, want at least 2", len(ops))
	}
	if got := ops[len(ops)-2:]; got[0] != sync.RequestID || got[1] != xrdclose.RequestID {
		t.Fatalf("the last two operations were %v, want [kXR_sync kXR_close]", got)
	}
	if n := srv.opCount(write.RequestID); n != 3 {
		t.Fatalf("got %d kXR_write requests, want 3", n)
	}
}

// TestConformance_PgRead_CRCVerifiedPages checks paged reads against pages the
// server framed and checksummed independently of the client's own encoder,
// aligned and not.
func TestConformance_PgRead_CRCVerifiedPages(t *testing.T) {
	for _, tc := range []struct {
		name string
		off  int64
		n    int
	}{
		{name: "short, aligned", off: 0, n: 10},
		{name: "exactly one page", off: 0, n: confPage},
		{name: "multi page, aligned", off: 0, n: 2*confPage + 100},
		{name: "unaligned start", off: 100, n: 2 * confPage},
		{name: "unaligned start and end", off: 4095, n: confPage + 1},
		{name: "to EOF", off: 8192, n: len(confContent) - 8192},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := confClient(t, confContent, func(srv *confServer, f *file) {
				p := make([]byte, tc.n)
				n, err := f.PgReadAt(context.Background(), p, tc.off)
				if err != nil {
					t.Fatalf("PgReadAt: %v", err)
				}
				want := confContent[tc.off : tc.off+int64(tc.n)]
				if !bytes.Equal(p[:n], want) {
					t.Fatalf("got %d bytes, want %d, content differs", n, len(want))
				}
			})
			srv.check(t)
		})
	}
}

// TestConformance_PgWrite_ServerVerifiesEveryCRC checks that the per-page
// CRC-32C the client attaches is the one an independent implementation
// computes: the server recomputes each page's checksum and flags a mismatch.
func TestConformance_PgWrite_ServerVerifiesEveryCRC(t *testing.T) {
	for _, tc := range []struct {
		name string
		off  int64
		n    int
	}{
		{name: "short, aligned", off: 0, n: 100},
		{name: "exactly one page", off: 0, n: confPage},
		{name: "multi page", off: 0, n: 2*confPage + 7},
		// The first unit of an unaligned write is short: it runs only to the
		// next 4 KiB boundary of the FILE offset, not of the buffer.
		{name: "unaligned start", off: 100, n: confPage + 50},
		{name: "starts one byte before a boundary", off: 4095, n: 4098},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := confContent[:tc.n]
			srv := confClient(t, nil, func(srv *confServer, f *file) {
				if err := f.PgWriteAt(context.Background(), payload, tc.off); err != nil {
					t.Fatalf("PgWriteAt: %v", err)
				}
				got := srv.content()
				if int64(len(got)) != tc.off+int64(tc.n) {
					t.Fatalf("server holds %d bytes, want %d", len(got), tc.off+int64(tc.n))
				}
				if !bytes.Equal(got[tc.off:], payload) {
					t.Fatal("the stored bytes differ from what was written")
				}
			})
			srv.check(t)
		})
	}
}

// TestConformance_PgWrite_RetriesOnlyTheCorruptPage checks the kXR_pgRetry
// recovery flow: exactly the reported page is resent, page aligned and alone,
// and the write succeeds once the server accepts it.
func TestConformance_PgWrite_RetriesOnlyTheCorruptPage(t *testing.T) {
	payload := confBytes(3 * confPage)

	srv := confClient(t, nil, func(srv *confServer, f *file) {
		srv.set(func(srv *confServer) { srv.badOnce[confPage] = true }) // the middle page fails once
		if err := f.PgWriteAt(context.Background(), payload, 0); err != nil {
			t.Fatalf("PgWriteAt: %v", err)
		}
		if got := srv.content(); !bytes.Equal(got, payload) {
			t.Fatal("the file does not hold the payload after recovery")
		}
	})
	srv.check(t) // the server flags a retry that is unaligned or covers >1 page

	// The initial write plus exactly one retry — no blind re-send of the whole
	// request, and no extra attempts once the server accepted the page.
	if n := srv.opCount(pgwrite.RequestID); n != 2 {
		t.Fatalf("got %d kXR_pgwrite requests, want 2 (initial + 1 retry)", n)
	}
}

// TestConformance_PgWrite_RetriesEveryReportedPage checks that a reply naming
// several corrupt pages has all of them resent, not just the first.
func TestConformance_PgWrite_RetriesEveryReportedPage(t *testing.T) {
	payload := confBytes(3 * confPage)

	srv := confClient(t, nil, func(srv *confServer, f *file) {
		srv.set(func(srv *confServer) {
			srv.badOnce[0] = true
			srv.badOnce[2*confPage] = true
		})
		if err := f.PgWriteAt(context.Background(), payload, 0); err != nil {
			t.Fatalf("PgWriteAt: %v", err)
		}
		if got := srv.content(); !bytes.Equal(got, payload) {
			t.Fatal("the file does not hold the payload after recovery")
		}
	})
	srv.check(t)

	if n := srv.opCount(pgwrite.RequestID); n != 3 {
		t.Fatalf("got %d kXR_pgwrite requests, want 3 (initial + 2 retries)", n)
	}
}

// TestConformance_ReadWriteRoundTrip writes a file with one API and reads it
// back with the other: the paged and unpaged paths must agree byte for byte.
func TestConformance_ReadWriteRoundTrip(t *testing.T) {
	payload := confContent[:2*confPage+123]

	srv := confClient(t, nil, func(srv *confServer, f *file) {
		ctx := context.Background()

		// Write unpaged, read back paged.
		if _, err := f.WriteAt(payload, 0); err != nil {
			t.Fatalf("WriteAt: %v", err)
		}
		got := make([]byte, len(payload))
		if _, err := f.PgReadAt(ctx, got, 0); err != nil {
			t.Fatalf("PgReadAt: %v", err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatal("pgread of an unpaged write differs")
		}

		// Write paged at an unaligned offset, read back unpaged.
		if err := f.PgWriteAt(ctx, payload, 17); err != nil {
			t.Fatalf("PgWriteAt: %v", err)
		}
		got = make([]byte, len(payload))
		if _, err := f.ReadAt(got, 17); err != nil {
			t.Fatalf("ReadAt: %v", err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatal("read of an unaligned paged write differs")
		}
	})
	srv.check(t)

	if n := srv.opCount(read.RequestID); n == 0 {
		t.Fatal("no kXR_read was issued")
	}
}

// TestConformance_CloseVerify_CarriesTheExpectedSize: CloseVerify is the only
// way a client states how long it believes the file is, and the statement
// travels in the size field of kXR_close. A client that sends a plain close
// instead reports a truncated upload as a successful one.
func TestConformance_CloseVerify_CarriesTheExpectedSize(t *testing.T) {
	srv := confClient(t, confContent, func(srv *confServer, f *file) {
		if err := f.CloseVerify(context.Background(), int64(len(confContent))); err != nil {
			t.Fatalf("CloseVerify: %v", err)
		}
	})
	srv.check(t)

	if got, want := srv.lastCloseSize(), int64(len(confContent)); got != want {
		t.Fatalf("kXR_close carried size %d, want %d", got, want)
	}
}

// TestConformance_Close_SendsNoSize: a plain Close must leave the size field
// zero. A non-zero value there is a claim about the file, and a server that
// honours it would reject the close of a file that is any other length.
func TestConformance_Close_SendsNoSize(t *testing.T) {
	srv := confClient(t, confContent, func(srv *confServer, f *file) {
		if err := f.Close(context.Background()); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	srv.check(t)

	if got := srv.lastCloseSize(); got != 0 {
		t.Fatalf("a plain kXR_close carried size %d, want 0", got)
	}
}

// TestConformance_CloseVerify_MismatchIsReported: the point of the size field
// is that the server can disagree. That disagreement is the last chance to
// notice a short upload, so it must reach the caller.
func TestConformance_CloseVerify_MismatchIsReported(t *testing.T) {
	srv := confClient(t, confContent, func(srv *confServer, f *file) {
		srv.set(func(srv *confServer) { srv.verifyClose = true })
		err := f.CloseVerify(context.Background(), int64(len(confContent))-1)
		wantErr(t, err, "CloseVerify", "not")
	})
	srv.check(t)

	if got, want := srv.lastCloseSize(), int64(len(confContent))-1; got != want {
		t.Fatalf("kXR_close carried size %d, want %d", got, want)
	}
}
