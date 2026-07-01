// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package protocol

import "testing"

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

func TestResponseTLSAccessors(t *testing.T) {
	const (
		kXRhaveTLS  = 0x80000000
		kXRgotoTLS  = 0x40000000
		kXRtlsLogin = 0x04000000
		kXRtlsData  = 0x01000000
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
