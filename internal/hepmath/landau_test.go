// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package hepmath

import (
	"math"
	"testing"
)

// TestLandauLandmarks checks the density against the values the Landau
// distribution is known by.
//
// The mode and the height at it are quoted to six figures in the literature,
// and phi(lambda) -> 1/lambda^2 far out on the right, which together pin down
// both the scale and the shape.
func TestLandauLandmarks(t *testing.T) {
	// the peak: phi is largest at lambda = -0.222782, where it is 0.180655.
	const (
		mode   = -0.222782
		atMode = 0.180655
		tol    = 1e-5
	)

	if got := Landau(mode); math.Abs(got-atMode) > tol {
		t.Errorf("Landau(%v): got=%v, want=%v", mode, got, atMode)
	}

	// it really is the largest: nothing either side beats it.
	for _, d := range []float64{-0.05, -0.02, 0.02, 0.05, 0.2} {
		if got := Landau(mode + d); got > Landau(mode) {
			t.Errorf("Landau(%v)=%v is above the mode %v", mode+d, got, Landau(mode))
		}
	}

	// the right tail goes like 1/lambda^2.
	for _, x := range []float64{50, 100, 500} {
		var (
			got  = Landau(x)
			want = 1 / (x * x)
		)
		if math.Abs(got-want)/want > 0.15 {
			t.Errorf("Landau(%v): got=%v, want~%v (1/x^2)", x, got, want)
		}
	}

	// and the left tail is gone.
	for _, x := range []float64{-4, -6, -20} {
		if got := Landau(x); got < 0 || got > 1e-5 {
			t.Errorf("Landau(%v): got=%v, want~0", x, got)
		}
	}
}

// TestLandauIsADensity checks it is positive and integrates to one, which is
// what makes it usable as a probability density at all.
func TestLandauIsADensity(t *testing.T) {
	const (
		lo = -5.0
		hi = 1000.0
		n  = 200000
	)

	var (
		sum float64
		dx  = (hi - lo) / n
	)
	for i := range n {
		x := lo + (float64(i)+0.5)*dx
		v := Landau(x)
		if v < 0 {
			t.Fatalf("Landau(%v) = %v is negative", x, v)
		}
		sum += v * dx
	}

	// the tail beyond hi contributes the integral of 1/x^2 from hi up, which
	// is 1/hi.
	sum += 1 / hi

	if math.Abs(sum-1) > 5e-3 {
		t.Errorf("the density integrates to %v, want 1", sum)
	}
}

// TestLandauCDF checks the cumulative agrees with the density it came from.
func TestLandauCDF(t *testing.T) {
	if got := LandauCDF(-5); got != 0 {
		t.Errorf("LandauCDF(-5): got=%v, want=0", got)
	}

	// the cumulative between two points is the integral of the density
	// between them, done coarsely here by hand.
	for _, tc := range []struct{ lo, hi float64 }{
		{-2, 0},
		{0, 2},
		{-1, 5},
	} {
		var (
			n   = 20000
			dx  = (tc.hi - tc.lo) / float64(n)
			sum float64
		)
		for i := range n {
			sum += Landau(tc.lo+(float64(i)+0.5)*dx) * dx
		}

		got := LandauCDF(tc.hi) - LandauCDF(tc.lo)
		if math.Abs(got-sum) > 1e-4 {
			t.Errorf("CDF over [%v,%v]: got=%v, want=%v", tc.lo, tc.hi, got, sum)
		}
	}

	// it never goes backwards.
	prev := 0.0
	for x := -5.0; x < 50; x += 0.5 {
		got := LandauCDF(x)
		if got < prev-1e-12 {
			t.Fatalf("LandauCDF fell from %v to %v at x=%v", prev, got, x)
		}
		prev = got
	}
}

func TestLandauNaN(t *testing.T) {
	if got := Landau(math.NaN()); !math.IsNaN(got) {
		t.Errorf("Landau(NaN): got=%v, want NaN", got)
	}
}
