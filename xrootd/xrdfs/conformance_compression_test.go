// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdfs_test

import (
	"reflect"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
)

// TestConformance_FileCompressionIsEightBytesOnTheWire pins the shape the
// kXR_open response carries when a server reports compression: a 4-byte
// big-endian page size followed by a fixed 4-byte algorithm name, padded with
// NULs and never length-prefixed. The name sits inside the same 12-byte
// response area as the file handle, so a byte too many or too few here shifts
// the handle and the client goes on to read a file it did not open.
func TestConformance_FileCompressionIsEightBytesOnTheWire(t *testing.T) {
	for _, tc := range []struct {
		name string
		comp xrdfs.FileCompression
		want []byte
	}{
		{
			name: "an uncompressed file",
			comp: xrdfs.FileCompression{},
			want: []byte{0, 0, 0, 0, 0, 0, 0, 0},
		},
		{
			name: "a 64 KiB page",
			comp: xrdfs.FileCompression{PageSize: 65536, Type: [4]byte{'z', 'i', 'p', 0}},
			want: []byte{0x00, 0x01, 0x00, 0x00, 'z', 'i', 'p', 0},
		},
		{
			name: "a four-character algorithm fills the field",
			comp: xrdfs.FileCompression{PageSize: 1, Type: [4]byte{'b', 'z', 'i', 'p'}},
			want: []byte{0, 0, 0, 1, 'b', 'z', 'i', 'p'},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var w xrdenc.WBuffer
			if err := tc.comp.MarshalXrd(&w); err != nil {
				t.Fatalf("could not marshal: %v", err)
			}
			if got := w.Bytes(); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("marshalled to %v, want %v", got, tc.want)
			}

			var got xrdfs.FileCompression
			if err := got.UnmarshalXrd(xrdenc.NewRBuffer(tc.want)); err != nil {
				t.Fatalf("could not unmarshal: %v", err)
			}
			if got != tc.comp {
				t.Fatalf("round trip changed the value:\ngot = %+v\nwant= %+v", got, tc.comp)
			}
		})
	}
}

// TestConformance_TruncatedCompressionInfoIsAnError checks the decoder reports
// a short response instead of returning a half-filled value: the page size a
// caller reads decides how it chunks every subsequent request.
func TestConformance_TruncatedCompressionInfoIsAnError(t *testing.T) {
	full := []byte{0, 0, 0, 1, 'z', 'i', 'p', 0}
	for n := range len(full) {
		var got xrdfs.FileCompression
		if err := got.UnmarshalXrd(xrdenc.NewRBuffer(full[:n])); err == nil {
			t.Errorf("%d bytes of compression info decoded without an error", n)
		}
	}
}
