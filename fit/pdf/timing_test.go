// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf_test

import "time"

func timeIt(f func()) time.Duration {
	t0 := time.Now()
	f()
	return time.Since(t0)
}
