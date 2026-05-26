// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"go-hep.org/x/hep/groot"
	"go-hep.org/x/hep/groot/root"
)

func TestROOTCopyIssue1053(t *testing.T) {
	dir, err := os.MkdirTemp("", "groot-root-cp-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	oname := filepath.Join(dir, "out.root")

	cmd := exec.Command(
		"go", "tool",
		"go-hep.org/x/hep/groot/cmd/root-cp", "../../testdata/embedded-tbox.root", oname,
	)
	got, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("could not copy embedded-tbox.root:\n%s\nerror: %v", got, err)
	}

	f, err := groot.Open(oname)
	if err != nil {
		t.Fatalf("could not open copied file: %v", err)
	}
	defer f.Close()

	h, err := f.Get("h1")
	if err != nil {
		t.Fatalf("could not retrieve h1: %v", err)
	}

	if got, want := h.(root.Named).Name(), "h1"; got != want {
		t.Fatalf("invalid h1 object: got=%q, want=%q", got, want)
	}

	var tbox bool
	for _, sinfo := range f.StreamerInfos() {
		tbox = sinfo.Name() == "TBox"
		if tbox {
			break
		}
	}
	if !tbox {
		t.Fatalf("missing streamer for TBox !")
	}
}
