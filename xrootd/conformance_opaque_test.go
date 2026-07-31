// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance: opaque data (CGI) on the path field.
//
// A root:// path is not just a name. Everything past the first "?" is opaque
// data that the server splits off and hands to its authorization layer
// unparsed — it is how a bearer token or a TPC key reaches an endpoint. Two
// things therefore have to hold on every request that names a path, and
// neither of them is visible in a status code:
//
//   - the CGI the caller wrote reaches the server intact, and
//   - the namespace the server ends up addressing is the name alone.
//
// The strict namespace server records both halves of every path field it
// receives, so these tests assert on what arrived rather than on whether the
// call succeeded. A client that drops the token, mangles it, addresses
// "/f?authz=t" as a file name, or invents a CGI for a path that had none fails
// here even though every one of those bugs answers kXR_ok.

package xrootd

import (
	"context"
	"reflect"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdfs"
)

// wantOpaque fails t unless the server recorded exactly these names and CGIs,
// in this order.
func wantOpaque(t *testing.T, srv *confFS, paths, opaque []string) {
	t.Helper()

	if got := srv.pathSeq(); !reflect.DeepEqual(got, paths) {
		t.Errorf("the server was asked for %q, want %q", got, paths)
	}
	if got := srv.opaqueSeq(); !reflect.DeepEqual(got, opaque) {
		t.Errorf("the server saw the opaque data %q, want %q", got, opaque)
	}
}

// TestConformance_Opaque_OpenCarriesItsCGI is the case the whole surface rests
// on: an open is the request a token travels with, because it is the request an
// authorization layer gates.
func TestConformance_Opaque_OpenCarriesItsCGI(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) { srv.mkfile("/data/a.txt", []byte("hello")) },
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			srv.forget()

			f, err := fs.Open(context.Background(), "/data/a.txt?authz=tok&xrd.wantprot=unix", xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer f.Close(context.Background())

			wantOpaque(t, srv,
				[]string{"/data/a.txt"},
				[]string{"authz=tok&xrd.wantprot=unix"},
			)
		},
	)
	srv.check(t)
}

// TestConformance_Opaque_EveryNamespaceRequestCarriesIt sweeps the namespace
// surface. Each operation is given its own token so that a client which caches
// one and re-sends it, or which sends the previous request's, cannot pass.
func TestConformance_Opaque_EveryNamespaceRequestCarriesIt(t *testing.T) {
	ctx := context.Background()

	srv := confFSClient(t,
		func(srv *confFS) {
			srv.mkdirAs("/d", 0o755)
			srv.mkfile("/d/a", []byte("aaa"))
			srv.mkfile("/f", []byte("0123456789"))
			srv.mkdirAs("/empty", 0o755)
		},
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			for _, tc := range []struct {
				name   string
				fn     func() error
				paths  []string
				opaque []string
			}{
				{
					name:   "stat",
					fn:     func() error { _, err := fs.Stat(ctx, "/f?authz=t1"); return err },
					paths:  []string{"/f"},
					opaque: []string{"authz=t1"},
				},
				{
					name:   "dirlist",
					fn:     func() error { _, err := fs.Dirlist(ctx, "/d?authz=t2"); return err },
					paths:  []string{"/d"},
					opaque: []string{"authz=t2"},
				},
				{
					name:   "statx",
					fn:     func() error { _, err := fs.Statx(ctx, []string{"/f?authz=t3", "/d?authz=t4"}); return err },
					paths:  []string{"/f", "/d"},
					opaque: []string{"authz=t3", "authz=t4"},
				},
				{
					name:   "mkdir",
					fn:     func() error { return fs.Mkdir(ctx, "/cgi?authz=t5", 0o755) },
					paths:  []string{"/cgi"},
					opaque: []string{"authz=t5"},
				},
				{
					name:   "chmod",
					fn:     func() error { return fs.Chmod(ctx, "/f?authz=t6", 0o600) },
					paths:  []string{"/f"},
					opaque: []string{"authz=t6"},
				},
				{
					name:   "truncate",
					fn:     func() error { return fs.Truncate(ctx, "/f?authz=t7", 4) },
					paths:  []string{"/f"},
					opaque: []string{"authz=t7"},
				},
				{
					name:   "virtual stat",
					fn:     func() error { _, err := fs.VirtualStat(ctx, "/?authz=t8"); return err },
					paths:  []string{"/"},
					opaque: []string{"authz=t8"},
				},
				{
					name:   "rmdir",
					fn:     func() error { return fs.RemoveDir(ctx, "/empty?authz=t9") },
					paths:  []string{"/empty"},
					opaque: []string{"authz=t9"},
				},
				{
					name:   "rm",
					fn:     func() error { return fs.RemoveFile(ctx, "/d/a?authz=t10") },
					paths:  []string{"/d/a"},
					opaque: []string{"authz=t10"},
				},
			} {
				t.Run(tc.name, func(t *testing.T) {
					srv.forget()
					if err := tc.fn(); err != nil {
						t.Fatalf("%s: %v", tc.name, err)
					}
					wantOpaque(t, srv, tc.paths, tc.opaque)
				})
			}
		},
	)
	srv.check(t)
}

// TestConformance_Opaque_BothHalvesOfARenameKeepTheirOwn: kXR_mv puts two paths
// in one blob split by a length field. Each of them is a name in its own right
// and carries its own CGI — a client that concatenates first and splits the CGI
// off afterwards sends one token for two paths.
func TestConformance_Opaque_BothHalvesOfARenameKeepTheirOwn(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) { srv.mkfile("/old", []byte("payload")) },
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			srv.forget()

			if err := fs.Rename(context.Background(), "/old?a=1", "/new?b=2"); err != nil {
				t.Fatalf("Rename: %v", err)
			}
			wantOpaque(t, srv, []string{"/old", "/new"}, []string{"a=1", "b=2"})

			if srv.node("/new") == nil {
				t.Error("the rename did not land on /new")
			}
			if srv.node("/new?b=2") != nil {
				t.Error("the server was made to create a node whose name carries the CGI")
			}
		},
	)
	srv.check(t)
}

// TestConformance_Opaque_ExtendedAttributesCarryIt covers kXR_fattr, whose path
// sits inside a sub-structured body rather than being the whole payload — the
// place a CGI is easiest to drop.
func TestConformance_Opaque_ExtendedAttributesCarryIt(t *testing.T) {
	ctx := context.Background()

	srv := confFSClient(t,
		func(srv *confFS) { srv.mkfile("/f", []byte("data")) },
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			x, ok := fs.(xrdfs.XAttrFS)
			if !ok {
				t.Fatal("the filesystem does not implement xrdfs.XAttrFS")
			}

			for _, tc := range []struct {
				name   string
				fn     func() error
				opaque string
			}{
				{"set", func() error { return x.SetXAttr(ctx, "/f?authz=s", "user.a", []byte("v")) }, "authz=s"},
				{"get", func() error { _, err := x.GetXAttr(ctx, "/f?authz=g", "user.a"); return err }, "authz=g"},
				{"list", func() error { _, err := x.ListXAttr(ctx, "/f?authz=l"); return err }, "authz=l"},
				{"del", func() error { return x.DelXAttr(ctx, "/f?authz=d", "user.a") }, "authz=d"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					srv.forget()
					if err := tc.fn(); err != nil {
						t.Fatalf("%s: %v", tc.name, err)
					}
					wantOpaque(t, srv, []string{"/f"}, []string{tc.opaque})
				})
			}
		},
	)
	srv.check(t)
}

// TestConformance_Opaque_APathWithNoneGetsNone is the other half of the
// contract: the client invents nothing. A stray "?" is not cosmetic — it
// reaches the server's authorization layer as an empty CGI, and it is exactly
// what an unconditional SetOpaque produces.
func TestConformance_Opaque_APathWithNoneGetsNone(t *testing.T) {
	ctx := context.Background()

	srv := confFSClient(t,
		func(srv *confFS) {
			srv.mkdirAs("/d", 0o755)
			srv.mkfile("/f", []byte("x"))
		},
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			srv.forget()

			if _, err := fs.Stat(ctx, "/f"); err != nil {
				t.Fatalf("Stat: %v", err)
			}
			if _, err := fs.Dirlist(ctx, "/d"); err != nil {
				t.Fatalf("Dirlist: %v", err)
			}
			f, err := fs.Open(ctx, "/f", xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer f.Close(ctx)

			wantOpaque(t, srv, []string{"/f", "/d", "/f"}, []string{"", "", ""})
		},
	)
	srv.check(t)
}

// TestConformance_Opaque_MkdirAllCarriesItOnEveryLevel: MkdirAll is one call
// that becomes several requests, and every one of them has to be authorized on
// its own. Sending the token with the last level only is a client that works
// against an open server and fails against a gated one.
func TestConformance_Opaque_MkdirAllCarriesItOnEveryLevel(t *testing.T) {
	srv := confFSClient(t, nil,
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			srv.forget()

			if err := fs.MkdirAll(context.Background(), "/a/b/c?authz=deep", 0o755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}

			for i, opaque := range srv.opaqueSeq() {
				if opaque != "authz=deep" {
					t.Errorf("request %d (%s) carried the opaque data %q, want %q",
						i, srv.pathSeq()[i], opaque, "authz=deep")
				}
			}
			for _, name := range []string{"/a", "/a/b", "/a/b/c"} {
				if srv.node(name) == nil {
					t.Errorf("%s was not created", name)
				}
			}
		},
	)
	srv.check(t)
}

// TestConformance_Opaque_RemoveAllCarriesItOnEveryChild is the same argument
// for the walk that goes the other way: RemoveAll lists a directory and then
// removes what it found, and the names it builds from a listing have to inherit
// the CGI of the path the caller named.
func TestConformance_Opaque_RemoveAllCarriesItOnEveryChild(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) {
			srv.mkdirAs("/tree", 0o755)
			srv.mkfile("/tree/a", []byte("a"))
			srv.mkdirAs("/tree/sub", 0o755)
			srv.mkfile("/tree/sub/b", []byte("b"))
		},
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			srv.forget()

			if err := fs.RemoveAll(context.Background(), "/tree?authz=rm"); err != nil {
				t.Fatalf("RemoveAll: %v", err)
			}

			paths, opaque := srv.pathSeq(), srv.opaqueSeq()
			if len(paths) < 4 {
				t.Fatalf("RemoveAll addressed %d paths, want at least 4: %q", len(paths), paths)
			}
			for i, o := range opaque {
				if o != "authz=rm" {
					t.Errorf("request %d (%s) carried the opaque data %q, want %q",
						i, paths[i], o, "authz=rm")
				}
			}
			if srv.node("/tree") != nil {
				t.Error("the tree survived the removal")
			}
		},
	)
	srv.check(t)
}

// TestConformance_Opaque_URLsKeepItInThePath pins the layer above: a URL is
// parsed into an address and a path, and the CGI stays on the path rather than
// being parsed, dropped, or promoted to a field of its own. Everything the
// tests above assert on the wire depends on this.
func TestConformance_Opaque_URLsKeepItInThePath(t *testing.T) {
	for _, tc := range []struct {
		url  string
		addr string
		path string
	}{
		{"root://host//f?authz=t", "host", "/f?authz=t"},
		{"root://host:1234//d/f?authz=t&x=1", "host:1234", "/d/f?authz=t&x=1"},
		{"roots://host//f?authz=t", "host", "/f?authz=t"},
		// A single slash names a path too; only the doubled one is collapsed.
		{"root://host/f?authz=t", "host", "/f?authz=t"},
		// A "?" in the CGI value is the server's problem, not the client's:
		// the path field is handed on whole.
		{"root://host//f?authz=a?b", "host", "/f?authz=a?b"},
	} {
		t.Run(tc.url, func(t *testing.T) {
			got, err := ParseURL(tc.url)
			if err != nil {
				t.Fatalf("ParseURL: %v", err)
			}
			if got.Addr != tc.addr {
				t.Errorf("address is %q, want %q", got.Addr, tc.addr)
			}
			if got.Path != tc.path {
				t.Errorf("path is %q, want %q", got.Path, tc.path)
			}
		})
	}
}
