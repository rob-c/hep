// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the copy engine behind xrd-cp, driven against an in-process
// XRootD server serving a temporary directory.
//
// The unit tests next to this one cover the pieces in isolation — URL
// classification, the resume arithmetic, the TPC key. These run the transfers
// themselves, in both directions, which is where the parts have to agree: a
// chunk size that does not divide the file, a resume that appends to the wrong
// offset and a recursive copy that flattens a tree all produce a plausible file
// and the wrong bytes.

package xrdcopy_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdcopy"
)

// copyServer starts an XRootD server on a fresh temporary directory and
// returns that directory and the root:// prefix that reaches it.
func copyServer(t *testing.T) (dir, url string) {
	t.Helper()

	dir = t.TempDir()

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

	return dir, fmt.Sprintf("root://%s/", listener.Addr())
}

// noRedial turns off the connection retries a client applies by default.
//
// A copy is driven by a URL rather than by a client the test builds, so there
// is no option to pass: XRD_CONNECTIONRETRY is how a program you did not write
// gets configured, and it is the mechanism under test here as much as it is the
// convenience. Tests that mean to observe one connection failure would
// otherwise wait out five redials — about eight seconds each — measuring the
// backoff schedule, which is pinned in xrootd's own conformance tests.
func noRedial(t *testing.T) {
	t.Helper()
	t.Setenv(xrootd.EnvConnectionRetry, "0")
}

// copyContent is 3 bytes over 64 kiB, so every chunk size below leaves a
// partial chunk at the end.
func copyContent(t *testing.T) []byte {
	t.Helper()
	data := make([]byte, 64<<10+3)
	for i := range data {
		data[i] = byte(i * 7)
	}
	return data
}

// TestDownload covers remote→local at chunk sizes that divide the file, that
// do not, and that exceed it in one read.
func TestDownload(t *testing.T) {
	dir, url := copyServer(t)
	data := copyContent(t)
	if err := os.WriteFile(filepath.Join(dir, "src.bin"), data, 0644); err != nil {
		t.Fatalf("could not write the source file: %v", err)
	}

	for _, chunk := range []int{0, 512, 4096, len(data), len(data) * 2} {
		t.Run(fmt.Sprintf("chunk-%d", chunk), func(t *testing.T) {
			dst := filepath.Join(t.TempDir(), "dst.bin")
			err := xrdcopy.Copy(context.Background(), dst, url+"/src.bin", xrdcopy.Options{
				ChunkSize: chunk,
				Username:  "gopher",
			})
			if err != nil {
				t.Fatalf("could not download: %v", err)
			}
			got, err := os.ReadFile(dst)
			if err != nil {
				t.Fatalf("could not read the destination: %v", err)
			}
			if !bytes.Equal(got, data) {
				t.Fatalf("the downloaded file differs: got %d bytes, want %d", len(got), len(data))
			}
		})
	}

	// One byte at a time, against a file small enough that the request per
	// byte does not dominate the run: this is the loop bound the chunk sizes
	// above never reach, where every read is a separate kXR_read.
	t.Run("chunk-1", func(t *testing.T) {
		small := []byte("go-hep")
		if err := os.WriteFile(filepath.Join(dir, "small.bin"), small, 0644); err != nil {
			t.Fatalf("could not write the source file: %v", err)
		}
		dst := filepath.Join(t.TempDir(), "dst.bin")
		err := xrdcopy.Copy(context.Background(), dst, url+"/small.bin", xrdcopy.Options{
			ChunkSize: 1,
			Username:  "gopher",
		})
		if err != nil {
			t.Fatalf("could not download: %v", err)
		}
		got, err := os.ReadFile(dst)
		if err != nil {
			t.Fatalf("could not read the destination: %v", err)
		}
		if !bytes.Equal(got, small) {
			t.Fatalf("the downloaded file is %q, want %q", got, small)
		}
	})
}

// TestUpload covers local→remote. The bytes are checked on the server's disk,
// not read back through the client, so a symmetric bug in both directions
// cannot hide.
func TestUpload(t *testing.T) {
	dir, url := copyServer(t)
	data := copyContent(t)

	src := filepath.Join(t.TempDir(), "src.bin")
	if err := os.WriteFile(src, data, 0644); err != nil {
		t.Fatalf("could not write the source file: %v", err)
	}

	for i, chunk := range []int{0, 512, 4096, len(data)} {
		t.Run(fmt.Sprintf("chunk-%d", chunk), func(t *testing.T) {
			name := fmt.Sprintf("up-%d.bin", i)
			err := xrdcopy.Copy(context.Background(), url+"/"+name, src, xrdcopy.Options{
				ChunkSize: chunk,
				Username:  "gopher",
			})
			if err != nil {
				t.Fatalf("could not upload: %v", err)
			}
			got, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("could not read the uploaded file: %v", err)
			}
			if !bytes.Equal(got, data) {
				t.Fatalf("the uploaded file differs: got %d bytes, want %d", len(got), len(data))
			}
		})
	}

	// One kXR_write per byte, on a file small enough to make that affordable.
	t.Run("chunk-1", func(t *testing.T) {
		small := []byte("go-hep")
		path := filepath.Join(t.TempDir(), "small.bin")
		if err := os.WriteFile(path, small, 0644); err != nil {
			t.Fatalf("could not write the source file: %v", err)
		}
		err := xrdcopy.Copy(context.Background(), url+"/up-small.bin", path, xrdcopy.Options{
			ChunkSize: 1,
			Username:  "gopher",
		})
		if err != nil {
			t.Fatalf("could not upload: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(dir, "up-small.bin"))
		if err != nil {
			t.Fatalf("could not read the uploaded file: %v", err)
		}
		if !bytes.Equal(got, small) {
			t.Fatalf("the uploaded file is %q, want %q", got, small)
		}
	})
}

// TestDownloadEmptyFile: an empty file is a file. The transfer loop must still
// create the destination, and the resume arithmetic must not read it as
// "nothing transferred yet, and nothing to transfer".
func TestDownloadEmptyFile(t *testing.T) {
	dir, url := copyServer(t)
	if err := os.WriteFile(filepath.Join(dir, "empty.bin"), nil, 0644); err != nil {
		t.Fatalf("could not write the source file: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "empty.bin")
	if err := xrdcopy.Copy(context.Background(), dst, url+"/empty.bin", xrdcopy.Options{Username: "gopher"}); err != nil {
		t.Fatalf("could not download: %v", err)
	}
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("the destination was not created: %v", err)
	}
	if fi.Size() != 0 {
		t.Fatalf("the destination holds %d bytes, want 0", fi.Size())
	}
}

// TestDownloadResume: a resumed download appends the remainder. The partial
// file below holds the true prefix of the source, so a resume that restarts
// from zero doubles the file and one that appends at the wrong offset gets the
// length right and the content wrong.
func TestDownloadResume(t *testing.T) {
	dir, url := copyServer(t)
	data := copyContent(t)
	if err := os.WriteFile(filepath.Join(dir, "src.bin"), data, 0644); err != nil {
		t.Fatalf("could not write the source file: %v", err)
	}

	for _, have := range []int{0, 1, 4096, len(data) - 1, len(data)} {
		t.Run(fmt.Sprintf("have-%d", have), func(t *testing.T) {
			dst := filepath.Join(t.TempDir(), "dst.bin")
			if err := os.WriteFile(dst, data[:have], 0644); err != nil {
				t.Fatalf("could not write the partial file: %v", err)
			}
			err := xrdcopy.Copy(context.Background(), dst, url+"/src.bin", xrdcopy.Options{
				Resume:    true,
				ChunkSize: 4096,
				Username:  "gopher",
			})
			if err != nil {
				t.Fatalf("could not resume the download: %v", err)
			}
			got, err := os.ReadFile(dst)
			if err != nil {
				t.Fatalf("could not read the destination: %v", err)
			}
			if !bytes.Equal(got, data) {
				t.Fatalf("the resumed file differs: got %d bytes, want %d", len(got), len(data))
			}
		})
	}
}

// TestDownloadWithoutResumeStartsOver: without Resume, a destination that
// already exists is overwritten, not appended to.
func TestDownloadWithoutResumeStartsOver(t *testing.T) {
	dir, url := copyServer(t)
	data := []byte("go-hep")
	if err := os.WriteFile(filepath.Join(dir, "src.bin"), data, 0644); err != nil {
		t.Fatalf("could not write the source file: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "dst.bin")
	if err := os.WriteFile(dst, []byte("stale content that is longer"), 0644); err != nil {
		t.Fatalf("could not write the stale file: %v", err)
	}
	if err := xrdcopy.Copy(context.Background(), dst, url+"/src.bin", xrdcopy.Options{Username: "gopher"}); err != nil {
		t.Fatalf("could not download: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("could not read the destination: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("the destination is %q, want %q", got, data)
	}
}

// TestRecursiveCopy: a directory tree has to arrive as a tree, in both
// directions, and only when the caller asked for it.
func TestRecursiveCopy(t *testing.T) {
	dir, url := copyServer(t)

	files := map[string][]byte{
		"a.txt":       []byte("a"),
		"sub/b.txt":   []byte("bb"),
		"sub/c/d.txt": []byte("ddd"),
	}
	for name, data := range files {
		path := filepath.Join(dir, "tree", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("could not create %q: %v", path, err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatalf("could not write %q: %v", path, err)
		}
	}

	t.Run("refused without the option", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "tree")
		err := xrdcopy.Copy(context.Background(), dst, url+"/tree", xrdcopy.Options{Username: "gopher"})
		if err == nil {
			t.Fatal("a directory was copied without Recursive")
		}
	})

	t.Run("download", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "tree")
		err := xrdcopy.Copy(context.Background(), dst, url+"/tree", xrdcopy.Options{
			Recursive: true,
			Username:  "gopher",
		})
		if err != nil {
			t.Fatalf("could not download the tree: %v", err)
		}
		for name, want := range files {
			got, err := os.ReadFile(filepath.Join(dst, name))
			if err != nil {
				t.Errorf("could not read %q: %v", name, err)
				continue
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%q is %q, want %q", name, got, want)
			}
		}
	})

	t.Run("upload", func(t *testing.T) {
		src := filepath.Join(dir, "tree")
		err := xrdcopy.Copy(context.Background(), url+"/uploaded", src, xrdcopy.Options{
			Recursive: true,
			Username:  "gopher",
		})
		if err != nil {
			t.Fatalf("could not upload the tree: %v", err)
		}
		for name, want := range files {
			got, err := os.ReadFile(filepath.Join(dir, "uploaded", name))
			if err != nil {
				t.Errorf("could not read %q: %v", name, err)
				continue
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%q is %q, want %q", name, got, want)
			}
		}
	})
}

// TestCopyReportsFailures: every one of these is a routine outcome for a
// network copy, and each must be reported rather than leaving a truncated file
// behind and returning nil.
func TestCopyReportsFailures(t *testing.T) {
	noRedial(t)

	_, url := copyServer(t)

	for _, tc := range []struct {
		name     string
		dst, src string
	}{
		{"no such remote file", filepath.Join(t.TempDir(), "dst.bin"), url + "/missing.bin"},
		{"no such local file", url + "/dst.bin", filepath.Join(t.TempDir(), "missing.bin")},
		{"no such server", filepath.Join(t.TempDir(), "dst.bin"), "root://localhost:1//file.bin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := xrdcopy.Copy(context.Background(), tc.dst, tc.src, xrdcopy.Options{Username: "gopher"}); err == nil {
				t.Fatal("the copy reported success")
			}
		})
	}
}

// TestCopyHonoursContext: a cancelled context stops a copy instead of running
// it to completion, which is what a caller's timeout relies on.
func TestCopyHonoursContext(t *testing.T) {
	dir, url := copyServer(t)
	if err := os.WriteFile(filepath.Join(dir, "src.bin"), copyContent(t), 0644); err != nil {
		t.Fatalf("could not write the source file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dst := filepath.Join(t.TempDir(), "dst.bin")
	if err := xrdcopy.Copy(ctx, dst, url+"/src.bin", xrdcopy.Options{Username: "gopher"}); err == nil {
		t.Fatal("a copy on a cancelled context reported success")
	}
}

// TestCopyCarriesOpaqueDataThroughTheWalk: a remote URL usually carries a
// bearer token as opaque data, and a recursive copy has to hand it to every
// request it makes — each level of the tree is authorized on its own. The
// names it builds from a listing therefore have to inherit the CGI of the path
// the caller named, without it leaking into a file name at either end: a walk
// that string-joins onto "/tree?authz=tok" asks for "/tree?authz=tok/a.txt",
// which no server holds, and writes a local file called "a.txt?authz=tok".
func TestCopyCarriesOpaqueDataThroughTheWalk(t *testing.T) {
	const token = "?authz=tok&xrd.wantprot=unix"

	dir, url := copyServer(t)

	files := map[string][]byte{
		"a.txt":       []byte("a"),
		"sub/b.txt":   []byte("bb"),
		"sub/c/d.txt": []byte("ddd"),
	}
	for name, data := range files {
		path := filepath.Join(dir, "tree", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("could not create %q: %v", path, err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatalf("could not write %q: %v", path, err)
		}
	}

	t.Run("single file", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "a.txt")
		err := xrdcopy.Copy(context.Background(), dst, url+"/tree/a.txt"+token, xrdcopy.Options{
			Username: "gopher",
		})
		if err != nil {
			t.Fatalf("could not download: %v", err)
		}
		got, err := os.ReadFile(dst)
		if err != nil {
			t.Fatalf("could not read the downloaded file: %v", err)
		}
		if want := files["a.txt"]; !bytes.Equal(got, want) {
			t.Fatalf("downloaded %q, want %q", got, want)
		}
	})

	t.Run("download", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "tree")
		err := xrdcopy.Copy(context.Background(), dst, url+"/tree"+token, xrdcopy.Options{
			Recursive: true,
			Username:  "gopher",
		})
		if err != nil {
			t.Fatalf("could not download the tree: %v", err)
		}
		for name, want := range files {
			got, err := os.ReadFile(filepath.Join(dst, name))
			if err != nil {
				t.Errorf("could not read %q: %v", name, err)
				continue
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%q is %q, want %q", name, got, want)
			}
		}
	})

	t.Run("upload", func(t *testing.T) {
		src := filepath.Join(dir, "tree")
		err := xrdcopy.Copy(context.Background(), url+"/tokened"+token, src, xrdcopy.Options{
			Recursive: true,
			Username:  "gopher",
		})
		if err != nil {
			t.Fatalf("could not upload the tree: %v", err)
		}
		for name, want := range files {
			got, err := os.ReadFile(filepath.Join(dir, "tokened", name))
			if err != nil {
				t.Errorf("could not read %q: %v", name, err)
				continue
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%q is %q, want %q", name, got, want)
			}
		}
	})
}
