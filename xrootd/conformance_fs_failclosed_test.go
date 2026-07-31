// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Fail-closed conformance for the namespace surface: a server that refuses,
// defers, answers a stream nobody is waiting on, or sends a reply that is well
// framed but not well formed must never be reported as success. Each case
// drives the strict server from conformance_fs_server_test.go into one specific
// misbehaviour.

package xrootd

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdfs"
)

// TestConformance_Dirlist_MalformedReplyIsRefused sends a stat-info listing
// with an odd number of lines: one name has no stat line to go with it, so
// pairing them up would attach the wrong stat to the wrong entry.
func TestConformance_Dirlist_MalformedReplyIsRefused(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) {
			srv.mkdirAs("/d", 0o755)
			srv.mkfile("/d/a", nil)
			srv.bodyNext[confDirlist] = []byte(".\n0 0 0 0\nonly-a-name\x00")
		},
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			got, err := fs.Dirlist(context.Background(), "/d")
			if err != nil {
				return // refused, which is the point
			}
			// Falling back to names-only decoding is acceptable; inventing
			// stat info for an unpaired name is not.
			for _, e := range got {
				if e.HasStatInfo {
					t.Fatalf("a malformed listing yielded stat info: %+v", got)
				}
			}
		},
	)
	srv.check(t)
}

// TestConformance_Stat_MalformedReplyIsRefused truncates the stat line to two
// fields. A client that parses what it can would report a file with a plausible
// size and no flags at all.
func TestConformance_Stat_MalformedReplyIsRefused(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) {
			srv.mkfile("/f", []byte("12345"))
			srv.bodyNext[confStat] = []byte("1 2")
		},
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			if _, err := fs.Stat(context.Background(), "/f"); err == nil {
				t.Fatal("a stat reply with two fields was accepted")
			}
		},
	)
	srv.check(t)
}

// TestConformance_Query_MalformedChecksumIsRefused covers a reply that is
// well-framed but not well-formed: the algorithm and the digest must both be
// there, and nothing else may be.
func TestConformance_Query_MalformedChecksumIsRefused(t *testing.T) {
	for _, body := range []string{"adler32", "", "adler32 062c0215 extra"} {
		t.Run(body, func(t *testing.T) {
			srv := confFSClient(t,
				func(srv *confFS) {
					srv.mkfile("/f", []byte("hello"))
					srv.bodyNext[confQuery] = []byte(body)
				},
				func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
					alg, sum, err := fs.(xrdfs.ChecksumFS).Checksum(context.Background(), "/f")
					if err == nil {
						t.Fatalf("a malformed checksum reply %q was accepted as %q %q", body, alg, sum)
					}
				},
			)
			srv.check(t)
		})
	}
}

// TestConformance_ServerErrorsSurfaceOnEveryOperation checks no operation
// swallows a kXR_error: every one of them must report the server's own message
// rather than a zero value.
func TestConformance_ServerErrorsSurfaceOnEveryOperation(t *testing.T) {
	const msg = "the server said no"

	for _, tc := range []struct {
		name  string
		reqID uint16
		call  func(ctx context.Context, fs xrdfs.FileSystem) error
	}{
		{"dirlist", confDirlist, func(ctx context.Context, fs xrdfs.FileSystem) error {
			_, err := fs.Dirlist(ctx, "/d")
			return err
		}},
		{"open", confOpen, func(ctx context.Context, fs xrdfs.FileSystem) error {
			_, err := fs.Open(ctx, "/f", 0, xrdfs.OpenOptionsOpenRead)
			return err
		}},
		{"stat", confStat, func(ctx context.Context, fs xrdfs.FileSystem) error {
			_, err := fs.Stat(ctx, "/f")
			return err
		}},
		{"virtualstat", confStat, func(ctx context.Context, fs xrdfs.FileSystem) error {
			_, err := fs.VirtualStat(ctx, "/")
			return err
		}},
		{"statx", confStatx, func(ctx context.Context, fs xrdfs.FileSystem) error {
			_, err := fs.Statx(ctx, []string{"/f"})
			return err
		}},
		{"mkdir", confMkdir, func(ctx context.Context, fs xrdfs.FileSystem) error {
			return fs.Mkdir(ctx, "/new", 0o755)
		}},
		{"mkdirall", confMkdir, func(ctx context.Context, fs xrdfs.FileSystem) error {
			return fs.MkdirAll(ctx, "/new/deep", 0o755)
		}},
		{"rename", confMv, func(ctx context.Context, fs xrdfs.FileSystem) error {
			return fs.Rename(ctx, "/f", "/g")
		}},
		{"chmod", confChmod, func(ctx context.Context, fs xrdfs.FileSystem) error {
			return fs.Chmod(ctx, "/f", 0o600)
		}},
		{"removefile", confRm, func(ctx context.Context, fs xrdfs.FileSystem) error {
			return fs.RemoveFile(ctx, "/f")
		}},
		{"removedir", confRmdir, func(ctx context.Context, fs xrdfs.FileSystem) error {
			return fs.RemoveDir(ctx, "/d")
		}},
		{"removeall", confStat, func(ctx context.Context, fs xrdfs.FileSystem) error {
			return fs.RemoveAll(ctx, "/d")
		}},
		{"truncate", confTruncate, func(ctx context.Context, fs xrdfs.FileSystem) error {
			return fs.Truncate(ctx, "/f", 1)
		}},
		{"getxattr", confFattr, func(ctx context.Context, fs xrdfs.FileSystem) error {
			_, err := fs.(xrdfs.XAttrFS).GetXAttr(ctx, "/f", "user.a")
			return err
		}},
		{"setxattr", confFattr, func(ctx context.Context, fs xrdfs.FileSystem) error {
			return fs.(xrdfs.XAttrFS).SetXAttr(ctx, "/f", "user.a", []byte("v"))
		}},
		{"delxattr", confFattr, func(ctx context.Context, fs xrdfs.FileSystem) error {
			return fs.(xrdfs.XAttrFS).DelXAttr(ctx, "/f", "user.a")
		}},
		{"listxattr", confFattr, func(ctx context.Context, fs xrdfs.FileSystem) error {
			_, err := fs.(xrdfs.XAttrFS).ListXAttr(ctx, "/f")
			return err
		}},
		{"checksum", confQuery, func(ctx context.Context, fs xrdfs.FileSystem) error {
			_, _, err := fs.(xrdfs.ChecksumFS).Checksum(ctx, "/f")
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := confFSClient(t,
				func(srv *confFS) {
					srv.mkfile("/f", []byte("12345"))
					srv.mkdirAs("/d", 0o755)
					srv.failNext[tc.reqID] = msg
				},
				func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
					err := tc.call(context.Background(), fs)
					if err == nil {
						t.Fatal("the server refused, but the client reported success")
					}
					if !strings.Contains(err.Error(), msg) {
						t.Fatalf("the server's message was lost: got %q, want it to mention %q", err, msg)
					}
				},
			)
			srv.check(t)
		})
	}
}

// TestConformance_WaitIsRetried checks kXR_wait is a deferral, not an answer:
// the request must be sent again rather than reported as an empty success.
func TestConformance_WaitIsRetried(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) {
			srv.mkfile("/f", []byte("12345"))
			srv.waitNext[confStat] = true
		},
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			got, err := fs.Stat(context.Background(), "/f")
			if err != nil {
				t.Fatalf("Stat: %v", err)
			}
			if got.EntrySize != 5 {
				t.Fatalf("got size %d, want 5", got.EntrySize)
			}
		},
	)
	srv.check(t)
	if got, want := srv.opCount(confStat), 2; got != want {
		t.Fatalf("got %d kXR_stat requests, want %d", got, want)
	}
}

// TestConformance_UnsolicitedFrameIsIgnored sends a well-formed reply on a
// stream id nobody is waiting on. It must not be handed to the pending request.
func TestConformance_UnsolicitedFrameIsIgnored(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) {
			srv.mkfile("/f", []byte("12345"))
			srv.junk = true
		},
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			got, err := fs.Stat(context.Background(), "/f")
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
		},
	)
	srv.check(t)
}

// TestConformance_ShortReplyIsRefused truncates the body below the length the
// header promises. Decoding what arrived would silently drop the last entry.
func TestConformance_ShortReplyIsRefused(t *testing.T) {
	srv := confFSClient(t,
		func(srv *confFS) {
			srv.mkdirAs("/d", 0o755)
			srv.mkfile("/d/a", nil)
			srv.mkfile("/d/b", nil)
			srv.cutNext[confDirlist] = 4
		},
		func(srv *confFS, fs xrdfs.FileSystem, cli *Client) {
			if _, err := fs.Dirlist(context.Background(), "/d"); err == nil {
				t.Fatal("a reply shorter than its header promised was accepted")
			}
		},
	)
	srv.check(t)
}
