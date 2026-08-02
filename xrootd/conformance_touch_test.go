// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for xrdfs.Touch, driven over a connection to the in-process
// server rather than against a mock: what makes touch work is the exact answer
// the server gives to kXR_open with kXR_new against a file that is already
// there, so a mock written to give that answer would be testing itself.

package xrootd_test // import "go-hep.org/x/hep/xrootd"

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdfs"
)

// touchFS returns a client filesystem over a fresh server, together with the
// directory it serves so a test can look at what actually landed on disk.
func touchFS(t *testing.T) (xrdfs.FileSystem, string) {
	t.Helper()

	srv, addr, baseDir, err := createServer(t, func(err error) { t.Error(err) })
	if err != nil {
		t.Fatalf("could not create the server: %v", err)
	}
	t.Cleanup(func() {
		_ = srv.Shutdown(context.Background())
	})

	cli, err := createClient(addr)
	if err != nil {
		t.Fatalf("could not create the client: %v", err)
	}
	t.Cleanup(func() { cli.Close() })

	return cli.FS(), baseDir
}

func TestConformance_ATouchCreatesAFileThatIsNotThere(t *testing.T) {
	fsys, dir := touchFS(t)
	ctx := context.Background()

	if err := xrdfs.Touch(ctx, fsys, "/new.bin", xrdfs.TouchMode); err != nil {
		t.Fatalf("could not touch the file: %v", err)
	}

	fi, err := os.Stat(filepath.Join(dir, "new.bin"))
	if err != nil {
		t.Fatalf("the file is not there: %v", err)
	}
	if fi.Size() != 0 {
		t.Fatalf("the new file holds %d bytes, want 0", fi.Size())
	}
}

func TestConformance_ATouchOfAFileThatIsThereKeepsIt(t *testing.T) {
	// This is the whole point of the exercise. kXR_new is the only open that
	// creates without truncating, so a touch has to be built on it and has to
	// read the refusal it gets back as success: a client that reached for
	// kXR_delete instead, or that reported the refusal, would either destroy a
	// file somebody spent a day producing or make an idempotent operation fail
	// the second time it is run.
	fsys, dir := touchFS(t)
	ctx := context.Background()

	const content = "go-hep"
	name := filepath.Join(dir, "kept.bin")
	if err := os.WriteFile(name, []byte(content), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}
	before, err := os.Stat(name)
	if err != nil {
		t.Fatalf("could not stat the file: %v", err)
	}

	for i := range 3 {
		// Idempotent: touching it again is not an error the second time either.
		if err := xrdfs.Touch(ctx, fsys, "/kept.bin", xrdfs.TouchMode); err != nil {
			t.Fatalf("touch %d failed: %v", i+1, err)
		}
	}

	got, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("could not read the file back: %v", err)
	}
	if string(got) != content {
		t.Fatalf("the file now holds %q, want %q", got, content)
	}
	after, err := os.Stat(name)
	if err != nil {
		t.Fatalf("could not stat the file: %v", err)
	}
	if after.Mode() != before.Mode() {
		t.Fatalf("the mode became %v, want %v", after.Mode(), before.Mode())
	}
}

func TestConformance_ATouchMakesThePathLeadingToIt(t *testing.T) {
	// kXR_mkpath, and the directories it makes have to be ones the open that
	// asked for them can enter: made with the file's own 0600 they would not be,
	// and the touch would fail on a path it had just created itself.
	fsys, dir := touchFS(t)
	ctx := context.Background()

	if err := xrdfs.Touch(ctx, fsys, "/a/b/c/new.bin", xrdfs.TouchMode); err != nil {
		t.Fatalf("could not touch the file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a", "b", "c", "new.bin")); err != nil {
		t.Fatalf("the file is not there: %v", err)
	}

	// And the namespace it made is one the client can read back.
	ents, err := fsys.Dirlist(ctx, "/a/b/c")
	if err != nil {
		t.Fatalf("could not list the directory that was made: %v", err)
	}
	if len(ents) != 1 || ents[0].EntryName != "new.bin" {
		t.Fatalf("the directory holds %v, want just new.bin", ents)
	}
}

func TestConformance_ATouchThatCannotSucceedSaysSo(t *testing.T) {
	// A refusal that is not "it is already there" is a refusal, and swallowing
	// it would leave a caller believing in a file that was never made.
	fsys, dir := touchFS(t)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(dir, "f.bin"), []byte("go-hep"), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}

	err := xrdfs.Touch(ctx, fsys, "/f.bin/below.bin", xrdfs.TouchMode)
	if err == nil {
		t.Fatalf("touching a path below a file succeeded")
	}
	if errors.Is(err, fs.ErrExist) {
		t.Fatalf("the failure was read as an existing file: %v", err)
	}
}

func TestConformance_ATouchIsAnExclusiveCreate(t *testing.T) {
	// Only one of several clients touching the same new path can be the one that
	// created it: the open that creates is the one that fails for everybody
	// else, which is what makes touch usable as a lock file.
	fsys, _ := touchFS(t)
	ctx := context.Background()

	const name = "/lock"
	if err := xrdfs.Touch(ctx, fsys, name, xrdfs.TouchMode); err != nil {
		t.Fatalf("could not touch the file: %v", err)
	}

	// The same open Touch makes, with the refusal left visible.
	f, err := fsys.Open(ctx, name, xrdfs.TouchMode, xrdfs.OpenOptionsNew|xrdfs.OpenOptionsOpenUpdate)
	if err == nil {
		f.Close(ctx)
		t.Fatalf("a second exclusive create of %q succeeded", name)
	}
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("the refusal is %v, want an fs.ErrExist", err)
	}
}

func TestConformance_ATouchIsWritable(t *testing.T) {
	// The file a touch leaves behind is one its owner can write to: a mode with
	// no write bit would make the next job's open fail, and TouchMode is what a
	// caller who does not care is told to pass.
	fsys, dir := touchFS(t)
	ctx := context.Background()

	if err := xrdfs.Touch(ctx, fsys, "/w.bin", xrdfs.TouchMode); err != nil {
		t.Fatalf("could not touch the file: %v", err)
	}

	fi, err := os.Stat(filepath.Join(dir, path.Base("/w.bin")))
	if err != nil {
		t.Fatalf("the file is not there: %v", err)
	}
	if perm := fi.Mode().Perm(); perm&0600 != 0600 {
		t.Fatalf("the file was created %v, want at least 0600", perm)
	}
}
