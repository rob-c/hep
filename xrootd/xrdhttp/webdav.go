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
	"path"
	"strings"
	"time"
)

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
		return nil, fmt.Errorf("xrdhttp: PROPFIND %q: unexpected status %s", name, resp.Status)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var ms davMultistatus
	if err := xml.Unmarshal(raw, &ms); err != nil {
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

// hrefPath reduces a multistatus href to its path component. An href may be an
// absolute URL or an absolute path, and both forms appear in the wild.
func hrefPath(href string) string {
	if i := strings.Index(href, "://"); i >= 0 {
		if j := strings.Index(href[i+3:], "/"); j >= 0 {
			return href[i+3+j:]
		}
	}
	return href
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
