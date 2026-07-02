// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import "testing"

func TestAddressAndTLS(t *testing.T) {
	for _, tc := range []struct {
		in    string
		addr  string
		wants bool
	}{
		{in: "example.org:1094", addr: "example.org:1094", wants: false},
		{in: "root://example.org:1094", addr: "example.org:1094", wants: false},
		{in: "roots://example.org:1094", addr: "example.org:1094", wants: true},
		{in: "xroots://example.org", addr: "example.org", wants: true},
	} {
		t.Run(tc.in, func(t *testing.T) {
			addr, tls, err := addressAndTLS(tc.in)
			if err != nil {
				t.Fatalf("addressAndTLS(%q): %v", tc.in, err)
			}
			if addr != tc.addr {
				t.Fatalf("addr mismatch: got=%q want=%q", addr, tc.addr)
			}
			if tls != tc.wants {
				t.Fatalf("tls mismatch: got=%v want=%v", tls, tc.wants)
			}
		})
	}
}
