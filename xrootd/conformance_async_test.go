// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the responses that are not answers: kXR_wait, kXR_waitresp,
// kXR_attn and kXR_status.
//
// XRootD multiplexes every request over one connection and keeps the stream
// open across frames that carry no result. A server under load parks a request
// with kXR_wait and expects it re-sent later; a manager that has to ask a data
// server first answers kXR_waitresp and delivers the real reply, unprompted,
// inside a kXR_attn — possibly seconds later and possibly to a stream the
// client has been holding open all along.
//
// Each of these is a chance to hang or to lie. A client that treats a wait as
// an answer returns success for work never done; one that closes the stream on
// a placeholder loses the delayed reply and blocks the caller forever; one that
// dispatches an unsolicited frame without checking whose stream it names lets
// an unauthenticated server complete somebody else's request. These pin what
// the session does with each of them.

package xrootd

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdproto"
)

// asyncStream reads one request off conn and reports the stream it arrived on.
func asyncStream(t *testing.T, conn net.Conn) xrdproto.StreamID {
	t.Helper()

	data, err := xrdproto.ReadRequest(conn)
	if err != nil {
		t.Errorf("could not read the request: %v", err)
		return xrdproto.StreamID{}
	}
	var sid xrdproto.StreamID
	copy(sid[:], data[:2])
	return sid
}

// asyncAttn wraps a response header and its data in a kXR_attn body carrying
// the kXR_asynresp action code.
func asyncAttn(action int32, sid xrdproto.StreamID, status uint16, data []byte) []byte {
	body := make([]byte, 8)
	binary.BigEndian.PutUint32(body[:4], uint32(action))
	inner := make([]byte, xrdproto.ResponseHeaderLength)
	copy(inner[0:2], sid[:])
	binary.BigEndian.PutUint16(inner[2:4], status)
	binary.BigEndian.PutUint32(inner[4:8], uint32(len(data)))
	body = append(body, inner...)
	return append(body, data...)
}

// asyncServerError encodes a kXR_error body: the code and a NUL-terminated
// message, as the server sends it.
func asyncServerError(code xrdproto.ServerErrorCode, msg string) []byte {
	body := make([]byte, 4)
	binary.BigEndian.PutUint32(body, uint32(code))
	body = append(body, msg...)
	return append(body, 0)
}

// asyncRemove is the request every test here uses: the smallest one whose
// reply is an empty kXR_ok, so nothing but the framing is under test.
func asyncRemove(t *testing.T) func(cancel func(), client *Client) {
	t.Helper()

	return func(cancel func(), client *Client) {
		if err := client.FS().RemoveFile(context.Background(), "/tmp/f.bin"); err != nil {
			t.Errorf("the request failed: %v", err)
		}
	}
}

func TestConformance_AWaitTooShortToDecodeFailsTheRequest(t *testing.T) {
	// The wait says how long to hold off and the body is too short to say.
	// Reading zero out of it would re-send the request immediately, which is
	// the opposite of what the server asked for — and against a server that is
	// shedding load, a client that retries at once is the load.
	serverFunc := func(cancel func(), conn net.Conn) {
		sid := asyncStream(t, conn)
		if _, err := conn.Write(append(confRespHdr(sid, uint16(xrdproto.Wait), 2), 0, 0)); err != nil {
			t.Errorf("could not write the wait response: %v", err)
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		if err := client.FS().RemoveFile(context.Background(), "/tmp/f.bin"); err == nil {
			t.Error("a truncated wait was accepted as an answer")
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}

func TestConformance_AWaitForARequestNobodyMadeDoesNotStopTheSession(t *testing.T) {
	// A wait naming a stream with no request behind it cannot be re-issued —
	// there is nothing to re-issue. It must not take the session down with it:
	// every other request on this connection is still outstanding.
	serverFunc := func(cancel func(), conn net.Conn) {
		sid := asyncStream(t, conn)

		wait := make([]byte, 4) // a duration of zero seconds
		if _, err := conn.Write(append(confRespHdr(xrdproto.StreamID{9, 9}, uint16(xrdproto.Wait), 4), wait...)); err != nil {
			t.Errorf("could not write the stray wait: %v", err)
			return
		}
		if _, err := conn.Write(confRespHdr(sid, uint16(xrdproto.Ok), 0)); err != nil {
			t.Errorf("could not write the response: %v", err)
		}
	}

	testClientWithMockServer(serverFunc, asyncRemove(t))
}

func TestConformance_ADeferredAnswerIsWaitedForRatherThanDelivered(t *testing.T) {
	// kXR_waitresp is a promise, not a reply. Handing it to the caller would
	// report success before the manager has even asked the data server; closing
	// the stream would drop the answer when it arrives.
	serverFunc := func(cancel func(), conn net.Conn) {
		sid := asyncStream(t, conn)

		placeholder := make([]byte, 4)
		if _, err := conn.Write(append(confRespHdr(sid, uint16(xrdproto.WaitResp), 4), placeholder...)); err != nil {
			t.Errorf("could not write the placeholder: %v", err)
			return
		}
		if _, err := conn.Write(confRespHdr(sid, uint16(xrdproto.Ok), 0)); err != nil {
			t.Errorf("could not write the response: %v", err)
		}
	}

	testClientWithMockServer(serverFunc, asyncRemove(t))
}

func TestConformance_AnAttentionThatCarriesNoDelayedResponseIsIgnored(t *testing.T) {
	// Attentions are also used to broadcast messages and to ask clients to
	// reconnect. Anything that is not a delayed response is not addressed to a
	// stream, and reading a stream ID out of it would deliver noise to whatever
	// request happens to be holding that ID.
	serverFunc := func(cancel func(), conn net.Conn) {
		sid := asyncStream(t, conn)

		// Too short to hold a response header at all.
		if _, err := conn.Write(append(confRespHdr(xrdproto.StreamID{0, 0}, uint16(xrdproto.Attn), 4), 0, 0, 0, 0)); err != nil {
			t.Errorf("could not write the short attention: %v", err)
			return
		}
		// Well-formed, but announcing something other than a delayed response.
		other := asyncAttn(5001, sid, uint16(xrdproto.Error), asyncServerError(xrdproto.IOError, "not for you"))
		if _, err := conn.Write(append(confRespHdr(xrdproto.StreamID{0, 0}, uint16(xrdproto.Attn), len(other)), other...)); err != nil {
			t.Errorf("could not write the attention: %v", err)
			return
		}
		if _, err := conn.Write(confRespHdr(sid, uint16(xrdproto.Ok), 0)); err != nil {
			t.Errorf("could not write the response: %v", err)
		}
	}

	testClientWithMockServer(serverFunc, asyncRemove(t))
}

func TestConformance_ADelayedAnswerArrivesInsideAnAttention(t *testing.T) {
	// The real reply to a kXR_waitresp comes back wrapped in an attention on
	// stream {0,0}, naming the stream it belongs to. Nothing else will arrive
	// for that request, so a client that ignores it waits forever.
	serverFunc := func(cancel func(), conn net.Conn) {
		sid := asyncStream(t, conn)

		placeholder := make([]byte, 4)
		if _, err := conn.Write(append(confRespHdr(sid, uint16(xrdproto.WaitResp), 4), placeholder...)); err != nil {
			t.Errorf("could not write the placeholder: %v", err)
			return
		}
		body := asyncAttn(xrdproto.AsyncResp, sid, uint16(xrdproto.Ok), nil)
		if _, err := conn.Write(append(confRespHdr(xrdproto.StreamID{0, 0}, uint16(xrdproto.Attn), len(body)), body...)); err != nil {
			t.Errorf("could not write the delayed response: %v", err)
		}
	}

	testClientWithMockServer(serverFunc, asyncRemove(t))
}

func TestConformance_ADelayedFailureArrivesInsideAnAttention(t *testing.T) {
	// The delayed reply is a failure. It has to reach the caller as one: this
	// is the answer, and there will not be another.
	const msg = "the pool node went away"

	serverFunc := func(cancel func(), conn net.Conn) {
		sid := asyncStream(t, conn)

		body := asyncAttn(xrdproto.AsyncResp, sid, uint16(xrdproto.Error), asyncServerError(xrdproto.IOError, msg))
		if _, err := conn.Write(append(confRespHdr(xrdproto.StreamID{0, 0}, uint16(xrdproto.Attn), len(body)), body...)); err != nil {
			t.Errorf("could not write the delayed failure: %v", err)
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		err := client.FS().RemoveFile(context.Background(), "/tmp/f.bin")
		if err == nil {
			t.Error("a delayed failure was reported as success")
			return
		}
		if !strings.Contains(err.Error(), msg) {
			t.Errorf("the failure says %q, want the server's message", err)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}

func TestConformance_ADelayedRedirectionThatCannotBeParsedIsAnError(t *testing.T) {
	// A redirection is an instruction to go and connect somewhere else, so it
	// is parsed before it is obeyed — and one that arrives inside an attention
	// is no more trusted than one that arrives directly.
	serverFunc := func(cancel func(), conn net.Conn) {
		sid := asyncStream(t, conn)

		body := asyncAttn(xrdproto.AsyncResp, sid, uint16(xrdproto.Redirect), []byte{0, 1})
		if _, err := conn.Write(append(confRespHdr(xrdproto.StreamID{0, 0}, uint16(xrdproto.Attn), len(body)), body...)); err != nil {
			t.Errorf("could not write the delayed redirection: %v", err)
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		if err := client.FS().RemoveFile(context.Background(), "/tmp/f.bin"); err == nil {
			t.Error("a redirection too short to name a port was followed")
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}

func TestConformance_AStatusFrameIsCheckedBeforeItIsRead(t *testing.T) {
	// kXR_status is the only response with a checksum and with data that lives
	// outside the header's length. Both are load bearing: the length says how
	// many bytes to take off the connection, so a wrong one desynchronises
	// every response that follows, and the CRC is what says it is not wrong.
	for _, tc := range []struct {
		name  string
		frame []byte
		want  string
	}{
		{
			name:  "a frame whose checksum does not match",
			frame: append(make([]byte, 4), make([]byte, xrdproto.StatusBodyLength-4)...),
			want:  "CRC mismatch",
		},
		{
			name: "trailing data larger than any response",
			frame: xrdproto.StatusFrame(xrdproto.StatusBody{
				DataLength: xrdproto.MaxResponseLength + 1,
			}, nil),
			want: "exceeds",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serverFunc := func(cancel func(), conn net.Conn) {
				sid := asyncStream(t, conn)
				if _, err := conn.Write(append(confRespHdr(sid, uint16(xrdproto.Status), len(tc.frame)), tc.frame...)); err != nil {
					t.Errorf("could not write the status frame: %v", err)
				}
			}

			clientFunc := func(cancel func(), client *Client) {
				err := client.FS().RemoveFile(context.Background(), "/tmp/f.bin")
				if err == nil {
					t.Error("a malformed kXR_status frame was accepted")
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

func TestConformance_AStatusFrameWhoseTrailingDataNeverArrivesIsAnError(t *testing.T) {
	// The frame announces bytes that follow it on the connection and the
	// connection ends instead. Those bytes cannot be skipped and cannot be
	// invented; the request they belong to has no answer.
	serverFunc := func(cancel func(), conn net.Conn) {
		sid := asyncStream(t, conn)

		frame := xrdproto.StatusFrame(xrdproto.StatusBody{StreamID: sid, DataLength: 64}, nil)
		if _, err := conn.Write(append(confRespHdr(sid, uint16(xrdproto.Status), len(frame)), frame...)); err != nil {
			t.Errorf("could not write the status frame: %v", err)
			return
		}
		conn.Close()
	}

	clientFunc := func(cancel func(), client *Client) {
		if err := client.FS().RemoveFile(context.Background(), "/tmp/f.bin"); err == nil {
			t.Error("a status frame whose data never arrived was accepted")
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}
