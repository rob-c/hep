// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for OpenFrom, the entry point that borrows a filesystem handle
// instead of dialling its own. Everything that walks a remote tree — xrd-ls,
// the copy engine, any caller that opens many files over one session — goes
// through it, so a File that closes the borrowed session, or that reports a
// name or size belonging to the connection rather than the file, breaks the
// caller's next open rather than its own.

package xrdio_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdio"
)

// xrdioFS starts the same in-process server as the rest of this package's
// conformance tests and returns a filesystem handle onto it.
func xrdioFS(t *testing.T) xrdfs.FileSystem {
	t.Helper()

	urn, err := xrdio.Parse(xrdioServer(t))
	if err != nil {
		t.Fatalf("could not parse the server URL: %v", err)
	}

	client, err := xrootd.NewClient(context.Background(), urn.Addr, "gopher")
	if err != nil {
		t.Fatalf("could not connect to the test server: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	return client.FS()
}

func TestOpenFromReadsThroughABorrowedFilesystem(t *testing.T) {
	fs := xrdioFS(t)

	f, err := xrdio.OpenFrom(fs, "/file.txt")
	if err != nil {
		t.Fatalf("could not open the test file: %v", err)
	}
	defer f.Close()

	if got, want := f.Name(), "/file.txt"; got != want {
		t.Errorf("the file is named %q, want %q", got, want)
	}

	fi, err := f.Stat()
	if err != nil {
		t.Fatalf("could not stat the file: %v", err)
	}
	if got, want := fi.Size(), int64(len(xrdioContent)); got != want {
		t.Errorf("the file is %d bytes, want %d", got, want)
	}

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("could not read the file: %v", err)
	}
	if !bytes.Equal(got, xrdioContent) {
		t.Errorf("read %q, want %q", got, xrdioContent)
	}
}

// TestOpenFromDoesNotCloseTheBorrowedFilesystem is the invariant that makes
// OpenFrom worth having: the session belongs to the caller. A File that closed
// it would work in isolation and break the second file a caller opens.
func TestOpenFromDoesNotCloseTheBorrowedFilesystem(t *testing.T) {
	fs := xrdioFS(t)

	f, err := xrdio.OpenFrom(fs, "/file.txt")
	if err != nil {
		t.Fatalf("could not open the test file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("could not close the file: %v", err)
	}

	// The filesystem must still answer.
	if _, err := fs.Stat(context.Background(), "/file.txt"); err != nil {
		t.Fatalf("the borrowed filesystem was closed with the file: %v", err)
	}

	// And it must still be able to open the same file again.
	g, err := xrdio.OpenFrom(fs, "/file.txt")
	if err != nil {
		t.Fatalf("could not reopen the test file: %v", err)
	}
	if err := g.Close(); err != nil {
		t.Fatalf("could not close the reopened file: %v", err)
	}
}

// TestOpenFromReportsAMissingFile checks the failure is reported at open rather
// than surfacing later as an empty read, and that it names the path.
func TestOpenFromReportsAMissingFile(t *testing.T) {
	fs := xrdioFS(t)

	f, err := xrdio.OpenFrom(fs, "/no-such-file.txt")
	if err == nil {
		f.Close()
		t.Fatal("a file that is not there was opened")
	}
	if !strings.Contains(err.Error(), "no-such-file.txt") {
		t.Errorf("the failure reads %q, want it to name the file", err)
	}
}

// TestWritesToAReadOnlyFileAreRefused covers io.Writer and io.WriterAt on the
// handle OpenFrom returns, which is opened for reading. The write has to be
// reported: a Write that returned nil would let a caller believe a copy
// succeeded and, because the position advances on a successful write, silently
// misplace everything after it.
func TestWritesToAReadOnlyFileAreRefused(t *testing.T) {
	fs := xrdioFS(t)

	f, err := xrdio.OpenFrom(fs, "/file.txt")
	if err != nil {
		t.Fatalf("could not open the test file: %v", err)
	}
	defer f.Close()

	if n, err := f.Write([]byte("nope")); err == nil {
		t.Errorf("Write reported %d bytes written to a read-only file, want an error", n)
	}
	if n, err := f.WriteAt([]byte("nope"), 0); err == nil {
		t.Errorf("WriteAt reported %d bytes written to a read-only file, want an error", n)
	}

	// The refused writes must not have moved the read position or the file.
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("could not read the file: %v", err)
	}
	if !bytes.Equal(got, xrdioContent) {
		t.Errorf("after the refused writes the file reads %q, want %q", got, xrdioContent)
	}
}
