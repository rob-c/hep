// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"io"
	"net"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

func readFull(r io.Reader, p []byte) (int, error) { return io.ReadFull(r, p) }

// readBootstrapRequest reads one request (header + body) synchronously.
// The returned body starts at the 16-byte parameter area, so request
// structs whose UnmarshalXrd covers params+dlen decode from it directly.
func readBootstrapRequest(conn net.Conn) (xrdproto.RequestHeader, []byte) {
	data, err := xrdproto.ReadRequest(conn)
	if err != nil {
		return xrdproto.RequestHeader{}, nil
	}
	var hdr xrdproto.RequestHeader
	_ = hdr.UnmarshalXrd(xrdenc.NewRBuffer(data[:xrdproto.RequestHeaderLength]))
	return hdr, data[xrdproto.RequestHeaderLength:]
}

// writeBootstrapResponse marshals resp and writes it as an Ok response on streamID.
func writeBootstrapResponse(conn net.Conn, streamID xrdproto.StreamID, resp xrdproto.Marshaler) {
	_ = xrdproto.WriteResponse(conn, streamID, xrdproto.Ok, resp)
}
