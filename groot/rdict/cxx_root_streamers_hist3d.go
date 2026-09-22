// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rdict

import (
	"fmt"

	"go-hep.org/x/hep/groot/rbase"
	"go-hep.org/x/hep/groot/rbytes"
	"go-hep.org/x/hep/groot/rmeta"
	"go-hep.org/x/hep/groot/rvers"
)

// The streamers of the 3-dim histograms, transcribed from the ROOT headers
// that declare them (hist/hist/inc/TH3.h and core/base/inc/TAtt3D.h), rather
// than dumped from a running ROOT the way cxx_root_streamers_gen.go was.
//
// Each class states the version its ClassDef gives it and the data members it
// declares, in declaration order. Everything else — the checksums, both the
// class' own and the one every base carries — is computed, so there is no
// magic number here to get wrong or to leave behind when a version moves.
//
// This file registers streamers whose bases (TH1, TArrayC and the rest) are
// registered by cxx_root_streamers_gen.go. Go runs a package's init functions
// in filename order and this file sorts after that one, which is what makes
// the bases available below; cxxBase says so loudly if that ever stops
// being true.

// cxxBase returns the streamer element for a base class, carrying the base's
// checksum the way C++ ROOT writes it.
//
// cxxBase panics if the base class is not registered yet: its checksum would
// silently come out as zero, and a wrong checksum is exactly what this file
// exists to avoid.
func cxxBase(name, title string, vers int16) *StreamerBase {
	si, ok := StreamerInfos.Get(name, int(vers))
	if !ok {
		panic(fmt.Errorf("rdict: no streamer for base class %q (version=%d)", name, vers))
	}

	return NewStreamerBase(Element{
		Name:   *rbase.NewNamed(name, title),
		Type:   rmeta.Base,
		Size:   0,
		MaxIdx: [5]int32{0, int32(si.CheckSum()), 0, 0, 0},
		EName:  "BASE",
	}.New(), int32(vers))
}

// cxxF64 returns the streamer element for a Double_t data member.
func cxxF64(name, title string) *StreamerBasicType {
	return &StreamerBasicType{StreamerElement: Element{
		Name:  *rbase.NewNamed(name, title),
		Type:  rmeta.Double,
		Size:  8,
		EName: "double",
	}.New()}
}

// cxxSI returns a streamer info carrying the checksum C++ ROOT computes for it.
func cxxSI(name string, vers int16, elems []rbytes.StreamerElement) *StreamerInfo {
	return NewCxxStreamerInfo(name, int32(vers), CxxCheckSum(name, elems), elems)
}

func init() {
	// TAtt3D holds no data: it is a marker class saying "this can be drawn
	// in 3 dimensions". It still streams a header of its own.
	StreamerInfos.Add(cxxSI("TAtt3D", rvers.Att3D, []rbytes.StreamerElement{}))

	// TH3 : public TH1, public TAtt3D
	StreamerInfos.Add(cxxSI("TH3", rvers.H3, []rbytes.StreamerElement{
		cxxBase("TH1", "1-Dim histogram base class", rvers.H1),
		cxxBase("TAtt3D", "3D attributes", rvers.Att3D),
		cxxF64("fTsumwy", "Total Sum of weight*Y"),
		cxxF64("fTsumwy2", "Total Sum of weight*Y*Y"),
		cxxF64("fTsumwxy", "Total Sum of weight*X*Y"),
		cxxF64("fTsumwz", "Total Sum of weight*Z"),
		cxxF64("fTsumwz2", "Total Sum of weight*Z*Z"),
		cxxF64("fTsumwxz", "Total Sum of weight*X*Z"),
		cxxF64("fTsumwyz", "Total Sum of weight*Y*Z"),
	}))

	// TH3x : public TH3, public TArrayx — the payload and nothing else.
	for _, tc := range []struct {
		name  string
		vers  int16
		array string
		title string
		avers int16
	}{
		{"TH3C", rvers.H3C, "TArrayC", "Array of chars", rvers.ArrayC},
		{"TH3D", rvers.H3D, "TArrayD", "Array of doubles", rvers.ArrayD},
		{"TH3F", rvers.H3F, "TArrayF", "Array of floats", rvers.ArrayF},
		{"TH3I", rvers.H3I, "TArrayI", "Array of ints", rvers.ArrayI},
		{"TH3S", rvers.H3S, "TArrayS", "Array of shorts", rvers.ArrayS},
	} {
		StreamerInfos.Add(cxxSI(tc.name, tc.vers, []rbytes.StreamerElement{
			cxxBase("TH3", "3-Dim histogram base class", rvers.H3),
			cxxBase(tc.array, tc.title, tc.avers),
		}))
	}
}

// cxxI32 returns the streamer element for an Int_t data member.
func cxxI32(name, title string) *StreamerBasicType {
	return &StreamerBasicType{StreamerElement: Element{
		Name:  *rbase.NewNamed(name, title),
		Type:  rmeta.Int,
		Size:  4,
		EName: "int",
	}.New()}
}

func init() {
	// TF2 : public TF1, and TF3 : public TF2, transcribed from TF2.h/TF3.h.
	// Both leave their clip-box and painter members out: those are marked
	// transient in the headers and never reach a file.
	StreamerInfos.Add(cxxSI("TF2", rvers.F2, []rbytes.StreamerElement{
		cxxBase("TF1", "The Parametric 1-D function", rvers.F1),
		cxxF64("fYmin", "Lower bound for the range in y"),
		cxxF64("fYmax", "Upper bound for the range in y"),
		cxxI32("fNpy", "Number of points along y used for the graphical representation"),
	}))

	StreamerInfos.Add(cxxSI("TF3", rvers.F3, []rbytes.StreamerElement{
		cxxBase("TF2", "The Parametric 2-D function", rvers.F2),
		cxxF64("fZmin", "Lower bound for the range in z"),
		cxxF64("fZmax", "Upper bound for the range in z"),
		cxxI32("fNpz", "Number of points along z used for the graphical representation"),
	}))
}
