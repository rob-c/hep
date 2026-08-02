// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package heppdt_test

import (
	"testing"

	"go-hep.org/x/hep/heppdt"
)

func TestConstituent(t *testing.T) {
	t.Parallel()

	type flavours struct {
		up, down, strange, charm, bottom, top bool
	}

	for _, tc := range []struct {
		name string
		id   heppdt.PID
		want flavours
	}{
		{name: "down", id: heppdt.PDG_d, want: flavours{down: true}},
		{name: "anti-down", id: heppdt.PDG_anti_d, want: flavours{down: true}},
		{name: "up", id: heppdt.PDG_u, want: flavours{up: true}},
		{name: "anti-up", id: heppdt.PDG_anti_u, want: flavours{up: true}},
		{name: "strange", id: heppdt.PDG_s, want: flavours{strange: true}},
		{name: "anti-strange", id: heppdt.PDG_anti_s, want: flavours{strange: true}},
		{name: "charm", id: heppdt.PDG_c, want: flavours{charm: true}},
		{name: "anti-charm", id: heppdt.PDG_anti_c, want: flavours{charm: true}},
		{name: "bottom", id: heppdt.PDG_b, want: flavours{bottom: true}},
		{name: "anti-bottom", id: heppdt.PDG_anti_b, want: flavours{bottom: true}},
		{name: "top", id: heppdt.PDG_t, want: flavours{top: true}},
		{name: "anti-top", id: heppdt.PDG_anti_t, want: flavours{top: true}},
		// nothing else is a quark of any flavour
		{name: "gluon", id: heppdt.PDG_g},
		{name: "electron", id: heppdt.PDG_e_minus},
		{name: "photon", id: heppdt.PDG_gamma},
		{name: "unset", id: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := heppdt.Constituent{ID: tc.id, Mul: 1}
			got := flavours{
				up:      c.IsUp(),
				down:    c.IsDown(),
				strange: c.IsStrange(),
				charm:   c.IsCharm(),
				bottom:  c.IsBottom(),
				top:     c.IsTop(),
			}
			if got != tc.want {
				t.Fatalf("%v:\ngot= %+v\nwant=%+v", tc.id, got, tc.want)
			}
		})
	}
}
