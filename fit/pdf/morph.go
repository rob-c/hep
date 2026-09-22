// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"fmt"
	"math"

	"go-hep.org/x/hep/hbook"
	"gonum.org/v1/gonum/mat"
)

// Interp says how a template is interpolated between its variations as a
// nuisance parameter moves.
//
// A systematic uncertainty is given to a fit as three templates: the nominal
// one, and what the template looks like when the uncertainty is pushed one
// standard deviation up and one down. The nuisance parameter is measured in
// those standard deviations, and the interpolation is what fills in
// everything between and beyond them.
type Interp int

const (
	// InterpLinear moves each bin along a straight line through the
	// variations: nominal + alpha*(up - nominal) above, and
	// nominal + alpha*(nominal - down) below.
	//
	// It is the plainest choice and the one that can send a bin negative,
	// which for a yield is not a thing that can happen.
	InterpLinear Interp = iota

	// InterpExp multiplies instead: (up/nominal)^alpha above and
	// (down/nominal)^(-alpha) below.
	//
	// A bin can never go negative, which is why this is what a
	// normalisation uncertainty usually uses. Its derivative jumps at
	// alpha = 0, which a minimiser can feel.
	InterpExp

	// InterpPolyExp is exponential beyond one standard deviation and a
	// polynomial within it, chosen so that the value, the slope and the
	// curvature all join up at alpha = ±1.
	//
	// It keeps the positivity of the exponential and loses the kink at
	// zero, which is why it is what HistFactory uses by default.
	InterpPolyExp
)

func (i Interp) String() string {
	switch i {
	case InterpLinear:
		return "linear"
	case InterpExp:
		return "exponential"
	case InterpPolyExp:
		return "polynomial-exponential"
	}
	return fmt.Sprintf("Interp(%d)", int(i))
}

// morphBin holds what one bin needs to be interpolated for one systematic.
type morphBin struct {
	up   float64 // the ratio up/nominal
	down float64 // the ratio down/nominal

	// poly are the coefficients of the degree-six polynomial used within
	// one standard deviation, lowest order first, for InterpPolyExp.
	poly []float64
}

// interp returns the factor this systematic multiplies the bin by at alpha.
func (b *morphBin) interp(alpha float64, code Interp) float64 {
	switch code {
	case InterpLinear:
		if alpha >= 0 {
			return 1 + alpha*(b.up-1)
		}
		return 1 + alpha*(1-b.down)

	case InterpExp:
		if alpha >= 0 {
			return math.Pow(b.up, alpha)
		}
		return math.Pow(b.down, -alpha)

	case InterpPolyExp:
		switch {
		case alpha >= 1:
			return math.Pow(b.up, alpha)
		case alpha <= -1:
			return math.Pow(b.down, -alpha)
		}
		var (
			o = 0.0
			v = 1.0
		)
		for _, c := range b.poly {
			o += c * v
			v *= alpha
		}
		return o
	}

	return 1
}

// fitPoly solves for the degree-six polynomial that joins the two
// exponentials smoothly.
//
// Seven coefficients need seven conditions, and the conditions are that at
// alpha = ±1 the polynomial matches the exponential's value, slope and
// curvature, and that at alpha = 0 it is one — the nominal template being
// what a nuisance parameter of zero means.
//
// The system is solved rather than quoted, so there is no table of constants
// here to have copied down wrong.
func fitPoly(up, down float64) []float64 {
	const n = 7 // degree six

	var (
		a = mat.NewDense(n, n, nil)
		b = mat.NewVecDense(n, nil)
	)

	// row for the value of the polynomial at x.
	value := func(r int, x, want float64) {
		v := 1.0
		for c := range n {
			a.Set(r, c, v)
			v *= x
		}
		b.SetVec(r, want)
	}

	// row for its first derivative at x.
	slope := func(r int, x, want float64) {
		for c := range n {
			switch c {
			case 0:
				a.Set(r, c, 0)
			default:
				a.Set(r, c, float64(c)*math.Pow(x, float64(c-1)))
			}
		}
		b.SetVec(r, want)
	}

	// and its second.
	curve := func(r int, x, want float64) {
		for c := range n {
			switch {
			case c < 2:
				a.Set(r, c, 0)
			default:
				a.Set(r, c, float64(c*(c-1))*math.Pow(x, float64(c-2)))
			}
		}
		b.SetVec(r, want)
	}

	var (
		lu = math.Log(up)
		ld = math.Log(down)
	)

	// up^alpha at alpha=1, and its derivatives.
	value(0, +1, up)
	slope(1, +1, up*lu)
	curve(2, +1, up*lu*lu)

	// down^(-alpha) at alpha=-1: the derivative carries the minus sign.
	value(3, -1, down)
	slope(4, -1, -down*ld)
	curve(5, -1, down*ld*ld)

	// and the nominal at zero.
	value(6, 0, 1)

	var x mat.VecDense
	if err := x.SolveVec(a, b); err != nil {
		// a degenerate variation: fall back on no interpolation at all,
		// which is what a systematic that does nothing should do.
		o := make([]float64, n)
		o[0] = 1
		return o
	}

	o := make([]float64, n)
	for i := range n {
		o[i] = x.AtVec(i)
	}
	return o
}

// Morph is a binned template whose shape depends on nuisance parameters.
//
// Each parameter is one systematic uncertainty, measured in standard
// deviations: at zero the template is the nominal one, at ±1 it is the up or
// down variation, and in between and beyond it is whatever the interpolation
// says.
type Morph struct {
	nominal []float64 // bin contents
	widths  []float64
	edges   []float64

	// bins is one slice per systematic, one entry per bin.
	bins [][]morphBin
	code Interp
	npar int
}

// NewMorph returns a template that morphs between its variations.
//
// ups and downs give, for each systematic, what the template becomes when
// that uncertainty is pushed one standard deviation either way. They must be
// binned like the nominal template.
//
// A bin where the nominal is zero cannot be scaled into anything, so it
// stays zero however the parameters move: there is no ratio to interpolate.
func NewMorph(nominal *hbook.H1D, ups, downs []*hbook.H1D, code Interp) (*Morph, error) {
	if nominal == nil {
		return nil, fmt.Errorf("pdf: a morph needs a nominal template")
	}
	if len(ups) != len(downs) {
		return nil, fmt.Errorf(
			"pdf: %d up variations and %d down ones", len(ups), len(downs),
		)
	}

	var (
		bins = nominal.Binning.Bins
		m    = &Morph{
			nominal: make([]float64, len(bins)),
			widths:  make([]float64, len(bins)),
			edges:   make([]float64, 0, len(bins)+1),
			code:    code,
			npar:    len(ups),
		}
	)

	for i := range bins {
		b := &bins[i]
		m.nominal[i] = b.SumW()
		m.widths[i] = b.XWidth()
		if i == 0 {
			m.edges = append(m.edges, b.XMin())
		}
		m.edges = append(m.edges, b.XMax())
	}

	for s := range ups {
		if ups[s] == nil || downs[s] == nil {
			return nil, fmt.Errorf("pdf: systematic %d is missing a variation", s)
		}

		ub := ups[s].Binning.Bins
		db := downs[s].Binning.Bins
		if len(ub) != len(bins) || len(db) != len(bins) {
			return nil, fmt.Errorf(
				"pdf: systematic %d has %d and %d bins, the nominal has %d",
				s, len(ub), len(db), len(bins),
			)
		}

		mb := make([]morphBin, len(bins))
		for i := range bins {
			nom := m.nominal[i]
			switch {
			case nom == 0:
				// nothing to scale.
				mb[i] = morphBin{up: 1, down: 1}
			default:
				mb[i] = morphBin{
					up:   ub[i].SumW() / nom,
					down: db[i].SumW() / nom,
				}
			}

			// a variation that is zero or negative has no logarithm, so it
			// cannot be interpolated multiplicatively: hold it at the
			// nominal rather than return a NaN from the fit.
			if mb[i].up <= 0 || mb[i].down <= 0 {
				mb[i] = morphBin{up: 1, down: 1}
			}

			if code == InterpPolyExp {
				mb[i].poly = fitPoly(mb[i].up, mb[i].down)
			}
		}
		m.bins = append(m.bins, mb)
	}

	return m, nil
}

func (m *Morph) Name() string { return "morph" }

func (m *Morph) ParNames() []string {
	o := make([]string, m.npar)
	for i := range o {
		o[i] = fmt.Sprintf("alpha%d", i)
	}
	return o
}

func (m *Morph) NPar() int { return m.npar }

// NBins returns how many bins the template has.
func (m *Morph) NBins() int { return len(m.nominal) }

// Bin returns the content of the i-th bin at the given nuisance parameters.
func (m *Morph) Bin(i int, par []float64) float64 {
	if i < 0 || i >= len(m.nominal) {
		return 0
	}

	o := m.nominal[i]
	for s := range m.bins {
		if s >= len(par) {
			break
		}
		o *= m.bins[s][i].interp(par[s], m.code)
	}

	// an interpolation that can go negative has: a yield cannot.
	if o < 0 {
		return 0
	}
	return o
}

// index returns the bin x falls in, or -1.
func (m *Morph) index(x float64) int {
	if x < m.edges[0] || x >= m.edges[len(m.edges)-1] {
		return -1
	}
	// the bins are in order, so a search is enough.
	lo, hi := 0, len(m.edges)-1
	for lo < hi-1 {
		mid := (lo + hi) / 2
		if x < m.edges[mid] {
			hi = mid
			continue
		}
		lo = mid
	}
	return lo
}

func (m *Morph) Shape(x float64, par []float64) float64 {
	i := m.index(x)
	if i < 0 || m.widths[i] <= 0 {
		return 0
	}
	return m.Bin(i, par) / m.widths[i]
}

func (m *Morph) Integral(lo, hi float64, par []float64) float64 {
	var sum float64
	for i := range m.nominal {
		a := math.Max(lo, m.edges[i])
		z := math.Min(hi, m.edges[i+1])
		if z <= a || m.widths[i] <= 0 {
			continue
		}
		sum += m.Bin(i, par) / m.widths[i] * (z - a)
	}
	return sum
}

// Total returns the sum of every bin, which for a template is how many
// events it expects altogether.
func (m *Morph) Total(par []float64) float64 {
	var o float64
	for i := range m.nominal {
		o += m.Bin(i, par)
	}
	return o
}
