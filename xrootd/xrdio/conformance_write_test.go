// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the write half of xrdio. Reading a remote file and writing
// one are not symmetric jobs: XRootD has no write-only open, no single flag
// meaning "create it if it is not there", and a truncating open is spelled
// "delete". The tests below pin what each os.O_* flag turns into, and check
// the result against the directory the server is serving, rather than against
// what the client believes it did.

package xrdio_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdio"
)

// writeServer starts an XRootD server over a temporary directory and returns
// that directory together with the root:// prefix that reaches it. A path is
// appended to the prefix: the extra slash is the one separating the endpoint
// from an absolute path on it.
func writeServer(t *testing.T) (dir, url string) {
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
		_ = srv.Shutdown(context.Background())
	})

	return dir, fmt.Sprintf("root://%s/", listener.Addr())
}

func TestConformance_CreateWritesAFileTheServerCanSee(t *testing.T) {
	dir, url := writeServer(t)

	f, err := xrdio.Create(url + "/out.txt")
	if err != nil {
		t.Fatalf("could not create: %v", err)
	}

	const data = "written over the wire\n"
	n, err := f.Write([]byte(data))
	if err != nil {
		t.Fatalf("could not write: %v", err)
	}
	if n != len(data) {
		t.Fatalf("wrote %d bytes, want %d", n, len(data))
	}
	if err := f.Sync(); err != nil {
		t.Fatalf("could not sync: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("could not close: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil {
		t.Fatalf("the server has no such file: %v", err)
	}
	if string(got) != data {
		t.Fatalf("the file holds %q, want %q", got, data)
	}
}

func TestConformance_CreateTruncatesWhatWasThere(t *testing.T) {
	dir, url := writeServer(t)
	if err := os.WriteFile(filepath.Join(dir, "out.txt"), []byte("a long previous life"), 0644); err != nil {
		t.Fatalf("could not seed the file: %v", err)
	}

	f, err := xrdio.Create(url + "/out.txt")
	if err != nil {
		t.Fatalf("could not create: %v", err)
	}
	if _, err := f.Write([]byte("short")); err != nil {
		t.Fatalf("could not write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("could not close: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil {
		t.Fatalf("could not read back: %v", err)
	}
	if string(got) != "short" {
		t.Fatalf("the file holds %q, want the previous contents gone", got)
	}
}

func TestConformance_CreateWithoutTruncateKeepsTheContents(t *testing.T) {
	// O_CREATE without O_TRUNC or O_EXCL is the open XRootD has no flag for:
	// kXR_new refuses a file that exists and kXR_open_updt refuses one that
	// does not. Both cases have to end with the file open and, if it was
	// already there, unchanged.
	dir, url := writeServer(t)
	if err := os.WriteFile(filepath.Join(dir, "out.txt"), []byte("0123456789"), 0644); err != nil {
		t.Fatalf("could not seed the file: %v", err)
	}

	f, err := xrdio.OpenFile(url+"/out.txt", os.O_WRONLY|os.O_CREATE)
	if err != nil {
		t.Fatalf("could not open: %v", err)
	}
	if _, err := f.WriteAt([]byte("ab"), 2); err != nil {
		t.Fatalf("could not write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("could not close: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil {
		t.Fatalf("could not read back: %v", err)
	}
	if want := "01ab456789"; string(got) != want {
		t.Fatalf("the file holds %q, want %q", got, want)
	}

	// And the same flags against a name that is not there yet.
	f, err = xrdio.OpenFile(url+"/fresh.txt", os.O_WRONLY|os.O_CREATE)
	if err != nil {
		t.Fatalf("could not create a missing file: %v", err)
	}
	if _, err := f.Write([]byte("new")); err != nil {
		t.Fatalf("could not write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("could not close: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "fresh.txt")); err != nil || string(got) != "new" {
		t.Fatalf("the created file holds %q (err=%v), want %q", got, err, "new")
	}
}

func TestConformance_ExclusiveCreateRefusesAnExistingFile(t *testing.T) {
	dir, url := writeServer(t)
	if err := os.WriteFile(filepath.Join(dir, "out.txt"), []byte("mine"), 0644); err != nil {
		t.Fatalf("could not seed the file: %v", err)
	}

	f, err := xrdio.OpenFile(url+"/out.txt", os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err == nil {
		f.Close()
		t.Fatal("an exclusive create overwrote an existing file")
	}
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("the failure is %v, want it to answer errors.Is(err, fs.ErrExist)", err)
	}

	if got, err := os.ReadFile(filepath.Join(dir, "out.txt")); err != nil || string(got) != "mine" {
		t.Fatalf("the file holds %q (err=%v), want it untouched", got, err)
	}
}

func TestConformance_AppendStartsAtTheEnd(t *testing.T) {
	dir, url := writeServer(t)
	if err := os.WriteFile(filepath.Join(dir, "log.txt"), []byte("first\n"), 0644); err != nil {
		t.Fatalf("could not seed the file: %v", err)
	}

	f, err := xrdio.OpenFile(url+"/log.txt", os.O_WRONLY|os.O_APPEND)
	if err != nil {
		t.Fatalf("could not open for append: %v", err)
	}
	if _, err := f.Write([]byte("second\n")); err != nil {
		t.Fatalf("could not append: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("could not close: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "log.txt"))
	if err != nil {
		t.Fatalf("could not read back: %v", err)
	}
	if want := "first\nsecond\n"; string(got) != want {
		t.Fatalf("the file holds %q, want %q", got, want)
	}
}

func TestConformance_MkPathCreatesTheParents(t *testing.T) {
	dir, url := writeServer(t)

	// Without it, the missing directory is the failure.
	if f, err := xrdio.Create(url + "/store/user/gopher/out.txt"); err == nil {
		f.Close()
		t.Fatal("a create into a directory that does not exist succeeded")
	}

	f, err := xrdio.OpenFile(url+"/store/user/gopher/out.txt", os.O_RDWR|os.O_CREATE|os.O_TRUNC|xrdio.MkPath)
	if err != nil {
		t.Fatalf("could not create with MkPath: %v", err)
	}
	if _, err := f.Write([]byte("deep")); err != nil {
		t.Fatalf("could not write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("could not close: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "store", "user", "gopher", "out.txt"))
	if err != nil {
		t.Fatalf("the parents were not created: %v", err)
	}
	if string(got) != "deep" {
		t.Fatalf("the file holds %q, want %q", got, "deep")
	}
}

func TestConformance_AWriteMovesTheEndOfTheFile(t *testing.T) {
	// A File caches the size it was opened with, and Read reports io.EOF from
	// it. A file written past that point has grown, and a reader that is not
	// told stops early on data that is there.
	_, url := writeServer(t)

	f, err := xrdio.Create(url + "/out.txt")
	if err != nil {
		t.Fatalf("could not create: %v", err)
	}
	defer f.Close()

	if _, err := f.Write([]byte("0123456789")); err != nil {
		t.Fatalf("could not write: %v", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("could not seek: %v", err)
	}

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("could not read back what was just written: %v", err)
	}
	if want := "0123456789"; string(got) != want {
		t.Fatalf("read %q, want %q", got, want)
	}
}

func TestConformance_TruncateShortensTheFile(t *testing.T) {
	dir, url := writeServer(t)

	f, err := xrdio.Create(url + "/out.txt")
	if err != nil {
		t.Fatalf("could not create: %v", err)
	}
	if _, err := f.Write([]byte("0123456789")); err != nil {
		t.Fatalf("could not write: %v", err)
	}
	if err := f.Truncate(4); err != nil {
		t.Fatalf("could not truncate: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("could not close: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil {
		t.Fatalf("could not read back: %v", err)
	}
	if want := "0123"; string(got) != want {
		t.Fatalf("the file holds %q, want %q", got, want)
	}
}

func TestConformance_CreateFromReusesTheFilesystem(t *testing.T) {
	// CreateFrom is handed a filesystem it does not own, exactly as OpenFrom
	// is: closing the file must not close the caller's connection.
	dir, url := writeServer(t)

	cli, err := xrootd.NewClient(context.Background(), strings.TrimSuffix(url, "/"), "gopher")
	if err != nil {
		t.Fatalf("could not connect: %v", err)
	}
	defer cli.Close()

	for _, name := range []string{"/one.txt", "/two.txt"} {
		f, err := xrdio.CreateFrom(cli.FS(), name)
		if err != nil {
			t.Fatalf("could not create %s: %v", name, err)
		}
		if _, err := f.Write([]byte(name)); err != nil {
			t.Fatalf("could not write %s: %v", name, err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("could not close %s: %v", name, err)
		}
	}

	for _, name := range []string{"one.txt", "two.txt"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("could not read back %s: %v", name, err)
		}
		if want := "/" + name; string(got) != want {
			t.Fatalf("%s holds %q, want %q", name, got, want)
		}
	}
}

func TestOpenFileRejectsImpossibleFlags(t *testing.T) {
	// Nothing here reaches a server: a flag combination that cannot be
	// expressed is a caller error, and finding it out after a connection and
	// an open is finding it out twice as slowly.
	for _, tc := range []struct {
		name string
		flag int
		want string
	}{
		{
			name: "write-only and read-write",
			flag: os.O_WRONLY | os.O_RDWR,
			want: "O_WRONLY and O_RDWR together",
		},
		{
			name: "exclusive without create",
			flag: os.O_WRONLY | os.O_EXCL,
			want: "O_EXCL without O_CREATE",
		},
		{
			name: "read-only asking for parents",
			flag: os.O_RDONLY | xrdio.MkPath,
			want: "read-only open asking to create",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := xrdio.OpenFile("root://localhost:1094//file.txt", tc.flag)
			if err == nil {
				t.Fatal("an impossible flag combination was accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the failure says %q, want it to name %q", err, tc.want)
			}
		})
	}
}
