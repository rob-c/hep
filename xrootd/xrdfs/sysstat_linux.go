// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdfs

import (
	"os"
	"strconv"
	"syscall"
)

// sysStat returns the fields of the extended stat line that os.FileInfo does not
// carry, as this port can see them.
func sysStat(info os.FileInfo) (ctime, atime int64, owner, group string) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, "", ""
	}
	return st.Ctim.Sec, st.Atim.Sec,
		strconv.FormatUint(uint64(st.Uid), 10),
		strconv.FormatUint(uint64(st.Gid), 10)
}
