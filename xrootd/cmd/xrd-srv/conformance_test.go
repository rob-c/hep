// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for xrd-srv.
//
// The server itself is tested in the xrootd package; what is pinned here is the
// command around it — that the arguments it refuses are refused before it binds
// a port, that a served directory is actually reachable over root://, and that
// an interrupt shuts the server down rather than dropping connections on the
// floor.

package main

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdio"
)

func TestConformance_BadArgumentsAreRefusedBeforeBinding(t *testing.T) {
	// Each of these has to fail without a listening socket: binding first and
	// validating afterwards leaves a port held open on a run that was never
	// going to serve anything.
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no base dir", nil, "missing base dir operand"},
		{"two base dirs", []string{"/tmp", "/var"}, "missing base dir operand"},
		{"an unknown flag", []string{"-nope", "/tmp"}, "-nope"},
		{"an address that cannot be bound", []string{"-addr=localhost:-1", "/tmp"}, "could not listen"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(&stdout, &stderr, tc.args); code == 0 {
				t.Fatal("the command exited 0")
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("stderr does not mention %q:\n%s", tc.want, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("a refused run announced itself on stdout:\n%s", stdout.String())
			}
		})
	}
}

func TestConformance_TheUsageIsNotAnError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(&stdout, &stderr, []string{"-h"}); code != 0 {
		t.Fatalf("asking for help exited %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("the usage was written to stdout:\n%s", stdout.String())
	}
	for _, want := range []string{"xrd-srv", "-addr"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("the usage does not mention %q:\n%s", want, stderr.String())
		}
	}
}

func TestConformance_AServedDirectoryIsReachableAndShutsDownOnInterrupt(t *testing.T) {
	// The command's whole job: hand a directory to a client over root://, and
	// stop when asked. An interrupt that did not shut the server down would
	// leave the process alive after Ctrl-C.
	dir := t.TempDir()
	const content = "go-hep xrootd"
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(content), 0644); err != nil {
		t.Fatalf("could not populate the served directory: %v", err)
	}

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}

	quit := make(chan os.Signal, 1)
	done := make(chan int, 1)
	var stdout, stderr bytes.Buffer
	go func() { done <- serve(&stdout, &stderr, listener, dir, quit) }()

	url, err := xrdio.Parse("root://" + listener.Addr().String() + "//a.txt")
	if err != nil {
		t.Fatalf("could not parse the server url: %v", err)
	}

	ctx := context.Background()
	cli, err := xrootd.NewClient(ctx, url.Addr, "gopher")
	if err != nil {
		t.Fatalf("could not reach the served directory: %v", err)
	}

	f, err := cli.FS().Open(ctx, url.Path, xrdfs.OpenModeOtherRead, xrdfs.OpenOptionsOpenRead)
	if err != nil {
		t.Fatalf("could not open the served file: %v", err)
	}
	got := make([]byte, len(content))
	if _, err := f.ReadAtContext(ctx, got, 0); err != nil {
		t.Fatalf("could not read the served file: %v", err)
	}
	if string(got) != content {
		t.Fatalf("the served file holds %q, want %q", got, content)
	}
	_ = f.Close(ctx)
	_ = cli.Close()

	quit <- os.Interrupt
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("the interrupted server exited %d: %s", code, stderr.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the server did not shut down on an interrupt")
	}

	if !strings.Contains(stdout.String(), listener.Addr().String()) {
		t.Fatalf("the server did not announce the address it listens on:\n%s", stdout.String())
	}
}

func TestConformance_AListenerThatDiesIsReported(t *testing.T) {
	// A serving loop that ends on its own — the listener closed underneath it
	// — is a failure, and the command has to exit non-zero rather than look
	// like a clean shutdown.
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}
	listener.Close()

	var stdout, stderr bytes.Buffer
	code := serve(&stdout, &stderr, listener, t.TempDir(), make(chan os.Signal))
	if code == 0 {
		t.Fatal("a server that stopped serving exited 0")
	}
	if !strings.Contains(stderr.String(), "could not serve") {
		t.Fatalf("stderr does not say the server stopped:\n%s", stderr.String())
	}
}
