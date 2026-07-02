// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestTLSInterop connects to a real XRootD server over roots:// and stats a
// path. It is skipped unless XROOTD_TLS_SERVER is set, e.g.
//
//	XROOTD_TLS_SERVER=roots://xrootd.example.org:1094 \
//	XROOTD_TLS_PATH=//store/test/file.root \
//	go test ./xrootd/ -run TestTLSInterop -v
//
// Set XROOTD_TLS_INSECURE=1 to accept a self-signed server certificate.
func TestTLSInterop(t *testing.T) {
	server := os.Getenv("XROOTD_TLS_SERVER")
	if server == "" {
		t.Skip("set XROOTD_TLS_SERVER to run the TLS interop test")
	}
	path := os.Getenv("XROOTD_TLS_PATH")
	if path == "" {
		t.Skip("set XROOTD_TLS_PATH to a stat-able path on the server")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var opts []Option
	if os.Getenv("XROOTD_TLS_INSECURE") == "1" {
		opts = append(opts, WithInsecureTLS())
	}

	be, err := Dial(ctx, server, os.Getenv("USER"), opts...)
	if err != nil {
		t.Fatalf("Dial(%q) over TLS: %v", server, err)
	}
	defer be.Close()

	fi, err := be.FS().Stat(ctx, path)
	if err != nil {
		t.Fatalf("Stat(%q): %v", path, err)
	}
	t.Logf("stat ok: name=%s size=%d dir=%v", fi.Name(), fi.Size(), fi.IsDir())
}
