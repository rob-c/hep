// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rdict

import (
	"go-hep.org/x/hep/groot/rbase"
	"go-hep.org/x/hep/groot/rbytes"
	"go-hep.org/x/hep/groot/rmeta"
)

// The streamer for the object a ROOT file holds to point at an RNTuple.
//
// The anchor is the only part of an RNTuple written as an ordinary ROOT
// object, so it is the only part that needs a streamer; the header, the
// footer and the pages are unnamed blobs that nothing streams.
//
// The members below are the ones ROOT writes, read back out of a file ROOT
// wrote. The checksum the test computes from them comes to what ROOT put in
// that file, which is how this is known to be right rather than merely
// plausible.
func init() {
	StreamerInfos.Add(cxxSI("ROOT::RNTuple", rntupleAnchorVersion, []rbytes.StreamerElement{
		cxxU16("fVersionEpoch", ""),
		cxxU16("fVersionMajor", ""),
		cxxU16("fVersionMinor", ""),
		cxxU16("fVersionPatch", ""),
		cxxU64("fSeekHeader", ""),
		cxxU64("fNBytesHeader", ""),
		cxxU64("fLenHeader", ""),
		cxxU64("fSeekFooter", ""),
		cxxU64("fNBytesFooter", ""),
		cxxU64("fLenFooter", ""),
		cxxU64("fMaxKeySize", ""),
	}))
}

// rntupleAnchorVersion is the class version ROOT writes the anchor with.
const rntupleAnchorVersion = 2

// cxxU16 returns the streamer element for a UShort_t data member.
func cxxU16(name, title string) *StreamerBasicType {
	return &StreamerBasicType{StreamerElement: Element{
		Name:   *rbase.NewNamed(name, title),
		Type:   rmeta.UShort,
		Size:   2,
		EName:  "unsigned short",
		MaxIdx: [5]int32{0, 0, 0, 0, 0},
	}.New()}
}

// cxxU64 returns the streamer element for a ULong64_t data member.
func cxxU64(name, title string) *StreamerBasicType {
	return &StreamerBasicType{StreamerElement: Element{
		Name:   *rbase.NewNamed(name, title),
		Type:   rmeta.ULong64,
		Size:   8,
		EName:  "ULong64_t",
		MaxIdx: [5]int32{0, 0, 0, 0, 0},
	}.New()}
}
