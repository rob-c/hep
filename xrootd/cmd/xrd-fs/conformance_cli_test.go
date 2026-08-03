// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for xrd-fs, which exists so that the everyday jobs on remote
// storage need no program at all.
//
// What is pinned here is the shell contract: that each command does what its
// name says, that a name may be a URL or a local path, that a pattern is
// expanded, and — the part a script depends on — that failure is exit status 1
// on stderr and success is silence on it. A command that prints an error and
// exits 0 makes every `set -e` pipeline above it silently wrong.

package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrd"
)

// fsServer starts an XRootD server over a temporary directory and returns that
// directory together with the root:// prefix reaching it. A path is appended
// to the prefix: the extra slash separates the endpoint from an absolute path
// on it.
func fsServer(t *testing.T) (dir, url string) {
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
		// The command caches its connections: let them go before the server does.
		_ = xrd.Close()
		_ = srv.Shutdown(context.Background())
	})

	return dir, fmt.Sprintf("root://%s/", listener.Addr())
}

// runCLI drives the command the way a shell does and returns its two streams
// and its exit status.
func runCLI(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	var out, errb bytes.Buffer
	code = run(&out, &errb, args)
	return out.String(), errb.String(), code
}

// write puts a file on the server through the command's own package, which is
// the shortest way to arrange a fixture the command can then be pointed at.
func write(t *testing.T, name, data string) {
	t.Helper()

	if err := xrd.WriteFile(name, []byte(data)); err != nil {
		t.Fatalf("could not write %q: %v", name, err)
	}
}

func TestConformance_EveryCommandDoesWhatItsNameSays(t *testing.T) {
	dir, url := fsServer(t)

	write(t, url+"ds/a.root", "aaaa")
	write(t, url+"ds/sub/b.root", "bbbbbb")
	write(t, url+"notes.txt", "hello\n")

	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "check says a usable path is usable",
			args: []string{"check", url + "ds"},
			want: []string{"ok"},
		},
		{
			name: "stat says what it is and how big",
			args: []string{"stat", url + "ds/a.root"},
			want: []string{"file", "4 B", "a.root"},
		},
		{
			name: "stat knows a directory from a file",
			args: []string{"stat", url + "ds"},
			want: []string{"dir"},
		},
		{
			name: "du adds up the tree",
			args: []string{"du", url + "ds"},
			want: []string{"10 B"},
		},
		{
			name: "cat writes the contents out",
			args: []string{"cat", url + "notes.txt"},
			want: []string{"hello"},
		},
		{
			name: "find reaches the whole tree",
			args: []string{"find", url + "ds"},
			want: []string{"ds/a.root", "ds/sub/b.root"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runCLI(t, tc.args...)
			if code != 0 {
				t.Fatalf("exited %d: %s", code, stderr)
			}
			if stderr != "" {
				t.Fatalf("a command that worked wrote to stderr:\n%s", stderr)
			}
			for _, want := range tc.want {
				if !strings.Contains(stdout, want) {
					t.Fatalf("output does not have %q in it:\n%s", want, stdout)
				}
			}
		})
	}

	// The commands that change something are checked by what they leave behind
	// rather than by what they print.
	if _, _, code := runCLI(t, "mkdir", url+"new/deeper"); code != 0 {
		t.Fatalf("mkdir exited %d", code)
	}
	if fi, err := os.Stat(filepath.Join(dir, "new", "deeper")); err != nil || !fi.IsDir() {
		t.Fatalf("mkdir did not create the tree: %v", err)
	}

	if _, _, code := runCLI(t, "mv", url+"notes.txt", url+"new/notes.txt"); code != 0 {
		t.Fatalf("mv exited %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "new", "notes.txt")); err != nil {
		t.Fatalf("mv did not move the file: %v", err)
	}

	if _, _, code := runCLI(t, "rm", url+"new/notes.txt"); code != 0 {
		t.Fatalf("rm exited %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "new", "notes.txt")); !os.IsNotExist(err) {
		t.Fatalf("rm did not remove the file: %v", err)
	}

	// A directory with something in it needs -r, and says so rather than
	// pretending to have done it.
	if _, _, code := runCLI(t, "rm", url+"ds"); code == 0 {
		t.Fatal("rm removed a directory that was not empty, without -r")
	}
	if _, _, code := runCLI(t, "rm", "-r", url+"ds"); code != 0 {
		t.Fatalf("rm -r exited %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "ds")); !os.IsNotExist(err) {
		t.Fatalf("rm -r did not remove the tree: %v", err)
	}
}

// TestConformance_ALocalPathWorksToo: the same command on a laptop and on the
// grid is the same promise the library makes, and the command has to keep it.
func TestConformance_ALocalPathWorksToo(t *testing.T) {
	dir := t.TempDir()

	write(t, filepath.Join(dir, "ds", "a.root"), "aaaa")

	stdout, stderr, code := runCLI(t, "du", filepath.Join(dir, "ds"))
	if code != 0 {
		t.Fatalf("exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "4 B") {
		t.Fatalf("du of a local directory said:\n%s", stdout)
	}

	if _, _, code := runCLI(t, "check", filepath.Join(dir, "ds")); code != 0 {
		t.Fatal("check refused a local directory that is there")
	}
	if _, _, code := runCLI(t, "check", filepath.Join(dir, "nope")); code == 0 {
		t.Fatal("check passed a local path that is not there")
	}
}

// TestConformance_APatternIsExpanded: "delete the temporary files" is the
// commonest thing anyone types, and it is a pattern.
func TestConformance_APatternIsExpanded(t *testing.T) {
	dir, url := fsServer(t)

	for _, name := range []string{"a.tmp", "b.tmp", "keep.root"} {
		write(t, url+"work/"+name, name)
	}

	if _, stderr, code := runCLI(t, "rm", url+"work/*.tmp"); code != 0 {
		t.Fatalf("rm of a pattern exited %d: %s", code, stderr)
	}
	for _, name := range []string{"a.tmp", "b.tmp"} {
		if _, err := os.Stat(filepath.Join(dir, "work", name)); !os.IsNotExist(err) {
			t.Fatalf("%s survived the pattern: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "work", "keep.root")); err != nil {
		t.Fatalf("the pattern took a file it did not match: %v", err)
	}

	// A pattern that matches nothing is a failure here, not a silent success:
	// a script that deletes nothing should know it deleted nothing.
	stdout, stderr, code := runCLI(t, "rm", url+"work/*.tmp")
	if code == 0 {
		t.Fatalf("a pattern matching nothing succeeded: %s", stdout)
	}
	if !strings.Contains(stderr, "nothing matches") {
		t.Fatalf("the error does not say what happened: %s", stderr)
	}
}

// TestConformance_TheExitStatusAndTheStreamsAreUsable is the contract every
// script above this command depends on.
func TestConformance_TheExitStatusAndTheStreamsAreUsable(t *testing.T) {
	_, url := fsServer(t)

	for _, tc := range []struct {
		name string
		args []string
		says string
	}{
		{
			name: "no command at all",
			args: nil,
			says: "missing command",
		},
		{
			name: "a command it does not have",
			args: []string{"frobnicate", url},
			says: "not one of its commands",
		},
		{
			name: "a command with nothing to work on",
			args: []string{"stat"},
			says: "needs something to work on",
		},
		{
			name: "mv with one name",
			args: []string{"mv", url + "a"},
			says: "the old name and the new one",
		},
		{
			name: "a file that is not there",
			args: []string{"cat", url + "nope.txt"},
			says: "list the directory above",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runCLI(t, tc.args...)
			if code != 1 {
				t.Fatalf("exited %d, want 1", code)
			}
			if stdout != "" {
				t.Fatalf("a failure wrote to stdout:\n%s", stdout)
			}
			if !strings.Contains(stderr, tc.says) {
				t.Fatalf("stderr does not say %q:\n%s", tc.says, stderr)
			}
		})
	}

	// Asking for help is not a failure.
	stdout, _, code := runCLI(t, "help")
	if code != 0 {
		t.Fatalf("help exited %d, want 0", code)
	}
	if !strings.Contains(stdout, "xrd-fs") {
		t.Fatalf("help said nothing useful:\n%s", stdout)
	}
}

// TestConformance_SizesAreReadableUnlessAskedOtherwise: a person reads GiB, a
// script reads bytes, and both are one flag apart.
func TestConformance_SizesAreReadableUnlessAskedOtherwise(t *testing.T) {
	_, url := fsServer(t)

	write(t, url+"big.dat", strings.Repeat("x", 4096))

	stdout, _, code := runCLI(t, "du", url+"big.dat")
	if code != 0 {
		t.Fatalf("du exited %d", code)
	}
	if !strings.Contains(stdout, "4.0 KiB") {
		t.Fatalf("du did not round the size:\n%s", stdout)
	}

	stdout, _, code = runCLI(t, "du", "-b", url+"big.dat")
	if code != 0 {
		t.Fatalf("du -b exited %d", code)
	}
	if !strings.Contains(stdout, "4096 B") {
		t.Fatalf("du -b did not give the exact size:\n%s", stdout)
	}
}
