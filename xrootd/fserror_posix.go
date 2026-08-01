// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !plan9

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"errors"
	"syscall"
)

// errNotEmpty and errNoSpace are the two filesystem failures that have an
// XRootD error code of their own and no portable sentinel in io/fs, as this
// port reports them.
var (
	errNotEmpty error = syscall.ENOTEMPTY
	errNoSpace  error = syscall.ENOSPC
)

func isNotEmpty(err error) bool { return errors.Is(err, syscall.ENOTEMPTY) }

func isNoSpace(err error) bool { return errors.Is(err, syscall.ENOSPC) }
