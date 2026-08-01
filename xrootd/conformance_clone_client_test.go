// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the client side of kXR_clone and kXR_dcksm, driven over a
// connection to the in-process server.
//
// Both are optional surfaces: a server that does not do them answers
// kXR_Unsupported, and the client has to hand that back as an error a caller
// can act on rather than as an empty success. The handler-level tests next door
// cover what the server does with the request; these cover what gets onto the
// wire and what comes back off it.

package xrootd_test // import "go-hep.org/x/hep/xrootd"

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/clone"
)

// cloneClientFS returns a client filesystem over a fresh server, together with
// the directory it serves.
func cloneClientFS(t *testing.T) (xrdfs.FileSystem, string) {
	t.Helper()

	srv, addr, baseDir, err := createServer(func(err error) { t.Error(err) })
	if err != nil {
		t.Fatalf("could not create the server: %v", err)
	}
	t.Cleanup(func() {
		_ = srv.Shutdown(context.Background())
		os.RemoveAll(baseDir)
	})

	cli, err := createClient(addr)
	if err != nil {
		t.Fatalf("could not create the client: %v", err)
	}
	t.Cleanup(func() { cli.Close() })

	return cli.FS(), baseDir
}

func TestConformance_AClientClonesRangesBetweenOpenFiles(t *testing.T) {
	fsys, dir := cloneClientFS(t)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(dir, "src.bin"), []byte("0123456789"), 0644); err != nil {
		t.Fatalf("could not write the source: %v", err)
	}

	src, err := fsys.Open(ctx, "/src.bin", 0, xrdfs.OpenOptionsOpenRead)
	if err != nil {
		t.Fatalf("could not open the source: %v", err)
	}
	defer src.Close(ctx)

	dst, err := fsys.Open(ctx, "/dst.bin", xrdfs.OpenModeOwnerRead|xrdfs.OpenModeOwnerWrite, xrdfs.OpenOptionsNew|xrdfs.OpenOptionsOpenUpdate)
	if err != nil {
		t.Fatalf("could not open the destination: %v", err)
	}
	defer dst.Close(ctx)

	cloner, ok := dst.(xrdfs.Cloner)
	if !ok {
		t.Fatalf("a file is a %T, which does not implement xrdfs.Cloner", dst)
	}

	err = cloner.Clone(ctx, []xrdfs.CloneRange{
		{Src: src, SrcOffset: 6, Length: 4, DstOffset: 0},
		{Src: src, SrcOffset: 0, Length: 6, DstOffset: 4},
	})
	if err != nil {
		t.Fatalf("could not clone: %v", err)
	}
	if err := dst.Sync(ctx); err != nil {
		t.Fatalf("could not sync the destination: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "dst.bin"))
	if err != nil {
		t.Fatalf("could not read the destination: %v", err)
	}
	if want := "6789012345"; string(got) != want {
		t.Fatalf("the destination holds %q, want %q", got, want)
	}
}

func TestConformance_AClientRefusesACloneItCannotSend(t *testing.T) {
	fsys, _ := cloneClientFS(t)
	ctx := context.Background()

	dst, err := fsys.Open(ctx, "/dst.bin", xrdfs.OpenModeOwnerRead|xrdfs.OpenModeOwnerWrite, xrdfs.OpenOptionsNew|xrdfs.OpenOptionsOpenUpdate)
	if err != nil {
		t.Fatalf("could not open the destination: %v", err)
	}
	defer dst.Close(ctx)
	cloner := dst.(xrdfs.Cloner)

	t.Run("nothing to copy", func(t *testing.T) {
		// A server answers an empty list with kXR_ArgMissing, and a round trip
		// to be told that a caller asked for nothing is a round trip wasted.
		if err := cloner.Clone(ctx, nil); err != nil {
			t.Fatalf("an empty clone was refused: %v", err)
		}
	})

	t.Run("more ranges than a server accepts", func(t *testing.T) {
		err := cloner.Clone(ctx, make([]xrdfs.CloneRange, clone.MaxItems+1))
		if err == nil {
			t.Fatal("a list longer than the server accepts was sent")
		}
		if !strings.Contains(err.Error(), "too many clone ranges") {
			t.Fatalf("the failure says %q, want it to name the limit", err)
		}
	})

	t.Run("a range with no source", func(t *testing.T) {
		err := cloner.Clone(ctx, []xrdfs.CloneRange{{Length: 1}})
		if err == nil {
			t.Fatal("a range naming no source file was sent")
		}
		if !strings.Contains(err.Error(), "no source file") {
			t.Fatalf("the failure says %q, want it to name the source", err)
		}
	})
}

// noCloneHandler is the filesystem handler with its clone support taken away:
// the embedded interface is xrootd.Handler, which does not name Clone, so the
// wrapper does not implement xrootd.CloneHandler however its value was built.
type noCloneHandler struct {
	xrootd.Handler
}

func TestConformance_AServerThatDoesNotCloneSaysSo(t *testing.T) {
	// kXR_clone is optional, and a client talking to a server without it has to
	// be told. The alternative — a handler that answers kXR_ok because it did
	// not recognise the request — leaves the caller believing bytes were copied
	// that never were.
	dir := t.TempDir()
	_, addr := serveHandler(t, &noCloneHandler{xrootd.NewFSHandler(dir)}, func(err error) { t.Error(err) })

	cli, err := createClient(addr)
	if err != nil {
		t.Fatalf("could not create the client: %v", err)
	}
	defer cli.Close()
	ctx := context.Background()

	dst, err := cli.FS().Open(ctx, "/dst.bin", xrdfs.OpenModeOwnerRead|xrdfs.OpenModeOwnerWrite, xrdfs.OpenOptionsNew|xrdfs.OpenOptionsOpenUpdate)
	if err != nil {
		t.Fatalf("could not open the destination: %v", err)
	}
	defer dst.Close(ctx)

	err = dst.(xrdfs.Cloner).Clone(ctx, []xrdfs.CloneRange{{Src: dst, Length: 1}})
	if err == nil {
		t.Fatal("a server without clone support answered a clone")
	}
	var srvErr xrdproto.ServerError
	if !errors.As(err, &srvErr) {
		t.Fatalf("the failure is a %T, want an xrdproto.ServerError", err)
	}
	if srvErr.Code != xrdproto.Unsupported {
		t.Fatalf("the failure is coded %v, want %v", srvErr.Code, xrdproto.Unsupported)
	}
	if !strings.Contains(srvErr.Message, "clone is not supported") {
		t.Fatalf("the failure says %q, want it to say the server does not clone", srvErr.Message)
	}
}

func TestConformance_AClientReadsAChecksumListing(t *testing.T) {
	fsys, dir := cloneClientFS(t)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("123456789"), 0644); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}

	cfs, ok := fsys.(xrdfs.ChecksumDirFS)
	if !ok {
		t.Fatalf("a filesystem is a %T, which does not implement xrdfs.ChecksumDirFS", fsys)
	}

	entries, err := cfs.DirlistChecksum(ctx, "/", "sha256")
	if err != nil {
		t.Fatalf("could not list the directory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("the listing holds %d entries, want 1", len(entries))
	}

	entry := entries[0]
	if got, want := entry.EntryName, "a.txt"; got != want {
		t.Fatalf("the entry is %q, want %q", got, want)
	}
	if !entry.HasStatInfo || !entry.HasExtendedInfo {
		t.Fatalf("the entry came back stat=%v extended=%v, want both", entry.HasStatInfo, entry.HasExtendedInfo)
	}
	if got, want := entry.ChecksumAlgo(), "sha256"; got != want {
		t.Fatalf("the entry is checksummed with %q, want %q", got, want)
	}
	const want = "15e2b0d3c33891ebb0f1ef609ec419420c20e320ce94c65fbc8c3312448eb225"
	if got := entry.ChecksumValue(); got != want {
		t.Fatalf("the entry hashes to %q, want %q", got, want)
	}
	if got, want := entry.Perm, uint32(0644); got != want {
		t.Fatalf("the entry is %04o, want %04o", got, want)
	}
}

func TestConformance_AChecksumListingOfAnAlgorithmNobodyHasFails(t *testing.T) {
	fsys, _ := cloneClientFS(t)
	ctx := context.Background()

	_, err := fsys.(xrdfs.ChecksumDirFS).DirlistChecksum(ctx, "/", "sha3")
	if err == nil {
		t.Fatal("a listing under an algorithm no server implements succeeded")
	}
	if !strings.Contains(err.Error(), "sha3 checksum not supported") {
		t.Fatalf("the failure says %q, want it to name the algorithm", err)
	}
}
