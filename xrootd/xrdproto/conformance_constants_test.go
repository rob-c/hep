// Copyright ©2025 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The numbers in this file are transcribed from the XRootD protocol
// specification (http://xrootd.org/doc/dev45/XRdv310.pdf), not read back from
// the constants they check. A round-trip test cannot catch a mistyped
// constant: both ends of the round trip use it, so both ends agree and the
// wire is wrong in the same way.
//
// Request codes are pinned the same way in frame_conformance_test.go, beside
// each request's wire shape; what is left here is everything a request is not:
// the statuses and errors a server answers with, the flags and options a
// client sends, and the greeting that opens a connection.

package xrdproto_test

import (
	"bytes"
	"reflect"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/dirlist"
	"go-hep.org/x/hep/xrootd/xrdproto/fattr"
	"go-hep.org/x/hep/xrootd/xrdproto/handshake"
	"go-hep.org/x/hep/xrootd/xrdproto/protocol"
	"go-hep.org/x/hep/xrootd/xrdproto/query"
)

func TestConformance_ResponseStatusesMatchTheSpecification(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  xrdproto.ResponseStatus
		want uint16
	}{
		{"kXR_ok", xrdproto.Ok, 0},
		{"kXR_oksofar", xrdproto.OkSoFar, 4000},
		{"kXR_attn", xrdproto.Attn, 4001},
		{"kXR_authmore", xrdproto.AuthMore, 4002},
		{"kXR_error", xrdproto.Error, 4003},
		{"kXR_redirect", xrdproto.Redirect, 4004},
		{"kXR_wait", xrdproto.Wait, 4005},
		{"kXR_waitresp", xrdproto.WaitResp, 4006},
		{"kXR_status", xrdproto.Status, 4007},
	} {
		if uint16(tc.got) != tc.want {
			t.Errorf("%s is %d, want %d", tc.name, tc.got, tc.want)
		}
	}

	// kXR_asyncresp is not a status but the action code that marks an Attn
	// body as carrying a deferred response for another stream.
	if xrdproto.AsyncResp != 5008 {
		t.Errorf("kXR_asyncresp is %d, want 5008", xrdproto.AsyncResp)
	}
}

func TestConformance_ServerErrorCodesMatchTheSpecification(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  xrdproto.ServerErrorCode
		want int32
	}{
		{"kXR_InvalidRequest", xrdproto.InvalidRequest, 3006},
		{"kXR_IOError", xrdproto.IOError, 3007},
		{"kXR_NotAuthorized", xrdproto.NotAuthorized, 3010},
		{"kXR_NotFound", xrdproto.NotFound, 3011},
	} {
		if int32(tc.got) != tc.want {
			t.Errorf("%s is %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

// TestConformance_HeaderLengthsMatchTheSpecification pins the two framing
// constants everything else is read against: a 4-byte request header (stream
// id, request code) and an 8-byte response header (stream id, status, data
// length). Both are also checked against what a marshalled header actually
// occupies, since a constant that no longer describes the struct beside it
// desynchronizes every read.
func TestConformance_HeaderLengthsMatchTheSpecification(t *testing.T) {
	if xrdproto.RequestHeaderLength != 4 {
		t.Errorf("request header is %d bytes, want 4", xrdproto.RequestHeaderLength)
	}
	if xrdproto.ResponseHeaderLength != 8 {
		t.Errorf("response header is %d bytes, want 8", xrdproto.ResponseHeaderLength)
	}

	var buf bytes.Buffer
	err := xrdproto.WriteResponse(&buf, xrdproto.StreamID{0x01, 0x02}, xrdproto.Ok, nil)
	if err != nil {
		t.Fatalf("could not write response: %v", err)
	}
	if got, want := buf.Len(), xrdproto.ResponseHeaderLength; got != want {
		t.Errorf("an empty response occupies %d bytes, want %d", got, want)
	}
}

// TestConformance_HandshakeIsTheProtocolsFirstTwentyBytes pins the bytes that
// open every connection. They are not a request — a server matches them
// literally before any protocol version has been agreed — so nothing about
// them can be renegotiated, and a single wrong byte is a connection that never
// gets past hello.
func TestConformance_HandshakeIsTheProtocolsFirstTwentyBytes(t *testing.T) {
	req := handshake.NewRequest()
	if got, want := req, (handshake.Request{0, 0, 0, 4, 2012}); got != want {
		t.Fatalf("handshake is %v, want %v", got, want)
	}

	var w xrdenc.WBuffer
	if err := req.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal handshake: %v", err)
	}
	if got, want := len(w.Bytes()), handshake.RequestLength; got != want {
		t.Fatalf("handshake marshals to %d bytes, want %d", got, want)
	}
	want := []byte{
		0, 0, 0, 0,
		0, 0, 0, 0,
		0, 0, 0, 0,
		0, 0, 0, 4,
		0, 0, 7, 0xdc, // 2012
	}
	if got := w.Bytes(); !reflect.DeepEqual(got, want) {
		t.Errorf("handshake bytes are %v, want %v", got, want)
	}
}

// TestConformance_OpenModeBitsMatchPOSIX checks the mode bits a client sends
// with open, mkdir and chmod. They are the POSIX permission bits, and a
// mismatch is not a protocol error the server can report: it silently creates
// files with the wrong permissions.
func TestConformance_OpenModeBitsMatchPOSIX(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  xrdfs.OpenMode
		want uint16
	}{
		{"kXR_ur", xrdfs.OpenModeOwnerRead, 0o400},
		{"kXR_uw", xrdfs.OpenModeOwnerWrite, 0o200},
		{"kXR_ux", xrdfs.OpenModeOwnerExecute, 0o100},
		{"kXR_gr", xrdfs.OpenModeGroupRead, 0o040},
		{"kXR_gw", xrdfs.OpenModeGroupWrite, 0o020},
		{"kXR_gx", xrdfs.OpenModeGroupExecute, 0o010},
		{"kXR_or", xrdfs.OpenModeOtherRead, 0o004},
		{"kXR_ow", xrdfs.OpenModeOtherWrite, 0o002},
		{"kXR_ox", xrdfs.OpenModeOtherExecute, 0o001},
	} {
		if uint16(tc.got) != tc.want {
			t.Errorf("%s is %#o, want %#o", tc.name, tc.got, tc.want)
		}
	}
}

func TestConformance_OpenOptionBitsMatchTheSpecification(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  xrdfs.OpenOptions
		want uint16
	}{
		{"kXR_none", xrdfs.OpenOptionsNone, 0},
		{"kXR_compress", xrdfs.OpenOptionsCompress, 1},
		{"kXR_delete", xrdfs.OpenOptionsDelete, 2},
		{"kXR_force", xrdfs.OpenOptionsForce, 4},
		{"kXR_new", xrdfs.OpenOptionsNew, 8},
		{"kXR_open_read", xrdfs.OpenOptionsOpenRead, 16},
		{"kXR_open_updt", xrdfs.OpenOptionsOpenUpdate, 32},
		{"kXR_async", xrdfs.OpenOptionsAsync, 64},
		{"kXR_refresh", xrdfs.OpenOptionsRefresh, 128},
		{"kXR_mkpath", xrdfs.OpenOptionsMkPath, 256},
		{"kXR_open_apnd", xrdfs.OpenOptionsOpenAppend, 512},
		{"kXR_retstat", xrdfs.OpenOptionsReturnStatus, 1024},
		{"kXR_replica", xrdfs.OpenOptionsReplica, 2048},
		{"kXR_posc", xrdfs.OpenOptionsPOSC, 4096},
		{"kXR_nowait", xrdfs.OpenOptionsNoWait, 8192},
		{"kXR_seqio", xrdfs.OpenOptionsSequentiallyIO, 16384},
	} {
		if uint16(tc.got) != tc.want {
			t.Errorf("%s is %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

func TestConformance_StatFlagBitsMatchTheSpecification(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  xrdfs.StatFlags
		want int32
	}{
		{"kXR_file", xrdfs.StatIsFile, 0},
		{"kXR_xset", xrdfs.StatIsExecutable, 1},
		{"kXR_isDir", xrdfs.StatIsDir, 2},
		{"kXR_other", xrdfs.StatIsOther, 4},
		{"kXR_offline", xrdfs.StatIsOffline, 8},
		{"kXR_readable", xrdfs.StatIsReadable, 16},
		{"kXR_writable", xrdfs.StatIsWritable, 32},
		{"kXR_poscpend", xrdfs.StatIsPOSCPending, 64},
	} {
		if int32(tc.got) != tc.want {
			t.Errorf("%s is %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

func TestConformance_QueryCodesMatchTheSpecification(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  int
		want int
	}{
		{"kXR_Qstats", query.Stats, 1},
		{"kXR_Qprep", query.Prepare, 2},
		{"kXR_Qcksum", query.Checksum, 3},
		{"kXR_Qxattr", query.XAttr, 4},
		{"kXR_Qspace", query.Space, 5},
		{"kXR_Qckscan", query.CancelChecksum, 6},
		{"kXR_Qconfig", query.Config, 7},
		{"kXR_Qvisa", query.Visa, 8},
		{"kXR_Qopaque", query.Opaque1, 16},
		{"kXR_Qopaquf", query.Opaque2, 32},
		{"kXR_Qopaqug", query.Opaque3, 64},
	} {
		if tc.got != tc.want {
			t.Errorf("%s is %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

func TestConformance_DirlistOptionsMatchTheSpecification(t *testing.T) {
	if got, want := dirlist.None, dirlist.RequestOptions(0); got != want {
		t.Errorf("kXR_dirlist none is %d, want %d", got, want)
	}
	if got, want := dirlist.WithStatInfo, dirlist.RequestOptions(2); got != want {
		t.Errorf("kXR_dstat is %d, want %d", got, want)
	}
}

func TestConformance_FattrSubcodesMatchTheSpecification(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  uint8
		want uint8
	}{
		{"kXR_fattrDel", fattr.Del, 0},
		{"kXR_fattrGet", fattr.Get, 1},
		{"kXR_fattrList", fattr.List, 2},
		{"kXR_fattrSet", fattr.Set, 3},
		{"kXR_fattrIsNew", fattr.IsNew, 0x01},
		{"kXR_fattrAData", fattr.AData, 0x10},
	} {
		if tc.got != tc.want {
			t.Errorf("%s is %d, want %d", tc.name, tc.got, tc.want)
		}
	}
	if fattr.MaxVars != 16 {
		t.Errorf("kXR_faLimits maxvars is %d, want 16", fattr.MaxVars)
	}
}

func TestConformance_ProtocolFlagsMatchTheSpecification(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  protocol.Flags
		want uint32
	}{
		{"kXR_isServer", protocol.IsServer, 0x00000001},
		{"kXR_isManager", protocol.IsManager, 0x00000002},
		{"kXR_attrMeta", protocol.IsMeta, 0x00000100},
		{"kXR_attrProxy", protocol.IsProxy, 0x00000200},
		{"kXR_attrSuper", protocol.IsSupervisor, 0x00000400},
	} {
		if uint32(tc.got) != tc.want {
			t.Errorf("%s is %#x, want %#x", tc.name, tc.got, tc.want)
		}
	}

	if got, want := protocol.ForceSecurity, protocol.SecurityOptions(0x02); got != want {
		t.Errorf("kXR_secOFrce is %#x, want %#x", got, want)
	}
	for _, tc := range []struct {
		name string
		got  protocol.RequestOptions
		want uint8
	}{
		{"none", protocol.RequestOptionsNone, 0},
		{"kXR_secreqs", protocol.ReturnSecurityRequirements, 0x01},
		{"kXR_ableTLS", protocol.AbleTLS, 0x02},
		{"kXR_wantTLS", protocol.WantTLS, 0x04},
	} {
		if uint8(tc.got) != tc.want {
			t.Errorf("kXR_protocol option %s is %#x, want %#x", tc.name, tc.got, tc.want)
		}
	}
}

// TestConformance_SecurityLevelsMatchTheSpecification pins the levels a server
// uses to say how much of the session it wants signed. They are ordered, and a
// client that misreads one signs less than the server demanded.
func TestConformance_SecurityLevelsMatchTheSpecification(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  xrdproto.SecurityLevel
		want byte
	}{
		{"kXR_secNone", xrdproto.NoneLevel, 0},
		{"kXR_secCompatible", xrdproto.Compatible, 1},
		{"kXR_secStandard", xrdproto.Standard, 2},
		{"kXR_secIntense", xrdproto.Intense, 3},
		{"kXR_secPedantic", xrdproto.Pedantic, 4},
	} {
		if byte(tc.got) != tc.want {
			t.Errorf("%s is %d, want %d", tc.name, tc.got, tc.want)
		}
	}

	for _, tc := range []struct {
		name string
		got  xrdproto.RequestLevel
		want byte
	}{
		{"kXR_signIgnore", xrdproto.SignNone, 0},
		{"kXR_signLikely", xrdproto.SignLikely, 1},
		{"kXR_signNeeded", xrdproto.SignNeeded, 2},
	} {
		if byte(tc.got) != tc.want {
			t.Errorf("%s is %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}
