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

// The streamers of the classes groot learnt after the ROOT streamer dump in
// cxx_root_streamers_gen.go was taken, transcribed from the ROOT headers that
// declare them rather than read out of a running ROOT.
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

// cxxBool returns the streamer element for a Bool_t data member.
func cxxBool(name, title string) *StreamerBasicType {
	return &StreamerBasicType{StreamerElement: Element{
		Name:  *rbase.NewNamed(name, title),
		Type:  rmeta.Bool,
		Size:  1,
		EName: "bool",
	}.New()}
}

// cxxCounter returns the streamer element for the Int_t data member that
// counts the entries of a variable-length array.
func cxxCounter(name, title string) *StreamerBasicType {
	return &StreamerBasicType{StreamerElement: Element{
		Name:  *rbase.NewNamed(name, title),
		Type:  rmeta.Counter,
		Size:  4,
		EName: "int",
	}.New()}
}

// cxxArrF64 returns the streamer element for a Double_t* data member whose
// length is held by the counter member cnt of class cls.
func cxxArrF64(name, title, cnt, cls string, vers int16) *StreamerBasicPointer {
	return NewStreamerBasicPointer(Element{
		Name:  *rbase.NewNamed(name, title),
		Type:  rmeta.OffsetP + rmeta.Double,
		Size:  8,
		EName: "double*",
	}.New(), int32(vers), cnt, cls)
}

// cxxObjPtr returns the streamer element for a pointer to a ROOT object.
func cxxObjPtr(name, title, ename string) *StreamerObjectPointer {
	return &StreamerObjectPointer{StreamerElement: Element{
		Name:  *rbase.NewNamed(name, title),
		Type:  rmeta.ObjectP,
		EName: ename,
	}.New()}
}

func init() {
	// TGraph2D : public TNamed, TAttLine, TAttFill, TAttMarker.
	//
	// Its interpolating histogram, Delaunay triangulation, painter and the
	// real size of its arrays are all marked transient in TGraph2D.h and so
	// are absent here.
	graph2d := []rbytes.StreamerElement{
		cxxBase("TNamed", "The basis for a named object (name, title)", rvers.Named),
		cxxBase("TAttLine", "Line attributes", rvers.AttLine),
		cxxBase("TAttFill", "Fill area attributes", rvers.AttFill),
		cxxBase("TAttMarker", "Marker attributes", rvers.AttMarker),
		cxxCounter("fNpoints", "Number of points in the data set"),
		cxxI32("fNpx", "Number of bins along X in fHistogram"),
		cxxI32("fNpy", "Number of bins along Y in fHistogram"),
		cxxI32("fMaxIter", "Maximum number of iterations to find Delaunay triangles"),
		cxxArrF64("fX", "[fNpoints]", "fNpoints", "TGraph2D", rvers.Graph2D),
		cxxArrF64("fY", "[fNpoints] Data set to be plotted", "fNpoints", "TGraph2D", rvers.Graph2D),
		cxxArrF64("fZ", "[fNpoints]", "fNpoints", "TGraph2D", rvers.Graph2D),
		cxxF64("fMinimum", "Minimum value for plotting along z"),
		cxxF64("fMaximum", "Maximum value for plotting along z"),
		cxxF64("fMargin", "Extra space (in %) around interpolated area for fHistogram"),
		cxxF64("fZout", "fHistogram bin height for points lying outside the interpolated area"),
		cxxObjPtr("fFunctions", "Pointer to list of functions (fits and user)", "TList*"),
		cxxBool("fUserHisto", "True when SetHistogram has been called"),
	}
	StreamerInfos.Add(cxxSI("TGraph2D", rvers.Graph2D, graph2d))

	// TGraph2DErrors : public TGraph2D, adding an error on each coordinate.
	StreamerInfos.Add(cxxSI("TGraph2DErrors", rvers.Graph2DErrors, []rbytes.StreamerElement{
		cxxBase("TGraph2D", "Set of n x[n],y[n],z[n] points with 3-d graphics including Delaunay triangulation", rvers.Graph2D),
		cxxArrF64("fEX", "[fNpoints] array of X errors", "fNpoints", "TGraph2DErrors", rvers.Graph2DErrors),
		cxxArrF64("fEY", "[fNpoints] array of Y errors", "fNpoints", "TGraph2DErrors", rvers.Graph2DErrors),
		cxxArrF64("fEZ", "[fNpoints] array of Z errors", "fNpoints", "TGraph2DErrors", rvers.Graph2DErrors),
	}))
}

// cxxArrayD returns the streamer element for a TArrayD data member.
func cxxArrayD(name, title string) *StreamerObjectAny {
	return &StreamerObjectAny{StreamerElement: Element{
		Name:  *rbase.NewNamed(name, title),
		Type:  rmeta.Any,
		Size:  24,
		EName: "TArrayD",
	}.New()}
}

// cxxEnum returns the streamer element for an enum data member, which reaches
// the file as an int and names itself in the file as the enum it is.
func cxxEnum(name, title, ename string) *StreamerBasicType {
	return &StreamerBasicType{StreamerElement: Element{
		Name:  *rbase.NewNamed(name, title),
		Type:  rmeta.Int,
		Size:  4,
		EName: ename,
	}.New()}
}

func init() {
	// TProfile3D : public TH3D, laid out the way TProfile2D is one dimension
	// down. fScaling is transient in TProfile3D.h and so is absent here.
	StreamerInfos.Add(cxxSI("TProfile3D", rvers.Profile3D, []rbytes.StreamerElement{
		cxxBase("TH3D", "3-Dim histograms (one double per channel)", rvers.H3D),
		cxxArrayD("fBinEntries", "Number of entries per bin"),
		cxxEnum("fErrorMode", "Option to compute errors", "EErrorType"),
		cxxF64("fTmin", "Lower limit in T (if set)"),
		cxxF64("fTmax", "Upper limit in T (if set)"),
		cxxF64("fTsumwt", "Total Sum of weight*T"),
		cxxF64("fTsumwt2", "Total Sum of weight*T*T"),
		cxxArrayD("fBinSumw2", "Array of sum of squares of weights per bin"),
	}))
}
