// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"net"
	"strconv"
	"strings"
)

// addressAndTLS splits a client address into a dial target and a TLS flag.
// The input may be a bare host[:port] or a full URL; a roots:// or xroots://
// scheme sets tls=true. The returned addr never carries a scheme.
func addressAndTLS(address string) (addr string, tls bool, err error) {
	if !strings.Contains(address, "://") {
		return address, false, nil
	}
	u, err := ParseURL(address)
	if err != nil {
		return "", false, err
	}
	return u.Addr, u.TLS(), nil
}

func parseAddr(addr string) string {
	_, _, err := net.SplitHostPort(addr)
	if err == nil {
		return addr
	}
	switch err := err.(type) {
	case *net.AddrError:
		switch err.Err {
		case "missing port in address":
			port, e := net.LookupPort("tcp", "rootd")
			if e != nil {
				return addr + ":1094"
			}
			return addr + ":" + strconv.Itoa(port)
		}
	}
	return addr
}
