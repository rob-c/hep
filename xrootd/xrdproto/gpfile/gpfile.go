// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package gpfile names the kXR_gpfile request and says why this client does not
// send it.
//
// kXR_gpfile, "Grouped Parallel Fetch", was retired in XRootD v5 and no server
// or client this package was checked against implements it: the reference
// server marks the opcode "legacy, unused", and the two capability bits that
// would advertise it, kXR_supgpf and kXR_anongpf, are never set. What it was
// for — asking for several extents of a file in one round trip — is what
// kXR_readv does, and readv is what every current client uses.
//
// That leaves its request layout undocumented in every source available here.
// This package therefore stops at the identifiers that are known to be right —
// the opcode, and the capability flags read back through
// [go-hep.org/x/hep/xrootd/xrdproto/protocol.Response.SupportsGPFile] — and
// refuses the request itself with [ErrUnsupported] rather than putting a
// guessed body on the wire. A guess that marshals is worse than a refusal: it
// fails at the far end of a network, in a server's error log, long after the
// caller could have chosen [go-hep.org/x/hep/xrootd/xrdproto/readv] instead.
//
// If a use for it appears, what is needed first is a server that answers it, not
// a struct in this package.
package gpfile // import "go-hep.org/x/hep/xrootd/xrdproto/gpfile"

import (
	"errors"
	"fmt"
)

// RequestID is the id of the request, it is sent as part of message.
// See xrootd protocol specification for details: http://xrootd.org/doc/dev45/XRdv310.pdf, 2.3 Client Request Format.
const RequestID uint16 = 3005

// ErrUnsupported is returned by [Request] instead of a marshalled kXR_gpfile.
// It wraps [errors.ErrUnsupported], so a caller that already handles "this
// server cannot do that" handles this too.
var ErrUnsupported = fmt.Errorf("xrootd: kXR_gpfile was retired in XRootD v5 and is not implemented; use readv instead: %w", errors.ErrUnsupported)

// Request reports that this client does not send kXR_gpfile. It always returns
// [ErrUnsupported], and exists so the refusal has a single documented place to
// come from rather than being scattered over the callers that might have
// wanted it.
func Request() error { return ErrUnsupported }
