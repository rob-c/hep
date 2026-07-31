// Copyright ©2025 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package protocol

import (
	"reflect"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

// tlsFlags is every TLS capability a server can report, with the bit the
// specification gives it (XRdv310.pdf, kXR_protocol response flags) and the
// accessor that is supposed to read it.
var tlsFlags = []struct {
	name string
	bit  uint32
	got  func(*Response) bool
}{
	{"kXR_haveTLS", 0x80000000, (*Response).HasTLS},
	{"kXR_gotoTLS", 0x40000000, (*Response).GotoTLS},
	{"kXR_tlsData", 0x01000000, (*Response).TLSForData},
	{"kXR_tlsGPF", 0x02000000, nil},
	{"kXR_tlsLogin", 0x04000000, (*Response).TLSForLogin},
	{"kXR_tlsSess", 0x08000000, (*Response).TLSForSession},
	{"kXR_tlsTPC", 0x10000000, (*Response).TLSForTPC},
	{"kXR_tlsGPFA", 0x20000000, nil},
}

func TestConformance_TLSFlagBitsMatchTheSpecification(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  uint32
		want uint32
	}{
		{"kXR_haveTLS", flagHaveTLS, 0x80000000},
		{"kXR_gotoTLS", flagGotoTLS, 0x40000000},
		{"kXR_tlsData", flagTLSData, 0x01000000},
		{"kXR_tlsGPF", flagTLSGPF, 0x02000000},
		{"kXR_tlsLogin", flagTLSLogin, 0x04000000},
		{"kXR_tlsSess", flagTLSSess, 0x08000000},
		{"kXR_tlsTPC", flagTLSTPC, 0x10000000},
		{"kXR_tlsGPFA", flagTLSGPFA, 0x20000000},
	} {
		if tc.got != tc.want {
			t.Errorf("%s is %#x, want %#x", tc.name, tc.got, tc.want)
		}
	}
}

// TestConformance_EveryTLSCapabilityIsReadFromItsOwnBit sets one flag at a
// time and requires every accessor but the one it belongs to to stay false.
//
// These are single bits in the same byte, and an accessor that reads its
// neighbour is not a visible failure: the client keeps working, over a
// connection it believes the server asked it to encrypt and did not.
func TestConformance_EveryTLSCapabilityIsReadFromItsOwnBit(t *testing.T) {
	for _, set := range tlsFlags {
		if set.got == nil {
			continue // no accessor: gpfile is not a surface this client has.
		}
		t.Run(set.name, func(t *testing.T) {
			resp := &Response{Flags: Flags(int32(set.bit))}
			for _, read := range tlsFlags {
				if read.got == nil {
					continue
				}
				want := read.name == set.name
				if got := read.got(resp); got != want {
					t.Errorf("with only %s set, %s reports %v, want %v", set.name, read.name, got, want)
				}
			}
		})
	}
}

// TestConformance_ResponseRoundTripsWithItsSecurityTrailer covers the response
// shape a signing-capable server sends: the flags, then the "S" trailer with
// the security level and the per-request overrides that relax it.
func TestConformance_ResponseRoundTripsWithItsSecurityTrailer(t *testing.T) {
	for _, tc := range []struct {
		name string
		resp Response
	}{
		{
			name: "without security info",
			resp: Response{BinaryProtocolVersion: 0x310, Flags: IsServer},
		},
		{
			name: "with security info",
			resp: Response{
				BinaryProtocolVersion: 0x310,
				Flags:                 IsServer | IsManager,
				HasSecurityInfo:       true,
				SecurityVersion:       1,
				SecurityOptions:       ForceSecurity,
				SecurityLevel:         xrdproto.Standard,
				SecurityOverrides: []xrdproto.SecurityOverride{
					{RequestIndex: 19, RequestLevel: xrdproto.SignNone},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var w xrdenc.WBuffer
			if err := tc.resp.MarshalXrd(&w); err != nil {
				t.Fatalf("could not marshal response: %v", err)
			}

			var got Response
			if err := got.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
				t.Fatalf("could not unmarshal response: %v", err)
			}

			want := tc.resp
			if !tc.resp.HasSecurityInfo {
				// An empty override list decodes as an empty, non-nil slice.
				want.SecurityOverrides = got.SecurityOverrides
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("round trip changed the response:\ngot = %+v\nwant= %+v", got, want)
			}
		})
	}
}
