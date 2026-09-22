// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package symbols holds the go-hep packages the shell makes available to the
// code it interprets.
//
// The interpreter runs Go source, but the packages it calls into are compiled
// in: what it needs of each one is a table of names to values, which
// 'yaegi extract' writes out. The go:generate lines below are what produced
// the files beside this one, and rerunning them is how the shell keeps up
// with the packages it exposes.
package symbols

import "reflect"

// Symbols maps an import path to the exported names of that package.
var Symbols = map[string]map[string]reflect.Value{}

//go:generate go run github.com/traefik/yaegi/cmd/yaegi extract go-hep.org/x/hep/hbook
//go:generate go run github.com/traefik/yaegi/cmd/yaegi extract go-hep.org/x/hep/hplot
//go:generate go run github.com/traefik/yaegi/cmd/yaegi extract go-hep.org/x/hep/fit
//go:generate go run github.com/traefik/yaegi/cmd/yaegi extract go-hep.org/x/hep/fit/minuit
//go:generate go run github.com/traefik/yaegi/cmd/yaegi extract go-hep.org/x/hep/groot
//go:generate go run github.com/traefik/yaegi/cmd/yaegi extract go-hep.org/x/hep/groot/rtree
//go:generate go run github.com/traefik/yaegi/cmd/yaegi extract go-hep.org/x/hep/groot/rhist
//go:generate go run github.com/traefik/yaegi/cmd/yaegi extract go-hep.org/x/hep/hbook/ntup/ntroot
//go:generate go run github.com/traefik/yaegi/cmd/yaegi extract go-hep.org/x/hep/hbook/rootcnv
//go:generate go run github.com/traefik/yaegi/cmd/yaegi extract go-hep.org/x/hep/groot/riofs
//go:generate go run github.com/traefik/yaegi/cmd/yaegi extract go-hep.org/x/hep/groot/rtree/rdraw
//go:generate go run github.com/traefik/yaegi/cmd/yaegi extract go-hep.org/x/hep/groot/rtree/rdf
//go:generate go run github.com/traefik/yaegi/cmd/yaegi extract go-hep.org/x/hep/cint/rt

// yaegi extract puts the go-hep import in with the standard library ones,
// where goimports wants it in a group of its own.
//go:generate go tool goimports -w .
