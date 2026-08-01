// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"reflect"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/chkpoint"
	"go-hep.org/x/hep/xrootd/xrdproto/truncate"
	"go-hep.org/x/hep/xrootd/xrdproto/write"
)

// The frame layout a checkpoint request has to have, named here because every
// test below reads the same bytes out of it.
const (
	frameParams  = xrdproto.RequestHeaderLength      // where the 16 parameter bytes start
	frameSubCode = frameParams + 15                  // the sub-code is the LAST parameter byte
	frameDataLen = frameParams + 16                  // the 4-byte data length
	frameBody    = xrdproto.RequestHeaderLength + 20 // where the data begins
)

func TestConformance_ACheckpointSubCodeIsTheLastParameterByte(t *testing.T) {
	// The sub-code is what tells a commit from a rollback, and it sits at the
	// end of the parameter area rather than the beginning. Putting it first
	// leaves it reading as zero — kXR_ckpBegin — so a rollback would open a
	// checkpoint instead of undoing one, and the writes it was meant to discard
	// would stay.
	handle := xrdfs.FileHandle{1, 2, 3, 4}

	for _, tc := range []struct {
		name string
		call func(f *file) error
		code uint8
	}{
		{"begin", func(f *file) error { return f.CheckpointBegin(context.Background()) }, chkpoint.Begin},
		{"commit", func(f *file) error { return f.CheckpointCommit(context.Background()) }, chkpoint.Commit},
		{"rollback", func(f *file) error { return f.CheckpointRollback(context.Background()) }, chkpoint.Rollback},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serverFunc := func(cancel func(), conn net.Conn) {
				data, err := xrdproto.ReadRequest(conn)
				if err != nil {
					cancel()
					return
				}
				if got, want := binary.BigEndian.Uint16(data[2:4]), chkpoint.RequestID; got != want {
					cancel()
					t.Errorf("request id: got = %d, want = %d", got, want)
					return
				}
				if got := data[frameSubCode]; got != tc.code {
					cancel()
					t.Errorf("sub-code byte: got = %d, want = %d", got, tc.code)
					return
				}
				if got := data[frameParams : frameParams+4]; !reflect.DeepEqual(got, handle[:]) {
					cancel()
					t.Errorf("file handle: got = %v, want = %v", got, handle)
					return
				}
				// Everything between the handle and the sub-code is reserved,
				// and a server is entitled to reject a request that fills it.
				for i, b := range data[frameParams+4 : frameSubCode] {
					if b != 0 {
						cancel()
						t.Errorf("reserved parameter byte %d is %d, want 0", i+4, b)
						return
					}
				}
				if got := binary.BigEndian.Uint32(data[frameDataLen:]); got != 0 {
					cancel()
					t.Errorf("data length: got = %d, want 0", got)
					return
				}
				if err := xrdproto.WriteResponse(conn, xrdproto.StreamID{data[0], data[1]}, xrdproto.Ok, nil); err != nil {
					cancel()
				}
			}

			clientFunc := func(cancel func(), client *Client) {
				f := file{fs: client.FS().(*fileSystem), handle: handle, sessionID: client.initialSessionID}
				if err := tc.call(&f); err != nil {
					t.Fatalf("%s: %v", tc.name, err)
				}
			}

			testClientWithMockServer(serverFunc, clientFunc)
		})
	}
}

func TestConformance_ACheckpointQuerySaysHowMuchMayBeUndone(t *testing.T) {
	handle := xrdfs.FileHandle{9, 8, 7, 6}

	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		if got := data[frameSubCode]; got != chkpoint.Query {
			cancel()
			t.Errorf("sub-code byte: got = %d, want = %d", got, chkpoint.Query)
			return
		}
		resp := chkpoint.Response{Capacity: 64 << 20, Used: 4096}
		if err := xrdproto.WriteResponse(conn, xrdproto.StreamID{data[0], data[1]}, xrdproto.Ok, resp); err != nil {
			cancel()
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		f := file{fs: client.FS().(*fileSystem), handle: handle, sessionID: client.initialSessionID}
		got, err := f.CheckpointQuery(context.Background())
		if err != nil {
			t.Fatalf("CheckpointQuery: %v", err)
		}
		want := xrdfs.CheckpointLimits{Capacity: 64 << 20, Used: 4096}
		if got != want {
			t.Fatalf("limits:\ngot = %+v\nwant = %+v", got, want)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}

func TestConformance_ACheckpointedWriteStreamsPastTheDeclaredLength(t *testing.T) {
	// kXR_ckpXeq declares only the enclosed request's 24-byte header as its
	// data length, and the enclosed payload follows the frame uncounted. A
	// client that counted the payload too would leave the server waiting for
	// bytes that never come; a server that stopped at the declared length would
	// take the first payload byte for the start of the next request. Both ends
	// have to agree, so the length is pinned here rather than inferred.
	handle := xrdfs.FileHandle{4, 3, 2, 1}
	payload := []byte("the bytes the checkpoint can undo")

	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		if got := data[frameSubCode]; got != chkpoint.Xeq {
			cancel()
			t.Errorf("sub-code byte: got = %d, want = %d", got, chkpoint.Xeq)
			return
		}
		if got, want := binary.BigEndian.Uint32(data[frameDataLen:]), uint32(xrdproto.RequestHeaderLength+20); got != want {
			cancel()
			t.Errorf("data length: got = %d, want = %d (the enclosed header alone)", got, want)
			return
		}
		// The enclosed request has no stream id of its own: the answer comes
		// back on the outer frame's.
		enclosed := data[frameBody:]
		if got := enclosed[0:2]; got[0] != 0 || got[1] != 0 {
			cancel()
			t.Errorf("the enclosed request carries stream id %v, want none", got)
			return
		}
		// What is left on the wire is the write's own data, which the frame did
		// not account for.
		rest := make([]byte, len(payload))
		if _, err := io.ReadFull(conn, rest); err != nil {
			cancel()
			t.Errorf("could not read the enclosed payload: %v", err)
			return
		}

		var got write.Request
		if _, err := unmarshalRequest(append(enclosed, rest...), &got); err != nil {
			cancel()
			t.Errorf("could not unmarshal the enclosed write: %v", err)
			return
		}
		want := write.Request{Handle: handle, Offset: 1024, Data: payload}
		if !reflect.DeepEqual(got, want) {
			cancel()
			t.Errorf("enclosed write:\ngot = %#v\nwant = %#v", got, want)
			return
		}
		if err := xrdproto.WriteResponse(conn, xrdproto.StreamID{data[0], data[1]}, xrdproto.Ok, nil); err != nil {
			cancel()
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		f := file{fs: client.FS().(*fileSystem), handle: handle, sessionID: client.initialSessionID}
		if err := f.CheckpointWriteAt(context.Background(), payload, 1024); err != nil {
			t.Fatalf("CheckpointWriteAt: %v", err)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}

func TestConformance_ACheckpointCallReportsTheServersRefusal(t *testing.T) {
	// A server that will not open a checkpoint has to be heard: a client that
	// carried on would make its writes outside one and call a rollback that
	// undoes nothing a success.
	handle := xrdfs.FileHandle{1, 1, 1, 1}

	for _, tc := range []struct {
		name string
		call func(f *file) error
		// extra is what the request leaves on the wire past its declared data
		// length, which the server has to take before it can answer.
		extra int
	}{
		{name: "begin", call: func(f *file) error { return f.CheckpointBegin(context.Background()) }},
		{
			name:  "write",
			call:  func(f *file) error { return f.CheckpointWriteAt(context.Background(), []byte("x"), 0) },
			extra: 1,
		},
		{name: "truncate", call: func(f *file) error { return f.CheckpointTruncate(context.Background(), 0) }},
		{name: "query", call: func(f *file) error {
			limits, err := f.CheckpointQuery(context.Background())
			if err == nil && limits != (xrdfs.CheckpointLimits{}) {
				t.Errorf("a refused query returned %+v", limits)
			}
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serverFunc := func(cancel func(), conn net.Conn) {
				data, err := xrdproto.ReadRequest(conn)
				if err != nil {
					cancel()
					return
				}
				if _, err := io.ReadFull(conn, make([]byte, tc.extra)); err != nil {
					cancel()
					return
				}
				err = xrdproto.WriteResponse(conn, xrdproto.StreamID{data[0], data[1]}, xrdproto.Error,
					xrdproto.ServerError{Code: xrdproto.NotAuthorized, Message: "no"})
				if err != nil {
					cancel()
				}
			}

			testClientWithMockServer(serverFunc, func(cancel func(), client *Client) {
				f := file{fs: client.FS().(*fileSystem), handle: handle, sessionID: client.initialSessionID}
				if err := tc.call(&f); err == nil {
					t.Fatalf("a refused checkpoint %s reported success", tc.name)
				}
			})
		})
	}
}

func TestConformance_ACheckpointedTruncateEnclosesATruncate(t *testing.T) {
	handle := xrdfs.FileHandle{5, 5, 5, 5}

	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		if got := data[frameSubCode]; got != chkpoint.Xeq {
			cancel()
			t.Errorf("sub-code byte: got = %d, want = %d", got, chkpoint.Xeq)
			return
		}
		var got truncate.Request
		if _, err := unmarshalRequest(data[frameBody:], &got); err != nil {
			cancel()
			t.Errorf("could not unmarshal the enclosed truncate: %v", err)
			return
		}
		want := truncate.Request{Handle: handle, Size: 42}
		if !reflect.DeepEqual(got, want) {
			cancel()
			t.Errorf("enclosed truncate:\ngot = %#v\nwant = %#v", got, want)
			return
		}
		if err := xrdproto.WriteResponse(conn, xrdproto.StreamID{data[0], data[1]}, xrdproto.Ok, nil); err != nil {
			cancel()
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		f := file{fs: client.FS().(*fileSystem), handle: handle, sessionID: client.initialSessionID}
		if err := f.CheckpointTruncate(context.Background(), 42); err != nil {
			t.Fatalf("CheckpointTruncate: %v", err)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}
