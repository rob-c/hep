// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/pgbuf"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/pgwrite"
	"go-hep.org/x/hep/xrootd/xrdproto/read"
)

// cseFor builds a pgwrite checksum-error trailer naming the given file offsets.
func cseFor(offsets ...int64) []byte {
	cse := make([]byte, pgwrite.CSEHeaderLength+8*len(offsets))
	for i, off := range offsets {
		binary.BigEndian.PutUint64(cse[pgwrite.CSEHeaderLength+8*i:], uint64(off))
	}
	return cse
}

// readPgWrite reads one pgwrite request off conn.
func readPgWrite(conn net.Conn) (xrdproto.RequestHeader, pgwrite.Request, error) {
	data, err := xrdproto.ReadRequest(conn)
	if err != nil {
		return xrdproto.RequestHeader{}, pgwrite.Request{}, err
	}
	var req pgwrite.Request
	hdr, err := unmarshalRequest(data, &req)
	return hdr, req, err
}

// TestFile_PgWriteAt_RetriesCorruptPage checks the kXR_pgRetry recovery flow:
// a page the server reports as corrupt is retransmitted on its own, with the
// retry flag set, and the write succeeds once the server accepts it.
func TestFile_PgWriteAt_RetriesCorruptPage(t *testing.T) {
	// Three pages; the server will reject the middle one.
	payload := bytes.Repeat([]byte{0x5a}, 3*pgbuf.PageSize)
	const badPage = int64(pgbuf.PageSize)

	serverFunc := func(cancel func(), conn net.Conn) {
		hdr, req, err := readPgWrite(conn)
		if err != nil {
			cancel()
			t.Errorf("bad pgwrite request: %v", err)
			return
		}
		if req.Flags != 0 {
			cancel()
			t.Errorf("first pgwrite: got flags %#x, want 0", req.Flags)
			return
		}
		// Take the write, but report the middle page as corrupt.
		if err := writePgStatus(conn, hdr.StreamID, pgwrite.RequestID, xrdproto.FinalResult, 0, cseFor(badPage)); err != nil {
			cancel()
			return
		}

		hdr, req, err = readPgWrite(conn)
		if err != nil {
			cancel()
			t.Errorf("bad pgwrite retry: %v", err)
			return
		}
		if req.Flags != pgwrite.Retry {
			cancel()
			t.Errorf("retry: got flags %#x, want %#x (kXR_pgRetry)", req.Flags, pgwrite.Retry)
			return
		}
		if req.Offset != badPage {
			cancel()
			t.Errorf("retry: got offset %d, want %d", req.Offset, badPage)
			return
		}
		// The retry must carry that page alone, not the whole request again.
		if len(req.Data) != pgbuf.PageSize {
			cancel()
			t.Errorf("retry: got %d bytes, want one page (%d)", len(req.Data), pgbuf.PageSize)
			return
		}
		if err := writePgStatus(conn, hdr.StreamID, pgwrite.RequestID, xrdproto.FinalResult, badPage, nil); err != nil {
			cancel()
			return
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		f := file{fs: client.FS().(*fileSystem), handle: xrdfs.FileHandle{0, 0, 0, 0}, sessionID: client.initialSessionID}
		if err := f.PgWriteAt(context.Background(), payload, 0); err != nil {
			t.Fatalf("PgWriteAt: %v", err)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}

// TestFile_PgWriteAt_RetryBudget checks that a page which stays corrupt fails
// the write instead of retrying forever. Silently returning success would
// leave corrupt data on the server that the caller believes it wrote.
func TestFile_PgWriteAt_RetryBudget(t *testing.T) {
	payload := bytes.Repeat([]byte{0x33}, pgbuf.PageSize)

	serverFunc := func(cancel func(), conn net.Conn) {
		// Answer every attempt — the initial write and each retry — with the
		// same corrupt-page report.
		for range maxPgRetries + 1 {
			hdr, _, err := readPgWrite(conn)
			if err != nil {
				cancel()
				return
			}
			if err := writePgStatus(conn, hdr.StreamID, pgwrite.RequestID, xrdproto.FinalResult, 0, cseFor(0)); err != nil {
				cancel()
				return
			}
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		f := file{fs: client.FS().(*fileSystem), handle: xrdfs.FileHandle{0, 0, 0, 0}, sessionID: client.initialSessionID}
		err := f.PgWriteAt(context.Background(), payload, 0)
		if err == nil {
			t.Fatal("PgWriteAt: got no error, want a corrupt-page failure")
		}
		if !strings.Contains(err.Error(), "still corrupt") {
			t.Fatalf("PgWriteAt: got %q, want a corrupt-page failure", err)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}

// TestFile_PgWriteAt_CorruptPageOutsideRequest checks that a server naming a
// page the client never sent is refused rather than slicing out of bounds.
func TestFile_PgWriteAt_CorruptPageOutsideRequest(t *testing.T) {
	payload := bytes.Repeat([]byte{0x77}, pgbuf.PageSize)

	serverFunc := func(cancel func(), conn net.Conn) {
		hdr, _, err := readPgWrite(conn)
		if err != nil {
			cancel()
			return
		}
		// The request covered [0, 4096); point far outside it.
		if err := writePgStatus(conn, hdr.StreamID, pgwrite.RequestID, xrdproto.FinalResult, 0, cseFor(1<<40)); err != nil {
			cancel()
			return
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		f := file{fs: client.FS().(*fileSystem), handle: xrdfs.FileHandle{0, 0, 0, 0}, sessionID: client.initialSessionID}
		err := f.PgWriteAt(context.Background(), payload, 0)
		if err == nil {
			t.Fatal("PgWriteAt: got no error, want a rejection of the out-of-range page")
		}
		if !strings.Contains(err.Error(), "outside") {
			t.Fatalf("PgWriteAt: got %q, want an out-of-range rejection", err)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}

// TestFile_ReadAt_OverlongResponse checks that a server answering with more
// data than the read asked for is cut off. Each frame is small enough to pass
// the per-frame limit; only the per-request bound stops the accumulation.
func TestFile_ReadAt_OverlongResponse(t *testing.T) {
	const want = 8

	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		var req read.Request
		hdr, err := unmarshalRequest(data, &req)
		if err != nil {
			cancel()
			t.Errorf("bad read request: %v", err)
			return
		}
		// Answer with the requested bytes, then keep going.
		chunk := bytes.Repeat([]byte{0x01}, want)
		for range 4 {
			if err := xrdproto.WriteResponse(conn, hdr.StreamID, xrdproto.OkSoFar, read.Response{Data: chunk}); err != nil {
				cancel()
				return
			}
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		f := file{fs: client.FS().(*fileSystem), handle: xrdfs.FileHandle{0, 0, 0, 0}, sessionID: client.initialSessionID}
		got := make([]byte, want)
		_, err := f.ReadAt(got, 0)
		if err == nil {
			t.Fatal("ReadAt: got no error, want the over-answering server to be refused")
		}
		if !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("ReadAt: got %q, want a response-limit error", err)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}
