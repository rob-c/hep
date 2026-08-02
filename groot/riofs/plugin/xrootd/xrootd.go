// Copyright ©2019 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package xrootd is a plugin for riofs.Open and riofs.Create, to read and
// write ROOT files over xrootd.
//
// The TLS-bearing schemes are registered alongside the plain ones: roots://
// and xroots:// differ from root:// and xroot:// only in that the session is
// encrypted, which is not a difference riofs should have to know about.
//
// Writing goes over the same native connection as reading, and without a local
// copy: riofs seeks back over its own output to close directories and to write
// the header, and every XRootD write names the offset it lands at.
package xrootd

import (
	"os"

	"go-hep.org/x/hep/groot/riofs"
	"go-hep.org/x/hep/xrootd/xrdio"
)

func init() {
	for _, scheme := range []string{"root", "roots", "xroot", "xroots"} {
		riofs.Register(scheme, openFile)
		riofs.RegisterWriter(scheme, createFile)
	}
}

func openFile(path string) (riofs.Reader, error) {
	return xrdio.Open(path)
}

func createFile(path string) (riofs.Writer, error) {
	// MkPath: a ROOT file is often the first thing written to the directory
	// holding a job's output, and failing because the parent is missing wastes
	// a round trip on something the server will do on the way.
	return xrdio.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC|xrdio.MkPath)
}

var (
	_ riofs.Reader = (*xrdio.File)(nil)
	_ riofs.Writer = (*xrdio.File)(nil)
)
