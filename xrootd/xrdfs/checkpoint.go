// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdfs

import "context"

// CheckpointLimits is what a server is prepared to undo for one file.
//
// Capacity bounds the checkpoint, not the file: it is how much old data the
// server will keep so that a rollback can put it back. A transaction that
// overwrites more than that is one the server could no longer undo, so it
// refuses the write instead of quietly dropping the guarantee.
type CheckpointLimits struct {
	// Capacity is the largest checkpoint, in bytes, this file may hold.
	Capacity int64
	// Used is how much of that the open checkpoint already holds. It is zero
	// when no checkpoint is open.
	Used int64
}

// Checkpointer is implemented by files that can group writes into an undoable
// checkpoint (kXR_chkpoint).
//
// A checkpoint is what makes a partial update safe on a file that readers are
// already using: the writes made inside it become visible together at
// CheckpointCommit, and CheckpointRollback puts the file back as it was.
// Without one, a job that dies half-way through rewriting a file leaves it
// neither the old file nor the new one.
//
// Only one checkpoint may be open on a file at a time, and the writes inside it
// must be made with CheckpointWriteAt and CheckpointTruncate: an ordinary
// WriteAt on a file with an open checkpoint is not part of it, and is not
// undone by a rollback.
type Checkpointer interface {
	// CheckpointBegin opens a checkpoint on the file.
	CheckpointBegin(ctx context.Context) error

	// CheckpointCommit makes the checkpointed writes permanent and closes the
	// checkpoint.
	CheckpointCommit(ctx context.Context) error

	// CheckpointRollback undoes the checkpointed writes and closes the
	// checkpoint.
	CheckpointRollback(ctx context.Context) error

	// CheckpointQuery reports how much this file's checkpoint may hold, and how
	// much of that is already used.
	CheckpointQuery(ctx context.Context) (CheckpointLimits, error)

	// CheckpointWriteAt writes len(p) bytes at off inside the open checkpoint.
	CheckpointWriteAt(ctx context.Context, p []byte, off int64) error

	// CheckpointTruncate changes the size of the file inside the open
	// checkpoint.
	CheckpointTruncate(ctx context.Context, size int64) error
}
