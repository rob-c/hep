// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the login name, which nearly every URL leaves out.
//
// "root://server//store/user/gopher/f.root" names no user, and a client that
// takes that literally logs in as nobody in the unhelpful sense: an empty
// name, which is not what the command-line tools send and not what a site's
// mapping expects. What is pinned here is that an unstated login name is
// filled in, and filled in the same way everywhere.

package xrootd

import (
	"context"
	"net"
	"os"
	"testing"
)

func TestConformance_AnUnstatedLoginNameIsFilledIn(t *testing.T) {
	for _, tc := range []struct {
		name    string
		want    string
		fromURL string
		user    string // $USER
		want2   string
	}{
		{name: "gopher", fromURL: "urluser", user: "shelluser", want2: "gopher"},
		{name: "", fromURL: "urluser", user: "shelluser", want2: "urluser"},
		{name: "", fromURL: "", user: "shelluser", want2: "shelluser"},
		{name: "", fromURL: "", user: "", want2: "nobody"},
	} {
		t.Run(tc.want2, func(t *testing.T) {
			t.Setenv("USER", tc.user)
			if got := Username(tc.name, tc.fromURL); got != tc.want2 {
				t.Fatalf("Username(%q, %q) = %q, want %q", tc.name, tc.fromURL, got, tc.want2)
			}
		})
	}
}

func TestConformance_AClientWithNoLoginNameUsesTheDefault(t *testing.T) {
	t.Setenv("USER", "gopher")

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}

	srv := NewServer(NewFSHandler(t.TempDir()), func(err error) {
		t.Logf("xrd-srv: %v", err)
	})
	go func() {
		if err := srv.Serve(listener); err != nil && err != ErrServerClosed {
			t.Logf("xrd-srv: could not serve: %v", err)
		}
	}()
	defer srv.Shutdown(context.Background())

	ctx := context.Background()
	addr := listener.Addr().String()

	for _, tc := range []struct {
		name string
		url  string
		user string
		want string
	}{
		{
			name: "no user anywhere",
			url:  "root://" + addr + "/",
			want: "gopher", // $USER
		},
		{
			name: "the URL carries one",
			url:  "root://alice@" + addr + "/",
			want: "alice",
		},
		{
			name: "the caller asked for one",
			url:  "root://alice@" + addr + "/",
			user: "bob",
			want: "bob",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewClient(ctx, tc.url, tc.user)
			if err != nil {
				t.Fatalf("could not connect: %v", err)
			}
			defer client.Close()

			if client.username != tc.want {
				t.Fatalf("logged in as %q, want %q", client.username, tc.want)
			}
		})
	}

	// And with no $USER either, the name every site accepts for anonymous
	// access, rather than an empty one.
	os.Unsetenv("USER")
	client, err := NewClient(ctx, "root://"+addr+"/", "")
	if err != nil {
		t.Fatalf("could not connect: %v", err)
	}
	defer client.Close()
	if client.username != "nobody" {
		t.Fatalf("logged in as %q, want %q", client.username, "nobody")
	}
}
