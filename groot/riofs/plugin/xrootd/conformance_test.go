// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for what riofs.Create and riofs.Open promise about a root:// URL.
//
// A ROOT file is not written front to back: riofs seeks back over its own
// output to close the directories and to rewrite the header once the size is
// known. So writing one remotely is a claim about random-access writes over
// the wire, and the only honest test of it is to write a file to a server,
// read it back through the same plugin, and then look at what actually landed
// in the server's directory.

package xrootd

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"go-hep.org/x/hep/groot/rbase"
	"go-hep.org/x/hep/groot/riofs"
	_ "go-hep.org/x/hep/groot/ztypes" // class registrations, as a real caller has
	xrd "go-hep.org/x/hep/xrootd"
)

// server starts an XRootD server over a temporary directory and returns that
// directory together with the root:// prefix that reaches it. A path is
// appended to the prefix: the extra slash separates the endpoint from an
// absolute path on it.
func server(t *testing.T) (dir, url string) {
	t.Helper()

	dir = t.TempDir()

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}

	srv := xrd.NewServer(xrd.NewFSHandler(dir), func(err error) {
		t.Logf("xrd-srv: %v", err)
	})
	go func() {
		if err := srv.Serve(listener); err != nil && err != xrd.ErrServerClosed {
			t.Logf("xrd-srv: could not serve: %v", err)
		}
	}()
	t.Cleanup(func() {
		_ = srv.Shutdown(context.Background())
	})

	return dir, fmt.Sprintf("root://%s/", listener.Addr())
}

// TestConformance_ARootFileIsWrittenOverXRootD: the whole stack — riofs.Create,
// the scheme dispatch, the native transport, the seeks back over the header —
// against a server, with the result read back over the same path.
func TestConformance_ARootFileIsWrittenOverXRootD(t *testing.T) {
	dir, url := server(t)
	name := url + "/out.root"

	w, err := riofs.Create(name)
	if err != nil {
		t.Fatalf("could not create: %+v", err)
	}

	if err := w.Put("o1", rbase.NewObjString("v1")); err != nil {
		t.Fatalf("could not put: %+v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("could not close: %+v", err)
	}

	// The file is on the server, and it is a ROOT file: the header is the
	// last thing written, and a truncated one would still be a file.
	raw, err := os.ReadFile(filepath.Join(dir, "out.root"))
	if err != nil {
		t.Fatalf("the server has no such file: %v", err)
	}
	if len(raw) < 4 || string(raw[:4]) != "root" {
		t.Fatalf("what landed on the server is not a ROOT file (%d bytes)", len(raw))
	}

	r, err := riofs.Open(name)
	if err != nil {
		t.Fatalf("could not re-open: %+v", err)
	}
	defer r.Close()

	obj, err := riofs.Get[*rbase.ObjString](r, "o1")
	if err != nil {
		t.Fatalf("could not read back: %+v", err)
	}
	if got, want := obj.String(), "v1"; got != want {
		t.Fatalf("read back %q, want %q", got, want)
	}
}

// TestConformance_ACreateMakesItsParentDirectories: a ROOT file is often the
// first thing written to the directory holding a job's output, and riofs.Open
// has no option to pass. So the plugin asks the server to make the path.
func TestConformance_ACreateMakesItsParentDirectories(t *testing.T) {
	dir, url := server(t)

	w, err := riofs.Create(url + "/store/user/gopher/out.root")
	if err != nil {
		t.Fatalf("could not create: %+v", err)
	}
	if err := w.Put("o1", rbase.NewObjString("v1")); err != nil {
		t.Fatalf("could not put: %+v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("could not close: %+v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "store", "user", "gopher", "out.root")); err != nil {
		t.Fatalf("the parents were not created: %v", err)
	}
}

// TestConformance_ACreateThatCannotReachTheServerFails: the failure mode worth
// pinning is not the error, it is the file that must not appear. A create that
// fell back to the local filesystem would leave a file whose name is the URL.
func TestConformance_ACreateThatCannotReachTheServerFails(t *testing.T) {
	t.Setenv(xrd.EnvConnectionRetry, "0")

	// A port that is listening to nothing: it is bound, then closed, so the
	// dial is refused rather than left hanging.
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	dir := t.TempDir()
	t.Chdir(dir)

	name := fmt.Sprintf("root://%s//out.root", addr)
	if w, err := riofs.Create(name); err == nil {
		w.Close()
		t.Fatal("a create against a dead endpoint succeeded")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("could not list the working directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("a failed remote create left %v behind", entries)
	}
}

// TestConformance_EveryNativeSchemeGoesBothWays: roots:// and xroots:// are
// root:// with the session encrypted, which is not a difference riofs should
// have to know about — and a scheme that can be read but not written is a
// caller finding out at the end of a job.
func TestConformance_EveryNativeSchemeGoesBothWays(t *testing.T) {
	var (
		read  = riofs.Drivers()
		write = riofs.WriteDrivers()
	)
	for _, scheme := range []string{"root", "roots", "xroot", "xroots"} {
		if !slices.Contains(read, scheme) {
			t.Errorf("%q cannot be read: %v", scheme, read)
		}
		if !slices.Contains(write, scheme) {
			t.Errorf("%q cannot be written: %v", scheme, write)
		}
	}
}
