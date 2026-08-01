// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package hardlink contains the structures describing the hard link request.
//
// kXR_link is a vendor extension rather than part of the protocol
// specification: a server that has not been built with it answers
// kXR_Unsupported, which is the only way to find out whether it is there.
package hardlink // import "go-hep.org/x/hep/xrootd/xrdproto/hardlink"

import (
	"errors"
	"fmt"
	"strings"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
)

// RequestID is the id of the request, it is sent as part of message.
const RequestID uint16 = 3503

// Request holds the hard link request parameters.
//
// The two paths travel as one space-separated string, exactly as they do in a
// mv request, and the length of the first is sent separately so that a path
// containing a space is still read correctly.
type Request struct {
	_       [14]byte
	OldPath string // OldPath is the existing name.
	NewPath string // NewPath is the name to add for it.
}

// MarshalXrd implements xrdproto.Marshaler.
func (req Request) MarshalXrd(w *xrdenc.WBuffer) error {
	w.Next(14)
	w.WriteU16(uint16(len(req.OldPath)))
	w.WriteLen(len(req.OldPath) + len(req.NewPath) + 1)
	w.WriteBytes([]byte(req.OldPath))
	w.WriteBytes([]byte{' '})
	w.WriteBytes([]byte(req.NewPath))
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (req *Request) UnmarshalXrd(r *xrdenc.RBuffer) error {
	r.Skip(14)
	n := int(r.ReadU16())
	paths := r.ReadStr()
	if n >= len(paths) {
		return fmt.Errorf("xrootd: wrong link request. Want oldLen < %d, got %d", len(paths)-1, n)
	}
	if n == 0 {
		n = strings.Index(paths, " ")
		if n == -1 {
			return errors.New("xrootd: wrong link request. Want paths to be separated by ' ', none found")
		}
	}
	req.OldPath = paths[:n]
	req.NewPath = paths[n+1:]
	return r.Err()
}

// ReqID implements xrdproto.Request.ReqID.
func (req *Request) ReqID() uint16 { return RequestID }

// ShouldSign implements xrdproto.Request.ShouldSign.
func (req *Request) ShouldSign() bool { return false }
