// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the promises this package makes to someone who is not going
// to read its source: that a name works whether it is on this machine or on a
// server, that nothing has to be opened or closed, that a connection dying is
// not their problem, and that when something does go wrong the error says what
// to do about it.

package xrd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sync/errgroup"

	"go-hep.org/x/hep/xrootd"
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

	srv := xrootd.NewServer(xrootd.NewFSHandler(dir), func(err error) {
		t.Logf("xrd-srv: %v", err)
	})
	go func() {
		if err := srv.Serve(listener); err != nil && err != xrootd.ErrServerClosed {
			t.Logf("xrd-srv: could not serve: %v", err)
		}
	}()
	t.Cleanup(func() {
		// The connections this package cached are on that server: let them go
		// before it does, and leave the cache empty for the next test.
		_ = Close()
		_ = srv.Shutdown(context.Background())
	})

	return dir, fmt.Sprintf("root://%s/", listener.Addr())
}

// TestConformance_TheSameNameWorksHereAndOnAServer is the whole point of the
// package: a program written against a local file runs against grid storage by
// changing the string, and nothing else.
func TestConformance_TheSameNameWorksHereAndOnAServer(t *testing.T) {
	dir, url := server(t)

	for _, name := range []string{
		filepath.Join(t.TempDir(), "run.dat"),
		url + "/run.dat",
	} {
		t.Run(name, func(t *testing.T) {
			const data = "42 events\n"

			if err := WriteFile(name, []byte(data)); err != nil {
				t.Fatalf("could not write: %v", err)
			}

			got, err := ReadFile(name)
			if err != nil {
				t.Fatalf("could not read: %v", err)
			}
			if string(got) != data {
				t.Fatalf("read %q, want %q", got, data)
			}

			fi, err := Stat(name)
			if err != nil {
				t.Fatalf("could not stat: %v", err)
			}
			if fi.Size() != int64(len(data)) {
				t.Fatalf("stat says %d bytes, want %d", fi.Size(), len(data))
			}
			if fi.IsDir() {
				t.Fatal("stat says a file is a directory")
			}

			ok, err := Exists(name)
			if err != nil || !ok {
				t.Fatalf("Exists=%v (err=%v), want true", ok, err)
			}
		})
	}

	// And the remote write really is on the server, not somewhere local that
	// happens to answer.
	if _, err := os.Stat(filepath.Join(dir, "run.dat")); err != nil {
		t.Fatalf("the file is not on the server: %v", err)
	}
}

// TestConformance_WritingMakesTheDirectoriesItNeeds: writing into a directory
// that is not there yet is what every job does with its output, and having to
// create the tree first is a step nobody remembers.
func TestConformance_WritingMakesTheDirectoriesItNeeds(t *testing.T) {
	dir, url := server(t)

	if err := WriteFile(url+"/store/user/gopher/out.dat", []byte("out")); err != nil {
		t.Fatalf("could not write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "store", "user", "gopher", "out.dat")); err != nil {
		t.Fatalf("the parents were not created: %v", err)
	}

	local := filepath.Join(t.TempDir(), "a", "b", "out.dat")
	if err := WriteFile(local, []byte("out")); err != nil {
		t.Fatalf("could not write locally: %v", err)
	}
	if _, err := os.Stat(local); err != nil {
		t.Fatalf("the local parents were not created: %v", err)
	}
}

// TestConformance_ExistsSeparatesAbsentFromUnreachable: "the file is not
// there" and "I could not look" are different answers, and a bool that means
// both is how a job silently processes nothing.
func TestConformance_ExistsSeparatesAbsentFromUnreachable(t *testing.T) {
	_, url := server(t)

	ok, err := Exists(url + "/nothing-here.dat")
	if err != nil {
		t.Fatalf("a missing file is not an error: %v", err)
	}
	if ok {
		t.Fatal("Exists=true for a file that is not there")
	}

	t.Setenv(xrootd.EnvConnectionRetry, "0")
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}
	dead := listener.Addr().String()
	listener.Close()

	if _, err := Exists(fmt.Sprintf("root://%s//run.dat", dead)); err == nil {
		t.Fatal("a server that cannot be reached reported an answer")
	}
}

// TestConformance_ListAndGlobHandBackUsableNames: a listing whose entries have
// to be pasted back together with the server name is a listing that gets
// pasted back together wrongly.
func TestConformance_ListAndGlobHandBackUsableNames(t *testing.T) {
	_, url := server(t)

	for _, name := range []string{"a.root", "b.root", "notes.txt"} {
		if err := WriteFile(url+"/data/"+name, []byte(name)); err != nil {
			t.Fatalf("could not write %s: %v", name, err)
		}
	}

	entries, err := List(url + "/data")
	if err != nil {
		t.Fatalf("could not list: %v", err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if want := []string{"a.root", "b.root", "notes.txt"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the listing is %v, want %v in that order", got, want)
	}

	names, err := Glob(url + "/data/*.root")
	if err != nil {
		t.Fatalf("could not glob: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("the pattern matched %v, want two ROOT files", names)
	}
	for _, name := range names {
		data, err := ReadFile(name)
		if err != nil {
			t.Fatalf("the name the glob gave back does not work: %v", err)
		}
		if !strings.HasSuffix(name, string(data)) {
			t.Fatalf("%q holds %q", name, data)
		}
	}

	// A pattern matching nothing is an empty answer, not a failure.
	names, err = Glob(url + "/data/*.nope")
	if err != nil {
		t.Fatalf("a pattern that matched nothing failed: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("the pattern matched %v, want nothing", names)
	}
}

// TestConformance_FindReachesTheWholeTree: the files a job wants are usually
// one directory per run down, and Glob only goes as deep as it is told.
func TestConformance_FindReachesTheWholeTree(t *testing.T) {
	_, url := server(t)

	for _, name := range []string{"run1/a.root", "run1/sub/b.root", "run2/c.root"} {
		if err := WriteFile(url+"/data/"+name, []byte(name)); err != nil {
			t.Fatalf("could not write %s: %v", name, err)
		}
	}

	found, err := Find(url + "/data")
	if err != nil {
		t.Fatalf("could not find: %v", err)
	}
	if len(found) != 3 {
		t.Fatalf("found %v, want the three files", found)
	}
	for _, name := range found {
		if _, err := ReadFile(name); err != nil {
			t.Fatalf("the name Find gave back does not work: %v", err)
		}
	}

	// Walk sees the directories too, which is the difference between the two.
	var dirs int
	err = Walk(url+"/data", func(path string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() {
			dirs++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("could not walk: %v", err)
	}
	if want := 4; dirs != want { // data, run1, run1/sub, run2
		t.Fatalf("the walk saw %d directories, want %d", dirs, want)
	}
}

// TestConformance_DirectoriesAreMadeAndRemoved covers the four namespace verbs
// against the file the server actually ends up holding.
func TestConformance_DirectoriesAreMadeAndRemoved(t *testing.T) {
	dir, url := server(t)

	if err := Mkdir(url + "/store/user/gopher"); err != nil {
		t.Fatalf("could not create the directory: %v", err)
	}
	// Twice, because a job that is retried should not fail on its own output
	// directory already existing.
	if err := Mkdir(url + "/store/user/gopher"); err != nil {
		t.Fatalf("creating an existing directory failed: %v", err)
	}

	if err := WriteFile(url+"/store/user/gopher/a.dat", []byte("a")); err != nil {
		t.Fatalf("could not write: %v", err)
	}
	if err := Remove(url + "/store/user/gopher/a.dat"); err != nil {
		t.Fatalf("could not remove the file: %v", err)
	}
	if err := Remove(url + "/store/user/gopher"); err != nil {
		t.Fatalf("could not remove the empty directory: %v", err)
	}

	if err := WriteFile(url+"/store/user/gopher/b.dat", []byte("b")); err != nil {
		t.Fatalf("could not write: %v", err)
	}
	if err := RemoveAll(url + "/store"); err != nil {
		t.Fatalf("could not remove the tree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "store")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the tree is still on the server (err=%v)", err)
	}
}

// TestConformance_RenameStaysOnOneServer: a rename that quietly became a copy
// and a delete would be a data-loss bug wearing a familiar name.
func TestConformance_RenameStaysOnOneServer(t *testing.T) {
	_, url := server(t)

	if err := WriteFile(url+"/a.dat", []byte("a")); err != nil {
		t.Fatalf("could not write: %v", err)
	}
	if err := Rename(url+"/a.dat", url+"/b.dat"); err != nil {
		t.Fatalf("could not rename: %v", err)
	}
	if got, err := ReadFile(url + "/b.dat"); err != nil || string(got) != "a" {
		t.Fatalf("the renamed file holds %q (err=%v)", got, err)
	}
	if ok, err := Exists(url + "/a.dat"); err != nil || ok {
		t.Fatalf("the old name is still there (err=%v)", err)
	}

	local := filepath.Join(t.TempDir(), "a.dat")
	if err := Rename(url+"/b.dat", local); err == nil {
		t.Fatal("a rename from a server to this machine was accepted")
	}
	if err := Rename(local, url+"/b.dat"); err == nil {
		t.Fatal("a rename from this machine to a server was accepted")
	}
	if err := Rename(url+"/b.dat", "root://elsewhere.example.org//b.dat"); err == nil {
		t.Fatal("a rename between two servers was accepted")
	}
}

// TestConformance_DownloadAndUploadRoundTrip: the two verbs people look for
// first, and the one thing that must be true of them.
func TestConformance_DownloadAndUploadRoundTrip(t *testing.T) {
	_, url := server(t)

	var (
		dir   = t.TempDir()
		local = filepath.Join(dir, "run.dat")
		data  = strings.Repeat("event\n", 1000)
	)
	if err := os.WriteFile(local, []byte(data), 0644); err != nil {
		t.Fatalf("could not write the local file: %v", err)
	}

	if err := Upload(local, url+"/store/run.dat"); err != nil {
		t.Fatalf("could not upload: %v", err)
	}

	back := filepath.Join(dir, "back.dat")
	if err := Download(url+"/store/run.dat", back); err != nil {
		t.Fatalf("could not download: %v", err)
	}

	got, err := os.ReadFile(back)
	if err != nil {
		t.Fatalf("could not read what was downloaded: %v", err)
	}
	if string(got) != data {
		t.Fatalf("the round trip changed the file: %d bytes back, %d sent", len(got), len(data))
	}
}

// TestConformance_AppendAddsToTheEnd: a log written by a job over hours is the
// case, and an append that started at zero would erase it every time.
func TestConformance_AppendAddsToTheEnd(t *testing.T) {
	_, url := server(t)

	for _, line := range []string{"first\n", "second\n"} {
		f, err := Append(url + "/log.txt")
		if err != nil {
			t.Fatalf("could not open for append: %v", err)
		}
		if _, err := f.Write([]byte(line)); err != nil {
			t.Fatalf("could not write: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("could not close: %v", err)
		}
	}

	got, err := ReadFile(url + "/log.txt")
	if err != nil {
		t.Fatalf("could not read back: %v", err)
	}
	if want := "first\nsecond\n"; string(got) != want {
		t.Fatalf("the log holds %q, want %q", got, want)
	}
}

// TestConformance_OneConnectionServesEveryCall: the package's answer to "do I
// have to open a connection?" is no, and the cost of that answer must not be a
// login per file.
func TestConformance_OneConnectionServesEveryCall(t *testing.T) {
	_, url := server(t)

	for i := range 5 {
		name := fmt.Sprintf("%s/f%d.dat", url, i)
		if err := WriteFile(name, []byte("x")); err != nil {
			t.Fatalf("could not write: %v", err)
		}
		if _, err := ReadFile(name); err != nil {
			t.Fatalf("could not read: %v", err)
		}
	}

	sessions.Lock()
	n := len(sessions.db)
	sessions.Unlock()
	if n != 1 {
		t.Fatalf("%d connections for one server, want 1", n)
	}

	if err := Close(); err != nil {
		t.Fatalf("could not close: %v", err)
	}
	sessions.Lock()
	n = len(sessions.db)
	sessions.Unlock()
	if n != 0 {
		t.Fatalf("%d connections after Close, want none", n)
	}
}

// TestConformance_ADeadConnectionIsReplaced: a cached connection is a
// connection that can die between two calls — an idle timeout at the server, a
// firewall dropping the flow — and the caller who never opened it should not
// have to notice that it closed.
func TestConformance_ADeadConnectionIsReplaced(t *testing.T) {
	_, url := server(t)

	if err := WriteFile(url+"/a.dat", []byte("a")); err != nil {
		t.Fatalf("could not write: %v", err)
	}

	// Kill the cached connection behind the package's back, which is what a
	// server-side idle timeout amounts to.
	sessions.Lock()
	if len(sessions.db) != 1 {
		sessions.Unlock()
		t.Fatal("no connection was cached to kill")
	}
	for _, be := range sessions.db {
		_ = be.Close()
	}
	sessions.Unlock()

	got, err := ReadFile(url + "/a.dat")
	if err != nil {
		t.Fatalf("the dead connection was not replaced: %v", err)
	}
	if string(got) != "a" {
		t.Fatalf("read %q, want %q", got, "a")
	}
}

// TestConformance_TheErrorSaysWhatToDo: the audience for these errors is
// someone who has not read this package, has not read the XRootD protocol, and
// is looking at a terminal wondering what they got wrong.
func TestConformance_TheErrorSaysWhatToDo(t *testing.T) {
	_, url := server(t)

	_, err := ReadFile(url + "/not-here.dat")
	if err == nil {
		t.Fatal("reading a missing file succeeded")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("errors.Is(err, fs.ErrNotExist) is false for %v", err)
	}
	if !strings.Contains(err.Error(), "xrd.List") {
		t.Fatalf("the error does not say what to try next: %v", err)
	}
	var xerr *Error
	if !errors.As(err, &xerr) {
		t.Fatalf("the error is not an *xrd.Error: %v", err)
	}
	if xerr.Name != url+"/not-here.dat" {
		t.Fatalf("the error names %q, want the file that failed", xerr.Name)
	}

	_, err = ReadFile("gopher://storage.example.org//a.dat")
	if err == nil {
		t.Fatal("an unknown scheme was accepted")
	}
	if !strings.Contains(err.Error(), "the schemes are") {
		t.Fatalf("the error does not list the schemes: %v", err)
	}

	t.Setenv(xrootd.EnvConnectionRetry, "0")
	listener, _ := net.Listen("tcp", "localhost:0")
	dead := listener.Addr().String()
	listener.Close()

	_, err = ReadFile(fmt.Sprintf("root://%s//a.dat", dead))
	if err == nil {
		t.Fatal("reading from a dead endpoint succeeded")
	}
	if !strings.Contains(err.Error(), "1094") {
		t.Fatalf("the error does not mention the port to check: %v", err)
	}
}

// TestConformance_TheSchemeIsTheOnlyThingThatChanges: the package claims one
// set of functions over every protocol, and an https endpoint is where that
// claim is easiest to get wrong — a different transport, a different
// filesystem implementation, a different way of spelling the URL.
func TestConformance_TheSchemeIsTheOnlyThingThatChanges(t *testing.T) {
	dir := t.TempDir()
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
	t.Cleanup(func() {
		_ = Close()
		srv.Close()
	})

	const data = "over http\n"
	name := srv.URL + "/run.dat"

	if err := WriteFile(name, []byte(data)); err != nil {
		t.Fatalf("could not write: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "run.dat")); err != nil || string(raw) != data {
		t.Fatalf("the server holds %q (err=%v), want %q", raw, err, data)
	}

	got, err := ReadFile(name)
	if err != nil {
		t.Fatalf("could not read: %v", err)
	}
	if string(got) != data {
		t.Fatalf("read %q, want %q", got, data)
	}

	fi, err := Stat(name)
	if err != nil {
		t.Fatalf("could not stat: %v", err)
	}
	if fi.Size() != int64(len(data)) {
		t.Fatalf("stat says %d bytes, want %d", fi.Size(), len(data))
	}
}

// TestConformance_ManyGoroutinesShareOneConnection: the connection cache is
// the one piece of shared state in the package, and a physicist fanning out
// over files with a goroutine each is exactly how it gets used.
func TestConformance_ManyGoroutinesShareOneConnection(t *testing.T) {
	_, url := server(t)

	var (
		grp  errgroup.Group
		want = 8
	)
	for i := range want {
		grp.Go(func() error {
			name := fmt.Sprintf("%s/data/f%d.dat", url, i)
			data := fmt.Sprintf("file %d", i)
			if err := WriteFile(name, []byte(data)); err != nil {
				return err
			}
			got, err := ReadFile(name)
			if err != nil {
				return err
			}
			if string(got) != data {
				return fmt.Errorf("%s holds %q, want %q", name, got, data)
			}
			return nil
		})
	}
	if err := grp.Wait(); err != nil {
		t.Fatalf("concurrent use failed: %v", err)
	}

	entries, err := List(url + "/data")
	if err != nil {
		t.Fatalf("could not list: %v", err)
	}
	if len(entries) != want {
		t.Fatalf("%d files landed, want %d", len(entries), want)
	}

	sessions.Lock()
	n := len(sessions.db)
	sessions.Unlock()
	if n != 1 {
		t.Fatalf("%d connections for one server, want 1", n)
	}
}
