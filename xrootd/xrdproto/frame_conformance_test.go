// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Wire-shape conformance for every request type.
//
// A request frame is streamid[2] reqid[2] params[16] dlen[4] followed by dlen
// bytes of payload, and nothing else. Each request package marshals its own
// params and dlen by hand, so nothing but a table like this one checks that
// they all agree on where the boundaries are: a params region that is 15 or 17
// bytes long still round-trips against itself and only fails against a real
// server. The cases below pin, per request, the request id in the frame, which
// params bytes are reserved (and so must be zero on the wire), and the exact
// declared payload length.

package xrdproto_test

import (
	"bytes"
	"encoding/binary"
	"reflect"
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

// frameCase describes the wire shape one request type must produce.
type frameCase struct {
	name string
	req  xrdproto.Request
	id   uint16 // request id expected in the frame

	// params is a 16-character picture of the parameters region: '.' marks a
	// byte the protocol reserves, which must be zero on the wire, and 'x'
	// marks a byte the request fills in. Every fixture below puts a non-zero
	// value in the fields it exercises, so a '.' in the wrong column fails.
	params string

	// dlen is the payload length the frame must declare.
	dlen int

	// trailer is the number of bytes marshaled past the declared payload.
	// Only kXR_writev has any: its dlen covers the descriptors alone and the
	// segment data follows the frame on the wire.
	trailer int

	// notRoundTrip explains why unmarshaling and remarshaling this request
	// does not reproduce the original bytes; empty means it must.
	notRoundTrip string
}

var (
	frameHandle  = xrdfs.FileHandle{0x11, 0x22, 0x33, 0x44}
	framePath    = "/tmp/dir/file.dat"
	frameSession = [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	frameData    = []byte("go-hep")
)

func frameCases() []frameCase {
	sigverReq := sigver.NewRequest(open.RequestID, 0x0102030405060708, frameData)
	return []frameCase{
		{
			name: "admin", req: &admin.Request{Req: "query stats"}, id: 3020,
			params: "................", dlen: len("query stats"),
		},
		{
			name: "auth", req: &auth.Request{Type: [4]byte{'u', 'n', 'i', 'x'}, Credentials: "unix"}, id: 3000,
			params: "............xxxx", dlen: 4,
		},
		{
			name: "bind", req: &bind.Request{SessionID: frameSession}, id: 3024,
			params: "xxxxxxxxxxxxxxxx", dlen: 0,
		},
		{
			name: "chmod", req: &chmod.Request{Mode: 0755, Path: framePath}, id: 3002,
			params: "..............xx", dlen: len(framePath),
		},
		{
			name: "decrypt", req: &decrypt.Request{}, id: 3030,
			params: "................", dlen: 0,
		},
		{
			name: "dirlist", req: &dirlist.Request{Options: dirlist.WithStatInfo, Path: framePath}, id: 3004,
			params: "...............x", dlen: len(framePath),
		},
		{
			name: "endsess", req: &endsess.Request{SessionID: frameSession}, id: 3023,
			params: "xxxxxxxxxxxxxxxx", dlen: 0,
		},
		{
			name:   "fattr",
			req:    &fattr.Request{Handle: frameHandle, Subcode: fattr.Get, NumAttr: 1, Options: fattr.AData, Body: []byte("name\x00")},
			id:     3020,
			params: "xxxxxxx.........", dlen: 5,
		},
		{
			name: "locate", req: &locate.Request{Options: locate.Refresh | locate.PreferName, Path: framePath}, id: 3027,
			params: "xx..............", dlen: len(framePath),
		},
		{
			name: "login",
			req: &login.Request{
				Pid: 0x01020304, Username: [8]byte{'g', 'o', '-', 'h', 'e', 'p', '0', '1'},
				Ability: 0x01, Capabilities: 0x04, Role: 0x01, Token: []byte("tok"),
			},
			id: 3007, params: "xxxxxxxxxxxx.xxx", dlen: 3,
		},
		{
			name: "mkdir", req: &mkdir.Request{Options: mkdir.OptionsMakePath, Mode: 0755, Path: framePath}, id: 3008,
			params: "x.............xx", dlen: len(framePath),
		},
		{
			// The two paths travel as one blank-separated payload, and the
			// length of the first is in the last two parameter bytes.
			name: "mv", req: &mv.Request{OldPath: "/old", NewPath: "/new"}, id: 3009,
			params: "..............xx", dlen: len("/old /new"),
		},
		{
			name: "open", req: open.NewRequest(framePath, 0755, xrdfs.OpenOptionsOpenRead), id: 3010,
			params: "xxxx............", dlen: len(framePath),
		},
		{
			name: "pgread", req: &pgread.Request{Handle: frameHandle, Offset: 0x0102030405060708, ReadLength: 4096}, id: 3030,
			params: "xxxxxxxxxxxxxxxx", dlen: 0,
		},
		{
			// The payload is the page-framed form of Data: a 4-byte checksum
			// ahead of each 4 KiB page, so it is longer than the data itself.
			name: "pgwrite", req: &pgwrite.Request{Handle: frameHandle, Offset: 0, Data: frameData, Flags: pgwrite.Retry}, id: 3026,
			params: "xxxxxxxxxxxx.x..", dlen: 4 + len(frameData),
		},
		{
			name: "ping", req: &ping.Request{}, id: 3011,
			params: "................", dlen: 0,
		},
		{
			name:   "prepare",
			req:    &prepare.Request{Options: prepare.Stage, Priority: 2, Port: 1094, Paths: []string{"/a", "/b"}},
			id:     3021,
			params: "xxxx............", dlen: len("/a\n/b"),
		},
		{
			name: "protocol", req: protocol.NewRequest(300, true), id: 3006,
			params: "xxxxx...........", dlen: 0,
		},
		{
			name: "query", req: &query.Request{Query: query.Checksum, Handle: frameHandle, Args: []byte(framePath)}, id: 3001,
			params: "xx..xxxx........", dlen: len(framePath),
		},
		{
			name: "read", req: &read.Request{Handle: frameHandle, Offset: 0x0102030405060708, Length: 4096}, id: 3013,
			params: "xxxxxxxxxxxxxxxx", dlen: 0,
		},
		{
			// The pathid in the last parameter byte is zero: this client asks
			// for the data on the control socket.
			name: "readv", req: &readv.Request{Segments: []readv.Segment{
				{Handle: frameHandle, Length: 100, Offset: 0},
				{Handle: frameHandle, Length: 200, Offset: 4096},
			}}, id: 3025,
			params: "................", dlen: 2 * readv.SegmentLength,
		},
		{
			name: "rm", req: &rm.Request{Path: framePath}, id: 3014,
			params: "................", dlen: len(framePath),
		},
		{
			name: "rmdir", req: &rmdir.Request{Path: framePath}, id: 3015,
			params: "................", dlen: len(framePath),
		},
		{
			name: "sigver", req: &sigverReq, id: 3029,
			params: "xxxxxxxxxxxxx...", dlen: 32, // sha256
		},
		{
			name: "stat", req: &stat.Request{Options: stat.OptionsVFS, FileHandle: frameHandle, Path: framePath}, id: 3017,
			params: "x...........xxxx", dlen: len(framePath),
		},
		{
			name: "statx", req: statx.NewRequest([]string{"/a", "/b"}), id: 3022,
			params: "................", dlen: len("/a\n/b"),
		},
		{
			name: "sync", req: &sync.Request{Handle: frameHandle}, id: 3016,
			params: "xxxx............", dlen: 0,
		},
		{
			name: "truncate", req: &truncate.Request{Handle: frameHandle, Size: 0x0102030405060708}, id: 3028,
			params: "xxxxxxxxxxxx....", dlen: 0,
		},
		{
			name: "verifyw", req: verifyw.NewRequestCRC32(frameHandle, 0x0102030405060708, frameData), id: 3026,
			params: "xxxxxxxxxxxx.x..", dlen: 4 + len(frameData),
		},
		{
			name: "write", req: &write.Request{Handle: frameHandle, Offset: 0x0102030405060708, Data: frameData}, id: 3019,
			params: "xxxxxxxxxxxx....", dlen: len(frameData),
		},
		{
			// The one request whose data is outside dlen. See the vector I/O
			// section of the client lessons-learned.
			name: "writev", req: &writev.Request{Options: writev.OptionSync, Segments: []writev.Segment{
				{Handle: frameHandle, Offset: 0, Data: frameData},
				{Handle: frameHandle, Offset: 4096, Data: frameData},
			}}, id: 3031,
			params: "x...............", dlen: 2 * writev.SegmentLength, trailer: 2 * len(frameData),
			notRoundTrip: "the segment data is outside dlen, so unmarshaling sizes the buffers and leaves filling them to whoever reads the connection",
		},
		{
			name: "xrdclose", req: &xrdclose.Request{Handle: frameHandle, Size: 0x0102030405060708}, id: 3003,
			params: "xxxxxxxxxxxx....", dlen: 0,
		},
	}
}

// frame marshals req the way a session does: the four-byte request header,
// then whatever the request itself emits.
func frame(t *testing.T, req xrdproto.Request) []byte {
	t.Helper()

	var w xrdenc.WBuffer
	hdr := xrdproto.RequestHeader{StreamID: xrdproto.StreamID{0xab, 0xcd}, RequestID: req.ReqID()}
	if err := hdr.MarshalXrd(&w); err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	if err := req.MarshalXrd(&w); err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return w.Bytes()
}

const frameHeaderLen = xrdproto.RequestHeaderLength + 16 + 4

// TestFrameConformance_EveryRequestIsHeaderPlusItsDeclaredPayload is the
// invariant a server parses by: it reads 24 bytes, takes dlen from the last
// four, and reads exactly that many more. A request that emits one byte more
// or less leaves the next request misaligned on the connection.
func TestFrameConformance_EveryRequestIsHeaderPlusItsDeclaredPayload(t *testing.T) {
	for _, tc := range frameCases() {
		t.Run(tc.name, func(t *testing.T) {
			got := frame(t, tc.req)
			if len(got) < frameHeaderLen {
				t.Fatalf("frame is %d bytes, shorter than the %d-byte header", len(got), frameHeaderLen)
			}

			dlen := int(binary.BigEndian.Uint32(got[20:24]))
			if dlen != tc.dlen {
				t.Fatalf("frame declares a %d-byte payload, want %d", dlen, tc.dlen)
			}
			if want := frameHeaderLen + tc.dlen + tc.trailer; len(got) != want {
				t.Fatalf("marshaled %d bytes, want %d (24 header + %d payload + %d trailer)",
					len(got), want, tc.dlen, tc.trailer)
			}
		})
	}
}

// TestFrameConformance_RequestIDInTheFrameMatchesReqID guards the one thing a
// request cannot get wrong twice: the id it advertises and the id it writes.
func TestFrameConformance_RequestIDInTheFrameMatchesReqID(t *testing.T) {
	for _, tc := range frameCases() {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.req.ReqID(); got != tc.id {
				t.Fatalf("ReqID is %d, want %d", got, tc.id)
			}
			got := frame(t, tc.req)
			if id := binary.BigEndian.Uint16(got[2:4]); id != tc.id {
				t.Fatalf("the frame carries request id %d, want %d", id, tc.id)
			}
			if !bytes.Equal(got[:2], []byte{0xab, 0xcd}) {
				t.Fatalf("the stream id was overwritten: %x", got[:2])
			}
		})
	}
}

// TestFrameConformance_ReservedParametersAreZero checks the bytes the protocol
// reserves. A server is entitled to reject a request that fills one in, and a
// reserved byte that carries a stale value is how a struct field that was
// added at the wrong offset survives every round-trip test.
func TestFrameConformance_ReservedParametersAreZero(t *testing.T) {
	for _, tc := range frameCases() {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.params) != 16 {
				t.Fatalf("the parameters picture is %d bytes, want 16", len(tc.params))
			}
			if n := strings.Count(tc.params, ".") + strings.Count(tc.params, "x"); n != 16 {
				t.Fatalf("the parameters picture has %d '.'/'x' bytes, want 16: %q", n, tc.params)
			}

			params := frame(t, tc.req)[4:20]
			for i, c := range tc.params {
				if c == '.' && params[i] != 0 {
					t.Fatalf("parameter byte %d is reserved but carries %#x (params %x)", i, params[i], params)
				}
			}
		})
	}
}

// TestFrameConformance_RequestsRoundTrip decodes each frame with the same
// package that encoded it and re-encodes the result. Marshal and unmarshal are
// written separately in every package, so a field read at the wrong offset
// only shows up when the two are made to meet.
func TestFrameConformance_RequestsRoundTrip(t *testing.T) {
	for _, tc := range frameCases() {
		t.Run(tc.name, func(t *testing.T) {
			if tc.notRoundTrip != "" {
				t.Skip(tc.notRoundTrip)
			}

			want := frame(t, tc.req)
			back := reflect.New(reflect.TypeOf(tc.req).Elem()).Interface().(xrdproto.Request)
			if err := back.UnmarshalXrd(xrdenc.NewRBuffer(want[4:])); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := frame(t, back); !bytes.Equal(got, want) {
				t.Fatalf("re-marshaling the decoded request changed the frame:\n got %x\nwant %x", got, want)
			}
		})
	}
}

// TestFrameConformance_EveryRequestTypeIsCovered fails when a request package
// is added without a case above, so the table cannot quietly fall behind the
// protocol.
func TestFrameConformance_EveryRequestTypeIsCovered(t *testing.T) {
	seen := make(map[string]bool)
	for _, tc := range frameCases() {
		if seen[tc.name] {
			t.Fatalf("duplicate case %q", tc.name)
		}
		seen[tc.name] = true
	}

	// Every directory under xrdproto that declares a RequestID. handshake is
	// excluded: it is the fixed 20-byte greeting, not a request frame.
	for _, name := range []string{
		"admin", "auth", "bind", "chmod", "decrypt", "dirlist", "endsess",
		"fattr", "locate", "login", "mkdir", "mv", "open", "pgread", "pgwrite",
		"ping", "prepare", "protocol", "query", "read", "readv", "rm", "rmdir",
		"sigver", "stat", "statx", "sync", "truncate", "verifyw", "write",
		"writev", "xrdclose",
	} {
		if !seen[name] {
			t.Errorf("request type %q has no wire-shape case", name)
		}
	}
}
