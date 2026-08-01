// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the copies that cannot be made.
//
// xrd-cp is what runs inside job wrappers, and its exit status is the only
// thing the wrapper looks at. Every one of these is a case where the command
// has already printed a plausible-looking start and then cannot finish: a
// source that is not there, a destination directory that cannot exist, a file
// the server will not hand over. Each has to exit non-zero and say what
// happened, because the alternative is a job that reports success having staged
// nothing and a downstream step that reads a file it never got.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConformance_ASourceThatIsNotThereIsReportedBeforeAnyBytesMove(t *testing.T) {
	_, url := cpServer(t)
	dst := filepath.Join(t.TempDir(), "out.bin")

	stdout, stderr, code := runCLI(t, url+"nosuch.bin", dst)
	if code == 0 {
		t.Fatalf("a missing source exited 0 with:\n%s", stdout)
	}
	if !strings.Contains(stderr, "could not stat remote src") {
		t.Fatalf("stderr says %q, want it to say the source could not be statted", stderr)
	}
	if _, err := os.Stat(dst); err == nil {
		t.Fatal("a copy that never started created its destination")
	}
}

func TestConformance_ADestinationDirectoryThatCannotBeCreatedStopsTheCopy(t *testing.T) {
	// The destination does not exist and cannot be created: its parent is a
	// directory this process may not write to. A tree copy that ignored this
	// would go on to open every file in the tree against a directory that does
	// not exist.
	dir, url := cpServer(t)
	if err := os.MkdirAll(filepath.Join(dir, "tree"), 0o755); err != nil {
		t.Fatalf("could not create the remote tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tree", "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatalf("could not populate the remote tree: %v", err)
	}

	readonly := filepath.Join(t.TempDir(), "readonly")
	if err := os.Mkdir(readonly, 0o555); err != nil {
		t.Fatalf("could not create the read-only directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(readonly, 0o755) })
	if err := os.WriteFile(filepath.Join(readonly, "probe"), nil, 0644); err == nil {
		t.Skip("this process can write to a mode-555 directory; the test cannot deny itself access")
	}

	stdout, stderr, code := runCLI(t, "-r", url+"tree", filepath.Join(readonly, "out"))
	if code == 0 {
		t.Fatalf("a destination that cannot exist exited 0 with:\n%s", stdout)
	}
	if !strings.Contains(stderr, "could not create output directory") {
		t.Fatalf("stderr says %q, want it to name the directory it could not create", stderr)
	}
}

func TestConformance_AFileTheServerWillNotOpenIsAFailedCopy(t *testing.T) {
	// The file stats and will not open. The destination is created before the
	// source is opened, so the only thing that separates this from a successful
	// copy of an empty file is the exit status.
	dir, url := cpServer(t)
	remote := filepath.Join(dir, "locked.bin")
	if err := os.WriteFile(remote, []byte("go-hep"), 0644); err != nil {
		t.Fatalf("could not write the remote file: %v", err)
	}
	if err := os.Chmod(remote, 0o000); err != nil {
		t.Fatalf("could not chmod the remote file: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(remote, 0o644) })
	if _, err := os.ReadFile(remote); err == nil {
		t.Skip("this process can read a mode-000 file; the test cannot deny itself access")
	}

	dst := filepath.Join(t.TempDir(), "out.bin")
	stdout, _, code := runCLI(t, url+"locked.bin", dst)
	if code == 0 {
		t.Fatalf("a file the server would not open exited 0 with:\n%s", stdout)
	}
	if fi, err := os.Stat(dst); err == nil && fi.Size() != 0 {
		t.Fatalf("the failed copy left %d bytes behind", fi.Size())
	}
}
