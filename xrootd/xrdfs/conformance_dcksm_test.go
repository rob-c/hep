// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the extended stat line and the checksum token that kXR_dcksm
// appends to it.
//
// The four-field stat line every XRootD server has always sent grew a tail of
// five more — ctime, atime, mode, owner, group — and then an optional digest in
// brackets. All of it is one space-separated line, so a reader that miscounts
// by one field reports the mode of a file as its access time. These tests pin
// the field order, the octal mode token, and the rule that a short line is
// still a valid line.

package xrdfs

import (
	"os"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
)

func TestConformance_TheExtendedStatLineIsNineFieldsAndADigest(t *testing.T) {
	es := EntryStat{
		HasStatInfo:     true,
		HasExtendedInfo: true,
		EntryName:       "a.txt",
		ID:              12345,
		EntrySize:       6,
		Flags:           StatIsReadable,
		Mtime:           1700000000,
		Ctime:           1600000000,
		Atime:           1800000000,
		Perm:            0o644,
		Owner:           "1000",
		Group:           "1001",
		Checksum:        "adler32:08ab0289",
	}

	var w xrdenc.WBuffer
	if err := es.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal the entry: %v", err)
	}

	const want = "12345 6 16 1700000000 1600000000 1800000000 0644 1000 1001 [ adler32:08ab0289 ]"
	if got := string(w.Bytes()); got != want {
		t.Fatalf("the stat line is %q, want %q", got, want)
	}
}

func TestConformance_TheModeTokenIsFourDigitOctal(t *testing.T) {
	// A reader scans this back with strtoul(.., 8). Written in decimal, 0644
	// would come back as 0420: a file the caller thinks nobody may read.
	for _, tc := range []struct {
		name string
		perm uint32
		want string
	}{
		{"an ordinary file", 0o644, "0644"},
		{"an ordinary directory", 0o755, "0755"},
		{"a private file", 0o600, "0600"},
		{"nothing at all", 0, "0000"},
		{"a setuid binary", 0o4755, "4755"},
		{"a sticky directory", 0o1777, "1777"},
		{"a file type above the permission bits", 0o100644, "0644"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			es := EntryStat{HasStatInfo: true, HasExtendedInfo: true, Perm: tc.perm}
			var w xrdenc.WBuffer
			if err := es.MarshalXrd(&w); err != nil {
				t.Fatalf("could not marshal the entry: %v", err)
			}
			fields := strings.Fields(string(w.Bytes()))
			if len(fields) != 9 {
				t.Fatalf("the stat line holds %d fields, want 9: %q", len(fields), w.Bytes())
			}
			if got := fields[6]; got != tc.want {
				t.Fatalf("the mode token is %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConformance_AnOwnerlessEntryStillFillsItsField(t *testing.T) {
	// A server that does not know the owner still has to put something where
	// the owner goes, or the group is read as the owner and the digest as the
	// group.
	es := EntryStat{HasStatInfo: true, HasExtendedInfo: true, Perm: 0o644}
	var w xrdenc.WBuffer
	if err := es.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal the entry: %v", err)
	}
	fields := strings.Fields(string(w.Bytes()))
	if got, want := fields[7], "0"; got != want {
		t.Fatalf("the owner token is %q, want %q", got, want)
	}
	if got, want := fields[8], "0"; got != want {
		t.Fatalf("the group token is %q, want %q", got, want)
	}
}

func TestConformance_AnExtendedStatLineRoundTrips(t *testing.T) {
	for _, tc := range []struct {
		name string
		want EntryStat
	}{
		{"a short line", EntryStat{
			HasStatInfo: true, EntryName: "a.txt", ID: 1, EntrySize: 2, Flags: StatIsReadable, Mtime: 3,
		}},
		{"a long line", EntryStat{
			HasStatInfo: true, HasExtendedInfo: true, EntryName: "a.txt",
			ID: 1, EntrySize: 2, Flags: StatIsReadable, Mtime: 3,
			Ctime: 4, Atime: 5, Perm: 0o640, Owner: "root", Group: "wheel",
		}},
		{"a long line with a digest", EntryStat{
			HasStatInfo: true, HasExtendedInfo: true, EntryName: "a.txt",
			ID: 1, EntrySize: 2, Flags: StatIsReadable, Mtime: 3,
			Ctime: 4, Atime: 5, Perm: 0o640, Owner: "root", Group: "wheel",
			Checksum: "sha256:" + strings.Repeat("ab", 32),
		}},
		{"a short line with a digest", EntryStat{
			HasStatInfo: true, EntryName: "a.txt", ID: 1, EntrySize: 2, Flags: StatIsReadable, Mtime: 3,
			Checksum: "adler32:none",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var w xrdenc.WBuffer
			if err := tc.want.MarshalXrd(&w); err != nil {
				t.Fatalf("could not marshal the entry: %v", err)
			}

			got := EntryStat{HasStatInfo: true, EntryName: tc.want.EntryName}
			if err := got.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
				t.Fatalf("could not unmarshal the entry: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestConformance_TheDigestIsSplitFromItsAlgorithm(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cksum string
		algo  string
		value string
	}{
		{"no checksum was asked for", "", "", ""},
		{"a digest", "adler32:08ab0289", "adler32", "08ab0289"},
		{"an entry that cannot have one", "adler32:none", "adler32", ""},
		{"a token with no algorithm", "08ab0289", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			es := EntryStat{Checksum: tc.cksum}
			if got := es.ChecksumAlgo(); got != tc.algo {
				t.Fatalf("ChecksumAlgo() = %q, want %q", got, tc.algo)
			}
			if got := es.ChecksumValue(); got != tc.value {
				t.Fatalf("ChecksumValue() = %q, want %q", got, tc.value)
			}
		})
	}
}

func TestConformance_TheExtendedModeIsTheOneOnTheWire(t *testing.T) {
	// Without the tail, Mode() can only widen the three bits the flags word
	// carries into 0444/0222. With it, the bits are there to be reported, and
	// a caller copying a file with its permissions has to get the real ones.
	es := EntryStat{HasStatInfo: true, HasExtendedInfo: true, Perm: 0o640, Flags: StatIsReadable | StatIsWritable}
	if got, want := es.Mode(), os.FileMode(0o640); got != want {
		t.Fatalf("Mode() = %v, want %v", got, want)
	}

	es.Flags |= StatIsDir
	if got, want := es.Mode(), os.ModeDir|0o640; got != want {
		t.Fatalf("Mode() = %v, want %v", got, want)
	}

	es.HasExtendedInfo = false
	if got, want := es.Mode(), os.ModeDir|0o666; got != want {
		t.Fatalf("Mode() = %v, want %v", got, want)
	}
}

func TestConformance_AnExtendedEntryIsBuiltFromAFileOnDisk(t *testing.T) {
	dir := t.TempDir()
	name := dir + "/a.txt"
	if err := os.WriteFile(name, []byte("go-hep"), 0o640); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}
	info, err := os.Stat(name)
	if err != nil {
		t.Fatalf("could not stat the file: %v", err)
	}

	es := EntryStatExtendedFrom(info)
	if !es.HasExtendedInfo {
		t.Fatal("the entry does not carry the extended tail")
	}
	if got, want := es.Perm, uint32(0o640); got != want {
		t.Fatalf("the permission bits are %04o, want %04o", got, want)
	}
	if got, want := es.EntrySize, int64(6); got != want {
		t.Fatalf("the size is %d, want %d", got, want)
	}
	if es.Checksum != "" {
		t.Fatalf("an entry built from a file carries a digest %q, want none", es.Checksum)
	}
}

// fileInfoWithoutSys is an os.FileInfo from somewhere other than this port's
// filesystem — an archive reader, a virtual tree, a test double. Sys() gives
// back something that is not a *syscall.Stat_t, which is the case sysStat has
// to answer without asserting its way into a panic.
type fileInfoWithoutSys struct {
	os.FileInfo
}

func (fileInfoWithoutSys) Sys() any { return nil }

func TestConformance_AnEntryFromAFilesystemThisPortCannotReadStillEncodes(t *testing.T) {
	dir := t.TempDir()
	name := dir + "/a.txt"
	if err := os.WriteFile(name, []byte("go-hep"), 0o600); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}
	info, err := os.Stat(name)
	if err != nil {
		t.Fatalf("could not stat the file: %v", err)
	}

	es := EntryStatExtendedFrom(fileInfoWithoutSys{info})
	if !es.HasExtendedInfo {
		t.Fatal("the entry does not carry the extended tail")
	}
	// The fields this port cannot see are left empty rather than filled with a
	// plausible value: a fabricated ctime is worse than an admitted zero,
	// because a caller comparing it against a recorded one acts on it.
	if es.Ctime != 0 || es.Atime != 0 {
		t.Fatalf("the entry claims ctime=%d atime=%d, want zeros", es.Ctime, es.Atime)
	}
	if got, want := es.Perm, uint32(0o600); got != want {
		// The permission bits do come through os.FileInfo, so they are still right.
		t.Fatalf("the entry is %04o, want %04o", got, want)
	}

	// An owner nobody knows still occupies its field, or every field after it
	// is read as the wrong one.
	var w xrdenc.WBuffer
	if err := es.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal the entry: %v", err)
	}
	if got, want := len(strings.Fields(string(w.Bytes()))), 9; got != want {
		t.Fatalf("the stat line holds %d fields, want %d: %q", got, want, w.Bytes())
	}
}

func TestConformance_AnUnreadableExtendedTailFailsTheEntry(t *testing.T) {
	// Every field of the tail is a number, and a line where one of them is not
	// is a line this reader does not understand. Taking the fields it can parse
	// and leaving the rest zero would report a file as world-writable, or as
	// last touched in 1970.
	for _, tc := range []struct {
		name string
		line string
	}{
		{"a ctime that is not a number", "1 2 16 3 x 5 0644 1000 1000"},
		{"an atime that is not a number", "1 2 16 3 4 x 0644 1000 1000"},
		{"a mode that is not a number", "1 2 16 3 4 5 x 1000 1000"},
		{"a mode that is not octal", "1 2 16 3 4 5 0899 1000 1000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			es := EntryStat{HasStatInfo: true}
			if err := es.UnmarshalXrd(xrdenc.NewRBuffer([]byte(tc.line))); err == nil {
				t.Fatalf("a stat line %q was decoded as %+v", tc.line, es)
			}
		})
	}
}
