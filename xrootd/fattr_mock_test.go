// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"net"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/fattr"
)

func TestFileSystem_GetXAttr_Mock(t *testing.T) {
	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		var gotReq fattr.Request
		gotHdr, err := unmarshalRequest(data, &gotReq)
		if err != nil || gotReq.Subcode != fattr.Get {
			cancel()
			t.Errorf("bad fattr request: subcode=%d err=%v", gotReq.Subcode, err)
			return
		}
		// reply: errcount=0 numattr=1 [rc=0]["user.tag\0"][len=3]["abc"]
		raw := []byte{0, 1, 0, 0}
		raw = append(raw, []byte("user.tag\x00")...)
		raw = append(raw, 0, 0, 0, 3, 'a', 'b', 'c')
		if err := xrdproto.WriteResponse(conn, gotHdr.StreamID, xrdproto.Ok, fattr.Response{Raw: raw}); err != nil {
			cancel()
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		fs := client.FS().(*fileSystem)
		value, err := fs.GetXAttr(context.Background(), "/a/f.root", "user.tag")
		if err != nil {
			t.Fatalf("GetXAttr: %v", err)
		}
		if string(value) != "abc" {
			t.Fatalf("GetXAttr value: %q", value)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}

func TestFileSystem_ListXAttr_Mock(t *testing.T) {
	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		var gotReq fattr.Request
		gotHdr, err := unmarshalRequest(data, &gotReq)
		if err != nil || gotReq.Subcode != fattr.List {
			cancel()
			t.Errorf("bad fattr request: subcode=%d err=%v", gotReq.Subcode, err)
			return
		}
		if err := xrdproto.WriteResponse(conn, gotHdr.StreamID, xrdproto.Ok, fattr.Response{Raw: []byte("user.a\x00user.b\x00")}); err != nil {
			cancel()
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		fs := client.FS().(*fileSystem)
		names, err := fs.ListXAttr(context.Background(), "/a")
		if err != nil {
			t.Fatalf("ListXAttr: %v", err)
		}
		if len(names) != 2 || names[0] != "user.a" || names[1] != "user.b" {
			t.Fatalf("ListXAttr: %v", names)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}
