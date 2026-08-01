// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"encoding/binary"
	"net"
	"reflect"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/hardlink"
	"go-hep.org/x/hep/xrootd/xrdproto/prepare"
	"go-hep.org/x/hep/xrootd/xrdproto/query"
	"go-hep.org/x/hep/xrootd/xrdproto/readlink"
	"go-hep.org/x/hep/xrootd/xrdproto/set"
	"go-hep.org/x/hep/xrootd/xrdproto/symlink"
)

// answering runs one request through the mock server, hands the raw frame to
// check, and answers Ok with resp. It is the shape every test below shares:
// the client call is only interesting for the bytes it puts on the wire.
func answering(t *testing.T, resp xrdproto.Marshaler, check func(t *testing.T, raw []byte)) func(cancel func(), conn net.Conn) {
	t.Helper()

	return func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		check(t, data)
		if err := xrdproto.WriteResponse(conn, xrdproto.StreamID{data[0], data[1]}, xrdproto.Ok, resp); err != nil {
			cancel()
		}
	}
}

func TestConformance_ALinkRequestNamesBothPathsInOneString(t *testing.T) {
	// kXR_symlink and kXR_link are shaped like kXR_mv: the two paths travel as
	// one space-separated string, and the length of the first is sent
	// separately. A server that had only the space to go on would cut a name
	// that contains one in half.
	const (
		first  = "/data/raw/run 42.root"
		second = "/data/by-tag/latest.root"
	)

	for _, tc := range []struct {
		name string
		id   uint16
		call func(fs *fileSystem) error
	}{
		{
			name: "symlink",
			id:   symlink.RequestID,
			call: func(fs *fileSystem) error { return fs.Symlink(context.Background(), first, second) },
		},
		{
			name: "hardlink",
			id:   hardlink.RequestID,
			call: func(fs *fileSystem) error { return fs.Hardlink(context.Background(), first, second) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serverFunc := answering(t, nil, func(t *testing.T, raw []byte) {
				if got := binary.BigEndian.Uint16(raw[2:4]); got != tc.id {
					t.Errorf("request id: got = %d, want = %d", got, tc.id)
				}
				if got, want := binary.BigEndian.Uint16(raw[frameParams+14:frameParams+16]), uint16(len(first)); got != want {
					t.Errorf("length of the first path: got = %d, want = %d", got, want)
				}
				if got, want := binary.BigEndian.Uint32(raw[frameDataLen:frameBody]), uint32(len(first)+len(second)+1); got != want {
					t.Errorf("data length: got = %d, want = %d", got, want)
				}
				if got, want := string(raw[frameBody:]), first+" "+second; got != want {
					t.Errorf("payload: got = %q, want = %q", got, want)
				}
			})

			testClientWithMockServer(serverFunc, func(cancel func(), client *Client) {
				if err := tc.call(client.FS().(*fileSystem)); err != nil {
					t.Fatalf("%s: %v", tc.name, err)
				}
			})
		})
	}
}

func TestConformance_AReadlinkAnswerIsTheTargetWithoutItsPadding(t *testing.T) {
	const (
		link   = "/data/by-tag/latest.root"
		target = "/data/raw/run42.root"
	)

	serverFunc := answering(t, readlink.Response{Data: []byte(target + "\x00\x00")}, func(t *testing.T, raw []byte) {
		if got, want := binary.BigEndian.Uint16(raw[2:4]), readlink.RequestID; got != want {
			t.Errorf("request id: got = %d, want = %d", got, want)
		}
		if got := string(raw[frameBody:]); got != link {
			t.Errorf("path: got = %q, want = %q", got, link)
		}
	})

	testClientWithMockServer(serverFunc, func(cancel func(), client *Client) {
		got, err := client.FS().(*fileSystem).Readlink(context.Background(), link)
		if err != nil {
			t.Fatalf("Readlink: %v", err)
		}
		if got != target {
			t.Fatalf("target: got = %q, want = %q", got, target)
		}
	})
}

func TestConformance_AVendorExtensionReportsAServerThatDoesNotHaveIt(t *testing.T) {
	// The link requests are not in the protocol specification. A server built
	// without them says kXR_Unsupported, and that answer is the only way to
	// find out; swallowing it would leave a caller believing it had published
	// a name that does not exist.
	for _, tc := range []struct {
		name string
		call func(fs *fileSystem) error
	}{
		{"symlink", func(fs *fileSystem) error { return fs.Symlink(context.Background(), "/a", "/b") }},
		{"hardlink", func(fs *fileSystem) error { return fs.Hardlink(context.Background(), "/a", "/b") }},
		{"readlink", func(fs *fileSystem) error {
			target, err := fs.Readlink(context.Background(), "/a")
			if err == nil && target != "" {
				t.Errorf("a refused readlink returned %q", target)
			}
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serverFunc := func(cancel func(), conn net.Conn) {
				data, err := xrdproto.ReadRequest(conn)
				if err != nil {
					cancel()
					return
				}
				err = xrdproto.WriteResponse(conn, xrdproto.StreamID{data[0], data[1]}, xrdproto.Error,
					xrdproto.ServerError{Code: xrdproto.Unsupported, Message: "not built with links"})
				if err != nil {
					cancel()
				}
			}

			testClientWithMockServer(serverFunc, func(cancel func(), client *Client) {
				err := tc.call(client.FS().(*fileSystem))
				if err == nil {
					t.Fatalf("a %s the server does not support reported success", tc.name)
				}
				if !strings.Contains(err.Error(), "not built with links") {
					t.Fatalf("error %q does not carry what the server said", err)
				}
			})
		})
	}
}

func TestConformance_AnAppIDIsSentAsASetDirective(t *testing.T) {
	// Every other client labels its connection, and a site that cannot tell
	// whose traffic a connection carries cannot answer the question its
	// monitoring exists to answer.
	const name = "analysis-42"

	serverFunc := answering(t, set.Response{}, func(t *testing.T, raw []byte) {
		if got, want := binary.BigEndian.Uint16(raw[2:4]), set.RequestID; got != want {
			t.Errorf("request id: got = %d, want = %d", got, want)
		}
		if got, want := string(raw[frameBody:]), "appid "+name; got != want {
			t.Errorf("directive: got = %q, want = %q", got, want)
		}
		for i, b := range raw[frameParams:frameDataLen] {
			if b != 0 {
				t.Errorf("reserved parameter byte %d is %d, want 0", i, b)
			}
		}
	})

	testClientWithMockServer(serverFunc, func(cancel func(), client *Client) {
		if err := client.FS().(*fileSystem).SetAppID(context.Background(), name); err != nil {
			t.Fatalf("SetAppID: %v", err)
		}
	})
}

func TestConformance_APrepareAsksInTheHalfWordThatHoldsTheOption(t *testing.T) {
	// Evicting lives in the extended half-word and everything older lives in
	// the options byte. An evict written into the byte would read as
	// kXR_cancel, so a request meant to free disk would instead withdraw an
	// unrelated staging request and leave the disk full.
	for _, tc := range []struct {
		name     string
		call     func(fs *fileSystem) error
		options  byte
		optionsX uint16
		paths    string
	}{
		{
			name: "stage",
			call: func(fs *fileSystem) error {
				_, err := fs.Stage(context.Background(), []string{"/a", "/b"}, 2)
				return err
			},
			options: prepare.Stage,
			paths:   "/a\n/b",
		},
		{
			name:     "evict",
			call:     func(fs *fileSystem) error { return fs.Evict(context.Background(), []string{"/a"}) },
			optionsX: prepare.Evict,
			paths:    "/a",
		},
		{
			name:    "cancel",
			call:    func(fs *fileSystem) error { return fs.CancelPrepare(context.Background(), "23297f000001") },
			options: prepare.Cancel,
			paths:   "23297f000001",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serverFunc := answering(t, prepare.Response{}, func(t *testing.T, raw []byte) {
				if got, want := binary.BigEndian.Uint16(raw[2:4]), prepare.RequestID; got != want {
					t.Errorf("request id: got = %d, want = %d", got, want)
				}
				if got := raw[frameParams]; got != tc.options {
					t.Errorf("options byte: got = %#02x, want = %#02x", got, tc.options)
				}
				if got := binary.BigEndian.Uint16(raw[frameParams+4 : frameParams+6]); got != tc.optionsX {
					t.Errorf("extended options: got = %#04x, want = %#04x", got, tc.optionsX)
				}
				if got := string(raw[frameBody:]); got != tc.paths {
					t.Errorf("paths: got = %q, want = %q", got, tc.paths)
				}
			})

			testClientWithMockServer(serverFunc, func(cancel func(), client *Client) {
				if err := tc.call(client.FS().(*fileSystem)); err != nil {
					t.Fatalf("%s: %v", tc.name, err)
				}
			})
		})
	}
}

func TestConformance_AStageReturnsTheHandleItWasGiven(t *testing.T) {
	const handle = "23297f000001"

	serverFunc := answering(t, prepare.Response{Data: []byte(handle + "\x00")}, func(t *testing.T, raw []byte) {})

	testClientWithMockServer(serverFunc, func(cancel func(), client *Client) {
		got, err := client.FS().(*fileSystem).Stage(context.Background(), []string{"/a"}, 1)
		if err != nil {
			t.Fatalf("Stage: %v", err)
		}
		if got != handle {
			t.Fatalf("handle: got = %q, want = %q", got, handle)
		}
	})
}

func TestConformance_AVisaIsAskedOfTheHandleNotThePath(t *testing.T) {
	// A visa describes the file this client has open. Asking by name would
	// answer about whatever the name resolves to now, which on a federation
	// need not be the replica the handle was opened on.
	handle := xrdfs.FileHandle{7, 7, 7, 7}
	const answer = "pool=disk1 staged=1"

	serverFunc := answering(t, query.Response{Data: []byte(answer + "\x00")}, func(t *testing.T, raw []byte) {
		var got query.Request
		if _, err := unmarshalRequest(raw, &got); err != nil {
			t.Errorf("could not unmarshal the query: %v", err)
			return
		}
		if got.Query != query.Visa {
			t.Errorf("query code: got = %d, want = %d", got.Query, query.Visa)
		}
		if !reflect.DeepEqual(got.Handle, handle) {
			t.Errorf("file handle: got = %v, want = %v", got.Handle, handle)
		}
		if len(got.Args) != 0 {
			t.Errorf("the visa query carries %q, want nothing", got.Args)
		}
	})

	testClientWithMockServer(serverFunc, func(cancel func(), client *Client) {
		f := file{fs: client.FS().(*fileSystem), handle: handle, sessionID: client.initialSessionID}
		got, err := f.Visa(context.Background())
		if err != nil {
			t.Fatalf("Visa: %v", err)
		}
		if got != answer {
			t.Fatalf("visa: got = %q, want = %q", got, answer)
		}
	})
}

func TestConformance_ACancelledChecksumNamesThePathItAbandons(t *testing.T) {
	// Checksumming a multi-terabyte file costs the server a full read of it.
	// A caller that has given up says which one, or the server keeps reading.
	const path = "/data/raw/run42.root"

	serverFunc := answering(t, query.Response{}, func(t *testing.T, raw []byte) {
		var got query.Request
		if _, err := unmarshalRequest(raw, &got); err != nil {
			t.Errorf("could not unmarshal the query: %v", err)
			return
		}
		if got.Query != query.CancelChecksum {
			t.Errorf("query code: got = %d, want = %d", got.Query, query.CancelChecksum)
		}
		if string(got.Args) != path {
			t.Errorf("path: got = %q, want = %q", got.Args, path)
		}
	})

	testClientWithMockServer(serverFunc, func(cancel func(), client *Client) {
		if err := client.FS().(*fileSystem).CancelChecksum(context.Background(), path); err != nil {
			t.Fatalf("CancelChecksum: %v", err)
		}
	})
}
