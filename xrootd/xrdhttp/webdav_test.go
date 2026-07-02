// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDirlistWebDAV(t *testing.T) {
	const multistatus = `<?xml version="1.0"?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>/data/</D:href>
    <D:propstat><D:status>HTTP/1.1 200 OK</D:status>
      <D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop>
    </D:propstat>
  </D:response>
  <D:response>
    <D:href>/data/a.root</D:href>
    <D:propstat><D:status>HTTP/1.1 200 OK</D:status>
      <D:prop><D:displayname>a.root</D:displayname><D:getcontentlength>1024</D:getcontentlength><D:resourcetype/></D:prop>
    </D:propstat>
  </D:response>
  <D:response>
    <D:href>/data/sub/</D:href>
    <D:propstat><D:status>HTTP/1.1 200 OK</D:status>
      <D:prop><D:displayname>sub</D:displayname><D:resourcetype><D:collection/></D:resourcetype></D:prop>
    </D:propstat>
  </D:response>
</D:multistatus>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PROPFIND" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Depth") != "1" {
			t.Errorf("Depth header = %q, want 1", r.Header.Get("Depth"))
		}
		w.WriteHeader(http.StatusMultiStatus)
		w.Write([]byte(multistatus))
	}))
	defer srv.Close()

	c, err := Dial(srv.URL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	entries, err := c.Dirlist(context.Background(), "/data")
	if err != nil {
		t.Fatalf("Dirlist: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (collection self excluded): %+v", len(entries), entries)
	}
	byName := map[string]DirEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	if f, ok := byName["a.root"]; !ok || f.IsDir || f.Size != 1024 {
		t.Fatalf("a.root entry wrong: %+v", f)
	}
	if d, ok := byName["sub"]; !ok || !d.IsDir {
		t.Fatalf("sub entry wrong: %+v", d)
	}
}
