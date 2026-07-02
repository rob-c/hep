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
}

func TestSum(t *testing.T) {
	data := []byte("123456789")
	for _, tc := range []struct{ algo, want string }{
		{"adler32", "091e01de"},
		{"crc32c", "e3069283"},
		{"crc64", "995dc9bbdf1939fa"},
		{"md5", "25f9e794323b453885f5181f1b624d0b"},
	} {
		got, err := Sum(tc.algo, data)
		if err != nil {
			t.Fatalf("Sum(%q): %v", tc.algo, err)
		}
		if got != tc.want {
			t.Fatalf("Sum(%q): got=%q want=%q", tc.algo, got, tc.want)
		}
	}
	if _, err := Sum("sha1", data); err == nil {
		t.Fatal("Sum(sha1): expected error for unsupported algorithm")
	}
}
