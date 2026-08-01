// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the WebDAV filesystem view when the server answers, but not
// with what was asked for.
//
// The HTTP file is buffered: writes go into memory and reach the server as a
// single PUT at Close. That is the only way to give a range-less protocol a
// file-like API, and it moves every write failure to the close — which is
// exactly where callers stop checking. So Close has to carry the PUT's failure
// out, and CloseVerify has to go back and ask the server how many bytes it
// really holds, because a PUT that returns 200 having stored a truncated body
// is a failure mode real storage elements have.

package xrdhttp

import (
	"context"
	"errors"
	iofs "io/fs"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdfs"
)

// davFailing builds a filesystem view backed by h.
func davFailing(t *testing.T, h http.Handler) xrdfs.FileSystem {
	t.Helper()

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	c, err := Dial(srv.URL)
	if err != nil {
		t.Fatalf("could not build a client: %v", err)
	}
	return c.FS()
}

// davMultiStatus writes a 207 carrying body.
func davMultiStatus(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusMultiStatus)
	_, _ = w.Write([]byte(`<?xml version="1.0"?><D:multistatus xmlns:D="DAV:">` + body + `</D:multistatus>`))
}

func TestConformance_APropfindThatDescribesNothingIsNotAStat(t *testing.T) {
	// A collection often does not answer HEAD, so a 404 there is not an
	// answer and the client asks WebDAV instead. A 207 with an empty
	// multistatus is well-formed XML that describes no resource: reading a
	// zero-valued EntryStat out of it would report a file of size 0, last
	// modified at the epoch, that does not exist at all.
	fs := davFailing(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PROPFIND":
			davMultiStatus(w, "")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	_, err := fs.Stat(context.Background(), "/data.txt")
	if err == nil {
		t.Fatal("an empty PROPFIND was accepted as a stat")
	}
	if !errors.Is(err, iofs.ErrNotExist) {
		t.Fatalf("the failure is %v, want it to say the path is not there", err)
	}

	// The same emptiness, seen where it is detected.
	_, err = fs.(*davFS).statViaPropfind(context.Background(), "/data.txt")
	if err == nil || !strings.Contains(err.Error(), "no resource") {
		t.Fatalf("the PROPFIND failure says %v, want it to say the server described nothing", err)
	}
}

func TestConformance_ACloseThatCannotStoreTheBytesFails(t *testing.T) {
	// Everything written to an HTTP file is still in the client when Close is
	// called. If the PUT is refused and Close returns nil, the caller has
	// written a file that exists nowhere.
	fs := davFailing(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			w.WriteHeader(http.StatusInsufficientStorage)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))

	ctx := context.Background()
	f, err := fs.Open(ctx, "/data.txt", xrdfs.OpenModeOwnerWrite, xrdfs.OpenOptionsNew)
	if err != nil {
		t.Fatalf("could not open the file: %v", err)
	}
	if _, err := f.WriteAt([]byte("go-hep"), 0); err != nil {
		t.Fatalf("could not write: %v", err)
	}

	if err := f.Close(ctx); err == nil {
		t.Fatal("a close whose PUT was refused reported success")
	}
}

func TestConformance_AVerifiedCloseChecksWhatTheServerKept(t *testing.T) {
	// The PUT succeeds and the server keeps less than it was sent. Only a stat
	// after the close can see that, which is what CloseVerify is for.
	for _, tc := range []struct {
		name string
		size int64 // what the server reports afterwards
		want int64 // what the caller asks CloseVerify to confirm
		fail string
	}{
		{"the server kept it all", 6, 6, ""},
		{"the server kept less", 2, 6, "holds 2 bytes after close"},
		{"a zero expectation asks nothing", 2, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := davFailing(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case "PUT":
					w.WriteHeader(http.StatusCreated)
				case "HEAD":
					w.Header().Set("Content-Length", strconv.FormatInt(tc.size, 10))
					w.WriteHeader(http.StatusOK)
				default:
					w.WriteHeader(http.StatusMethodNotAllowed)
				}
			}))

			ctx := context.Background()
			f, err := fs.Open(ctx, "/data.txt", xrdfs.OpenModeOwnerWrite, xrdfs.OpenOptionsNew)
			if err != nil {
				t.Fatalf("could not open the file: %v", err)
			}
			if _, err := f.WriteAt([]byte("go-hep"), 0); err != nil {
				t.Fatalf("could not write: %v", err)
			}

			err = f.CloseVerify(ctx, tc.want)
			switch {
			case tc.fail == "" && err != nil:
				t.Fatalf("a close that stored everything failed: %v", err)
			case tc.fail == "":
				return
			case err == nil:
				t.Fatal("a truncated file passed verification")
			case !strings.Contains(err.Error(), tc.fail):
				t.Fatalf("the failure says %q, want it to mention %q", err, tc.fail)
			}
		})
	}
}

func TestConformance_AVerifiedCloseFailsWithWhateverFailedFirst(t *testing.T) {
	// CloseVerify is two operations and either can fail. The first failure is
	// the one that matters: a PUT that was refused makes the follow-up stat
	// meaningless, and a stat that cannot be had says nothing about a PUT that
	// succeeded — reporting the wrong one sends the caller looking in the wrong
	// place.
	for _, tc := range []struct {
		name string
		put  int
		head int
	}{
		{"the store was refused", http.StatusInsufficientStorage, http.StatusOK},
		{"the check could not be made", http.StatusCreated, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := davFailing(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case "PUT":
					w.WriteHeader(tc.put)
				case "HEAD":
					w.Header().Set("Content-Length", "6")
					w.WriteHeader(tc.head)
				default:
					w.WriteHeader(http.StatusMethodNotAllowed)
				}
			}))

			ctx := context.Background()
			f, err := fs.Open(ctx, "/data.txt", xrdfs.OpenModeOwnerWrite, xrdfs.OpenOptionsNew)
			if err != nil {
				t.Fatalf("could not open the file: %v", err)
			}
			if _, err := f.WriteAt([]byte("go-hep"), 0); err != nil {
				t.Fatalf("could not write: %v", err)
			}

			if err := f.CloseVerify(ctx, 6); err == nil {
				t.Fatal("a verified close reported success")
			}
		})
	}
}

func TestConformance_AStatOfAFileTheServerLostIsAnError(t *testing.T) {
	// The handle is open; the resource behind it is gone. A stat that answered
	// from what the client remembers would keep reporting a size for a file
	// that has been deleted underneath it.
	var served bool
	fs := davFailing(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "HEAD" && !served:
			served = true
			w.Header().Set("Content-Length", "6")
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	ctx := context.Background()
	f, err := fs.Open(ctx, "/data.txt", xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
	if err != nil {
		t.Fatalf("could not open the file: %v", err)
	}
	defer f.Close(ctx)

	if _, err := f.Stat(ctx); err == nil {
		t.Fatal("a file the server no longer has was statted successfully")
	}
}
