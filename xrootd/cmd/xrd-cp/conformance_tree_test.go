// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for xrd-cp walking a remote tree, and for the failures it can
// meet part way through one.
//
// `cp -r` has one behaviour everybody relies on: whether the destination
// already exists decides where the tree lands. "cp -r src dst" with no dst
// creates dst *as* src, and with an existing dst creates dst/src. Getting that
// backwards does not fail — it puts a hundred thousand files one directory
// away from where the next job looks for them.
//
// The failures below all happen after something has already been copied, which
// is the case that matters: a walk that swallows them exits 0 having written a
// tree that is missing files.

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// errWriter refuses everything written to it, standing in for a pipe whose
// reader has gone away.
type errWriter struct{}

func (errWriter) Write(p []byte) (int, error) { return 0, errors.New("the reader is gone") }

// cpTree populates a remote tree under dir.
func cpTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("could not create %q: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("could not write %q: %v", path, err)
		}
	}
}

// cpUnreadable denies the process access to path, skipping if it cannot.
func cpUnreadable(t *testing.T, path string) {
	t.Helper()

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("could not stat %q: %v", path, err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("could not chmod %q: %v", path, err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, fi.Mode().Perm()) })

	if _, err := os.ReadDir(path); err == nil {
		t.Skip("this process can read a mode-000 directory; the test cannot deny itself access")
	}
}

func TestConformance_ADestinationThatIsNotThereBecomesTheTree(t *testing.T) {
	// "xrd-cp -r root://host//store/run3 /data/run3" with /data/run3 absent
	// copies the *contents* into it, rather than creating /data/run3/run3.
	// This is what cp does, and what every script around this command assumes.
	dir, url := cpServer(t)
	cpTree(t, filepath.Join(dir, "tree"), map[string]string{
		"a.txt":     "a",
		"sub/b.txt": "bb",
	})

	dst := filepath.Join(t.TempDir(), "outdir")
	_, stderr, code := runCLI(t, "-r", url+"/tree", dst)
	if code != 0 {
		t.Fatalf("the copy exited %d: %s", code, stderr)
	}

	for name, want := range map[string]string{
		"a.txt":     "a",
		"sub/b.txt": "bb",
	} {
		got, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("could not read %q from the copied tree: %v", name, err)
		}
		if string(got) != want {
			t.Fatalf("%q holds %q, want %q", name, got, want)
		}
	}

	// The tree went *into* the destination, not one level below it.
	if _, err := os.Stat(filepath.Join(dst, "tree")); err == nil {
		t.Fatal("the copy nested the tree inside the destination it was told to create")
	}
}

func TestConformance_ATreeCopyStopsWhenItCannotWrite(t *testing.T) {
	// Both failures are planted one level down, so the walk has already created
	// the top of the tree and copied a file by the time it meets them.
	for _, tc := range []struct {
		name  string
		block func(t *testing.T, dst string)
		want  string
	}{
		{"a subdirectory is blocked by a file", func(t *testing.T, dst string) {
			if err := os.MkdirAll(filepath.Join(dst, "tree"), 0o755); err != nil {
				t.Fatalf("could not create the destination: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dst, "tree", "sub"), []byte("x"), 0o644); err != nil {
				t.Fatalf("could not write the blocking file: %v", err)
			}
		}, "could not create output directory"},
		{"a file is blocked by a directory", func(t *testing.T, dst string) {
			if err := os.MkdirAll(filepath.Join(dst, "tree", "sub", "b.txt"), 0o755); err != nil {
				t.Fatalf("could not create the blocking directory: %v", err)
			}
		}, "could not create output file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, url := cpServer(t)
			cpTree(t, filepath.Join(dir, "tree"), map[string]string{
				"a.txt":     "a",
				"sub/b.txt": "bb",
			})

			dst := t.TempDir()
			tc.block(t, dst)

			_, stderr, code := runCLI(t, "-r", url+"/tree", dst)
			if code == 0 {
				t.Fatal("a partial tree copy exited 0")
			}
			if !strings.Contains(stderr, tc.want) {
				t.Fatalf("stderr does not say %q:\n%s", tc.want, stderr)
			}
		})
	}
}

func TestConformance_ATreeCopyStopsWhenItCannotList(t *testing.T) {
	// The listing is what drives the walk, so a directory the server cannot read
	// is not an empty directory. Both the top of the tree and a directory inside
	// it have to fail the same way.
	for _, tc := range []struct {
		name  string
		block string
		dst   func(t *testing.T) string
	}{
		{"the top of the tree, into a new destination", "tree", func(t *testing.T) string {
			return filepath.Join(t.TempDir(), "outdir")
		}},
		{"a directory inside the tree", "tree/sub", func(t *testing.T) string {
			return t.TempDir()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, url := cpServer(t)
			cpTree(t, filepath.Join(dir, "tree"), map[string]string{
				"a.txt":     "a",
				"sub/b.txt": "bb",
			})
			cpUnreadable(t, filepath.Join(dir, filepath.FromSlash(tc.block)))

			_, stderr, code := runCLI(t, "-r", url+"/tree", tc.dst(t))
			if code == 0 {
				t.Fatal("a directory that could not be listed was copied as if it were empty")
			}
			if !strings.Contains(stderr, "could not") {
				t.Fatalf("stderr does not say what failed:\n%s", stderr)
			}
		})
	}
}

func TestConformance_ADestinationThatCannotBeStattedIsAFailure(t *testing.T) {
	// A destination under a path component that is a file gives ENOTDIR rather
	// than ENOENT. Reading every stat failure as "it is not there yet" would
	// send the command on to create a file it cannot create, and report the
	// wrong reason when it could not.
	dir, url := cpServer(t)
	if err := os.WriteFile(filepath.Join(dir, "src.bin"), []byte("go-hep"), 0644); err != nil {
		t.Fatalf("could not write the remote file: %v", err)
	}
	cpTree(t, filepath.Join(dir, "tree"), map[string]string{"a.txt": "a"})

	blocking := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(blocking, []byte("x"), 0644); err != nil {
		t.Fatalf("could not write the blocking file: %v", err)
	}
	dst := filepath.Join(blocking, "sub")

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"a single file", []string{url + "/src.bin", dst}},
		{"a tree", []string{"-r", url + "/tree", dst}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, code := runCLI(t, tc.args...)
			if code == 0 {
				t.Fatal("a destination that could not be statted exited 0")
			}
			if !strings.Contains(stderr, "could not stat local dst") {
				t.Fatalf("stderr does not say the destination could not be statted:\n%s", stderr)
			}
		})
	}
}

func TestConformance_ASourceThatIsNotAURLIsRejectedBeforeDialling(t *testing.T) {
	// The address is parsed before anything is opened, so a typo is a parse
	// error naming the operand rather than a connection timeout naming a host
	// the user never wrote.
	for _, src := range []string{
		"root://[::1//f.bin",
		"root://example.org:1:2//f.bin",
	} {
		t.Run(src, func(t *testing.T) {
			_, stderr, code := runCLI(t, src, filepath.Join(t.TempDir(), "d.bin"))
			if code == 0 {
				t.Fatalf("%q was accepted as a source", src)
			}
			if !strings.Contains(stderr, "could not parse") {
				t.Fatalf("stderr does not say the source could not be parsed:\n%s", stderr)
			}
			if !strings.Contains(stderr, src) {
				t.Fatalf("stderr does not quote the operand:\n%s", stderr)
			}
		})
	}
}

func TestConformance_ADestinationThatRefusesTheBytesIsAFailure(t *testing.T) {
	// "xrd-cp src - | head" leaves the command writing to a closed pipe. The
	// write error has to become a non-zero exit: a transfer that ends early and
	// reports success is a truncated file wherever the pipeline put it.
	dir, url := cpServer(t)
	if err := os.WriteFile(filepath.Join(dir, "src.bin"), bytes.Repeat([]byte("go-hep "), 1024), 0644); err != nil {
		t.Fatalf("could not write the remote file: %v", err)
	}

	var stderr bytes.Buffer
	if code := run(errWriter{}, &stderr, []string{url + "/src.bin", "-"}); code == 0 {
		t.Fatal("a copy to a destination that refused the bytes exited 0")
	}
	if !strings.Contains(stderr.String(), "could not copy") {
		t.Fatalf("stderr does not say the copy failed:\n%s", stderr.String())
	}
}
