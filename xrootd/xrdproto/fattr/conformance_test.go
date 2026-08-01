// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the extended-attribute response decoder, where the body is
// self-describing and the header says nothing about its length.
//
// A kXR_fattr reply is a count, a status code, a NUL-terminated name and a
// length-prefixed value. Both delimiters are attacker-supplied: a name with no
// NUL and a length larger than the bytes that follow are what a truncated or
// hostile response looks like. Reading them without checking is a slice out of
// range in the first case and, in the second, a read past the value into
// whatever else the buffer holds.

package fattr

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestConformance_AnAttributeNameWithNoEndIsNotAName(t *testing.T) {
	// Counts, a status code, and then bytes that never terminate.
	resp := Response{Raw: []byte{0, 1, 0, 0, 'u', 's', 'e', 'r', '.', 'x'}}

	_, _, _, err := resp.Attr()
	if err == nil {
		t.Fatal("an unterminated attribute name was accepted")
	}
	if !strings.Contains(err.Error(), "unterminated") {
		t.Fatalf("the failure says %q, want it to say the name never ended", err)
	}
}

func TestConformance_AnAttributeLongerThanItsResponseIsRejected(t *testing.T) {
	// The declared value length runs past the end of the body. Slicing to it
	// would either panic or, with a longer buffer behind it, hand the caller
	// bytes that belong to something else.
	raw := []byte{0, 1, 0, 0}
	raw = append(raw, "user.checksum"...)
	raw = append(raw, 0)
	raw = binary.BigEndian.AppendUint32(raw, 4096)
	raw = append(raw, "adler32:0"...)

	resp := Response{Raw: raw}
	_, _, _, err := resp.Attr()
	if err == nil {
		t.Fatal("a value length past the end of the response was accepted")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("the failure says %q, want it to say the length is past the end", err)
	}
}

func TestConformance_AnAttributeWithNoValueIsStillAnAnswer(t *testing.T) {
	// A del or set reply carries a name and a status code and no value at all;
	// the missing length prefix is not an error, it is the shape of the reply.
	raw := []byte{0, 1}
	raw = binary.BigEndian.AppendUint16(raw, 3)
	raw = append(raw, "user.checksum"...)
	raw = append(raw, 0)

	resp := Response{Raw: raw}
	name, rc, value, err := resp.Attr()
	if err != nil {
		t.Fatalf("could not decode a valueless reply: %v", err)
	}
	if name != "user.checksum" {
		t.Errorf("the reply names %q", name)
	}
	if rc != 3 {
		t.Errorf("the reply carries status code %d, want 3", rc)
	}
	if len(value) != 0 {
		t.Errorf("the reply carries %d bytes of value", len(value))
	}
}

func TestConformance_AnEmptyAttributeListIsNoNamesRatherThanOne(t *testing.T) {
	// A directory with no attributes answers with padding and nothing else.
	// Splitting that on NUL yields one empty name, and a caller that iterates
	// it goes on to query an attribute called "".
	for _, tc := range []struct {
		name string
		raw  []byte
	}{
		{"an empty body", nil},
		{"nothing but padding", []byte{0, 0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := Response{Raw: tc.raw}
			names, err := resp.Names()
			if err != nil {
				t.Fatalf("could not decode the list: %v", err)
			}
			if len(names) != 0 {
				t.Fatalf("an empty list decoded to %q", names)
			}
		})
	}

	// The control: two names and their padding.
	resp := Response{Raw: []byte("user.a\x00user.b\x00")}
	names, err := resp.Names()
	if err != nil {
		t.Fatalf("could not decode the list: %v", err)
	}
	if len(names) != 2 || names[0] != "user.a" || names[1] != "user.b" {
		t.Fatalf("the list decoded to %q", names)
	}
}
