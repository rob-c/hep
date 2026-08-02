// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package riofs

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestLocalFile(t *testing.T) {
	local, err := filepath.Abs("../testdata/simple.root")
	if err != nil {
		t.Fatal(err)
	}
	f, err := openFile("file://" + local)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
}

func TestRegister(t *testing.T) {
	func() {
		defer func() {
			e := recover()
			if e == nil {
				t.Fatalf("expected a panic")
			}
		}()
		Register("file1", nil)
	}()

	func() {
		defer func() {
			e := recover()
			if e == nil {
				t.Fatalf("expected a panic")
			}
		}()
		Register("test-register", openLocalFile)
		Register("test-register", openLocalFile)
	}()
}

func TestDrivers(t *testing.T) {
	list := Drivers()
	const name = "test-drivers"
	defer func() {
		drivers.Lock()
		defer drivers.Unlock()
		delete(drivers.db, name)
	}()

	Register(name, openLocalFile)
	list = append(list, name)
	sort.Strings(list)

	if got, want := Drivers(), list; !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v, want=%v", got, want)
	}
}

func TestRegisterWriter(t *testing.T) {
	func() {
		defer func() {
			e := recover()
			if e == nil {
				t.Fatalf("expected a panic")
			}
		}()
		RegisterWriter("file1", nil)
	}()

	func() {
		defer func() {
			e := recover()
			if e == nil {
				t.Fatalf("expected a panic")
			}
		}()
		RegisterWriter("test-register-writer", createLocalFile)
		RegisterWriter("test-register-writer", createLocalFile)
	}()
}

func TestWriteDrivers(t *testing.T) {
	list := WriteDrivers()
	const name = "test-write-drivers"
	defer func() {
		writers.Lock()
		defer writers.Unlock()
		delete(writers.db, name)
	}()

	RegisterWriter(name, createLocalFile)
	list = append(list, name)
	sort.Strings(list)

	if got, want := WriteDrivers(), list; !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v, want=%v", got, want)
	}
}

func TestCreateFile(t *testing.T) {
	// A local name is created locally, with or without the scheme spelled out.
	for _, name := range []string{
		filepath.Join(t.TempDir(), "out.root"),
		"file://" + filepath.Join(t.TempDir(), "out.root"),
	} {
		f, err := createFile(name)
		if err != nil {
			t.Fatalf("could not create %q: %v", name, err)
		}
		f.Close()

		if _, err := os.Stat(strings.TrimPrefix(name, "file://")); err != nil {
			t.Fatalf("%q was not created: %v", name, err)
		}
	}

	// A scheme with no writer plugin is an error, not a local file with a
	// colon in its name.
	const remote = "no-such-scheme://server.example.com//some/path/out.root"
	if f, err := createFile(remote); err == nil {
		f.Close()
		t.Fatalf("a scheme with no writer plugin was created locally")
	}
}

func TestFileScheme(t *testing.T) {
	for _, tc := range []struct {
		path string
		want string
	}{
		{path: "out.root", want: "file"},
		{path: "/some/path/out.root", want: "file"},
		{path: "file:///some/path/out.root", want: "file"},
		{path: "file+mmap:///some/path/out.root", want: "file+mmap"},
		{path: "root://server.example.com//out.root", want: "root"},
		{path: "roots://server.example.com//out.root", want: "roots"},
		{path: "https://server.example.com/out.root", want: "https"},
		// A single-letter scheme is a Windows drive, and creating "C:" as a
		// remote endpoint would be a strange way to lose a file.
		{path: `C:\some\path\out.root`, want: "file"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			if got := fileScheme(tc.path); got != tc.want {
				t.Fatalf("fileScheme(%q)=%q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestCreateLocalFileOnAWindowsPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive letters only mean anything on windows")
	}
	name := filepath.Join(t.TempDir(), "out.root")
	f, err := createFile(name)
	if err != nil {
		t.Fatalf("could not create %q: %v", name, err)
	}
	f.Close()
}
