// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdhttp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/xrdfs"
)

// davServer is an in-memory WebDAV server: the HTTP verbs the client uses for
// data, plus PROPFIND, MKCOL and MOVE for the namespace. It deliberately
// answers HEAD on a collection with 404, as real servers do, so the PROPFIND
// fallback in Stat is exercised.
type davServer struct {
	mu    sync.Mutex
	files map[string][]byte
	dirs  map[string]bool
}

func newDAVServer() *davServer {
	return &davServer{
		files: map[string][]byte{},
		dirs:  map[string]bool{"/": true},
	}
}

// The accessors below take the lock even where the test is the only live
// goroutine: the happens-before edge between a test and a handler runs through
// a socket, which the race detector cannot see.

func (s *davServer) put(name string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[name] = data
}

func (s *davServer) get(name string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.files[name]
	return b, ok
}

func (s *davServer) isDir(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dirs[name]
}

func (s *davServer) nfiles() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.files)
}

func (s *davServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p := path.Clean("/" + strings.Trim(r.URL.Path, "/"))
	switch r.Method {
	case http.MethodHead:
		b, ok := s.files[p]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))

	case http.MethodGet:
		b, ok := s.files[p]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		http.ServeContent(w, r, p, time.Time{}, bytes.NewReader(b))

	case http.MethodPut:
		buf := new(bytes.Buffer)
		buf.ReadFrom(r.Body)
		s.files[p] = buf.Bytes()
		w.WriteHeader(http.StatusCreated)

	case http.MethodDelete:
		if s.dirs[p] {
			// A WebDAV DELETE on a collection is recursive.
			for name := range s.files {
				if strings.HasPrefix(name, p+"/") {
					delete(s.files, name)
				}
			}
			for name := range s.dirs {
				if strings.HasPrefix(name, p+"/") {
					delete(s.dirs, name)
				}
			}
			delete(s.dirs, p)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if _, ok := s.files[p]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		delete(s.files, p)
		w.WriteHeader(http.StatusNoContent)

	case "MKCOL":
		if s.dirs[p] {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !s.dirs[path.Dir(p)] {
			w.WriteHeader(http.StatusConflict)
			return
		}
		s.dirs[p] = true
		w.WriteHeader(http.StatusCreated)

	case "MOVE":
		dst, err := url.Parse(r.Header.Get("Destination"))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		to := path.Clean("/" + strings.Trim(dst.Path, "/"))
		b, ok := s.files[p]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		delete(s.files, p)
		s.files[to] = b
		w.WriteHeader(http.StatusCreated)

	case "PROPFIND":
		s.propfind(w, r, p)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *davServer) propfind(w http.ResponseWriter, r *http.Request, p string) {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0"?><D:multistatus xmlns:D="DAV:">`)

	write := func(href string, size int, isdir bool) {
		rtype := ""
		if isdir {
			rtype = "<D:collection/>"
			href += "/"
		}
		fmt.Fprintf(&body,
			`<D:response><D:href>%s</D:href><D:propstat><D:status>HTTP/1.1 200 OK</D:status>`+
				`<D:prop><D:displayname>%s</D:displayname><D:getcontentlength>%d</D:getcontentlength>`+
				`<D:resourcetype>%s</D:resourcetype></D:prop></D:propstat></D:response>`,
			href, strings.TrimRight(path.Base(href), "/"), size, rtype)
	}

	switch {
	case s.dirs[p]:
		write(p, 0, true)
		if r.Header.Get("Depth") == "1" {
			var names []string
			for name := range s.files {
				names = append(names, name)
			}
			for name := range s.dirs {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				if path.Dir(name) != p || name == p {
					continue
				}
				write(name, len(s.files[name]), s.dirs[name])
			}
		}
	case s.files[p] != nil:
		write(p, len(s.files[p]), false)
	default:
		w.WriteHeader(http.StatusNotFound)
		return
	}

	body.WriteString(`</D:multistatus>`)
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusMultiStatus)
	w.Write([]byte(body.String()))
}

func newTestFS(t *testing.T) (*davServer, xrdfs.FileSystem) {
	t.Helper()
	dav := newDAVServer()
	srv := httptest.NewServer(dav)
	t.Cleanup(srv.Close)

	c, err := Dial(srv.URL, Unbounded())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	return dav, c.FS()
}

func TestFSWriteReadRoundTrip(t *testing.T) {
	ctx := context.Background()
	dav, fs := newTestFS(t)

	want := []byte("the quick brown fox jumps over the lazy dog")

	f, err := fs.Open(ctx, "/data.txt", xrdfs.OpenModeOwnerWrite, xrdfs.OpenOptionsNew)
	if err != nil {
		t.Fatalf("open for writing: %v", err)
	}
	if _, err := f.WriteAt(want, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	// Nothing is on the server until the file is closed: HTTP has no
	// random-access write, so the whole body is PUT at once.
	if _, early := dav.get("/data.txt"); early {
		t.Fatal("the file reached the server before it was closed")
	}
	if err := f.CloseVerify(ctx, int64(len(want))); err != nil {
		t.Fatalf("CloseVerify: %v", err)
	}

	f, err = fs.Open(ctx, "/data.txt", xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
	if err != nil {
		t.Fatalf("open for reading: %v", err)
	}
	defer f.Close(ctx)

	if got := f.Info().EntrySize; got != int64(len(want)) {
		t.Fatalf("got size %d, want %d", got, len(want))
	}
	p := make([]byte, 5)
	if _, err := f.ReadAt(p, 4); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if !bytes.Equal(p, want[4:9]) {
		t.Fatalf("got %q, want %q", p, want[4:9])
	}
}

func TestFSUpdateStartsFromTheExistingContent(t *testing.T) {
	ctx := context.Background()
	dav, fs := newTestFS(t)
	dav.put("/f", []byte("aaaaaaaa"))

	f, err := fs.Open(ctx, "/f", xrdfs.OpenModeOwnerWrite, xrdfs.OpenOptionsOpenUpdate)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteAt([]byte("bb"), 2); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := f.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got, _ := dav.get("/f")
	if want := "aabbaaaa"; string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFSNamespace(t *testing.T) {
	ctx := context.Background()
	dav, fs := newTestFS(t)

	if err := fs.MkdirAll(ctx, "/a/b/c", xrdfs.OpenModeOwnerWrite); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// A component that already exists must not turn MkdirAll into an error.
	if err := fs.MkdirAll(ctx, "/a/b/c", xrdfs.OpenModeOwnerWrite); err != nil {
		t.Fatalf("MkdirAll on an existing tree: %v", err)
	}
	for _, dir := range []string{"/a", "/a/b", "/a/b/c"} {
		if !dav.isDir(dir) {
			t.Fatalf("%q was not created", dir)
		}
	}

	dav.put("/a/b/f", []byte("hello"))

	ents, err := fs.Dirlist(ctx, "/a/b")
	if err != nil {
		t.Fatalf("Dirlist: %v", err)
	}
	got := make(map[string]bool, len(ents))
	for _, e := range ents {
		got[e.EntryName] = e.IsDir()
	}
	want := map[string]bool{"c": true, "f": false}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	// Stat of a collection: HEAD says 404, PROPFIND says it is there.
	st, err := fs.Stat(ctx, "/a/b/c")
	if err != nil {
		t.Fatalf("Stat of a collection: %v", err)
	}
	if !st.IsDir() {
		t.Fatalf("got flags %v, want a directory", st.Flags)
	}

	if err := fs.Rename(ctx, "/a/b/f", "/a/g"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, ok := dav.get("/a/g"); !ok {
		t.Fatal("the renamed file is missing")
	}

	// RemoveDir keeps the XRootD contract even though a WebDAV DELETE on a
	// collection is recursive.
	if err := fs.RemoveDir(ctx, "/a/b"); err == nil {
		t.Fatal("a non-empty directory was removed")
	}
	if err := fs.RemoveDir(ctx, "/a/b/c"); err != nil {
		t.Fatalf("RemoveDir: %v", err)
	}
	if dav.isDir("/a/b/c") {
		t.Fatal("the empty directory was not removed")
	}

	if err := fs.RemoveAll(ctx, "/a"); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if dav.isDir("/a/b") || dav.nfiles() != 0 {
		t.Fatal("the tree survived RemoveAll")
	}
}

func TestFSStatxAndMissingFiles(t *testing.T) {
	ctx := context.Background()
	dav, fs := newTestFS(t)
	dav.put("/there", []byte("x"))

	flags, err := fs.Statx(ctx, []string{"/there", "/gone"})
	if err != nil {
		t.Fatalf("Statx: %v", err)
	}
	if flags[0]&xrdfs.StatIsReadable == 0 {
		t.Fatalf("got %v for an existing file", flags[0])
	}
	if flags[1] != xrdfs.StatIsOffline {
		t.Fatalf("got %v for a missing file, want StatIsOffline", flags[1])
	}
	if _, err := fs.Open(ctx, "/gone", xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead); err == nil {
		t.Fatal("opening a missing file succeeded")
	}
}

func TestFSTruncate(t *testing.T) {
	ctx := context.Background()
	dav, fs := newTestFS(t)
	dav.put("/f", []byte("0123456789"))

	if err := fs.Truncate(ctx, "/f", 0); err != nil {
		t.Fatalf("Truncate to zero: %v", err)
	}
	if b, _ := dav.get("/f"); len(b) != 0 {
		t.Fatalf("the file still holds %d bytes", len(b))
	}

	err := fs.Truncate(ctx, "/f", 4)
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("got %v, want ErrNotSupported", err)
	}
}

// TestFSUnsupportedOperations checks the operations with no HTTP equivalent say
// so rather than silently doing nothing.
func TestFSUnsupportedOperations(t *testing.T) {
	ctx := context.Background()
	dav, fs := newTestFS(t)
	dav.put("/f", []byte("x"))

	if err := fs.Chmod(ctx, "/f", xrdfs.OpenModeOwnerWrite); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("Chmod: got %v, want ErrNotSupported", err)
	}
	if _, err := fs.VirtualStat(ctx, "/"); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("VirtualStat: got %v, want ErrNotSupported", err)
	}

	f, err := fs.Open(ctx, "/f", xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close(ctx)

	if _, err := f.StatVirtualFS(ctx); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("StatVirtualFS: got %v, want ErrNotSupported", err)
	}
	if err := f.VerifyWriteAt(ctx, []byte("x"), 0); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("VerifyWriteAt: got %v, want ErrNotSupported", err)
	}
	// A file opened for reading must refuse a write rather than buffer it and
	// drop it at close.
	if _, err := f.WriteAt([]byte("x"), 0); err == nil {
		t.Fatal("a read-only file accepted a write")
	}
}
