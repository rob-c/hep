// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rdict

import (
	"strings"
	"testing"
)

// classes that stream themselves by hand: their data members never show up as
// streamer elements, so there is nothing for CxxCheckSum to hash.
var chksumExceptions = map[string]struct{}{
	"TF1":            {},
	"TF1Convolution": {},
	"TF1NormSum":     {},
	"THashList":      {},
	"THashTable":     {},
	"TSeqCollection": {},
	"TStreamerInfo":  {},
	"TString":        {},
}

// TestCxxCheckSum checks groot computes the same class checksum C++ ROOT does,
// against every streamer C++ ROOT itself produced.
func TestCxxCheckSum(t *testing.T) {
	var n, bad int
	for _, si := range StreamerInfos.Values() {
		name := si.Name()
		if strings.Contains(name, "<") {
			continue // templates: ROOT spells those differently
		}
		if _, skip := chksumExceptions[name]; skip {
			continue
		}
		n++

		got := CxxCheckSum(name, si.Elements())
		if want := uint32(si.CheckSum()); got != want {
			bad++
			t.Errorf("%s (v%d): got=0x%08x, want=0x%08x", name, si.ClassVersion(), got, want)
		}
	}

	if n < 100 {
		t.Fatalf("only %d classes checked, expected the ROOT streamer dump to hold more", n)
	}
	t.Logf("checked %d C++ ROOT classes, %d disagreed", n, bad)
}

// TestCxxCheckSumExceptionsStillFail guards the exception list: should groot
// learn to compute one of these, the list should lose it rather than quietly
// excuse a class that now works.
func TestCxxCheckSumExceptionsStillFail(t *testing.T) {
	for name := range chksumExceptions {
		si, ok := StreamerInfos.Get(name, -1)
		if !ok {
			t.Errorf("%s: no such streamer, drop it from the exception list", name)
			continue
		}
		if got, want := CxxCheckSum(name, si.Elements()), uint32(si.CheckSum()); got == want {
			t.Errorf("%s: now computes correctly (0x%08x), drop it from the exception list", name, got)
		}
	}
}
