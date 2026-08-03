// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package riofs

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"

	"codeberg.org/go-mmap/mmap"
)

var drivers = struct {
	sync.RWMutex
	db map[string]func(path string) (Reader, error)
}{
	db: make(map[string]func(path string) (Reader, error)),
}

// Register registers a plugin to open ROOT files.
// Register panics if it is called twice with the same name of if the plugin
// function is nil.
func Register(name string, f func(path string) (Reader, error)) {
	drivers.Lock()
	defer drivers.Unlock()
	if f == nil {
		panic("riofs: plugin function is nil")
	}
	if _, dup := drivers.db[name]; dup {
		panic(fmt.Errorf("riofs: Register called twice for plugin %q", name))
	}
	drivers.db[name] = f
}

// Drivers returns a sorted list of the names of the registered plugins
// to open ROOT files.
func Drivers() []string {
	drivers.RLock()
	defer drivers.RUnlock()
	names := make([]string, 0, len(drivers.db))
	for name := range drivers.db {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

var writers = struct {
	sync.RWMutex
	db map[string]func(path string) (Writer, error)
}{
	db: make(map[string]func(path string) (Writer, error)),
}

// RegisterWriter registers a plugin to create ROOT files, for the URL scheme
// name. RegisterWriter panics if it is called twice with the same name or if
// the plugin function is nil.
func RegisterWriter(name string, f func(path string) (Writer, error)) {
	writers.Lock()
	defer writers.Unlock()
	if f == nil {
		panic("riofs: plugin function is nil")
	}
	if _, dup := writers.db[name]; dup {
		panic(fmt.Errorf("riofs: RegisterWriter called twice for plugin %q", name))
	}
	writers.db[name] = f
}

// WriteDrivers returns a sorted list of the names of the registered plugins
// to create ROOT files.
func WriteDrivers() []string {
	writers.RLock()
	defer writers.RUnlock()
	names := make([]string, 0, len(writers.db))
	for name := range writers.db {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func createFile(path string) (Writer, error) {
	writers.RLock()
	defer writers.RUnlock()

	scheme := fileScheme(path)
	if create, ok := writers.db[scheme]; ok {
		return create(path)
	}
	if scheme != "file" {
		return nil, fmt.Errorf("riofs: no ROOT plugin to create [%s] (scheme=%s)", path, scheme)
	}

	return createLocalFile(path)
}

func createLocalFile(path string) (Writer, error) {
	path = strings.TrimPrefix(path, "file://")
	return os.Create(path)
}

// fileScheme is the URL scheme of path, or "file" for a local name: a name
// with no scheme at all is a path on this machine, and a one-letter scheme is
// not a scheme but a Windows drive.
//
// A create decides the scheme this way because it cannot try the local
// filesystem first and fall back — it would make a file literally named
// "root://server//path". A read decides it this way so that the reason a local
// file could not be opened is the one the filesystem gave, rather than a
// complaint about plugins.
func fileScheme(path string) string {
	u, err := url.Parse(path)
	if err != nil || len(u.Scheme) < 2 {
		return "file"
	}
	return u.Scheme
}

func openFile(path string) (Reader, error) {
	drivers.RLock()
	defer drivers.RUnlock()

	if f, err := openLocalFile(path); err == nil {
		return f, nil
	}

	scheme := fileScheme(path)
	if open, ok := drivers.db[scheme]; ok {
		return open(path)
	}

	return nil, fmt.Errorf("riofs: no ROOT plugin to open [%s] (scheme=%s)", path, scheme)
}

func openLocalFile(path string) (Reader, error) {
	path = strings.TrimPrefix(path, "file://")
	return os.Open(path)
}

func mmapLocalFile(path string) (Reader, error) {
	path = strings.TrimPrefix(path, "file+mmap://")
	return mmap.Open(path)
}

func init() {
	Register("file", openLocalFile)
	Register("file+mmap", mmapLocalFile)
}
