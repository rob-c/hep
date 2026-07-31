// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdcopy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdfs"
)

// TestConformance_TheLoginNameFollowsAFixedPrecedence pins who the copy logs in
// as. XRootD authorises on the login name, so picking the wrong one turns a
// permitted copy into a permission denied — or, worse, reads someone else's
// namespace. The order is: the option, then the URL, then $USER, then a name
// that is deliberately unprivileged.
func TestConformance_TheLoginNameFollowsAFixedPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
		url  xrootd.URL
		env  string
		want string
	}{
		{
			name: "the option wins over everything",
			opts: Options{Username: "from-option"},
			url:  xrootd.URL{User: "from-url"},
			env:  "from-env",
			want: "from-option",
		},
		{
			name: "the URL wins over the environment",
			url:  xrootd.URL{User: "from-url"},
			env:  "from-env",
			want: "from-url",
		},
		{
			name: "the environment is the last real answer",
			env:  "from-env",
			want: "from-env",
		},
		{
			name: "with nothing to go on, nobody",
			want: "nobody",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("USER", tc.env)
			if got := tc.opts.user(tc.url); got != tc.want {
				t.Errorf("the copy would log in as %q, want %q", got, tc.want)
			}
		})
	}
}

// checksumFS is an xrdfs.FileSystem that only answers checksum queries. The
// embedded nil interface satisfies the rest of the contract at compile time and
// panics if verifyChecksum ever reaches for anything else, which is the point:
// verifying a checksum must not touch the namespace.
type checksumFS struct {
	xrdfs.FileSystem

	algo  string
	value string
	err   error
}

func (fs checksumFS) Checksum(ctx context.Context, path string) (string, string, error) {
	return fs.algo, fs.value, fs.err
}

// plainFS is a filesystem with no checksum support at all.
type plainFS struct{ xrdfs.FileSystem }

// TestConformance_ChecksumVerificationOnlyFailsOnAMismatch is the contract that
// makes the check safe to run by default: it is silent when the server cannot
// answer, when it names an algorithm this client does not implement, or when
// the digests agree — and it fails, loudly and only, when the bytes on disk are
// not the bytes the server holds.
func TestConformance_ChecksumVerificationOnlyFailsOnAMismatch(t *testing.T) {
	ctx := context.Background()

	dir := t.TempDir()
	local := filepath.Join(dir, "copy.bin")
	const content = "the quick brown fox jumps over the lazy dog"
	if err := os.WriteFile(local, []byte(content), 0644); err != nil {
		t.Fatalf("could not write the local file: %v", err)
	}

	// The digest the server is supposed to report for that content.
	const wantAdler = "613c0ffa"

	for _, tc := range []struct {
		name    string
		fs      xrdfs.FileSystem
		local   string
		wantErr bool
		wantMsg string
	}{
		{
			name:  "a server with no checksum support",
			fs:    plainFS{},
			local: local,
		},
		{
			name:  "a server that refuses the query",
			fs:    checksumFS{err: errors.New("no checksum for this file")},
			local: local,
		},
		{
			name:  "an algorithm this client cannot compute",
			fs:    checksumFS{algo: "sha3-512", value: "whatever"},
			local: local,
		},
		{
			name:  "digests that agree",
			fs:    checksumFS{algo: "adler32", value: wantAdler},
			local: local,
		},
		{
			name:    "digests that disagree",
			fs:      checksumFS{algo: "adler32", value: "deadbeef"},
			local:   local,
			wantErr: true,
			// The message has to identify the file and both digests, or the
			// operator cannot tell which half of the copy to distrust.
			wantMsg: wantAdler,
		},
		{
			name:    "a local file that is not there",
			fs:      checksumFS{algo: "adler32", value: wantAdler},
			local:   filepath.Join(dir, "absent.bin"),
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyChecksum(ctx, tc.fs, "/remote/copy.bin", tc.local)
			switch {
			case tc.wantErr && err == nil:
				t.Error("the copy was accepted, want it reported as corrupt")
			case !tc.wantErr && err != nil:
				t.Errorf("the copy was rejected: %v", err)
			}
			if tc.wantMsg != "" && err != nil && !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("the failure reads %q, want it to mention %q", err, tc.wantMsg)
			}
		})
	}
}
