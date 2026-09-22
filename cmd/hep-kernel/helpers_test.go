// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"io"

	"go-hep.org/x/hep/cmd/internal/hepsh"
)

func newTestSession(out, errw io.Writer) (*hepsh.Session, error) {
	return hepsh.New(out, errw)
}
