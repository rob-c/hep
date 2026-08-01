// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the request target: which URL each operation is addressed
// to. HEP namespaces are full of names an HTTP client has to be careful with —
// spaces, plus signs, percent signs, unicode, and the '?' and '#' that would
// otherwise turn the rest of the name into a query or a fragment the server
// never sees. Every one of those is a file that either opens or silently
// resolves to a different path.

package xrdhttp

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// targetServer records the decoded path and raw query of the last request, and
// the Destination header when there was one. Go's server decodes r.URL.Path
// for us, so an equality check against the name asked for is exactly the
// question "did the right file get addressed".
type targetServer struct {
	path string
	raw  string
	dest string
	verb string
}

func newTargetServer(t *testing.T, opts ...Option) (*targetServer, *Client) {
	t.Helper()
	rec := &targetServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.verb = r.Method
		rec.path = r.URL.Path
		rec.raw = r.URL.RawQuery
		rec.dest = r.Header.Get("Destination")
		switch r.Method {
		case "PROPFIND":
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusMultiStatus)
			io.WriteString(w, `<?xml version="1.0"?><D:multistatus xmlns:D="DAV:"></D:multistatus>`)
		case http.MethodHead, http.MethodGet:
			w.Header().Set("Content-Length", "0")
		default:
			w.WriteHeader(http.StatusCreated)
		}
	}))
	t.Cleanup(srv.Close)

	c, err := Dial(srv.URL, append([]Option{Unbounded()}, opts...)...)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	return rec, c
}

// awkwardNames are paths that a client which pastes strings together rather
// than building a URL gets wrong.
var awkwardNames = []struct {
	desc string
	name string
}{
	{desc: "a plain path", name: "/store/data/run1.root"},
	{desc: "a space", name: "/store/my data/run 1.root"},
	{desc: "a plus, which means a space in a query but not in a path", name: "/store/a+b/c+d.root"},
	{desc: "a percent sign, which must not be read as an escape", name: "/store/100%/done.root"},
	{desc: "a question mark, which would start a query", name: "/store/what?.root"},
	{desc: "a hash, which would start a fragment", name: "/store/tag#1/f.root"},
	{desc: "an ampersand and an equals sign", name: "/store/a&b=c/f.root"},
	{desc: "unicode", name: "/store/données/mesuré.root"},
	{desc: "a colon, which must not look like a scheme", name: "/store/run:1/f.root"},
}

// TestConformance_EveryOperationAddressesTheNameItWasGiven drives one name
// through every verb the client speaks and asserts the path the server decoded
// is the name the caller asked for — nothing truncated at a '?' or a '#', and
// nothing double-escaped.
func TestConformance_EveryOperationAddressesTheNameItWasGiven(t *testing.T) {
	ctx := context.Background()

	for _, tc := range awkwardNames {
		t.Run(tc.desc, func(t *testing.T) {
			rec, c := newTargetServer(t)

			ops := []struct {
				verb string
				call func() error
			}{
				{"HEAD", func() error { _, err := c.Stat(ctx, tc.name); return err }},
				{"GET", func() error { _, err := c.ReadAll(ctx, tc.name); return err }},
				{"GET", func() error { _, err := c.ReadAt(ctx, make([]byte, 1), tc.name, 0); return err }},
				{"PUT", func() error { return c.Create(ctx, tc.name, strings.NewReader("x"), 1) }},
				{"DELETE", func() error { return c.Remove(ctx, tc.name) }},
				{"PROPFIND", func() error { _, err := c.Dirlist(ctx, tc.name); return err }},
				{"MKCOL", func() error { return c.mkcol(ctx, tc.name) }},
			}
			for _, op := range ops {
				// A ranged GET of an empty object is io.EOF, and a HEAD of one
				// is a zero-size file: neither is a failure of addressing.
				if err := op.call(); err != nil && err != io.EOF {
					t.Fatalf("%s: %v", op.verb, err)
				}
				if rec.verb != op.verb {
					t.Errorf("the server saw a %s, want a %s", rec.verb, op.verb)
				}
				if rec.path != tc.name {
					t.Errorf("%s addressed %q, want %q", op.verb, rec.path, tc.name)
				}
				if rec.raw != "" {
					t.Errorf("%s left %q in the query string; part of the name was lost", op.verb, rec.raw)
				}
			}

			// MOVE names two paths, one in the target and one in a header, and
			// the header is a full URL the server has to parse back.
			dst := tc.name + ".moved"
			if err := c.move(ctx, tc.name, dst); err != nil {
				t.Fatalf("MOVE: %v", err)
			}
			if rec.path != tc.name {
				t.Errorf("MOVE addressed %q, want %q", rec.path, tc.name)
			}
			u, err := url.Parse(rec.dest)
			if err != nil {
				t.Fatalf("the Destination header does not parse as a URL: %q: %v", rec.dest, err)
			}
			if u.Path != dst {
				t.Errorf("MOVE destination is %q, want %q", u.Path, dst)
			}
			if u.Host == "" || u.Scheme == "" {
				t.Errorf("the Destination header is not an absolute URL: %q", rec.dest)
			}
		})
	}
}

// TestConformance_ANameIsResolvedAgainstTheBaseURL pins the rule the package
// documents: an absolute name replaces the base path, a relative one is joined
// to it. Getting this wrong against an endpoint published with a path prefix
// (the usual shape, https://host:1094/store) means every path lands outside
// the exported namespace.
func TestConformance_ANameIsResolvedAgainstTheBaseURL(t *testing.T) {
	for _, tc := range []struct {
		base string
		name string
		want string
	}{
		{base: "https://h:1094", name: "/store/f.root", want: "/store/f.root"},
		{base: "https://h:1094/", name: "/store/f.root", want: "/store/f.root"},
		// An absolute name is absolute: it replaces the base path rather than
		// nesting under it, so a caller holding an XRootD path uses it as is.
		{base: "https://h:1094/store", name: "/data/f.root", want: "/data/f.root"},
		{base: "https://h:1094/store/", name: "/data/f.root", want: "/data/f.root"},
		// A relative name is joined, which is how a base acts as a prefix.
		{base: "https://h:1094/store/", name: "f.root", want: "/store/f.root"},
		{base: "https://h:1094/store/", name: "sub/f.root", want: "/store/sub/f.root"},
		// A base with no trailing slash names a resource, not a collection, so
		// a relative reference replaces its last segment (RFC 3986 §5.3).
		{base: "https://h:1094/store", name: "f.root", want: "/f.root"},
	} {
		t.Run(tc.base+" + "+tc.name, func(t *testing.T) {
			c, err := Dial(tc.base, Unbounded())
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}
			u, err := url.Parse(c.urlFor(tc.name))
			if err != nil {
				t.Fatalf("urlFor produced an unparseable URL: %v", err)
			}
			if u.Path != tc.want {
				t.Errorf("the request target is %q, want %q", u.Path, tc.want)
			}
			if u.Host != "h:1094" || u.Scheme != "https" {
				t.Errorf("the request target moved to %q://%q", u.Scheme, u.Host)
			}
		})
	}
}

// TestConformance_ANameIsEscapedOnTheWire looks at the bytes rather than the
// decoded path: the request line has to carry an escaped form, because a raw
// space would end the target and a raw '?' would begin a query. This is the
// difference the decoded-path assertions above cannot see.
func TestConformance_ANameIsEscapedOnTheWire(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{name: "/a b.root", want: "/a%20b.root"},
		{name: "/a?b.root", want: "/a%3Fb.root"},
		{name: "/a#b.root", want: "/a%23b.root"},
		{name: "/100%.root", want: "/100%25.root"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Dial("https://h:1094", Unbounded())
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}
			got := c.urlFor(tc.name)
			if want := "https://h:1094" + tc.want; got != want {
				t.Errorf("the request target is %q, want %q", got, want)
			}
		})
	}
}

// TestConformance_ADirlistDropsItsOwnEntryWhateverTheHrefLooksLike is the
// listing counterpart. A PROPFIND with Depth: 1 always includes the collection
// itself, and the hrefs come back as URI references — percent-encoded, and
// absolute or relative depending on the server. A listing that fails to match
// one of those forms against the request target reports the directory as a
// member of itself, which turns any recursive walk into an infinite one.
func TestConformance_ADirlistDropsItsOwnEntryWhateverTheHrefLooksLike(t *testing.T) {
	const dir = "/store/my data"

	for _, tc := range []struct {
		desc string
		self func(base string) string
	}{
		{desc: "an absolute path", self: func(string) string { return "/store/my%20data/" }},
		{desc: "an absolute path with no trailing slash", self: func(string) string { return "/store/my%20data" }},
		{desc: "a full URL", self: func(base string) string { return base + "/store/my%20data/" }},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			var base string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusMultiStatus)
				fmt.Fprintf(w, `<?xml version="1.0"?><D:multistatus xmlns:D="DAV:">%s%s</D:multistatus>`,
					davResponseXML(tc.self(base), "", true, 0),
					davResponseXML("/store/my%20data/run%201.root", "run 1.root", false, 42),
				)
			}))
			defer srv.Close()
			base = srv.URL

			c, err := Dial(srv.URL, Unbounded())
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}
			ents, err := c.Dirlist(context.Background(), dir)
			if err != nil {
				t.Fatalf("Dirlist: %v", err)
			}
			if len(ents) != 1 {
				t.Fatalf("the listing has %d entries, want 1: %+v", len(ents), ents)
			}
			if got, want := ents[0].Name, "run 1.root"; got != want {
				t.Errorf("the entry is named %q, want %q", got, want)
			}
			if ents[0].Size != 42 || ents[0].IsDir {
				t.Errorf("the entry is %+v, want a 42-byte file", ents[0])
			}
		})
	}
}

// TestConformance_AnEntryWithoutADisplayNameIsNamedFromItsHref covers the
// servers that omit displayname (nginx's dav module among them): the name then
// has to come from the href, which is percent-encoded. Handing that encoding
// back to the caller produces a name no subsequent request can open.
func TestConformance_AnEntryWithoutADisplayNameIsNamedFromItsHref(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusMultiStatus)
		fmt.Fprintf(w, `<?xml version="1.0"?><D:multistatus xmlns:D="DAV:">%s%s%s</D:multistatus>`,
			davResponseXML("/store/", "", true, 0),
			davResponseXML("/store/run%201.root", "", false, 7),
			davResponseXML("/store/donn%C3%A9es/", "", true, 0),
		)
	}))
	defer srv.Close()

	c, err := Dial(srv.URL, Unbounded())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	ents, err := c.Dirlist(context.Background(), "/store")
	if err != nil {
		t.Fatalf("Dirlist: %v", err)
	}
	got := make([]string, len(ents))
	for i, e := range ents {
		got[i] = e.Name
	}
	want := []string{"run 1.root", "données"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the listing names %q, want %q", got, want)
	}
}

// davResponseXML builds one <D:response> element with an already-escaped href.
func davResponseXML(href, display string, isDir bool, size int64) string {
	var b strings.Builder
	b.WriteString("<D:response><D:href>")
	xml.EscapeText(&b, []byte(href))
	b.WriteString("</D:href><D:propstat><D:status>HTTP/1.1 200 OK</D:status><D:prop>")
	if display != "" {
		b.WriteString("<D:displayname>")
		xml.EscapeText(&b, []byte(display))
		b.WriteString("</D:displayname>")
	}
	switch {
	case isDir:
		b.WriteString("<D:resourcetype><D:collection/></D:resourcetype>")
	default:
		fmt.Fprintf(&b, "<D:resourcetype/><D:getcontentlength>%d</D:getcontentlength>", size)
	}
	b.WriteString("</D:prop></D:propstat></D:response>")
	return b.String()
}
