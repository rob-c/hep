// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrds3_test // import "go-hep.org/x/hep/xrootd/xrds3"

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-hep.org/x/hep/xrootd/xrds3"
)

// TestConformance_TheClientIsBuildableFromOutsideTheModule pins the one thing
// this test file's package clause is here to check: xrds3 must be usable by a
// caller that cannot import go-hep.org/x/hep/xrootd/internal/s3cred.
//
// New takes a credential pair, and the type of that pair used to be nameable
// only through the internal package — so every declaration a caller needed to
// write in order to call New was rejected by the import rule, and the package
// was public but unreachable. This test lives in xrds3_test, outside the
// package, and names every type it uses; it stops compiling the moment that is
// true again.
func TestConformance_TheClientIsBuildableFromOutsideTheModule(t *testing.T) {
	var provider xrds3.Provider = xrds3.Provider{
		AccessKey: "AKIAIOSFODNN7EXAMPLE",
		Secret:    "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}
	var creds xrds3.Credentials
	creds, err := provider.Resolve()
	if err != nil {
		t.Fatalf("could not resolve the credentials: %+v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/bucket/key"; got != want {
			t.Errorf("got path %q, want %q", got, want)
		}
		if r.Header.Get("Authorization") == "" {
			t.Error("request carries no signature")
		}
		w.Header().Set("Content-Length", "3")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cli := xrds3.New(srv.URL, "bucket", creds, xrds3.WithRegion("eu-west-1"))
	size, exists, err := cli.Stat(context.Background(), "key")
	if err != nil {
		t.Fatalf("could not stat: %+v", err)
	}
	if !exists {
		t.Fatal("object reported as absent")
	}
	if got, want := size, int64(3); got != want {
		t.Errorf("got size %d, want %d", got, want)
	}
}
