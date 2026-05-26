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
	root = flag.String("f", "embedded-tbox.root", "output ROOT file")
)

func main() {
	flag.Parse()

	out, err := rtests.RunCxxROOT("gentbox", []byte(script), *root)
	if err != nil {
		log.Fatalf("could not run ROOT macro:\noutput:\n%v\nerror: %+v", string(out), err)
	}
}

const script = `
void gentbox(const char* fname) {
	auto f = TFile::Open(fname, "RECREATE");

	auto h = new TH1F("h1", "h1", 10, 0, 10);
	h->FillRandom("gaus", 5);

	auto b = new TBox(0,0,10,10);
	b->SetFillColor(kGray);
	b->SetLineColor(kGray);
	h->GetListOfFunctions()->Add(b);
	h->Write();

	f->Write();
	f->Close();

	exit(0);
}
`
