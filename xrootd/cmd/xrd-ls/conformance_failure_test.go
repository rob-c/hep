// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the listings that cannot be produced.
//
// A recursive listing is a walk, and a walk that hits a directory it cannot
// read has two ways out: report it, or print what it has and exit 0. The second
// is how "xrd-ls -R" becomes a silently incomplete inventory — the output looks
// like a listing of the whole tree, nothing on stderr says otherwise, and the
// script consuming it concludes that the missing files are not there.

package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd"
)

// lockedServer serves a tree with one directory the server process cannot read,
// and returns the root URL. The mode is restored at cleanup so the temporary
// directory can be removed.
func lockedServer(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	for _, name := range []string{"a.txt", "locked/b.txt"} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("could not create %q: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(name), 0644); err != nil {
			t.Fatalf("could not write %q: %v", path, err)
		}
	}

	locked := filepath.Join(dir, "locked")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("could not chmod %q: %v", locked, err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("this process can read a mode-000 directory; the test cannot deny itself access")
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
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	return fmt.Sprintf("root://%s/", listener.Addr())
}

func TestConformance_AnOperandThatIsNotAURLIsReportedNotDialled(t *testing.T) {
	// A bracketed host with no closing bracket cannot be parsed, and there is
	// no host in it to fall back to.
	stdout, stderr, code := runCLI(t, "root://[::1//store/file.root")
	if code == 0 {
		t.Fatalf("a malformed URL exited 0 with:\n%s", stdout)
	}
	if !strings.Contains(stderr, "could not parse") {
		t.Fatalf("stderr says %q, want it to name the parse failure", stderr)
	}
}

func TestConformance_ADirectoryThatCannotBeListedIsAnErrorNotAnEmptyListing(t *testing.T) {
	url := lockedServer(t)

	for _, tc := range []struct {
		name    string
		operand string
	}{
		{"named directly", url + "locked"},
		{"reached by recursion", url},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runCLI(t, "-R", tc.operand)
			if code == 0 {
				t.Fatalf("an unreadable directory listed successfully:\n%s", stdout)
			}
			if !strings.Contains(stderr, "could not list") {
				t.Fatalf("stderr says %q, want it to say the listing failed", stderr)
			}
		})
	}
}
