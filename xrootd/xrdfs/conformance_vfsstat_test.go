// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the virtual filesystem statistics, which arrive as text.
//
// kXR_stat with the vfs flag answers with six space-separated numbers rather
// than a binary structure, so every field is a decimal string parsed at the
// client. A field that is not a number is a server the client does not
// understand — a different response than it asked for, or a newer format — and
// silently reading zero out of it reports a storage element with no space, no
// free nodes and no staging capacity, which is indistinguishable from a full
// pool and will divert every write away from a server that is in fact healthy.

package xrdfs

import (
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
)

func TestConformance_VirtualStatFieldsThatAreNotNumbersAreRejected(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"the read-write node count", "x 20 30 40 50 60"},
		{"the free read-write space", "10 x 30 40 50 60"},
		{"the read-write utilization", "10 20 x 40 50 60"},
		{"the staging node count", "10 20 30 x 50 60"},
		{"the free staging space", "10 20 30 40 x 60"},
		{"the staging utilization", "10 20 30 40 50 x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var vfs VirtualFSStat
			err := vfs.UnmarshalXrd(xrdenc.NewRBuffer([]byte(tc.body)))
			if err == nil {
				t.Fatalf("%q was accepted as virtual stat information", tc.body)
			}
			if !strings.Contains(err.Error(), "x") {
				t.Fatalf("the failure says %q, want it to carry the field it could not read", err)
			}
			if vfs != (VirtualFSStat{}) {
				t.Fatalf("a rejected response left %+v behind", vfs)
			}
		})
	}
}

func TestConformance_VirtualStatNeedsAllSixFields(t *testing.T) {
	// Five fields is not five-sixths of an answer: which five is unknowable, so
	// there is no field that can be read out of it.
	var vfs VirtualFSStat
	err := vfs.UnmarshalXrd(xrdenc.NewRBuffer([]byte("10 20 30 40 50")))
	if err == nil {
		t.Fatal("a five-field response was accepted")
	}
	if !strings.Contains(err.Error(), "enough fields") {
		t.Fatalf("the failure says %q, want it to say a field is missing", err)
	}
}

func TestConformance_VirtualStatReadsEveryFieldInOrder(t *testing.T) {
	// The control for the two above, and the pinning of the field order: a
	// transposition here would report free space as a node count.
	var vfs VirtualFSStat
	if err := vfs.UnmarshalXrd(xrdenc.NewRBuffer([]byte("10 20 30 40 50 60\n"))); err != nil {
		t.Fatalf("could not read the virtual stat information: %v", err)
	}
	want := VirtualFSStat{
		NumberRW:           10,
		FreeRW:             20,
		UtilizationRW:      30,
		NumberStaging:      40,
		FreeStaging:        50,
		UtilizationStaging: 60,
	}
	if vfs != want {
		t.Fatalf("the response reads %+v, want %+v", vfs, want)
	}
}
