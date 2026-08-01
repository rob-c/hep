// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the file operations whose contract is what the *server* is
// allowed to send back.
//
// A vector read is the one request where the client cannot check the answer by
// its length alone: kXR_readv returns a stream of chunks, each labelled with
// the handle and offset it belongs to, and the client hands them back to the
// caller as an ordered slice. If a server mislabels one — or a redirector
// splices in a reply from a different file — an unchecked client returns the
// wrong bytes under the right offset, and nothing downstream can tell. So the
// labels are verified against the request, and any disagreement is an error
// rather than data.
//
// These drive a scripted server over a pipe, because the point is to send
// replies a correct server would never send.

package xrootd

import (
	"context"
	"net"
	"reflect"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/readv"
	"go-hep.org/x/hep/xrootd/xrdproto/sigver"
	"go-hep.org/x/hep/xrootd/xrdproto/stat"
	"go-hep.org/x/hep/xrootd/xrdproto/write"
)

func TestConformance_AVirtualStatOfAFileGoesOutOnItsHandle(t *testing.T) {
	// kXR_stat with kXR_vfs against an open file asks the server that is holding
	// it about the space it lives in — which is how a client decides whether a
	// write is worth starting. The handle has to travel with the request: sent
	// without one, the answer describes whatever the server considers its
	// default export.
	t.Parallel()

	handle := xrdfs.FileHandle{1, 2, 3, 4}
	want := xrdfs.VirtualFSStat{
		NumberRW:           1,
		FreeRW:             100,
		UtilizationRW:      10,
		NumberStaging:      2,
		FreeStaging:        200,
		UtilizationStaging: 20,
	}
	wantRequest := stat.Request{FileHandle: handle, Options: stat.OptionsVFS}

	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			t.Errorf("could not read the request: %v", err)
			return
		}

		var got stat.Request
		hdr, err := unmarshalRequest(data, &got)
		if err != nil {
			cancel()
			t.Errorf("could not unmarshal the request: %v", err)
			return
		}
		if !reflect.DeepEqual(got, wantRequest) {
			cancel()
			t.Errorf("the request does not match:\ngot  = %v\nwant = %v", got, wantRequest)
			return
		}

		if err := xrdproto.WriteResponse(conn, hdr.StreamID, xrdproto.Ok, stat.VirtualFSResponse{VirtualFSStat: want}); err != nil {
			cancel()
			t.Errorf("could not write the response: %v", err)
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		f := file{fs: client.FS().(*fileSystem), handle: handle, sessionID: client.initialSessionID}

		got, err := f.StatVirtualFS(context.Background())
		if err != nil {
			t.Errorf("could not stat the virtual filesystem: %v", err)
			return
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("the virtual stat does not match:\ngot  = %v\nwant = %v", got, want)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}

func TestConformance_AVirtualStatThatFailsIsNotAnEmptyOne(t *testing.T) {
	// A server that does not keep space accounting answers kXR_error. Returning
	// the zero VirtualFSStat with a nil error would tell the caller the storage
	// element has no free space and no read-write partitions — a client sizing a
	// transfer against that refuses a write that would have succeeded.
	t.Parallel()

	handle := xrdfs.FileHandle{1, 2, 3, 4}

	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			t.Errorf("could not read the request: %v", err)
			return
		}
		hdr, err := unmarshalRequest(data, &stat.Request{})
		if err != nil {
			cancel()
			t.Errorf("could not unmarshal the request: %v", err)
			return
		}

		srvErr := xrdproto.ServerError{Code: xrdproto.InvalidRequest, Message: "no virtual filesystem here"}
		if err := xrdproto.WriteResponse(conn, hdr.StreamID, xrdproto.Error, srvErr); err != nil {
			cancel()
			t.Errorf("could not write the response: %v", err)
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		f := file{fs: client.FS().(*fileSystem), handle: handle, sessionID: client.initialSessionID}

		got, err := f.StatVirtualFS(context.Background())
		if err == nil {
			t.Errorf("a refused virtual stat returned %+v", got)
			return
		}
		if got != (xrdfs.VirtualFSStat{}) {
			t.Errorf("a failed virtual stat still returned %+v", got)
		}
		if !strings.Contains(err.Error(), "no virtual filesystem here") {
			t.Errorf("the failure lost the server's reason: %v", err)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}

func TestConformance_AVectorReadIsCheckedAgainstWhatWasAsked(t *testing.T) {
	// Each case is a reply a broken or confused server could send, and each one
	// would otherwise be handed to the caller as if it were the data requested.
	handle := xrdfs.FileHandle{1, 2, 3, 4}
	other := xrdfs.FileHandle{9, 9, 9, 9}
	segs := []xrdfs.ReadVSegment{
		{Offset: 0, Length: 4},
		{Offset: 64, Length: 4},
	}

	for _, tc := range []struct {
		name   string
		chunks []readv.Chunk
		want   string
	}{
		{
			"a chunk is missing",
			[]readv.Chunk{{Handle: handle, Offset: 0, Data: []byte("aaaa")}},
			"asked for 2 segments and got 1 back",
		},
		{
			"a chunk too many",
			[]readv.Chunk{
				{Handle: handle, Offset: 0, Data: []byte("aaaa")},
				{Handle: handle, Offset: 64, Data: []byte("bbbb")},
				{Handle: handle, Offset: 128, Data: []byte("cccc")},
			},
			// Caught even earlier: a kXR_readv reply is bounded by the length
			// the request asked for, so the extra chunk is refused as an
			// over-long response before it is ever decoded.
			"response exceeds the 40-byte limit",
		},
		{
			"a chunk for another file",
			[]readv.Chunk{
				{Handle: handle, Offset: 0, Data: []byte("aaaa")},
				{Handle: other, Offset: 64, Data: []byte("bbbb")},
			},
			"came back for handle",
		},
		{
			"a chunk for another offset",
			[]readv.Chunk{
				{Handle: handle, Offset: 0, Data: []byte("aaaa")},
				{Handle: handle, Offset: 4096, Data: []byte("bbbb")},
			},
			"came back for offset",
		},
		{
			"a chunk of the wrong length",
			[]readv.Chunk{
				{Handle: handle, Offset: 0, Data: []byte("aaaa")},
				{Handle: handle, Offset: 64, Data: []byte("bb")},
			},
			"came back with 2 bytes",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			serverFunc := func(cancel func(), conn net.Conn) {
				data, err := xrdproto.ReadRequest(conn)
				if err != nil {
					cancel()
					t.Errorf("could not read the request: %v", err)
					return
				}
				hdr, err := unmarshalRequest(data, &readv.Request{})
				if err != nil {
					cancel()
					t.Errorf("could not unmarshal the request: %v", err)
					return
				}

				if err := xrdproto.WriteResponse(conn, hdr.StreamID, xrdproto.Ok, readv.Response{Chunks: tc.chunks}); err != nil {
					cancel()
					t.Errorf("could not write the response: %v", err)
				}
			}

			clientFunc := func(cancel func(), client *Client) {
				f := file{fs: client.FS().(*fileSystem), handle: handle, sessionID: client.initialSessionID}

				got, err := f.ReadVAt(context.Background(), segs)
				if err == nil {
					t.Errorf("a mismatched vector read returned %q", got)
					return
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Errorf("the failure says %q, want it to mention %q", err, tc.want)
				}
			}

			testClientWithMockServer(serverFunc, clientFunc)
		})
	}
}

func TestConformance_ASignedRequestIsPrefixedByItsSignature(t *testing.T) {
	// kXR_sigver travels as its own request immediately ahead of the one it
	// covers, on the same stream, in the same write. A client that sent them as
	// two writes, or reordered them, would have the server reject a request that
	// was signed correctly — and a client that reused a sequence number would
	// have it rejected as a replay.
	sess := &cliSession{signKey: []byte("0123456789abcdef")}
	streamID := xrdproto.StreamID{0x1a, 0x2b}
	// kXR_write is signed over its 24-byte header only, so the payload handed
	// to sign has to be at least that long — which every marshalled request is.
	payload := []byte("the marshalled request, header and all")

	frame, err := sess.sign(streamID, write.RequestID, payload)
	if err != nil {
		t.Fatalf("could not sign a request: %v", err)
	}

	if n := len(frame); n <= len(payload) {
		t.Fatalf("the signed frame is %d bytes, which cannot hold the %d-byte request", n, len(payload))
	}
	if got := frame[len(frame)-len(payload):]; string(got) != string(payload) {
		t.Fatalf("the request does not follow its signature: %q", got)
	}

	var hdr xrdproto.RequestHeader
	if err := hdr.UnmarshalXrd(xrdenc.NewRBuffer(frame[:xrdproto.RequestHeaderLength])); err != nil {
		t.Fatalf("could not unmarshal the header: %v", err)
	}
	if hdr.StreamID != streamID {
		t.Fatalf("the signature went out on stream %v, want %v", hdr.StreamID, streamID)
	}
	if hdr.RequestID != sigver.RequestID {
		t.Fatalf("the leading request is %d, want kXR_sigver (%d)", hdr.RequestID, sigver.RequestID)
	}

	var sig sigver.Request
	if err := sig.UnmarshalXrd(xrdenc.NewRBuffer(frame[xrdproto.RequestHeaderLength:])); err != nil {
		t.Fatalf("could not unmarshal the signature: %v", err)
	}
	if sig.ID != write.RequestID {
		t.Fatalf("the signature covers request %d, want %d", sig.ID, write.RequestID)
	}
	if sig.SeqID != 1 {
		t.Fatalf("the first signature carries sequence %d, want 1", sig.SeqID)
	}
	// kXR_write carries its payload out of band, so only the header is hashed
	// and the server is told so.
	if sig.Flags&sigver.NoData == 0 {
		t.Fatal("a signed kXR_write did not set kXR_nodata")
	}
	if len(sig.Signature) == 0 {
		t.Fatal("the signature is empty")
	}

	// The sequence number is what makes a captured request useless to replay,
	// so it has to move on every signature.
	next, err := sess.sign(streamID, write.RequestID, payload)
	if err != nil {
		t.Fatalf("could not sign a second request: %v", err)
	}
	var sig2 sigver.Request
	if err := sig2.UnmarshalXrd(xrdenc.NewRBuffer(next[xrdproto.RequestHeaderLength:])); err != nil {
		t.Fatalf("could not unmarshal the second signature: %v", err)
	}
	if sig2.SeqID <= sig.SeqID {
		t.Fatalf("the second signature carries sequence %d, which does not follow %d", sig2.SeqID, sig.SeqID)
	}
}
