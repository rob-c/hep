// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdfs

import "context"

// CloneRange is one range to copy into a file: Length bytes taken from
// SrcOffset in Src, written at DstOffset in the file the copy is made into.
//
// A range of zero length copies nothing, which is not an error: a caller
// building a list from a table of extents does not have to filter the empty
// ones out.
type CloneRange struct {
	// Src is the file to copy from. It must be open on the same connection as
	// the destination, and open for reading.
	Src File

	// SrcOffset is where the range starts in Src.
	SrcOffset int64

	// Length is how many bytes to copy.
	Length int64

	// DstOffset is where the range lands in the destination file.
	DstOffset int64
}

// Cloner is implemented by files that can have byte ranges copied into them by
// the server (kXR_clone).
//
// This is the copy that does not cross the network. Assembling a file out of
// pieces of others — the same event selection taken from a run's worth of
// files, a header rewritten in front of data that has not changed — otherwise
// means reading every byte to the client and writing it straight back, at twice
// the network cost and with the client's uplink as the bottleneck. Here the
// ranges are named and the server moves them itself, with copy_file_range(2)
// where the filesystem allows it.
//
// Servers accept at most 1024 ranges in one call and refuse a longer list
// outright; see [go-hep.org/x/hep/xrootd/xrdproto/clone.MaxItems].
type Cloner interface {
	// Clone copies the given ranges into this file. It is not atomic: a
	// failure part-way through leaves the ranges already copied in place, so a
	// caller that needs all-or-nothing wraps it in a [Checkpointer].
	Clone(ctx context.Context, ranges []CloneRange) error
}
