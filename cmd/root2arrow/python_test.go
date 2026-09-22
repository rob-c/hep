// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestStdout checks the tool writes to standard output when asked, which is
// what anything piping it into another process needs -- the Python bridge in
// ../../python most of all.
func TestStdout(t *testing.T) {
	bin := build(t)

	for _, out := range []string{"-", ""} {
		t.Run("o="+out, func(t *testing.T) {
			cmd := exec.Command(bin, "-t", "tree", "-stream", "-o", out,
				"../../groot/testdata/simple.root")
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			if err != nil {
				t.Fatalf("could not run: %+v\n%s", err, stderr.String())
			}

			// an Arrow stream starts with its magic.
			if got := stdout.Bytes(); len(got) < 8 || !bytes.Contains(got[:8], []byte("ARROW1")) {
				// a stream, rather than a file, has no magic: it must at
				// least be non-empty and hold the column names.
				if len(got) == 0 {
					t.Fatalf("nothing was written to stdout")
				}
			}
			if !bytes.Contains(stdout.Bytes(), []byte("one")) {
				t.Errorf("the output does not name the columns of the tree")
			}
		})
	}
}

// TestPythonBridge runs the Python module against a tree, when there is a
// Python with pyarrow to run it with.
//
// The module is a few lines over a subprocess, so what is worth testing is
// that the two ends agree: that what go-hep writes is what pyarrow reads.
func TestPythonBridge(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3")
	}

	if err := exec.Command(py, "-c", "import pyarrow").Run(); err != nil {
		t.Skip("no pyarrow")
	}

	bin := build(t)

	script := `
import sys
sys.path.insert(0, "../../python")
import gohep

tbl = gohep.read_tree("../../groot/testdata/simple.root", "tree")
assert tbl.num_rows == 4, tbl.num_rows
assert tbl.column_names == ["one", "two", "three"], tbl.column_names
assert tbl.column("one").to_pylist() == [1, 2, 3, 4]
print("ok")
`

	cmd := exec.Command(py, "-c", script)
	cmd.Env = append(os.Environ(), "GOHEP_ROOT2ARROW="+bin)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python bridge failed: %+v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("python bridge did not report success:\n%s", out)
	}
}

// build compiles the tool and returns the path to it.
func build(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "root2arrow")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("could not build root2arrow: %+v\n%s", err, out)
	}
	return bin
}
