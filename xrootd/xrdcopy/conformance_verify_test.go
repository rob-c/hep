// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the mechanics of post-transfer verification.
//
// Which outcomes are fatal is pinned in conformance_identity_test.go. What is
// pinned here is the rest of the contract: that the comparison actually follows
// the server's choice of algorithm across every digest this client implements,
// that a local file it cannot read is a failure rather than a pass, and that
// the query cannot hang a copy whose bytes are already on disk.

package xrdcopy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdsum"
)

func verifyFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file.dat")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("could not write the local file: %v", err)
	}
	return path
}

func TestConformance_EveryDigestTheClientCanComputeIsCompared(t *testing.T) {
	// The server picks the algorithm; the client has to follow it for any of
	// the ones it advertises, and has to still notice a mismatch in each.
	const content = "go-hep xrootd"
	path := verifyFile(t, content)

	for _, algo := range xrdsum.Supported() {
		t.Run(algo, func(t *testing.T) {
			sum, err := xrdsum.Sum(algo, []byte(content))
			if err != nil {
				t.Fatalf("could not compute %s: %v", algo, err)
			}

			good := checksumFS{algo: algo, value: sum}
			if err := verifyChecksum(context.Background(), good, "/f", path); err != nil {
				t.Fatalf("a matching %s digest was reported as a failure: %v", algo, err)
			}

			bad := checksumFS{algo: algo, value: strings.Repeat("0", len(sum))}
			err = verifyChecksum(context.Background(), bad, "/f", path)
			if err == nil {
				t.Fatalf("a mismatched %s digest passed verification", algo)
			}
			if !strings.Contains(err.Error(), algo) {
				t.Fatalf("the failure does not name the algorithm that disagreed: %v", err)
			}
		})
	}
}

func TestConformance_AVerificationThatCannotReadTheLocalFileFails(t *testing.T) {
	// Here it is the client that cannot answer, and that is a real failure: it
	// has no evidence at all that the copy is intact.
	fs := checksumFS{algo: "adler32", value: "00000000"}
	err := verifyChecksum(context.Background(), fs, "/f", filepath.Join(t.TempDir(), "absent.dat"))
	if err == nil {
		t.Fatal("verification passed without reading the local file")
	}
	if !os.IsNotExist(err) {
		t.Fatalf("the failure does not say the local file is missing: %v", err)
	}
}

// deadlineFS records the deadline of the context each query was handed.
type deadlineFS struct {
	xrdfs.FileSystem

	seen []time.Time
	none int
}

func (fs *deadlineFS) Checksum(ctx context.Context, path string) (string, string, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		fs.none++
	} else {
		fs.seen = append(fs.seen, deadline)
	}
	return "adler32", "00000000", nil
}

func TestConformance_AChecksumQueryIsBounded(t *testing.T) {
	// A server that accepts the query and never answers would hang the copy
	// after the bytes are already safely on disk, so the query gets its own
	// deadline when the caller supplied none.
	local := verifyFile(t, "go-hep")
	fs := &deadlineFS{}

	start := time.Now()
	_ = verifyChecksum(context.Background(), fs, "/f", local)
	if fs.none != 0 {
		t.Fatal("the checksum query was made with no deadline")
	}
	if len(fs.seen) != 1 {
		t.Fatalf("the server was asked for a checksum %d times, want 1", len(fs.seen))
	}
	if d := fs.seen[0].Sub(start); d <= 0 || d > 2*time.Minute {
		t.Fatalf("the imposed deadline is %v away, want a bound on the order of a minute", d)
	}

	// A caller that already bounded the work keeps its own deadline rather
	// than having a second one imposed on top of it.
	want := time.Now().Add(3 * time.Hour)
	ctx, cancel := context.WithDeadline(context.Background(), want)
	defer cancel()
	_ = verifyChecksum(ctx, fs, "/f", local)
	if len(fs.seen) != 2 {
		t.Fatalf("the server was asked for a checksum %d times, want 2", len(fs.seen))
	}
	if !fs.seen[1].Equal(want) {
		t.Fatalf("the query ran to %v, want the caller's own deadline %v", fs.seen[1], want)
	}
}
