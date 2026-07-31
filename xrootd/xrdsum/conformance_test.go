// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdsum

import (
	"sort"
	"testing"
)

// TestConformance_SupportedNamesTheAlgorithmsSumImplements keeps the advertised
// list and the implemented set from drifting apart. Supported is what a caller
// negotiates with — xrd-cp uses it to decide whether it can check a server's
// digest at all — so a name on the list that Sum rejects turns a verified copy
// into an error, and an algorithm missing from the list is silently never used.
func TestConformance_SupportedNamesTheAlgorithmsSumImplements(t *testing.T) {
	got := Supported()

	if !sort.StringsAreSorted(got) {
		t.Errorf("Supported is %q, want it sorted as documented", got)
	}

	for _, algo := range got {
		if _, err := Sum(algo, []byte("payload")); err != nil {
			t.Errorf("Supported advertises %q, which Sum rejects: %v", algo, err)
		}
	}

	// The other direction: a name Sum accepts but Supported omits would be
	// invisible to anything negotiating with the list.
	for _, algo := range []string{"adler32", "crc32c", "crc64", "md5"} {
		if _, err := Sum(algo, nil); err != nil {
			continue // not implemented, so not expected on the list either
		}
		if sort.SearchStrings(got, algo) >= len(got) || got[sort.SearchStrings(got, algo)] != algo {
			t.Errorf("Sum implements %q, which Supported does not advertise", algo)
		}
	}

	// A caller must not be able to talk the list into an algorithm that is
	// not there.
	if _, err := Sum("", nil); err == nil {
		t.Error("the empty algorithm name was accepted")
	}
}

// TestConformance_DigestsAreFixedWidthHex pins the formatting, which is part of
// the answer and not a presentation detail: XRootD compares checksums as
// strings, so a digest printed with %x instead of %08x drops leading zeros and
// fails to match the very server that produced it.
func TestConformance_DigestsAreFixedWidthHex(t *testing.T) {
	// The first input has an adler32 with a leading zero byte; the second is
	// long enough to have none.
	inputs := [][]byte{{0x00}, []byte("the quick brown fox jumps over the lazy dog")}

	for _, algo := range Supported() {
		var widths []int
		for _, in := range inputs {
			got, err := Sum(algo, in)
			if err != nil {
				t.Fatalf("Sum(%q): %v", algo, err)
			}
			for _, c := range got {
				if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
					t.Fatalf("Sum(%q) is %q, want lower-case hexadecimal", algo, got)
				}
			}
			widths = append(widths, len(got))
		}
		if widths[0] != widths[1] {
			t.Errorf("%s digests are %d and %d characters wide, want a fixed width", algo, widths[0], widths[1])
		}
	}

	// The concrete case the fixed width exists for.
	if got, err := Sum("adler32", []byte{0x00}); err != nil || got != "00010001" {
		t.Errorf("Sum(adler32, 0x00) is %q (%v), want %q", got, err, "00010001")
	}
}
