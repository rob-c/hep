// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdproto // import "go-hep.org/x/hep/xrootd/xrdproto"

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
