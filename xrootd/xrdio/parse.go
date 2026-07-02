// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdio

import (
	"go-hep.org/x/hep/xrootd"
)

// URL stores an absolute reference to a XRootD path.
//
// It is an alias for xrootd.URL, where the parsing lives so that the
// xrootd package can consume URLs without importing xrdio.
type URL = xrootd.URL

// Parse parses name into an xrootd URL structure.
func Parse(name string) (URL, error) {
	return xrootd.ParseURL(name)
}
