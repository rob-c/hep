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

// Dirlist lists the immediate members of a WebDAV collection at name using a
// PROPFIND request with Depth: 1. The collection itself (the request target)
// is omitted from the result.
func (c *Client) Dirlist(ctx context.Context, name string) ([]DirEntry, error) {
	const body = `<?xml version="1.0" encoding="utf-8"?>` +
		`<D:propfind xmlns:D="DAV:"><D:prop>` +
		`<D:displayname/><D:getcontentlength/><D:getlastmodified/><D:resourcetype/>` +
		`</D:prop></D:propfind>`

	target := c.urlFor(name)
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", target, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", "application/xml")

	resp, err := c.http.Do(req)
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

	self := strings.TrimRight(path.Clean("/"+name), "/")
	var out []DirEntry
	for _, r := range ms.Responses {
		href := r.Href
		// Compare by cleaned path, ignoring a trailing slash, to skip the
		// collection's own entry.
		hp := href
		if i := strings.Index(hp, "://"); i >= 0 {
			if j := strings.Index(hp[i+3:], "/"); j >= 0 {
				hp = hp[i+3+j:]
			}
		}
		if strings.TrimRight(path.Clean(hp), "/") == self {
			continue
		}
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
		name := prop.DisplayName
		if name == "" {
			name = strings.TrimRight(path.Base(hp), "/")
		}
		e := DirEntry{
			Name:  name,
			Size:  prop.ContentLength,
			IsDir: prop.ResourceType.Collection != nil,
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
