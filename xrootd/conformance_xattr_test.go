// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for extended attributes, whose failures arrive inside a
// successful response.
//
// kXR_fattr is unusual: the server answers kXR_ok and puts the outcome of each
// attribute in the body, as a per-attribute return code. A client that reads
// only the response status sees every operation succeed — a get of an attribute
// that does not exist returns empty bytes and no error, and a set that the
// server rejected is reported as done. Both are silent data loss dressed as
// success, so the per-attribute code has to be checked and turned into an
// error, and a body too short to hold one is a failure rather than a zero.

package xrootd

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/fattr"
)

// fattrRaw builds a single-attribute kXR_fattr response body: the error and
// attribute counts, the per-attribute return code, the NUL-terminated name and
// the value.
func fattrRaw(name string, rc uint16, value []byte) []byte {
	raw := []byte{0, 1}
	raw = binary.BigEndian.AppendUint16(raw, rc)
	raw = append(raw, name...)
	raw = append(raw, 0)
	raw = binary.BigEndian.AppendUint32(raw, uint32(len(value)))
	return append(raw, value...)
}

// fattrReplying answers whatever request arrives with an kXR_ok carrying raw.
func fattrReplying(t *testing.T, raw []byte) func(cancel func(), conn net.Conn) {
	t.Helper()

	return func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			t.Errorf("could not read the request: %v", err)
			return
		}
		hdr, err := unmarshalRequest(data, &fattr.Request{})
		if err != nil {
			cancel()
			t.Errorf("could not unmarshal the request: %v", err)
			return
		}
		if err := xrdproto.WriteResponse(conn, hdr.StreamID, xrdproto.Ok, fattr.Response{Raw: raw}); err != nil {
			cancel()
			t.Errorf("could not write the response: %v", err)
		}
	}
}

func TestConformance_AnAttributeTheServerRefusedIsNotAnEmptyOne(t *testing.T) {
	// The response says kXR_ok and the attribute says ENOATTR. The second is
	// the answer; the first only says the request was understood.
	const rc = 61 // ENODATA on Linux: the attribute is not set.

	for _, tc := range []struct {
		name string
		call func(fs *fileSystem) error
	}{
		{"getting", func(fs *fileSystem) error {
			_, err := fs.GetXAttr(context.Background(), "/tmp/f.bin", "user.checksum")
			return err
		}},
		{"setting", func(fs *fileSystem) error {
			return fs.SetXAttr(context.Background(), "/tmp/f.bin", "user.checksum", []byte("adler32:0"))
		}},
		{"deleting", func(fs *fileSystem) error {
			return fs.DelXAttr(context.Background(), "/tmp/f.bin", "user.checksum")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clientFunc := func(cancel func(), client *Client) {
				err := tc.call(client.FS().(*fileSystem))
				if err == nil {
					t.Error("an attribute the server refused was reported as done")
					return
				}
				if !strings.Contains(err.Error(), "status code 61") {
					t.Errorf("the failure does not carry the server's status code: %v", err)
				}
				if !strings.Contains(err.Error(), "user.checksum") {
					t.Errorf("the failure does not name the attribute: %v", err)
				}
			}

			testClientWithMockServer(fattrReplying(t, fattrRaw("user.checksum", rc, nil)), clientFunc)
		})
	}
}

func TestConformance_AnAttributeResponseTooShortToDecodeIsAnError(t *testing.T) {
	// Three bytes cannot hold a return code and a name. Reading a zero out of
	// a body that never carried one would report success for an operation
	// whose outcome is unknown.
	for _, tc := range []struct {
		name string
		call func(fs *fileSystem) error
	}{
		{"getting", func(fs *fileSystem) error {
			_, err := fs.GetXAttr(context.Background(), "/tmp/f.bin", "user.checksum")
			return err
		}},
		{"setting", func(fs *fileSystem) error {
			return fs.SetXAttr(context.Background(), "/tmp/f.bin", "user.checksum", []byte("adler32:0"))
		}},
		{"deleting", func(fs *fileSystem) error {
			return fs.DelXAttr(context.Background(), "/tmp/f.bin", "user.checksum")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clientFunc := func(cancel func(), client *Client) {
				err := tc.call(client.FS().(*fileSystem))
				if err == nil {
					t.Error("a truncated attribute response was accepted")
					return
				}
				if !strings.Contains(err.Error(), "too short") {
					t.Errorf("the failure does not say the response was truncated: %v", err)
				}
			}

			testClientWithMockServer(fattrReplying(t, []byte{0, 1, 0}), clientFunc)
		})
	}
}

func TestConformance_AnAttributeThatIsThereComesBackWhole(t *testing.T) {
	// The positive control for the two above: a return code of zero is the
	// only case where the value may be handed to the caller.
	want := []byte("adler32:deadbeef")

	clientFunc := func(cancel func(), client *Client) {
		got, err := client.FS().(*fileSystem).GetXAttr(context.Background(), "/tmp/f.bin", "user.checksum")
		if err != nil {
			t.Errorf("could not read the attribute: %v", err)
			return
		}
		if string(got) != string(want) {
			t.Errorf("the attribute reads %q, want %q", got, want)
		}
	}

	testClientWithMockServer(fattrReplying(t, fattrRaw("user.checksum", 0, want)), clientFunc)
}
