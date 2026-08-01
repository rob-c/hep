// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly || windows)

package xrootd // import "go-hep.org/x/hep/xrootd"

import "fmt"

// diskSpace reports the free and the total space, in bytes, of the filesystem
// holding path.
//
// No such call is wired up on this platform. The error is returned rather than
// a pair of zeroes: a client reading zero free space would route every write
// somewhere else, which is a worse answer than saying the server cannot tell.
func diskSpace(path string) (free, total uint64, err error) {
	return 0, 0, fmt.Errorf("xrootd: free space of %q is not available on this platform", path)
}
