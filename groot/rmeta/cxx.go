// Copyright ©2017 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rmeta

import (
	"fmt"
	"reflect"
	"strings"

	"go-hep.org/x/hep/groot/root"
)

var GoType2ROOTEnum = map[reflect.Type]Enum{
	reflect.TypeFor[int8]():  Char,
	reflect.TypeFor[int16](): Short,
	reflect.TypeFor[int32](): Int,
	reflect.TypeFor[int64](): Long,
	//	reflect.TypeOf(int64(0)): Long64,
	reflect.TypeFor[float32](): Float,
	reflect.TypeFor[float64](): Double,
	reflect.TypeFor[uint8]():   UChar,
	//	reflect.TypeOf(uint8(0)): CharStar,
	reflect.TypeFor[uint16](): UShort,
	reflect.TypeFor[uint32](): UInt,
	reflect.TypeFor[uint64](): ULong,
	//	reflect.TypeOf(uint64(0)): ULong64,
	reflect.TypeFor[bool]():          Bool,
	reflect.TypeFor[root.Float16]():  Float16,
	reflect.TypeFor[root.Double32](): Double32,
	reflect.TypeFor[string]():        TString,
}

var GoType2Cxx = map[string]string{
	"bool":    "bool",
	"byte":    "unsigned char",
	"uint":    "unsigned int",
	"uint8":   "unsigned char",
	"uint16":  "unsigned short",
	"uint32":  "unsigned int",
	"uint64":  "unsigned long",
	"int":     "int",
	"int8":    "char",
	"int16":   "short",
	"int32":   "int",
	"int64":   "long",
	"float32": "float",
	"float64": "double",
}

var CxxBuiltins = map[string]reflect.Type{
	"bool": reflect.TypeFor[bool](),

	/*
		"uint":   reflect.TypeOf(uint(0)),
		"uint8":  reflect.TypeOf(uint8(0)),
		"uint16": reflect.TypeOf(uint16(0)),
		"uint32": reflect.TypeOf(uint32(0)),
		"uint64": reflect.TypeOf(uint64(0)),

		"int":   reflect.TypeOf(int(0)),
		"int8":  reflect.TypeOf(int8(0)),
		"int16": reflect.TypeOf(int16(0)),
		"int32": reflect.TypeOf(int32(0)),
		"int64": reflect.TypeOf(int64(0)),

		"float32": reflect.TypeOf(float32(0)),
		"float64": reflect.TypeOf(float64(0)),
	*/

	// stdint
	"int8_t":   reflect.TypeFor[int8](),
	"int16_t":  reflect.TypeFor[int16](),
	"int32_t":  reflect.TypeFor[int32](),
	"int64_t":  reflect.TypeFor[int64](),
	"uint8_t":  reflect.TypeFor[uint8](),
	"uint16_t": reflect.TypeFor[uint16](),
	"uint32_t": reflect.TypeFor[uint32](),
	"uint64_t": reflect.TypeFor[uint64](),

	// C/C++ builtins

	"unsigned":       reflect.TypeFor[uint32](),
	"unsigned char":  reflect.TypeFor[uint8](),
	"unsigned short": reflect.TypeFor[uint16](),
	"unsigned int":   reflect.TypeFor[uint32](),
	"unsigned long":  reflect.TypeFor[uint64](),

	//"int":   reflect.TypeOf(int(0)),
	"char":  reflect.TypeFor[int8](),
	"short": reflect.TypeFor[int16](),
	"int":   reflect.TypeFor[int32](),
	"long":  reflect.TypeFor[int64](),

	"float":  reflect.TypeFor[float32](),
	"double": reflect.TypeFor[float64](),

	"string": reflect.TypeFor[string](),

	// ROOT builtins
	"Bool_t": reflect.TypeFor[bool](),

	"Byte_t": reflect.TypeFor[uint8](),

	"Char_t":    reflect.TypeFor[int8](),
	"UChar_t":   reflect.TypeFor[uint8](),
	"Short_t":   reflect.TypeFor[int16](),
	"UShort_t":  reflect.TypeFor[uint16](),
	"Int_t":     reflect.TypeFor[int32](),
	"UInt_t":    reflect.TypeFor[uint32](),
	"Seek_t":    reflect.TypeFor[int64](),  // FIXME(sbinet): not portable
	"Long_t":    reflect.TypeFor[int64](),  // FIXME(sbinet): not portable
	"ULong_t":   reflect.TypeFor[uint64](), // FIXME(sbinet): not portable
	"Long64_t":  reflect.TypeFor[int64](),
	"ULong64_t": reflect.TypeFor[uint64](),

	"Float_t":    reflect.TypeFor[float32](),
	"Float16_t":  reflect.TypeFor[root.Float16](),
	"Double_t":   reflect.TypeFor[float64](),
	"Double32_t": reflect.TypeFor[root.Double32](),

	"Version_t": reflect.TypeFor[int16](),
	"Option_t":  reflect.TypeFor[string](),
	"Ssiz_t":    reflect.TypeFor[int](),
	"Real_t":    reflect.TypeFor[float32](),

	"Axis_t": reflect.TypeFor[float64](),
	"Stat_t": reflect.TypeFor[float64](),

	"Font_t":   reflect.TypeFor[int16](),
	"Style_t":  reflect.TypeFor[int16](),
	"Marker_t": reflect.TypeFor[int16](),
	"Width_t":  reflect.TypeFor[int16](),
	"Color_t":  reflect.TypeFor[int16](),
	"SCoord_t": reflect.TypeFor[int16](),
	"Coord_t":  reflect.TypeFor[float64](),
	"Angle_t":  reflect.TypeFor[float32](),
	"Size_t":   reflect.TypeFor[float32](),
}

func STLNameFrom(name string, vtype ESTLType, ctype Enum) string {
	if ctype == Object {
		return name
	}
	return STLNameFor(vtype, ctype)
}

// STLNameFor creates a regular C++ STL container name given a STL enum type
// and a ROOT enum value for the contained element.
func STLNameFor(vtype ESTLType, ctype Enum) string {
	ename := rmeta2Name(ctype)
	if strings.HasSuffix(ename, ">") {
		ename += " "
	}

	typfmt := "%s"
	switch vtype {
	case STLvector:
		typfmt = "vector<%s>"
	case STLlist:
		typfmt = "list<%s>"
	case STLdeque:
		typfmt = "deque<%s>"
	case STLmap:
		typfmt = "map<%s>"
	case STLmultimap:
		typfmt = "multimap<%s>"
	case STLset:
		typfmt = "set<%s>"
	case STLmultiset:
		typfmt = "multiset<%s>"
	case STLbitset:
		typfmt = "bitset<%s>"
	case STLforwardlist:
		typfmt = "forward_list<%s>"
	case STLunorderedset:
		typfmt = "unordered_set<%s>"
	case STLunorderedmultiset:
		typfmt = "unordered_multiset<%s>"
	case STLunorderedmap:
		typfmt = "unordered_map<%s>"
	case STLunorderedmultimap:
		typfmt = "unordered_multimap<%s>"
	}

	return fmt.Sprintf(typfmt, ename)
}

func rmeta2Name(t Enum) string {
	switch t {
	case Char:
		return "char"
	case Short:
		return "short"
	case Int:
		return "int"
	case Long:
		return "long"
	case Float:
		return "float"
	case Counter:
		return "int"
	case CharStar:
		return "char*"
	case Double:
		return "double"
	case Double32:
		return "Double32_t"
	case LegacyChar:
		return "char" // FIXME(sbinet)
	case UChar:
		return "unsigned char"
	case UShort:
		return "unsigned short"
	case UInt:
		return "unsigned int"
	case ULong:
		return "unsigned long"
	case Bits:
		return "bits" // FIXME(sbinet)
	case Long64:
		return "Long64_t"
	case ULong64:
		return "ULong64_t"
	case Bool:
		return "bool"
	case Float16:
		return "Float16_t"
	case Object:
		// class derived from TObject
		return "TObject" // FIXME(sbinet): better handling?
	case TString:
		return "TString"
	case TObject:
		return "TObject"
	case TNamed:
		return "TNamed"
	case STLstring:
		return "string"
	}
	panic(fmt.Errorf("not implemented: t=%v (%d)", t, t))
}
