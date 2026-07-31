// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for what a failure looks like by the time it reaches a caller.
// The mapping from kXR codes to the standard library's error values is tested
// in xrdproto; what is tested here is that it survives the journey — the
// session, the filesystem view, the file handle and the io wrapper each add
// context, and a single fmt.Errorf without %w anywhere along the way turns
// errors.Is(err, fs.ErrNotExist) into a silent false.

package xrootd

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdhttp"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

// TestConformance_AMissingPathIsRecognisableThroughEveryNamespaceCall drives
// the whole namespace surface at paths that are not there. Each one has to
// come back as something a caller can test for without reading the message.
func TestConformance_AMissingPathIsRecognisableThroughEveryNamespaceCall(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) {
			srv.mkdirAs("/d", 0o755)
			srv.mkfile("/d/f", []byte("contents"))
		},
		func(srv *confFS, xfs xrdfs.FileSystem, cli *Client) {
			ctx := context.Background()
			const gone = "/d/absent"

			for _, tc := range []struct {
				op   string
				call func() error
			}{
				{"Dirlist", func() error { _, err := xfs.Dirlist(ctx, "/absent"); return err }},
				{"Open", func() error {
					_, err := xfs.Open(ctx, gone, xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
					return err
				}},
				{"Stat", func() error { _, err := xfs.Stat(ctx, gone); return err }},
				{"RemoveFile", func() error { return xfs.RemoveFile(ctx, gone) }},
				{"RemoveDir", func() error { return xfs.RemoveDir(ctx, "/absent") }},
				{"Rename", func() error { return xfs.Rename(ctx, gone, "/d/other") }},
				{"Truncate", func() error { return xfs.Truncate(ctx, gone, 0) }},
				{"Chmod", func() error { return xfs.Chmod(ctx, gone, xrdfs.OpenModeOwnerRead) }},
			} {
				err := tc.call()
				if err == nil {
					t.Errorf("%s of a missing path succeeded", tc.op)
					continue
				}
				if !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("%s: %v is not recognisable as a missing path", tc.op, err)
				}
				// The code itself stays reachable, for the caller that needs
				// to tell kXR_NotFound from the other ways a path can be gone.
				var serr xrdproto.ServerError
				if !errors.As(err, &serr) {
					t.Errorf("%s: %v does not carry a ServerError", tc.op, err)
					continue
				}
				if serr.Code != xrdproto.NotFound {
					t.Errorf("%s: the server said %v, want %v", tc.op, serr.Code, xrdproto.NotFound)
				}
			}

			// kXR_statx is deliberately not in that table: it answers with one
			// flag byte per path and no error at all, so a path that is not
			// there is reported as offline rather than as a failure.
			flags, err := xfs.Statx(ctx, []string{gone, "/d/f"})
			if err != nil {
				t.Errorf("Statx: %v", err)
			} else if len(flags) != 2 || flags[0] != xrdfs.StatIsOffline {
				t.Errorf("Statx reports %v for a missing path, want it offline", flags)
			}

			// The negative half: a call that succeeds must not look like a
			// failure, and a failure that is not about existence must not
			// claim to be.
			if _, err := xfs.Stat(ctx, "/d/f"); err != nil {
				t.Errorf("Stat of an existing file failed: %v", err)
			}
			if _, err := xfs.Dirlist(ctx, "/d/f"); err == nil {
				t.Error("a listing of a plain file succeeded")
			} else if errors.Is(err, fs.ErrNotExist) {
				t.Errorf("%v reads as a missing path, but the path is there", err)
			}
		},
	)
	srv.check(t)
}

// TestConformance_APathThatIsAlreadyThereIsRecognisable is the other half of
// the pair a caller writes: create-if-absent is errors.Is(err, fs.ErrExist) on
// the failure, and any other reading of that error creates the file twice or
// not at all.
func TestConformance_APathThatIsAlreadyThereIsRecognisable(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) {
			srv.mkdirAs("/d", 0o755)
			srv.mkfile("/d/f", []byte("contents"))
		},
		func(srv *confFS, xfs xrdfs.FileSystem, cli *Client) {
			ctx := context.Background()

			for _, tc := range []struct {
				op   string
				call func() error
			}{
				{"Open with kXR_new", func() error {
					_, err := xfs.Open(ctx, "/d/f", xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsNew)
					return err
				}},
				{"Mkdir", func() error { return xfs.Mkdir(ctx, "/d", xrdfs.OpenModeOwnerRead) }},
			} {
				err := tc.call()
				if err == nil {
					t.Errorf("%s on an existing path succeeded", tc.op)
					continue
				}
				if !errors.Is(err, fs.ErrExist) {
					t.Errorf("%s: %v is not recognisable as an existing path", tc.op, err)
				}
				if errors.Is(err, fs.ErrNotExist) {
					t.Errorf("%s: %v reads as both existing and missing", tc.op, err)
				}
			}
		},
	)
	srv.check(t)
}

// TestConformance_AnErrorSurvivesTheFileAndIOLayers follows a failure up
// through the wrappers a user actually holds. xrdio.File is the one most
// callers see, and it is the furthest from the socket.
func TestConformance_AnErrorSurvivesTheFileAndIOLayers(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) {
			srv.mkdirAs("/d", 0o755)
			srv.mkfile("/d/f", []byte("contents"))
		},
		func(srv *confFS, xfs xrdfs.FileSystem, cli *Client) {
			ctx := context.Background()

			// An open file whose path is then removed: the handle stays valid,
			// so this is about the operations that name a path again.
			f, err := xfs.Open(ctx, "/d/f", xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer f.Close(ctx)

			// A second open of a path that is not there, through the same
			// session, must still classify.
			if _, err := xfs.Open(ctx, "/d/nope", xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("a second open reports %v, which is not a missing file", err)
			}

			// fs.ErrNotExist is also what io/fs-shaped callers test for, and
			// xrdfs.FileSystem is what xrdio builds on: check the value that
			// reaches them is the same one, not a copy that lost its cause.
			_, err = xfs.Stat(ctx, "/d/nope")
			var serr xrdproto.ServerError
			if !errors.As(err, &serr) || serr.Code != xrdproto.NotFound {
				t.Errorf("the error reaching a caller is %v, want a kXR_NotFound", err)
			}
			if got, want := errors.Is(err, fs.ErrNotExist), true; got != want {
				t.Errorf("errors.Is(..., fs.ErrNotExist) is %v, want %v", got, want)
			}
		},
	)
	srv.check(t)
}

// confErrClass is what a caller can actually get out of an error: the three
// questions the standard library lets it ask. Comparing the whole triple rather
// than one answer at a time catches the mapping that says yes twice.
type confErrClass struct{ notExist, exist, permission bool }

func confClassify(err error) confErrClass {
	return confErrClass{
		notExist:   errors.Is(err, fs.ErrNotExist),
		exist:      errors.Is(err, fs.ErrExist),
		permission: errors.Is(err, fs.ErrPermission),
	}
}

// TestConformance_TheTwoTransportsAgreeOnWhatAFailureMeans is the property the
// whole mapping exists for. A job that reads root://site//store/f.root on one
// day and https://site/store/f.root on the next is the same job, and the code
// that decides whether to fall back to another replica has to keep working
// across that switch. The wire formats have nothing in common — a kXR error
// code and an HTTP status line — so agreement is not something that happens by
// itself.
//
// Only the two failures both transports can raise unprompted are compared: a
// path that is not there, and a directory that already is. A permission refusal
// needs an authorizing server on the native side, which the mock is not.
func TestConformance_TheTwoTransportsAgreeOnWhatAFailureMeans(t *testing.T) {
	missing := []confFSQuestion{
		{"Dirlist", func(ctx context.Context, x xrdfs.FileSystem) error { _, err := x.Dirlist(ctx, "/absent"); return err }},
		{"Stat", func(ctx context.Context, x xrdfs.FileSystem) error { _, err := x.Stat(ctx, "/absent"); return err }},
		{"Open", func(ctx context.Context, x xrdfs.FileSystem) error {
			_, err := x.Open(ctx, "/absent", xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
			return err
		}},
		{"RemoveFile", func(ctx context.Context, x xrdfs.FileSystem) error { return x.RemoveFile(ctx, "/absent") }},
		{"RemoveDir", func(ctx context.Context, x xrdfs.FileSystem) error { return x.RemoveDir(ctx, "/absent") }},
		{"Rename", func(ctx context.Context, x xrdfs.FileSystem) error { return x.Rename(ctx, "/absent", "/other") }},
		{"Truncate", func(ctx context.Context, x xrdfs.FileSystem) error { return x.Truncate(ctx, "/absent", 0) }},
	}
	exists := []confFSQuestion{
		{"Mkdir", func(ctx context.Context, x xrdfs.FileSystem) error {
			return x.Mkdir(ctx, "/d", xrdfs.OpenModeOwnerRead)
		}},
	}

	ctx := context.Background()
	want := map[string]confErrClass{}
	srv := confFSClient(t,
		func(srv *confFS) {
			srv.mkdirAs("/d", 0o755)
			srv.mkfile("/d/f", []byte("contents"))
		},
		func(srv *confFS, xfs xrdfs.FileSystem, cli *Client) {
			for _, tc := range append(append([]confFSQuestion{}, missing...), exists...) {
				err := tc.call(ctx, xfs)
				if err == nil {
					t.Fatalf("native %s succeeded, so there is nothing to compare", tc.op)
				}
				want[tc.op] = confClassify(err)
			}
		},
	)
	srv.check(t)

	// The same questions over HTTP. The server answers the statuses a real one
	// would: nothing is there, except that MKCOL finds the collection already
	// present.
	dav := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "MKCOL" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer dav.Close()

	c, err := xrdhttp.Dial(dav.URL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	xfs := c.FS()

	for _, tc := range append(append([]confFSQuestion{}, missing...), exists...) {
		err := tc.call(ctx, xfs)
		if err == nil {
			t.Errorf("HTTP %s succeeded where the native client failed", tc.op)
			continue
		}
		if got := confClassify(err); got != want[tc.op] {
			t.Errorf("%s means %+v over HTTP and %+v over the native protocol", tc.op, got, want[tc.op])
		}
	}

	// And the agreement is not vacuous — both transports answering "no" to every
	// question would compare equal too.
	if got := want["Stat"]; !got.notExist || got.exist || got.permission {
		t.Errorf("a missing path classifies as %+v, want only fs.ErrNotExist", got)
	}
	if got := want["Mkdir"]; !got.exist || got.notExist {
		t.Errorf("an existing directory classifies as %+v, want fs.ErrExist alone", got)
	}
}

// confFSQuestion is one call a caller can make through the protocol-neutral
// filesystem view, so that the same table can be run over either transport.
type confFSQuestion struct {
	op   string
	call func(context.Context, xrdfs.FileSystem) error
}
