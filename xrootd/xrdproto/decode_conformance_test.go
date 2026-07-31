// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the decoding half of the protocol: nothing a peer can put on
// the wire may crash the client.
//
// Every message a client decodes arrives from a host it has not authenticated
// yet — the kXR_protocol reply comes before the security handshake even starts
// — and the client has no recover() anywhere. A decoder that indexes past the
// end of a truncated message, or sizes an allocation from a length the peer
// chose, therefore takes the whole process down. These tests feed every decoder
// in the protocol a truncated, an over-long and a garbage body and require an
// error instead.

package xrdproto_test

import (
	"fmt"
	"reflect"
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
	"go-hep.org/x/hep/xrootd/xrdproto/handshake"
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

var (
	decHandle  = xrdfs.FileHandle{0x11, 0x22, 0x33, 0x44}
	decPath    = "/tmp/dir/file.dat"
	decSession = [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
)

// decCase is one decoder, a well-formed message for it, and how much of that
// message it may not do without.
type decCase struct {
	name string
	// valid encodes a message the decoder must accept in full.
	valid xrdproto.Marshaler
	// dec builds a fresh, empty destination.
	dec func() xrdproto.Unmarshaler
	// needs is the size of the fixed part of the message: every shorter body
	// is truncated in a way the decoder must report. Messages that end in a
	// run-to-the-end field (a path, a data blob, a list) have a needs smaller
	// than their valid encoding, since a shorter body is then a legal
	// message rather than a truncated one.
	needs int
	// notRoundTrip, when set, says why re-encoding the decoded message
	// cannot reproduce the bytes it came from.
	notRoundTrip string
}

func decCases() []decCase {
	return []decCase{
		// Requests. The client only writes these, but xrd-srv reads them
		// from unauthenticated clients, which is the same exposure.
		{name: "admin/request", valid: &admin.Request{Req: "x"}, dec: func() xrdproto.Unmarshaler { return &admin.Request{} }, needs: 20},
		{name: "auth/request", valid: &auth.Request{Type: [4]byte{'k', 'r', 'b', '5'}, Credentials: "cred"}, dec: func() xrdproto.Unmarshaler { return &auth.Request{} }, needs: 24},
		{name: "bind/request", valid: &bind.Request{SessionID: decSession}, dec: func() xrdproto.Unmarshaler { return &bind.Request{} }, needs: 20},
		{name: "chmod/request", valid: &chmod.Request{Mode: 0755, Path: decPath}, dec: func() xrdproto.Unmarshaler { return &chmod.Request{} }, needs: 20},
		{name: "decrypt/request", valid: &decrypt.Request{}, dec: func() xrdproto.Unmarshaler { return &decrypt.Request{} }, needs: 16},
		{name: "dirlist/request", valid: &dirlist.Request{Path: decPath}, dec: func() xrdproto.Unmarshaler { return &dirlist.Request{} }, needs: 20},
		{name: "endsess/request", valid: &endsess.Request{SessionID: decSession}, dec: func() xrdproto.Unmarshaler { return &endsess.Request{} }, needs: 20},
		{name: "fattr/request", valid: &fattr.Request{Handle: decHandle, Body: []byte("v")}, dec: func() xrdproto.Unmarshaler { return &fattr.Request{} }, needs: 20},
		{name: "handshake/request", valid: handshake.NewRequest(), dec: func() xrdproto.Unmarshaler { return &handshake.Request{} }, needs: 20},
		{name: "locate/request", valid: &locate.Request{Options: locate.Refresh, Path: decPath}, dec: func() xrdproto.Unmarshaler { return &locate.Request{} }, needs: 20},
		{name: "login/request", valid: &login.Request{Username: [8]byte{'g', 'o', 'h', 'e', 'p'}, Token: []byte("t")}, dec: func() xrdproto.Unmarshaler { return &login.Request{} }, needs: 20},
		{name: "mkdir/request", valid: &mkdir.Request{Mode: 0755, Path: decPath}, dec: func() xrdproto.Unmarshaler { return &mkdir.Request{} }, needs: 20},
		{name: "mv/request", valid: &mv.Request{OldPath: decPath, NewPath: decPath + ".bak"}, dec: func() xrdproto.Unmarshaler { return &mv.Request{} }, needs: 20},
		{name: "open/request", valid: open.NewRequest(decPath, 0644, xrdfs.OpenOptionsOpenRead), dec: func() xrdproto.Unmarshaler { return &open.Request{} }, needs: 20},
		{name: "pgread/request", valid: &pgread.Request{Handle: decHandle, Offset: 1, ReadLength: 4096}, dec: func() xrdproto.Unmarshaler { return &pgread.Request{} }, needs: 20},
		{name: "pgwrite/request", valid: &pgwrite.Request{Handle: decHandle, Data: []byte("data")}, dec: func() xrdproto.Unmarshaler { return &pgwrite.Request{} }, needs: 20},
		{name: "ping/request", valid: &ping.Request{}, dec: func() xrdproto.Unmarshaler { return &ping.Request{} }, needs: 16},
		{name: "prepare/request", valid: &prepare.Request{Options: prepare.Stage, Paths: []string{decPath}}, dec: func() xrdproto.Unmarshaler { return &prepare.Request{} }, needs: 20},
		{name: "protocol/request", valid: protocol.NewRequest(0, true), dec: func() xrdproto.Unmarshaler { return &protocol.Request{} }, needs: 20},
		{name: "query/request", valid: &query.Request{Query: query.Checksum, Handle: decHandle, Args: []byte(decPath)}, dec: func() xrdproto.Unmarshaler { return &query.Request{} }, needs: 20},
		{name: "read/request", valid: &read.Request{Handle: decHandle, Offset: 1, Length: 2}, dec: func() xrdproto.Unmarshaler { return &read.Request{} }, needs: 20},
		{name: "readv/request", valid: &readv.Request{Segments: []readv.Segment{{Handle: decHandle, Length: 1}}}, dec: func() xrdproto.Unmarshaler { return &readv.Request{} }, needs: 20},
		{name: "rm/request", valid: &rm.Request{Path: decPath}, dec: func() xrdproto.Unmarshaler { return &rm.Request{} }, needs: 20},
		{name: "rmdir/request", valid: &rmdir.Request{Path: decPath}, dec: func() xrdproto.Unmarshaler { return &rmdir.Request{} }, needs: 20},
		{name: "sigver/request", valid: sigver.NewRequest(rm.RequestID, 1, []byte("sig")), dec: func() xrdproto.Unmarshaler { return &sigver.Request{} }, needs: 20},
		{name: "stat/request", valid: &stat.Request{Path: decPath}, dec: func() xrdproto.Unmarshaler { return &stat.Request{} }, needs: 20},
		{name: "statx/request", valid: statx.NewRequest([]string{decPath}), dec: func() xrdproto.Unmarshaler { return &statx.Request{} }, needs: 20},
		{name: "sync/request", valid: &sync.Request{Handle: decHandle}, dec: func() xrdproto.Unmarshaler { return &sync.Request{} }, needs: 20},
		{name: "truncate/request", valid: &truncate.Request{Handle: decHandle, Size: 7}, dec: func() xrdproto.Unmarshaler { return &truncate.Request{} }, needs: 20},
		{name: "verifyw/request", valid: &verifyw.Request{Handle: decHandle, Data: []byte("v")}, dec: func() xrdproto.Unmarshaler { return &verifyw.Request{} }, needs: 20},
		{name: "write/request", valid: &write.Request{Handle: decHandle, Data: []byte("w")}, dec: func() xrdproto.Unmarshaler { return &write.Request{} }, needs: 20},
		{
			name:  "writev/request",
			valid: &writev.Request{Segments: []writev.Segment{{Handle: decHandle, Data: []byte("w")}}},
			dec:   func() xrdproto.Unmarshaler { return &writev.Request{} },
			needs: 20,
			notRoundTrip: "the segment data sits past the declared payload, so decoding sizes " +
				"the buffers and leaves filling them to whoever reads the connection",
		},
		{name: "xrdclose/request", valid: &xrdclose.Request{Handle: decHandle}, dec: func() xrdproto.Unmarshaler { return &xrdclose.Request{} }, needs: 20},

		// Responses, the surface a rogue or broken server reaches.
		{name: "admin/response", valid: &admin.Response{Data: []byte("a")}, dec: func() xrdproto.Unmarshaler { return &admin.Response{} }, needs: 0},
		{name: "bind/response", valid: &bind.Response{PathID: 2}, dec: func() xrdproto.Unmarshaler { return &bind.Response{} }, needs: 1},
		{name: "dirlist/response", valid: &dirlist.Response{Entries: []xrdfs.EntryStat{{EntryName: "f"}}}, dec: func() xrdproto.Unmarshaler { return &dirlist.Response{} }, needs: 0},
		{name: "fattr/response", valid: &fattr.Response{Raw: []byte{0, 0}}, dec: func() xrdproto.Unmarshaler { return &fattr.Response{} }, needs: 0},
		{name: "handshake/response", valid: &handshake.Response{ProtocolVersion: 0x310, ServerType: xrdproto.DataServer}, dec: func() xrdproto.Unmarshaler { return &handshake.Response{} }, needs: 8},
		{name: "locate/response", valid: &locate.Response{Data: []byte("S host:1094")}, dec: func() xrdproto.Unmarshaler { return &locate.Response{} }, needs: 0},
		{name: "login/response", valid: &login.Response{SessionID: decSession, SecurityInformation: []byte("&P=krb5")}, dec: func() xrdproto.Unmarshaler { return &login.Response{} }, needs: 16},
		{name: "open/response", valid: &open.Response{FileHandle: decHandle}, dec: func() xrdproto.Unmarshaler { return &open.Response{} }, needs: 4},
		{name: "pgread/response", valid: &pgread.Response{Data: []byte("d"), Offset: 4096}, dec: func() xrdproto.Unmarshaler { return &pgread.Response{} }, needs: 0},
		{name: "pgwrite/response", valid: &pgwrite.Response{Offset: 4096}, dec: func() xrdproto.Unmarshaler { return &pgwrite.Response{} }, needs: 0},
		{name: "prepare/response", valid: &prepare.Response{Data: []byte("id")}, dec: func() xrdproto.Unmarshaler { return &prepare.Response{} }, needs: 0},
		{name: "protocol/response", valid: &protocol.Response{BinaryProtocolVersion: 0x310, HasSecurityInfo: true, SecurityLevel: xrdproto.Standard, SecurityOverrides: []xrdproto.SecurityOverride{{RequestIndex: 1, RequestLevel: xrdproto.SignNeeded}}}, dec: func() xrdproto.Unmarshaler { return &protocol.Response{} }, needs: 8},
		{name: "query/response", valid: &query.Response{Data: []byte("q")}, dec: func() xrdproto.Unmarshaler { return &query.Response{} }, needs: 0},
		{name: "read/response", valid: &read.Response{Data: []byte("r")}, dec: func() xrdproto.Unmarshaler { return &read.Response{} }, needs: 0},
		{name: "readv/response", valid: &readv.Response{Chunks: []readv.Chunk{{Handle: decHandle, Offset: 0, Data: []byte("r")}}}, dec: func() xrdproto.Unmarshaler { return &readv.Response{} }, needs: 0},
		{name: "stat/response", valid: &stat.DefaultResponse{EntryStat: xrdfs.EntryStat{HasStatInfo: true, EntrySize: 1}}, dec: func() xrdproto.Unmarshaler { return &stat.DefaultResponse{} }, needs: 0},
		{name: "stat/virtualfs-response", valid: &stat.VirtualFSResponse{}, dec: func() xrdproto.Unmarshaler { return &stat.VirtualFSResponse{} }, needs: 0},
		{name: "statx/response", valid: &statx.Response{StatFlags: []xrdfs.StatFlags{xrdfs.StatIsFile}}, dec: func() xrdproto.Unmarshaler { return &statx.Response{} }, needs: 0},

		// Framing and the pieces the messages above embed.
		{name: "xrdproto/request-header", valid: &xrdproto.RequestHeader{StreamID: xrdproto.StreamID{1, 2}, RequestID: rm.RequestID}, dec: func() xrdproto.Unmarshaler { return &xrdproto.RequestHeader{} }, needs: 4},
		{name: "xrdproto/response-header", valid: &xrdproto.ResponseHeader{StreamID: xrdproto.StreamID{1, 2}, Status: xrdproto.Ok, DataLength: 3}, dec: func() xrdproto.Unmarshaler { return &xrdproto.ResponseHeader{} }, needs: 8},
		{name: "xrdproto/server-error", valid: &xrdproto.ServerError{Code: xrdproto.IOError, Message: "boom"}, dec: func() xrdproto.Unmarshaler { return &xrdproto.ServerError{} }, needs: 4},
		{name: "xrdproto/wait-response", valid: &xrdproto.WaitResponse{Duration: 1}, dec: func() xrdproto.Unmarshaler { return &xrdproto.WaitResponse{} }, needs: 4},
		{name: "xrdproto/security-override", valid: &xrdproto.SecurityOverride{RequestIndex: 1, RequestLevel: xrdproto.SignNeeded}, dec: func() xrdproto.Unmarshaler { return &xrdproto.SecurityOverride{} }, needs: 2},
		{name: "xrdfs/file-compression", valid: &xrdfs.FileCompression{PageSize: 4096}, dec: func() xrdproto.Unmarshaler { return &xrdfs.FileCompression{} }, needs: 8},
		{name: "xrdfs/entry-stat", valid: &xrdfs.EntryStat{HasStatInfo: true, EntrySize: 1}, dec: func() xrdproto.Unmarshaler { return &xrdfs.EntryStat{} }, needs: 0},
		{name: "xrdfs/virtualfs-stat", valid: &xrdfs.VirtualFSStat{}, dec: func() xrdproto.Unmarshaler { return &xrdfs.VirtualFSStat{} }, needs: 0},
	}
}

func decMarshal(t *testing.T, m xrdproto.Marshaler) []byte {
	t.Helper()
	var w xrdenc.WBuffer
	if err := m.MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal the reference message: %v", err)
	}
	return w.Bytes()
}

// decSafe decodes body and turns a panic into a test failure. Decoding is the
// only place in the client where a remote peer picks the control flow, and
// there is no recover() between here and main.
func decSafe(t *testing.T, dec xrdproto.Unmarshaler, body []byte) (err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("decoding a %d-byte body panicked: %v", len(body), r)
		}
	}()
	return dec.UnmarshalXrd(xrdenc.NewRBuffer(body))
}

// TestDecodeTruncated cuts every message short at every offset. Below the fixed
// part of the message the decoder must say so; above it, it must at least not
// crash.
func TestDecodeTruncated(t *testing.T) {
	for _, tc := range decCases() {
		t.Run(tc.name, func(t *testing.T) {
			valid := decMarshal(t, tc.valid)
			if len(valid) < tc.needs {
				t.Fatalf("the reference message is %d bytes, shorter than the %d it is said to need", len(valid), tc.needs)
			}
			for n := range len(valid) {
				err := decSafe(t, tc.dec(), valid[:n])
				if err == nil && n < tc.needs {
					t.Errorf("a %d-byte body was accepted, but the message needs %d bytes", n, tc.needs)
				}
			}
		})
	}
}

// TestDecodeGarbage feeds bodies that are not messages at all. The patterns are
// the ones that flip decoders into their unusual branches: 0xff sets every
// length and count to its extreme, 'S' is the flag byte kXR_protocol keys its
// security block on, and the pseudo-random rounds cover the rest.
func TestDecodeGarbage(t *testing.T) {
	const rounds = 64
	seed := uint64(1)
	next := func() byte {
		seed = seed*6364136223846793005 + 1442695040888963407
		return byte(seed >> 33)
	}
	for _, tc := range decCases() {
		t.Run(tc.name, func(t *testing.T) {
			for n := range 72 {
				for round := range rounds {
					body := make([]byte, n)
					for i := range body {
						switch round {
						case 0: // nothing set
						case 1:
							body[i] = 0xff
						case 2:
							body[i] = 'S'
						case 3:
							body[i] = byte(i)
						default:
							body[i] = next()
						}
					}
					_ = decSafe(t, tc.dec(), body)
				}
			}
		})
	}
}

// TestDecodeRejectsPeerChosenAllocations covers the length-prefixed fields. The
// length arrives from the peer and is read before the bytes it counts, so a
// decoder that allocates first hands a remote host an out-of-memory switch —
// and a negative length panics outright.
func TestDecodeRejectsPeerChosenAllocations(t *testing.T) {
	// Each case is a body whose fixed part is well-formed and whose trailing
	// length says far more data follows than the body holds.
	for _, tc := range []struct {
		name   string
		dec    func() xrdproto.Unmarshaler
		prefix int // bytes before the length
	}{
		{"login/request token", func() xrdproto.Unmarshaler { return &login.Request{} }, 16},
		{"write/request data", func() xrdproto.Unmarshaler { return &write.Request{} }, 16},
		{"verifyw/request data", func() xrdproto.Unmarshaler { return &verifyw.Request{} }, 16},
		{"sigver/request signature", func() xrdproto.Unmarshaler { return &sigver.Request{} }, 16},
		{"query/request args", func() xrdproto.Unmarshaler { return &query.Request{} }, 16},
		{"fattr/request body", func() xrdproto.Unmarshaler { return &fattr.Request{} }, 16},
		{"chmod/request path", func() xrdproto.Unmarshaler { return &chmod.Request{} }, 16},
		{"open/request path", func() xrdproto.Unmarshaler { return &open.Request{} }, 16},
		{"rm/request path", func() xrdproto.Unmarshaler { return &rm.Request{} }, 16},
	} {
		for _, n := range []int32{-1, -1 << 31, 1 << 20, 1<<31 - 1} {
			t.Run(fmt.Sprintf("%s/%d", tc.name, n), func(t *testing.T) {
				var w xrdenc.WBuffer
				w.Next(tc.prefix)
				w.WriteI32(n)
				if err := decSafe(t, tc.dec(), w.Bytes()); err == nil {
					t.Fatalf("a declared length of %d was accepted with no data behind it", n)
				}
			})
		}
	}
}

// TestDecodeRoundTrip is the counterpart of the tests above: hardening a
// decoder against short input must not change what it does with good input.
func TestDecodeRoundTrip(t *testing.T) {
	for _, tc := range decCases() {
		t.Run(tc.name, func(t *testing.T) {
			valid := decMarshal(t, tc.valid)
			dst := tc.dec()
			if err := decSafe(t, dst, valid); err != nil {
				t.Fatalf("could not decode a well-formed message: %v", err)
			}
			if tc.notRoundTrip != "" {
				t.Skip(tc.notRoundTrip)
			}
			m, ok := dst.(xrdproto.Marshaler)
			if !ok {
				return
			}
			if got := decMarshal(t, m); !reflect.DeepEqual(got, valid) {
				t.Fatalf("re-encoding the decoded message changed it:\ngot  %x\nwant %x", got, valid)
			}
		})
	}
}
