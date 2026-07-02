// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/pgbuf"
	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/pgread"
	"go-hep.org/x/hep/xrootd/xrdproto/pgwrite"
)

func writePgStatus(conn net.Conn, streamID xrdproto.StreamID, reqID uint16, resptype uint8, off int64, pages []byte) error {
	info := make([]byte, 8)
	binary.BigEndian.PutUint64(info, uint64(off))
	frame := xrdproto.StatusFrame(xrdproto.StatusBody{
		StreamID:   streamID,
		RequestID:  uint8(reqID - 3000),
		RespType:   resptype,
		DataLength: int32(len(pages)),
	}, info)
	respHdr := xrdproto.ResponseHeader{StreamID: streamID, Status: xrdproto.Status, DataLength: int32(len(frame))}
	var w xrdenc.WBuffer
	if err := respHdr.MarshalXrd(&w); err != nil {
		return err
	}
	out := append(w.Bytes(), frame...)
	out = append(out, pages...)
	_, err := conn.Write(out)
	return err
}

func TestFile_PgReadAt_Mock(t *testing.T) {
	want := bytes.Repeat([]byte{0x42}, pgbuf.PageSize+100)

	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		var gotReq pgread.Request
		gotHdr, err := unmarshalRequest(data, &gotReq)
		if err != nil {
			cancel()
			t.Errorf("bad pgread request: %v", err)
			return
		}
		if gotReq.Offset != 0 || int(gotReq.ReadLength) != len(want) {
			cancel()
			t.Errorf("pgread params: off=%d rlen=%d", gotReq.Offset, gotReq.ReadLength)
			return
		}
		// Two frames: first page, then the tail.
		if err := writePgStatus(conn, gotHdr.StreamID, pgread.RequestID, xrdproto.PartialResult, 0, pgbuf.Encode(0, want[:pgbuf.PageSize])); err != nil {
			cancel()
			return
		}
		if err := writePgStatus(conn, gotHdr.StreamID, pgread.RequestID, xrdproto.FinalResult, pgbuf.PageSize, pgbuf.Encode(pgbuf.PageSize, want[pgbuf.PageSize:])); err != nil {
			cancel()
			return
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		f := file{fs: client.FS().(*fileSystem), handle: xrdfs.FileHandle{0, 0, 0, 0}, sessionID: client.initialSessionID}
		got := make([]byte, len(want))
		n, err := f.PgReadAt(context.Background(), got, 0)
		if err != nil {
			t.Fatalf("PgReadAt: %v", err)
		}
		if n != len(want) || !bytes.Equal(got, want) {
			t.Fatalf("PgReadAt data mismatch: n=%d", n)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}

func TestFile_PgWriteAt_Mock(t *testing.T) {
	payload := bytes.Repeat([]byte{0x5a}, 100)

	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		var gotReq pgwrite.Request
		gotHdr, err := unmarshalRequest(data, &gotReq)
		if err != nil {
			cancel()
			t.Errorf("bad pgwrite request: %v", err)
			return
		}
		if !bytes.Equal(gotReq.Data, payload) || gotReq.Offset != 4096 {
			cancel()
			t.Errorf("pgwrite payload mismatch")
			return
		}
		if err := writePgStatus(conn, gotHdr.StreamID, pgwrite.RequestID, xrdproto.FinalResult, 4096, nil); err != nil {
			cancel()
			return
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		f := file{fs: client.FS().(*fileSystem), handle: xrdfs.FileHandle{0, 0, 0, 0}, sessionID: client.initialSessionID}
		if err := f.PgWriteAt(context.Background(), payload, 4096); err != nil {
			t.Fatalf("PgWriteAt: %v", err)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}
