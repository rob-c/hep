// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the files that cannot be opened, and the ones that stop
// existing while they are open.
//
// xrdio.File is the io.Reader/Seeker face of remote storage: everything that
// reads a ROOT file over the network goes through it, and it caches the size it
// learned at open time. That cache is what makes a failing stat dangerous — a
// Seek from the end that fell back on a remembered size would seek into a file
// the server no longer has, and the read that follows would return whatever the
// server says about a name that is gone. Each of these has to fail at the call
// that first cannot be answered.

package xrdio_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdhttp"
	"go-hep.org/x/hep/xrootd/xrdio"
)

// vanishingFS serves a file that answers the first n HEAD requests and is gone
// afterwards, so a test can choose which operation is the first to fail.
func vanishingFS(t *testing.T, heads int32) *xrdhttp.Client {
	t.Helper()

	var seen atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead && seen.Add(1) <= heads {
			w.Header().Set("Content-Length", "6")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c, err := xrdhttp.Dial(srv.URL)
	if err != nil {
		t.Fatalf("could not build a client: %v", err)
	}
	return c
}

func TestConformance_ANameThatIsNotAURLIsNeverDialled(t *testing.T) {
	// A bracketed host with no closing bracket. There is no address in it to
	// fall back to, so this has to fail before any connection is attempted.
	_, err := xrdio.Open("root://[::1//store/file.root")
	if err == nil {
		t.Fatal("a malformed URL was opened")
	}
	if !strings.Contains(err.Error(), "could not parse") {
		t.Fatalf("the failure says %q, want it to name the parse failure", err)
	}
}

func TestConformance_AFileWhoseSizeCannotBeLearnedIsNotOpen(t *testing.T) {
	// The open succeeds and the stat that follows does not. Returning the file
	// anyway would hand back a *File whose cached size is zero, which reads as
	// an empty file rather than as an error.
	fs := vanishingFS(t, 1).FS()

	f, err := xrdio.OpenFrom(fs, "/data.txt")
	if err == nil {
		f.Close()
		t.Fatal("a file whose size could not be read was opened")
	}
	if !strings.Contains(err.Error(), "could not stat") {
		t.Fatalf("the failure says %q, want it to name the stat", err)
	}
}

func TestConformance_ASeekFromTheEndAsksTheServerWhereTheEndIs(t *testing.T) {
	// Two HEADs get the file open; the third is the one Seek makes. A Seek that
	// answered from the size cached at open time would silently position into a
	// file that is no longer there.
	fs := vanishingFS(t, 2).FS()

	f, err := xrdio.OpenFrom(fs, "/data.txt")
	if err != nil {
		t.Fatalf("could not open the file: %v", err)
	}
	defer f.Close()

	if _, err := f.Seek(-2, io.SeekEnd); err == nil {
		t.Fatal("a seek from the end of a file the server lost succeeded")
	}
}
