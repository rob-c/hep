// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rntup

import (
	"encoding/binary"
	"fmt"
	"reflect"

	"github.com/zeebo/xxh3"
	"go-hep.org/x/hep/groot/rbytes"
	"go-hep.org/x/hep/groot/root"
	"go-hep.org/x/hep/groot/rtypes"
)

// Anchor is the ROOT::RNTuple object that a ROOT file holds to point at an
// RNTuple's header and footer envelopes.
//
// Unlike the rest of an RNTuple, the anchor is written as an ordinary ROOT
// key and so is big-endian.
type Anchor struct {
	VersionEpoch uint16 // version of the binary format the RNTuple was written with
	VersionMajor uint16
	VersionMinor uint16
	VersionPatch uint16

	SeekHeader   uint64 // byte offset of the header envelope, from the start of the file
	NBytesHeader uint64 // size of the header envelope on disk, compressed
	LenHeader    uint64 // size of the header envelope once uncompressed

	SeekFooter   uint64
	NBytesFooter uint64
	LenFooter    uint64

	MaxKeySize uint64 // largest RBlob a key may hold; bigger payloads span several

	name string
}

const (
	// anchorLen is the size of the anchor's fields, which is what the
	// trailing checksum is computed over.
	anchorLen = 4*2 + 7*8
)

// payload serialises the anchor's fields the way they appear on disk, which
// is what the trailing checksum is computed over.
func (a *Anchor) payload() []byte {
	var (
		buf = make([]byte, anchorLen)
		be  = binary.BigEndian
	)
	be.PutUint16(buf[0:], a.VersionEpoch)
	be.PutUint16(buf[2:], a.VersionMajor)
	be.PutUint16(buf[4:], a.VersionMinor)
	be.PutUint16(buf[6:], a.VersionPatch)
	be.PutUint64(buf[8:], a.SeekHeader)
	be.PutUint64(buf[16:], a.NBytesHeader)
	be.PutUint64(buf[24:], a.LenHeader)
	be.PutUint64(buf[32:], a.SeekFooter)
	be.PutUint64(buf[40:], a.NBytesFooter)
	be.PutUint64(buf[48:], a.LenFooter)
	be.PutUint64(buf[56:], a.MaxKeySize)
	return buf
}

func (*Anchor) Class() string {
	return "ROOT::RNTuple"
}

func (*Anchor) RVersion() int16 {
	return 2
}

// Name returns the name the RNTuple was written under.
func (a *Anchor) Name() string { return a.name }

func (a *Anchor) String() string {
	return fmt.Sprintf(
		"RNTuple{name: %q, version: %d.%d.%d.%d, header: {seek: %d, nbytes: %d, len: %d}, footer: {seek: %d, nbytes: %d, len: %d}}",
		a.name,
		a.VersionEpoch, a.VersionMajor, a.VersionMinor, a.VersionPatch,
		a.SeekHeader, a.NBytesHeader, a.LenHeader,
		a.SeekFooter, a.NBytesFooter, a.LenFooter,
	)
}

func (a *Anchor) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(a.Class(), a.RVersion())

	w.WriteU16(a.VersionEpoch)
	w.WriteU16(a.VersionMajor)
	w.WriteU16(a.VersionMinor)
	w.WriteU16(a.VersionPatch)

	w.WriteU64(a.SeekHeader)
	w.WriteU64(a.NBytesHeader)
	w.WriteU64(a.LenHeader)

	w.WriteU64(a.SeekFooter)
	w.WriteU64(a.NBytesFooter)
	w.WriteU64(a.LenFooter)

	w.WriteU64(a.MaxKeySize)

	n, err := w.SetHeader(hdr)
	if err != nil {
		return n, err
	}

	// the checksum covers the fields alone, and sits outside the byte
	// count the header declared.
	w.WriteU64(xxh3.Hash(a.payload()))

	return n + 8, w.Err()
}

func (a *Anchor) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(a.Class(), a.RVersion())

	a.VersionEpoch = r.ReadU16()
	a.VersionMajor = r.ReadU16()
	a.VersionMinor = r.ReadU16()
	a.VersionPatch = r.ReadU16()

	a.SeekHeader = r.ReadU64()
	a.NBytesHeader = r.ReadU64()
	a.LenHeader = r.ReadU64()

	a.SeekFooter = r.ReadU64()
	a.NBytesFooter = r.ReadU64()
	a.LenFooter = r.ReadU64()

	a.MaxKeySize = r.ReadU64()

	r.CheckHeader(hdr)
	if r.Err() != nil {
		return r.Err()
	}

	want := r.ReadU64()
	if r.Err() != nil {
		return r.Err()
	}
	if got := xxh3.Hash(a.payload()); got != want {
		return fmt.Errorf("rntup: anchor checksum mismatch: got=%#016x, want=%#016x", got, want)
	}

	if a.VersionEpoch != 1 {
		return fmt.Errorf(
			"rntup: unsupported RNTuple version epoch %d (only 1 is supported)",
			a.VersionEpoch,
		)
	}

	return nil
}

func init() {
	f := func() reflect.Value {
		return reflect.ValueOf(&Anchor{})
	}
	rtypes.Factory.Add("ROOT::RNTuple", f)
}

var (
	_ root.Object        = (*Anchor)(nil)
	_ rbytes.RVersioner  = (*Anchor)(nil)
	_ rbytes.Marshaler   = (*Anchor)(nil)
	_ rbytes.Unmarshaler = (*Anchor)(nil)
)
