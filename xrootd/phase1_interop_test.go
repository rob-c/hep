// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"os"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdsum"
)

// TestPhase1Interop exercises pgread, checksum and fattr against a real
// XRootD server. Skipped unless XROOTD_P1_SERVER is set, e.g.
//
//	XROOTD_P1_SERVER=root://server:1094 XROOTD_P1_PATH=//tmp/f.dat \
//	go test ./xrootd/ -run TestPhase1Interop -v
func TestPhase1Interop(t *testing.T) {
	server := os.Getenv("XROOTD_P1_SERVER")
	if server == "" {
		t.Skip("set XROOTD_P1_SERVER to run the phase-1 interop test")
	}
	path := os.Getenv("XROOTD_P1_PATH")
	if path == "" {
		t.Skip("set XROOTD_P1_PATH to a readable file on the server")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	be, err := Dial(ctx, server, os.Getenv("USER"))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer be.Close()

	fs := be.FS()

	t.Run("pgread-vs-checksum", func(t *testing.T) {
		st, err := fs.Stat(ctx, path)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		f, err := fs.Open(ctx, path, xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer f.Close(ctx)

		pg, ok := f.(xrdfs.PgReader)
		if !ok {
			t.Fatal("file does not implement xrdfs.PgReader")
		}
		data := make([]byte, st.Size())
		n, err := pg.PgReadAt(ctx, data, 0)
		if err != nil {
			t.Fatalf("PgReadAt: %v", err)
		}
		data = data[:n]

		cks, ok := fs.(xrdfs.ChecksumFS)
		if !ok {
			t.Skip("filesystem does not implement ChecksumFS")
		}
		algo, want, err := cks.Checksum(ctx, path)
		if err != nil {
			t.Skipf("server checksum unavailable: %v", err)
		}
		got, err := xrdsum.Sum(algo, data)
		if err != nil {
			t.Skipf("local digest for %q unavailable: %v", algo, err)
		}
		if got != want {
			t.Fatalf("checksum mismatch (%s): local=%s server=%s", algo, got, want)
		}
	})

	t.Run("fattr-roundtrip", func(t *testing.T) {
		xa, ok := fs.(xrdfs.XAttrFS)
		if !ok {
			t.Fatal("filesystem does not implement xrdfs.XAttrFS")
		}
		if err := xa.SetXAttr(ctx, path, "user.gohep", []byte("p1")); err != nil {
			t.Skipf("server rejects fattr set (may be disabled): %v", err)
		}
		v, err := xa.GetXAttr(ctx, path, "user.gohep")
		if err != nil {
			t.Fatalf("GetXAttr: %v", err)
		}
		if string(v) != "p1" {
			t.Fatalf("xattr value: %q", v)
		}
		if err := xa.DelXAttr(ctx, path, "user.gohep"); err != nil {
			t.Fatalf("DelXAttr: %v", err)
		}
	})
}
