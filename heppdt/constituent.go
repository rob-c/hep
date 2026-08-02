// Copyright ©2017 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package heppdt

// Constituent holds a particle constituent
// (e.g. quark type and number of quarks of this type)
type Constituent struct {
	ID  PID // particle ID
	Mul int // multiplicity
}

// isQuark reports whether this constituent is the quark of the given flavour.
// A meson lists a quark and an antiquark, and both carry that flavour, so the
// sign of the ID does not enter.
func (c Constituent) isQuark(flavour PID) bool {
	switch c.ID {
	case flavour, -flavour:
		return true
	}
	return false
}

// IsUp returns whether this is an up-quark
func (c Constituent) IsUp() bool {
	return c.isQuark(PDG_u)
}

// IsDown returns whether this is a down-quark
func (c Constituent) IsDown() bool {
	return c.isQuark(PDG_d)
}

// IsStrange returns whether this is a strange-quark
func (c Constituent) IsStrange() bool {
	return c.isQuark(PDG_s)
}

// IsCharm returns whether this is a charm-quark
func (c Constituent) IsCharm() bool {
	return c.isQuark(PDG_c)
}

// IsBottom returns whether this is a bottom-quark
func (c Constituent) IsBottom() bool {
	return c.isQuark(PDG_b)
}

// IsTop returns whether this is a top-quark
func (c Constituent) IsTop() bool {
	return c.isQuark(PDG_t)
}
