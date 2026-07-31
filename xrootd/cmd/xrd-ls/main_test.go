// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd"
)

// lsServer starts an in-process XRootD server over a small tree and returns the
// root:// prefix that reaches it. The tree is deliberately two levels deep with
// a file at each level, which is the shape that tells a recursive listing from
// a flat one.
func lsServer(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	for _, name := range []string{"a.txt", "sub/b.txt", "sub/deeper/c.txt"} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("could not create %q: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(name), 0644); err != nil {
			t.Fatalf("could not write %q: %v", path, err)
		}
	}

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

	return fmt.Sprintf("root://%s/", listener.Addr())
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what it
// printed. xrd-ls writes its listing there directly, so this is the only way to
// see what the command actually produced.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("could not create a pipe: %v", err)
	}
	defer r.Close()

	old := os.Stdout
	os.Stdout = w
	fnErr := fn()
	os.Stdout = old
	w.Close()

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("could not read the captured output: %v", err)
	}
	return string(out), fnErr
}

func TestXrdLs_File(t *testing.T) {
	url := lsServer(t)

	out, err := captureStdout(t, func() error { return xrdls(url+"/a.txt", false, false) })
	if err != nil {
		t.Fatalf("could not list the file: %v", err)
	}
	if got := strings.TrimSpace(out); got != "/a.txt" {
		t.Fatalf("listing a file printed %q, want %q", got, "/a.txt")
	}
}

func TestXrdLs_Directory(t *testing.T) {
	url := lsServer(t)

	out, err := captureStdout(t, func() error { return xrdls(url+"/", false, false) })
	if err != nil {
		t.Fatalf("could not list the directory: %v", err)
	}
	for _, want := range []string{"/a.txt", "/sub"} {
		if !strings.Contains(out, want) {
			t.Errorf("the listing does not mention %q:\n%s", want, out)
		}
	}
	// Without -R, the contents of subdirectories stay out of it.
	if strings.Contains(out, "b.txt") {
		t.Errorf("a non-recursive listing descended into sub:\n%s", out)
	}
}

func TestXrdLs_Recursive(t *testing.T) {
	url := lsServer(t)

	out, err := captureStdout(t, func() error { return xrdls(url+"/", false, true) })
	if err != nil {
		t.Fatalf("could not list the tree: %v", err)
	}
	for _, want := range []string{"/a.txt", "/sub/b.txt", "/sub/deeper/c.txt"} {
		if !strings.Contains(out, want) {
			t.Errorf("the recursive listing does not mention %q:\n%s", want, out)
		}
	}
}

// TestXrdLs_Long checks the long format carries the size, which is the field
// the format string is really there for.
func TestXrdLs_Long(t *testing.T) {
	url := lsServer(t)

	out, err := captureStdout(t, func() error { return xrdls(url+"/a.txt", true, false) })
	if err != nil {
		t.Fatalf("could not list the file: %v", err)
	}
	if want := fmt.Sprint(len("a.txt")); !strings.Contains(out, want) {
		t.Fatalf("the long listing does not carry the size %s:\n%s", want, out)
	}
	if !strings.Contains(out, "/a.txt") {
		t.Fatalf("the long listing does not carry the name:\n%s", out)
	}
}

func TestXrdLs_Failures(t *testing.T) {
	url := lsServer(t)

	for _, tc := range []struct {
		name string
		arg  string
	}{
		{"no such path", url + "/missing.txt"},
		{"no such server", "root://localhost:1//a.txt"},
		{"not a url", "root://%zz//a.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := captureStdout(t, func() error { return xrdls(tc.arg, false, false) })
			if err == nil {
				t.Fatal("the listing reported success")
			}
		})
	}
}
