// Copyright ©2016 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package hbook is a set of data analysis tools for HEP (histograms (1D, 2D, 3D),
// profiles and ntuples).
// hbook is a work in progress of a concurrent friendly histogram filling toolkit.
// It is loosely based on AIDA interfaces and concepts as well as the "simplicity"
// of HBOOK and the previous work of YODA.
//
// # Ratios
//
// DivideH1D divides one histogram by another and propagates the two
// uncertainties in quadrature, which is what a ratio of independent
// measurements calls for.
//
// When the numerator counts a subset of what the denominator counts -- a
// trigger efficiency, a reconstruction efficiency, a cut acceptance -- the two
// are not independent and quadrature is the wrong answer: it gives an
// uncertainty that reaches past 1 and that stays nonzero where the efficiency
// saturates. NewEfficiency handles that case, with a confidence interval taken
// from the binomial distribution the counts actually follow. BinomialInterval
// does the same for a single pass/total pair, without a histogram.
//
// # Binning without deciding first
//
// NewH1D has to be given a range and a number of bins before it has seen any
// data, and what falls outside is counted but not drawn.
// [go-hep.org/x/hep/hplot/quick] takes both from the sample instead --
// quick.Bin returns the H1D, quick.Hist draws it -- which is the shorter way
// in when the numbers are already in hand.
package hbook
