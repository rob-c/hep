// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package dirlist contains the structures describing request and response
// for dirlist request used to obtain the contents of a directory.
package dirlist // import "go-hep.org/x/hep/xrootd/xrdproto/dirlist"

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdsum"
)

// RequestID is the id of the request, it is sent as part of message.
// See xrootd protocol specification for details: http://xrootd.org/doc/dev45/XRdv310.pdf, 2.3 Client Request Format.
const RequestID uint16 = 3004

// Response is a response for the dirlist request,
// which contains a slice of entries containing the entry name and the entry stat info,
// and a WithStatInfo flag indicating whether a request asked for a stat info.
type Response struct {
	Entries      []xrdfs.EntryStat
	WithStatInfo bool

	// WithChecksum reports whether the entries carry a checksum, which is what
	// a server answering kXR_dcksm sends. It is set from what actually arrived
	// rather than from what was asked for: a server that does not know the
	// option answers an ordinary listing, and this is where that shows.
	WithChecksum bool
}

// RespID implements xrdproto.Response.RespID.
func (resp *Response) RespID() uint16 { return RequestID }

// MarshalXrd implements xrdproto.Marshaler.
func (o Response) MarshalXrd(wBuffer *xrdenc.WBuffer) error {
	entries := o.Entries
	if o.WithStatInfo {
		firstEntry := []xrdfs.EntryStat{
			{EntryName: ".", HasStatInfo: true},
		}
		entries = append(firstEntry, entries...)
	}

	consistent := true
	for i := range entries {
		if entries[i].HasStatInfo != o.WithStatInfo {
			consistent = false
		}
	}

	if !consistent {
		// A half-stat listing cannot be put on the wire at all: the reader
		// decides how to read the whole reply from its leading entry, so a
		// response where some entries carry stat information and some do not
		// would be read back with every name after the first gap taken for a
		// stat line. Refusing here is the only place the mistake is still
		// visible.
		return errors.New("xrootd: all entries of dirlist.Response should either have stat info or not")
	}

	for i := range entries {
		nameSeparator := "\n"
		statInfoSeparator := "\n"
		if i == len(entries)-1 {
			if o.WithStatInfo {
				statInfoSeparator = "\x00"
			} else {
				nameSeparator = "\x00"
			}
		}

		wBuffer.WriteBytes([]byte(entries[i].EntryName + nameSeparator))
		if !o.WithStatInfo {
			continue
		}

		if err := entries[i].MarshalXrd(wBuffer); err != nil {
			return err
		}

		wBuffer.WriteBytes([]byte(statInfoSeparator))
	}

	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler
// When stat information is supported by the server, the format is
//
//	".\n"
//	"0 0 0 0\n"
//	"dirname\n"
//	"id size flags modtime\n"
//	...
//	0
//
// Otherwise, the format is the following:
//
//	"dirname\n"
//	...
//	0
//
// See xrootd protocol specification, page 45 for further details.
func (o *Response) UnmarshalXrd(rBuffer *xrdenc.RBuffer) error {
	if rBuffer.Len() == 0 {
		return rBuffer.Err()
	}

	data := bytes.TrimRight(rBuffer.Bytes(), "\x00")
	lines := bytes.Split(data, []byte{'\n'})

	// The dot entry is how a server says it honoured kXR_dstat, and it is
	// the whole payload when the directory is empty -- xrootd sends no
	// trailing newline in that case, so the prefix is tested without one.
	// See https://github.com/xrootd/xrootd/issues/739.
	if !bytes.HasPrefix(data, []byte(".\n0 0 0 0")) {
		// That means that the server doesn't support returning stat information.
		o.Entries = make([]xrdfs.EntryStat, len(lines))
		o.WithStatInfo = false
		for i, v := range lines {
			o.Entries[i] = xrdfs.EntryStat{EntryName: string(v)}
		}
		return rBuffer.Err()
	}

	if len(lines)%2 != 0 {
		return fmt.Errorf("xrootd: wrong response size for the dirlist request: want even number of lines, got %d", len(lines))
	}

	lines = lines[2:]
	o.Entries = make([]xrdfs.EntryStat, len(lines)/2)
	o.WithStatInfo = true
	o.WithChecksum = false

	for i := 0; i < len(lines); i += 2 {
		var rbuf = xrdenc.NewRBuffer(lines[i+1])
		err := o.Entries[i/2].UnmarshalXrd(rbuf)
		if err != nil {
			return err
		}
		o.Entries[i/2].EntryName = string(lines[i])
		if o.Entries[i/2].Checksum != "" {
			o.WithChecksum = true
		}
	}

	return rBuffer.Err()
}

// Request holds the dirlist request parameters.
type Request struct {
	_       [15]byte
	Options RequestOptions
	Path    string
}

// RequestOptions specifies what should be returned as part of response.
type RequestOptions byte

const (
	None         RequestOptions = 0 // None specifies that no addition information except entry names is required.
	Online       RequestOptions = 1 // Online specifies that only entries whose data is on disk should be returned. Wire value kXR_online.
	WithStatInfo RequestOptions = 2 // WithStatInfo specifies that stat information for every entry is required. Wire value kXR_dstat.

	// WithChecksum specifies that a checksum for every entry is required, next
	// to its stat information. Wire value kXR_dcksm.
	//
	// A checksum listing is a stat listing too: the digest is appended to the
	// stat line, so there is nowhere to put it in a reply that carries no stat
	// information. Servers act as though WithStatInfo had been asked for as
	// well, and [NewChecksumRequest] sends both.
	WithChecksum RequestOptions = 4
)

// NewRequest forms a Request according to provided path.
func NewRequest(path string) *Request {
	return &Request{Options: WithStatInfo, Path: path}
}

// DefaultChecksumAlgo is the digest a server computes for a checksum listing
// that does not name one.
const DefaultChecksumAlgo = "adler32"

// NewChecksumRequest forms a Request that asks for a checksum next to the stat
// information of every entry.
//
// algo names the digest — "adler32", "crc32", "crc32c", "md5", "sha1" and
// "sha256" are what servers are known to offer — and is passed as the cks.type
// CGI parameter of the path, which is the only place the protocol leaves for
// it. An empty algo asks for nothing in particular and gets
// [DefaultChecksumAlgo]; a server that cannot compute what was asked for
// refuses the whole listing rather than answering with a different digest.
//
// Every entry costs the server a read of the whole file it names, so this is a
// request to make of a directory, not of a tree.
func NewChecksumRequest(path, algo string) *Request {
	if algo != "" {
		path = xrdproto.WithOpaque(path, checksumKey+algo)
	}
	return &Request{Options: WithStatInfo | WithChecksum, Path: path}
}

// checksumKey is the CGI parameter that names the digest of a checksum listing.
const checksumKey = "cks.type="

// ChecksumAlgo returns the digest named by the cks.type parameter of path, or
// [DefaultChecksumAlgo] if it names none. It is what a server calls to find out
// what a checksum listing was asked for.
//
// An algorithm this build cannot compute is an error, with the message the
// reference server sends for it: answering with a different digest would be
// worse than refusing, since the caller has no way to tell that the digest it
// got is not the digest it asked about.
func ChecksumAlgo(path string) (string, error) {
	algo := DefaultChecksumAlgo
	for _, field := range strings.Split(xrdproto.Opaque(path), "&") {
		if name, ok := strings.CutPrefix(field, checksumKey); ok && name != "" {
			algo = strings.ToLower(name)
		}
	}
	if !slices.Contains(xrdsum.Supported(), algo) {
		return "", fmt.Errorf("%s checksum not supported.", algo)
	}
	return algo, nil
}

// ReqID implements xrdproto.Request.ReqID.
func (req *Request) ReqID() uint16 { return RequestID }

// ShouldSign implements xrdproto.Request.ShouldSign.
func (req *Request) ShouldSign() bool { return false }

// MarshalXrd implements xrdproto.Marshaler.
func (o Request) MarshalXrd(wBuffer *xrdenc.WBuffer) error {
	wBuffer.Next(15)
	wBuffer.WriteU8(byte(o.Options))
	wBuffer.WriteStr(o.Path)
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (o *Request) UnmarshalXrd(rBuffer *xrdenc.RBuffer) error {
	rBuffer.Skip(15)
	o.Options = RequestOptions(rBuffer.ReadU8())
	o.Path = rBuffer.ReadStr()
	return rBuffer.Err()
}

// Opaque implements xrdproto.FilepathRequest.Opaque.
func (req *Request) Opaque() string {
	return xrdproto.Opaque(req.Path)
}

// SetOpaque implements xrdproto.FilepathRequest.SetOpaque.
func (req *Request) SetOpaque(opaque string) {
	xrdproto.SetOpaque(&req.Path, opaque)
}
