// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
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

// noRedial turns off the connection retries a client applies by default.
//
// The listing is driven by a URL rather than by a client the test builds, so there is no
// option to pass: XRD_CONNECTIONRETRY is how a program you did not write gets
// configured. A test that means to observe one connection failure would
// otherwise wait out five redials — about eight seconds each — measuring the
// backoff schedule, which is pinned in xrootd's own conformance tests.
func noRedial(t *testing.T) {
	t.Helper()
	t.Setenv(xrootd.EnvConnectionRetry, "0")
}

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

// lsOutput runs fn with a buffer standing in for the command's stdout and
// returns what it printed. The listing is the whole product of xrd-ls, so every
// test below reads it rather than only the error.
func lsOutput(t *testing.T, fn func(w io.Writer) error) (string, error) {
	t.Helper()

	buf := new(bytes.Buffer)
	err := fn(buf)
	return buf.String(), err
}

func TestXrdLs_File(t *testing.T) {
	url := lsServer(t)

	out, err := lsOutput(t, func(w io.Writer) error { return xrdls(w, url+"/a.txt", false, false) })
	if err != nil {
		t.Fatalf("could not list the file: %v", err)
	}
	if got := strings.TrimSpace(out); got != "/a.txt" {
		t.Fatalf("listing a file printed %q, want %q", got, "/a.txt")
	}
}

func TestXrdLs_Directory(t *testing.T) {
	url := lsServer(t)

	out, err := lsOutput(t, func(w io.Writer) error { return xrdls(w, url+"/", false, false) })
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

	out, err := lsOutput(t, func(w io.Writer) error { return xrdls(w, url+"/", false, true) })
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

	out, err := lsOutput(t, func(w io.Writer) error { return xrdls(w, url+"/a.txt", true, false) })
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
	noRedial(t)

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
			_, err := lsOutput(t, func(w io.Writer) error { return xrdls(w, tc.arg, false, false) })
			if err == nil {
				t.Fatal("the listing reported success")
			}
		})
	}
}

// TestXrdLs_Opaque: a URL usually carries a bearer token as opaque data, and
// the recursive walk builds the name of every subdirectory from the name of its
// parent. Those names have to inherit the CGI rather than absorb it: joining
// onto "/sub?authz=tok" asks the server for "/sub?authz=tok/deeper", a
// directory nothing holds.
func TestXrdLs_Opaque(t *testing.T) {
	const token = "?authz=tok&xrd.wantprot=unix"

	url := lsServer(t)

	out, err := lsOutput(t, func(w io.Writer) error { return xrdls(w, url+"/"+token, false, true) })
	if err != nil {
		t.Fatalf("could not list the tree: %v", err)
	}
	for _, want := range []string{"/a.txt", "/sub/b.txt", "/sub/deeper/c.txt"} {
		if !strings.Contains(out, want) {
			t.Errorf("the recursive listing does not mention %q:\n%s", want, out)
		}
	}
}
