// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for kXR_dcksm, the dirlist option that appends a digest to every
// stat line.
//
// It is the cheapest way to ask "has anything here changed?" about a whole
// directory: one request, one round trip, and a digest per entry that a caller
// can compare against what it recorded last time without reading a byte of the
// files. The alternative is a kXR_query checksum per entry, which is one round
// trip each and a different code path per server.

package dirlist

import (
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
)

func TestConformance_TheDirlistOptionsAreDistinctBits(t *testing.T) {
	// kXR_dcksm implies kXR_dstat, and a server told to send digests answers
	// with stat lines whether or not the client also set kXR_dstat. That only
	// works if the three options are separate bits of one word.
	for _, tc := range []struct {
		name string
		opt  RequestOptions
		want RequestOptions
	}{
		{"kXR_online", Online, 1},
		{"kXR_dstat", WithStatInfo, 2},
		{"kXR_dcksm", WithChecksum, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.opt != tc.want {
				t.Fatalf("%s is %#x, want %#x", tc.name, tc.opt, tc.want)
			}
		})
	}
}

func TestConformance_AChecksumRequestAsksForStatToo(t *testing.T) {
	req := NewChecksumRequest("/tmp/dir", "sha256")
	if req.Options&WithStatInfo == 0 {
		t.Fatal("a checksum listing did not ask for stat information")
	}
	if req.Options&WithChecksum == 0 {
		t.Fatal("a checksum listing did not ask for checksums")
	}
	if got, want := req.Path, "/tmp/dir?cks.type=sha256"; got != want {
		t.Fatalf("the request path is %q, want %q", got, want)
	}

	// An unnamed algorithm leaves the path alone: the server picks its default,
	// and a client that spelled one out would be choosing for it.
	req = NewChecksumRequest("/tmp/dir", "")
	if got, want := req.Path, "/tmp/dir"; got != want {
		t.Fatalf("the request path is %q, want it untouched", got)
	}
}

func TestConformance_AChecksumRequestKeepsTheOpaqueDataItWasGiven(t *testing.T) {
	req := NewChecksumRequest("/tmp/dir?authz=abc", "md5")
	if got, want := req.Path, "/tmp/dir?authz=abc&cks.type=md5"; got != want {
		t.Fatalf("the request path is %q, want %q", got, want)
	}
}

func TestConformance_TheChecksumAlgorithmIsReadFromTheOpaqueData(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{"no opaque data at all", "/tmp/dir", DefaultChecksumAlgo},
		{"opaque data naming nothing", "/tmp/dir?authz=abc", DefaultChecksumAlgo},
		{"an algorithm on its own", "/tmp/dir?cks.type=md5", "md5"},
		{"an algorithm among others", "/tmp/dir?authz=abc&cks.type=sha1&x=1", "sha1"},
		{"an algorithm in capitals", "/tmp/dir?cks.type=SHA256", "sha256"},
		{"an empty algorithm", "/tmp/dir?cks.type=", DefaultChecksumAlgo},
		{"the last of two", "/tmp/dir?cks.type=md5&cks.type=crc32", "crc32"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ChecksumAlgo(tc.path)
			if err != nil {
				t.Fatalf("could not read the algorithm: %v", err)
			}
			if got != tc.want {
				t.Fatalf("the algorithm is %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConformance_AnAlgorithmNobodyImplementsIsRefused(t *testing.T) {
	// The reference server refuses the listing rather than answering with the
	// digest it does know: a caller comparing sha3 against an adler32 it was
	// silently handed would report every file in the directory as corrupt.
	_, err := ChecksumAlgo("/tmp/dir?cks.type=sha3")
	if err == nil {
		t.Fatal("an algorithm no server implements was accepted")
	}
	if got, want := err.Error(), "sha3 checksum not supported."; got != want {
		t.Fatalf("the failure says %q, want %q", got, want)
	}
}

func TestConformance_AChecksumListingRoundTrips(t *testing.T) {
	want := Response{
		WithStatInfo: true,
		WithChecksum: true,
		Entries: []xrdfs.EntryStat{
			{
				EntryName: "a.txt", HasStatInfo: true, EntrySize: 6, Mtime: 1000,
				HasExtendedInfo: true, Ctime: 900, Atime: 1100, Perm: 0o644,
				Owner: "1000", Group: "1000",
				Checksum: "adler32:08ab0289",
			},
			{
				EntryName: "sub", HasStatInfo: true, Flags: xrdfs.StatIsDir, Mtime: 1000,
				HasExtendedInfo: true, Ctime: 900, Atime: 1100, Perm: 0o755,
				Owner: "1000", Group: "1000",
				Checksum: "adler32:none",
			},
		},
	}

	var w xrdenc.WBuffer
	if err := want.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal the listing: %v", err)
	}

	var got Response
	if err := got.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
		t.Fatalf("could not unmarshal the listing: %v", err)
	}
	if !got.WithStatInfo || !got.WithChecksum {
		t.Fatalf("the listing came back as stat=%v cksum=%v, want both", got.WithStatInfo, got.WithChecksum)
	}
	if len(got.Entries) != len(want.Entries) {
		t.Fatalf("the listing holds %d entries, want %d", len(got.Entries), len(want.Entries))
	}
	for i, entry := range got.Entries {
		if entry != want.Entries[i] {
			t.Fatalf("entry %d is %+v, want %+v", i, entry, want.Entries[i])
		}
	}
}

func TestConformance_AListingWithoutChecksumsSaysSo(t *testing.T) {
	// A client that asked for digests and got a plain stat listing back has to
	// find out here rather than by parsing "" as a digest and deciding the
	// files are all identical.
	resp := Response{
		WithStatInfo: true,
		Entries:      []xrdfs.EntryStat{{EntryName: "a.txt", HasStatInfo: true}},
	}

	var w xrdenc.WBuffer
	if err := resp.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal the listing: %v", err)
	}
	if strings.Contains(string(w.Bytes()), "[") {
		t.Fatalf("a listing without checksums carries a checksum token: %q", w.Bytes())
	}

	var got Response
	if err := got.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
		t.Fatalf("could not unmarshal the listing: %v", err)
	}
	if got.WithChecksum {
		t.Fatal("a listing without checksums reports that it has them")
	}
}
