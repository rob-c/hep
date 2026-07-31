// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdio_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/xrdio"
)

// TestOpenHTTPScheme checks the URL scheme selects the transport. Before the
// scheme was honoured, an http:// name was handed to the native XRootD client
// and dialled as if it were root://.
func TestOpenHTTPScheme(t *testing.T) {
	const name = "/data/file.txt"
	data := []byte("the quick brown fox jumps over the lazy dog")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != name {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			return
		}
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()

	f, err := xrdio.Open(srv.URL + name)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if fi.Size() != int64(len(data)) {
		t.Fatalf("got size %d, want %d", fi.Size(), len(data))
	}
	if got := f.Name(); got != name {
		t.Fatalf("got name %q, want %q", got, name)
	}

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("got %q, want %q", got, data)
	}

	// A seek must land where it says: reads are Range requests, not a
	// sequential stream.
	if _, err := f.Seek(4, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	p := make([]byte, 5)
	if _, err := io.ReadFull(f, p); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(p, data[4:9]) {
		t.Fatalf("got %q, want %q", p, data[4:9])
	}
}

func TestOpenUnsupportedScheme(t *testing.T) {
	if _, err := xrdio.Open("s3://example.org/bucket/key"); err == nil {
		t.Fatal("an unsupported scheme was opened")
	}
}
