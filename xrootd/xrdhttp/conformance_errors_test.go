// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for what a refusal means. A status line is the HTTP equivalent of
// a kXR error code, and a caller should not have to know which transport it is
// on to ask "was that file missing?". The whole point of the mapping is that
// errors.Is(err, fs.ErrNotExist) is true for a 404 here and for kXR_NotFound
// over root://; what is tested here is that it is true for every verb that can
// produce one, and — as importantly — false for the statuses that mean
// something else.

package xrdhttp

import (
	"context"
	"errors"
	"fmt"
	"io"
	iofs "io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdfs"
)

// confErrTargets is every error value this package promises to answer for. A
// status is checked against all of them, not just the one it is meant to be:
// a mapping is only useful if the answers it gives to the other questions are
// no.
var confErrTargets = []struct {
	name string
	err  error
}{
	{"fs.ErrNotExist", iofs.ErrNotExist},
	{"fs.ErrExist", iofs.ErrExist},
	{"fs.ErrPermission", iofs.ErrPermission},
	{"ErrNotSupported", ErrNotSupported},
}

// newRefusingClient returns a client talking to a server that answers every
// request with a status: the one named for the verb, or dflt.
func newRefusingClient(t *testing.T, dflt int, byVerb map[string]int) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		status := dflt
		if s, ok := byVerb[r.Method]; ok {
			status = s
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)

	c, err := Dial(srv.URL, Unbounded())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	return c
}

// TestConformance_AStatusMeansWhatTheStandardLibraryMeans pins the mapping
// itself. The verb is part of it: 405 on MKCOL is RFC 4918's way of saying the
// collection is already there, and on any other verb it is the server saying it
// does not implement that method at all. Reading one as the other either
// swallows a real failure or reports a directory that exists as unsupported.
func TestConformance_AStatusMeansWhatTheStandardLibraryMeans(t *testing.T) {
	for _, tc := range []struct {
		op   string
		code int
		want []error
		why  string
	}{
		{op: "GET", code: http.StatusNotFound, want: []error{iofs.ErrNotExist}},
		{op: "HEAD", code: http.StatusNotFound, want: []error{iofs.ErrNotExist}},
		{op: "PROPFIND", code: http.StatusNotFound, want: []error{iofs.ErrNotExist}},
		{op: "DELETE", code: http.StatusNotFound, want: []error{iofs.ErrNotExist}},
		{op: "MOVE", code: http.StatusNotFound, want: []error{iofs.ErrNotExist}},
		{op: "GET", code: http.StatusGone, want: []error{iofs.ErrNotExist},
			why: "410 is 404 with the server admitting it knows the file used to be there"},

		{op: "MKCOL", code: http.StatusMethodNotAllowed, want: []error{iofs.ErrExist},
			why: "RFC 4918 §9.3.1: the collection is already there"},
		{op: "MKCOL", code: http.StatusConflict, want: []error{iofs.ErrNotExist},
			why: "RFC 4918 §9.3.1: a parent collection is missing"},
		{op: "PUT", code: http.StatusConflict, want: nil,
			why: "409 on any other verb is not defined to be about a path at all"},

		{op: "PROPFIND", code: http.StatusMethodNotAllowed, want: []error{ErrNotSupported},
			why: "a plain HTTP server that does not speak WebDAV"},
		{op: "MOVE", code: http.StatusMethodNotAllowed, want: []error{ErrNotSupported}},
		{op: "PROPFIND", code: http.StatusNotImplemented, want: []error{ErrNotSupported}},
		{op: "MKCOL", code: http.StatusNotImplemented, want: []error{ErrNotSupported}},

		{op: "GET", code: http.StatusUnauthorized, want: []error{iofs.ErrPermission}},
		{op: "GET", code: http.StatusForbidden, want: []error{iofs.ErrPermission}},
		{op: "PUT", code: http.StatusProxyAuthRequired, want: []error{iofs.ErrPermission}},

		// The statuses that mean none of the four. Each of these has an
		// obvious-looking mapping that would be wrong: a full disk is not a
		// permission problem, an overloaded server is not a missing file, and a
		// gateway that failed is not a statement about the file at all.
		{op: "GET", code: http.StatusInternalServerError, want: nil},
		{op: "GET", code: http.StatusBadGateway, want: nil},
		{op: "GET", code: http.StatusServiceUnavailable, want: nil},
		{op: "GET", code: http.StatusTooManyRequests, want: nil},
		{op: "PUT", code: http.StatusInsufficientStorage, want: nil},
		{op: "PUT", code: http.StatusRequestEntityTooLarge, want: nil},
		{op: "PUT", code: http.StatusPreconditionFailed, want: nil},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.op, tc.code), func(t *testing.T) {
			err := error(&StatusError{
				Op:     tc.op,
				Name:   "/store/data/f.root",
				Code:   tc.code,
				Status: fmt.Sprintf("%d %s", tc.code, http.StatusText(tc.code)),
			})

			set := make(map[error]bool, len(tc.want))
			for _, w := range tc.want {
				set[w] = true
			}
			for _, target := range confErrTargets {
				if got := errors.Is(err, target.err); got != set[target.err] {
					t.Errorf("errors.Is(%s %d, %s) is %v, want %v: %s",
						tc.op, tc.code, target.name, got, set[target.err], tc.why)
				}
			}

			// However it is classified, the error has to say what happened: the
			// verb, the path and the status are all a site administrator has to
			// go on when the mapping says nothing.
			msg := err.Error()
			for _, want := range []string{tc.op, "/store/data/f.root", fmt.Sprint(tc.code)} {
				if !strings.Contains(msg, want) {
					t.Errorf("the error reads %q, want it to name %q", msg, want)
				}
			}
		})
	}
}

// TestConformance_AMissingPathIsRecognisableOnEveryVerb drives the real client
// against a server that has nothing. The mapping above is only worth having if
// every call site actually produces a StatusError rather than a fmt.Errorf that
// reads the same and answers no.
func TestConformance_AMissingPathIsRecognisableOnEveryVerb(t *testing.T) {
	ctx := context.Background()
	c := newRefusingClient(t, http.StatusNotFound, nil)
	fs := c.FS()

	for _, tc := range []struct {
		op   string
		call func() error
	}{
		{"ReadAll", func() error { _, err := c.ReadAll(ctx, "/f.root"); return err }},
		{"ReadAt", func() error { _, err := c.ReadAt(ctx, make([]byte, 8), "/f.root", 0); return err }},
		{"Create", func() error { return c.Create(ctx, "/f.root", strings.NewReader("x"), 1) }},
		{"Dirlist", func() error { _, err := c.Dirlist(ctx, "/d"); return err }},
		{"fs.Dirlist", func() error { _, err := fs.Dirlist(ctx, "/d"); return err }},
		{"fs.Stat", func() error { _, err := fs.Stat(ctx, "/f.root"); return err }},
		{"fs.Open", func() error {
			_, err := fs.Open(ctx, "/f.root", xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
			return err
		}},
		{"fs.RemoveFile", func() error { return fs.RemoveFile(ctx, "/f.root") }},
		{"fs.RemoveDir", func() error { return fs.RemoveDir(ctx, "/d") }},
		{"fs.Rename", func() error { return fs.Rename(ctx, "/f.root", "/g.root") }},
		{"fs.Truncate", func() error { return fs.Truncate(ctx, "/f.root", 0) }},
		{"fs.Mkdir", func() error { return fs.Mkdir(ctx, "/d", xrdfs.OpenModeOwnerRead) }},
	} {
		err := tc.call()
		if err == nil {
			t.Errorf("%s against a server with nothing on it succeeded", tc.op)
			continue
		}
		if !errors.Is(err, iofs.ErrNotExist) {
			t.Errorf("%s: %v is not recognisable as a missing path", tc.op, err)
		}
		if errors.Is(err, iofs.ErrPermission) {
			t.Errorf("%s: %v reads as a permission refusal", tc.op, err)
		}
	}

	// The three deliberate exceptions, each of which answers a question rather
	// than failing.
	//
	// A HEAD is how the client asks whether a path exists, so 404 is its answer.
	if fi, err := c.Stat(ctx, "/f.root"); err != nil || fi.Exists {
		t.Errorf("Stat reported (%+v, %v), want a non-existent file and no error", fi, err)
	}
	// Client.Remove is idempotent: the caller wanted the path gone.
	if err := c.Remove(ctx, "/f.root"); err != nil {
		t.Errorf("Remove of a missing path failed: %v", err)
	}
	// So is RemoveAll, for the same reason os.RemoveAll is.
	if err := fs.RemoveAll(ctx, "/d"); err != nil {
		t.Errorf("RemoveAll of a missing tree failed: %v", err)
	}
	// Statx reports absence as a flag, exactly as kXR_statx does, so that the
	// two transports agree on the shape of the answer and not only on the error.
	flags, err := fs.Statx(ctx, []string{"/f.root"})
	if err != nil {
		t.Errorf("Statx: %v", err)
	} else if len(flags) != 1 || flags[0] != xrdfs.StatIsOffline {
		t.Errorf("Statx reports %v for a missing path, want it offline", flags)
	}
}

// TestConformance_ARefusalIsNotAMissingPath is the failure that costs the most
// when it is misread: a client whose token has expired sees 403 everywhere, and
// a caller that reads that as "not found" concludes the data is gone and moves
// on, or worse, re-uploads it.
func TestConformance_ARefusalIsNotAMissingPath(t *testing.T) {
	ctx := context.Background()

	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c := newRefusingClient(t, status, nil)
			fs := c.FS()

			for _, tc := range []struct {
				op   string
				call func() error
			}{
				{"Stat", func() error { _, err := c.Stat(ctx, "/f.root"); return err }},
				{"ReadAll", func() error { _, err := c.ReadAll(ctx, "/f.root"); return err }},
				{"ReadAt", func() error { _, err := c.ReadAt(ctx, make([]byte, 8), "/f.root", 0); return err }},
				{"Create", func() error { return c.Create(ctx, "/f.root", strings.NewReader("x"), 1) }},
				{"Remove", func() error { return c.Remove(ctx, "/f.root") }},
				{"Dirlist", func() error { _, err := c.Dirlist(ctx, "/d"); return err }},
				{"fs.Stat", func() error { _, err := fs.Stat(ctx, "/f.root"); return err }},
				{"fs.RemoveFile", func() error { return fs.RemoveFile(ctx, "/f.root") }},
				{"fs.RemoveAll", func() error { return fs.RemoveAll(ctx, "/d") }},
				{"fs.Mkdir", func() error { return fs.Mkdir(ctx, "/d", xrdfs.OpenModeOwnerRead) }},
				{"fs.MkdirAll", func() error { return fs.MkdirAll(ctx, "/d/e", xrdfs.OpenModeOwnerRead) }},
				{"fs.Rename", func() error { return fs.Rename(ctx, "/f.root", "/g.root") }},
			} {
				err := tc.call()
				if err == nil {
					t.Errorf("%s: a %d was not reported at all", tc.op, status)
					continue
				}
				if !errors.Is(err, iofs.ErrPermission) {
					t.Errorf("%s: %v is not recognisable as a refusal", tc.op, err)
				}
				if errors.Is(err, iofs.ErrNotExist) {
					t.Errorf("%s: %v reads as a missing path, and the data may still be there", tc.op, err)
				}
			}
		})
	}
}

// TestConformance_MkcolTellsAnExistingCollectionFromAMissingParent covers the
// one place the two answers share a shape. Both are ordinary outcomes of
// creating a directory, both arrive as a 4xx, and MkdirAll has to continue on
// one and stop on the other.
func TestConformance_MkcolTellsAnExistingCollectionFromAMissingParent(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		desc      string
		status    int
		mkdir     []error // what Mkdir's error must be
		mkdirAll  bool    // whether MkdirAll must succeed anyway
		mkdirAllE []error // and if not, what its error must be
	}{
		{
			desc:     "the collection is already there",
			status:   http.StatusMethodNotAllowed,
			mkdir:    []error{iofs.ErrExist},
			mkdirAll: true,
		},
		{
			desc:      "a parent collection is missing",
			status:    http.StatusConflict,
			mkdir:     []error{iofs.ErrNotExist},
			mkdirAllE: []error{iofs.ErrNotExist},
		},
		{
			desc:      "the caller may not create it",
			status:    http.StatusForbidden,
			mkdir:     []error{iofs.ErrPermission},
			mkdirAllE: []error{iofs.ErrPermission},
		},
		{
			desc:      "the server does not implement MKCOL",
			status:    http.StatusNotImplemented,
			mkdir:     []error{ErrNotSupported},
			mkdirAllE: []error{ErrNotSupported},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			fs := newRefusingClient(t, tc.status, nil).FS()

			err := fs.Mkdir(ctx, "/a", xrdfs.OpenModeOwnerRead)
			if err == nil {
				t.Fatalf("Mkdir answered by %d succeeded", tc.status)
			}
			for _, want := range tc.mkdir {
				if !errors.Is(err, want) {
					t.Errorf("Mkdir: %v is not %v", err, want)
				}
			}

			err = fs.MkdirAll(ctx, "/a/b/c", xrdfs.OpenModeOwnerRead)
			switch {
			case tc.mkdirAll:
				if err != nil {
					// This is the whole reason MkdirAll needs the distinction:
					// every component but the last usually exists already.
					t.Errorf("MkdirAll stopped at a collection that is already there: %v", err)
				}
			case err == nil:
				t.Errorf("MkdirAll answered by %d succeeded", tc.status)
			default:
				for _, want := range tc.mkdirAllE {
					if !errors.Is(err, want) {
						t.Errorf("MkdirAll: %v is not %v", err, want)
					}
				}
			}
		})
	}

	// And the success case, so that the tolerance above is not simply MkdirAll
	// ignoring everything: a server that creates the collections is not asked
	// to do anything twice.
	var made []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		made = append(made, r.URL.Path)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	c, err := Dial(srv.URL, Unbounded())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if err := c.FS().MkdirAll(ctx, "/a/b/c", xrdfs.OpenModeOwnerRead); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if want := []string{"/a", "/a/b", "/a/b/c"}; !equalStrings(made, want) {
		t.Errorf("MkdirAll created %v, want %v", made, want)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestConformance_AServerThatDoesNotSpeakWebDAVSaysSo covers the endpoint half
// the HEP world is full of: a plain HTTP origin, or an XRootD server with the
// HTTP plugin but no WebDAV verbs. Reading and writing work; listing does not.
// A caller has to be able to tell that from "the directory is not there", which
// is what it would otherwise do next.
func TestConformance_AServerThatDoesNotSpeakWebDAVSaysSo(t *testing.T) {
	ctx := context.Background()

	for _, status := range []int{http.StatusMethodNotAllowed, http.StatusNotImplemented} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c := newRefusingClient(t, http.StatusOK, map[string]int{
				"PROPFIND": status,
				"MKCOL":    http.StatusNotImplemented,
				"MOVE":     status,
			})
			fs := c.FS()

			for _, tc := range []struct {
				op   string
				call func() error
			}{
				{"Dirlist", func() error { _, err := c.Dirlist(ctx, "/d"); return err }},
				{"fs.Dirlist", func() error { _, err := fs.Dirlist(ctx, "/d"); return err }},
				{"fs.RemoveDir", func() error { return fs.RemoveDir(ctx, "/d") }},
				{"fs.Rename", func() error { return fs.Rename(ctx, "/f", "/g") }},
			} {
				err := tc.call()
				if err == nil {
					t.Errorf("%s: a %d verb was not reported at all", tc.op, status)
					continue
				}
				if !errors.Is(err, ErrNotSupported) {
					t.Errorf("%s: %v does not read as an unsupported verb", tc.op, err)
				}
				if errors.Is(err, iofs.ErrNotExist) {
					t.Errorf("%s: %v reads as a missing path rather than a missing feature", tc.op, err)
				}
			}

			// The reads and writes on the same server still work, which is the
			// point: an endpoint without WebDAV is usable, not broken.
			if _, err := c.ReadAll(ctx, "/f.root"); err != nil {
				t.Errorf("ReadAll on a server without WebDAV: %v", err)
			}
			if err := c.Create(ctx, "/f.root", strings.NewReader("x"), 1); err != nil {
				t.Errorf("Create on a server without WebDAV: %v", err)
			}
		})
	}
}

// TestConformance_TheStatusItselfStaysReachable is what a caller falls back on
// when the four standard values are not specific enough — a 507 that should
// pick a different endpoint, a 429 that should be retried later. None of that
// is possible if the status was flattened into a string on the way out.
func TestConformance_TheStatusItselfStaysReachable(t *testing.T) {
	ctx := context.Background()
	c := newRefusingClient(t, http.StatusInsufficientStorage, nil)

	err := c.Create(ctx, "/store/data/big.root", strings.NewReader("x"), 1)
	if err == nil {
		t.Fatal("an upload to a full server succeeded")
	}

	var serr *StatusError
	if !errors.As(err, &serr) {
		t.Fatalf("%v does not carry a StatusError", err)
	}
	if serr.Code != http.StatusInsufficientStorage {
		t.Errorf("the recovered status is %d, want %d", serr.Code, http.StatusInsufficientStorage)
	}
	if serr.Op != http.MethodPut {
		t.Errorf("the recovered verb is %q, want PUT", serr.Op)
	}
	if serr.Name != "/store/data/big.root" {
		t.Errorf("the recovered path is %q, want the one that was asked for", serr.Name)
	}

	// It survives the wrapping the layers above add, which is the only reason
	// the mapping works at all once a call goes through the filesystem view.
	for _, tc := range []struct {
		desc string
		err  error
	}{
		{"wrapped once", fmt.Errorf("xrdhttp: MOVE to %q: %w", "/g", err)},
		{"wrapped twice", fmt.Errorf("copy: %w", fmt.Errorf("upload: %w", err))},
		{"joined", errors.Join(errors.New("and something else"), err)},
	} {
		var got *StatusError
		if !errors.As(tc.err, &got) || got.Code != http.StatusInsufficientStorage {
			t.Errorf("%s: %v lost its status", tc.desc, tc.err)
		}
	}
}
