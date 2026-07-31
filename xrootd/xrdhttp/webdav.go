// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdhttp // import "go-hep.org/x/hep/xrootd/xrdhttp"

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// maxPropfindBody bounds the multistatus document a PROPFIND will parse. It is
// far above any real listing — a directory of a million entries is a few
// hundred megabytes of XML only if every entry carries a long name — and far
// below what an unbounded read costs when the server is wrong or hostile.
//
// It is a variable only so the tests can lower it; nothing changes it at run
// time.
var maxPropfindBody int64 = 64 << 20

// DirEntry is one member of a WebDAV collection.
type DirEntry struct {
	Name    string
	Size    int64
	ModTime time.Time
	IsDir   bool

	// rawHref is the path component of the multistatus href this entry came
	// from, kept so Dirlist can identify the request target.
	rawHref string
}

// davMultistatus mirrors the WebDAV PROPFIND multistatus response.
type davMultistatus struct {
	XMLName   xml.Name      `xml:"multistatus"`
	Responses []davResponse `xml:"response"`
}

type davResponse struct {
	Href     string        `xml:"href"`
	Propstat []davPropstat `xml:"propstat"`
}

type davPropstat struct {
	Status string  `xml:"status"`
	Prop   davProp `xml:"prop"`
}

type davProp struct {
	DisplayName   string `xml:"displayname"`
	ContentLength int64  `xml:"getcontentlength"`
	LastModified  string `xml:"getlastmodified"`
	ResourceType  struct {
		Collection *struct{} `xml:"collection"`
	} `xml:"resourcetype"`
}

// DirEntry carries the resource href it was parsed from, so Dirlist can drop
// the collection's own entry without re-deriving it.
func (e DirEntry) href() string { return e.rawHref }

// propfind issues a PROPFIND at the given Depth and returns every resource in
// the multistatus response, including the request target itself.
func (c *Client) propfind(ctx context.Context, name, depth string) ([]DirEntry, error) {
	const body = `<?xml version="1.0" encoding="utf-8"?>` +
		`<D:propfind xmlns:D="DAV:"><D:prop>` +
		`<D:displayname/><D:getcontentlength/><D:getlastmodified/><D:resourcetype/>` +
		`</D:prop></D:propfind>`

	req, err := http.NewRequestWithContext(ctx, "PROPFIND", c.urlFor(name), strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Depth", depth)
	req.Header.Set("Content-Type", "application/xml")

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("xrdhttp: PROPFIND %q: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMultiStatus {
		return nil, statusError("PROPFIND", name, resp)
	}

	// A multistatus document is whatever the server chooses to send, and it
	// arrives before anything in it has been validated. Reading it whole would
	// let an endpoint that answers a one-directory listing with an endless body
	// exhaust this process's memory, so the parse is bounded and streamed: the
	// document is never held twice, and a server that runs past the bound is
	// reported rather than followed.
	lr := &io.LimitedReader{R: resp.Body, N: maxPropfindBody + 1}
	var ms davMultistatus
	err = xml.NewDecoder(lr).Decode(&ms)
	if lr.N <= 0 {
		return nil, fmt.Errorf("xrdhttp: PROPFIND %q: response exceeds %d bytes", name, maxPropfindBody)
	}
	if err != nil {
		return nil, fmt.Errorf("xrdhttp: could not parse PROPFIND response: %w", err)
	}

	var out []DirEntry
	for _, r := range ms.Responses {
		var prop *davProp
		for i := range r.Propstat {
			if strings.Contains(r.Propstat[i].Status, "200") {
				prop = &r.Propstat[i].Prop
				break
			}
		}
		if prop == nil {
			continue
		}
		hp := hrefPath(r.Href)
		name := prop.DisplayName
		if name == "" {
			name = strings.TrimRight(path.Base(hp), "/")
		}
		e := DirEntry{
			Name:    name,
			Size:    prop.ContentLength,
			IsDir:   prop.ResourceType.Collection != nil,
			rawHref: hp,
		}
		if prop.LastModified != "" {
			if t, err := http.ParseTime(prop.LastModified); err == nil {
				e.ModTime = t
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// hrefPath reduces a multistatus href to its path component, decoded. An href
// is a URI reference (RFC 4918 §8.3), so it may be an absolute URL or an
// absolute path — both forms appear in the wild — and either way its path is
// percent-encoded. Everything downstream works in plain paths: the name handed
// back to the caller has to be openable, and Dirlist compares the href against
// the path it asked for to drop the collection's own entry. Leaving the
// encoding on makes a directory a member of itself, which is an infinite
// recursive walk.
//
// An href that does not parse is returned unchanged: it is not this function's
// job to decide what a server meant, and an unmatched entry is better than a
// mangled one.
func hrefPath(href string) string {
	u, err := url.Parse(href)
	if err != nil || u.Path == "" {
		return href
	}
	return u.Path
}

// Dirlist lists the immediate members of a WebDAV collection at name using a
// PROPFIND request with Depth: 1. The collection itself (the request target)
// is omitted from the result.
func (c *Client) Dirlist(ctx context.Context, name string) ([]DirEntry, error) {
	ents, err := c.propfind(ctx, name, "1")
	if err != nil {
		return nil, err
	}
	self := strings.TrimRight(path.Clean("/"+name), "/")
	out := ents[:0]
	for _, e := range ents {
		// Compare by cleaned path, ignoring a trailing slash, to skip the
		// collection's own entry.
		if strings.TrimRight(path.Clean(e.href()), "/") == self {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}
