// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the command-line surface of xrd-cp.
//
// The tests next to this one drive the transfer. What is pinned here is the
// shell contract: how operands are read (the last one is the destination), what
// "-" means, and the exit status — a copy that fails and exits 0 is a silently
// missing file in whatever ran next.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCLI drives the command the way a shell does and returns its two streams
// and its exit status.
func runCLI(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	var out, errb bytes.Buffer
	code = run(&out, &errb, args)
	return out.String(), errb.String(), code
}

func TestConformance_ACopySucceedsQuietly(t *testing.T) {
	// A successful copy says nothing: the file is the output. Anything on
	// stdout would corrupt a `xrd-cp src - | ...` pipeline used elsewhere.
	dir, url := cpServer(t)
	if err := os.WriteFile(filepath.Join(dir, "src.bin"), []byte("go-hep"), 0644); err != nil {
		t.Fatalf("could not write the remote file: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "dst.bin")
	stdout, stderr, code := runCLI(t, url+"/src.bin", dst)
	if code != 0 {
		t.Fatalf("a successful copy exited %d: %s", code, stderr)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("a successful copy was not quiet:\nstdout=%q\nstderr=%q", stdout, stderr)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("could not read the copy: %v", err)
	}
	if string(got) != "go-hep" {
		t.Fatalf("the copy holds %q", got)
	}
}

func TestConformance_ADashDestinationWritesTheFileToStdout(t *testing.T) {
	// This is the whole reason the command takes a writer: "xrd-cp src -" is
	// how a remote file gets piped somewhere without landing on disk first.
	dir, url := cpServer(t)
	want := bytes.Repeat([]byte("go-hep xrootd "), 100)
	if err := os.WriteFile(filepath.Join(dir, "src.bin"), want, 0644); err != nil {
		t.Fatalf("could not write the remote file: %v", err)
	}

	stdout, stderr, code := runCLI(t, url+"/src.bin", "-")
	if code != 0 {
		t.Fatalf("the copy exited %d: %s", code, stderr)
	}
	if stdout != string(want) {
		t.Fatalf("stdout holds %d bytes, want %d", len(stdout), len(want))
	}
}

func TestConformance_TheLastOperandIsTheDestination(t *testing.T) {
	// Several sources copy into one directory, and it is the last operand —
	// reading it as the first would overwrite a source with a source.
	dir, url := cpServer(t)
	for _, name := range []string{"a.bin", "b.bin"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0644); err != nil {
			t.Fatalf("could not write %q: %v", name, err)
		}
	}

	dst := t.TempDir()
	_, stderr, code := runCLI(t, url+"/a.bin", url+"/b.bin", dst)
	if code != 0 {
		t.Fatalf("the copy exited %d: %s", code, stderr)
	}
	for _, name := range []string{"a.bin", "b.bin"} {
		got, err := os.ReadFile(filepath.Join(dst, name))
		if err != nil {
			t.Fatalf("could not read %q: %v", name, err)
		}
		if string(got) != name {
			t.Fatalf("%q holds %q", name, got)
		}
	}
}

func TestConformance_ADotDestinationCopiesIntoTheWorkingDirectory(t *testing.T) {
	// "." is the idiom the usage text advertises, and it has to mean the
	// current directory rather than a file literally called ".".
	dir, url := cpServer(t)
	if err := os.WriteFile(filepath.Join(dir, "src.bin"), []byte("go-hep"), 0644); err != nil {
		t.Fatalf("could not write the remote file: %v", err)
	}

	t.Chdir(t.TempDir())

	_, stderr, code := runCLI(t, url+"/src.bin", ".")
	if code != 0 {
		t.Fatalf("the copy exited %d: %s", code, stderr)
	}
	got, err := os.ReadFile("src.bin")
	if err != nil {
		t.Fatalf("could not read the copy: %v", err)
	}
	if string(got) != "go-hep" {
		t.Fatalf("the copy holds %q", got)
	}
}

func TestConformance_VerboseReportsTheTransferOnStderr(t *testing.T) {
	// The byte count is a diagnostic, so it goes to stderr — on stdout it
	// would be indistinguishable from the file in a "-" copy.
	dir, url := cpServer(t)
	if err := os.WriteFile(filepath.Join(dir, "src.bin"), []byte("go-hep"), 0644); err != nil {
		t.Fatalf("could not write the remote file: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "dst.bin")
	stdout, stderr, code := runCLI(t, "-v", url+"/src.bin", dst)
	if code != 0 {
		t.Fatalf("the copy exited %d: %s", code, stderr)
	}
	if !strings.Contains(stderr, "transferred 6 bytes") {
		t.Fatalf("stderr does not report the transfer:\n%s", stderr)
	}
	if stdout != "" {
		t.Fatalf("the diagnostic reached stdout:\n%s", stdout)
	}
}

func TestConformance_RecursiveIsOptIn(t *testing.T) {
	// Copying a directory without -r is refused rather than silently copying
	// nothing, which is what makes a mistyped path visible.
	dir, url := cpServer(t)
	if err := os.MkdirAll(filepath.Join(dir, "tree", "sub"), 0755); err != nil {
		t.Fatalf("could not create the remote tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tree", "sub", "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatalf("could not populate the remote tree: %v", err)
	}

	t.Run("refused without -r", func(t *testing.T) {
		_, stderr, code := runCLI(t, url+"/tree", t.TempDir())
		if code == 0 {
			t.Fatal("a directory was copied without -r")
		}
		if !strings.Contains(stderr, "-r not specified") {
			t.Fatalf("stderr does not say which flag is missing:\n%s", stderr)
		}
	})

	t.Run("copied with -r", func(t *testing.T) {
		dst := t.TempDir()
		_, stderr, code := runCLI(t, "-r", url+"/tree", dst)
		if code != 0 {
			t.Fatalf("the copy exited %d: %s", code, stderr)
		}
		got, err := os.ReadFile(filepath.Join(dst, "tree", "sub", "a.txt"))
		if err != nil {
			t.Fatalf("could not read the copied tree: %v", err)
		}
		if string(got) != "a" {
			t.Fatalf("the copied file holds %q", got)
		}
	})
}

func TestConformance_AFailureExitsNonZeroAndSaysSoOnStderr(t *testing.T) {
	dir, url := cpServer(t)
	if err := os.WriteFile(filepath.Join(dir, "src.bin"), []byte("go-hep"), 0644); err != nil {
		t.Fatalf("could not write the remote file: %v", err)
	}

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no operands", nil, "missing file operand"},
		{"only a source", []string{url + "/src.bin"}, "missing destination file operand"},
		{"an unknown flag", []string{"-nope", url + "/src.bin", "."}, "-nope"},
		{"a source that is not there", []string{url + "/missing.bin", filepath.Join(t.TempDir(), "d.bin")}, "could not copy"},
		{"a server that is not listening", []string{"root://localhost:1//f.bin", filepath.Join(t.TempDir(), "d.bin")}, "could not copy"},
		{"a destination that cannot be created", []string{url + "/src.bin", filepath.Join(t.TempDir(), "absent", "d.bin")}, "could not create output file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runCLI(t, tc.args...)
			if code == 0 {
				t.Fatalf("the failure exited 0:\n%s", stdout)
			}
			if !strings.Contains(stderr, tc.want) {
				t.Fatalf("stderr does not mention %q:\n%s", tc.want, stderr)
			}
		})
	}
}

func TestConformance_TheFirstFailingSourceStopsTheRun(t *testing.T) {
	dir, url := cpServer(t)
	if err := os.WriteFile(filepath.Join(dir, "b.bin"), []byte("b"), 0644); err != nil {
		t.Fatalf("could not write the remote file: %v", err)
	}

	dst := t.TempDir()
	_, _, code := runCLI(t, url+"/missing.bin", url+"/b.bin", dst)
	if code == 0 {
		t.Fatal("the run exited 0 despite a failing source")
	}
	if _, err := os.Stat(filepath.Join(dst, "b.bin")); !os.IsNotExist(err) {
		t.Fatal("the run carried on past the failing source")
	}
}

func TestConformance_TheUsageIsNotAnError(t *testing.T) {
	stdout, stderr, code := runCLI(t, "-h")
	if code != 0 {
		t.Fatalf("asking for help exited %d", code)
	}
	if stdout != "" {
		t.Fatalf("the usage was written to stdout:\n%s", stdout)
	}
	for _, want := range []string{"xrd-cp", "-r", "-v"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the usage does not mention %q:\n%s", want, stderr)
		}
	}
}
