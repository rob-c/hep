// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import "golang.org/x/sys/windows"

// diskSpace reports the free and the total space, in bytes, of the volume
// holding path.
//
// The free figure is what this caller may still write rather than what the
// volume has left: the two differ where a disk quota applies, and a client told
// about space a quota forbids would size a transfer it is not allowed to
// complete.
func diskSpace(path string) (free, total uint64, err error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}

	var avail, bytes, unused uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &bytes, &unused); err != nil {
		return 0, 0, err
	}
	return avail, bytes, nil
}
