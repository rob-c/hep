// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build ignore

package main

import (
	"log"

	"go-hep.org/x/hep/groot/internal/rtests"
)

// Generate data to test issue #1063.
// See for more details:
//   - https://codeberg.org/go-hep/hep/issues/1063
func main() {
	const oname = "issue-1063.root"
	out, err := rtests.RunCxxROOT("run", []byte(script), oname)
	if err != nil {
		log.Fatalf("could not run ROOT macro:\noutput:\n%s\nerror: %+v", out, err)
	}
}

const script = `
#include "TFile.h"
#include "TEfficiency.h"
#include "TRandom3.h"
#include "TMath.h"

void run(const char *fname) {
	auto f = TFile::Open(fname, "RECREATE");
	auto e = new TEfficiency("eff", "efficiency;x;#epsilon", 20, 0, 10);
	auto r = new TRandom3();

	for (int i = 0; i < 10000; i++) {
		auto x = r->Uniform(10);
		e->Fill(r->Rndm() < TMath::Gaus(x, 5, 4), x);
	}
	f->WriteObject(e, "eff");

	f->Close();
}
`
