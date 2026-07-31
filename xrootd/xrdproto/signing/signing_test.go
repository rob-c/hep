// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the request-signing requirements table of XRootD protocol
// specification v3.1.0, p.75-76.
//
// The table is a ladder: each security level adds requests to the ones the
// level below already covers, and the client must sign exactly what the server
// asked for. Signing too little is rejected by the server; signing too much
// costs a kXR_sigver frame per request and, for kXR_write, hashes a payload
// the server does not hash back.

package signing_test

import (
	"fmt"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/chmod"
	"go-hep.org/x/hep/xrootd/xrdproto/dirlist"
	"go-hep.org/x/hep/xrootd/xrdproto/mkdir"
	"go-hep.org/x/hep/xrootd/xrdproto/mv"
	"go-hep.org/x/hep/xrootd/xrdproto/open"
	"go-hep.org/x/hep/xrootd/xrdproto/pgread"
	"go-hep.org/x/hep/xrootd/xrdproto/pgwrite"
	"go-hep.org/x/hep/xrootd/xrdproto/ping"
	"go-hep.org/x/hep/xrootd/xrdproto/read"
	"go-hep.org/x/hep/xrootd/xrdproto/readv"
	"go-hep.org/x/hep/xrootd/xrdproto/rm"
	"go-hep.org/x/hep/xrootd/xrdproto/rmdir"
	"go-hep.org/x/hep/xrootd/xrdproto/signing"
	"go-hep.org/x/hep/xrootd/xrdproto/stat"
	"go-hep.org/x/hep/xrootd/xrdproto/statx"
	"go-hep.org/x/hep/xrootd/xrdproto/sync"
	"go-hep.org/x/hep/xrootd/xrdproto/truncate"
	"go-hep.org/x/hep/xrootd/xrdproto/write"
	"go-hep.org/x/hep/xrootd/xrdproto/writev"
	"go-hep.org/x/hep/xrootd/xrdproto/xrdclose"
)

var (
	signHandle = xrdfs.FileHandle{1, 2, 3, 4}
	signPath   = "/tmp/file.dat"
)

// signCases enumerates one request of every type the requirements table can
// name, plus a few that it never names, and the lowest security level at which
// each must be signed. A level of 0 means "never".
func signCases() []struct {
	name string
	req  xrdproto.Request
	from xrdproto.SecurityLevel
} {
	return []struct {
		name string
		req  xrdproto.Request
		from xrdproto.SecurityLevel
	}{
		// Destructive: signed from the lowest non-zero level up.
		{"chmod", &chmod.Request{Mode: 0755, Path: signPath}, xrdproto.Compatible},
		{"mv", &mv.Request{OldPath: signPath, NewPath: signPath + ".bak"}, xrdproto.Compatible},
		{"rm", &rm.Request{Path: signPath}, xrdproto.Compatible},
		{"rmdir", &rmdir.Request{Path: signPath}, xrdproto.Compatible},
		{"truncate", &truncate.Request{Handle: signHandle, Size: 0}, xrdproto.Compatible},

		// open is the only request whose requirement depends on its own
		// options: at Compatible it is SignLikely, so only a modifying open
		// is signed; from Standard every open is.
		{"open-for-write", open.NewRequest(signPath, 0644, xrdfs.OpenOptionsOpenUpdate), xrdproto.Compatible},
		{"open-for-read", open.NewRequest(signPath, 0644, xrdfs.OpenOptionsOpenRead), xrdproto.Standard},
		{"mkdir", &mkdir.Request{Mode: 0755, Path: signPath}, xrdproto.Standard},

		// Data modification.
		{"write", &write.Request{Handle: signHandle, Offset: 0, Data: []byte("x")}, xrdproto.Intense},
		{"pgwrite", &pgwrite.Request{Handle: signHandle, Offset: 0, Data: []byte("x")}, xrdproto.Intense},
		{"close", &xrdclose.Request{Handle: signHandle}, xrdproto.Intense},

		// Metadata disclosure.
		{"dirlist", &dirlist.Request{Path: signPath}, xrdproto.Pedantic},
		{"read", &read.Request{Handle: signHandle, Length: 1}, xrdproto.Pedantic},
		{"pgread", &pgread.Request{Handle: signHandle, ReadLength: 1}, xrdproto.Pedantic},
		{"stat", &stat.Request{Path: signPath}, xrdproto.Pedantic},
		{"statx", statx.NewRequest([]string{signPath}), xrdproto.Pedantic},
		{"sync", &sync.Request{Handle: signHandle}, xrdproto.Pedantic},

		// Never in the table: ping carries nothing worth signing, and the
		// vector requests are absent by construction — a kXR_sigver hash
		// covers the frame and its declared payload, and a vector write's
		// data is in neither.
		{"ping", &ping.Request{}, 0},
		{"readv", &readv.Request{Segments: []readv.Segment{{Handle: signHandle, Length: 1}}}, 0},
		{"writev", &writev.Request{Segments: []writev.Segment{{Handle: signHandle, Data: []byte("x")}}}, 0},
	}
}

var signLevels = []struct {
	name  string
	level xrdproto.SecurityLevel
}{
	{"none", xrdproto.NoneLevel},
	{"compatible", xrdproto.Compatible},
	{"standard", xrdproto.Standard},
	{"intense", xrdproto.Intense},
	{"pedantic", xrdproto.Pedantic},
}

// TestRequirementsLadder walks every request past every security level. The
// levels are cumulative, so a request signed at one level must stay signed at
// all higher ones — a requirement written into the wrong branch of the ladder
// passes a spot check of its own level and fails here.
func TestRequirementsLadder(t *testing.T) {
	for _, lvl := range signLevels {
		reqs := signing.New(lvl.level, nil)
		for _, tc := range signCases() {
			t.Run(fmt.Sprintf("%s/%s", lvl.name, tc.name), func(t *testing.T) {
				want := tc.from != 0 && lvl.level >= tc.from
				if got := reqs.Needed(tc.req); got != want {
					t.Fatalf("Needed is %v at the %s level, want %v (signed from level %d up)",
						got, lvl.name, want, tc.from)
				}
			})
		}
	}
}

// TestDefaultSignsNothing: the default is the "none" level, which is what a
// session uses until the server's kXR_protocol reply says otherwise.
func TestDefaultSignsNothing(t *testing.T) {
	reqs := signing.Default()
	for _, tc := range signCases() {
		if reqs.Needed(tc.req) {
			t.Errorf("%s is signed at the default security level", tc.name)
		}
	}
}

// TestOpenIsSignedOnlyWhenItModifies covers the SignLikely rule at the
// Compatible level, where the requirement is delegated to the request itself.
func TestOpenIsSignedOnlyWhenItModifies(t *testing.T) {
	compatible := signing.New(xrdproto.Compatible, nil)
	standard := signing.New(xrdproto.Standard, nil)

	for _, tc := range []struct {
		name string
		opts xrdfs.OpenOptions
		want bool
	}{
		{"read", xrdfs.OpenOptionsOpenRead, false},
		{"none", xrdfs.OpenOptionsNone, false},
		{"compress", xrdfs.OpenOptionsCompress, false},
		{"refresh", xrdfs.OpenOptionsRefresh, false},
		{"delete", xrdfs.OpenOptionsDelete, true},
		{"new", xrdfs.OpenOptionsNew, true},
		{"update", xrdfs.OpenOptionsOpenUpdate, true},
		{"mkpath", xrdfs.OpenOptionsMkPath, true},
		{"read and update", xrdfs.OpenOptionsOpenRead | xrdfs.OpenOptionsOpenUpdate, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := open.NewRequest(signPath, 0644, tc.opts)
			if got := compatible.Needed(req); got != tc.want {
				t.Fatalf("at the compatible level Needed is %v, want %v", got, tc.want)
			}
			// From Standard the requirement is unconditional, so the
			// request's own opinion no longer matters.
			if !standard.Needed(req) {
				t.Fatal("at the standard level every open must be signed")
			}
		})
	}
}

// TestOverridesReplaceTheLevelRequirement checks the per-request overrides a
// server may attach to its kXR_protocol reply. The index is an offset from
// kXR_auth (3000), and an override wins over the level in both directions.
func TestOverridesReplaceTheLevelRequirement(t *testing.T) {
	const authID = 3000

	t.Run("raise", func(t *testing.T) {
		// ping is in no level's table; an override puts it in.
		reqs := signing.New(xrdproto.NoneLevel, []xrdproto.SecurityOverride{
			{RequestIndex: byte(ping.RequestID - authID), RequestLevel: xrdproto.SignNeeded},
		})
		if !reqs.Needed(&ping.Request{}) {
			t.Fatal("an override raising ping to SignNeeded was ignored")
		}
		if reqs.Needed(&rm.Request{Path: signPath}) {
			t.Fatal("an override for ping changed the requirement for rm")
		}
	})

	t.Run("lower", func(t *testing.T) {
		reqs := signing.New(xrdproto.Pedantic, []xrdproto.SecurityOverride{
			{RequestIndex: byte(rm.RequestID - authID), RequestLevel: xrdproto.SignNone},
		})
		if reqs.Needed(&rm.Request{Path: signPath}) {
			t.Fatal("an override lowering rm to SignNone was ignored")
		}
		if !reqs.Needed(&rmdir.Request{Path: signPath}) {
			t.Fatal("an override for rm changed the requirement for rmdir")
		}
	})

	t.Run("likely defers to the request", func(t *testing.T) {
		reqs := signing.New(xrdproto.NoneLevel, []xrdproto.SecurityOverride{
			{RequestIndex: byte(open.RequestID - authID), RequestLevel: xrdproto.SignLikely},
		})
		if reqs.Needed(open.NewRequest(signPath, 0644, xrdfs.OpenOptionsOpenRead)) {
			t.Fatal("a read-only open was signed under a SignLikely override")
		}
		if !reqs.Needed(open.NewRequest(signPath, 0644, xrdfs.OpenOptionsDelete)) {
			t.Fatal("a destructive open was not signed under a SignLikely override")
		}
	})
}
