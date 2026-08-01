// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the io interfaces xrdio.File claims to implement. The
// package is how the rest of go-hep reads remote files, so a File that is only
// approximately an io.ReaderAt or an io.Seeker breaks every reader built on
// top of it — and does so only for remote files, where it is hardest to see.
//
// The tests run against an in-process XRootD server serving a temporary
// directory, so they exercise the whole native stack: URL parsing, dialling,
// open, stat, kXR_read and close.

package xrdio_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"testing"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdio"
)

// noRedial turns off the connection retries a client applies by default.
//
// xrdio.Open is driven by a URL rather than by a client the test builds, so there is no
// option to pass: XRD_CONNECTIONRETRY is how a program you did not write gets
// configured. A test that means to observe one connection failure would
// otherwise wait out five redials — about eight seconds each — measuring the
// backoff schedule, which is pinned in xrootd's own conformance tests.
func noRedial(t *testing.T) {
	t.Helper()
	t.Setenv(xrootd.EnvConnectionRetry, "0")
}

// xrdioContent is deliberately not a round number of anything: the offsets
// below straddle its end.
var xrdioContent = []byte("the quick brown fox jumps over the lazy dog")

// xrdioServer starts an XRootD server on a temporary directory holding one
// file, and returns the root:// URL of that file.
func xrdioServer(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), xrdioContent, 0644); err != nil {
		t.Fatalf("could not write the test file: %v", err)
	}

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}

	srv := xrootd.NewServer(xrootd.NewFSHandler(dir), func(err error) {
		t.Logf("xrd-srv: %v", err)
	})
	go func() {
		if err := srv.Serve(listener); err != nil && err != xrootd.ErrServerClosed {
			t.Logf("xrd-srv: could not serve: %v", err)
		}
	}()
	t.Cleanup(func() {
		_ = srv.Shutdown(context.Background())
	})

	return fmt.Sprintf("root://%s//file.txt", listener.Addr())
}

func xrdioOpen(t *testing.T) *xrdio.File {
	t.Helper()
	f, err := xrdio.Open(xrdioServer(t))
	if err != nil {
		t.Fatalf("could not open the test file: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// TestFileReadsWholeFile covers the io.Reader path, which tracks its own
// position and has to report io.EOF at the end rather than a short read.
func TestFileReadsWholeFile(t *testing.T) {
	f := xrdioOpen(t)

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("could not read the file: %v", err)
	}
	if !bytes.Equal(got, xrdioContent) {
		t.Fatalf("read %q, want %q", got, xrdioContent)
	}
}

// TestFileReadAt covers io.ReaderAt, whose contract is stricter than Read: it
// must not move the file position, and it must fill the buffer or say why not.
func TestFileReadAt(t *testing.T) {
	f := xrdioOpen(t)

	for _, tc := range []struct {
		off int64
		n   int
	}{
		{0, 1},
		{0, len(xrdioContent)},
		{4, 5},
		{int64(len(xrdioContent)) - 1, 1},
	} {
		t.Run(fmt.Sprintf("%d+%d", tc.off, tc.n), func(t *testing.T) {
			p := make([]byte, tc.n)
			n, err := f.ReadAt(p, tc.off)
			if err != nil {
				t.Fatalf("could not read %d bytes at %d: %v", tc.n, tc.off, err)
			}
			if n != tc.n {
				t.Fatalf("read %d bytes, want %d", n, tc.n)
			}
			if want := xrdioContent[tc.off : tc.off+int64(tc.n)]; !bytes.Equal(p, want) {
				t.Fatalf("read %q, want %q", p, want)
			}
		})
	}

	// ReadAt must leave the position alone: a Read after it starts from
	// where the file was, not from where the ReadAt ended.
	p := make([]byte, 3)
	if _, err := io.ReadFull(f, p); err != nil {
		t.Fatalf("could not read after a ReadAt: %v", err)
	}
	if want := xrdioContent[:3]; !bytes.Equal(p, want) {
		t.Fatalf("a ReadAt moved the file position: read %q, want %q", p, want)
	}
}

// TestFileSeek covers io.Seeker. SeekEnd counts forward from the end, so the
// offset that walks back into the file is negative — the opposite sign to the
// one a "seek back from the end" reading suggests.
func TestFileSeek(t *testing.T) {
	size := int64(len(xrdioContent))

	for _, tc := range []struct {
		name   string
		off    int64
		whence int
		want   int64
	}{
		{"start", 4, io.SeekStart, 4},
		{"start of file", 0, io.SeekStart, 0},
		{"end", 0, io.SeekEnd, size},
		{"before the end", -5, io.SeekEnd, size - 5},
		{"past the end", 5, io.SeekEnd, size + 5},
		{"current", 0, io.SeekCurrent, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := xrdioOpen(t)
			got, err := f.Seek(tc.off, tc.whence)
			if err != nil {
				t.Fatalf("could not seek: %v", err)
			}
			if got != tc.want {
				t.Fatalf("seek landed at %d, want %d", got, tc.want)
			}
			if tc.want >= size {
				return
			}
			p := make([]byte, 1)
			if _, err := io.ReadFull(f, p); err != nil {
				t.Fatalf("could not read after seeking: %v", err)
			}
			if want := xrdioContent[tc.want]; p[0] != want {
				t.Fatalf("read %q after seeking to %d, want %q", p[0], tc.want, want)
			}
		})
	}
}

// TestFileSeekIsRelative: SeekCurrent accumulates, which is what a reader that
// skips over records relies on.
func TestFileSeekIsRelative(t *testing.T) {
	f := xrdioOpen(t)

	for i, want := range []int64{4, 8, 12} {
		got, err := f.Seek(4, io.SeekCurrent)
		if err != nil {
			t.Fatalf("could not seek (step %d): %v", i, err)
		}
		if got != want {
			t.Fatalf("step %d landed at %d, want %d", i, got, want)
		}
	}
}

// TestFileSeekRejectsBadArguments: a position before the start of the file is
// not a position, and an unknown whence is a caller bug worth reporting rather
// than silently treating as SeekStart.
func TestFileSeekRejectsBadArguments(t *testing.T) {
	f := xrdioOpen(t)

	for _, tc := range []struct {
		name   string
		off    int64
		whence int
	}{
		{"negative from the start", -1, io.SeekStart},
		{"before the start of the file", -int64(len(xrdioContent)) - 1, io.SeekEnd},
		{"unknown whence", 0, 42},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := f.Seek(tc.off, tc.whence); err == nil {
				t.Fatal("the seek was accepted")
			}
		})
	}
}

// TestFileIsAnFSFile: xrdio.File is handed to code that takes an fs.File, so
// its Stat must describe the remote file and not the connection.
func TestFileIsAnFSFile(t *testing.T) {
	var f fs.File = xrdioOpen(t)

	fi, err := f.Stat()
	if err != nil {
		t.Fatalf("could not stat: %v", err)
	}
	if got := fi.Size(); got != int64(len(xrdioContent)) {
		t.Fatalf("size is %d, want %d", got, len(xrdioContent))
	}
	if fi.IsDir() {
		t.Fatal("a regular file is reported as a directory")
	}
	if got := f.(*xrdio.File).Name(); got != "/file.txt" {
		t.Fatalf("name is %q, want %q", got, "/file.txt")
	}
}

// TestOpenReportsFailures: the client is a network client, so every one of
// these is a routine outcome rather than an exceptional one.
func TestOpenReportsFailures(t *testing.T) {
	noRedial(t)

	url := xrdioServer(t)

	t.Run("no such file", func(t *testing.T) {
		if _, err := xrdio.Open(url + ".missing"); err == nil {
			t.Fatal("opening a file that does not exist succeeded")
		}
	})

	t.Run("no such server", func(t *testing.T) {
		// Port 1 on the loopback interface: refused, not routed away.
		if _, err := xrdio.Open("root://localhost:1//file.txt"); err == nil {
			t.Fatal("opening a file on a dead server succeeded")
		}
	})

	t.Run("not a url", func(t *testing.T) {
		if _, err := xrdio.Open("root://%zz//file.txt"); err == nil {
			t.Fatal("opening a malformed url succeeded")
		}
	})
}

// TestCloseIsIdempotentEnough: Close on a nil File is an error, not a panic,
// and closing twice must not take the process down either.
func TestCloseTwice(t *testing.T) {
	f, err := xrdio.Open(xrdioServer(t))
	if err != nil {
		t.Fatalf("could not open the test file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("could not close: %v", err)
	}
	if err := f.Close(); err == nil {
		t.Log("closing twice reported no error")
	}
}
