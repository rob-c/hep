// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for what a copy does when one step of it fails.
//
// A recursive copy is a walk, and the dangerous failure mode in a walk is not
// crashing — it is continuing. A tree copy that swallows the error on one file
// and returns nil reports success for a transfer that is missing data, and the
// caller has no way to tell: the destination exists, most of it is right, and
// nothing said otherwise. Every case below fails *somewhere in the middle* and
// checks that the error reaches the caller.
//
// The failures are induced with the local filesystem rather than a fake, so
// they are the errors the real code paths actually produce: a directory where a
// file should be, a file where a directory should be, and a mode that denies
// the process its own data.

package xrdcopy_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdcopy"
)

// unreadable makes path inaccessible for the duration of the test. It skips
// when that does not actually deny access — running as root, or on a
// filesystem that ignores the mode.
func unreadable(t *testing.T, path string) {
	t.Helper()

	mode := os.FileMode(0o000)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("could not stat %q: %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("could not chmod %q: %v", path, err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, fi.Mode().Perm()) })

	if fi.IsDir() {
		if _, err := os.ReadDir(path); err == nil {
			t.Skip("this process can read a mode-000 directory; the test cannot deny itself access")
		}
		return
	}
	if f, err := os.Open(path); err == nil {
		f.Close()
		t.Skip("this process can read a mode-000 file; the test cannot deny itself access")
	}
}

// copyTree writes a small tree under dir and returns it.
func copyTree(t *testing.T, dir string, files map[string]string) string {
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
	return dir
}

func TestConformance_ADownloadThatCannotWriteLocallyFails(t *testing.T) {
	dir, url := copyServer(t)
	if err := os.WriteFile(filepath.Join(dir, "src.bin"), []byte("go-hep xrootd"), 0644); err != nil {
		t.Fatalf("could not write the remote file: %v", err)
	}

	for _, tc := range []struct {
		name string
		dst  func(t *testing.T) string
	}{
		{"the destination is a directory", func(t *testing.T) string {
			// os.OpenFile on a directory fails, and it fails *after* the
			// remote file is open — the error has to survive that unwind.
			d := filepath.Join(t.TempDir(), "dst.bin")
			if err := os.Mkdir(d, 0o755); err != nil {
				t.Fatalf("could not create the directory: %v", err)
			}
			return d
		}},
		{"the destination's parent is a file", func(t *testing.T) string {
			// MkdirAll of the parent fails with ENOTDIR: a copy that ignored
			// this would go on to create nothing and report success.
			base := filepath.Join(t.TempDir(), "notadir")
			if err := os.WriteFile(base, []byte("x"), 0644); err != nil {
				t.Fatalf("could not write the blocking file: %v", err)
			}
			return filepath.Join(base, "sub", "dst.bin")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := xrdcopy.Copy(context.Background(), tc.dst(t), url+"/src.bin",
				xrdcopy.Options{Username: "gopher"})
			if err == nil {
				t.Fatal("a download that cannot be written reported success")
			}
		})
	}
}

func TestConformance_ADownloadedTreeStopsAtTheFirstFailure(t *testing.T) {
	// The failure is planted below the root, so it is only reached after the
	// walk has already copied something. A walk that returns nil here has
	// silently produced a partial tree.
	for _, tc := range []struct {
		name  string
		block func(t *testing.T, local string)
	}{
		{"a subdirectory cannot be created", func(t *testing.T, local string) {
			// "sub" already exists as a file, so MkdirAll for the remote
			// subdirectory fails during the recursion.
			if err := os.MkdirAll(local, 0o755); err != nil {
				t.Fatalf("could not create the destination: %v", err)
			}
			if err := os.WriteFile(filepath.Join(local, "sub"), []byte("x"), 0644); err != nil {
				t.Fatalf("could not write the blocking file: %v", err)
			}
		}},
		{"a file cannot be written", func(t *testing.T, local string) {
			// "a.txt" already exists as a directory, so opening it for
			// writing fails during the recursion.
			if err := os.MkdirAll(filepath.Join(local, "a.txt"), 0o755); err != nil {
				t.Fatalf("could not create the blocking directory: %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, url := copyServer(t)
			copyTree(t, filepath.Join(dir, "tree"), map[string]string{
				"a.txt":     "a",
				"sub/b.txt": "bb",
			})

			local := filepath.Join(t.TempDir(), "dst")
			tc.block(t, local)

			err := xrdcopy.Copy(context.Background(), local, url+"/tree",
				xrdcopy.Options{Username: "gopher", Recursive: true})
			if err == nil {
				t.Fatal("a partial tree copy reported success")
			}
		})
	}
}

func TestConformance_ADownloadOfAnUnreadableRemoteDirectoryFails(t *testing.T) {
	// The listing is what drives the walk, so a directory the server cannot
	// read is not an empty directory — a copy that treats it as one produces
	// an empty destination and calls it done.
	dir, url := copyServer(t)
	tree := copyTree(t, filepath.Join(dir, "tree"), map[string]string{"a.txt": "a"})
	unreadable(t, tree)

	err := xrdcopy.Copy(context.Background(), filepath.Join(t.TempDir(), "dst"), url+"/tree",
		xrdcopy.Options{Username: "gopher", Recursive: true})
	if err == nil {
		t.Fatal("an unreadable remote directory was copied as if it were empty")
	}
	if !strings.Contains(err.Error(), "could not list") {
		t.Fatalf("the failure does not say the listing failed: %v", err)
	}
}

func TestConformance_AnUploadedTreeStopsAtTheFirstFailure(t *testing.T) {
	// The upload side of the same property. Both failures are planted one
	// level down, after the walk has already transferred a file.
	for _, tc := range []struct {
		name  string
		block func(t *testing.T, local string)
	}{
		{"a subdirectory cannot be read", func(t *testing.T, local string) {
			unreadable(t, filepath.Join(local, "sub"))
		}},
		{"a file cannot be read", func(t *testing.T, local string) {
			unreadable(t, filepath.Join(local, "sub", "b.txt"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, url := copyServer(t)
			local := copyTree(t, t.TempDir(), map[string]string{
				"a.txt":     "a",
				"sub/b.txt": "bb",
			})
			tc.block(t, local)

			err := xrdcopy.Copy(context.Background(), url+"/tree", local,
				xrdcopy.Options{Username: "gopher", Recursive: true})
			if err == nil {
				t.Fatal("a partial tree upload reported success")
			}
		})
	}
}

func TestConformance_AnUploadThatCannotCreateTheRemotePathFails(t *testing.T) {
	// A file already occupies the name the upload needs as a directory. The
	// server refuses, and the refusal has to come back rather than being read
	// as "the directory is already there".
	dir, url := copyServer(t)
	if err := os.WriteFile(filepath.Join(dir, "blocked"), []byte("x"), 0644); err != nil {
		t.Fatalf("could not write the blocking file: %v", err)
	}
	local := copyTree(t, t.TempDir(), map[string]string{"a.txt": "a"})

	t.Run("as a tree", func(t *testing.T) {
		err := xrdcopy.Copy(context.Background(), url+"/blocked/tree", local,
			xrdcopy.Options{Username: "gopher", Recursive: true})
		if err == nil {
			t.Fatal("an upload into a path blocked by a file reported success")
		}
		if !strings.Contains(err.Error(), "mkdir") {
			t.Fatalf("the failure does not say the directory could not be made: %v", err)
		}
	})

	t.Run("as a single file", func(t *testing.T) {
		err := xrdcopy.Copy(context.Background(), url+"/blocked/a.txt",
			filepath.Join(local, "a.txt"), xrdcopy.Options{Username: "gopher"})
		if err == nil {
			t.Fatal("an upload into a path blocked by a file reported success")
		}
		if !strings.Contains(err.Error(), "could not create") {
			t.Fatalf("the failure does not say the file could not be created: %v", err)
		}
	})
}

func TestConformance_ALocalCopyReportsWhichEndFailed(t *testing.T) {
	// Local-to-local is the path with no protocol in it at all, and it is
	// still a copy: the same refusals have to reach the caller.
	src := filepath.Join(t.TempDir(), "src.bin")
	if err := os.WriteFile(src, []byte("go-hep xrootd"), 0644); err != nil {
		t.Fatalf("could not write the source: %v", err)
	}

	for _, tc := range []struct {
		name string
		dst  func(t *testing.T) string
		src  string
	}{
		{"the source is not there", func(t *testing.T) string {
			return filepath.Join(t.TempDir(), "dst.bin")
		}, filepath.Join(t.TempDir(), "absent.bin")},
		{"the destination is a directory", func(t *testing.T) string {
			d := filepath.Join(t.TempDir(), "dst.bin")
			if err := os.Mkdir(d, 0o755); err != nil {
				t.Fatalf("could not create the directory: %v", err)
			}
			return d
		}, src},
		{"the destination's parent is a file", func(t *testing.T) string {
			base := filepath.Join(t.TempDir(), "notadir")
			if err := os.WriteFile(base, []byte("x"), 0644); err != nil {
				t.Fatalf("could not write the blocking file: %v", err)
			}
			return filepath.Join(base, "sub", "dst.bin")
		}, src},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := xrdcopy.Copy(context.Background(), tc.dst(t), tc.src, xrdcopy.Options{}); err == nil {
				t.Fatal("a local copy that could not be made reported success")
			}
		})
	}
}

func TestConformance_ALocalCopyGoesThroughWhenItCan(t *testing.T) {
	// The negative cases above are only meaningful if the positive one works:
	// a local-to-local copy creates the parent directories it needs and lands
	// the bytes.
	src := filepath.Join(t.TempDir(), "src.bin")
	want := strings.Repeat("go-hep xrootd ", 500)
	if err := os.WriteFile(src, []byte(want), 0644); err != nil {
		t.Fatalf("could not write the source: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "a", "b", "dst.bin")
	if err := xrdcopy.Copy(context.Background(), dst, src, xrdcopy.Options{ChunkSize: 64}); err != nil {
		t.Fatalf("could not copy locally: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("could not read the copy: %v", err)
	}
	if string(got) != want {
		t.Fatalf("the copy holds %d bytes, want %d", len(got), len(want))
	}
}
