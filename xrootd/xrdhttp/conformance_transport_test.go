// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for what happens when there is no server to answer.
//
// Every other conformance file here puts a server behind the client and asks
// what it makes of the answer. This one removes the server. A transport failure
// is the one error that arrives with no status line to map, and it is the one
// most easily dropped: a Stat that returns a zero FileInfo and a nil error on a
// dead endpoint reads exactly like a file that is not there, and a Create that
// returns nil reads like an upload that worked.

package xrdhttp

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdfs"
)

// deadClient returns a client pointed at an endpoint that was listening and is
// not any more — the shape of a server that went away mid-session.
func deadClient(t *testing.T) *Client {
	t.Helper()

	srv := httptest.NewServer(nil)
	url := srv.URL
	srv.Close()

	c, err := Dial(url)
	if err != nil {
		t.Fatalf("could not build a client: %v", err)
	}
	return c
}

func TestConformance_AnUnreachableEndpointFailsEveryOperation(t *testing.T) {
	// Each of these has to report the failure, and name the file it was
	// working on: "connection refused" alone, from a copy walking a tree, does
	// not say which file was lost.
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		fn   func(c *Client) error
	}{
		{"stat", func(c *Client) error {
			_, err := c.Stat(ctx, "/a.bin")
			return err
		}},
		{"read", func(c *Client) error {
			_, err := c.ReadAll(ctx, "/a.bin")
			return err
		}},
		{"read at an offset", func(c *Client) error {
			_, err := c.ReadAt(ctx, make([]byte, 8), "/a.bin", 16)
			return err
		}},
		{"create", func(c *Client) error {
			return c.Create(ctx, "/a.bin", strings.NewReader("go-hep"), 6)
		}},
		{"create of unknown length", func(c *Client) error {
			return c.Create(ctx, "/a.bin", strings.NewReader("go-hep"), -1)
		}},
		{"remove", func(c *Client) error {
			return c.Remove(ctx, "/a.bin")
		}},
		{"dirlist", func(c *Client) error {
			_, err := c.Dirlist(ctx, "/dir")
			return err
		}},
		{"mkcol", func(c *Client) error {
			return c.mkcol(ctx, "/dir")
		}},
		{"move", func(c *Client) error {
			return c.move(ctx, "/a.bin", "/b.bin")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn(deadClient(t))
			if err == nil {
				t.Fatal("an unreachable endpoint reported success")
			}
			if !strings.Contains(err.Error(), "xrdhttp:") {
				t.Fatalf("the failure is not attributed to this package: %v", err)
			}
			if !strings.Contains(err.Error(), "a.bin") && !strings.Contains(err.Error(), "dir") {
				t.Fatalf("the failure does not name what it was working on: %v", err)
			}
		})
	}
}

func TestConformance_AnUnreachableEndpointFailsEveryFilesystemOperation(t *testing.T) {
	// The xrdfs.FileSystem surface is what a copy actually drives, and it is a
	// layer above the one tested just now: an operation that reports the error
	// on the Client and swallows it here is still a lost file.
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		fn   func(fs xrdfs.FileSystem) error
	}{
		{"dirlist", func(fs xrdfs.FileSystem) error {
			_, err := fs.Dirlist(ctx, "/dir")
			return err
		}},
		{"stat", func(fs xrdfs.FileSystem) error {
			_, err := fs.Stat(ctx, "/a.bin")
			return err
		}},
		{"remove file", func(fs xrdfs.FileSystem) error { return fs.RemoveFile(ctx, "/a.bin") }},
		{"remove dir", func(fs xrdfs.FileSystem) error { return fs.RemoveDir(ctx, "/dir") }},
		{"remove all", func(fs xrdfs.FileSystem) error { return fs.RemoveAll(ctx, "/dir") }},
		{"mkdir", func(fs xrdfs.FileSystem) error { return fs.Mkdir(ctx, "/dir", 0755) }},
		{"mkdir all", func(fs xrdfs.FileSystem) error { return fs.MkdirAll(ctx, "/a/b/c", 0755) }},
		{"rename", func(fs xrdfs.FileSystem) error { return fs.Rename(ctx, "/a.bin", "/b.bin") }},
		{"truncate", func(fs xrdfs.FileSystem) error { return fs.Truncate(ctx, "/a.bin", 4) }},
		{"open for reading", func(fs xrdfs.FileSystem) error {
			_, err := fs.Open(ctx, "/a.bin", xrdfs.OpenModeOtherRead, xrdfs.OpenOptionsOpenRead)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.fn(deadClient(t).FS()); err == nil {
				t.Fatal("an unreachable endpoint reported success")
			}
		})
	}
}

func TestConformance_StatxReportsPerPathRatherThanFailing(t *testing.T) {
	// kXR_statx answers a flag per path, not one error for the batch: a path
	// it could not resolve comes back offline while the others still carry
	// their real flags. Returning an error for the whole call instead would
	// lose the answers for every path that did work.
	ctx := context.Background()

	flags, err := deadClient(t).FS().Statx(ctx, []string{"/a.bin", "/b.bin"})
	if err != nil {
		t.Fatalf("statx of an unreachable endpoint failed outright: %v", err)
	}
	if len(flags) != 2 {
		t.Fatalf("statx answered %d paths, want 2", len(flags))
	}
	for i, f := range flags {
		if f&xrdfs.StatIsOffline == 0 {
			t.Errorf("path %d is reported as reachable: %v", i, f)
		}
	}
}

func TestConformance_AStatOfADeadEndpointIsNotAMissingFile(t *testing.T) {
	// The distinction that matters to a copy: a file the server says is not
	// there can be skipped, a server that cannot be reached cannot.
	fi, err := deadClient(t).Stat(context.Background(), "/a.bin")
	if err == nil {
		t.Fatalf("an unreachable endpoint reported %+v", fi)
	}
	if fi.Exists {
		t.Fatal("a failed stat reported the file as present")
	}
}

func TestConformance_ACancelledContextStopsTheRequest(t *testing.T) {
	// A context cancelled before the call is made must not reach the network,
	// and must not be reported as anything but a cancellation.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := newRefusingClient(t, 200, nil)
	if _, err := c.Stat(ctx, "/a.bin"); err == nil {
		t.Fatal("a cancelled stat succeeded")
	} else if !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("the failure does not say it was cancelled: %v", err)
	}
	if err := c.Create(ctx, "/a.bin", bytes.NewReader([]byte("go-hep")), 6); err == nil {
		t.Fatal("a cancelled create succeeded")
	}
}

func TestConformance_AnEndpointMustBeHTTP(t *testing.T) {
	// root:// and file:// URLs reach this package by mistake — from a copy
	// dispatching on scheme — and have to be refused here rather than dialled
	// as if they were HTTP.
	for _, tc := range []struct {
		name string
		url  string
		want string
	}{
		{"a root url", "root://example.org//store/f.bin", "unsupported scheme"},
		{"a local path", "file:///tmp/f.bin", "unsupported scheme"},
		{"no scheme at all", "example.org/store", "unsupported scheme"},
		{"something that is not a url", "://%zz", "could not parse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Dial(tc.url)
			if err == nil {
				t.Fatalf("%q was accepted as an endpoint", tc.url)
			}
			if c != nil {
				t.Fatal("a refused endpoint still returned a client")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the failure does not say why: %v", err)
			}
		})
	}
}
