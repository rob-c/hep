// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the filesystem-backed handler, driven at the handler API
// rather than over a connection.
//
// Every request that names an open file names it by a handle the server issued,
// and the server is the only thing that knows whether that handle is still
// good. A handler that answers kXR_ok for a handle it does not recognise is the
// worst possible failure: the client reads zero bytes and calls the file empty,
// or writes bytes that go nowhere and calls the transfer complete. So each
// operation below is asked to work with a handle from another session, a handle
// that was never issued, and a handle that has been closed, and each has to come
// back as kXR_ArgInvalid.
//
// The other half is the filesystem saying no. Those refusals are kXR_FSError,
// and they have to carry the reason: a client that is told only "it failed"
// retries a request that will never succeed.

package xrootd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/dirlist"
	"go-hep.org/x/hep/xrootd/xrdproto/mkdir"
	"go-hep.org/x/hep/xrootd/xrdproto/mv"
	"go-hep.org/x/hep/xrootd/xrdproto/open"
	"go-hep.org/x/hep/xrootd/xrdproto/read"
	"go-hep.org/x/hep/xrootd/xrdproto/rm"
	"go-hep.org/x/hep/xrootd/xrdproto/rmdir"
	"go-hep.org/x/hep/xrootd/xrdproto/stat"
	xrdsync "go-hep.org/x/hep/xrootd/xrdproto/sync"
	"go-hep.org/x/hep/xrootd/xrdproto/truncate"
	"go-hep.org/x/hep/xrootd/xrdproto/write"
	"go-hep.org/x/hep/xrootd/xrdproto/xrdclose"
)

// fsHandler returns a handler over a fresh directory, together with the
// directory itself so a test can plant files in it or take them away.
func fsHandler(t *testing.T) (*fshandler, string) {
	t.Helper()

	dir := t.TempDir()
	h, ok := NewFSHandler(dir).(*fshandler)
	if !ok {
		t.Fatalf("NewFSHandler no longer returns *fshandler")
	}
	return h, dir
}

// fsOpen opens path through the handler and returns the handle it issued.
func fsOpen(t *testing.T, h *fshandler, sessionID [16]byte, path string, opts xrdfs.OpenOptions) xrdfs.FileHandle {
	t.Helper()

	resp, status := h.Open(sessionID, &open.Request{
		Path:    path,
		Mode:    xrdfs.OpenModeOwnerRead | xrdfs.OpenModeOwnerWrite,
		Options: opts,
	})
	if status != xrdproto.Ok {
		t.Fatalf("could not open %q: %v", path, resp)
	}
	o, ok := resp.(open.Response)
	if !ok {
		t.Fatalf("open answered %T, want open.Response", resp)
	}
	return o.FileHandle
}

// fsRefused asserts that an operation failed with the given code and a message
// that says what went wrong.
func fsRefused(t *testing.T, resp xrdproto.Marshaler, status xrdproto.ResponseStatus, code xrdproto.ServerErrorCode, want string) {
	t.Helper()

	if status != xrdproto.Error {
		t.Fatalf("the operation answered %v, want an error", status)
	}
	srvErr, ok := resp.(xrdproto.ServerError)
	if !ok {
		t.Fatalf("the failure is a %T, want xrdproto.ServerError", resp)
	}
	if srvErr.Code != code {
		t.Fatalf("the failure is coded %v, want %v (%q)", srvErr.Code, code, srvErr.Message)
	}
	if !strings.Contains(srvErr.Message, want) {
		t.Fatalf("the failure says %q, want it to mention %q", srvErr.Message, want)
	}
}

// fsHandleOps are the requests that work through a file handle. Each is run
// against a handle the server never issued.
func fsHandleOps(h *fshandler, handle xrdfs.FileHandle) []struct {
	name string
	call func(sessionID [16]byte) (xrdproto.Marshaler, xrdproto.ResponseStatus)
} {
	return []struct {
		name string
		call func(sessionID [16]byte) (xrdproto.Marshaler, xrdproto.ResponseStatus)
	}{
		{"close", func(id [16]byte) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Close(id, &xrdclose.Request{Handle: handle})
		}},
		{"read", func(id [16]byte) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Read(id, &read.Request{Handle: handle, Length: 8})
		}},
		{"write", func(id [16]byte) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Write(id, &write.Request{Handle: handle, Data: []byte("go-hep")})
		}},
		{"stat", func(id [16]byte) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Stat(id, &stat.Request{FileHandle: handle})
		}},
		{"truncate", func(id [16]byte) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Truncate(id, &truncate.Request{Handle: handle, Size: 1})
		}},
		{"sync", func(id [16]byte) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Sync(id, &xrdsync.Request{Handle: handle})
		}},
	}
}

func TestConformance_AHandleFromAnotherSessionIsNotAHandle(t *testing.T) {
	// Handles are per-session, and a session is a login. If they were not, a
	// client that guessed a number would be reading another user's open file —
	// and the handler would have no way to notice, because by then the
	// authorization decision has already been made.
	h, dir := fsHandler(t)
	if err := os.WriteFile(filepath.Join(dir, "f.bin"), []byte("go-hep xrootd"), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}
	handle := fsOpen(t, h, confSessionID, "/f.bin", xrdfs.OpenOptionsOpenRead)

	other := [16]byte{16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	for _, tc := range fsHandleOps(h, handle) {
		t.Run(tc.name, func(t *testing.T) {
			resp, status := tc.call(other)
			fsRefused(t, resp, status, xrdproto.InvalidRequest, "Invalid file handle")
		})
	}
}

func TestConformance_AHandleThatWasNeverOpenedIsRefused(t *testing.T) {
	// The session is real and has an open file, so the handler cannot refuse on
	// the session alone: it has to look the handle up.
	h, dir := fsHandler(t)
	if err := os.WriteFile(filepath.Join(dir, "f.bin"), []byte("go-hep xrootd"), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}
	fsOpen(t, h, confSessionID, "/f.bin", xrdfs.OpenOptionsOpenRead)

	bogus := xrdfs.FileHandle{0xde, 0xad, 0xbe, 0xef}
	for _, tc := range fsHandleOps(h, bogus) {
		t.Run(tc.name, func(t *testing.T) {
			resp, status := tc.call(confSessionID)
			fsRefused(t, resp, status, xrdproto.InvalidRequest, "Invalid file handle")
		})
	}
}

func TestConformance_AClosedHandleIsGoneForGood(t *testing.T) {
	// kXR_close frees the handle, and the server may hand the same number out
	// again. A handler that kept answering for it would serve the wrong file the
	// moment it did.
	h, dir := fsHandler(t)
	if err := os.WriteFile(filepath.Join(dir, "f.bin"), []byte("go-hep xrootd"), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}
	handle := fsOpen(t, h, confSessionID, "/f.bin", xrdfs.OpenOptionsOpenRead)

	if _, status := h.Close(confSessionID, &xrdclose.Request{Handle: handle}); status != xrdproto.Ok {
		t.Fatalf("could not close the file: %v", status)
	}

	for _, tc := range fsHandleOps(h, handle) {
		t.Run(tc.name, func(t *testing.T) {
			resp, status := tc.call(confSessionID)
			fsRefused(t, resp, status, xrdproto.InvalidRequest, "Invalid file handle")
		})
	}
}

func TestConformance_APathTheFilesystemRefusesIsAnIOError(t *testing.T) {
	// Every one of these is a request a client will make against a path that is
	// not there — a stale catalogue entry, a job that ran twice, a redirector
	// pointing at a server that no longer holds the replica. The answer has to
	// be kXR_FSError with the reason attached, so the client can tell "not here"
	// from "not allowed" and decide whether to try the next endpoint.
	h, dir := fsHandler(t)
	if err := os.WriteFile(filepath.Join(dir, "f.bin"), []byte("go-hep"), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}

	for _, tc := range []struct {
		name string
		call func() (xrdproto.Marshaler, xrdproto.ResponseStatus)
	}{
		{"dirlist of a directory that is not there", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Dirlist(confSessionID, &dirlist.Request{Path: "/absent"})
		}},
		{"stat of a file that is not there", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Stat(confSessionID, &stat.Request{Path: "/absent.bin"})
		}},
		{"truncate of a file that is not there", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Truncate(confSessionID, &truncate.Request{Path: "/absent.bin", Size: 1})
		}},
		{"open of a file that is not there", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Open(confSessionID, &open.Request{Path: "/absent.bin", Options: xrdfs.OpenOptionsOpenRead})
		}},
		{"remove of a file that is not there", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Remove(confSessionID, &rm.Request{Path: "/absent.bin"})
		}},
		{"rmdir of a directory that is not there", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.RemoveDir(confSessionID, &rmdir.Request{Path: "/absent"})
		}},
		{"rename of a file that is not there", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Rename(confSessionID, &mv.Request{OldPath: "/absent.bin", NewPath: "/other.bin"})
		}},
		{"mkdir below a directory that is not there", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Mkdir(confSessionID, &mkdir.Request{Path: "/absent/dir", Mode: 0o755})
		}},
		{"mkdir where a file already is", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Mkdir(confSessionID, &mkdir.Request{Path: "/f.bin", Mode: 0o755})
		}},
		{"open of a new file below a file", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Open(confSessionID, &open.Request{
				Path:    "/f.bin/sub/new.bin",
				Mode:    xrdfs.OpenModeOwnerRead | xrdfs.OpenModeOwnerWrite,
				Options: xrdfs.OpenOptionsNew | xrdfs.OpenOptionsMkPath,
			})
		}},
		{"open of a file that already exists as new", func() (xrdproto.Marshaler, xrdproto.ResponseStatus) {
			return h.Open(confSessionID, &open.Request{
				Path:    "/f.bin",
				Mode:    xrdfs.OpenModeOwnerRead | xrdfs.OpenModeOwnerWrite,
				Options: xrdfs.OpenOptionsNew,
			})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, status := tc.call()
			fsRefused(t, resp, status, xrdproto.IOError, "An IO error occurred")
		})
	}
}

func TestConformance_AVirtualStatDescribesTheDiskNotTheFile(t *testing.T) {
	// kXR_stat with kXR_vfs asks how much room the storage element has. An
	// ordinary stat answer would report a file's size as the free space, and a
	// client sizing a transfer against that would fill the disk.
	//
	// The answer has to come from the filesystem the writes land on, so a path,
	// a handle and no name at all must all describe the same disk.
	h, dir := fsHandler(t)

	if err := os.WriteFile(filepath.Join(dir, "file.bin"), []byte("data"), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}
	handle := fsOpen(t, h, confSessionID, "/file.bin", xrdfs.OpenOptionsOpenRead)

	for _, tc := range []struct {
		name string
		req  *stat.Request
	}{
		{"the export as a whole", &stat.Request{Options: stat.OptionsVFS}},
		{"a path", &stat.Request{Path: "/file.bin", Options: stat.OptionsVFS}},
		{"an open file", &stat.Request{FileHandle: handle, Options: stat.OptionsVFS}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, status := h.Stat(confSessionID, tc.req)
			if status != xrdproto.Ok {
				t.Fatalf("a virtual stat was refused: %v", resp)
			}
			vfs, ok := resp.(xrdfs.VirtualFSStat)
			if !ok {
				t.Fatalf("a virtual stat was answered with %T, want an xrdfs.VirtualFSStat", resp)
			}
			if vfs.NumberRW != 1 {
				t.Fatalf("the server reports %d nodes with writable space, want itself (1)", vfs.NumberRW)
			}
			if vfs.FreeRW <= 0 {
				t.Fatalf("the server reports %d MB free on a disk it just wrote to", vfs.FreeRW)
			}
			if vfs.UtilizationRW < 0 || vfs.UtilizationRW > 100 {
				t.Fatalf("the server reports %d%% utilization, which is not a percentage", vfs.UtilizationRW)
			}
			// Nothing here fetches a file from tape, and a client told
			// otherwise would wait for a stage that will never be scheduled.
			if vfs.NumberStaging != 0 || vfs.FreeStaging != 0 || vfs.UtilizationStaging != 0 {
				t.Fatalf("the server claims staging space it does not have: %+v", vfs)
			}
		})
	}

	t.Run("a path that is not there is refused", func(t *testing.T) {
		// The filesystem cannot say how much room a disk it cannot find has,
		// and a client told zero would route every write somewhere else.
		resp, status := h.Stat(confSessionID, &stat.Request{Path: "/nowhere/at/all", Options: stat.OptionsVFS})
		fsRefused(t, resp, status, xrdproto.IOError, "An IO error occurred")
	})

	t.Run("an unknown handle is still refused", func(t *testing.T) {
		resp, status := h.Stat(confSessionID, &stat.Request{
			FileHandle: xrdfs.FileHandle{0xff, 0xff, 0xff, 0xff},
			Options:    stat.OptionsVFS,
		})
		fsRefused(t, resp, status, xrdproto.InvalidRequest, "Invalid file handle")
	})
}

func TestConformance_HandlesAreIssuedInOrderAndNeverReused(t *testing.T) {
	// A handle names server state, and a server that guesses at one can collide
	// with a live file: the second open wins and the first caller's reads and
	// writes silently move to another file. Handing them out in order cannot
	// collide, and a closed handle is not offered again to the same session,
	// so a request that arrives late is refused rather than answered about a
	// file that took its number.
	h, dir := fsHandler(t)
	for _, name := range []string{"a.bin", "b.bin", "c.bin"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0644); err != nil {
			t.Fatalf("could not write the file: %v", err)
		}
	}

	var handles []xrdfs.FileHandle
	for _, name := range []string{"a.bin", "b.bin", "c.bin"} {
		handles = append(handles, fsOpen(t, h, confSessionID, "/"+name, xrdfs.OpenOptionsOpenRead))
	}

	want := []xrdfs.FileHandle{{0, 0, 0, 1}, {0, 0, 0, 2}, {0, 0, 0, 3}}
	if handles[0] != want[0] || handles[1] != want[1] || handles[2] != want[2] {
		t.Fatalf("the handler issued %v, want %v", handles, want)
	}

	// The zero handle is what an uninitialized request carries, and no open
	// may ever hand it out.
	for _, handle := range handles {
		if handle == (xrdfs.FileHandle{}) {
			t.Fatal("the handler issued the zero handle")
		}
	}

	if _, status := h.Close(confSessionID, &xrdclose.Request{Handle: handles[1]}); status != xrdproto.Ok {
		t.Fatal("could not close the file")
	}
	next := fsOpen(t, h, confSessionID, "/b.bin", xrdfs.OpenOptionsOpenRead)
	if next == handles[1] {
		t.Fatalf("a closed handle %v was issued again", next)
	}

	// Four billion opens later the counter would repeat itself, and a repeated
	// handle would silently move one caller's reads and writes onto another
	// caller's file. The open is refused instead, and the file it had already
	// opened is closed rather than leaked.
	t.Run("the counter runs out rather than repeating itself", func(t *testing.T) {
		exhausted := [16]byte{0xee}
		fsOpen(t, h, exhausted, "/a.bin", xrdfs.OpenOptionsOpenRead)
		h.mu.RLock()
		sess := h.sessions[exhausted]
		h.mu.RUnlock()
		sess.mu.Lock()
		sess.next = ^uint32(0)
		sess.mu.Unlock()

		resp, status := h.Open(exhausted, &open.Request{Path: "/a.bin", Options: xrdfs.OpenOptionsOpenRead})
		fsRefused(t, resp, status, xrdproto.InvalidRequest, "handle limit exceeded")
	})

	// A second session numbers its own handles, because a handle is only ever
	// looked up in the session that was given it.
	other := [16]byte{0xaa}
	if got := fsOpen(t, h, other, "/a.bin", xrdfs.OpenOptionsOpenRead); got != want[0] {
		t.Fatalf("a fresh session started at handle %v, want %v", got, want[0])
	}
}

func TestConformance_AnOpenReturnsTheFieldsItWasAskedFor(t *testing.T) {
	// kXR_retstat exists so a client can open and stat in one round trip: a
	// handler that withheld the stat when asked would cost an extra round trip
	// per file on a workload that is millions of files, and one that sent it
	// unasked would make the response longer than the client allocated for.
	//
	// The compression fields sit between the handle and the stat information on
	// the wire, so a response carrying stat has to carry them too, whether or
	// not the client asked about compression: every client reads the eight
	// bytes past the handle as the page size and the algorithm name before it
	// reads the stat record.
	h, dir := fsHandler(t)
	if err := os.WriteFile(filepath.Join(dir, "file.bin"), []byte("0123456789"), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}

	for _, tc := range []struct {
		name            string
		opts            xrdfs.OpenOptions
		wantCompression bool
		wantStat        bool
	}{
		{"plain", xrdfs.OpenOptionsOpenRead, false, false},
		{"kXR_retstat", xrdfs.OpenOptionsOpenRead | xrdfs.OpenOptionsReturnStatus, true, true},
		{"kXR_compress", xrdfs.OpenOptionsOpenRead | xrdfs.OpenOptionsCompress, true, false},
		{"both", xrdfs.OpenOptionsOpenRead | xrdfs.OpenOptionsCompress | xrdfs.OpenOptionsReturnStatus, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, status := h.Open(confSessionID, &open.Request{Path: "/file.bin", Options: tc.opts})
			if status != xrdproto.Ok {
				t.Fatalf("could not open the file: %v", resp)
			}
			got, ok := resp.(open.Response)
			if !ok {
				t.Fatalf("an open was answered with %T, want an open.Response", resp)
			}
			if (got.Compression != nil) != tc.wantCompression {
				t.Fatalf("the response has compression %v, want present=%v", got.Compression, tc.wantCompression)
			}
			if (got.Stat != nil) != tc.wantStat {
				t.Fatalf("the response has stat %v, want present=%v", got.Stat, tc.wantStat)
			}
			if tc.wantCompression && *got.Compression != (xrdfs.FileCompression{}) {
				// The handler stores files as they were written. Naming an
				// algorithm would have the client decompress plain bytes.
				t.Fatalf("the handler claims compression %+v on a plain file", *got.Compression)
			}
			if tc.wantStat && got.Stat.Size() != 10 {
				t.Fatalf("the response reports %d bytes, want 10", got.Stat.Size())
			}
		})
	}
}

func TestConformance_OpenCreatesOnlyWhatItWasAskedTo(t *testing.T) {
	// kXR_new, kXR_delete and kXR_mkpath each say something different about a
	// path that is not there, and getting them the wrong way round either
	// destroys data (delete where new was meant) or refuses a legitimate write.
	h, dir := fsHandler(t)

	t.Run("kXR_new will not overwrite", func(t *testing.T) {
		path := filepath.Join(dir, "keep.bin")
		if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
			t.Fatalf("could not write the file: %v", err)
		}

		resp, status := h.Open(confSessionID, &open.Request{
			Path: "/keep.bin", Mode: 0o644, Options: xrdfs.OpenOptionsNew,
		})
		fsRefused(t, resp, status, xrdproto.IOError, "An IO error occurred")

		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("could not read the file back: %v", err)
		}
		if string(got) != "original" {
			t.Fatalf("a refused open still changed the file to %q", got)
		}
	})

	t.Run("kXR_delete truncates what is there", func(t *testing.T) {
		path := filepath.Join(dir, "clobber.bin")
		if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
			t.Fatalf("could not write the file: %v", err)
		}

		handle := fsOpen(t, h, confSessionID, "/clobber.bin", xrdfs.OpenOptionsDelete)
		if _, status := h.Close(confSessionID, &xrdclose.Request{Handle: handle}); status != xrdproto.Ok {
			t.Fatalf("could not close the file: %v", status)
		}

		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("could not read the file back: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("kXR_delete left %q behind", got)
		}
	})

	t.Run("kXR_mkpath makes the parents", func(t *testing.T) {
		// The parents are made with the mode the request carries, so it has to
		// include the owner-execute bit: a directory made 0600 cannot be
		// descended into, and the very next level of the same MkdirAll fails
		// with EACCES.
		resp, status := h.Open(confSessionID, &open.Request{
			Path:    "/a/b/c/new.bin",
			Mode:    xrdfs.OpenModeOwnerRead | xrdfs.OpenModeOwnerWrite | xrdfs.OpenModeOwnerExecute,
			Options: xrdfs.OpenOptionsNew | xrdfs.OpenOptionsMkPath,
		})
		if status != xrdproto.Ok {
			t.Fatalf("could not open a new file below directories that are not there: %v", resp)
		}
		handle := resp.(open.Response).FileHandle
		if _, status := h.Write(confSessionID, &write.Request{Handle: handle, Data: []byte("go-hep")}); status != xrdproto.Ok {
			t.Fatalf("could not write the new file: %v", status)
		}
		if _, status := h.Close(confSessionID, &xrdclose.Request{Handle: handle}); status != xrdproto.Ok {
			t.Fatalf("could not close the file: %v", status)
		}

		got, err := os.ReadFile(filepath.Join(dir, "a", "b", "c", "new.bin"))
		if err != nil {
			t.Fatalf("could not read the new file: %v", err)
		}
		if string(got) != "go-hep" {
			t.Fatalf("the new file holds %q", got)
		}
	})

	t.Run("without kXR_mkpath the parents are not made", func(t *testing.T) {
		resp, status := h.Open(confSessionID, &open.Request{
			Path: "/x/y/new.bin", Mode: 0o644, Options: xrdfs.OpenOptionsNew,
		})
		fsRefused(t, resp, status, xrdproto.IOError, "An IO error occurred")
	})
}

func TestConformance_AnAppendingFileIgnoresTheOffset(t *testing.T) {
	// kXR_open_apnd says every write lands at the end whatever offset the
	// request carries. A handler that honoured the offset would let two
	// concurrent appenders overwrite each other at byte zero.
	h, dir := fsHandler(t)
	if err := os.WriteFile(filepath.Join(dir, "log.txt"), []byte("first "), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}

	handle := fsOpen(t, h, confSessionID, "/log.txt", xrdfs.OpenOptionsOpenAppend)
	for _, data := range []string{"second ", "third"} {
		if _, status := h.Write(confSessionID, &write.Request{Handle: handle, Offset: 0, Data: []byte(data)}); status != xrdproto.Ok {
			t.Fatalf("could not append %q: %v", data, status)
		}
	}
	if _, status := h.Close(confSessionID, &xrdclose.Request{Handle: handle}); status != xrdproto.Ok {
		t.Fatalf("could not close the file: %v", status)
	}

	got, err := os.ReadFile(filepath.Join(dir, "log.txt"))
	if err != nil {
		t.Fatalf("could not read the file back: %v", err)
	}
	if want := "first second third"; string(got) != want {
		t.Fatalf("the file holds %q, want %q", got, want)
	}
}

func TestConformance_AFileOpenedForReadingRefusesAWrite(t *testing.T) {
	// A read-only open is an access-control decision, and the descriptor is
	// where it is enforced. Reporting the EBADF rather than swallowing it is
	// what stops a client from believing the write landed.
	h, dir := fsHandler(t)
	if err := os.WriteFile(filepath.Join(dir, "f.bin"), []byte("go-hep xrootd"), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}
	handle := fsOpen(t, h, confSessionID, "/f.bin", xrdfs.OpenOptionsOpenRead)

	resp, status := h.Write(confSessionID, &write.Request{Handle: handle, Data: []byte("clobber")})
	fsRefused(t, resp, status, xrdproto.IOError, "An IO error occurred")

	got, err := os.ReadFile(filepath.Join(dir, "f.bin"))
	if err != nil {
		t.Fatalf("could not read the file back: %v", err)
	}
	if string(got) != "go-hep xrootd" {
		t.Fatalf("a refused write still changed the file to %q", got)
	}
}

func TestConformance_AReadTheFilesystemRefusesIsNotAShortRead(t *testing.T) {
	// A directory opens and stats like a file and then refuses to be read. An
	// empty kXR_ok here is indistinguishable from end-of-file, and the client
	// stops with a truncated copy it believes is whole.
	h, dir := fsHandler(t)
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("could not create the directory: %v", err)
	}
	handle := fsOpen(t, h, confSessionID, "/sub", xrdfs.OpenOptionsOpenRead)

	resp, status := h.Read(confSessionID, &read.Request{Handle: handle, Length: 8})
	fsRefused(t, resp, status, xrdproto.IOError, "An IO error occurred")
}

func TestConformance_AReadPastTheEndIsEmptyRatherThanAnError(t *testing.T) {
	// io.EOF is the one read failure that is not a failure: it is how a client
	// learns it has the whole file. Turning it into kXR_FSError would make every
	// completed transfer end in an error.
	h, dir := fsHandler(t)
	if err := os.WriteFile(filepath.Join(dir, "f.bin"), []byte("go-hep"), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}
	handle := fsOpen(t, h, confSessionID, "/f.bin", xrdfs.OpenOptionsOpenRead)

	resp, status := h.Read(confSessionID, &read.Request{Handle: handle, Offset: 1024, Length: 8})
	if status != xrdproto.Ok {
		t.Fatalf("a read past the end answered %v (%v), want an empty kXR_ok", status, resp)
	}
	if got := resp.(read.Response); len(got.Data) != 0 {
		t.Fatalf("a read past the end returned %q", got.Data)
	}
}

func TestConformance_AShortReadStopsAtTheEndOfTheFile(t *testing.T) {
	// The other half: a read that straddles the end returns what is there and
	// no more. A handler returning the whole zero-padded buffer would append
	// NULs to every file whose length is not a multiple of the chunk size.
	h, dir := fsHandler(t)
	if err := os.WriteFile(filepath.Join(dir, "f.bin"), []byte("go-hep"), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}
	handle := fsOpen(t, h, confSessionID, "/f.bin", xrdfs.OpenOptionsOpenRead)

	resp, status := h.Read(confSessionID, &read.Request{Handle: handle, Offset: 3, Length: 512})
	if status != xrdproto.Ok {
		t.Fatalf("could not read the file: %v", resp)
	}
	if got := string(resp.(read.Response).Data); got != "hep" {
		t.Fatalf("the read returned %q, want %q", got, "hep")
	}
}

func TestConformance_AHandleIsEnoughToStatAndTruncate(t *testing.T) {
	// kXR_stat and kXR_truncate both accept a handle instead of a path, and a
	// handler that only looked at the path field would stat its own base
	// directory for every such request.
	h, dir := fsHandler(t)
	if err := os.WriteFile(filepath.Join(dir, "f.bin"), []byte("go-hep xrootd"), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}
	handle := fsOpen(t, h, confSessionID, "/f.bin", xrdfs.OpenOptionsOpenUpdate)

	resp, status := h.Stat(confSessionID, &stat.Request{FileHandle: handle})
	if status != xrdproto.Ok {
		t.Fatalf("could not stat by handle: %v", resp)
	}
	if got := resp.(stat.DefaultResponse).EntryStat.EntrySize; got != int64(len("go-hep xrootd")) {
		t.Fatalf("the stat reports %d bytes", got)
	}

	if _, status := h.Truncate(confSessionID, &truncate.Request{Handle: handle, Size: 6}); status != xrdproto.Ok {
		t.Fatalf("could not truncate by handle: %v", status)
	}
	if _, status := h.Sync(confSessionID, &xrdsync.Request{Handle: handle}); status != xrdproto.Ok {
		t.Fatalf("could not sync by handle: %v", status)
	}

	got, err := os.ReadFile(filepath.Join(dir, "f.bin"))
	if err != nil {
		t.Fatalf("could not read the file back: %v", err)
	}
	if string(got) != "go-hep" {
		t.Fatalf("the truncated file holds %q", got)
	}
}

func TestConformance_OpaqueDataIsNotPartOfTheName(t *testing.T) {
	// The CGI on a path is for the authorization layer, not the namespace. A
	// handler that joined it onto its base path would serve a different file to
	// every client that presented a different token — which is to say, it would
	// serve nothing to anyone.
	h, dir := fsHandler(t)
	if err := os.WriteFile(filepath.Join(dir, "f.bin"), []byte("go-hep xrootd"), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}

	const path = "/f.bin?authz=Bearer%20tok&scitag.flow=17"

	resp, status := h.Stat(confSessionID, &stat.Request{Path: path})
	if status != xrdproto.Ok {
		t.Fatalf("could not stat a path carrying opaque data: %v", resp)
	}
	if got := resp.(stat.DefaultResponse).EntryStat.EntrySize; got != int64(len("go-hep xrootd")) {
		t.Fatalf("the stat reports %d bytes", got)
	}

	handle := fsOpen(t, h, confSessionID, path, xrdfs.OpenOptionsOpenRead)
	data, status := h.Read(confSessionID, &read.Request{Handle: handle, Length: 64})
	if status != xrdproto.Ok {
		t.Fatalf("could not read a file opened with opaque data: %v", data)
	}
	if got := string(data.(read.Response).Data); got != "go-hep xrootd" {
		t.Fatalf("the read returned %q", got)
	}
}

func TestConformance_ADirlistCarriesTheStatOnlyWhenAsked(t *testing.T) {
	// kXR_dirlist without kXR_dstat returns names, and a client that receives
	// stat blocks it did not ask for reads them as the next name. The reverse
	// costs a stat round trip per entry on a directory with thousands of them.
	h, dir := fsHandler(t)
	if err := os.WriteFile(filepath.Join(dir, "f.bin"), []byte("go-hep"), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("could not create the directory: %v", err)
	}

	for _, tc := range []struct {
		name     string
		opts     dirlist.RequestOptions
		withStat bool
	}{
		{"without kXR_dstat", dirlist.None, false},
		{"with kXR_dstat", dirlist.WithStatInfo, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, status := h.Dirlist(confSessionID, &dirlist.Request{Path: "/", Options: tc.opts})
			if status != xrdproto.Ok {
				t.Fatalf("could not list the directory: %v", resp)
			}
			list := resp.(*dirlist.Response)
			if list.WithStatInfo != tc.withStat {
				t.Fatalf("the listing reports WithStatInfo=%v, want %v", list.WithStatInfo, tc.withStat)
			}
			if len(list.Entries) != 2 {
				t.Fatalf("the listing holds %d entries, want 2", len(list.Entries))
			}
			for _, e := range list.Entries {
				if e.EntryName == "sub" && !e.IsDir() {
					t.Fatal("the listing does not report the directory as one")
				}
			}
		})
	}
}

func TestConformance_ClosingASessionClosesWhatItLeftOpen(t *testing.T) {
	// A client that drops its connection never sends kXR_close. If the session
	// teardown did not close the descriptors, a busy server would run out of
	// them — and the failure would appear as unrelated opens being refused.
	h, dir := fsHandler(t)
	for _, name := range []string{"a.bin", "b.bin"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("go-hep"), 0644); err != nil {
			t.Fatalf("could not write %q: %v", name, err)
		}
	}
	a := fsOpen(t, h, confSessionID, "/a.bin", xrdfs.OpenOptionsOpenRead)
	fsOpen(t, h, confSessionID, "/b.bin", xrdfs.OpenOptionsOpenRead)

	if err := h.CloseSession(confSessionID); err != nil {
		t.Fatalf("could not close the session: %v", err)
	}

	resp, status := h.Read(confSessionID, &read.Request{Handle: a, Length: 8})
	fsRefused(t, resp, status, xrdproto.InvalidRequest, "Invalid file handle")

	// A session that never opened anything is not an error to close: it is the
	// common case for a client that only ever stats.
	if err := h.CloseSession([16]byte{0xff}); err != nil {
		t.Fatalf("closing a session with nothing open reported %v", err)
	}
}

func TestConformance_ASessionThatCannotCloseItsFilesSaysSo(t *testing.T) {
	// The descriptor is closed behind the handler's back, so both kXR_close and
	// the session teardown meet a file that will not close. Swallowing that
	// would hide the one condition — descriptor exhaustion — the teardown exists
	// to prevent.
	h, dir := fsHandler(t)
	if err := os.WriteFile(filepath.Join(dir, "f.bin"), []byte("go-hep"), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}

	t.Run("on kXR_close", func(t *testing.T) {
		handle := fsOpen(t, h, confSessionID, "/f.bin", xrdfs.OpenOptionsOpenRead)
		file := h.getFile(confSessionID, handle)
		if file == nil {
			t.Fatal("the handle the server issued is not in the session")
		}
		if err := file.File.Close(); err != nil {
			t.Fatalf("could not close the descriptor: %v", err)
		}

		resp, status := h.Close(confSessionID, &xrdclose.Request{Handle: handle})
		fsRefused(t, resp, status, xrdproto.IOError, "An IO error occurred")
	})

	t.Run("on the session teardown", func(t *testing.T) {
		handle := fsOpen(t, h, confSessionID, "/f.bin", xrdfs.OpenOptionsOpenRead)
		file := h.getFile(confSessionID, handle)
		if file == nil {
			t.Fatal("the handle the server issued is not in the session")
		}
		if err := file.File.Close(); err != nil {
			t.Fatalf("could not close the descriptor: %v", err)
		}

		if err := h.CloseSession(confSessionID); err == nil {
			t.Fatal("a session that could not close its files reported success")
		}
	})
}

func TestConformance_ASyncThatTheFilesystemRefusesIsAnIOError(t *testing.T) {
	// kXR_sync is the client asking for a durability guarantee before it deletes
	// its own copy. Answering kXR_ok for a sync that did not happen is how data
	// is lost on a power cut.
	h, dir := fsHandler(t)
	if err := os.WriteFile(filepath.Join(dir, "f.bin"), []byte("go-hep"), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}
	handle := fsOpen(t, h, confSessionID, "/f.bin", xrdfs.OpenOptionsOpenUpdate)

	file := h.getFile(confSessionID, handle)
	if file == nil {
		t.Fatal("the handle the server issued is not in the session")
	}
	if err := file.File.Close(); err != nil {
		t.Fatalf("could not close the descriptor: %v", err)
	}

	resp, status := h.Sync(confSessionID, &xrdsync.Request{Handle: handle})
	fsRefused(t, resp, status, xrdproto.IOError, "An IO error occurred")

	resp, status = h.Truncate(confSessionID, &truncate.Request{Handle: handle, Size: 1})
	fsRefused(t, resp, status, xrdproto.IOError, "An IO error occurred")

	resp, status = h.Stat(confSessionID, &stat.Request{FileHandle: handle})
	fsRefused(t, resp, status, xrdproto.IOError, "An IO error occurred")
}
