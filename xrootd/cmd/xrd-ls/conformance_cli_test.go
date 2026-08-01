// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the command-line surface of xrd-ls.
//
// The tests next to this one drive the listing itself. What is pinned here is
// the shell contract around it: which arguments are accepted, what goes to
// stdout versus stderr, and — the part a script actually depends on — the exit
// status. A command that prints an error and exits 0 makes every `set -e`
// pipeline above it silently wrong.

package main

import (
	"bytes"
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

func TestConformance_ListingSucceedsAndPrintsToStdout(t *testing.T) {
	url := lsServer(t)

	stdout, stderr, code := runCLI(t, url+"/")
	if code != 0 {
		t.Fatalf("a successful listing exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "/a.txt") {
		t.Fatalf("the listing did not reach stdout:\n%s", stdout)
	}
	if stderr != "" {
		t.Fatalf("a successful listing wrote to stderr:\n%s", stderr)
	}
}

func TestConformance_TheFlagsReachTheListing(t *testing.T) {
	url := lsServer(t)

	for _, tc := range []struct {
		name string
		args []string
		want []string
		deny []string
	}{
		{
			name: "no flags stops at the top level",
			args: []string{url + "/"},
			want: []string{"/a.txt", "/sub"},
			deny: []string{"b.txt"},
		},
		{
			name: "-R descends",
			args: []string{"-R", url + "/"},
			want: []string{"/a.txt", "/sub/b.txt", "/sub/deeper/c.txt"},
		},
		{
			name: "-l carries the size",
			args: []string{"-l", url + "/a.txt"},
			want: []string{"/a.txt", "5"},
		},
		{
			name: "-l -R combine",
			args: []string{"-l", "-R", url + "/"},
			// The long format names each directory as a heading and its
			// entries by base name underneath, so a recursive long listing is
			// a sequence of headings rather than a list of full paths.
			want: []string{"total ", "/sub:", "b.txt", "/sub/deeper:", "c.txt"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runCLI(t, tc.args...)
			if code != 0 {
				t.Fatalf("the listing exited %d: %s", code, stderr)
			}
			for _, want := range tc.want {
				if !strings.Contains(stdout, want) {
					t.Errorf("the listing does not mention %q:\n%s", want, stdout)
				}
			}
			for _, deny := range tc.deny {
				if strings.Contains(stdout, deny) {
					t.Errorf("the listing mentions %q, which it should not have reached:\n%s", deny, stdout)
				}
			}
		})
	}
}

func TestConformance_SeveralOperandsAreListedInOrderAndSeparated(t *testing.T) {
	// Listing two paths in one invocation has to keep them apart and in the
	// order given; a caller parsing the output has nothing else to go on.
	url := lsServer(t)

	stdout, stderr, code := runCLI(t, url+"/sub", url+"/a.txt")
	if code != 0 {
		t.Fatalf("the listing exited %d: %s", code, stderr)
	}
	sub, file := strings.Index(stdout, "/sub"), strings.Index(stdout, "/a.txt")
	if sub < 0 || file < 0 {
		t.Fatalf("both operands were not listed:\n%s", stdout)
	}
	if sub > file {
		t.Fatalf("the operands came out in the wrong order:\n%s", stdout)
	}
	if !strings.Contains(stdout, "\n\n") {
		t.Fatalf("the two listings were not separated by a blank line:\n%s", stdout)
	}
}

func TestConformance_AFailureExitsNonZeroAndSaysSoOnStderr(t *testing.T) {
	url := lsServer(t)

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no operand at all", nil, "missing directory operand"},
		{"an unknown flag", []string{"-nope", url + "/"}, "-nope"},
		{"a path that is not there", []string{url + "/missing.txt"}, "could not list"},
		{"a server that is not listening", []string{"root://localhost:1//a.txt"}, "could not list"},
		{"an operand that is not a url", []string{"root://%zz//a.txt"}, "could not list"},
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

func TestConformance_TheUsageIsNotAnError(t *testing.T) {
	// -h is what the user asked for, so it exits 0 — and the text goes to
	// stderr next to the rest of the diagnostics, not into a pipe that is
	// expecting a listing.
	stdout, stderr, code := runCLI(t, "-h")
	if code != 0 {
		t.Fatalf("asking for help exited %d", code)
	}
	if stdout != "" {
		t.Fatalf("the usage was written to stdout:\n%s", stdout)
	}
	for _, want := range []string{"xrd-ls", "-R", "-l"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the usage does not mention %q:\n%s", want, stderr)
		}
	}
}

func TestConformance_TheFirstFailingOperandStopsTheRun(t *testing.T) {
	// A listing that kept going after a failure would exit non-zero with a
	// partial listing on stdout, which is the worst of both.
	url := lsServer(t)

	stdout, _, code := runCLI(t, url+"/missing.txt", url+"/a.txt")
	if code == 0 {
		t.Fatal("the run exited 0 despite a failing operand")
	}
	if strings.Contains(stdout, "/a.txt") {
		t.Fatalf("the run carried on past the failing operand:\n%s", stdout)
	}
}
