// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdproto

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// header builds a raw response header announcing dlen body bytes.
func header(dlen int32) []byte {
	hdr := make([]byte, ResponseHeaderLength)
	binary.BigEndian.PutUint32(hdr[4:8], uint32(dlen))
	return hdr
}

func TestReadResponseRejectsOversizedBody(t *testing.T) {
	for _, tc := range []struct {
		name string
		dlen int32
		want string
	}{
		{
			name: "beyond the cap",
			dlen: MaxResponseLength + 1,
			want: "exceeds",
		},
		{
			// The wire field is a signed 32-bit integer, so a peer can
			// announce a negative length. It must not reach make().
			name: "negative",
			dlen: -1,
			want: "negative",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Only the header is provided: a correct implementation refuses
			// before trying to read (or allocate) the body at all.
			_, _, err := ReadResponse(bytes.NewReader(header(tc.dlen)))
			if err == nil {
				t.Fatalf("got no error, want one mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got error %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestReadResponseAcceptsBodyAtTheCap(t *testing.T) {
	// The bound is inclusive: a body of exactly MaxResponseLength must still
	// be accepted, or the constant means one byte less than it says. The body
	// is deliberately absent so the test does not stream 64 MiB — reaching the
	// short read at all proves the length itself was let through.
	_, _, err := ReadResponse(bytes.NewReader(header(MaxResponseLength)))
	if err == nil {
		t.Fatal("got no error from a truncated body")
	}
	if strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("a body of exactly MaxResponseLength was refused: %v", err)
	}
}

func TestReadResponseNormalBody(t *testing.T) {
	const dlen = 1024
	raw := append(header(dlen), bytes.Repeat([]byte{0xab}, dlen)...)

	_, data, err := ReadResponse(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("ReadResponse: %v", err)
	}
	if len(data) != dlen {
		t.Fatalf("got %d bytes, want %d", len(data), dlen)
	}
}
