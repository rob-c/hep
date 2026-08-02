// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-31-root-groot-write writes a ROOT file straight to remote storage,
// then reads it back, without a local copy at either end.
//
// The blank import is the whole trick: it registers the root, roots, xroot and
// xroots schemes with riofs, after which groot.Create and groot.Open take a
// URL wherever they took a path. The parent directories are created on the
// way, because a ROOT file is usually the first thing a job writes into its
// output directory.
//
// A ROOT file is not written front to back — the directory is closed and the
// header rewritten once the size is known — so this is a random-access write
// over the wire, and each write names the offset it lands at. Over http:// and
// davs:// the same code works, but HTTP has no ranged write, so the file is
// held in memory and PUT whole on close: it has to fit in RAM. Over root:// it
// does not.
package main

import (
	"fmt"
	"log"

	"go-hep.org/x/hep/groot"
	"go-hep.org/x/hep/groot/rbase"
	"go-hep.org/x/hep/groot/riofs"
	_ "go-hep.org/x/hep/groot/riofs/plugin/xrootd" // root://, roots://, xroot://, xroots://
)

func main() {
	const name = "root://storage.example.org:1094//store/user/gopher/out.root"

	w, err := groot.Create(name)
	if err != nil {
		log.Fatalf("could not create %s: %+v", name, err)
	}

	if err := w.Put("greeting", rbase.NewObjString("hello from go-hep")); err != nil {
		w.Close()
		log.Fatalf("could not write the object: %+v", err)
	}

	// Close is where the header and the directory are rewritten, so it is a
	// write like any other and its error is not a formality.
	if err := w.Close(); err != nil {
		log.Fatalf("could not close %s: %+v", name, err)
	}

	r, err := groot.Open(name)
	if err != nil {
		log.Fatalf("could not open %s: %+v", name, err)
	}
	defer r.Close()

	obj, err := riofs.Get[*rbase.ObjString](r, "greeting")
	if err != nil {
		log.Fatalf("could not read the object back: %+v", err)
	}
	fmt.Printf("%s: %q\n", name, obj.String())
}
