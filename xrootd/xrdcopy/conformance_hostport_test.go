// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the address the destination server is told to pull from.
//
// A third-party copy hands the source's host and port to the destination, which
// dials it directly. The two travel as separate fields, so the port has to be a
// number by the time it leaves this process — and an address that carries no
// port, or one that is a service name rather than a number, has to become the
// XRootD default rather than zero. Sending port 0 asks the destination to
// connect to a port that does not exist, and the error surfaces there, on a
// machine whose operator has no idea what this client sent.

package xrdcopy

import "testing"

func TestConformance_ASourceAddressAlwaysCarriesANumericPort(t *testing.T) {
	for _, tc := range []struct {
		addr string
		host string
		port int
	}{
		{"eos.example.org:1094", "eos.example.org", 1094},
		{"eos.example.org:2094", "eos.example.org", 2094},
		{"eos.example.org", "eos.example.org", 1094},
		{"eos.example.org:xroot", "eos.example.org", 1094},
		{"eos.example.org:", "eos.example.org", 1094},
		{"127.0.0.1:1095", "127.0.0.1", 1095},
	} {
		t.Run(tc.addr, func(t *testing.T) {
			host, port := hostPort(tc.addr)
			if host != tc.host || port != tc.port {
				t.Fatalf("%q splits into %q:%d, want %q:%d", tc.addr, host, port, tc.host, tc.port)
			}
		})
	}
}
