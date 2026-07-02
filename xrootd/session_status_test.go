// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"net"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/ping"
)

// rawStatusResponse is a Response that captures the raw bytes handed back by
// the session layer.
type rawStatusResponse struct{ data []byte }

func (r *rawStatusResponse) UnmarshalXrd(rBuffer *xrdenc.RBuffer) error {
	r.data = rBuffer.Bytes()
	return nil
}

func (r *rawStatusResponse) MarshalXrd(wBuffer *xrdenc.WBuffer) error {
	wBuffer.WriteBytes(r.data)
	return nil
}

func (r *rawStatusResponse) RespID() uint16 { return ping.RequestID }

func TestConsumeStatusFrames(t *testing.T) {
	trailing1 := []byte("0123456789") // partial frame payload
	trailing2 := []byte("abcdef")     // final frame payload

	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			t.Errorf("could not read request: %v", err)
			return
		}
		var hdr xrdproto.RequestHeader
		_ = hdr.UnmarshalXrd(xrdenc.NewRBuffer(data[:xrdproto.RequestHeaderLength]))

		writeStatus := func(resptype uint8, trailing []byte) {
			frame := xrdproto.StatusFrame(xrdproto.StatusBody{
				StreamID:   hdr.StreamID,
				RespType:   resptype,
				DataLength: int32(len(trailing)),
			}, nil)
			respHdr := xrdproto.ResponseHeader{StreamID: hdr.StreamID, Status: xrdproto.Status, DataLength: int32(len(frame))}
			var w xrdenc.WBuffer
			_ = respHdr.MarshalXrd(&w)
			out := append(w.Bytes(), frame...)
			out = append(out, trailing...) // trailing data lives OUTSIDE the header dlen
			if _, err := conn.Write(out); err != nil {
				cancel()
			}
		}
		writeStatus(xrdproto.PartialResult, trailing1)
		writeStatus(xrdproto.FinalResult, trailing2)
	}

	clientFunc := func(cancel func(), client *Client) {
		var resp rawStatusResponse
		_, err := client.Send(context.Background(), &resp, &ping.Request{})
		if err != nil {
			t.Fatalf("Send: %v", err)
		}
		want := xrdproto.StatusBodyLength + len(trailing1) + xrdproto.StatusBodyLength + len(trailing2)
		if len(resp.data) != want {
			t.Fatalf("received %d bytes, want %d (two frames with trailing data)", len(resp.data), want)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}
