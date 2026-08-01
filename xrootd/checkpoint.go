// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"context"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto/chkpoint"
	"go-hep.org/x/hep/xrootd/xrdproto/truncate"
	"go-hep.org/x/hep/xrootd/xrdproto/write"
)

// CheckpointBegin opens a checkpoint on the file, so that the writes made with
// CheckpointWriteAt and CheckpointTruncate can later be undone as a group.
func (f *file) CheckpointBegin(ctx context.Context) error {
	return f.checkpoint(ctx, &chkpoint.Request{Handle: f.handle, SubCode: chkpoint.Begin})
}

// CheckpointCommit makes the checkpointed writes permanent and closes the
// checkpoint.
func (f *file) CheckpointCommit(ctx context.Context) error {
	return f.checkpoint(ctx, &chkpoint.Request{Handle: f.handle, SubCode: chkpoint.Commit})
}

// CheckpointRollback undoes the checkpointed writes and closes the checkpoint,
// leaving the file as it was when the checkpoint was opened.
func (f *file) CheckpointRollback(ctx context.Context) error {
	return f.checkpoint(ctx, &chkpoint.Request{Handle: f.handle, SubCode: chkpoint.Rollback})
}

// CheckpointQuery reports how much this file's checkpoint may hold.
//
// It is worth asking before a large transaction rather than after: the server
// refuses the write that would exceed the capacity, and a job that finds out at
// that point has already done the work.
func (f *file) CheckpointQuery(ctx context.Context) (xrdfs.CheckpointLimits, error) {
	var resp chkpoint.Response
	req := &chkpoint.Request{Handle: f.handle, SubCode: chkpoint.Query}
	err := f.do(ctx, func(ctx context.Context, sid string) (string, error) {
		return f.fs.c.sendSession(ctx, sid, &resp, req)
	})
	if err != nil {
		return xrdfs.CheckpointLimits{}, err
	}
	return xrdfs.CheckpointLimits{Capacity: int64(resp.Capacity), Used: int64(resp.Used)}, nil
}

// CheckpointWriteAt writes len(p) bytes from p at offset off, inside the open
// checkpoint.
//
// An ordinary WriteAt on the same file is not part of the checkpoint and
// survives a rollback, so a transaction must make every one of its writes
// through here.
func (f *file) CheckpointWriteAt(ctx context.Context, p []byte, off int64) error {
	req, err := chkpoint.NewXeq(f.handle, &write.Request{Handle: f.handle, Offset: off, Data: p})
	if err != nil {
		return err
	}
	return f.checkpoint(ctx, req)
}

// CheckpointTruncate changes the size of the file inside the open checkpoint.
func (f *file) CheckpointTruncate(ctx context.Context, size int64) error {
	req, err := chkpoint.NewXeq(f.handle, &truncate.Request{Handle: f.handle, Size: size})
	if err != nil {
		return err
	}
	return f.checkpoint(ctx, req)
}

func (f *file) checkpoint(ctx context.Context, req *chkpoint.Request) error {
	return f.do(ctx, func(ctx context.Context, sid string) (string, error) {
		return f.fs.c.sendSession(ctx, sid, nil, req)
	})
}

var _ xrdfs.Checkpointer = (*file)(nil)
