// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package xrootd // import "go-hep.org/x/hep/xrootd"

import "syscall"

// diskSpace reports the free and the total space, in bytes, of the filesystem
// holding path.
//
// The free figure is what an unprivileged process may still write: the blocks
// a filesystem reserves for root are counted as used, because a client told
// about them would size a transfer it is not allowed to complete.
func diskSpace(path string) (free, total uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bsize := uint64(st.Bsize)
	return uint64(st.Bavail) * bsize, uint64(st.Blocks) * bsize, nil
}
