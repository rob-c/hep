// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the upload half of the copy engine.
//
// Resume on download is pinned next door; the upload direction is the one that
// is easy to get subtly wrong, because the offset the client seeks to locally
// and the offset it writes to remotely have to agree. A resume that seeks the
// source but writes from zero produces a file of the right length holding the
// tail twice, which no size check catches.

package xrdcopy_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdcopy"
)

func uploadContent(t *testing.T) []byte {
	t.Helper()
	data := make([]byte, 32<<10+7)
	for i := range data {
		data[i] = byte(i * 13)
	}
	return data
}

func TestConformance_AnInterruptedUploadResumesWhereItStopped(t *testing.T) {
	dir, url := copyServer(t)

	data := uploadContent(t)
	src := filepath.Join(t.TempDir(), "src.bin")
	if err := os.WriteFile(src, data, 0644); err != nil {
		t.Fatalf("could not write the source file: %v", err)
	}

	// What an interrupted upload leaves behind: a prefix of the source.
	const partial = 4096
	if err := os.WriteFile(filepath.Join(dir, "dst.bin"), data[:partial], 0644); err != nil {
		t.Fatalf("could not write the partial destination: %v", err)
	}

	err := xrdcopy.Copy(context.Background(), url+"/dst.bin", src, xrdcopy.Options{
		Username:  "gopher",
		Resume:    true,
		ChunkSize: 8192,
	})
	if err != nil {
		t.Fatalf("could not resume the upload: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "dst.bin"))
	if err != nil {
		t.Fatalf("could not read the uploaded file: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("the resumed upload is %d bytes, want %d (first difference at %d)",
			len(got), len(data), firstDiff(got, data))
	}
}

func TestConformance_AnUploadThatIsAlreadyCompleteIsNotRepeated(t *testing.T) {
	// A resumed copy of a file that finished last time has nothing to do. It
	// must not truncate and re-send it — that is the failure mode that turns a
	// retry loop into an infinite transfer.
	dir, url := copyServer(t)

	data := uploadContent(t)
	src := filepath.Join(t.TempDir(), "src.bin")
	if err := os.WriteFile(src, data, 0644); err != nil {
		t.Fatalf("could not write the source file: %v", err)
	}

	remote := filepath.Join(dir, "dst.bin")
	if err := os.WriteFile(remote, data, 0644); err != nil {
		t.Fatalf("could not write the destination: %v", err)
	}
	before, err := os.Stat(remote)
	if err != nil {
		t.Fatalf("could not stat the destination: %v", err)
	}

	err = xrdcopy.Copy(context.Background(), url+"/dst.bin", src, xrdcopy.Options{
		Username: "gopher",
		Resume:   true,
	})
	if err != nil {
		t.Fatalf("could not resume a complete upload: %v", err)
	}

	after, err := os.Stat(remote)
	if err != nil {
		t.Fatalf("could not stat the destination: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("a complete upload was transferred again")
	}
}

func TestConformance_AnUploadWithoutResumeStartsOver(t *testing.T) {
	// Without Resume the destination is replaced outright, whatever was there.
	// A longer stale file left in place would leave a tail of the old content
	// past the end of the new one.
	dir, url := copyServer(t)

	data := []byte("go-hep xrootd")
	src := filepath.Join(t.TempDir(), "src.bin")
	if err := os.WriteFile(src, data, 0644); err != nil {
		t.Fatalf("could not write the source file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dst.bin"), bytes.Repeat([]byte("x"), 4096), 0644); err != nil {
		t.Fatalf("could not write the stale destination: %v", err)
	}

	err := xrdcopy.Copy(context.Background(), url+"/dst.bin", src, xrdcopy.Options{Username: "gopher"})
	if err != nil {
		t.Fatalf("could not upload: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "dst.bin"))
	if err != nil {
		t.Fatalf("could not read the uploaded file: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("the upload holds %d bytes, want %d", len(got), len(data))
	}
}

func TestConformance_AnUploadedTreeKeepsItsShape(t *testing.T) {
	// Recursive upload creates the remote directories as it goes. A walk that
	// flattened the tree, or joined the paths the wrong way round, produces
	// files that are all present and none findable.
	dir, url := copyServer(t)

	local := t.TempDir()
	files := map[string]string{
		"a.txt":              "a",
		"sub/b.txt":          "bb",
		"sub/deeper/c.txt":   "ccc",
		"sub/deeper/d.empty": "",
	}
	for name, content := range files {
		path := filepath.Join(local, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("could not create %q: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("could not write %q: %v", path, err)
		}
	}

	err := xrdcopy.Copy(context.Background(), url+"/tree", local, xrdcopy.Options{
		Username:  "gopher",
		Recursive: true,
	})
	if err != nil {
		t.Fatalf("could not upload the tree: %v", err)
	}

	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(dir, "tree", filepath.FromSlash(name)))
		if err != nil {
			t.Errorf("could not read %q: %v", name, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%q holds %q, want %q", name, got, want)
		}
	}
}

func TestConformance_AnUploadOfSomethingUnreadableFails(t *testing.T) {
	// The source is opened before anything is created remotely, so a source
	// that cannot be read leaves no half-made file at the far end.
	dir, url := copyServer(t)

	err := xrdcopy.Copy(context.Background(), url+"/dst.bin",
		filepath.Join(t.TempDir(), "absent.bin"), xrdcopy.Options{Username: "gopher"})
	if err == nil {
		t.Fatal("uploading a missing file reported success")
	}
	if _, err := os.Stat(filepath.Join(dir, "dst.bin")); !os.IsNotExist(err) {
		t.Fatal("a failed upload created the destination anyway")
	}
}

// firstDiff reports the index of the first differing byte, or -1.
func firstDiff(a, b []byte) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return n
	}
	return -1
}
