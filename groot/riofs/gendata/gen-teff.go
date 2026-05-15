// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build ignore

package main

import (
	"flag"
	"log"

	"go-hep.org/x/hep/groot/internal/rtests"
)

var (
	root = flag.String("f", "test-tefficiency.root", "output ROOT file")
)

func main() {
	flag.Parse()

	out, err := rtests.RunCxxROOT("gentefficiency", []byte(script), *root)
	if err != nil {
		log.Fatalf("could not run ROOT macro:\noutput:\n%v\nerror: %+v", string(out), err)
	}
}

const script = `
#include "TEfficiency.h"

void gentefficiency(const char* fname) {
	auto eff1 = new TEfficiency("eff1", "Eff1D", 10, 0, 10);
	auto eff2 = new TEfficiency("eff2", "Eff2D", 10, 0, 10, 10, 0, 20);
	auto eff3 = new TEfficiency("eff3", "Eff3D", 10, 0, 10, 10, 0, 20, 10, 0, 30);

	for (int i = 0; i < 1000; i++) {
		Bool_t passed = gRandom->Uniform(2) <= 1;
		Double_t x = gRandom->Uniform(10);
		Double_t y = gRandom->Uniform(20);
		Double_t z = gRandom->Uniform(30);
		eff1->Fill(passed, x);
		eff2->Fill(passed, x, y);
		eff3->Fill(passed, x, y, z);
	}

	auto f = TFile::Open(fname, "RECREATE");
	f->WriteTObject(eff1, "eff1");
	f->WriteTObject(eff2, "eff2");
	f->WriteTObject(eff3, "eff3");

	f->Write();
	f->Close();

	exit(0);
}
`
