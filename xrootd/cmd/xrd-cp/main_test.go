// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"go-hep.org/x/hep/xrootd"
)

func TestXrdCp(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "chain.1.root")
	src := "root://ccxrootdgotest.in2p3.fr:9001/tmp/rootio/testdata/chain.1.root"

	const (
		recursive = false
		verbose   = true
	)

	err := xrdcopy(dst, src, recursive, verbose)
	if err != nil {
		t.Fatalf("could not copy remote file: %v", err)
	}
}

func BenchmarkXrdCp_Small(b *testing.B) {
	benchmarkXrdCp(b, "root://ccxrootdgotest.in2p3.fr:9001/tmp/rootio/testdata/chain.1.root")
}

func BenchmarkXrdCp_Medium(b *testing.B) {
	benchmarkXrdCp(b, "root://eospublic.cern.ch//eos/root-eos/cms_opendata_2012_nanoaod/SMHiggsToZZTo4L.root")
}

func BenchmarkXrdCp_Large(b *testing.B) {
	benchmarkXrdCp(b, "root://eospublic.cern.ch//eos/root-eos/cms_opendata_2012_nanoaod/Run2012B_DoubleElectron.root")
}

func benchmarkXrdCp(b *testing.B, src string) {
	dir := b.TempDir()
	dst := filepath.Join(dir, filepath.Base(src))

	const (
		recursive = false
		verbose   = false
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		os.RemoveAll(dst)
		err := xrdcopy(dst, src, recursive, verbose)
		if err != nil {
			b.Fatalf("could not copy remote file: %v", err)
		}
	}
}

// cpServer starts an in-process server over a temporary directory and returns
// that directory and the root:// prefix reaching it. xrd-cp only ever reads
// from the remote side, so the fixtures are written to the directory directly.
func cpServer(t *testing.T) (dir, url string) {
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
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	return dir, fmt.Sprintf("root://%s/", listener.Addr())
}

// TestXrdCp_Offline covers the copy the command actually performs — remote to
// local — against an in-process server, so the path is exercised without
// reaching out to a remote one.
func TestXrdCp_Offline(t *testing.T) {
	dir, url := cpServer(t)

	// Comfortably more than one chunk, and not a round number of them.
	data := bytes.Repeat([]byte("go-hep xrootd "), 5000)
	if err := os.WriteFile(filepath.Join(dir, "remote.bin"), data, 0644); err != nil {
		t.Fatalf("could not write the remote file: %v", err)
	}

	const (
		recursive = false
		verbose   = false
	)

	dst := filepath.Join(t.TempDir(), "dst.bin")
	if err := xrdcopy(dst, url+"/remote.bin", recursive, verbose); err != nil {
		t.Fatalf("could not download: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("could not read the downloaded file: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("the downloaded file is %d bytes, want %d", len(got), len(data))
	}
}

// TestXrdCp_OfflineRecursive: a directory arrives as a directory, and only when
// -r was given. Refusing without it is what keeps a mistyped path from silently
// copying nothing.
func TestXrdCp_OfflineRecursive(t *testing.T) {
	dir, url := cpServer(t)

	files := map[string][]byte{
		"a.txt":     []byte("a"),
		"sub/b.txt": []byte("bb"),
	}
	for name, data := range files {
		path := filepath.Join(dir, "tree", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("could not create %q: %v", path, err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatalf("could not write %q: %v", path, err)
		}
	}

	t.Run("refused without -r", func(t *testing.T) {
		if err := xrdcopy(t.TempDir(), url+"/tree", false, false); err == nil {
			t.Fatal("a directory was copied without -r")
		}
	})

	t.Run("copied with -r", func(t *testing.T) {
		dst := t.TempDir()
		if err := xrdcopy(dst, url+"/tree", true, false); err != nil {
			t.Fatalf("could not download the tree: %v", err)
		}
		for name, want := range files {
			got, err := os.ReadFile(filepath.Join(dst, "tree", filepath.FromSlash(name)))
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

func TestXrdCp_OfflineFailures(t *testing.T) {
	_, url := cpServer(t)

	for _, tc := range []struct {
		name string
		src  string
	}{
		{"no such remote file", url + "/missing.bin"},
		{"no such server", "root://localhost:1//file.bin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dst := filepath.Join(t.TempDir(), "dst.bin")
			if err := xrdcopy(dst, tc.src, false, false); err == nil {
				t.Fatal("the copy reported success")
			}
		})
	}
}

// TestXrdCp_OfflineOpaque: the same walk as above, plus the half that only a
// copy has — the local name. A file downloaded from "/tree/a.txt?authz=tok"
// is called "a.txt" on disk; a token is a credential, not part of a name.
func TestXrdCp_OfflineOpaque(t *testing.T) {
	const token = "?authz=tok&xrd.wantprot=unix"

	dir, url := cpServer(t)

	files := map[string][]byte{
		"a.txt":     []byte("a"),
		"sub/b.txt": []byte("bb"),
	}
	for name, data := range files {
		path := filepath.Join(dir, "tree", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("could not create %q: %v", path, err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatalf("could not write %q: %v", path, err)
		}
	}

	t.Run("file", func(t *testing.T) {
		dst := t.TempDir()
		if err := xrdcopy(dst, url+"/tree/a.txt"+token, false, false); err != nil {
			t.Fatalf("could not download: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(dst, "a.txt"))
		if err != nil {
			t.Fatalf("could not read the downloaded file: %v", err)
		}
		if !bytes.Equal(got, files["a.txt"]) {
			t.Fatalf("downloaded %q, want %q", got, files["a.txt"])
		}
	})

	t.Run("tree", func(t *testing.T) {
		dst := t.TempDir()
		if err := xrdcopy(dst, url+"/tree"+token, true, false); err != nil {
			t.Fatalf("could not download the tree: %v", err)
		}
		for name, want := range files {
			got, err := os.ReadFile(filepath.Join(dst, "tree", filepath.FromSlash(name)))
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
