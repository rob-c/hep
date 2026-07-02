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
	"go-hep.org/x/hep/xrootd/xrdproto/auth"
)

// twoRoundAuther is a test provider that completes in two rounds: the initial
// request, then one follow-up after a kXR_authmore challenge.
type twoRoundAuther struct {
	challenge []byte
}

func (*twoRoundAuther) Provider() string { return "test2" }

func (a *twoRoundAuther) Request(params []string) (*auth.Request, error) {
	return &auth.Request{Type: [4]byte{'t', 's', 't', '2'}, Credentials: "round1"}, nil
}

func (a *twoRoundAuther) More(challenge []byte) (*auth.Request, error) {
	a.challenge = append([]byte(nil), challenge...)
	return &auth.Request{Type: [4]byte{'t', 's', 't', '2'}, Credentials: "round2"}, nil
}

func TestAuthMoreMultiRound(t *testing.T) {
	prov := &twoRoundAuther{}

	serverFunc := func(cancel func(), conn net.Conn) {
		// Round 1: read auth request, reply kXR_authmore with a challenge.
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			t.Errorf("read round1: %v", err)
			return
		}
		var hdr xrdproto.RequestHeader
		_ = hdr.UnmarshalXrd(xrdenc.NewRBuffer(data[:xrdproto.RequestHeaderLength]))
		challenge := []byte("please-continue")
		if err := xrdproto.WriteResponse(conn, hdr.StreamID, xrdproto.AuthMore, rawBytes(challenge)); err != nil {
			cancel()
			return
		}

		// Round 2: read the follow-up, reply kXR_ok.
		data, err = xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			t.Errorf("read round2: %v", err)
			return
		}
		_ = hdr.UnmarshalXrd(xrdenc.NewRBuffer(data[:xrdproto.RequestHeaderLength]))
		if err := xrdproto.WriteResponse(conn, hdr.StreamID, xrdproto.Ok, nil); err != nil {
			cancel()
			return
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		sess := client.sessions[client.initialSessionID]
		req, _ := prov.Request(nil)
		if err := sess.runAuth(context.Background(), prov, req); err != nil {
			t.Fatalf("runAuth: %v", err)
		}
		if string(prov.challenge) != "please-continue" {
			t.Fatalf("provider did not receive the challenge: %q", prov.challenge)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}

// rawBytes is a Marshaler that writes fixed bytes verbatim.
type rawBytes []byte

func (r rawBytes) MarshalXrd(w *xrdenc.WBuffer) error {
	w.WriteBytes(r)
	return nil
}
