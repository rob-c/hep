// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdfs_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
)

// TestEntryStatWireFormat pins the encoding stat information travels in. It is
// not a binary struct: the server writes four decimal numbers separated by
// blanks, and a dirlist reply concatenates one such record per entry. A change
// in the separator or the field order is invisible to a round-trip test and
// fatal against a real server.
func TestEntryStatWireFormat(t *testing.T) {
	for _, tc := range []struct {
		name string
		stat xrdfs.EntryStat
		want string
	}{
		{
			name: "file",
			stat: xrdfs.EntryStat{HasStatInfo: true, ID: 12, EntrySize: 345, Flags: xrdfs.StatIsReadable, Mtime: 1500000000},
			want: "12 345 16 1500000000",
		},
		{
			name: "directory",
			stat: xrdfs.EntryStat{HasStatInfo: true, ID: 1, EntrySize: 4096, Flags: xrdfs.StatIsDir | xrdfs.StatIsReadable | xrdfs.StatIsWritable, Mtime: 1},
			want: "1 4096 50 1",
		},
		{
			name: "plain file has no flag of its own",
			stat: xrdfs.EntryStat{HasStatInfo: true, ID: 0, EntrySize: 0, Flags: xrdfs.StatIsFile, Mtime: 0},
			want: "0 0 0 0",
		},
		{
			name: "without stat info nothing is written",
			stat: xrdfs.EntryStat{EntryName: "f.txt"},
			want: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var w xrdenc.WBuffer
			if err := tc.stat.MarshalXrd(&w); err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if got := string(w.Bytes()); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEntryStatUnmarshalStopsAtTheRecordSeparator covers how a dirlist reply is
// laid out: entry names and stat records alternate, separated by newlines, and
// the whole thing may be NUL-terminated. A decoder that reads to the end of the
// buffer swallows the next entry.
func TestEntryStatUnmarshalStopsAtTheRecordSeparator(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
		rest string
	}{
		{name: "newline", data: "1 2 3 4\nnext-entry", rest: "next-entry"},
		{name: "nul", data: "1 2 3 4\x00trailing", rest: "trailing"},
		{name: "end of buffer", data: "1 2 3 4", rest: ""},
		{name: "extra fields are ignored", data: "1 2 3 4 5 6\nnext-entry", rest: "next-entry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := xrdenc.NewRBuffer([]byte(tc.data))

			var got xrdfs.EntryStat
			if err := got.UnmarshalXrd(r); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			want := xrdfs.EntryStat{HasStatInfo: true, ID: 1, EntrySize: 2, Flags: 3, Mtime: 4}
			if got != want {
				t.Fatalf("got %+v, want %+v", got, want)
			}
			if rest := string(r.Bytes()); rest != tc.rest {
				t.Fatalf("the decoder consumed %d bytes and left %q, want %q left", r.Pos(), rest, tc.rest)
			}
		})
	}
}

// TestEntryStatUnmarshalRefusesMalformedRecords: stat information is text from
// the server, so every field is untrusted. A record that does not parse must
// be an error rather than a zero-valued stat that reads as an empty file.
func TestEntryStatUnmarshalRefusesMalformedRecords(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
	}{
		{name: "empty", data: ""},
		{name: "too few fields", data: "1 2 3"},
		{name: "id is not a number", data: "one 2 3 4"},
		{name: "size is not a number", data: "1 two 3 4"},
		{name: "flags are not a number", data: "1 2 three 4"},
		{name: "mtime is not a number", data: "1 2 3 four"},
		{name: "tab is not the separator", data: "1\t2\t3\t4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got xrdfs.EntryStat
			if err := got.UnmarshalXrd(xrdenc.NewRBuffer([]byte(tc.data))); err == nil {
				t.Fatalf("a malformed stat record decoded to %+v", got)
			}
		})
	}
}

// TestEntryStatRoundTrip checks the decoder against the encoder over the whole
// flag space rather than a handful of values.
func TestEntryStatRoundTrip(t *testing.T) {
	flags := []xrdfs.StatFlags{
		xrdfs.StatIsFile, xrdfs.StatIsExecutable, xrdfs.StatIsDir, xrdfs.StatIsOther,
		xrdfs.StatIsOffline, xrdfs.StatIsReadable, xrdfs.StatIsWritable, xrdfs.StatIsPOSCPending,
	}
	// Every combination of the eight flags.
	for mask := range 1 << len(flags) {
		var f xrdfs.StatFlags
		for i, flag := range flags {
			if mask&(1<<i) != 0 {
				f |= flag
			}
		}
		want := xrdfs.EntryStat{HasStatInfo: true, ID: 7, EntrySize: 1 << 40, Flags: f, Mtime: 1600000000}

		var w xrdenc.WBuffer
		if err := want.MarshalXrd(&w); err != nil {
			t.Fatalf("flags %d: marshal: %v", f, err)
		}
		var got xrdfs.EntryStat
		if err := got.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
			t.Fatalf("flags %d: unmarshal: %v", f, err)
		}
		if got != want {
			t.Fatalf("flags %d: got %+v, want %+v", f, got, want)
		}
	}
}

// TestEntryStatPredicates checks each accessor answers for its own flag and
// no other. The flags are a bit set, so a predicate that tests equality rather
// than a bit works for a plain file and fails for everything else.
func TestEntryStatPredicates(t *testing.T) {
	type pred struct {
		name string
		flag xrdfs.StatFlags
		fn   func(xrdfs.EntryStat) bool
	}
	preds := []pred{
		{"IsExecutable", xrdfs.StatIsExecutable, xrdfs.EntryStat.IsExecutable},
		{"IsDir", xrdfs.StatIsDir, xrdfs.EntryStat.IsDir},
		{"IsOther", xrdfs.StatIsOther, xrdfs.EntryStat.IsOther},
		{"IsOffline", xrdfs.StatIsOffline, xrdfs.EntryStat.IsOffline},
		{"IsReadable", xrdfs.StatIsReadable, xrdfs.EntryStat.IsReadable},
		{"IsWritable", xrdfs.StatIsWritable, xrdfs.EntryStat.IsWritable},
		{"IsPOSCPending", xrdfs.StatIsPOSCPending, xrdfs.EntryStat.IsPOSCPending},
	}

	for _, set := range preds {
		t.Run(set.name, func(t *testing.T) {
			// The flag on its own, and the flag inside the full set.
			for _, es := range []xrdfs.EntryStat{{Flags: set.flag}, {Flags: ^xrdfs.StatFlags(0)}} {
				if !set.fn(es) {
					t.Fatalf("%s is false for flags %d", set.name, es.Flags)
				}
			}
			// Every other flag, and none.
			for _, other := range preds {
				if other.flag == set.flag {
					continue
				}
				if set.fn(xrdfs.EntryStat{Flags: other.flag}) {
					t.Fatalf("%s is true for %s alone", set.name, other.name)
				}
			}
			if set.fn(xrdfs.EntryStat{Flags: xrdfs.StatIsFile}) {
				t.Fatalf("%s is true for a plain file", set.name)
			}
		})
	}
}

// TestEntryStatIsFileInfo checks the os.FileInfo view, which is what
// xrdio and io/fs consumers see.
func TestEntryStatIsFileInfo(t *testing.T) {
	es := xrdfs.EntryStat{
		EntryName:   "data.root",
		HasStatInfo: true,
		EntrySize:   1234,
		Flags:       xrdfs.StatIsReadable,
		Mtime:       1500000000,
	}

	var fi os.FileInfo = es
	if got, want := fi.Name(), "data.root"; got != want {
		t.Fatalf("Name is %q, want %q", got, want)
	}
	if got, want := fi.Size(), int64(1234); got != want {
		t.Fatalf("Size is %d, want %d", got, want)
	}
	if got, want := fi.ModTime(), time.Unix(1500000000, 0); !got.Equal(want) {
		t.Fatalf("ModTime is %v, want %v", got, want)
	}
	if fi.Sys() != nil {
		t.Fatalf("Sys is %v, want nil", fi.Sys())
	}
	if fi.IsDir() {
		t.Fatal("a readable file reports itself as a directory")
	}
	if got, want := fi.Mode(), os.FileMode(0444); got != want {
		t.Fatalf("Mode is %v, want %v", got, want)
	}

	dir := xrdfs.EntryStat{Flags: xrdfs.StatIsDir | xrdfs.StatIsReadable | xrdfs.StatIsWritable}
	if got, want := dir.Mode(), os.ModeDir|0666; got != want {
		t.Fatalf("directory Mode is %v, want %v", got, want)
	}
}

// TestEntryStatFrom checks the conversion the go-hep server uses to answer a
// stat: the permission bits of the underlying file must reach the client as
// the readable and writable flags.
func TestEntryStatFrom(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(name, []byte("hello"), 0400); err != nil {
		t.Fatalf("write: %v", err)
	}

	fi, err := os.Stat(name)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	es := xrdfs.EntryStatFrom(fi)
	switch {
	case !es.HasStatInfo:
		t.Fatal("the converted entry carries no stat info")
	case es.EntryName != "file.txt":
		t.Fatalf("name is %q, want %q", es.EntryName, "file.txt")
	case es.EntrySize != 5:
		t.Fatalf("size is %d, want 5", es.EntrySize)
	case !es.IsReadable():
		t.Fatal("a 0400 file is not readable")
	case es.IsWritable():
		t.Fatal("a 0400 file is writable")
	case es.IsDir():
		t.Fatal("a regular file is a directory")
	case es.Mtime != fi.ModTime().Unix():
		t.Fatalf("mtime is %d, want %d", es.Mtime, fi.ModTime().Unix())
	}

	dirStat := xrdfs.EntryStatFrom(mustStat(t, dir))
	if !dirStat.IsDir() {
		t.Fatal("a directory is not reported as one")
	}
	if !dirStat.IsWritable() {
		t.Fatal("a writable directory is not reported as writable")
	}
}

func mustStat(t *testing.T, name string) os.FileInfo {
	t.Helper()
	fi, err := os.Stat(name)
	if err != nil {
		t.Fatalf("stat %s: %v", name, err)
	}
	return fi
}

// TestVirtualFSStatWireFormat pins the six blank-separated fields of a
// kXR_stat reply with kXR_vfs, in order.
func TestVirtualFSStatWireFormat(t *testing.T) {
	want := xrdfs.VirtualFSStat{
		NumberRW: 1, FreeRW: 2, UtilizationRW: 3,
		NumberStaging: 4, FreeStaging: 5, UtilizationStaging: 6,
	}

	var w xrdenc.WBuffer
	if err := want.MarshalXrd(&w); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got, want := string(w.Bytes()), "1 2 3 4 5 6"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	var got xrdfs.VirtualFSStat
	if err := got.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestVirtualFSStatUnmarshalRefusesMalformedRecords(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
	}{
		{name: "empty", data: ""},
		{name: "five fields", data: "1 2 3 4 5"},
		{name: "not numbers", data: "a b c d e f"},
		{name: "last field is not a number", data: "1 2 3 4 5 six"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got xrdfs.VirtualFSStat
			if err := got.UnmarshalXrd(xrdenc.NewRBuffer([]byte(tc.data))); err == nil {
				t.Fatalf("a malformed record decoded to %+v", got)
			}
		})
	}
}

func TestVirtualFSStatUnmarshalStopsAtTheRecordSeparator(t *testing.T) {
	r := xrdenc.NewRBuffer([]byte("1 2 3 4 5 6\nrest"))

	var got xrdfs.VirtualFSStat
	if err := got.UnmarshalXrd(r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rest := string(r.Bytes()); rest != "rest" {
		t.Fatalf("left %q on the buffer, want %q", rest, "rest")
	}
}

// TestOpenModeIsPOSIXPermissionBits checks the open mode is the ordinary
// permission word and not a renumbering of it: chmod and mkdir pass it
// straight through to the server, which applies it to the filesystem.
func TestOpenModeIsPOSIXPermissionBits(t *testing.T) {
	for _, tc := range []struct {
		mode xrdfs.OpenMode
		want os.FileMode
	}{
		{xrdfs.OpenModeOwnerRead, 0400},
		{xrdfs.OpenModeOwnerWrite, 0200},
		{xrdfs.OpenModeOwnerExecute, 0100},
		{xrdfs.OpenModeGroupRead, 0040},
		{xrdfs.OpenModeGroupWrite, 0020},
		{xrdfs.OpenModeGroupExecute, 0010},
		{xrdfs.OpenModeOtherRead, 0004},
		{xrdfs.OpenModeOtherWrite, 0002},
		{xrdfs.OpenModeOtherExecute, 0001},
	} {
		t.Run(fmt.Sprintf("%#o", tc.want), func(t *testing.T) {
			if got := os.FileMode(tc.mode); got != tc.want {
				t.Fatalf("got %#o, want %#o", got, tc.want)
			}
		})
	}

	rwxrxrx := xrdfs.OpenModeOwnerRead | xrdfs.OpenModeOwnerWrite | xrdfs.OpenModeOwnerExecute |
		xrdfs.OpenModeGroupRead | xrdfs.OpenModeGroupExecute |
		xrdfs.OpenModeOtherRead | xrdfs.OpenModeOtherExecute
	if got, want := rwxrxrx, xrdfs.OpenMode(0755); got != want {
		t.Fatalf("the named bits make up %#o, want %#o", got, want)
	}
}

// TestOpenOptionsAreDistinctBits checks the options are a bit set. They are
// declared with iota, so a constant inserted in the middle silently renumbers
// every option after it; what this catches is two options sharing a bit.
func TestOpenOptionsAreDistinctBits(t *testing.T) {
	opts := map[string]xrdfs.OpenOptions{
		"Compress":       xrdfs.OpenOptionsCompress,
		"Delete":         xrdfs.OpenOptionsDelete,
		"Force":          xrdfs.OpenOptionsForce,
		"New":            xrdfs.OpenOptionsNew,
		"OpenRead":       xrdfs.OpenOptionsOpenRead,
		"OpenUpdate":     xrdfs.OpenOptionsOpenUpdate,
		"Async":          xrdfs.OpenOptionsAsync,
		"Refresh":        xrdfs.OpenOptionsRefresh,
		"MkPath":         xrdfs.OpenOptionsMkPath,
		"OpenAppend":     xrdfs.OpenOptionsOpenAppend,
		"ReturnStatus":   xrdfs.OpenOptionsReturnStatus,
		"Replica":        xrdfs.OpenOptionsReplica,
		"POSC":           xrdfs.OpenOptionsPOSC,
		"NoWait":         xrdfs.OpenOptionsNoWait,
		"SequentiallyIO": xrdfs.OpenOptionsSequentiallyIO,
	}

	seen := make(map[xrdfs.OpenOptions]string, len(opts))
	var all xrdfs.OpenOptions
	for name, opt := range opts {
		switch {
		case opt == 0:
			t.Errorf("%s is zero: it cannot be told apart from no option at all", name)
		case opt&(opt-1) != 0:
			t.Errorf("%s is %#x, which is not a single bit", name, opt)
		}
		if other, dup := seen[opt]; dup {
			t.Errorf("%s and %s are both %#x", name, other, opt)
		}
		seen[opt] = name
		all |= opt
	}
	if got, want := all, xrdfs.OpenOptions(1<<len(opts)-1); got != want {
		t.Fatalf("the options together cover %#x, want the low %d bits (%#x)", got, len(opts), want)
	}
	if xrdfs.OpenOptionsNone != 0 {
		t.Fatalf("OpenOptionsNone is %#x, want 0", xrdfs.OpenOptionsNone)
	}
}
