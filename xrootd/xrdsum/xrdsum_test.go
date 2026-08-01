// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdsum

import "testing"

func TestKnownAnswers(t *testing.T) {
	data := []byte("123456789")
	if got, want := CRC32C(data), uint32(0xe3069283); got != want {
		t.Fatalf("CRC32C: got=%#x want=%#x", got, want)
	}
	if got, want := Adler32(data), uint32(0x091e01de); got != want {
		t.Fatalf("Adler32: got=%#x want=%#x", got, want)
	}
	if got, want := CRC64(data), uint64(0x995dc9bbdf1939fa); got != want {
		t.Fatalf("CRC64: got=%#x want=%#x", got, want)
	}
	if got, want := CRC32(data), uint32(0xcbf43926); got != want {
		t.Fatalf("CRC32: got=%#x want=%#x", got, want)
	}
}

// TestSupportedIsWhatSumComputes keeps the advertised list and the implemented
// switch from drifting apart. The list is what a server answers kXR_query
// checksum with and what a dirlist checksum request is validated against, so an
// algorithm named there and missing here is a request accepted and then failed
// at the point where the digest was supposed to be computed.
func TestSupportedIsWhatSumComputes(t *testing.T) {
	got := Supported()
	if len(got) == 0 {
		t.Fatal("no checksum algorithm is advertised")
	}

	seen := make(map[string]bool, len(got))
	for _, algo := range got {
		if seen[algo] {
			t.Fatalf("%q is advertised twice", algo)
		}
		seen[algo] = true
		if _, err := Sum(algo, []byte("123456789")); err != nil {
			t.Fatalf("%q is advertised but not implemented: %v", algo, err)
		}
	}

	for _, algo := range []string{"adler32", "crc32", "crc32c", "crc64", "md5", "sha1", "sha256"} {
		if !seen[algo] {
			t.Fatalf("%q is implemented but not advertised", algo)
		}
	}
}

func TestSum(t *testing.T) {
	data := []byte("123456789")
	for _, tc := range []struct{ algo, want string }{
		{"adler32", "091e01de"},
		{"crc32c", "e3069283"},
		{"crc64", "995dc9bbdf1939fa"},
		{"crc32", "cbf43926"},
		{"md5", "25f9e794323b453885f5181f1b624d0b"},
		{"sha1", "f7c3bc1d808e04732adf679965ccc34ca7ae3441"},
		{"sha256", "15e2b0d3c33891ebb0f1ef609ec419420c20e320ce94c65fbc8c3312448eb225"},
	} {
		got, err := Sum(tc.algo, data)
		if err != nil {
			t.Fatalf("Sum(%q): %v", tc.algo, err)
		}
		if got != tc.want {
			t.Fatalf("Sum(%q): got=%q want=%q", tc.algo, got, tc.want)
		}
	}
	if _, err := Sum("sha3", data); err == nil {
		t.Fatal("Sum(sha3): expected error for unsupported algorithm")
	}
}
