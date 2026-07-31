// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance: the namespace happy paths, driven against the strict server in
// conformance_fs_server_test.go. Every case asserts on what the server ended
// up holding rather than on a status code, and asserts the server recorded no
// protocol violation.

package xrootd

import (
	"context"
	"reflect"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdfs"
)

func TestConformance_Dirlist_EntriesAndStatInfo(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) {
			srv.mkdirAs("/d", 0o755)
			srv.mkfile("/d/a", []byte("aaa"))
			srv.mkfile("/d/b", []byte("bb"))
			srv.mkdirAs("/d/sub", 0o755)
			srv.mkfile("/other", []byte("not a child of /d"))
		},
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			got, err := fs.Dirlist(context.Background(), "/d")
			if err != nil {
				t.Fatalf("Dirlist: %v", err)
			}

			rw := xrdfs.StatIsReadable | xrdfs.StatIsWritable
			want := []xrdfs.EntryStat{
				{EntryName: "a", HasStatInfo: true, ID: 4, EntrySize: 3, Flags: rw, Mtime: confMtime},
				{EntryName: "b", HasStatInfo: true, ID: 4, EntrySize: 2, Flags: rw, Mtime: confMtime},
				{EntryName: "sub", HasStatInfo: true, ID: 6, Flags: rw | xrdfs.StatIsDir | xrdfs.StatIsExecutable, Mtime: confMtime},
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Dirlist:\ngot  = %+v\nwant = %+v", got, want)
			}
		},
	)
	srv.check(t)
}

// TestConformance_Dirlist_WithoutStatInfo covers the other half of the reply
// format: a server that ignores the stat-info option answers with bare names,
// and the client must notice rather than read the first name as a stat line.
func TestConformance_Dirlist_WithoutStatInfo(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) {
			srv.noStat = true
			srv.mkdirAs("/d", 0o755)
			srv.mkfile("/d/a", []byte("aaa"))
			srv.mkfile("/d/b", nil)
		},
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			got, err := fs.Dirlist(context.Background(), "/d")
			if err != nil {
				t.Fatalf("Dirlist: %v", err)
			}
			want := []xrdfs.EntryStat{{EntryName: "a"}, {EntryName: "b"}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Dirlist:\ngot  = %+v\nwant = %+v", got, want)
			}
		},
	)
	srv.check(t)
}

func TestConformance_Dirlist_EmptyDirectory(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) { srv.mkdirAs("/empty", 0o755) },
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			got, err := fs.Dirlist(context.Background(), "/empty")
			if err != nil {
				t.Fatalf("Dirlist: %v", err)
			}
			if len(got) != 0 {
				t.Fatalf("got %d entries for an empty directory: %+v", len(got), got)
			}
		},
	)
	srv.check(t)
}

// TestConformance_Open_CloseReleasesTheHandle also covers the reply layout: the
// stat info sits behind the compression descriptor, so a client that skips the
// descriptor reads the file size out of the compression page size.
func TestConformance_Open_CloseReleasesTheHandle(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) { srv.mkfile("/f", []byte("0123456789")) },
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			ctx := context.Background()
			f, err := fs.Open(ctx, "/f", 0, xrdfs.OpenOptionsOpenRead|xrdfs.OpenOptionsReturnStatus)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if got, want := f.Handle(), (xrdfs.FileHandle{0, 0, 0, 1}); got != want {
				t.Fatalf("got handle %v, want %v", got, want)
			}
			if c := f.Compression(); c == nil {
				t.Fatal("the compression descriptor was not decoded")
			}
			info := f.Info()
			if info == nil {
				t.Fatal("the returned stat info was not decoded")
			}
			if got, want := info.EntrySize, int64(10); got != want {
				t.Fatalf("got size %d, want %d", got, want)
			}
			if info.IsDir() {
				t.Fatal("a regular file was reported as a directory")
			}

			if err := f.Close(ctx); err != nil {
				t.Fatalf("Close: %v", err)
			}
			// The handle is the server's to reclaim: a client that keeps using
			// one after close is reading someone else's file.
			srv.mu.Lock()
			left := len(srv.handles)
			srv.mu.Unlock()
			if left != 0 {
				t.Fatalf("%d file handles were left open after Close", left)
			}
		},
	)
	srv.check(t)
	if got, want := srv.opSeq(), []uint16{confOpen, confClose}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got op sequence %v, want %v", got, want)
	}
}

func TestConformance_Open_Refusals(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) { srv.mkfile("/f", nil) },
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			ctx := context.Background()
			if _, err := fs.Open(ctx, "/absent", 0, xrdfs.OpenOptionsOpenRead); err == nil {
				t.Fatal("a missing file was opened for reading")
			}
			if _, err := fs.Open(ctx, "/f", 0o644, xrdfs.OpenOptionsNew); err == nil {
				t.Fatal("kXR_new opened a file that already exists")
			}
		},
	)
	srv.check(t)
}

// TestConformance_Open_CreatesOnTheWritePath checks the file appears, which is
// the only way to tell an accepted open from an ignored one.
func TestConformance_Open_CreatesOnTheWritePath(t *testing.T) {
	srv := confFSClient(t, nil,
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			ctx := context.Background()
			f, err := fs.Open(ctx, "/new", 0o644, xrdfs.OpenOptionsDelete|xrdfs.OpenOptionsOpenUpdate)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if err := f.Close(ctx); err != nil {
				t.Fatalf("Close: %v", err)
			}
		},
	)
	srv.check(t)
	if got := srv.sizeOf("/new"); got != 0 {
		t.Fatalf("the file was not created: size %d", got)
	}
}

func TestConformance_Stat_ByPath(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) {
			srv.mkfile("/f", []byte("12345"))
			srv.mkdirAs("/d", 0o755)
		},
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			ctx := context.Background()

			got, err := fs.Stat(ctx, "/f")
			if err != nil {
				t.Fatalf("Stat: %v", err)
			}
			want := xrdfs.EntryStat{
				HasStatInfo: true,
				ID:          2,
				EntrySize:   5,
				Flags:       xrdfs.StatIsReadable | xrdfs.StatIsWritable,
				Mtime:       confMtime,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Stat:\ngot  = %+v\nwant = %+v", got, want)
			}

			dir, err := fs.Stat(ctx, "/d")
			if err != nil {
				t.Fatalf("Stat: %v", err)
			}
			if !dir.IsDir() {
				t.Fatal("a directory was not reported as one")
			}

			if _, err := fs.Stat(ctx, "/absent"); err == nil {
				t.Fatal("a missing file was stat'ed without error")
			}
		},
	)
	srv.check(t)
}

// TestConformance_Stat_ByHandle checks the other addressing mode: an open file
// is stat'ed by handle, with no path on the wire at all.
func TestConformance_Stat_ByHandle(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) { srv.mkfile("/f", []byte("123")) },
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			ctx := context.Background()
			f, err := fs.Open(ctx, "/f", 0, xrdfs.OpenOptionsOpenRead)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer f.Close(ctx)

			got, err := f.Stat(ctx)
			if err != nil {
				t.Fatalf("Stat: %v", err)
			}
			if got.EntrySize != 3 {
				t.Fatalf("got size %d, want 3", got.EntrySize)
			}
			if f.Info() == nil || f.Info().EntrySize != 3 {
				t.Fatal("the cached stat info was not refreshed")
			}
		},
	)
	srv.check(t)
}

func TestConformance_Stat_VirtualFS(t *testing.T) {
	srv := confFSClient(t, nil,
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			got, err := fs.VirtualStat(context.Background(), "/")
			if err != nil {
				t.Fatalf("VirtualStat: %v", err)
			}
			want := xrdfs.VirtualFSStat{
				NumberRW: 2, FreeRW: 1024, UtilizationRW: 50,
				NumberStaging: 1, FreeStaging: 2048, UtilizationStaging: 25,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("VirtualStat:\ngot  = %+v\nwant = %+v", got, want)
			}
		},
	)
	srv.check(t)
}

// TestConformance_Statx_FlagsFollowTheRequestOrder checks the answers stay tied
// to the questions: the reply is a bare run of flag bytes, so only their order
// says which path each one belongs to.
func TestConformance_Statx_FlagsFollowTheRequestOrder(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) {
			srv.mkfile("/f", nil)
			srv.mkdirAs("/d", 0o755)
		},
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			got, err := fs.Statx(context.Background(), []string{"/d", "/absent", "/f"})
			if err != nil {
				t.Fatalf("Statx: %v", err)
			}
			want := []xrdfs.StatFlags{
				xrdfs.StatIsDir | xrdfs.StatIsExecutable,
				xrdfs.StatIsOffline,
				xrdfs.StatIsFile,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Statx:\ngot  = %v\nwant = %v", got, want)
			}
		},
	)
	srv.check(t)
}

// TestConformance_Mkdir_MkpathIsWhatMakesMkdirAll checks the option bit is the
// only difference between the two: without it the server must refuse a missing
// parent, so a client that always sets it silently deepens the namespace.
func TestConformance_Mkdir_MkpathIsWhatMakesMkdirAll(t *testing.T) {
	srv := confFSClient(t, nil,
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			ctx := context.Background()

			if err := fs.Mkdir(ctx, "/a/b", 0o750); err == nil {
				t.Fatal("Mkdir created a directory under a missing parent")
			}
			if err := fs.Mkdir(ctx, "/a", 0o750); err != nil {
				t.Fatalf("Mkdir: %v", err)
			}
			if err := fs.Mkdir(ctx, "/a", 0o750); err == nil {
				t.Fatal("Mkdir overwrote an existing directory")
			}
			if err := fs.MkdirAll(ctx, "/x/y/z", 0o755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}
		},
	)
	srv.check(t)

	if got, want := srv.names(), []string{"/", "/a", "/x", "/x/y", "/x/y/z"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("namespace:\ngot  = %v\nwant = %v", got, want)
	}
	if got, want := srv.modeOf("/a"), uint16(0o750); got != want {
		t.Fatalf("got mode %#o, want %#o", got, want)
	}
}

// TestConformance_Mv_RenamesExactlyOnePath guards the split: the old and the
// new path share one blob on the wire, divided by a length the client computes,
// so getting it wrong renames a neighbouring path.
func TestConformance_Mv_RenamesExactlyOnePath(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) { srv.mkfile("/old-name", []byte("payload")) },
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			ctx := context.Background()
			if err := fs.Rename(ctx, "/old-name", "/new"); err != nil {
				t.Fatalf("Rename: %v", err)
			}
			if err := fs.Rename(ctx, "/absent", "/whatever"); err == nil {
				t.Fatal("a missing file was renamed")
			}
		},
	)
	srv.check(t)

	if got, want := srv.names(), []string{"/", "/new"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("namespace:\ngot  = %v\nwant = %v", got, want)
	}
	if got := srv.sizeOf("/new"); got != len("payload") {
		t.Fatalf("the renamed file holds %d bytes, want %d", got, len("payload"))
	}
}

func TestConformance_Chmod_ChangesTheStoredMode(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) { srv.mkfile("/f", nil) },
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			ctx := context.Background()
			if err := fs.Chmod(ctx, "/f", 0o640); err != nil {
				t.Fatalf("Chmod: %v", err)
			}
			if err := fs.Chmod(ctx, "/absent", 0o640); err == nil {
				t.Fatal("a missing file was chmod'ed")
			}
		},
	)
	srv.check(t)
	if got, want := srv.modeOf("/f"), uint16(0o640); got != want {
		t.Fatalf("got mode %#o, want %#o", got, want)
	}
}

func TestConformance_Rm_AndRmdirAreNotInterchangeable(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) {
			srv.mkfile("/f", nil)
			srv.mkdirAs("/d", 0o755)
			srv.mkfile("/d/kid", nil)
		},
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			ctx := context.Background()

			if err := fs.RemoveFile(ctx, "/f"); err != nil {
				t.Fatalf("RemoveFile: %v", err)
			}
			if err := fs.RemoveFile(ctx, "/d"); err == nil {
				t.Fatal("a directory was removed with kXR_rm")
			}
			if err := fs.RemoveDir(ctx, "/d"); err == nil {
				t.Fatal("a non-empty directory was removed")
			}
			if err := fs.RemoveDir(ctx, "/d/kid"); err == nil {
				t.Fatal("a regular file was removed with kXR_rmdir")
			}
		},
	)
	srv.check(t)
	if got, want := srv.names(), []string{"/", "/d", "/d/kid"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("namespace:\ngot  = %v\nwant = %v", got, want)
	}
}

// TestConformance_RemoveAll_WalksBottomUp checks the recursion: RemoveAll has
// no request of its own, so it is a sequence of stat, dirlist and removals, and
// the only thing proving it is bottom-up is that every kXR_rmdir lands after
// the removals of that directory's members.
func TestConformance_RemoveAll_WalksBottomUp(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) {
			srv.mkdirAs("/d", 0o755)
			srv.mkdirAs("/d/sub", 0o755)
			srv.mkfile("/d/sub/deep", []byte("x"))
			srv.mkfile("/d/kid", nil)
			srv.mkfile("/keep", nil)
		},
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			if err := fs.RemoveAll(context.Background(), "/d"); err != nil {
				t.Fatalf("RemoveAll: %v", err)
			}
		},
	)
	srv.check(t)

	if got, want := srv.names(), []string{"/", "/keep"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("namespace:\ngot  = %v\nwant = %v", got, want)
	}
	want := []uint16{
		confStat, confDirlist, // /d
		confStat, confRm, // /d/kid
		confStat, confDirlist, // /d/sub
		confStat, confRm, // /d/sub/deep
		confRmdir, // /d/sub
		confRmdir, // /d
	}
	if got := srv.opSeq(); !reflect.DeepEqual(got, want) {
		t.Fatalf("op sequence:\ngot  = %v\nwant = %v", got, want)
	}
}

// TestConformance_Truncate_ByPathAndByHandle covers both addressing modes. The
// handle form must not also send the path: the server would have two targets
// and no rule for choosing between them.
func TestConformance_Truncate_ByPathAndByHandle(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) {
			srv.mkfile("/shrink", []byte("0123456789"))
			srv.mkfile("/grow", []byte("012"))
			srv.mkfile("/by-handle", []byte("0123456789"))
		},
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			ctx := context.Background()

			if err := fs.Truncate(ctx, "/shrink", 4); err != nil {
				t.Fatalf("Truncate: %v", err)
			}
			if err := fs.Truncate(ctx, "/grow", 8); err != nil {
				t.Fatalf("Truncate: %v", err)
			}
			if err := fs.Truncate(ctx, "/absent", 0); err == nil {
				t.Fatal("a missing file was truncated")
			}

			f, err := fs.Open(ctx, "/by-handle", 0, xrdfs.OpenOptionsOpenUpdate)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer f.Close(ctx)
			if err := f.Truncate(ctx, 2); err != nil {
				t.Fatalf("Truncate: %v", err)
			}
		},
	)
	srv.check(t)

	for _, tc := range []struct {
		name string
		want int
	}{
		{"/shrink", 4},
		{"/grow", 8},
		{"/by-handle", 2},
	} {
		if got := srv.sizeOf(tc.name); got != tc.want {
			t.Errorf("%s: got size %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestConformance_FAttr_RoundTrip(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) { srv.mkfile("/f", nil) },
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			ctx := context.Background()
			x, ok := fs.(xrdfs.XAttrFS)
			if !ok {
				t.Fatal("the filesystem does not implement xrdfs.XAttrFS")
			}

			if err := x.SetXAttr(ctx, "/f", "user.a", []byte("value-a")); err != nil {
				t.Fatalf("SetXAttr: %v", err)
			}
			if err := x.SetXAttr(ctx, "/f", "user.b", nil); err != nil {
				t.Fatalf("SetXAttr: %v", err)
			}

			got, err := x.GetXAttr(ctx, "/f", "user.a")
			if err != nil {
				t.Fatalf("GetXAttr: %v", err)
			}
			if string(got) != "value-a" {
				t.Fatalf("got %q, want %q", got, "value-a")
			}

			names, err := x.ListXAttr(ctx, "/f")
			if err != nil {
				t.Fatalf("ListXAttr: %v", err)
			}
			if want := []string{"user.a", "user.b"}; !reflect.DeepEqual(names, want) {
				t.Fatalf("ListXAttr:\ngot  = %v\nwant = %v", names, want)
			}

			if err := x.DelXAttr(ctx, "/f", "user.a"); err != nil {
				t.Fatalf("DelXAttr: %v", err)
			}
			// The per-attribute status code is not the request status: the
			// reply is kXR_ok and carries the failure inside its body.
			if _, err := x.GetXAttr(ctx, "/f", "user.a"); err == nil {
				t.Fatal("a deleted attribute was read back without error")
			}

			names, err = x.ListXAttr(ctx, "/f")
			if err != nil {
				t.Fatalf("ListXAttr: %v", err)
			}
			if want := []string{"user.b"}; !reflect.DeepEqual(names, want) {
				t.Fatalf("ListXAttr:\ngot  = %v\nwant = %v", names, want)
			}
		},
	)
	srv.check(t)

	if v, ok := srv.xattrOf("/f", "user.b"); !ok || len(v) != 0 {
		t.Fatalf("got attribute %q (present=%v), want an empty value", v, ok)
	}
	if _, ok := srv.xattrOf("/f", "user.a"); ok {
		t.Fatal("the deleted attribute is still on the server")
	}
}

func TestConformance_FAttr_MissingFileIsAnError(t *testing.T) {
	srv := confFSClient(t, nil,
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			ctx := context.Background()
			x := fs.(xrdfs.XAttrFS)
			if _, err := x.ListXAttr(ctx, "/absent"); err == nil {
				t.Fatal("attributes were listed for a missing file")
			}
			if err := x.SetXAttr(ctx, "/absent", "user.a", []byte("v")); err == nil {
				t.Fatal("an attribute was set on a missing file")
			}
		},
	)
	srv.check(t)
}

func TestConformance_Query_Checksum(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) { srv.mkfile("/f", []byte("hello")) },
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			cs, ok := fs.(xrdfs.ChecksumFS)
			if !ok {
				t.Fatal("the filesystem does not implement xrdfs.ChecksumFS")
			}
			alg, sum, err := cs.Checksum(context.Background(), "/f")
			if err != nil {
				t.Fatalf("Checksum: %v", err)
			}
			// Adler-32 of "hello", worked out by hand rather than by the code
			// that would verify it.
			if alg != "adler32" || sum != "062c0215" {
				t.Fatalf("got %q %q, want %q %q", alg, sum, "adler32", "062c0215")
			}

			if _, _, err := cs.Checksum(context.Background(), "/absent"); err == nil {
				t.Fatal("a missing file returned a checksum")
			}
		},
	)
	srv.check(t)
}

func TestConformance_Ping(t *testing.T) {
	srv := confFSClient(t, nil,
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			sess := cli.sessions[cli.initialSessionID]
			for range 3 {
				if err := sess.Ping(context.Background()); err != nil {
					t.Fatalf("Ping: %v", err)
				}
			}
		},
	)
	srv.check(t)
	if got, want := srv.opCount(confPing), 3; got != want {
		t.Fatalf("got %d pings, want %d", got, want)
	}
}
