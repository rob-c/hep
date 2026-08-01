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
	{"kXR_tlsGPF", 0x01000000, (*Response).TLSForGPFile},
	{"kXR_tlsData", 0x02000000, (*Response).TLSForData},
	{"kXR_tlsLogin", 0x04000000, (*Response).TLSForLogin},
	{"kXR_tlsSess", 0x08000000, (*Response).TLSForSession},
	{"kXR_tlsTPC", 0x10000000, (*Response).TLSForTPC},
	{"kXR_tlsGPFA", 0x20000000, (*Response).TLSForAnonGPFile},
}

func TestConformance_TLSFlagBitsMatchTheSpecification(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  uint32
		want uint32
	}{
		{"kXR_haveTLS", flagHaveTLS, 0x80000000},
		{"kXR_gotoTLS", flagGotoTLS, 0x40000000},
		{"kXR_tlsGPF", flagTLSGPF, 0x01000000},
		{"kXR_tlsData", flagTLSData, 0x02000000},
		{"kXR_tlsLogin", flagTLSLogin, 0x04000000},
		{"kXR_tlsSess", flagTLSSess, 0x08000000},
		{"kXR_tlsTPC", flagTLSTPC, 0x10000000},
		{"kXR_tlsGPFA", flagTLSGPFA, 0x20000000},
		{"kXR_tlsAny", flagTLSAny, 0x1F000000},
		{"kXR_supgpf", flagSupGPF, 0x00400000},
		{"kXR_anongpf", flagAnonGPF, 0x00800000},
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
		t.Run(set.name, func(t *testing.T) {
			resp := &Response{Flags: Flags(int32(set.bit))}
			for _, read := range tlsFlags {
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

// TestConformance_TLSForAnythingIsEveryBitThatNamesWork sets one flag at a time
// and asks whether the connection needs TLS at all.
//
// kXR_tlsAny is what a client checks before it decides to dial plain TCP, and
// it covers the bits that say what the work on the connection requires — not
// kXR_haveTLS, which says only that the server could encrypt if asked, and not
// kXR_gotoTLS, which is an instruction for this connection rather than a
// requirement of a request. Folding either of those in would make every
// TLS-capable server look like one that demands TLS.
//
// kXR_tlsGPFA is outside it too, and that is the mask the servers publish
// rather than a choice made here: it qualifies kXR_tlsGPF for unauthenticated
// callers, and on its own it asks nothing of a session that never sends the
// request it qualifies.
func TestConformance_TLSForAnythingIsEveryBitThatNamesWork(t *testing.T) {
	for _, tc := range tlsFlags {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.name != "kXR_haveTLS" && tc.name != "kXR_gotoTLS" && tc.name != "kXR_tlsGPFA"
			resp := &Response{Flags: Flags(int32(tc.bit))}
			if got := resp.TLSForAnything(); got != want {
				t.Fatalf("with only %s set, TLSForAnything() = %v, want %v", tc.name, got, want)
			}
		})
	}

	if resp := (&Response{}); resp.TLSForAnything() {
		t.Fatal("a server that asked for nothing reports that it needs TLS")
	}
}

// TestConformance_GPFileCapabilitiesAreReadFromTheirOwnBits covers the two bits
// that advertise kXR_gpfile.
//
// This client does not send that request — it was retired in XRootD v5 and no
// server here answers it — but the bits still have to be read from the right
// place: they sit in the same word as the TLS flags, and an accessor reading a
// neighbour would report a server as requiring TLS for a session because it
// happened to advertise a request nobody sends.
func TestConformance_GPFileCapabilitiesAreReadFromTheirOwnBits(t *testing.T) {
	for _, tc := range []struct {
		name string
		bit  uint32
		got  func(*Response) bool
		twin func(*Response) bool
	}{
		{"kXR_supgpf", flagSupGPF, (*Response).SupportsGPFile, (*Response).AllowsAnonGPFile},
		{"kXR_anongpf", flagAnonGPF, (*Response).AllowsAnonGPFile, (*Response).SupportsGPFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := &Response{Flags: Flags(int32(tc.bit))}
			if !tc.got(resp) {
				t.Fatalf("with %s set, it reports false", tc.name)
			}
			if tc.twin(resp) {
				t.Fatalf("with only %s set, the other gpfile capability reports true", tc.name)
			}
			if resp.TLSForAnything() {
				t.Fatalf("%s made the server look like one that requires TLS", tc.name)
			}
			if got := (&Response{}); tc.got(got) {
				t.Fatalf("a server that advertised nothing reports %s", tc.name)
			}
		})
	}
}
