// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for a copy that asks to be checked afterwards.
//
// What verification does with an answer is pinned in conformance_verify_test.go;
// what is pinned here is that the copy reaches it at all, and that a server
// which does not answer checksum queries is not thereby a failed copy. Most
// storage elements this client talks to answer some algorithms and not others,
// and treating silence as a mismatch would make Verify unusable against them —
// which is how a verification flag ends up switched off everywhere.

package xrdcopy_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdcopy"
)

func TestConformance_AVerifiedCopyStillCopies(t *testing.T) {
	dir, url := copyServer(t)
	data := copyContent(t)
	if err := os.WriteFile(filepath.Join(dir, "src.bin"), data, 0644); err != nil {
		t.Fatalf("could not write the source file: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "dst.bin")
	if err := xrdcopy.Copy(context.Background(), dst, url+"/src.bin", xrdcopy.Options{Verify: true}); err != nil {
		t.Fatalf("a verified copy failed: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("could not read the destination: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("the copy is %d bytes, want %d", len(got), len(data))
	}
}

func TestConformance_AVerifiedResumeChecksWhatWasAlreadyThere(t *testing.T) {
	// Resume trusts the bytes already on disk, and verification is the only
	// thing that ever looks at them again. The two have to compose rather than
	// one of them short-circuiting the other.
	dir, url := copyServer(t)
	data := copyContent(t)
	if err := os.WriteFile(filepath.Join(dir, "src.bin"), data, 0644); err != nil {
		t.Fatalf("could not write the source file: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "dst.bin")
	if err := os.WriteFile(dst, data[:1024], 0644); err != nil {
		t.Fatalf("could not write the partial destination: %v", err)
	}

	opts := xrdcopy.Options{Verify: true, Resume: true}
	if err := xrdcopy.Copy(context.Background(), dst, url+"/src.bin", opts); err != nil {
		t.Fatalf("a verified resume failed: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("could not read the destination: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("the resumed copy does not match the source")
	}
}
