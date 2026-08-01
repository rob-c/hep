// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for what a server does when a connection, or the handler behind
// it, fails on it — and for the one recursive operation the client builds out
// of several requests.
//
// A server is a long-lived process holding other people's file handles. The
// rule these pin is that nothing is dropped in silence: a session whose files
// could not be released, and a connection that died mid-request, both reach the
// error handler, because that handler is the only thing an operator has.
//
// RemoveAll is the client-side counterpart. It is a walk, not a request, so a
// failure anywhere inside it has to come back out — a "rm -r" that returns nil
// having skipped a subtree it could not read is how data survives a deletion it
// was supposed to be included in, and how a directory refuses to go away for
// reasons nobody can see.

package xrootd_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd"
)

// stubbornHandler is a server handler that cannot let go of a session.
type stubbornHandler struct {
	xrootd.Handler
}

func (stubbornHandler) CloseSession(sessionID [16]byte) error {
	return errors.New("the session is still busy")
}

func TestConformance_ASessionThatCannotBeReleasedIsReported(t *testing.T) {
	// The handler owns the open files of a session, and the server calls it
	// when the connection goes. If that call fails and nobody is told, the
	// server leaks a descriptor per connection and the first sign of it is the
	// process hitting its file limit hours later.
	sink := newErrorSink()
	_, addr := serveHandler(t, stubbornHandler{Handler: xrootd.Default()}, sink.handle)

	conn := dialServer(t, addr)
	clientHandshake(t, conn)
	if err := conn.Close(); err != nil {
		t.Fatalf("could not close the connection: %v", err)
	}

	if err := sink.wait(t); !strings.Contains(err.Error(), "could not close session") {
		t.Fatalf("the server reported %v, want the session it could not close", err)
	}
}

func TestConformance_ARequestThatStopsHalfwayIsReported(t *testing.T) {
	// A client that dies mid-write leaves a header promising bytes that never
	// arrive. That is not the same as a client that hung up cleanly — which is
	// routine and silent — and it must not be treated as one, or a network
	// dropping connections looks exactly like clients finishing their work.
	sink := newErrorSink()
	_, addr := serveTCP(t, sink.handle)

	conn := dialServer(t, addr)
	clientHandshake(t, conn)

	// A well-formed 24-byte request header claiming 4 KiB of body, followed by
	// two bytes of it and a half-close.
	frame := make([]byte, 24)
	frame[3] = 0x01 // a request ID the server would otherwise dispatch
	frame[23] = 0x00
	frame[22] = 0x10 // dlen = 4096
	if _, err := conn.Write(append(frame, 'g', 'o')); err != nil {
		t.Fatalf("could not send the truncated request: %v", err)
	}
	if err := conn.CloseWrite(); err != nil {
		t.Fatalf("could not half-close the connection: %v", err)
	}

	if err := sink.wait(t); !strings.Contains(err.Error(), "could not close connection") {
		t.Fatalf("the server reported %v, want the connection it lost", err)
	}
}

func TestConformance_ARecursiveRemovalStopsAtWhatItCannotRemove(t *testing.T) {
	// One unreadable directory inside the tree. RemoveAll cannot list it, so
	// it cannot empty it, so the removal is incomplete — and saying so is the
	// only way the caller learns that the data is still there.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tree", "locked"), 0o755); err != nil {
		t.Fatalf("could not create the tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tree", "locked", "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("could not populate the tree: %v", err)
	}

	locked := filepath.Join(dir, "tree", "locked")
	fi, err := os.Stat(locked)
	if err != nil {
		t.Fatalf("could not stat %q: %v", locked, err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("could not chmod %q: %v", locked, err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, fi.Mode().Perm()) })
	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("this process can read a mode-000 directory; the test cannot deny itself access")
	}

	_, addr := serveHandler(t, xrootd.NewFSHandler(dir), func(error) {})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := xrootd.NewClient(ctx, addr, "gopher")
	if err != nil {
		t.Fatalf("could not create a client: %v", err)
	}
	defer client.Close()

	if err := client.FS().RemoveAll(ctx, "/tree"); err == nil {
		t.Fatal("a removal that could not descend into the tree reported success")
	}
	// Restore the mode before looking: the test cannot read what it just made
	// unreadable either.
	if err := os.Chmod(locked, fi.Mode().Perm()); err != nil {
		t.Fatalf("could not restore %q: %v", locked, err)
	}
	if _, err := os.Stat(filepath.Join(locked, "a.txt")); err != nil {
		t.Fatalf("the file under the unreadable directory is gone: %v", err)
	}
}

func TestConformance_ARecursiveRemovalTakesTheWholeTree(t *testing.T) {
	// The positive control: nested directories and files all go, in an order
	// the server will accept — a directory is only removed once it is empty.
	dir := t.TempDir()
	for _, name := range []string{"tree/a.txt", "tree/sub/b.txt", "tree/sub/deep/c.txt"} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("could not create %q: %v", path, err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("could not write %q: %v", path, err)
		}
	}

	_, addr := serveHandler(t, xrootd.NewFSHandler(dir), func(error) {})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := xrootd.NewClient(ctx, addr, "gopher")
	if err != nil {
		t.Fatalf("could not create a client: %v", err)
	}
	defer client.Close()

	if err := client.FS().RemoveAll(ctx, "/tree"); err != nil {
		t.Fatalf("could not remove the tree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "tree")); !os.IsNotExist(err) {
		t.Fatalf("the tree is still there: %v", err)
	}
}
