// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"net"
	"slices"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto/locate"
	"go-hep.org/x/hep/xrootd/xrdproto/login"
	"go-hep.org/x/hep/xrootd/xrdproto/protocol"
)

// bootLocator brings a bootstrapped connection up and then answers each
// kXR_locate with the next of answers, in order. Once they run out — and for
// any other request — it answers with an error, and it keeps reading until the
// client hangs up so that a session is never closed underneath a test.
func bootLocator(t *testing.T, conn net.Conn, answers ...string) {
	t.Helper()

	bootHandshake(t, conn)
	bootProtocol(t, conn, protocol.Response{BinaryProtocolVersion: 0x310, Flags: protocol.IsServer})
	bootLogin(t, conn, login.Response{})

	for {
		hdr, _ := readBootstrapRequest(conn)
		switch {
		case hdr.RequestID == 0:
			return
		case hdr.RequestID != locate.RequestID || len(answers) == 0:
			bootErrorFrame(conn, hdr.StreamID, "no answer for this request")
		default:
			writeBootstrapResponse(conn, hdr.StreamID, locate.Response{Data: []byte(answers[0])})
			answers = answers[1:]
		}
	}
}

// deepLocate dials addr and walks it.
func deepLocate(t *testing.T, addr string) ([]xrdfs.Location, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Unbounded: one of these servers points at an address nothing is
	// listening on, and observing that once is the test. Redialling it five
	// times measures the backoff schedule, which is pinned elsewhere.
	client, err := NewClient(ctx, addr, "gopher", Unbounded())
	if err != nil {
		t.Fatalf("could not connect: %v", err)
	}
	defer client.Close()

	return client.FS().(*fileSystem).DeepLocate(ctx, "/data/run42.root", xrdfs.LocateNone)
}

func addrsOf(locs []xrdfs.Location) []string {
	out := make([]string, len(locs))
	for i, loc := range locs {
		out[i] = loc.Addr
	}
	return out
}

func TestConformance_ADeepLocateAsksEveryManagerTheAnswerNames(t *testing.T) {
	// A locate against the top of a federation answers with the tier below it,
	// not with anything holding a byte. A caller handed those addresses and
	// told they were replicas would open a manager and read nothing.
	const dataA, dataB = "storeA.example.org:1094", "storeB.example.org:1094"

	mgr := bootServer(t, func(conn net.Conn) {
		bootLocator(t, conn, "Sr"+dataB)
	})
	root := bootServer(t, func(conn net.Conn) {
		bootLocator(t, conn, "Mr"+mgr+" Sr"+dataA)
	})

	locs, err := deepLocate(t, root)
	if err != nil {
		t.Fatalf("DeepLocate: %v", err)
	}

	want := []string{dataA, dataB}
	if got := addrsOf(locs); !slices.Equal(got, want) {
		t.Fatalf("locations: got = %v, want = %v", got, want)
	}
	for _, loc := range locs {
		if loc.IsManager() {
			t.Errorf("%q is a manager and was returned as a replica", loc.Addr)
		}
	}
}

func TestConformance_ASupervisorIsKeptAsTheServerItAlsoIs(t *testing.T) {
	// A supervisor answers as a manager to the tier above it and as a server
	// to the tier below. Recording only the first answer would drop a node
	// that does hold the file.
	const dataA = "storeA.example.org:1094"

	// The supervisor names itself, so its own address has to reach the script
	// that answers on it; a channel is how it gets there without the two
	// goroutines racing for the variable.
	self := make(chan string, 1)
	sup := bootServer(t, func(conn net.Conn) {
		bootLocator(t, conn, "Sr"+<-self+" Sr"+dataA)
	})
	self <- sup
	root := bootServer(t, func(conn net.Conn) {
		bootLocator(t, conn, "Mr"+sup)
	})

	locs, err := deepLocate(t, root)
	if err != nil {
		t.Fatalf("DeepLocate: %v", err)
	}

	want := []string{sup, dataA}
	if got := addrsOf(locs); !slices.Equal(got, want) {
		t.Fatalf("locations: got = %v, want = %v", got, want)
	}
}

func TestConformance_AnEndpointIsAskedOnce(t *testing.T) {
	// Two managers naming the same third one is ordinary in a federation, and
	// a walk that followed both would ask it twice — or, where the tree has a
	// cycle, forever. The manager below answers exactly one locate; a second
	// would come back as an error and lose the replica it reported.
	const data = "storeA.example.org:1094"

	mgr := bootServer(t, func(conn net.Conn) {
		bootLocator(t, conn, "Sr"+data)
	})
	root := bootServer(t, func(conn net.Conn) {
		bootLocator(t, conn, "Mr"+mgr+" Mr"+mgr)
	})

	locs, err := deepLocate(t, root)
	if err != nil {
		t.Fatalf("DeepLocate: %v", err)
	}

	if got, want := addrsOf(locs), []string{data}; !slices.Equal(got, want) {
		t.Fatalf("locations: got = %v, want = %v", got, want)
	}
}

func TestConformance_AnUnreachableSubtreeDoesNotHideTheRest(t *testing.T) {
	// One manager being down is the normal state of a large federation. If it
	// were fatal, a job would be told the file does not exist anywhere while
	// every other replica sat there readable.
	const data = "storeA.example.org:1094"

	// Nothing listens here: port 1 is privileged and the test does not bind it.
	const dead = "127.0.0.1:1"

	root := bootServer(t, func(conn net.Conn) {
		bootLocator(t, conn, "Mr"+dead+" Sr"+data)
	})

	locs, err := deepLocate(t, root)
	if err != nil {
		t.Fatalf("DeepLocate: %v", err)
	}

	if got, want := addrsOf(locs), []string{data}; !slices.Equal(got, want) {
		t.Fatalf("locations: got = %v, want = %v", got, want)
	}
}

func TestConformance_AManagerThatRefusesTheQuestionIsSkipped(t *testing.T) {
	// A manager that is reachable but answers kXR_error is no more use than
	// one that is down, and is treated the same way.
	const data = "storeA.example.org:1094"

	mgr := bootServer(t, func(conn net.Conn) {
		bootLocator(t, conn) // no answers: every locate comes back as an error
	})
	root := bootServer(t, func(conn net.Conn) {
		bootLocator(t, conn, "Mr"+mgr+" Sr"+data)
	})

	locs, err := deepLocate(t, root)
	if err != nil {
		t.Fatalf("DeepLocate: %v", err)
	}

	if got, want := addrsOf(locs), []string{data}; !slices.Equal(got, want) {
		t.Fatalf("locations: got = %v, want = %v", got, want)
	}
}

func TestConformance_ADeepLocateThatCannotStartIsAnError(t *testing.T) {
	// The first locate is the one that says whether the path exists at all.
	// Skipping a subtree is recovery; skipping the root is inventing an
	// answer, so this one failure is reported.
	root := bootServer(t, func(conn net.Conn) {
		bootLocator(t, conn) // no answers
	})

	if _, err := deepLocate(t, root); err == nil {
		t.Fatal("a deep locate whose first question failed reported success")
	}
}
