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

func TestGenTPCKey(t *testing.T) {
	k1, err := genTPCKey()
	if err != nil {
		t.Fatalf("genTPCKey: %v", err)
	}
	if len(k1) != 24 {
		t.Fatalf("key length: got=%d want=24", len(k1))
	}
	k2, _ := genTPCKey()
	if k1 == k2 {
		t.Fatal("two keys are identical")
	}
}

func TestHostPort(t *testing.T) {
	for _, tc := range []struct {
		addr string
		host string
		port int
	}{
		{"example.org:1094", "example.org", 1094},
		{"example.org:2094", "example.org", 2094},
		{"example.org", "example.org", 1094},
	} {
		host, port := hostPort(tc.addr)
		if host != tc.host || port != tc.port {
			t.Fatalf("hostPort(%q)=(%q,%d) want (%q,%d)", tc.addr, host, port, tc.host, tc.port)
		}
	}
}
