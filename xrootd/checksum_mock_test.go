// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"net"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/query"
)

func TestFileSystem_Checksum_Mock(t *testing.T) {
	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		var gotReq query.Request
		gotHdr, err := unmarshalRequest(data, &gotReq)
		if err != nil {
			cancel()
			t.Errorf("bad query request: %v", err)
			return
		}
		if gotReq.Query != query.Checksum || string(gotReq.Args) != "/a/f.root" {
			cancel()
			t.Errorf("query params: query=%d args=%q", gotReq.Query, gotReq.Args)
			return
		}
		if err := xrdproto.WriteResponse(conn, gotHdr.StreamID, xrdproto.Ok, query.Response{Data: []byte("adler32 03d74a07\x00")}); err != nil {
			cancel()
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		fs := client.FS().(*fileSystem)
		algo, value, err := fs.Checksum(context.Background(), "/a/f.root")
		if err != nil {
			t.Fatalf("Checksum: %v", err)
		}
		if algo != "adler32" || value != "03d74a07" {
			t.Fatalf("Checksum: algo=%q value=%q", algo, value)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}
