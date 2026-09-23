// Copyright ©2020 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package rntup reads RNTuples, the columnar storage that ROOT 7 introduced
// as the successor to TTree.
//
// An RNTuple is opened by name out of a ROOT file, its fields are bound to
// Go values, and its entries are walked:
//
//	r, err := rntup.Open("data.root", "ntuple")
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer r.Close()
//
//	var (
//		n  int32
//		xs []float32
//	)
//	rvars := []rntup.ReadVar{
//		{Name: "n", Value: &n},
//		{Name: "xs", Value: &xs},
//	}
//
//	err = r.Read(rvars, func(entry uint64) error {
//		fmt.Printf("entry %d: n=%d xs=%v\n", entry, n, xs)
//		return nil
//	})
//
// NewReadVars builds that list from the schema when it is not known ahead of
// time, allocating a value of the Go type each field calls for.
//
// # C++ types
//
// The fundamental types map onto the Go type of the same width, with bool
// for a boolean and float32 for the half precision and the lossy real
// columns. A std::string reads into a string, a collection into a slice, a
// fixed-size array into an array, and a struct, pair or tuple into a struct
// this package builds from the schema. A std::variant reads into an any
// holding whichever alternative was live, or nil when it held none.
//
// A field may be bound to a type of the caller's own rather than the one it
// reads into, as long as the two line up: numbers convert, and the members
// of a struct are matched by name, ignoring case and underscores, or by
// position when there is no other way to tell.
//
// Objects written with the ROOT streamer are not supported yet.
//
// # Writing
//
// Create makes an RNTuple, binding each field to a Go value that Write reads
// one entry from:
//
//	var (
//		n  int32
//		xs []float64
//	)
//	w, err := rntup.Create("out.root", "ntuple", []rntup.WriteVar{
//		{Name: "n", Value: &n},
//		{Name: "xs", Value: &xs},
//	})
//	defer w.Close()
//
//	for i := range 1000 {
//		n, xs = int32(i), []float64{float64(i)}
//		err = w.Write()
//	}
//
// The schema is worked out from the Go types, and the C++ type each one
// stands for is recorded so that another reader knows what it is looking at:
// an int32 is a std::int32_t, a string is a std::string, a slice is a
// std::vector and a struct is a record whose members are named by their
// "rntup" tags. Pages are compressed one at a time.
//
// What is written is written at version 1.0.0.0 of the format, with the
// split column encodings, as ROOT writes: splitting rearranges a page so
// that the bytes of its elements sit beside the bytes that resemble them,
// which costs nothing and roughly halves what is left after compression.
// PlainEncoding turns that off, for comparing the two.
//
// # What is not here
//
// Objects written with the ROOT streamer.
package rntup // import "go-hep.org/x/hep/groot/exp/rntup"

import (
	"fmt"
	"reflect"

	"go-hep.org/x/hep/groot/rbytes"
	"go-hep.org/x/hep/groot/root"
	"go-hep.org/x/hep/groot/rtypes"
)

type span struct {
	seek   uint64
	nbytes uint32
	length uint32
}

type NTuple struct {
	rvers uint32
	size  uint32

	header span
	footer span

	reserved uint64
}

func (*NTuple) Class() string {
	return "ROOT::Experimental::RNTuple"
}

func (*NTuple) RVersion() int16 {
	return 1 // FIXME(sbinet): generate through gen.rboot
}

func (nt *NTuple) String() string {
	return fmt.Sprintf("NTuple{version:%d, size:%d, header:%v, footer:%v}",
		nt.rvers, nt.size, nt.header, nt.footer,
	)
}

func (nt *NTuple) MarshalROOT(w *rbytes.WBuffer) (int, error) {
	if w.Err() != nil {
		return 0, w.Err()
	}

	hdr := w.WriteHeader(nt.Class(), nt.RVersion())

	w.WriteU32(nt.rvers)
	w.WriteU32(nt.size)

	w.WriteU64(nt.header.seek)
	w.WriteU32(nt.header.nbytes)
	w.WriteU32(nt.header.length)

	w.WriteU64(nt.footer.seek)
	w.WriteU32(nt.footer.nbytes)
	w.WriteU32(nt.footer.length)

	w.WriteU64(nt.reserved)

	return w.SetHeader(hdr)
}

func (nt *NTuple) UnmarshalROOT(r *rbytes.RBuffer) error {
	if r.Err() != nil {
		return r.Err()
	}

	hdr := r.ReadHeader(nt.Class(), nt.RVersion())

	nt.rvers = r.ReadU32()
	nt.size = r.ReadU32()

	nt.header.seek = r.ReadU64()
	nt.header.nbytes = r.ReadU32()
	nt.header.length = r.ReadU32()

	nt.footer.seek = r.ReadU64()
	nt.footer.nbytes = r.ReadU32()
	nt.footer.length = r.ReadU32()

	nt.reserved = r.ReadU64()

	r.CheckHeader(hdr)
	return r.Err()
}

func init() {
	{
		f := func() reflect.Value {
			o := &NTuple{}
			return reflect.ValueOf(o)
		}
		rtypes.Factory.Add("ROOT::Experimental::RNTuple", f)
	}
}

var (
	_ root.Object        = (*NTuple)(nil)
	_ rbytes.RVersioner  = (*NTuple)(nil)
	_ rbytes.Marshaler   = (*NTuple)(nil)
	_ rbytes.Unmarshaler = (*NTuple)(nil)
)
