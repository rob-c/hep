// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package signing contains implementation of a way to check if request should be signed
// according to XRootD protocol specification v. 3.1.0, p.75-76.
package signing // import "go-hep.org/x/hep/xrootd/xrdproto/signing"

import (
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/auth"
	"go-hep.org/x/hep/xrootd/xrdproto/chkpoint"
	"go-hep.org/x/hep/xrootd/xrdproto/chmod"
	"go-hep.org/x/hep/xrootd/xrdproto/dirlist"
	"go-hep.org/x/hep/xrootd/xrdproto/fattr"
	"go-hep.org/x/hep/xrootd/xrdproto/mkdir"
	"go-hep.org/x/hep/xrootd/xrdproto/mv"
	"go-hep.org/x/hep/xrootd/xrdproto/open"
	"go-hep.org/x/hep/xrootd/xrdproto/pgread"
	"go-hep.org/x/hep/xrootd/xrdproto/pgwrite"
	"go-hep.org/x/hep/xrootd/xrdproto/prepare"
	"go-hep.org/x/hep/xrootd/xrdproto/read"
	"go-hep.org/x/hep/xrootd/xrdproto/rm"
	"go-hep.org/x/hep/xrootd/xrdproto/rmdir"
	"go-hep.org/x/hep/xrootd/xrdproto/set"
	"go-hep.org/x/hep/xrootd/xrdproto/stat"
	"go-hep.org/x/hep/xrootd/xrdproto/statx"
	"go-hep.org/x/hep/xrootd/xrdproto/sync"
	"go-hep.org/x/hep/xrootd/xrdproto/truncate"
	"go-hep.org/x/hep/xrootd/xrdproto/verifyw"
	"go-hep.org/x/hep/xrootd/xrdproto/write"
	"go-hep.org/x/hep/xrootd/xrdproto/xrdclose"
)

// Requirements implements a way to check if request should be signed
// according to XRootD protocol specification v. 3.1.0, p.75-76.
type Requirements struct {
	requirements map[uint16]xrdproto.RequestLevel
}

// Needed returns whether the request should be signed.
// For the list of actual examples see XRootD protocol specification v. 3.1.0, p.76.
func (sr *Requirements) Needed(request xrdproto.Request) bool {
	v, exist := sr.requirements[request.ReqID()]
	if !exist || v == xrdproto.SignNone {
		return false
	}
	if v == xrdproto.SignLikely && !request.ShouldSign() {
		return false
	}
	return true
}

// Default creates a default Requirements with "None" security level.
func Default() Requirements {
	return New(xrdproto.NoneLevel, nil)
}

// New creates a Requirements according to provided security level and security overrides.
func New(level xrdproto.SecurityLevel, overrides []xrdproto.SecurityOverride) Requirements {
	var sr = Requirements{make(map[uint16]xrdproto.RequestLevel)}

	if level >= xrdproto.Compatible {
		// TODO: set requirements
		sr.requirements[chmod.RequestID] = xrdproto.SignNeeded
		sr.requirements[mv.RequestID] = xrdproto.SignNeeded
		sr.requirements[open.RequestID] = xrdproto.SignLikely
		sr.requirements[rm.RequestID] = xrdproto.SignNeeded
		sr.requirements[rmdir.RequestID] = xrdproto.SignNeeded
		sr.requirements[truncate.RequestID] = xrdproto.SignNeeded
	}
	if level >= xrdproto.Standard {
		// TODO: set requirements
		// The three below change server state without touching a byte of file
		// content, which is why they are easy to overlook and why they belong
		// here: an attribute is as good a place to hide something as the data
		// is, a prepare can be made to pull a tape system to a halt, and a set
		// rewrites what the server records about this connection.
		sr.requirements[fattr.RequestID] = xrdproto.SignNeeded
		sr.requirements[mkdir.RequestID] = xrdproto.SignNeeded
		sr.requirements[open.RequestID] = xrdproto.SignNeeded
		sr.requirements[prepare.RequestID] = xrdproto.SignNeeded
		sr.requirements[set.RequestID] = xrdproto.SignNeeded
	}
	if level >= xrdproto.Intense {
		// TODO: set requirements
		// A checkpoint carries a write or a truncate, and is signed where those
		// are: an unsigned checkpoint would be a way to make exactly the
		// modifications this level exists to authenticate.
		sr.requirements[chkpoint.RequestID] = xrdproto.SignNeeded
		sr.requirements[pgwrite.RequestID] = xrdproto.SignNeeded
		sr.requirements[xrdclose.RequestID] = xrdproto.SignNeeded
		sr.requirements[verifyw.RequestID] = xrdproto.SignNeeded
		sr.requirements[write.RequestID] = xrdproto.SignNeeded
	}
	if level >= xrdproto.Pedantic {
		// TODO: set requirements
		sr.requirements[dirlist.RequestID] = xrdproto.SignNeeded
		sr.requirements[pgread.RequestID] = xrdproto.SignNeeded
		sr.requirements[read.RequestID] = xrdproto.SignNeeded
		sr.requirements[stat.RequestID] = xrdproto.SignNeeded
		sr.requirements[statx.RequestID] = xrdproto.SignNeeded
		sr.requirements[sync.RequestID] = xrdproto.SignNeeded
	}

	for _, override := range overrides {
		requestID := auth.RequestID + uint16(override.RequestIndex)
		sr.requirements[requestID] = override.RequestLevel
	}

	return sr
}
