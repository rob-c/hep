// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdcopy

import (
	"os"
	"testing"
)

// TestResumeOffsetLogic checks the offset decisions of a resumed transfer using
// the local-side helpers, independent of a server: a partial destination
// resumes from its size, a complete one is a no-op, and a fresh one starts over.
func TestResumeOffsetLogic(t *testing.T) {
	dir := t.TempDir()

	// Simulate a "remote" size of 100 bytes via local files of known sizes.
	for _, tc := range []struct {
		name       string
		localSize  int
		remoteSize int64
		wantOffset int64
		wantSkip   bool
	}{
		{"partial", 40, 100, 40, false},
		{"complete", 100, 100, 0, true},
		{"empty", 0, 100, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := dir + "/" + tc.name
			if tc.localSize > 0 {
				if err := os.WriteFile(p, make([]byte, tc.localSize), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			off, skip := resumeOffset(p, tc.remoteSize)
			if skip != tc.wantSkip {
				t.Fatalf("skip: got=%v want=%v", skip, tc.wantSkip)
			}
			if off != tc.wantOffset {
				t.Fatalf("offset: got=%d want=%d", off, tc.wantOffset)
			}
		})
	}
}
