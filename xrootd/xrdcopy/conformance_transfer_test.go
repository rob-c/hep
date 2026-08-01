// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the failures that arrive in the middle of a transfer, once
// both ends have been opened and bytes are moving.
//
// These are the dangerous ones. A copy that refuses to start leaves nothing
// behind; a copy that stops halfway leaves a file of the right name and the
// wrong length. If the error is swallowed, the next job reads that file and
// gets a truncated ROOT tree — with no indication anywhere that the transfer
// was the cause. So every one of them has to surface as an error from Copy.

package xrdcopy_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdcopy"
)

// devFull returns /dev/full, a destination that accepts an open and refuses
// every write with ENOSPC — a full filesystem without needing one.
func devFull(t *testing.T) string {
	t.Helper()

	f, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	if err != nil {
		t.Skipf("/dev/full is not available here: %v", err)
	}
	defer f.Close()

	if _, err := f.Write([]byte("x")); err == nil {
		t.Skip("/dev/full accepts writes here, so it cannot stand in for a full disk")
	}
	return "/dev/full"
}

func TestConformance_AURLThatCannotBeParsedFailsOnEitherEnd(t *testing.T) {
	// Both operands are parsed before either is opened. A source that is not a
	// URL must not be mistaken for a local path of the same spelling: the copy
	// would then look for "root:" in the working directory and report that it
	// is missing.
	local := filepath.Join(t.TempDir(), "src.bin")
	if err := os.WriteFile(local, []byte("go-hep"), 0644); err != nil {
		t.Fatalf("could not write the local file: %v", err)
	}

	// A bracketed host that is never closed: net/url refuses it outright.
	const bad = "root://[::1//file.bin"

	for _, tc := range []struct {
		name     string
		dst, src string
	}{
		{"as the source", filepath.Join(t.TempDir(), "dst.bin"), bad},
		{"as the destination", bad, local},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := xrdcopy.Copy(context.Background(), tc.dst, tc.src, xrdcopy.Options{Username: "gopher"})
			if err == nil {
				t.Fatal("a malformed URL was accepted")
			}
			if _, serr := os.Stat(tc.dst); serr == nil && tc.dst != bad {
				t.Fatal("a copy that never started created its destination")
			}
		})
	}
}

func TestConformance_ACopyThatCannotWriteEveryByteFails(t *testing.T) {
	// The destination accepts the open and then refuses the data. This is what
	// a full pool disk looks like from the client side, and it is precisely the
	// case where a silent success is unrecoverable: the destination exists, is
	// short, and nothing says so.
	dst := devFull(t)

	t.Run("downloading", func(t *testing.T) {
		dir, url := copyServer(t)
		if err := os.WriteFile(filepath.Join(dir, "src.bin"), []byte(strings.Repeat("go-hep ", 512)), 0644); err != nil {
			t.Fatalf("could not write the remote file: %v", err)
		}

		err := xrdcopy.Copy(context.Background(), dst, url+"/src.bin", xrdcopy.Options{Username: "gopher"})
		if err == nil {
			t.Fatal("a download that could not write its destination succeeded")
		}
		if !strings.Contains(err.Error(), "could not copy") {
			t.Fatalf("the failure does not say the copy stopped: %v", err)
		}
	})

	t.Run("locally", func(t *testing.T) {
		src := filepath.Join(t.TempDir(), "src.bin")
		if err := os.WriteFile(src, []byte(strings.Repeat("go-hep ", 512)), 0644); err != nil {
			t.Fatalf("could not write the local file: %v", err)
		}

		if err := xrdcopy.Copy(context.Background(), dst, src, xrdcopy.Options{}); err == nil {
			t.Fatal("a local copy that could not write its destination succeeded")
		}
	})
}

func TestConformance_ADownloadOfAFileThatCannotBeOpenedFails(t *testing.T) {
	// The file is there — it stats — and the server refuses to open it. That
	// gap between "exists" and "readable" is where a download that trusted its
	// own stat would create an empty local file and report success.
	dir, url := copyServer(t)
	remote := filepath.Join(dir, "locked.bin")
	if err := os.WriteFile(remote, []byte("go-hep"), 0644); err != nil {
		t.Fatalf("could not write the remote file: %v", err)
	}
	unreadable(t, remote)

	dst := filepath.Join(t.TempDir(), "dst.bin")
	if err := xrdcopy.Copy(context.Background(), dst, url+"/locked.bin", xrdcopy.Options{Username: "gopher"}); err == nil {
		t.Fatal("a file the server would not open was downloaded")
	}
	if _, err := os.Stat(dst); err == nil {
		t.Fatal("a download that never read a byte created its destination")
	}
}

func TestConformance_AnUploadOfADirectoryIsAFailureNotAnEmptyFile(t *testing.T) {
	// "xrdcopy root://host//store/x /some/dir" without Recursive. The local
	// directory opens like a file and refuses to be read, and the upload has
	// already created the remote path by then. Reporting success would leave a
	// zero-length object where a dataset was meant to be.
	dir, url := copyServer(t)
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatalf("could not populate the local directory: %v", err)
	}

	err := xrdcopy.Copy(context.Background(), url+"/uploaded.bin", src, xrdcopy.Options{Username: "gopher"})
	if err == nil {
		t.Fatal("a directory was uploaded as if it were a file")
	}

	// Whatever the server was left holding, it is not a copy of anything.
	if fi, serr := os.Stat(filepath.Join(dir, "uploaded.bin")); serr == nil && fi.Size() != 0 {
		t.Fatalf("the failed upload left %d bytes at the destination", fi.Size())
	}
}
