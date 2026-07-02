// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"os"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/xrdproto/auth/sss"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/token"
)

// TestPhase3Interop authenticates against a real XRootD server using the auth
// provider named by XROOTD_P3_PROVIDER (ztn or sss). Skipped unless
// XROOTD_P3_SERVER and XROOTD_P3_PATH are set.
func TestPhase3Interop(t *testing.T) {
	server := os.Getenv("XROOTD_P3_SERVER")
	path := os.Getenv("XROOTD_P3_PATH")
	if server == "" || path == "" {
		t.Skip("set XROOTD_P3_SERVER and XROOTD_P3_PATH to run the phase-3 interop test")
	}

	var opt Option
	switch os.Getenv("XROOTD_P3_PROVIDER") {
	case "ztn":
		tok, err := token.Discover()
		if err != nil {
			t.Skipf("no bearer token: %v", err)
		}
		opt = WithAuth(&token.Auth{Token: tok})
	case "sss":
		a, err := sss.New()
		if err != nil {
			t.Skipf("no sss keytab: %v", err)
		}
		opt = WithAuth(a)
	default:
		t.Skip("set XROOTD_P3_PROVIDER to ztn or sss")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	be, err := Dial(ctx, server, os.Getenv("USER"), opt)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer be.Close()

	fi, err := be.FS().Stat(ctx, path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	t.Logf("authenticated ok: name=%s size=%d", fi.Name(), fi.Size())
}
