// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for what a copy says when it cannot proceed.
//
// A copy is a command-line operation before it is an API: the caller reads the
// error and decides what to do next. "could not stat" and "is a directory (use
// Recursive)" are two different fixes, and a bare "copy failed" is neither. So
// these pin the shape of the refusal, not just that one happened — which end
// failed, what it was trying to do, and the path it was doing it to.

package xrdcopy_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdcopy"
)

// TestConformance_ACopyRefusalNamesTheEndAndTheOperation walks the refusals a
// caller can hit before a single byte moves.
func TestConformance_ACopyRefusalNamesTheEndAndTheOperation(t *testing.T) {
	noRedial(t)

	dir, url := copyServer(t)

	if err := os.Mkdir(filepath.Join(dir, "tree"), 0755); err != nil {
		t.Fatalf("could not create the remote directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tree", "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatalf("could not populate the remote directory: %v", err)
	}

	local := t.TempDir()
	localFile := filepath.Join(local, "src.bin")
	if err := os.WriteFile(localFile, []byte("go-hep"), 0644); err != nil {
		t.Fatalf("could not write the local file: %v", err)
	}
	localDir := filepath.Join(local, "tree")
	if err := os.Mkdir(localDir, 0755); err != nil {
		t.Fatalf("could not create the local directory: %v", err)
	}

	for _, tc := range []struct {
		name     string
		dst, src string
		opts     xrdcopy.Options
		want     []string
	}{
		{
			name: "a remote source that is not there",
			dst:  filepath.Join(t.TempDir(), "dst.bin"),
			src:  url + "/missing.bin",
			want: []string{"could not stat", "missing.bin"},
		},
		{
			name: "a remote directory without Recursive",
			dst:  filepath.Join(t.TempDir(), "dst.bin"),
			src:  url + "/tree",
			want: []string{"is a directory", "Recursive", "/tree"},
		},
		{
			name: "a local directory without Recursive",
			dst:  url + "/dst.bin",
			src:  localDir,
			want: []string{"is a directory", "Recursive", localDir},
		},
		{
			name: "a local source that is not there",
			dst:  url + "/dst.bin",
			src:  filepath.Join(local, "missing.bin"),
			want: []string{"missing.bin"},
		},
		{
			name: "a local directory copied locally",
			dst:  filepath.Join(t.TempDir(), "tree"),
			src:  localDir,
			opts: xrdcopy.Options{Recursive: true},
			want: []string{"local directory copy is not supported"},
		},
		{
			name: "a server that is not listening",
			dst:  filepath.Join(t.TempDir(), "dst.bin"),
			src:  "root://localhost:1//file.bin",
			want: []string{"could not connect", "localhost:1"},
		},
		{
			name: "a destination server that is not listening",
			dst:  "root://localhost:1//file.bin",
			src:  localFile,
			want: []string{"could not connect", "localhost:1"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts
			opts.Username = "gopher"

			err := xrdcopy.Copy(context.Background(), tc.dst, tc.src, opts)
			if err == nil {
				t.Fatal("the copy reported success")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("the failure does not mention %q: %v", want, err)
				}
			}
		})
	}
}

// TestConformance_AFailedCopyLeavesNoPartialFileBehindItsError: a download that
// fails part-way is reported, and the caller is not left with a file that looks
// finished. The engine writes into the destination it was given, so the
// contract here is only that the failure is visible — but a destination that
// was never opened must not exist at all.
func TestConformance_AFailedCopyLeavesNoDestinationItNeverOpened(t *testing.T) {
	_, url := copyServer(t)

	dst := filepath.Join(t.TempDir(), "dst.bin")
	err := xrdcopy.Copy(context.Background(), dst, url+"/missing.bin", xrdcopy.Options{Username: "gopher"})
	if err == nil {
		t.Fatal("the copy of a missing file reported success")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("the failed copy created %q anyway", dst)
	}
}

// TestConformance_AnUnparseableURLIsRejectedBeforeAnyConnection: the URL is
// classified first, so a malformed one fails without a network round trip and
// without touching the other end.
func TestConformance_AnUnparseableURLIsRejectedBeforeAnyConnection(t *testing.T) {
	noRedial(t)

	local := filepath.Join(t.TempDir(), "src.bin")
	if err := os.WriteFile(local, []byte("go-hep"), 0644); err != nil {
		t.Fatalf("could not write the local file: %v", err)
	}

	const bad = "root://%zz/file.bin"
	for _, tc := range []struct {
		name     string
		dst, src string
	}{
		{"as the source", filepath.Join(t.TempDir(), "dst.bin"), bad},
		{"as the destination", bad, local},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := xrdcopy.Copy(context.Background(), tc.dst, tc.src, xrdcopy.Options{Username: "gopher"}); err == nil {
				t.Fatal("a malformed URL was accepted")
			}
		})
	}
}

// TestConformance_ARecursiveCopyOfAnEmptyTreeSucceeds: an empty directory is a
// degenerate but legitimate input, and it must not be mistaken for a missing
// one.
func TestConformance_ARecursiveCopyOfAnEmptyTreeSucceeds(t *testing.T) {
	dir, url := copyServer(t)
	if err := os.Mkdir(filepath.Join(dir, "empty"), 0755); err != nil {
		t.Fatalf("could not create the remote directory: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "empty")
	err := xrdcopy.Copy(context.Background(), dst, url+"/empty", xrdcopy.Options{
		Username:  "gopher",
		Recursive: true,
	})
	if err != nil {
		t.Fatalf("could not copy an empty tree: %v", err)
	}

	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatalf("the destination directory was not created: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("the copy of an empty tree holds %d entries", len(entries))
	}
}
