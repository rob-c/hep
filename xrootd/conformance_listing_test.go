// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for listing a namespace across a link that does not hold.
//
// A listing of one directory is one request, and either it worked or it did
// not. A walk of a subtree is dozens or hundreds of them over minutes, which is
// long enough for a wide-area connection to be lost in the middle — and the
// answer that matters is not whether the failed listing is reported, but what
// happens to the rest of the tree. A walk that gives up on the first lost
// connection reports a namespace far smaller than it is, and nothing about the
// result says so.
//
// These run against a real TCP server with a real namespace behind it, using
// the bootstrap helpers in conformance_redirect_test.go.

package xrootd

import (
	"context"
	"io/fs"
	"net"
	"path"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto/dirlist"
	"go-hep.org/x/hep/xrootd/xrdproto/stat"
	"go-hep.org/x/hep/xrootd/xrdproto/statx"
)

// treeNamespace is the namespace the listing server answers for: four
// directories under /top, two files in each, so a walk is one listing of /top
// and four below it. Big enough that a connection can be lost in the middle of
// it and there is still a tree left to finish.
var treeNamespace = map[string][]string{
	"/top":    {"d0", "d1", "d2", "d3"},
	"/top/d0": {"a.root", "b.root"},
	"/top/d1": {"a.root", "b.root"},
	"/top/d2": {"a.root", "b.root"},
	"/top/d3": {"a.root", "b.root"},
}

// treeServer answers stat and dirlist over treeNamespace, and severs the
// connection once, after a set number of requests. It keeps listening, so the
// client can reconnect — a lost connection, not a server that has died.
type treeServer struct {
	addr string
	ln   net.Listener

	mu sync.Mutex
	// severAfter is how many requests to answer before dropping the
	// connection. -1 answers everything.
	severAfter int
	// served counts requests answered on any connection.
	served int
	// severed records that the drop has happened, so it happens once.
	severed bool
	// listed records every path that was listed, which is how a test tells
	// "the walk stopped" from "the walk carried on".
	listed []string
}

func newTreeServer(t *testing.T, severAfter int) *treeServer {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	srv := &treeServer{addr: ln.Addr().String(), ln: ln, severAfter: severAfter}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.serve(conn)
		}
	}()
	return srv
}

func (srv *treeServer) serve(conn net.Conn) {
	defer conn.Close()

	if _, ok := redirBootstrap(conn); !ok {
		return
	}

	for {
		hdr, body := readBootstrapRequest(conn)
		if body == nil {
			return
		}

		srv.mu.Lock()
		sever := !srv.severed && srv.severAfter >= 0 && srv.served >= srv.severAfter
		if sever {
			srv.severed = true
		} else {
			srv.served++
		}
		srv.mu.Unlock()

		if sever {
			// The socket goes without a reply, which is what a link that
			// drops looks like from this end.
			return
		}

		switch hdr.RequestID {
		case stat.RequestID:
			var req stat.Request
			_ = req.UnmarshalXrd(xrdenc.NewRBuffer(body))
			writeBootstrapResponse(conn, hdr.StreamID, &stat.DefaultResponse{
				EntryStat: srv.entry(cleanPath(req.Path)),
			})
		case dirlist.RequestID:
			var req dirlist.Request
			_ = req.UnmarshalXrd(xrdenc.NewRBuffer(body))
			name := cleanPath(req.Path)

			srv.mu.Lock()
			srv.listed = append(srv.listed, name)
			srv.mu.Unlock()

			var entries []xrdfs.EntryStat
			for _, child := range treeNamespace[name] {
				entries = append(entries, srv.entry(path.Join(name, child)))
			}
			writeBootstrapResponse(conn, hdr.StreamID, &dirlist.Response{
				Entries: entries, WithStatInfo: true,
			})
		case statx.RequestID:
			var req statx.Request
			_ = req.UnmarshalXrd(xrdenc.NewRBuffer(body))
			var flags []xrdfs.StatFlags
			for _, p := range strings.Split(req.Paths, "\n") {
				flags = append(flags, srv.entry(cleanPath(p)).Flags)
			}
			writeBootstrapResponse(conn, hdr.StreamID, &statx.Response{StatFlags: flags})
		default:
			writeBootstrapResponse(conn, hdr.StreamID, rawBody(nil))
		}
	}
}

// entry is the stat line for one path: a directory if the namespace has a
// listing for it, a file otherwise.
func (srv *treeServer) entry(name string) xrdfs.EntryStat {
	es := xrdfs.EntryStat{
		EntryName:   path.Base(name),
		HasStatInfo: true,
		EntrySize:   10,
	}
	if _, ok := treeNamespace[name]; ok {
		es.Flags |= xrdfs.StatIsDir
		es.EntrySize = 0
	}
	return es
}

// listings returns the paths listed so far, in order.
func (srv *treeServer) listings() []string {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	return append([]string(nil), srv.listed...)
}

// cleanPath drops the opaque data a request may carry and normalises the path,
// so the namespace can be a plain map.
func cleanPath(p string) string {
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	return path.Clean(p)
}

func treeClient(t *testing.T, addr string) (*Client, context.Context) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	cli, err := NewClient(ctx, addr, "gopher")
	if err != nil {
		t.Fatalf("could not create the client: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	return cli, ctx
}

// TestConformance_AWalkOfAHealthyNamespaceIsComplete is the baseline the lossy
// cases are measured against: every directory listed once, every file reported.
func TestConformance_AWalkOfAHealthyNamespaceIsComplete(t *testing.T) {
	srv := newTreeServer(t, -1)
	cli, ctx := treeClient(t, srv.addr)

	var files []string
	err := xrdfs.Walk(ctx, cli.FS(), "/top", func(p string, e xrdfs.EntryStat, err error) error {
		if err != nil {
			t.Errorf("the walk of a healthy namespace reported %s as unreadable: %v", p, err)
			return nil
		}
		if !e.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the walk failed: %v", err)
	}

	if got, want := len(files), 8; got != want {
		t.Fatalf("the walk found %d files, want %d: %v", got, want, files)
	}
	if got, want := len(srv.listings()), 5; got != want {
		t.Fatalf("the walk made %d listings, want %d: %v", got, want, srv.listings())
	}
}

// TestConformance_AWalkSurvivesAConnectionLostMidWay: the connection is severed
// after the second listing, so one directory in the middle of the tree cannot
// be read. That directory has to be reported to the caller — a walk that
// swallowed it would return a namespace two files short with nothing to say so
// — and the directories after it have to be listed anyway, on a connection the
// client re-establishes for itself.
//
// This is the property that decides whether a walk over a wide-area link is
// usable at all. A walk that aborts on the first lost connection is a walk that
// never finishes a large namespace.
func TestConformance_AWalkSurvivesAConnectionLostMidWay(t *testing.T) {
	// One stat of /top, one listing of /top, then the listing of d0 is the
	// request that is lost.
	srv := newTreeServer(t, 2)
	cli, ctx := treeClient(t, srv.addr)

	var (
		files  []string
		failed []string
	)
	err := xrdfs.Walk(ctx, cli.FS(), "/top", func(p string, e xrdfs.EntryStat, err error) error {
		switch {
		case err != nil:
			failed = append(failed, p)
		case !e.IsDir():
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("a walk that met one lost connection failed outright: %v", err)
	}

	if len(failed) == 0 {
		t.Fatal("the lost connection was never reported: a directory that could not be read was walked past in silence")
	}
	if len(failed) > 1 {
		t.Fatalf("one lost connection cost %d directories, want 1: %v", len(failed), failed)
	}

	// Three of the four directories were readable, so six of the eight files
	// have to be there.
	if got, want := len(files), 6; got != want {
		t.Fatalf("the walk found %d files after one lost connection, want %d: %v", got, want, files)
	}

	// And the proof that it reconnected rather than being answered from
	// anything cached: the directories after the loss were really listed.
	if got, want := len(srv.listings()), 4; got < want {
		t.Fatalf("only %d listings reached the server, want at least %d: the walk stopped at the loss instead of reconnecting", got, want)
	}
}

// TestConformance_AWalkThatGivesUpStopsListing: the other half of the rule
// above. Reporting an unreadable directory is the caller's decision point, and
// a caller who returns the error has to end the walk — otherwise "stop" means
// "carry on and tell me again", and a namespace behind a link that is down
// would be walked in full, one failed listing at a time.
func TestConformance_AWalkThatGivesUpStopsListing(t *testing.T) {
	srv := newTreeServer(t, 2)
	cli, ctx := treeClient(t, srv.addr)

	var seen error
	err := xrdfs.Walk(ctx, cli.FS(), "/top", func(p string, e xrdfs.EntryStat, err error) error {
		if err != nil {
			seen = err
			return err
		}
		return nil
	})
	if err == nil {
		t.Fatal("the walk returned nil after its callback returned an error")
	}
	if err != seen {
		t.Fatalf("the walk returned %v, want the error its callback gave it: %v", err, seen)
	}
}

// TestConformance_ASkippedDirectoryIsNotListed: a caller that knows a subtree
// is not worth the round trips says so with fs.SkipDir, and the listing must
// not be made. Over a link with a hundred milliseconds of latency this is the
// difference between a walk that takes a minute and one that takes ten.
func TestConformance_ASkippedDirectoryIsNotListed(t *testing.T) {
	srv := newTreeServer(t, -1)
	cli, ctx := treeClient(t, srv.addr)

	err := xrdfs.Walk(ctx, cli.FS(), "/top", func(p string, e xrdfs.EntryStat, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() && (p == "/top/d1" || p == "/top/d2") {
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the walk failed: %v", err)
	}

	got := srv.listings()
	sort.Strings(got)
	want := []string{"/top", "/top/d0", "/top/d3"}
	if len(got) != len(want) {
		t.Fatalf("the walk listed %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the walk listed %v, want %v", got, want)
		}
	}
}

// TestConformance_AGlobListsOnlyWhatThePatternCanReach: a glob over a namespace
// is a walk of the literal prefix and nothing else. A glob that listed from the
// root would make a pattern naming one subtree cost a listing of every other
// one — on a real namespace, minutes of round trips for an answer that could
// not have come from any of them.
func TestConformance_AGlobListsOnlyWhatThePatternCanReach(t *testing.T) {
	srv := newTreeServer(t, -1)
	cli, ctx := treeClient(t, srv.addr)

	got, err := xrdfs.Glob(ctx, cli.FS(), "/top/d1/*.root")
	if err != nil {
		t.Fatalf("the glob failed: %v", err)
	}

	want := []string{"/top/d1/a.root", "/top/d1/b.root"}
	if len(got) != len(want) {
		t.Fatalf("the glob matched %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the glob matched %v, want %v", got, want)
		}
	}

	for _, listed := range srv.listings() {
		if listed != "/top/d1" {
			t.Fatalf("a glob under /top/d1 listed %q: the pattern's literal prefix is not being used to bound the walk", listed)
		}
	}
}

// TestConformance_AGlobCarriesOnPastAnUnreadableDirectory: a glob has no way to
// report a directory it could not read — filepath.Glob has none either — so the
// question is what it does with the rest. Stopping would make a token that
// covers most of a namespace return the matches from whichever subtree happened
// to be listed first.
func TestConformance_AGlobCarriesOnPastAnUnreadableDirectory(t *testing.T) {
	// The walk stats /top, lists /top, then loses the listing of d0.
	srv := newTreeServer(t, 2)
	cli, ctx := treeClient(t, srv.addr)

	got, err := xrdfs.Glob(ctx, cli.FS(), "/top/**/b.root")
	if err != nil {
		t.Fatalf("a glob that met one lost connection failed outright: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("one lost connection cost the whole glob: nothing was matched below the directories that were readable")
	}
	for _, p := range got {
		if !strings.HasSuffix(p, "/b.root") {
			t.Fatalf("the glob matched %q, which the pattern does not name", p)
		}
	}
}

// TestConformance_ABatchStatAnswersPerPath: Statx is one request for many
// paths, which is what makes checking a job's input list affordable. The answer
// has to be per path — a client that failed the batch because one path is not
// there would lose the answers for every path that is.
func TestConformance_ABatchStatAnswersPerPath(t *testing.T) {
	srv := newTreeServer(t, -1)
	cli, ctx := treeClient(t, srv.addr)

	paths := []string{"/top/d0/a.root", "/top/d1", "/top/d2/b.root"}
	flags, err := cli.FS().Statx(ctx, paths)
	if err != nil {
		t.Fatalf("the batch stat failed: %v", err)
	}
	if got, want := len(flags), len(paths); got != want {
		t.Fatalf("the batch stat answered %d paths, want %d", got, want)
	}
	if flags[0]&xrdfs.StatIsDir != 0 {
		t.Errorf("%s came back as a directory", paths[0])
	}
	if flags[1]&xrdfs.StatIsDir == 0 {
		t.Errorf("%s came back as a file", paths[1])
	}

	// One request, whatever the length of the list.
	if got := len(srv.listings()); got != 0 {
		t.Fatalf("the batch stat made %d listings: it is being answered one path at a time", got)
	}
}
