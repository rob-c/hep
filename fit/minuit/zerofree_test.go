// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package minuit_test

import (
	"math"
	"testing"

	"go-hep.org/x/hep/fit/minuit"
)

// TestNoFreeParameters checks a minimisation with nothing left to vary works
// rather than falling over.
//
// It is not a silly case: a profile scan of a model with one parameter fixes
// the only one there is, and then asks for a fit of what remains.
func TestNoFreeParameters(t *testing.T) {
	m := minuit.New(1)
	m.SetPrintLevel(-1)
	m.SetFCN(minuit.FuncOf(func(par []float64) float64 {
		return (par[0] - 3) * (par[0] - 3)
	}))

	// a step of zero fixes it.
	if err := m.Parameter(0, "x", 5, 0, 0, 0); err != nil {
		t.Fatalf("could not set the parameter: %+v", err)
	}
	if got, want := m.NFree(), 0; got != want {
		t.Fatalf("nfree: got=%d, want=%d", got, want)
	}

	for _, cmd := range []string{"MIGRAD", "HESSE", "MINOS", "SIMPLEX"} {
		t.Run(cmd, func(t *testing.T) {
			if err := m.Command(cmd); err != nil {
				t.Fatalf("%s: %+v", cmd, err)
			}
		})
	}

	// the function was evaluated where the parameter was held.
	if got, want := m.FMin(), 4.0; math.Abs(got-want) > 1e-12 {
		t.Errorf("fmin: got=%v, want=%v", got, want)
	}
	if got, want := m.Status(), minuit.Converged; got != want {
		t.Errorf("status: got=%v, want=%v", got, want)
	}
	if m.Covariance() != nil {
		t.Error("a fit with nothing free reported a covariance")
	}
}
