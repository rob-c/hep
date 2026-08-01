// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the behaviour the request and response interfaces promise.
//
// The wire shape of every request is pinned in frame_conformance_test.go. What
// this file pins is the other half: the small methods a request answers off the
// wire, which the session layer relies on to decide where a request goes, who
// signs it, and which connection carries its data. Each of them is a one-liner
// per request package, so nothing but a table over every implementation checks
// that they all agree: a request that answers the wrong path field, the wrong
// direction, or the wrong signing intent still marshals perfectly and only
// fails against a real server.

package xrdproto_test

import (
	"bytes"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/admin"
	"go-hep.org/x/hep/xrootd/xrdproto/auth"
	"go-hep.org/x/hep/xrootd/xrdproto/bind"
	"go-hep.org/x/hep/xrootd/xrdproto/chmod"
	"go-hep.org/x/hep/xrootd/xrdproto/decrypt"
	"go-hep.org/x/hep/xrootd/xrdproto/dirlist"
	"go-hep.org/x/hep/xrootd/xrdproto/endsess"
	"go-hep.org/x/hep/xrootd/xrdproto/fattr"
	"go-hep.org/x/hep/xrootd/xrdproto/locate"
	"go-hep.org/x/hep/xrootd/xrdproto/login"
	"go-hep.org/x/hep/xrootd/xrdproto/mkdir"
	"go-hep.org/x/hep/xrootd/xrdproto/mv"
	"go-hep.org/x/hep/xrootd/xrdproto/open"
	"go-hep.org/x/hep/xrootd/xrdproto/pgread"
	"go-hep.org/x/hep/xrootd/xrdproto/pgwrite"
	"go-hep.org/x/hep/xrootd/xrdproto/ping"
	"go-hep.org/x/hep/xrootd/xrdproto/prepare"
	"go-hep.org/x/hep/xrootd/xrdproto/protocol"
	"go-hep.org/x/hep/xrootd/xrdproto/query"
	"go-hep.org/x/hep/xrootd/xrdproto/read"
	"go-hep.org/x/hep/xrootd/xrdproto/readv"
	"go-hep.org/x/hep/xrootd/xrdproto/rm"
	"go-hep.org/x/hep/xrootd/xrdproto/rmdir"
	"go-hep.org/x/hep/xrootd/xrdproto/signing"
	"go-hep.org/x/hep/xrootd/xrdproto/sigver"
	"go-hep.org/x/hep/xrootd/xrdproto/stat"
	"go-hep.org/x/hep/xrootd/xrdproto/statx"
	"go-hep.org/x/hep/xrootd/xrdproto/sync"
	"go-hep.org/x/hep/xrootd/xrdproto/truncate"
	"go-hep.org/x/hep/xrootd/xrdproto/verifyw"
	"go-hep.org/x/hep/xrootd/xrdproto/write"
	"go-hep.org/x/hep/xrootd/xrdproto/writev"
	"go-hep.org/x/hep/xrootd/xrdproto/xrdclose"
)

// confSignCase is one request type and the signing intent it reports before a
// security level has had a say. A request answers true only when the operation
// it carries can change what is stored; the level table in package signing then
// decides whether that answer is consulted at all.
type confSignCase struct {
	name string
	req  xrdproto.Request
	want bool
}

func confSignCases() []confSignCase {
	return []confSignCase{
		{"admin", &admin.Request{}, false},
		{"auth", &auth.Request{}, false},
		{"bind", &bind.Request{}, false},
		{"chmod", &chmod.Request{}, false},
		{"decrypt", &decrypt.Request{}, false},
		{"dirlist", &dirlist.Request{}, false},
		{"endsess", &endsess.Request{}, false},
		{"fattr", &fattr.Request{}, true},
		{"locate", &locate.Request{}, false},
		{"login", &login.Request{}, false},
		{"mkdir", &mkdir.Request{}, false},
		{"mv", &mv.Request{}, false},
		{"open", &open.Request{}, false},
		{"pgread", &pgread.Request{}, false},
		{"pgwrite", &pgwrite.Request{}, true},
		{"ping", &ping.Request{}, false},
		{"prepare", &prepare.Request{}, false},
		{"protocol", &protocol.Request{}, false},
		{"query", &query.Request{}, false},
		{"read", &read.Request{}, false},
		{"readv", &readv.Request{}, false},
		{"rm", &rm.Request{}, false},
		{"rmdir", &rmdir.Request{}, false},
		{"sigver", func() *sigver.Request { r := sigver.NewRequest(open.RequestID, 1, nil); return &r }(), false},
		{"stat", &stat.Request{}, false},
		{"statx", &statx.Request{}, false},
		{"sync", &sync.Request{}, false},
		{"truncate", &truncate.Request{}, false},
		{"verifyw", &verifyw.Request{}, false},
		{"write", &write.Request{}, false},
		{"writev", &writev.Request{}, false},
		{"xrdclose", &xrdclose.Request{}, false},
	}
}

func TestConformance_ARequestReportsWhetherItAsksToBeSigned(t *testing.T) {
	for _, tc := range confSignCases() {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.req.ShouldSign(); got != tc.want {
				t.Fatalf("%s.ShouldSign() = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestConformance_AnOpenAsksToBeSignedWhenItCanChangeTheFile(t *testing.T) {
	// kXR_open is the one request whose signing intent depends on its own
	// arguments: opening for reading changes nothing, while any option that
	// can create, replace or extend the file does.
	for _, tc := range []struct {
		name string
		opts xrdfs.OpenOptions
		want bool
	}{
		{"none", xrdfs.OpenOptionsNone, false},
		{"read", xrdfs.OpenOptionsOpenRead, false},
		{"compress", xrdfs.OpenOptionsCompress, false},
		{"force", xrdfs.OpenOptionsForce, false},
		{"async", xrdfs.OpenOptionsAsync, false},
		{"refresh", xrdfs.OpenOptionsRefresh, false},
		{"return-status", xrdfs.OpenOptionsReturnStatus, false},
		{"posc", xrdfs.OpenOptionsPOSC, false},
		{"delete", xrdfs.OpenOptionsDelete, true},
		{"new", xrdfs.OpenOptionsNew, true},
		{"update", xrdfs.OpenOptionsOpenUpdate, true},
		{"mkpath", xrdfs.OpenOptionsMkPath, true},
		{"append", xrdfs.OpenOptionsOpenAppend, true},
		{"read-and-mkpath", xrdfs.OpenOptionsOpenRead | xrdfs.OpenOptionsMkPath, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := &open.Request{Options: tc.opts, Path: "/tmp/file"}
			if got := req.ShouldSign(); got != tc.want {
				t.Fatalf("open{Options: %#04x}.ShouldSign() = %v, want %v", uint16(tc.opts), got, tc.want)
			}
		})
	}
}

func TestConformance_AConditionalIntentIsOnlyConsultedWhereTheLevelSaysLikely(t *testing.T) {
	// ShouldSign is a request's own opinion; Requirements.Needed is the
	// decision. The two meet only where a level maps the request to
	// SignLikely, so a request that answers true is not thereby signed and a
	// request that answers false is not thereby exempt.
	levels := []xrdproto.SecurityLevel{
		xrdproto.NoneLevel, xrdproto.Compatible, xrdproto.Standard,
		xrdproto.Intense, xrdproto.Pedantic,
	}

	// fattr answers true, and is in no level's table: nothing ever asks.
	for _, level := range levels {
		reqs := signing.New(level, nil)
		if got := reqs.Needed(&fattr.Request{}); got {
			t.Fatalf("level %v: fattr is signed, but no level lists it", level)
		}
	}

	// chmod answers false, and is SignNeeded from Compatible upwards: the
	// level overrules the request.
	for _, level := range levels {
		reqs := signing.New(level, nil)
		want := level >= xrdproto.Compatible
		if got := reqs.Needed(&chmod.Request{}); got != want {
			t.Fatalf("level %v: chmod signed = %v, want %v", level, got, want)
		}
	}

	// open is the only SignLikely entry, at Compatible alone: there and only
	// there does its own answer decide.
	openRead := &open.Request{Options: xrdfs.OpenOptionsOpenRead}
	openWrite := &open.Request{Options: xrdfs.OpenOptionsOpenUpdate}
	for _, tc := range []struct {
		level             xrdproto.SecurityLevel
		wantRead, wantWri bool
	}{
		{xrdproto.NoneLevel, false, false},
		{xrdproto.Compatible, false, true},
		{xrdproto.Standard, true, true},
		{xrdproto.Intense, true, true},
		{xrdproto.Pedantic, true, true},
	} {
		reqs := signing.New(tc.level, nil)
		if got := reqs.Needed(openRead); got != tc.wantRead {
			t.Fatalf("level %v: open for reading signed = %v, want %v", tc.level, got, tc.wantRead)
		}
		if got := reqs.Needed(openWrite); got != tc.wantWri {
			t.Fatalf("level %v: open for writing signed = %v, want %v", tc.level, got, tc.wantWri)
		}
	}
}

// confRespCase is one response type and the request id it replays to. A
// response frame carries only a stream id, so the session matches it to the
// request it belongs to and then trusts RespID to say what was decoded.
type confRespCase struct {
	name string
	resp xrdproto.Response
	id   uint16
}

func confRespCases() []confRespCase {
	return []confRespCase{
		{"admin", &admin.Response{}, admin.RequestID},
		{"bind", &bind.Response{}, bind.RequestID},
		{"dirlist", &dirlist.Response{}, dirlist.RequestID},
		{"fattr", &fattr.Response{}, fattr.RequestID},
		{"locate", &locate.Response{}, locate.RequestID},
		{"login", &login.Response{}, login.RequestID},
		{"open", &open.Response{}, open.RequestID},
		{"pgread", &pgread.Response{}, pgread.RequestID},
		{"pgwrite", &pgwrite.Response{}, pgwrite.RequestID},
		{"prepare", &prepare.Response{}, prepare.RequestID},
		{"protocol", &protocol.Response{}, protocol.RequestID},
		{"query", &query.Response{}, query.RequestID},
		{"read", &read.Response{}, read.RequestID},
		{"readv", &readv.Response{}, readv.RequestID},
		{"stat-default", &stat.DefaultResponse{}, stat.RequestID},
		{"stat-virtualfs", &stat.VirtualFSResponse{}, stat.RequestID},
		{"statx", &statx.Response{}, statx.RequestID},
	}
}

func TestConformance_AResponseNamesTheRequestItReplaysTo(t *testing.T) {
	for _, tc := range confRespCases() {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.resp.RespID(); got != tc.id {
				t.Fatalf("%s.RespID() = %d, want %d", tc.name, got, tc.id)
			}
		})
	}
}

func TestConformance_BothStatRepliesNameTheSameRequest(t *testing.T) {
	// kXR_stat answers with one of two bodies depending on whether the
	// request asked about a file or the virtual filesystem. A caller that
	// picked the wrong one would still be told the reply is a stat, so the
	// two must not disagree about that much.
	dflt := (&stat.DefaultResponse{}).RespID()
	vfs := (&stat.VirtualFSResponse{}).RespID()
	if dflt != vfs {
		t.Fatalf("stat replies disagree: default=%d virtualfs=%d", dflt, vfs)
	}
}

// confOpaqueCase is one request that carries a path, and so must let a caller
// read and replace the CGI travelling with it. The session appends
// authorization data this way just before a request goes out.
type confOpaqueCase struct {
	name string

	// make builds the request with path in the field the opaque data lives on.
	make func(path string) xrdproto.FilepathRequest

	// field reads that same field back, so a request that reads one path and
	// writes another is caught.
	field func(xrdproto.FilepathRequest) string
}

func confOpaqueCases() []confOpaqueCase {
	return []confOpaqueCase{
		{
			name:  "chmod",
			make:  func(p string) xrdproto.FilepathRequest { return &chmod.Request{Mode: 0755, Path: p} },
			field: func(r xrdproto.FilepathRequest) string { return r.(*chmod.Request).Path },
		},
		{
			name:  "dirlist",
			make:  func(p string) xrdproto.FilepathRequest { return &dirlist.Request{Path: p} },
			field: func(r xrdproto.FilepathRequest) string { return r.(*dirlist.Request).Path },
		},
		{
			name:  "mkdir",
			make:  func(p string) xrdproto.FilepathRequest { return &mkdir.Request{Mode: 0755, Path: p} },
			field: func(r xrdproto.FilepathRequest) string { return r.(*mkdir.Request).Path },
		},
		{
			// kXR_mv carries two paths, and the opaque data belongs to the
			// destination: the authorization the server needs is for where
			// the file is going.
			name:  "mv",
			make:  func(p string) xrdproto.FilepathRequest { return &mv.Request{OldPath: "/tmp/old", NewPath: p} },
			field: func(r xrdproto.FilepathRequest) string { return r.(*mv.Request).NewPath },
		},
		{
			name:  "open",
			make:  func(p string) xrdproto.FilepathRequest { return &open.Request{Path: p} },
			field: func(r xrdproto.FilepathRequest) string { return r.(*open.Request).Path },
		},
		{
			name:  "rm",
			make:  func(p string) xrdproto.FilepathRequest { return &rm.Request{Path: p} },
			field: func(r xrdproto.FilepathRequest) string { return r.(*rm.Request).Path },
		},
		{
			name:  "rmdir",
			make:  func(p string) xrdproto.FilepathRequest { return &rmdir.Request{Path: p} },
			field: func(r xrdproto.FilepathRequest) string { return r.(*rmdir.Request).Path },
		},
		{
			name:  "stat",
			make:  func(p string) xrdproto.FilepathRequest { return &stat.Request{Path: p} },
			field: func(r xrdproto.FilepathRequest) string { return r.(*stat.Request).Path },
		},
		{
			name:  "truncate",
			make:  func(p string) xrdproto.FilepathRequest { return &truncate.Request{Size: 1, Path: p} },
			field: func(r xrdproto.FilepathRequest) string { return r.(*truncate.Request).Path },
		},
	}
}

func TestConformance_OpaqueDataRoundTripsThroughEveryPathRequest(t *testing.T) {
	const path = "/tmp/dir/file.dat"
	for _, tc := range confOpaqueCases() {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.make(path)
			if got := req.Opaque(); got != "" {
				t.Fatalf("a fresh %s already carries opaque data %q", tc.name, got)
			}

			req.SetOpaque("authz=first")
			if got, want := req.Opaque(), "authz=first"; got != want {
				t.Fatalf("%s.Opaque() = %q, want %q", tc.name, got, want)
			}
			if got, want := tc.field(req), path+"?authz=first"; got != want {
				t.Fatalf("%s path = %q, want %q", tc.name, got, want)
			}

			// A second call replaces the first: opaque data is a whole
			// value, not a list to append to, and a session that re-signs a
			// retried request must not stack two tokens on one path.
			req.SetOpaque("authz=second")
			if got, want := req.Opaque(), "authz=second"; got != want {
				t.Fatalf("%s.Opaque() after replacement = %q, want %q", tc.name, got, want)
			}
			if got, want := tc.field(req), path+"?authz=second"; got != want {
				t.Fatalf("%s path after replacement = %q, want %q", tc.name, got, want)
			}
			if strings.Count(tc.field(req), "?") != 1 {
				t.Fatalf("%s path %q accumulated separators", tc.name, tc.field(req))
			}
		})
	}
}

func TestConformance_AMoveLeavesItsSourcePathAlone(t *testing.T) {
	req := &mv.Request{OldPath: "/tmp/old", NewPath: "/tmp/new"}
	req.SetOpaque("authz=x")
	if got, want := req.OldPath, "/tmp/old"; got != want {
		t.Fatalf("mv source path = %q, want %q", got, want)
	}
}

func TestConformance_ATruncateOnAnOpenFileCarriesNoOpaqueData(t *testing.T) {
	// kXR_truncate reaches a file either by path or by an open handle. There
	// is nothing to authorize a second time on the handle form, and appending
	// a "?" to an empty path would turn it into a path.
	req := &truncate.Request{Handle: xrdfs.FileHandle{1, 2, 3, 4}, Size: 8}
	req.SetOpaque("authz=x")
	if got := req.Path; got != "" {
		t.Fatalf("truncate by handle grew a path %q", got)
	}
	if got := req.Opaque(); got != "" {
		t.Fatalf("truncate by handle reports opaque data %q", got)
	}
}

func TestConformance_ADataRequestReportsWhichWayItsDataTravels(t *testing.T) {
	// A bound path carries the bulk data of a request on a second connection.
	// Which connection reads and which writes follows from the direction, so
	// the two data requests must not agree about it.
	rd := &read.Request{Handle: xrdfs.FileHandle{1}, Length: 8}
	wr := &write.Request{Handle: xrdfs.FileHandle{1}, Data: []byte("go-hep")}

	if got, want := rd.Direction(), xrdproto.DataRequestRead; got != want {
		t.Fatalf("read direction = %v, want %v", got, want)
	}
	if got, want := wr.Direction(), xrdproto.DataRequestWrite; got != want {
		t.Fatalf("write direction = %v, want %v", got, want)
	}
}

func TestConformance_ADataRequestStartsOnTheMainConnection(t *testing.T) {
	// Path id 0 is the connection the request itself went out on. Every data
	// request must start there, so that a client which never binds a second
	// connection still works.
	for _, tc := range []struct {
		name string
		req  xrdproto.DataRequest
	}{
		{"read", &read.Request{Handle: xrdfs.FileHandle{1}, Length: 8}},
		{"write", &write.Request{Handle: xrdfs.FileHandle{1}, Data: []byte("go-hep")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.req.PathID(); got != 0 {
				t.Fatalf("a fresh %s already names path %d", tc.name, got)
			}
			if got := tc.req.PathData(); got != nil {
				t.Fatalf("%s on the main connection sets data aside: %q", tc.name, got)
			}

			tc.req.SetPathID(3)
			if got := tc.req.PathID(); got != 3 {
				t.Fatalf("%s.PathID() = %d after binding, want 3", tc.name, got)
			}

			tc.req.SetPathID(4)
			if got := tc.req.PathID(); got != 4 {
				t.Fatalf("%s.PathID() = %d after rebinding, want 4", tc.name, got)
			}
		})
	}
}

func TestConformance_OnlyAWriteHandsOverItsPayload(t *testing.T) {
	// PathData is the data the caller must push down the bound connection
	// itself. A read has none: the payload comes back from the server. A
	// write has its buffer, but only once a path is bound, because otherwise
	// the buffer is marshaled into the request frame instead.
	rd := &read.Request{Handle: xrdfs.FileHandle{1}, Length: 8}
	rd.SetPathID(2)
	if got := rd.PathData(); got != nil {
		t.Fatalf("read hands over %q, want nothing", got)
	}

	data := []byte("go-hep")
	wr := &write.Request{Handle: xrdfs.FileHandle{1}, Data: data}
	if got := wr.PathData(); got != nil {
		t.Fatalf("unbound write hands over %q, want nothing", got)
	}
	wr.SetPathID(2)
	if got := wr.PathData(); !bytes.Equal(got, data) {
		t.Fatalf("bound write hands over %q, want %q", got, data)
	}

	// The frame and the bound connection carry the payload exclusively: what
	// PathData returns must not also be marshaled.
	var w xrdenc.WBuffer
	if err := wr.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal a bound write: %v", err)
	}
	if bytes.Contains(w.Bytes(), data) {
		t.Fatalf("a bound write marshaled its payload as well as handing it over")
	}
}

func TestConformance_ABoundReadNamesItsPathInTheOptionalArguments(t *testing.T) {
	// kXR_read has no room for a path id in its parameters: it travels in an
	// optional trailer that a plain read does not send at all. Setting a path
	// on a request that has no trailer must create one.
	req := &read.Request{Handle: xrdfs.FileHandle{1}, Length: 8}
	if req.OptionalArgs != nil {
		t.Fatal("a fresh read already carries optional arguments")
	}
	req.SetPathID(7)
	if req.OptionalArgs == nil {
		t.Fatal("setting a path on a read did not create the optional arguments")
	}
	if got := req.OptionalArgs.PathID; got != 7 {
		t.Fatalf("optional arguments name path %d, want 7", got)
	}

	// And a request that already has a trailer keeps its pre-reads.
	req.OptionalArgs.ReadAheads = []read.ReadAhead{{Handle: xrdfs.FileHandle{2}, Length: 4, Offset: 16}}
	req.SetPathID(8)
	if got := req.PathID(); got != 8 {
		t.Fatalf("read names path %d after rebinding, want 8", got)
	}
	if got := len(req.OptionalArgs.ReadAheads); got != 1 {
		t.Fatalf("rebinding dropped the pre-reads: %d left, want 1", got)
	}
}

func TestConformance_APreReadRoundTrips(t *testing.T) {
	want := read.ReadAhead{Handle: xrdfs.FileHandle{0x11, 0x22, 0x33, 0x44}, Length: 1024, Offset: 4096}

	var w xrdenc.WBuffer
	if err := want.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal a pre-read: %v", err)
	}
	// A pre-read is a fixed 16-byte record: handle, length, offset. The
	// count of them is how the reader sizes its slice, so the size is not
	// something the encoder may drift on.
	if got, want := len(w.Bytes()), 16; got != want {
		t.Fatalf("a pre-read marshaled to %d bytes, want %d", got, want)
	}

	var got read.ReadAhead
	if err := got.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
		t.Fatalf("could not unmarshal a pre-read: %v", err)
	}
	if got != want {
		t.Fatalf("pre-read round trip = %+v, want %+v", got, want)
	}
}

func TestConformance_AReadCarriesItsPreReadsAcrossTheWire(t *testing.T) {
	want := &read.Request{
		Handle: xrdfs.FileHandle{1, 2, 3, 4},
		Offset: 64,
		Length: 128,
		OptionalArgs: &read.OptionalArgs{
			PathID: 2,
			ReadAheads: []read.ReadAhead{
				{Handle: xrdfs.FileHandle{5}, Length: 16, Offset: 192},
				{Handle: xrdfs.FileHandle{6}, Length: 32, Offset: 256},
			},
		},
	}

	var w xrdenc.WBuffer
	if err := want.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal a read: %v", err)
	}

	var got read.Request
	if err := got.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
		t.Fatalf("could not unmarshal a read: %v", err)
	}
	if got.OptionalArgs == nil {
		t.Fatal("the optional arguments did not survive the wire")
	}
	if got.OptionalArgs.PathID != want.OptionalArgs.PathID {
		t.Fatalf("path id = %d, want %d", got.OptionalArgs.PathID, want.OptionalArgs.PathID)
	}
	if len(got.OptionalArgs.ReadAheads) != len(want.OptionalArgs.ReadAheads) {
		t.Fatalf("got %d pre-reads, want %d", len(got.OptionalArgs.ReadAheads), len(want.OptionalArgs.ReadAheads))
	}
	for i, ra := range got.OptionalArgs.ReadAheads {
		if ra != want.OptionalArgs.ReadAheads[i] {
			t.Fatalf("pre-read %d = %+v, want %+v", i, ra, want.OptionalArgs.ReadAheads[i])
		}
	}
}

func TestConformance_AnOptionalTrailerIsRejectedUnlessItsLengthFits(t *testing.T) {
	// alen comes off the wire and sizes an allocation, so it is checked
	// against what is actually there: 8 bytes of header plus a whole number
	// of 16-byte pre-reads, none of which may be missing.
	trailer := func(alen int32, tail []byte) []byte {
		var w xrdenc.WBuffer
		w.WriteBytes(make([]byte, 8)) // handle
		w.WriteI64(0)                 // offset
		w.WriteI32(0)                 // length
		w.WriteLen(int(alen))
		w.WriteU8(0) // path id
		w.Next(7)    // reserved
		w.WriteBytes(tail)
		return w.Bytes()
	}

	for _, tc := range []struct {
		name  string
		frame []byte
	}{
		{"too-short", trailer(4, nil)},
		{"not-a-whole-number-of-pre-reads", trailer(20, make([]byte, 12))},
		{"more-pre-reads-than-bytes", trailer(8+16*1024, make([]byte, 16))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var req read.Request
			if err := req.UnmarshalXrd(xrdenc.NewRBuffer(tc.frame)); err == nil {
				t.Fatal("a malformed optional trailer was accepted")
			}
		})
	}
}

func TestConformance_AServerRoleIsReadOneBitAtATime(t *testing.T) {
	// The protocol reply packs the server's roles and attributes into one
	// word. A predicate that masked the wrong bit would still answer
	// correctly for a server that happens to have both, so each is asked
	// against a reply that has that bit and nothing else.
	type roles struct{ server, manager, meta, proxy, supervisor bool }
	ask := func(flags protocol.Flags) roles {
		resp := &protocol.Response{Flags: flags}
		return roles{resp.IsServer(), resp.IsManager(), resp.IsMeta(), resp.IsProxy(), resp.IsSupervisor()}
	}

	for _, tc := range []struct {
		name  string
		flags protocol.Flags
		want  roles
	}{
		{"none", 0, roles{}},
		{"server", protocol.IsServer, roles{server: true}},
		{"manager", protocol.IsManager, roles{manager: true}},
		{"meta", protocol.IsMeta, roles{meta: true}},
		{"proxy", protocol.IsProxy, roles{proxy: true}},
		{"supervisor", protocol.IsSupervisor, roles{supervisor: true}},
		{
			// A meta-manager is the combination a redirector reports.
			name:  "meta-manager",
			flags: protocol.IsManager | protocol.IsMeta,
			want:  roles{manager: true, meta: true},
		},
		{
			name:  "proxy-server",
			flags: protocol.IsServer | protocol.IsProxy,
			want:  roles{server: true, proxy: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ask(tc.flags); got != tc.want {
				t.Fatalf("flags %#08x read as %+v, want %+v", int32(tc.flags), got, tc.want)
			}
		})
	}
}

func TestConformance_ForcedSigningIsNotAServerRole(t *testing.T) {
	// kXR_secOFrce arrives in the security options byte, not in the flags
	// word, and the two are adjacent in the reply. A predicate that read the
	// flags would find nothing there and quietly stop signing.
	resp := &protocol.Response{Flags: protocol.Flags(protocol.ForceSecurity)}
	if resp.ForceSecurity() {
		t.Fatal("forced signing was read out of the flags word")
	}

	resp = &protocol.Response{SecurityOptions: protocol.ForceSecurity}
	if !resp.ForceSecurity() {
		t.Fatal("forced signing was not read out of the security options")
	}
	if resp.IsServer() || resp.IsManager() {
		t.Fatal("the security options byte leaked into the role flags")
	}
}

func TestConformance_ASecurityOptionSurvivesTheWire(t *testing.T) {
	want := protocol.Response{
		BinaryProtocolVersion: 0x310,
		Flags:                 protocol.IsServer | protocol.IsProxy,
		HasSecurityInfo:       true,
		SecurityVersion:       1,
		SecurityOptions:       protocol.ForceSecurity,
		SecurityLevel:         xrdproto.Pedantic,
		SecurityOverrides:     []xrdproto.SecurityOverride{{RequestIndex: 13, RequestLevel: xrdproto.SignNone}},
	}

	var w xrdenc.WBuffer
	if err := want.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal a protocol reply: %v", err)
	}

	var got protocol.Response
	if err := got.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
		t.Fatalf("could not unmarshal a protocol reply: %v", err)
	}
	if !got.ForceSecurity() {
		t.Fatal("forced signing did not survive the wire")
	}
	if !got.IsServer() || !got.IsProxy() || got.IsManager() {
		t.Fatalf("roles did not survive the wire: %#08x", int32(got.Flags))
	}
	if got.SecurityLevel != want.SecurityLevel {
		t.Fatalf("security level = %v, want %v", got.SecurityLevel, want.SecurityLevel)
	}
	if len(got.SecurityOverrides) != 1 || got.SecurityOverrides[0] != want.SecurityOverrides[0] {
		t.Fatalf("security overrides = %+v, want %+v", got.SecurityOverrides, want.SecurityOverrides)
	}
}

func TestConformance_AProtocolReplyWithoutSecurityInfoSaysSo(t *testing.T) {
	// A server that answers the short form sends the version and flags and
	// stops. Nothing may be invented for the fields that did not arrive.
	want := protocol.Response{BinaryProtocolVersion: 0x310, Flags: protocol.IsServer}

	var w xrdenc.WBuffer
	if err := want.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal a protocol reply: %v", err)
	}
	if got, want := len(w.Bytes()), 8; got != want {
		t.Fatalf("the short protocol reply is %d bytes, want %d", got, want)
	}

	var got protocol.Response
	if err := got.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
		t.Fatalf("could not unmarshal a protocol reply: %v", err)
	}
	if got.HasSecurityInfo {
		t.Fatal("a reply with no security info claims to have some")
	}
	if got.ForceSecurity() || got.SecurityLevel != 0 || got.SecurityOverrides != nil {
		t.Fatalf("a reply with no security info produced requirements: %+v", got)
	}
}

func TestConformance_AStatusBodyRoundTrips(t *testing.T) {
	// kXR_status is the only reply whose fixed part is checksummed, and the
	// encoder writes the stored value as it stands so that a test can build
	// a frame with a deliberately wrong one. What must hold is that a body
	// marshaled by hand and a frame stamped by StatusFrame agree byte for
	// byte once the checksum is filled in.
	body := xrdproto.StatusBody{
		StreamID:   xrdproto.StreamID{0xab, 0xcd},
		RequestID:  uint8(pgread.RequestID - 3000),
		RespType:   xrdproto.PartialResult,
		DataLength: 4096,
	}

	var w xrdenc.WBuffer
	if err := body.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal a status body: %v", err)
	}
	if got, want := len(w.Bytes()), xrdproto.StatusBodyLength; got != want {
		t.Fatalf("a status body marshaled to %d bytes, want %d", got, want)
	}

	frame := xrdproto.StatusFrame(body, nil)
	if !bytes.Equal(frame[4:], w.Bytes()[4:]) {
		t.Fatalf("StatusFrame and MarshalXrd disagree past the checksum:\n got %x\nwant %x", frame[4:], w.Bytes()[4:])
	}

	var got xrdproto.StatusBody
	if err := got.UnmarshalVerifyXrd(frame); err != nil {
		t.Fatalf("could not decode a stamped status frame: %v", err)
	}
	body.CRC32C = got.CRC32C
	if got != body {
		t.Fatalf("status body round trip = %+v, want %+v", got, body)
	}
}

func TestConformance_AStatusBodyWithTheStoredChecksumIsNotSelfCertifying(t *testing.T) {
	// MarshalXrd writes the CRC field as it stands, so a body built by hand
	// does not verify. That is deliberate: it is what lets a test forge a
	// bad frame, and it is why servers go through StatusFrame instead.
	body := xrdproto.StatusBody{
		StreamID:  xrdproto.StreamID{1, 2},
		RequestID: uint8(pgread.RequestID - 3000),
		CRC32C:    0xdeadbeef,
	}

	var w xrdenc.WBuffer
	if err := body.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal a status body: %v", err)
	}

	var got xrdproto.StatusBody
	if err := got.UnmarshalVerifyXrd(w.Bytes()); err == nil {
		t.Fatal("a status frame with a made-up checksum was accepted")
	}
}
