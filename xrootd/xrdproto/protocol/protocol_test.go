// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package protocol

import (
	"encoding/binary"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

func TestNewRequestTLSOptions(t *testing.T) {
	req := NewRequestTLS(0x310, true, true, true)
	want := ReturnSecurityRequirements | AbleTLS | WantTLS
	if req.Options != want {
		t.Fatalf("options mismatch: got=%#x want=%#x", req.Options, want)
	}

	req = NewRequestTLS(0x310, true, true, false)
	want = ReturnSecurityRequirements | AbleTLS
	if req.Options != want {
		t.Fatalf("options mismatch (no wantTLS): got=%#x want=%#x", req.Options, want)
	}
}

// decodeResponse marshals a kXR_protocol response body — version, flags, then
// whatever trailer the server sent — and decodes it back.
func decodeResponse(t *testing.T, trailer []byte) *Response {
	t.Helper()
	raw := make([]byte, 8, 8+len(trailer))
	binary.BigEndian.PutUint32(raw[0:4], 0x310) // the protocol version
	binary.BigEndian.PutUint32(raw[4:8], 0)     // the flags
	raw = append(raw, trailer...)

	resp := &Response{}
	if err := resp.UnmarshalXrd(xrdenc.NewRBuffer(raw)); err != nil {
		t.Fatalf("could not unmarshal response: %v", err)
	}
	if resp.BinaryProtocolVersion != 0x310 {
		t.Fatalf("BinaryProtocolVersion = %#x, want 0x310", resp.BinaryProtocolVersion)
	}
	return resp
}

func TestResponseParsesTheSecurityRecordWhereverItSits(t *testing.T) {
	// 'S', reserved, secver, secopt, seclvl, secvsz, then one (index, level) pair.
	record := []byte{'S', 0x00, 1, 0x01, 2, 1, 3, 4}

	for _, tc := range []struct {
		name    string
		trailer []byte
	}{
		{
			name:    "spec shape, record first",
			trailer: record,
		},
		{
			name: "vendor shape, record after a security-methods header",
			// A 4-byte header [reserved, required, method count, reserved] and
			// then that many 8-byte method entries precede the record.
			trailer: append([]byte{
				0x00, 0x01, 0x02, 0x00,
				'p', 's', 's', '3', 0, 0, 0, 0,
				'z', 't', 'n', 0, 0, 0, 0, 0,
			}, record...),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := decodeResponse(t, tc.trailer)
			if !resp.HasSecurityInfo {
				t.Fatal("HasSecurityInfo = false, want true")
			}
			if resp.SecurityVersion != 1 {
				t.Fatalf("SecurityVersion = %d, want 1", resp.SecurityVersion)
			}
			if resp.SecurityLevel != xrdproto.SecurityLevel(2) {
				t.Fatalf("SecurityLevel = %d, want 2", resp.SecurityLevel)
			}
			if len(resp.SecurityOverrides) != 1 {
				t.Fatalf("got %d overrides, want 1", len(resp.SecurityOverrides))
			}
			if got := resp.SecurityOverrides[0]; got.RequestIndex != 3 || got.RequestLevel != xrdproto.RequestLevel(4) {
				t.Fatalf("override = %+v, want {RequestIndex:3 RequestLevel:4}", got)
			}
		})
	}
}

func TestResponseWithoutARecognisedRecordAsksForNoSignatures(t *testing.T) {
	// A response with no security record — or a trailer shaped in a way this
	// client does not model — must read as a server asking for no signatures,
	// the way the reference client reads it, rather than failing the handshake.
	for _, tc := range []struct {
		name    string
		trailer []byte
	}{
		{"no trailer at all", nil},
		{"a trailer we do not model", []byte{0x00, 0x01, 0x02, 0x03, 0x04}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := decodeResponse(t, tc.trailer)
			if resp.HasSecurityInfo {
				t.Fatal("HasSecurityInfo = true, want false")
			}
			if len(resp.SecurityOverrides) != 0 {
				t.Fatalf("got %d overrides, want none", len(resp.SecurityOverrides))
			}
		})
	}
}

func TestResponseTLSAccessors(t *testing.T) {
	const (
		kXRhaveTLS  = 0x80000000
		kXRgotoTLS  = 0x40000000
		kXRtlsLogin = 0x04000000
		kXRtlsData  = 0x02000000
	)
	// The conversion goes through a uint32 variable: kXR_haveTLS occupies the
	// sign bit, so a direct constant conversion to int32 would not compile.
	bits := uint32(kXRhaveTLS | kXRgotoTLS | kXRtlsLogin | kXRtlsData)
	resp := &Response{Flags: Flags(int32(bits))}

	if !resp.HasTLS() {
		t.Fatal("HasTLS() = false, want true")
	}
	if !resp.GotoTLS() {
		t.Fatal("GotoTLS() = false, want true")
	}
	if !resp.TLSForLogin() {
		t.Fatal("TLSForLogin() = false, want true")
	}
	if !resp.TLSForData() {
		t.Fatal("TLSForData() = false, want true")
	}
	if resp.TLSForTPC() {
		t.Fatal("TLSForTPC() = true, want false")
	}

	// NeedsTLS: gotoTLS forces upgrade regardless of client preference.
	if !resp.NeedsTLS(false) {
		t.Fatal("NeedsTLS(false) = false with gotoTLS set, want true")
	}

	// Server that only advertises haveTLS (no gotoTLS): upgrade only if the client wanted TLS.
	haveOnly := uint32(kXRhaveTLS)
	only := &Response{Flags: Flags(int32(haveOnly))}
	if only.NeedsTLS(false) {
		t.Fatal("NeedsTLS(false) = true with only haveTLS, want false")
	}
	if !only.NeedsTLS(true) {
		t.Fatal("NeedsTLS(true) = false with haveTLS, want true")
	}
}
