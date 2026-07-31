// Copyright ©2025 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdhttp

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/xrdfs"
)

// recorder counts the verbs that reached the server and keeps the Range
// headers it was asked for. A WebDAV client's cost is measured in requests —
// a file that flushes twice on close, or reads a whole object to answer a
// small ReadAt, is correct and useless — so the tests below assert on what
// arrived and not only on what came back.
type recorder struct {
	http.Handler

	mu     sync.Mutex
	verbs  map[string]int
	ranges []string
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.verbs[req.Method]++
	if rng := req.Header.Get("Range"); rng != "" {
		r.ranges = append(r.ranges, rng)
	}
	r.mu.Unlock()
	r.Handler.ServeHTTP(w, req)
}

func (r *recorder) count(verb string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.verbs[verb]
}

func (r *recorder) rangeHeaders() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ranges...)
}

func newRecordingFS(t *testing.T) (*davServer, xrdfs.FileSystem, *recorder) {
	t.Helper()
	dav := newDAVServer()
	rec := &recorder{Handler: dav, verbs: map[string]int{}}
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)

	c, err := Dial(srv.URL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	return dav, c.FS(), rec
}

// TestConformance_ReadAtAsksTheServerForTheRangeItNeeds is the property the
// whole read path exists for: a positional read of 4 bytes must be a ranged
// GET, not a download of the object with the other bytes discarded.
func TestConformance_ReadAtAsksTheServerForTheRangeItNeeds(t *testing.T) {
	ctx := context.Background()
	dav, fs, rec := newRecordingFS(t)
	dav.put("/data.bin", []byte("0123456789"))

	f, err := fs.Open(ctx, "/data.bin", xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close(ctx)

	for _, tc := range []struct {
		off  int64
		n    int
		want string
	}{
		{off: 0, n: 4, want: "0123"},
		{off: 3, n: 4, want: "3456"},
		{off: 6, n: 4, want: "6789"},
	} {
		p := make([]byte, tc.n)
		n, err := f.ReadAtContext(ctx, p, tc.off)
		if err != nil {
			t.Fatalf("ReadAt(%d): %v", tc.off, err)
		}
		if got := string(p[:n]); got != tc.want {
			t.Errorf("ReadAt(%d) is %q, want %q", tc.off, got, tc.want)
		}
	}

	if got := rec.rangeHeaders(); len(got) != 3 {
		t.Errorf("the server saw %d ranged requests, want 3: %v", len(got), got)
	}
	for _, rng := range rec.rangeHeaders() {
		if !strings.HasPrefix(rng, "bytes=") {
			t.Errorf("range header %q is not a byte range", rng)
		}
	}
}

// TestConformance_ReadAtPastTheEndIsEOF pins the io.ReaderAt contract at the
// boundary: a short read reports what it got *and* io.EOF, and a read that
// starts past the end reports nothing but io.EOF.
func TestConformance_ReadAtPastTheEndIsEOF(t *testing.T) {
	ctx := context.Background()
	dav, fs, _ := newRecordingFS(t)
	dav.put("/short.bin", []byte("abc"))

	f, err := fs.Open(ctx, "/short.bin", xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close(ctx)

	p := make([]byte, 8)
	n, err := f.ReadAtContext(ctx, p, 1)
	if n != 2 || string(p[:n]) != "bc" {
		t.Errorf("a read straddling the end returned %d bytes (%q), want 2 (%q)", n, p[:n], "bc")
	}
	if !errors.Is(err, io.EOF) {
		t.Errorf("a read straddling the end returned %v, want io.EOF", err)
	}

	n, err = f.ReadAtContext(ctx, p, 16)
	if n != 0 || !errors.Is(err, io.EOF) {
		t.Errorf("a read past the end returned (%d, %v), want (0, io.EOF)", n, err)
	}
}

// TestConformance_AWriteIsReadableBeforeItIsFlushed covers the half of
// ReadAtContext that never touches the network: an open-for-write file buffers
// until it is synced, and a caller that reads back what it just wrote must see
// its own bytes rather than the server's older ones.
func TestConformance_AWriteIsReadableBeforeItIsFlushed(t *testing.T) {
	ctx := context.Background()
	dav, fs, rec := newRecordingFS(t)
	dav.put("/w.bin", []byte("old content"))

	f, err := fs.Open(ctx, "/w.bin", xrdfs.OpenModeOwnerWrite, xrdfs.OpenOptionsDelete)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := f.WriteAt([]byte("new"), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}

	before := rec.count(http.MethodGet)
	p := make([]byte, 3)
	n, err := f.ReadAtContext(ctx, p, 0)
	if err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if got := string(p[:n]); got != "new" {
		t.Errorf("read back %q, want %q", got, "new")
	}
	if got := rec.count(http.MethodGet); got != before {
		t.Errorf("reading back a buffered write made %d GETs, want none", got-before)
	}

	// Past the end of the buffer the contract is the same as on the wire.
	if _, err := f.ReadAtContext(ctx, p, 3); !errors.Is(err, io.EOF) {
		t.Errorf("reading past the buffer returned %v, want io.EOF", err)
	}
	p8 := make([]byte, 8)
	if n, err := f.ReadAtContext(ctx, p8, 1); n != 2 || !errors.Is(err, io.EOF) {
		t.Errorf("a short read of the buffer returned (%d, %v), want (2, io.EOF)", n, err)
	}

	if err := f.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got, _ := dav.get("/w.bin"); string(got) != "new" {
		t.Errorf("the server holds %q after close, want %q", got, "new")
	}
}

// TestConformance_SyncUploadsAndCloseDoesNotUploadAgain pins the flush
// bookkeeping. Sync is a full PUT — HTTP has nothing finer — so a close that
// re-uploads unchanged content doubles the cost of every written file, and a
// close that skips a genuine change loses it.
func TestConformance_SyncUploadsAndCloseDoesNotUploadAgain(t *testing.T) {
	ctx := context.Background()
	dav, fs, rec := newRecordingFS(t)

	f, err := fs.Open(ctx, "/s.bin", xrdfs.OpenModeOwnerWrite, xrdfs.OpenOptionsNew)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := f.WriteAt([]byte("first"), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := f.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if got, _ := dav.get("/s.bin"); string(got) != "first" {
		t.Fatalf("the server holds %q after sync, want %q", got, "first")
	}
	puts := rec.count(http.MethodPut)

	// Nothing changed since the sync: closing must not upload again.
	if err := f.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := rec.count(http.MethodPut); got != puts {
		t.Errorf("closing an already-synced file made %d more PUTs, want none", got-puts)
	}

	// Closing twice is a no-op rather than a second upload or an error.
	if err := f.Close(ctx); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if got := rec.count(http.MethodPut); got != puts {
		t.Errorf("closing twice made %d more PUTs, want none", got-puts)
	}
}

// TestConformance_TruncateOnAnOpenFileGrowsAndShrinks covers the buffered
// truncate: growing zero-fills, shrinking drops the tail, and both are only
// visible to the server once the file is flushed.
func TestConformance_TruncateOnAnOpenFileGrowsAndShrinks(t *testing.T) {
	ctx := context.Background()
	dav, fs, _ := newRecordingFS(t)

	f, err := fs.Open(ctx, "/t.bin", xrdfs.OpenModeOwnerWrite, xrdfs.OpenOptionsNew)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := f.WriteAt([]byte("abcdef"), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}

	if err := f.Truncate(ctx, 3); err != nil {
		t.Fatalf("Truncate(3): %v", err)
	}
	if err := f.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if got, _ := dav.get("/t.bin"); string(got) != "abc" {
		t.Errorf("after truncating to 3 the server holds %q, want %q", got, "abc")
	}

	if err := f.Truncate(ctx, 5); err != nil {
		t.Fatalf("Truncate(5): %v", err)
	}
	if err := f.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got, _ := dav.get("/t.bin"); string(got) != "abc\x00\x00" {
		t.Errorf("after growing to 5 the server holds %q, want %q", got, "abc\x00\x00")
	}
}

// TestConformance_AReadOnlyFileRefusesToBeWritten checks the one guard the
// buffered file has: a file opened for reading has no buffer to write into,
// and silently accepting the write would lose it at close.
func TestConformance_AReadOnlyFileRefusesToBeWritten(t *testing.T) {
	ctx := context.Background()
	dav, fs, _ := newRecordingFS(t)
	dav.put("/ro.bin", []byte("data"))

	f, err := fs.Open(ctx, "/ro.bin", xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close(ctx)

	if _, err := f.WriteAt([]byte("x"), 0); err == nil {
		t.Error("writing to a read-only file succeeded, want an error")
	}
	if err := f.Truncate(ctx, 0); err == nil {
		t.Error("truncating a read-only file succeeded, want an error")
	}
	if got, _ := dav.get("/ro.bin"); string(got) != "data" {
		t.Errorf("the server holds %q, want it untouched (%q)", got, "data")
	}
}

// TestConformance_FileStatReportsWhatTheServerHolds checks that a file's stat
// follows the server rather than the buffer: the size a caller sees after a
// flush is the stored one.
func TestConformance_FileStatReportsWhatTheServerHolds(t *testing.T) {
	ctx := context.Background()
	_, fs, _ := newRecordingFS(t)

	f, err := fs.Open(ctx, "/st.bin", xrdfs.OpenModeOwnerWrite, xrdfs.OpenOptionsNew)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := f.WriteAt([]byte("0123456789"), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := f.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	es, err := f.Stat(ctx)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if es.Size() != 10 {
		t.Errorf("Stat reports %d bytes, want 10", es.Size())
	}
	if f.Info() == nil || f.Info().Size() != 10 {
		t.Errorf("Info was not refreshed by Stat: %+v", f.Info())
	}

	// HTTP is stateless: there is no server-side handle and no compression
	// negotiation to report, and claiming otherwise would let a caller act on
	// a handle no server knows.
	if got := f.Handle(); got != (xrdfs.FileHandle{}) {
		t.Errorf("Handle is %v, want the zero handle", got)
	}
	if got := f.Compression(); got != nil {
		t.Errorf("Compression is %+v, want nil", got)
	}

	if err := f.CloseVerify(ctx, 10); err != nil {
		t.Errorf("CloseVerify(10): %v", err)
	}
}

// TestConformance_CloseVerifyRejectsTheWrongSize is the other half: the check
// has to be able to fail, or every close passes it.
func TestConformance_CloseVerifyRejectsTheWrongSize(t *testing.T) {
	ctx := context.Background()
	_, fs, _ := newRecordingFS(t)

	f, err := fs.Open(ctx, "/cv.bin", xrdfs.OpenModeOwnerWrite, xrdfs.OpenOptionsNew)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := f.WriteAt([]byte("abc"), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := f.CloseVerify(ctx, 99); err == nil {
		t.Error("CloseVerify accepted a size the server does not hold, want an error")
	}
}

// TestConformance_NamespaceRemovalDistinguishesFilesFromCollections covers the
// three removals, which are the same DELETE on the wire and three different
// contracts above it: a file, an *empty* collection, and a recursive drop.
// WebDAV's DELETE on a collection is always recursive, so RemoveDir has to
// check emptiness itself or it silently deletes a tree.
func TestConformance_NamespaceRemovalDistinguishesFilesFromCollections(t *testing.T) {
	ctx := context.Background()
	dav, fs, _ := newRecordingFS(t)

	if err := fs.Mkdir(ctx, "/dir", xrdfs.OpenModeOwnerRead|xrdfs.OpenModeOwnerWrite); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	dav.put("/dir/a.txt", []byte("a"))
	dav.put("/loose.txt", []byte("l"))

	if err := fs.RemoveFile(ctx, "/loose.txt"); err != nil {
		t.Fatalf("RemoveFile: %v", err)
	}
	if _, ok := dav.get("/loose.txt"); ok {
		t.Error("the file is still on the server after RemoveFile")
	}

	if err := fs.RemoveDir(ctx, "/dir"); err == nil {
		t.Error("RemoveDir emptied a non-empty collection, want an error")
	}
	if _, ok := dav.get("/dir/a.txt"); !ok {
		t.Fatal("the refused RemoveDir deleted the collection's contents anyway")
	}

	if err := fs.RemoveAll(ctx, "/dir"); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if _, ok := dav.get("/dir/a.txt"); ok {
		t.Error("RemoveAll left the collection's contents behind")
	}

	// An empty collection is what RemoveDir is for.
	if err := fs.Mkdir(ctx, "/empty", xrdfs.OpenModeOwnerRead); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := fs.RemoveDir(ctx, "/empty"); err != nil {
		t.Errorf("RemoveDir on an empty collection: %v", err)
	}
}

// TestConformance_TimeoutBoundsARequest checks the option that keeps a stalled
// endpoint from parking a caller forever. A WebDAV server that accepts the
// connection and then says nothing is indistinguishable from a slow one until
// something ends the wait.
func TestConformance_TimeoutBoundsARequest(t *testing.T) {
	stall := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-stall
	}))
	t.Cleanup(func() {
		close(stall)
		srv.Close()
	})

	c, err := Dial(srv.URL, WithTimeout(100*time.Millisecond))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := c.Stat(context.Background(), "/anything")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("a stalled server returned no error, want a timeout")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the client is still waiting on a stalled server: WithTimeout did not bound it")
	}
}

// TestConformance_TLSOptionsDecideWhoIsTrusted covers the two option paths
// that change *who the client will talk to*: a self-signed server is refused
// by default, accepted with an explicit CA, and accepted by
// WithInsecureTLS — which must therefore be reachable only by asking for it.
func TestConformance_TLSOptionsDecideWhoIsTrusted(t *testing.T) {
	ctx := context.Background()
	dav := newDAVServer()
	dav.put("/f.txt", []byte("hello"))

	srv := httptest.NewTLSServer(dav)
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	for _, tc := range []struct {
		name    string
		opts    []Option
		wantErr bool
	}{
		{name: "no trust configured", wantErr: true},
		{name: "the server's CA", opts: []Option{WithRootCAs(pool)}},
		{name: "verification disabled", opts: []Option{WithInsecureTLS()}},
		{name: "a whole TLS config", opts: []Option{WithTLSConfig(srv.Client().Transport.(*http.Transport).TLSClientConfig)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Dial(srv.URL, tc.opts...)
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}
			_, err = c.Stat(ctx, "/f.txt")
			switch {
			case tc.wantErr && err == nil:
				t.Error("the client trusted a certificate it has no reason to trust")
			case !tc.wantErr && err != nil:
				t.Errorf("Stat: %v", err)
			}
		})
	}
}

// TestConformance_LoadX509KeyPairReadsAProxyFile covers the entry point an
// x509 caller actually uses: certificate and key read from disk. A grid proxy
// is a file, and the failure to load one has to be reported rather than
// producing a client that authenticates as nobody.
func TestConformance_LoadX509KeyPairReadsAProxyFile(t *testing.T) {
	ca := mkCert(t, "test-ca", nil, true, nil, nil)
	user := mkCert(t, "gopher", &ca, false, nil, nil)

	dir := t.TempDir()
	certFile := filepath.Join(dir, "usercert.pem")
	keyFile := filepath.Join(dir, "userkey.pem")

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: user.tlsCert.Certificate[0]})
	if err := os.WriteFile(certFile, certPEM, 0600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	der, err := x509.MarshalECPrivateKey(user.tlsCert.PrivateKey.(*ecdsa.PrivateKey))
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	got, err := LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("LoadX509KeyPair: %v", err)
	}
	if len(got.Certificate) == 0 {
		t.Error("the loaded pair carries no certificate")
	}

	if _, err := LoadX509KeyPair(filepath.Join(dir, "absent.pem"), keyFile); err == nil {
		t.Error("loading a missing certificate succeeded, want an error")
	}
}

// TestConformance_AnHrefIsReducedToItsPath covers the normalisation every
// PROPFIND result goes through. Servers disagree about what an href is —
// dCache answers with an absolute URL, XRootD's own HTTP layer with a bare
// path — and the listing compares hrefs against the requested path to drop the
// collection's own entry. An href that keeps its scheme and host never matches,
// so the collection lists itself as one of its own members.
func TestConformance_AnHrefIsReducedToItsPath(t *testing.T) {
	for _, tc := range []struct {
		href string
		want string
	}{
		{href: "/data/f.txt", want: "/data/f.txt"},
		{href: "https://dav.example.org/data/f.txt", want: "/data/f.txt"},
		{href: "http://dav.example.org:1094/data/f.txt", want: "/data/f.txt"},
		{href: "https://dav.example.org/data/", want: "/data/"},
		// A percent-encoded path is left as it is: it is compared against a
		// path that went through the same encoding.
		{href: "https://dav.example.org/data/a%20b.txt", want: "/data/a%20b.txt"},
		// An authority with no path at all has nothing to reduce.
		{href: "https://dav.example.org", want: "https://dav.example.org"},
	} {
		if got := hrefPath(tc.href); got != tc.want {
			t.Errorf("hrefPath(%q) is %q, want %q", tc.href, got, tc.want)
		}
	}
}

// TestConformance_TPCErrorSaysWhatFailed pins the message a caller sees when an
// endpoint accepts a copy and then announces its failure in the body. The
// status was 2xx, so the reason is the only thing that explains the outcome.
func TestConformance_TPCErrorSaysWhatFailed(t *testing.T) {
	err := error(&TPCError{Reason: "source is unreadable"})
	if got := err.Error(); !strings.Contains(got, "source is unreadable") {
		t.Errorf("the error reads %q, want it to carry the endpoint's reason", got)
	}

	var tpc *TPCError
	if !errors.As(err, &tpc) {
		t.Error("a TPC failure is not recoverable with errors.As")
	}
}
