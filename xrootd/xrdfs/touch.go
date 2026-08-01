// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdfs

import (
	"context"
	"errors"
	"io/fs"
)

// TouchMode is the mode a file created by [Touch] is given: readable and
// writable by its owner, as touch(1) leaves one under a 0022 umask minus the
// group and other bits a namespace has no use for.
const TouchMode = OpenModeOwnerRead | OpenModeOwnerWrite

// Touch creates path if it is not already there, and does nothing at all if it
// is.
//
// There is no request in the protocol that moves a file's modification time, so
// unlike touch(1) this cannot refresh one — and it will not pretend to by
// rewriting a file, which would destroy exactly what a caller reaching for
// "touch" is trying to keep. What it is for is the other half of the utility:
// bringing a file into existence, and saying so without a race, since the open
// that creates it is the one that fails if somebody else got there first.
//
// The parent directories are created as needed. perm is the mode a newly
// created file is given; [TouchMode] is the usual choice.
func Touch(ctx context.Context, fsys FileSystem, path string, perm OpenMode) error {
	const options = OpenOptionsNew | OpenOptionsOpenUpdate | OpenOptionsMkPath

	f, err := fsys.Open(ctx, path, perm, options)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			// kXR_new is the open that refuses to truncate, so being told the
			// file is there is this call's successful outcome and not a failure
			// to report: the file exists, which is all Touch was asked for.
			return nil
		}
		return err
	}
	return f.Close(ctx)
}
