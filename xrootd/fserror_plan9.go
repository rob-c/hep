// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build plan9

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"strings"
	"syscall"
)

// errNotEmpty and errNoSpace are the two filesystem failures that have an
// XRootD error code of their own and no portable sentinel in io/fs, as this
// port reports them.
//
// Plan 9 has no errno: the kernel answers with a string, and two errors that
// say the same thing are still different values, so these cannot be compared
// against with errors.Is and are matched by what they say instead.
var (
	errNotEmpty = syscall.NewError("directory not empty")
	errNoSpace  = syscall.NewError("no free space")
)

func isNotEmpty(err error) bool {
	return err != nil && strings.Contains(err.Error(), errNotEmpty.Error())
}

func isNoSpace(err error) bool {
	return err != nil && strings.Contains(err.Error(), errNoSpace.Error())
}
