// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the directory-listing wire format, which has two shapes on
// the same request ID.
//
// kXR_dirlist answers either with bare names or with a stat line after every
// name, and the two are told apart by a sentinel first entry (".") rather than
// by anything in the header. That makes the encoder's uniformity rule load
// bearing: a response carrying stat information for some entries and not others
// cannot be decoded at all, because the reader counts lines in pairs and a
// single missing stat line shifts every subsequent name onto the stat of its
// neighbour — a listing where each file reports the size and mode of another.

package dirlist

import (
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
)

func TestConformance_AListingIsEitherAllStattedOrNoneOfIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		resp Response
	}{
		{"a statted entry in a listing without stat information", Response{
			Entries: []xrdfs.EntryStat{
				{EntryName: "a.txt"},
				{EntryName: "b.txt", HasStatInfo: true},
			},
		}},
		{"a bare entry in a listing with stat information", Response{
			WithStatInfo: true,
			Entries: []xrdfs.EntryStat{
				{EntryName: "a.txt", HasStatInfo: true},
				{EntryName: "b.txt"},
			},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var w xrdenc.WBuffer
			err := tc.resp.MarshalXrd(&w)
			if err == nil {
				t.Fatal("a listing of mixed entries was encoded")
			}
			if !strings.Contains(err.Error(), "either have stat info or not") {
				t.Fatalf("the failure says %q, want it to name the inconsistency", err)
			}
		})
	}
}

func TestConformance_AStatLineThatCannotBeReadFailsTheListing(t *testing.T) {
	// The sentinel says every name is followed by a stat line, and one of them
	// is not a stat line. Reading a zero-valued entry out of it would report a
	// file of size 0 with no flags, which callers use to decide whether to
	// descend into it.
	body := ".\n0 0 0 0\na.txt\nnot a stat line\x00"

	var resp Response
	err := resp.UnmarshalXrd(xrdenc.NewRBuffer([]byte(body)))
	if err == nil {
		t.Fatal("a listing with an unreadable stat line was accepted")
	}
}

func TestConformance_AListingRoundTripsInBothShapes(t *testing.T) {
	// The control for both rules above: what the server sends when it does and
	// does not support stat information, through the encoder and back.
	for _, tc := range []struct {
		name string
		resp Response
	}{
		{"bare names", Response{Entries: []xrdfs.EntryStat{
			{EntryName: "a.txt"},
			{EntryName: "sub"},
		}}},
		{"names with stat information", Response{WithStatInfo: true, Entries: []xrdfs.EntryStat{
			{EntryName: "a.txt", HasStatInfo: true, EntrySize: 42, Mtime: 1600000000},
			{EntryName: "sub", HasStatInfo: true, Flags: xrdfs.StatIsDir},
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var w xrdenc.WBuffer
			if err := tc.resp.MarshalXrd(&w); err != nil {
				t.Fatalf("could not encode the listing: %v", err)
			}

			var got Response
			if err := got.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
				t.Fatalf("could not decode the listing: %v", err)
			}
			if got.WithStatInfo != tc.resp.WithStatInfo {
				t.Fatalf("the listing came back with WithStatInfo=%v", got.WithStatInfo)
			}
			if len(got.Entries) != len(tc.resp.Entries) {
				t.Fatalf("the listing came back with %d entries, want %d", len(got.Entries), len(tc.resp.Entries))
			}
			for i, want := range tc.resp.Entries {
				if got.Entries[i].EntryName != want.EntryName {
					t.Errorf("entry %d is %q, want %q", i, got.Entries[i].EntryName, want.EntryName)
				}
				if got.Entries[i].EntrySize != want.EntrySize {
					t.Errorf("entry %d has size %d, want %d", i, got.Entries[i].EntrySize, want.EntrySize)
				}
				if got.Entries[i].Flags != want.Flags {
					t.Errorf("entry %d has flags %v, want %v", i, got.Entries[i].Flags, want.Flags)
				}
			}
		})
	}
}
