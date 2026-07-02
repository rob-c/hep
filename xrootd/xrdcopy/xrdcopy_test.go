// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdcopy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyLocal(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "sub", "dst.txt")
	want := []byte("local copy engine payload")
	if err := os.WriteFile(src, want, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Copy(context.Background(), dst, src, Options{}); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("content mismatch: got=%q want=%q", got, want)
	}
}

func TestRemoteURLDetection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		remote bool
	}{
		{"root://h//p", true},
		{"roots://h//p", true},
		{"xroot://h/p", true},
		{"/local/path", false},
		{"relative/path", false},
	} {
		got, _, err := remoteURL(tc.name)
		if err != nil {
			t.Fatalf("remoteURL(%q): %v", tc.name, err)
		}
		if got != tc.remote {
			t.Fatalf("remoteURL(%q)=%v want %v", tc.name, got, tc.remote)
		}
	}
}

func TestRemoteToRemoteUnsupported(t *testing.T) {
	err := Copy(context.Background(), "root://h//dst", "root://h//src", Options{})
	if err == nil {
		t.Fatal("expected error for remote-to-remote copy")
	}
}
