// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for a server that answers badly. A PROPFIND response is an XML
// document chosen by the other end and parsed before anything in it has been
// checked, which makes it this package's widest attack surface: the native
// side has `conformance_hostile_test.go` for the same reason. Nothing here may
// panic, hang, allocate without bound, or fetch anything the document points
// at — every case must come back as an ordinary error.

package xrdhttp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestConformance_AHostileMultistatusIsAnErrorNotACrash drives the parser with
// documents a cooperating server would never send.
func TestConformance_AHostileMultistatusIsAnErrorNotACrash(t *testing.T) {
	for _, tc := range []struct {
		desc string
		body string
	}{
		{desc: "an empty body", body: ""},
		{desc: "whitespace", body: "   \n\t "},
		{desc: "not xml at all", body: "<html><body>502 Bad Gateway</body></html>"},
		{desc: "truncated mid-element", body: `<?xml version="1.0"?><D:multistatus xmlns:D="DAV:"><D:response><D:href>/a`},
		{desc: "an unclosed root", body: `<?xml version="1.0"?><D:multistatus xmlns:D="DAV:">`},
		{desc: "a mismatched close tag", body: `<multistatus><response></multistatus></response>`},
		{desc: "a root that is not a multistatus", body: `<?xml version="1.0"?><D:error xmlns:D="DAV:"><D:lock-token-submitted/></D:error>`},
		{desc: "a NUL byte in the middle", body: "<multistatus>\x00</multistatus>"},
		{desc: "an invalid encoding declaration", body: `<?xml version="1.0" encoding="utf-77"?><multistatus/>`},
		{desc: "an undefined namespace prefix", body: `<Q:multistatus><Q:response/></Q:multistatus>`},
		{
			desc: "a content length that is not a number",
			body: `<multistatus><response><href>/a</href><propstat><status>HTTP/1.1 200 OK</status>` +
				`<prop><getcontentlength>enormous</getcontentlength></prop></propstat></response></multistatus>`,
		},
		{
			desc: "a content length past int64",
			body: `<multistatus><response><href>/a</href><propstat><status>HTTP/1.1 200 OK</status>` +
				`<prop><getcontentlength>99999999999999999999999</getcontentlength></prop></propstat></response></multistatus>`,
		},
		{
			// The classic entity-expansion bomb. It must not be expanded, and
			// it must not be attempted: the failure has to be immediate.
			desc: "an entity expansion bomb",
			body: `<?xml version="1.0"?><!DOCTYPE lolz [` +
				`<!ENTITY lol "lol">` +
				`<!ENTITY lol1 "&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;">` +
				`<!ENTITY lol2 "&lol1;&lol1;&lol1;&lol1;&lol1;&lol1;&lol1;&lol1;&lol1;&lol1;">` +
				`<!ENTITY lol3 "&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;">` +
				`<!ENTITY lol4 "&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;">` +
				`<!ENTITY lol5 "&lol4;&lol4;&lol4;&lol4;&lol4;&lol4;&lol4;&lol4;&lol4;&lol4;">` +
				`]><multistatus><response><href>&lol5;</href></response></multistatus>`,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusMultiStatus)
				io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			c, err := Dial(srv.URL)
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}

			// A deadline turns a hang into a failure rather than a hung suite.
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			ents, err := c.Dirlist(ctx, "/store")
			if err == nil {
				// Not every malformed document has to be rejected — a parser
				// may ignore an element it does not understand — but it must
				// never invent entries out of one.
				for _, e := range ents {
					if e.Name == "" || strings.Contains(e.Name, "lol") {
						t.Errorf("the listing produced %+v out of a hostile document", e)
					}
					if e.Size < 0 {
						t.Errorf("the listing reports a negative size: %+v", e)
					}
				}
				return
			}
			if !strings.Contains(err.Error(), "xrdhttp") {
				t.Errorf("the error does not name the package: %v", err)
			}
		})
	}
}

// TestConformance_AnEndlessMultistatusIsBounded is the allocation half. A
// server that never stops writing must not be read until this process dies:
// the client gives up at its own limit, with an error that says so.
func TestConformance_AnEndlessMultistatusIsBounded(t *testing.T) {
	// The shipped bound has to stay well clear of a real listing: a directory
	// with tens of thousands of members is an ordinary thing in this ecosystem,
	// and a bound tuned down to what a test can produce quickly would start
	// rejecting them.
	if maxPropfindBody < 16<<20 {
		t.Errorf("the shipped bound is %d bytes, too low for a large directory", maxPropfindBody)
	}
	defer func(n int64) { maxPropfindBody = n }(maxPropfindBody)
	maxPropfindBody = 1 << 20

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusMultiStatus)
		io.WriteString(w, `<?xml version="1.0"?><D:multistatus xmlns:D="DAV:">`)

		// One entry, repeated for as long as the client keeps reading. The
		// document stays well-formed, so only the bound can stop it.
		chunk := davResponseXML("/store/f.root", "f.root", false, 1)
		for {
			if _, err := io.WriteString(w, chunk); err != nil {
				return // the client hung up, which is the point.
			}
		}
	}))
	defer srv.Close()

	c, err := Dial(srv.URL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := c.Dirlist(ctx, "/store")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("an endless listing was accepted")
		}
		if !strings.Contains(err.Error(), "exceeds") {
			t.Errorf("the error is %v, want one naming the size limit", err)
		}
	case <-ctx.Done():
		t.Fatal("the client is still reading an endless response")
	}
}

// TestConformance_AnExternalEntityIsNotFetched checks the property that makes
// XML parsing dangerous in the first place. A document that names a local file
// as an entity must not cause the client to read it, and must certainly not
// hand its contents back as a directory listing.
func TestConformance_AnExternalEntityIsNotFetched(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "secret.txt")
	const contents = "this is not a directory listing"
	if err := os.WriteFile(secret, []byte(contents), 0o600); err != nil {
		t.Fatalf("could not write the fixture: %v", err)
	}

	var fetched bool
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetched = true
		io.WriteString(w, contents)
	}))
	defer remote.Close()

	for _, tc := range []struct {
		desc   string
		entity string
	}{
		{desc: "a local file", entity: "file://" + secret},
		{desc: "an http url", entity: remote.URL + "/xxe"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusMultiStatus)
				fmt.Fprintf(w, `<?xml version="1.0"?><!DOCTYPE m [<!ENTITY xxe SYSTEM %q>]>`+
					`<multistatus><response><href>&xxe;</href><propstat>`+
					`<status>HTTP/1.1 200 OK</status><prop><displayname>&xxe;</displayname></prop>`+
					`</propstat></response></multistatus>`, tc.entity)
			}))
			defer srv.Close()

			c, err := Dial(srv.URL)
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}
			ents, err := c.Dirlist(context.Background(), "/store")
			if err == nil {
				for _, e := range ents {
					if strings.Contains(e.Name, contents) {
						t.Errorf("the entity was expanded into the listing: %+v", e)
					}
				}
			}
			if fetched {
				t.Error("the client fetched a URL named by the response document")
			}
		})
	}
}

// TestConformance_AStatusThatIsNotMultistatusIsNotParsed pins the check that
// comes before the parser. A 200 with a body full of HTML is what a proxy or a
// login page answers with, and treating it as a listing turns an
// authentication failure into an empty directory.
func TestConformance_AStatusThatIsNotMultistatusIsNotParsed(t *testing.T) {
	for _, status := range []int{
		http.StatusOK,
		http.StatusNoContent,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusMethodNotAllowed,
		http.StatusInternalServerError,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				// A body that would parse cleanly, to be sure the status is
				// what is being checked and not the document.
				io.WriteString(w, `<?xml version="1.0"?><D:multistatus xmlns:D="DAV:"/>`)
			}))
			defer srv.Close()

			c, err := Dial(srv.URL)
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}
			ents, err := c.Dirlist(context.Background(), "/store")
			if err == nil {
				t.Fatalf("a %d answer was accepted as a listing of %d entries", status, len(ents))
			}
			if !strings.Contains(err.Error(), http.StatusText(status)) {
				t.Errorf("the error is %v, want it to name the status", err)
			}
		})
	}
}
