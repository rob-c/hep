// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rntup_test

import (
	"fmt"
	"log"

	"go-hep.org/x/hep/groot/exp/rntup"
)

func ExampleReader() {
	r, err := rntup.Open("../../testdata/rntuple/test_int_float_rntuple_v1-0-0-0.root", "ntuple")
	if err != nil {
		log.Fatalf("could not open RNTuple: %+v", err)
	}
	defer r.Close()

	fmt.Printf("ntuple %q: %d entries\n", r.Name(), r.Entries())

	var (
		i     int32
		f     float32
		rvars = []rntup.ReadVar{
			{Name: "one_integers", Value: &i},
			{Name: "two_floats", Value: &f},
		}
	)

	err = r.ReadRange(rvars, 0, 4, func(entry uint64) error {
		fmt.Printf("entry %d: one_integers=%d two_floats=%.1f\n", entry, i, f)
		return nil
	})
	if err != nil {
		log.Fatalf("could not read RNTuple: %+v", err)
	}

	// Output:
	// ntuple "ntuple": 10 entries
	// entry 0: one_integers=9 two_floats=9.9
	// entry 1: one_integers=8 two_floats=8.8
	// entry 2: one_integers=7 two_floats=7.7
	// entry 3: one_integers=6 two_floats=6.6
}

// ExampleNewReadVars shows how to read an RNTuple whose schema is not known
// ahead of time.
func ExampleNewReadVars() {
	r, err := rntup.Open("../../testdata/rntuple/test_stl_containers_rntuple_v1-0-0-0.root", "ntuple")
	if err != nil {
		log.Fatalf("could not open RNTuple: %+v", err)
	}
	defer r.Close()

	rvars, err := rntup.NewReadVars(r)
	if err != nil {
		log.Fatalf("could not build read-vars: %+v", err)
	}

	for _, rvar := range rvars[:5] {
		typ, err := r.GoType(rvar.Name)
		if err != nil {
			log.Fatalf("could not resolve %q: %+v", rvar.Name, err)
		}
		fmt.Printf("%-22s %v\n", rvar.Name, typ)
	}

	// Output:
	// string                 string
	// vector_int32           []int32
	// array_float            [3]float32
	// vector_vector_int32    [][]int32
	// vector_string          []string
}
