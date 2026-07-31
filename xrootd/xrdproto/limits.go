// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdproto // import "go-hep.org/x/hep/xrootd/xrdproto"

import "fmt"

// MaxResponseLength is the largest body, in bytes, that a client accepts in a
// single response frame.
//
// A response header is untrusted input: its data length is chosen by the peer
// and is read before a single byte of the body is validated. Allocating it
// unconditionally lets a hostile or malfunctioning server ask the client to
// reserve up to 2 GiB per frame. No legitimate reply is this large — a server
// with more data than fits in one frame splits it across several OkSoFar
// frames — so a body beyond this bound is refused rather than allocated.
const MaxResponseLength = 64 << 20 // 64 MiB

// MaxVectorSegments and MaxVectorBytes bound a vector request (kXR_readv,
// kXR_writev) before it reaches the wire.
//
// A server is free to refuse an over-long vector, but discovering that after
// the request has been assembled costs a round trip and, for a vector built
// from user input, an arbitrarily large buffer on this side first. These are
// the bounds the reference clients apply.
const (
	// MaxVectorSegments is the largest number of segments in one vector request.
	MaxVectorSegments = 1024
	// MaxVectorBytes is the largest aggregate payload of one vector request.
	MaxVectorBytes = 256 << 20 // 256 MiB
)

// ValidateVector reports whether a vector request of nsegs segments carrying
// total bytes in all is within MaxVectorSegments and MaxVectorBytes. what
// names the operation in the error message.
func ValidateVector(nsegs int, total int64, what string) error {
	switch {
	case nsegs < 1:
		return fmt.Errorf("xrootd: %s with no segments", what)
	case nsegs > MaxVectorSegments:
		return fmt.Errorf("xrootd: %s of %d segments exceeds the %d-segment limit", what, nsegs, MaxVectorSegments)
	case total < 0:
		return fmt.Errorf("xrootd: %s with a negative segment length", what)
	case total > MaxVectorBytes:
		return fmt.Errorf("xrootd: %s of %d bytes exceeds the %d-byte limit", what, total, MaxVectorBytes)
	}
	return nil
}

// ResponseLimiter is the interface implemented by requests that can state an
// upper bound on the response they may legitimately receive.
//
// MaxResponseLength bounds the total a session will accumulate across the
// OkSoFar (or partial kXR_status) frames of one request. Without such a
// bound a server that never sends a terminal frame grows the client's heap
// without limit, which no per-frame cap can prevent. A request that cannot
// predict its reply size does not implement this interface, and is bounded
// only per frame.
type ResponseLimiter interface {
	// MaxResponseLength returns the largest total response body, in bytes,
	// that a well-behaved server may send in answer to this request. It must
	// be positive; a non-positive value is treated as "no bound".
	MaxResponseLength() int64
}
