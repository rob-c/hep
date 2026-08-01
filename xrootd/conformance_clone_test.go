// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for kXR_clone and kXR_dcksm at the handler API.
//
// A clone moves bytes between two files the server already has open, so the
// data never crosses the network at all. That is the whole point of it, and it
// is also what makes it worth being careful about: the client names ranges of
// files by handle, and a handler that copied first and checked afterwards would
// leave half a clone in the destination whenever the second item turned out to
// be nonsense. So the list is validated whole before a byte moves, and every
// refusal below happens with the destination untouched.

package xrootd

import (
	"os"
	"path/filepath"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/clone"
	"go-hep.org/x/hep/xrootd/xrdproto/dirlist"
)

// cloneSetup plants src on disk, opens it for reading and an empty destination
// for writing, and returns both handles together with the directory.
func cloneSetup(t *testing.T, src []byte) (h *fshandler, dir string, srcHandle, dstHandle xrdfs.FileHandle) {
	t.Helper()

	h, dir = fsHandler(t)
	if err := os.WriteFile(filepath.Join(dir, "src.bin"), src, 0644); err != nil {
		t.Fatalf("could not write the source: %v", err)
	}
	srcHandle = fsOpen(t, h, confSessionID, "/src.bin", xrdfs.OpenOptionsOpenRead)
	dstHandle = fsOpen(t, h, confSessionID, "/dst.bin", xrdfs.OpenOptionsNew|xrdfs.OpenOptionsOpenUpdate)
	return h, dir, srcHandle, dstHandle
}

func TestConformance_ACloneCopiesRangesWithoutSendingThem(t *testing.T) {
	h, dir, src, dst := cloneSetup(t, []byte("0123456789"))

	resp, status := h.Clone(confSessionID, clone.NewRequest(dst, []clone.Item{
		{Src: src, SrcOffset: 6, SrcLength: 4, DstOffset: 0},
		{Src: src, SrcOffset: 0, SrcLength: 6, DstOffset: 4},
	}))
	if status != xrdproto.Ok {
		t.Fatalf("the clone was refused: %v", resp)
	}
	if _, ok := resp.(*clone.Response); !ok {
		t.Fatalf("the clone answered %T, want *clone.Response", resp)
	}

	got, err := os.ReadFile(filepath.Join(dir, "dst.bin"))
	if err != nil {
		t.Fatalf("could not read the destination: %v", err)
	}
	if want := "6789012345"; string(got) != want {
		t.Fatalf("the destination holds %q, want %q", got, want)
	}
}

func TestConformance_AnEmptyCloneRangeCopiesNothingAndIsNotAnError(t *testing.T) {
	// A caller building a list from a loop over extents can produce a zero-
	// length one, and refusing it would make the caller special-case something
	// that means exactly what it says.
	h, dir, src, dst := cloneSetup(t, []byte("0123456789"))

	resp, status := h.Clone(confSessionID, clone.NewRequest(dst, []clone.Item{
		{Src: src, SrcOffset: 0, SrcLength: 0, DstOffset: 0},
	}))
	if status != xrdproto.Ok {
		t.Fatalf("an empty range was refused: %v", resp)
	}

	got, err := os.ReadFile(filepath.Join(dir, "dst.bin"))
	if err != nil {
		t.Fatalf("could not read the destination: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("an empty range wrote %q", got)
	}
}

func TestConformance_ACloneCopiesMoreThanOneChunk(t *testing.T) {
	// The copy runs in fixed-size chunks, and a range longer than one chunk is
	// where an off-by-one in the loop shows up as a destination that is right
	// at the front and wrong at the back.
	const size = (1 << 20) + 4096
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i)
	}
	h, dir, src, dst := cloneSetup(t, data)

	resp, status := h.Clone(confSessionID, clone.NewRequest(dst, []clone.Item{
		{Src: src, SrcOffset: 0, SrcLength: size, DstOffset: 0},
	}))
	if status != xrdproto.Ok {
		t.Fatalf("the clone was refused: %v", resp)
	}

	got, err := os.ReadFile(filepath.Join(dir, "dst.bin"))
	if err != nil {
		t.Fatalf("could not read the destination: %v", err)
	}
	if len(got) != size {
		t.Fatalf("the destination holds %d bytes, want %d", len(got), size)
	}
	for i := range got {
		if got[i] != data[i] {
			t.Fatalf("the destination differs from the source at byte %d", i)
		}
	}
}

func TestConformance_ACloneListIsCheckedBeforeAnythingIsCopied(t *testing.T) {
	// The second item is the bad one. If the handler copied as it went, the
	// first would already be in the destination when the request came back as
	// an error, and the caller would have no way to know how much of what it
	// asked for happened.
	for _, tc := range []struct {
		name  string
		items []clone.Item
		code  xrdproto.ServerErrorCode
		msg   string
	}{
		{
			name:  "no items at all",
			items: nil,
			code:  xrdproto.ArgMissing,
			msg:   "clone list is missing",
		},
		{
			name:  "more items than a server accepts",
			items: make([]clone.Item, clone.MaxItems+1),
			code:  xrdproto.ArgTooLong,
			msg:   "too many clone items",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, dir, _, dst := cloneSetup(t, []byte("0123456789"))

			resp, status := h.Clone(confSessionID, clone.NewRequest(dst, tc.items))
			fsRefused(t, resp, status, tc.code, tc.msg)

			assertEmptyFile(t, filepath.Join(dir, "dst.bin"))
		})
	}
}

func TestConformance_ACloneOfARangeNoFileHasIsRefused(t *testing.T) {
	h, dir, src, dst := cloneSetup(t, []byte("0123456789"))

	resp, status := h.Clone(confSessionID, clone.NewRequest(dst, []clone.Item{
		{Src: src, SrcOffset: 0, SrcLength: 4, DstOffset: 0},
		{Src: src, SrcOffset: -1, SrcLength: 4, DstOffset: 4},
	}))
	fsRefused(t, resp, status, xrdproto.ArgInvalid, "clone offset/length out of range")

	assertEmptyFile(t, filepath.Join(dir, "dst.bin"))
}

func TestConformance_ACloneNamingAHandleNobodyIssuedIsRefused(t *testing.T) {
	// Both ends of a clone are handles, and both have to be checked: a source
	// handle that is not this session's would be a way to read a file the
	// caller was never given.
	h, dir, src, dst := cloneSetup(t, []byte("0123456789"))
	never := xrdfs.FileHandle{0xff, 0xff, 0xff, 0xff}

	t.Run("the destination", func(t *testing.T) {
		resp, status := h.Clone(confSessionID, clone.NewRequest(never, []clone.Item{
			{Src: src, SrcLength: 4},
		}))
		fsRefused(t, resp, status, xrdproto.InvalidRequest, "Invalid file handle")
	})

	t.Run("a source", func(t *testing.T) {
		resp, status := h.Clone(confSessionID, clone.NewRequest(dst, []clone.Item{
			{Src: src, SrcLength: 4},
			{Src: never, SrcLength: 4, DstOffset: 4},
		}))
		fsRefused(t, resp, status, xrdproto.InvalidRequest, "Invalid file handle")
		assertEmptyFile(t, filepath.Join(dir, "dst.bin"))
	})

	t.Run("a source from another session", func(t *testing.T) {
		var other [16]byte
		other[0] = confSessionID[0] + 1
		resp, status := h.Clone(other, clone.NewRequest(dst, []clone.Item{
			{Src: src, SrcLength: 4},
		}))
		fsRefused(t, resp, status, xrdproto.InvalidRequest, "Invalid file handle")
	})
}

func TestConformance_ACloneThatCannotBeCopiedIsAnIOError(t *testing.T) {
	// The destination is open for reading only, so every write into it fails.
	// The client asked for bytes to be moved and none were: that is an I/O
	// error, and not an argument the caller could have written differently.
	h, dir := fsHandler(t)
	if err := os.WriteFile(filepath.Join(dir, "src.bin"), []byte("0123456789"), 0644); err != nil {
		t.Fatalf("could not write the source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dst.bin"), nil, 0644); err != nil {
		t.Fatalf("could not write the destination: %v", err)
	}
	src := fsOpen(t, h, confSessionID, "/src.bin", xrdfs.OpenOptionsOpenRead)
	dst := fsOpen(t, h, confSessionID, "/dst.bin", xrdfs.OpenOptionsOpenRead)

	resp, status := h.Clone(confSessionID, clone.NewRequest(dst, []clone.Item{
		{Src: src, SrcLength: 4},
	}))
	fsRefused(t, resp, status, xrdproto.IOError, "clone copy failed")
}

// assertEmptyFile fails the test unless the named file is there and empty.
func assertEmptyFile(t *testing.T, name string) {
	t.Helper()

	fi, err := os.Stat(name)
	if err != nil {
		t.Fatalf("could not stat %q: %v", name, err)
	}
	if fi.Size() != 0 {
		t.Fatalf("%q holds %d bytes, want a file nothing was copied into", name, fi.Size())
	}
}

func TestConformance_AChecksumListingCarriesADigestPerEntry(t *testing.T) {
	h, dir := fsHandler(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("123456789"), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0755); err != nil {
		t.Fatalf("could not create the directory: %v", err)
	}

	resp, status := h.Dirlist(confSessionID, dirlist.NewChecksumRequest("/", "adler32"))
	if status != xrdproto.Ok {
		t.Fatalf("the listing was refused: %v", resp)
	}
	list, ok := resp.(*dirlist.Response)
	if !ok {
		t.Fatalf("the listing answered %T, want *dirlist.Response", resp)
	}
	if !list.WithStatInfo {
		// kXR_dcksm implies kXR_dstat: the digest is appended to a stat line,
		// and there is nowhere else for it to go.
		t.Fatal("a checksum listing came back without stat information")
	}
	if !list.WithChecksum {
		t.Fatal("a checksum listing came back without checksums")
	}
	if len(list.Entries) != 2 {
		t.Fatalf("the listing holds %d entries, want 2", len(list.Entries))
	}

	for _, entry := range list.Entries {
		if got, want := entry.ChecksumAlgo(), "adler32"; got != want {
			t.Fatalf("%q is checksummed with %q, want %q", entry.EntryName, got, want)
		}
		switch entry.EntryName {
		case "a.txt":
			// adler32("123456789") is the value every implementation of the
			// algorithm agrees on.
			if got, want := entry.ChecksumValue(), "091e01de"; got != want {
				t.Fatalf("a.txt hashes to %q, want %q", got, want)
			}
			if got, want := entry.Perm, uint32(0644); got != want {
				t.Fatalf("a.txt is %04o, want %04o", got, want)
			}
		case "sub":
			// A directory has no digest, and says so rather than being left
			// out of the listing or given the digest of nothing.
			if got := entry.ChecksumValue(); got != "" {
				t.Fatalf("the directory hashes to %q, want no digest", got)
			}
			if !entry.IsDir() {
				t.Fatal("the directory is not reported as one")
			}
		default:
			t.Fatalf("the listing holds an entry %q nobody created", entry.EntryName)
		}
	}
}

func TestConformance_AChecksumListingHonoursTheAlgorithmItWasAsked(t *testing.T) {
	h, dir := fsHandler(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("123456789"), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}

	for _, tc := range []struct {
		algo string
		want string
	}{
		{"adler32", "091e01de"},
		{"crc32", "cbf43926"},
		{"md5", "25f9e794323b453885f5181f1b624d0b"},
		{"sha1", "f7c3bc1d808e04732adf679965ccc34ca7ae3441"},
		{"sha256", "15e2b0d3c33891ebb0f1ef609ec419420c20e320ce94c65fbc8c3312448eb225"},
	} {
		t.Run(tc.algo, func(t *testing.T) {
			resp, status := h.Dirlist(confSessionID, dirlist.NewChecksumRequest("/", tc.algo))
			if status != xrdproto.Ok {
				t.Fatalf("the listing was refused: %v", resp)
			}
			list := resp.(*dirlist.Response)
			if got, want := list.Entries[0].Checksum, tc.algo+":"+tc.want; got != want {
				t.Fatalf("a.txt is checksummed %q, want %q", got, want)
			}
		})
	}
}

func TestConformance_AChecksumListingRefusesAnAlgorithmItCannotCompute(t *testing.T) {
	// Answering with the digest the server does know would be worse than
	// failing: a caller comparing sha3 against an adler32 it was silently
	// handed finds every file in the directory corrupt.
	h, dir := fsHandler(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("123456789"), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}

	resp, status := h.Dirlist(confSessionID, dirlist.NewChecksumRequest("/", "sha3"))
	fsRefused(t, resp, status, xrdproto.InternalServerError, "sha3 checksum not supported.")
}

func TestConformance_APlainListingCarriesNoDigests(t *testing.T) {
	h, dir := fsHandler(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("123456789"), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}

	for _, tc := range []struct {
		name string
		req  *dirlist.Request
		stat bool
	}{
		{"names alone", &dirlist.Request{Path: "/"}, false},
		{"names and stat", &dirlist.Request{Options: dirlist.WithStatInfo, Path: "/"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, status := h.Dirlist(confSessionID, tc.req)
			if status != xrdproto.Ok {
				t.Fatalf("the listing was refused: %v", resp)
			}
			list := resp.(*dirlist.Response)
			if list.WithChecksum {
				t.Fatal("a listing that asked for no digests reports that it has them")
			}
			if list.WithStatInfo != tc.stat {
				t.Fatalf("the listing came back with stat=%v, want %v", list.WithStatInfo, tc.stat)
			}
			if got := list.Entries[0].Checksum; got != "" {
				t.Fatalf("an entry carries the digest %q, want none", got)
			}
		})
	}
}
