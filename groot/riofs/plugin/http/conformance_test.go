// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for what riofs.Open promises about a remote URL.
//
// The plugin is the one place a groot user's URL meets the network, and
// riofs.Open gives them nowhere to pass an option — so everything here is
// about what they get without asking: ranged reads where the server offers
// them, one whole download where it does not, and the ambient bearer token
// offered to encrypted endpoints only.

package http

import (
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"go-hep.org/x/hep/groot/rbase"
	"go-hep.org/x/hep/groot/riofs"
	_ "go-hep.org/x/hep/groot/ztypes" // class registrations, as a real caller has
	"go-hep.org/x/hep/xrootd/xrdhttp"
)

const testfile = "../../../testdata/simple.root"

// TestConformance_ARootFileIsReadOverHTTP: the whole stack — riofs.Open, the
// scheme dispatch, the hardened transport, ranged reads — against a server
// that answers ranges, which is what any real storage endpoint does.
func TestConformance_ARootFileIsReadOverHTTP(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		stdhttp.ServeFile(w, r, testfile)
	}))
	defer srv.Close()

	f, err := riofs.Open(srv.URL + "/simple.root")
	if err != nil {
		t.Fatalf("could not open: %+v", err)
	}
	defer f.Close()

	keys := f.Keys()
	if len(keys) == 0 {
		t.Fatal("the file reports no keys")
	}
	if _, err := f.Get(keys[0].Name()); err != nil {
		t.Fatalf("could not read %q: %+v", keys[0].Name(), err)
	}
}

// TestConformance_ARangelessServerCostsOneDownload: a server that ignores
// Range still answers every read correctly, but each read would carry the
// whole file prefix in front of it. The plugin must notice and fetch the file
// once — after which no read touches the network at all.
func TestConformance_ARangelessServerCostsOneDownload(t *testing.T) {
	var gets atomic.Int64
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		gets.Add(1)
		// Deliberately blind to the Range header: the whole file, always.
		data, err := os.ReadFile(testfile)
		if err != nil {
			stdhttp.Error(w, err.Error(), stdhttp.StatusInternalServerError)
			return
		}
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	f, err := riofs.Open(srv.URL + "/simple.root")
	if err != nil {
		t.Fatalf("could not open: %+v", err)
	}
	defer f.Close()

	if len(f.Keys()) == 0 {
		t.Fatal("the file reports no keys")
	}

	// One GET probing for range support, one fetching the file. A count
	// beyond that means reads are going to the network after all.
	if got := gets.Load(); got != 2 {
		t.Errorf("the server saw %d GETs, want 2 (probe + download)", got)
	}
}

// TestConformance_TheTokenGoesOnlyToEncryptedEndpoints pins both halves of
// the credential decision: an encrypted scheme is offered the discovered
// token, and a cleartext scheme is offered nothing — a bearer token is a
// credential anyone who observes it can replay.
func TestConformance_TheTokenGoesOnlyToEncryptedEndpoints(t *testing.T) {
	t.Setenv("BEARER_TOKEN", "sekrit-token")
	// Make sure the fallback steps of the discovery sequence cannot leak in
	// from the machine running the tests.
	t.Setenv("BEARER_TOKEN_FILE", "")

	for _, scheme := range []string{"https", "davs"} {
		if got := credentialOptions(scheme); len(got) != 1 {
			t.Errorf("%s: got %d options, want the discovered token", scheme, len(got))
		}
	}
	for _, scheme := range []string{"http", "dav"} {
		if got := credentialOptions(scheme); got != nil {
			t.Errorf("%s: a cleartext endpoint was offered a credential", scheme)
		}
	}
}

// TestConformance_NoTokenIsNotAnError: anonymous access is a working
// configuration; a machine with no token must open public files exactly as
// before.
func TestConformance_NoTokenIsNotAnError(t *testing.T) {
	t.Setenv("BEARER_TOKEN", "")
	t.Setenv("BEARER_TOKEN_FILE", "/dev/null/definitely-not-a-file")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	if got := credentialOptions("https"); got != nil {
		t.Fatalf("no token anywhere, but got %d options", len(got))
	}
}

// TestConformance_TheDiscoveredTokenReachesTheWire drives the options the
// plugin would assemble through a real TLS dial, because "an option was
// returned" and "the header went out" are different claims.
func TestConformance_TheDiscoveredTokenReachesTheWire(t *testing.T) {
	t.Setenv("BEARER_TOKEN", "sekrit-token")

	var got atomic.Value
	srv := httptest.NewTLSServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		got.Store(r.Header.Get("Authorization"))
		stdhttp.ServeFile(w, r, testfile)
	}))
	defer srv.Close()

	opts := append(credentialOptions("https"), xrdhttp.WithInsecureTLS())
	cli, err := xrdhttp.Dial(srv.URL, opts...)
	if err != nil {
		t.Fatalf("could not dial: %+v", err)
	}
	if _, err := cli.Stat(t.Context(), "/simple.root"); err != nil {
		t.Fatalf("could not stat: %+v", err)
	}
	if auth, _ := got.Load().(string); !strings.HasPrefix(auth, "Bearer ") || !strings.Contains(auth, "sekrit-token") {
		t.Fatalf("the token did not reach the server: Authorization=%q", auth)
	}
}

// davDir serves a directory over HTTP with the three methods a WebDAV write
// needs: PUT to store, GET to read back, HEAD so the reader can find out
// whether ranges are on offer.
func davDir(t *testing.T) (dir, url string) {
	t.Helper()

	dir = t.TempDir()
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		name := filepath.Join(dir, filepath.Clean("/"+r.URL.Path))
		switch r.Method {
		case stdhttp.MethodPut:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				stdhttp.Error(w, err.Error(), stdhttp.StatusBadRequest)
				return
			}
			if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
				stdhttp.Error(w, err.Error(), stdhttp.StatusInternalServerError)
				return
			}
			if err := os.WriteFile(name, body, 0644); err != nil {
				stdhttp.Error(w, err.Error(), stdhttp.StatusInternalServerError)
				return
			}
			w.WriteHeader(stdhttp.StatusCreated)
		case stdhttp.MethodGet, stdhttp.MethodHead:
			stdhttp.ServeFile(w, r, name)
		default:
			stdhttp.Error(w, "method not allowed", stdhttp.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)

	return dir, srv.URL
}

// TestConformance_ARootFileIsWrittenOverHTTP: riofs.Create over http buffers
// the file and PUTs it whole, so the claim to test is not that each write
// reached the server — none of them did — but that the file the server ends
// up holding is the ROOT file the caller wrote, readable through the plugin.
func TestConformance_ARootFileIsWrittenOverHTTP(t *testing.T) {
	dir, url := davDir(t)

	w, err := riofs.Create(url + "/out.root")
	if err != nil {
		t.Fatalf("could not create: %+v", err)
	}
	if err := w.Put("o1", rbase.NewObjString("v1")); err != nil {
		t.Fatalf("could not put: %+v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("could not close: %+v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "out.root"))
	if err != nil {
		t.Fatalf("the server has no such file: %v", err)
	}
	if len(raw) < 4 || string(raw[:4]) != "root" {
		t.Fatalf("what landed on the server is not a ROOT file (%d bytes)", len(raw))
	}

	r, err := riofs.Open(url + "/out.root")
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

// TestConformance_EveryHTTPSchemeGoesBothWays: a scheme that can be read but
// not written is a caller finding out at the end of a job.
func TestConformance_EveryHTTPSchemeGoesBothWays(t *testing.T) {
	var (
		read  = riofs.Drivers()
		write = riofs.WriteDrivers()
	)
	for _, scheme := range []string{"http", "https", "dav", "davs"} {
		if !slices.Contains(read, scheme) {
			t.Errorf("%q cannot be read: %v", scheme, read)
		}
		if !slices.Contains(write, scheme) {
			t.Errorf("%q cannot be written: %v", scheme, write)
		}
	}
}
