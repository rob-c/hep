// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for third-party copy.
//
// In a TPC the bytes never touch this process: the destination server opens the
// source itself and pulls. All this client does is set up the rendezvous, and
// every part of that setup is opaque data on an open — which means a mistake in
// it produces a perfectly valid open of the wrong kind rather than an error. A
// destination that is handed no tpc.key writes an empty file and reports
// success; one that is handed the source's key but not oss.asize transfers and
// then fails a size check at the far end. So the opaque strings, and the order
// the opens happen in, are the contract, and this file pins them against a
// server that records what it was asked.

package xrdcopy_test

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"testing"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdcopy"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/open"
	"go-hep.org/x/hep/xrootd/xrdproto/stat"
	xrdsync "go-hep.org/x/hep/xrootd/xrdproto/sync"
	"go-hep.org/x/hep/xrootd/xrdproto/xrdclose"
)

// tpcEvent is one thing a server was asked to do, in the order it was asked.
type tpcEvent struct {
	op   string // "stat", "open", "sync", "close"
	path string // for an open, the path with its opaque data still attached
}

// tpcServer is an XRootD server that implements just enough to be one end of a
// third-party copy and records what the client asked of it. Everything it does
// not override — handshake, login, protocol — it inherits, because none of that
// is what a TPC exercises.
type tpcServer struct {
	xrootd.Handler

	size     int64  // the size to report for any stat
	statErr  bool   // refuse stat requests
	openErr  string // refuse opens whose path contains this (empty: refuse none)
	syncErr  bool   // refuse sync requests
	failSync int    // refuse the nth sync (1-based); 0 means none

	mu     sync.Mutex
	events []tpcEvent
	syncs  int
	handle byte
}

func (s *tpcServer) record(op, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, tpcEvent{op: op, path: path})
}

func (s *tpcServer) log() []tpcEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]tpcEvent(nil), s.events...)
}

// opens returns the paths, opaque data included, of every open in order.
func (s *tpcServer) opens() []string {
	var out []string
	for _, ev := range s.log() {
		if ev.op == "open" {
			out = append(out, ev.path)
		}
	}
	return out
}

func (s *tpcServer) Stat(sessionID [16]byte, req *stat.Request) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
	s.record("stat", req.Path)
	if s.statErr {
		return xrdproto.ServerError{Code: xrdproto.NotFound, Message: "no such file"}, xrdproto.Error
	}
	return &stat.DefaultResponse{EntryStat: xrdfs.EntryStat{
		EntryName:   req.Path,
		HasStatInfo: true,
		EntrySize:   s.size,
		Flags:       xrdfs.StatIsReadable | xrdfs.StatIsWritable,
	}}, xrdproto.Ok
}

func (s *tpcServer) Open(sessionID [16]byte, req *open.Request) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
	s.record("open", req.Path)
	if s.openErr != "" && strings.Contains(req.Path, s.openErr) {
		return xrdproto.ServerError{Code: xrdproto.NotAuthorized, Message: "open refused"}, xrdproto.Error
	}
	s.mu.Lock()
	s.handle++
	h := s.handle
	s.mu.Unlock()
	return &open.Response{FileHandle: xrdfs.FileHandle{h}}, xrdproto.Ok
}

func (s *tpcServer) Sync(sessionID [16]byte, req *xrdsync.Request) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
	s.mu.Lock()
	s.syncs++
	n := s.syncs
	s.mu.Unlock()
	s.record("sync", "")
	if s.syncErr || (s.failSync != 0 && n == s.failSync) {
		return xrdproto.ServerError{Code: xrdproto.FSError, Message: "transfer failed"}, xrdproto.Error
	}
	return nil, xrdproto.Ok
}

func (s *tpcServer) Close(sessionID [16]byte, req *xrdclose.Request) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
	s.record("close", "")
	return nil, xrdproto.Ok
}

// tpcServe starts srv on a fresh port and returns the root:// prefix and the
// host:port a peer would be told to connect back to.
func tpcServe(t *testing.T, srv *tpcServer) (url, addr string) {
	t.Helper()

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}
	// Handshake, login and protocol come from the default handler; only the
	// operations a TPC actually uses are overridden above.
	srv.Handler = xrootd.Default()
	s := xrootd.NewServer(srv, func(err error) { t.Logf("tpc-srv: %v", err) })
	go func() {
		if err := s.Serve(listener); err != nil && err != xrootd.ErrServerClosed {
			t.Logf("tpc-srv: could not serve: %v", err)
		}
	}()
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

	addr = listener.Addr().String()
	return fmt.Sprintf("root://%s/", addr), addr
}

// tpcOpaque splits "path?k=v&k=v" into the path and a map of its opaque data.
func tpcOpaque(t *testing.T, s string) (string, map[string]string) {
	t.Helper()
	path, q, ok := strings.Cut(s, "?")
	if !ok {
		return path, nil
	}
	kv := make(map[string]string)
	for _, field := range strings.Split(q, "&") {
		if field == "" {
			continue
		}
		k, v, _ := strings.Cut(field, "=")
		kv[k] = v
	}
	return path, kv
}

func TestConformance_ATPCSetsUpTheRendezvousOnBothServers(t *testing.T) {
	const size = 4096
	src := &tpcServer{size: size}
	dst := &tpcServer{}
	srcURL, srcAddr := tpcServe(t, src)
	dstURL, dstAddr := tpcServe(t, dst)
	dstHost, _, _ := net.SplitHostPort(dstAddr)

	if err := xrdcopy.TPC(context.Background(), dstURL+"out.dat", srcURL+"in.dat", xrdcopy.Options{}); err != nil {
		t.Fatalf("could not run a third-party copy: %v", err)
	}

	// The source is asked its size first: the destination needs it up front,
	// because oss.asize is what lets the destination reserve space and
	// recognise a short transfer.
	srcLog := src.log()
	if len(srcLog) == 0 || srcLog[0].op != "stat" {
		t.Fatalf("the source was not sized before the transfer: %v", srcLog)
	}
	if got, want := srcLog[0].path, "/in.dat"; got != want {
		t.Fatalf("the source was sized as %q, want %q", got, want)
	}

	srcOpens := src.opens()
	if len(srcOpens) != 2 {
		t.Fatalf("the source was opened %d times, want 2: %q", len(srcOpens), srcOpens)
	}

	// The first source open is the placement probe the stock client sends:
	// it asks the source to prepare, and carries no key because there is
	// nothing to rendezvous with yet.
	probePath, probe := tpcOpaque(t, srcOpens[0])
	if probePath != "/in.dat" {
		t.Fatalf("the placement probe opened %q, want %q", probePath, "/in.dat")
	}
	if got, want := probe["tpc.stage"], "placement"; got != want {
		t.Fatalf("the placement probe named stage %q, want %q", got, want)
	}
	if _, ok := probe["tpc.key"]; ok {
		t.Fatalf("the placement probe carried a rendezvous key: %q", srcOpens[0])
	}

	// The second is the coordinator open: it registers the key against the
	// destination that is allowed to use it.
	coordPath, coord := tpcOpaque(t, srcOpens[1])
	if coordPath != "/in.dat" {
		t.Fatalf("the coordinator opened %q, want %q", coordPath, "/in.dat")
	}
	if got, want := coord["tpc.stage"], "copy"; got != want {
		t.Fatalf("the coordinator named stage %q, want %q", got, want)
	}
	if got := coord["tpc.dst"]; got != dstHost {
		t.Fatalf("the coordinator authorised %q to pull, want %q", got, dstHost)
	}
	key := coord["tpc.key"]
	if key == "" {
		t.Fatal("the coordinator registered no rendezvous key")
	}

	// The destination is opened once, and is told everything it needs to
	// reach the source on its own.
	dstOpens := dst.opens()
	if len(dstOpens) != 1 {
		t.Fatalf("the destination was opened %d times, want 1: %q", len(dstOpens), dstOpens)
	}
	pullPath, pull := tpcOpaque(t, dstOpens[0])
	if pullPath != "/out.dat" {
		t.Fatalf("the destination opened %q, want %q", pullPath, "/out.dat")
	}
	if got := pull["tpc.key"]; got != key {
		t.Fatalf("the destination was given key %q, want the one registered on the source, %q", got, key)
	}
	for _, tc := range []struct{ field, want string }{
		{"tpc.src", srcAddr},
		{"tpc.dlg", srcAddr},
		{"tpc.lfn", "/in.dat"},
		{"tpc.stage", "copy"},
		{"tpc.dlgon", "0"},
		{"tpc.spr", "root"},
		{"tpc.tpr", "root"},
		{"oss.asize", fmt.Sprint(size)},
	} {
		if got := pull[tc.field]; got != tc.want {
			t.Errorf("the destination was given %s=%q, want %q", tc.field, got, tc.want)
		}
	}
}

func TestConformance_ATPCKeepsTheSourceRegistrationOpenUntilTheTransferEnds(t *testing.T) {
	// The key lives for as long as the coordinator handle does. Closing it
	// before the destination has pulled unregisters the key, and the
	// destination's own open is then refused by the source for a reason that
	// has nothing to do with this client.
	src := &tpcServer{size: 1}
	dst := &tpcServer{}
	srcURL, _ := tpcServe(t, src)
	dstURL, _ := tpcServe(t, dst)

	if err := xrdcopy.TPC(context.Background(), dstURL+"out.dat", srcURL+"in.dat", xrdcopy.Options{}); err != nil {
		t.Fatalf("could not run a third-party copy: %v", err)
	}

	// Of the source's two opens, only the placement probe is closed before
	// the destination is opened at all; the coordinator's close comes last.
	var sawCoordinator bool
	for _, ev := range src.log() {
		switch {
		case ev.op == "open" && strings.Contains(ev.path, "tpc.stage=copy"):
			sawCoordinator = true
		case ev.op == "close" && sawCoordinator:
			// The coordinator close is the last thing on the source.
			if got := dst.syncs; got != 2 {
				t.Fatalf("the coordinator was closed after %d destination syncs, want 2", got)
			}
			return
		}
	}
	t.Fatal("the coordinator handle was never closed")
}

func TestConformance_ATPCDrivesTheTransferWithTwoSyncs(t *testing.T) {
	// The destination's open only registers the pull. The first sync starts
	// the copy job and the second blocks until it finishes, which is how the
	// client learns the transfer succeeded rather than merely started.
	src := &tpcServer{size: 1}
	dst := &tpcServer{}
	srcURL, _ := tpcServe(t, src)
	dstURL, _ := tpcServe(t, dst)

	if err := xrdcopy.TPC(context.Background(), dstURL+"out.dat", srcURL+"in.dat", xrdcopy.Options{}); err != nil {
		t.Fatalf("could not run a third-party copy: %v", err)
	}
	if got := dst.syncs; got != 2 {
		t.Fatalf("the destination was synced %d times, want 2", got)
	}
	if got := src.syncs; got != 0 {
		t.Fatalf("the source was synced %d times, want 0: the source does not drive the transfer", got)
	}

	// The syncs come after the open, on the same connection.
	log := dst.log()
	var opened bool
	for _, ev := range log {
		switch ev.op {
		case "open":
			opened = true
		case "sync":
			if !opened {
				t.Fatalf("the destination was synced before it was opened: %v", log)
			}
		}
	}
}

func TestConformance_EveryTPCGetsItsOwnRendezvousKey(t *testing.T) {
	// The key is what stops one transfer being hijacked by another. Two
	// copies run against the same pair of servers must not share one.
	src := &tpcServer{size: 1}
	dst := &tpcServer{}
	srcURL, _ := tpcServe(t, src)
	dstURL, _ := tpcServe(t, dst)

	for i := range 2 {
		if err := xrdcopy.TPC(context.Background(), dstURL+"out.dat", srcURL+"in.dat", xrdcopy.Options{}); err != nil {
			t.Fatalf("could not run third-party copy %d: %v", i, err)
		}
	}

	// The stock client mints 12 random bytes and writes them as lowercase
	// hex; a server that logs the key expects that shape.
	hex24 := regexp.MustCompile(`^[0-9a-f]{24}$`)
	var keys []string
	for _, path := range src.opens() {
		_, kv := tpcOpaque(t, path)
		if k := kv["tpc.key"]; k != "" {
			if !hex24.MatchString(k) {
				t.Fatalf("rendezvous key %q is not 24 lowercase hex characters", k)
			}
			keys = append(keys, k)
		}
	}
	if len(keys) != 2 {
		t.Fatalf("got %d rendezvous keys over two copies, want 2", len(keys))
	}
	if keys[0] == keys[1] {
		t.Fatalf("two copies shared the rendezvous key %q", keys[0])
	}
}

func TestConformance_ATPCNeedsTwoRemoteEnds(t *testing.T) {
	// A local path on either end is a plain download or upload. Reaching TPC
	// with one would connect to a host named after a directory.
	srcURL, _ := tpcServe(t, &tpcServer{size: 1})
	dstURL, _ := tpcServe(t, &tpcServer{})

	for _, tc := range []struct{ name, dst, src string }{
		{"local-source", dstURL + "out.dat", "/tmp/in.dat"},
		{"local-destination", "/tmp/out.dat", srcURL + "in.dat"},
		{"both-local", "/tmp/out.dat", "/tmp/in.dat"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := xrdcopy.TPC(context.Background(), tc.dst, tc.src, xrdcopy.Options{})
			if err == nil {
				t.Fatal("a third-party copy was accepted with a local end")
			}
			if !strings.Contains(err.Error(), "two XRootD URLs") {
				t.Fatalf("the failure does not say what a TPC needs: %v", err)
			}
		})
	}
}

func TestConformance_ATPCFailureNamesTheStageThatFailed(t *testing.T) {
	// A TPC is four exchanges against two servers, and an operator reading a
	// log needs to know which one to look at: a refused source open is a
	// permissions problem on the source, a refused second sync is a transfer
	// that started and then broke.
	for _, tc := range []struct {
		name string
		src  *tpcServer
		dst  *tpcServer
		want string
	}{
		{
			name: "source-stat",
			src:  &tpcServer{statErr: true},
			dst:  &tpcServer{},
			want: "TPC source stat failed",
		},
		{
			name: "source-coordinator-open",
			src:  &tpcServer{size: 1, openErr: "tpc.stage=copy"},
			dst:  &tpcServer{},
			want: "TPC source coordinator open failed",
		},
		{
			name: "destination-open",
			src:  &tpcServer{size: 1},
			dst:  &tpcServer{openErr: "tpc.key"},
			want: "TPC open failed",
		},
		{
			name: "transfer-start",
			src:  &tpcServer{size: 1},
			dst:  &tpcServer{failSync: 1},
			want: "TPC start failed",
		},
		{
			name: "transfer-completion",
			src:  &tpcServer{size: 1},
			dst:  &tpcServer{failSync: 2},
			want: "TPC transfer failed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srcURL, _ := tpcServe(t, tc.src)
			dstURL, _ := tpcServe(t, tc.dst)

			err := xrdcopy.TPC(context.Background(), dstURL+"out.dat", srcURL+"in.dat", xrdcopy.Options{})
			if err == nil {
				t.Fatal("a third-party copy succeeded against a server that refused it")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the failure does not name the stage: got %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestConformance_APlacementProbeThatFailsDoesNotStopTheCopy(t *testing.T) {
	// The probe is a courtesy to servers that stage from tape. A server that
	// does not recognise it refuses the open, and that is not a reason to
	// abandon a transfer the server is otherwise willing to do.
	src := &tpcServer{size: 1, openErr: "tpc.stage=placement"}
	dst := &tpcServer{}
	srcURL, _ := tpcServe(t, src)
	dstURL, _ := tpcServe(t, dst)

	if err := xrdcopy.TPC(context.Background(), dstURL+"out.dat", srcURL+"in.dat", xrdcopy.Options{}); err != nil {
		t.Fatalf("a refused placement probe stopped the copy: %v", err)
	}
	if got := dst.syncs; got != 2 {
		t.Fatalf("the destination was synced %d times, want 2", got)
	}
}

func TestConformance_ARemoteToRemoteCopyGoesThroughTPC(t *testing.T) {
	// Copy dispatches on where each end lives; two remote ends is the case
	// that must not fall through to a download into a file named root://...
	src := &tpcServer{size: 1}
	dst := &tpcServer{}
	srcURL, _ := tpcServe(t, src)
	dstURL, _ := tpcServe(t, dst)

	if err := xrdcopy.Copy(context.Background(), dstURL+"out.dat", srcURL+"in.dat", xrdcopy.Options{}); err != nil {
		t.Fatalf("could not copy between two servers: %v", err)
	}
	for _, path := range src.opens() {
		if strings.Contains(path, "tpc.stage=copy") {
			return
		}
	}
	t.Fatalf("Copy did not set up a third-party copy: %q", src.opens())
}
