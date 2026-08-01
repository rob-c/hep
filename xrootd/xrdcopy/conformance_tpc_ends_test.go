// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the two ends of a third-party copy before the copy begins.
//
// A TPC is arranged, not performed: this process talks to both servers and then
// steps out of the way while the destination pulls from the source. Everything
// that can go wrong before that hand-off — an address that is not a URL, a
// server that is not answering — has to be reported here, by this process,
// because once the transfer is delegated there is nobody left to report it to.
// A TPC that returned nil having never reached one of its ends is a copy that
// nobody is performing and everybody believes in.

package xrdcopy_test

import (
	"context"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdcopy"
)

func TestConformance_ATPCEndThatIsNotAURLIsRejected(t *testing.T) {
	// A bracketed host that is never closed. Both ends are parsed before
	// either is dialled, so neither should reach the network.
	const bad = "root://[::1//file.dat"

	srcURL, _ := tpcServe(t, &tpcServer{size: 1})
	dstURL, _ := tpcServe(t, &tpcServer{})

	for _, tc := range []struct{ name, dst, src string }{
		{"as the source", dstURL + "out.dat", bad},
		{"as the destination", bad, srcURL + "in.dat"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := xrdcopy.TPC(context.Background(), tc.dst, tc.src, xrdcopy.Options{}); err == nil {
				t.Fatal("a malformed URL was accepted as a third-party-copy end")
			}
		})
	}
}

func TestConformance_ATPCEndThatDoesNotAnswerIsReportedByName(t *testing.T) {
	noRedial(t)

	// One end is up and the other is not. Which one it is decides who gets
	// called at three in the morning, so the failure names it.
	const dead = "root://127.0.0.1:1/"

	t.Run("the source", func(t *testing.T) {
		dstURL, _ := tpcServe(t, &tpcServer{})

		err := xrdcopy.TPC(context.Background(), dstURL+"out.dat", dead+"in.dat", xrdcopy.Options{})
		if err == nil {
			t.Fatal("a third-party copy from a source that is not there succeeded")
		}
		if !strings.Contains(err.Error(), "source") {
			t.Fatalf("the failure says %q, want it to name the source", err)
		}
	})

	t.Run("the destination", func(t *testing.T) {
		srcURL, _ := tpcServe(t, &tpcServer{size: 1})

		err := xrdcopy.TPC(context.Background(), dead+"out.dat", srcURL+"in.dat", xrdcopy.Options{})
		if err == nil {
			t.Fatal("a third-party copy to a destination that is not there succeeded")
		}
		if !strings.Contains(err.Error(), "destination") {
			t.Fatalf("the failure says %q, want it to name the destination", err)
		}
	})
}
