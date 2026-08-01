// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !linux && !darwin

package xrdfs

import "os"

// sysStat returns the fields of the extended stat line that os.FileInfo does not
// carry — on this port, none of them.
//
// The two timestamps and the ownership live in the operating system's stat
// buffer, which not every port of Go exposes and which the ones that do spell
// differently. Zero and empty are what a reader of the line sees, and are read
// back as what they are: an answer this server did not have.
func sysStat(info os.FileInfo) (ctime, atime int64, owner, group string) {
	return 0, 0, "", ""
}
