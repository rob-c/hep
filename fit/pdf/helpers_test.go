// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf_test

import "go-hep.org/x/hep/hbook"

func newH1D(lo, hi float64, n int) *hbook.H1D { return hbook.NewH1D(n, lo, hi) }
