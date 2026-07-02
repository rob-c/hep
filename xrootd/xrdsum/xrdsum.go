// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package xrdsum provides the checksum algorithms used by the XRootD
// protocol and its tooling: adler32, crc32c (RFC 7143), crc64 (CRC-64/XZ)
// and md5. The names accepted by Sum match the algorithm names XRootD
// servers report for kXR_Qcksum queries.
package xrdsum // import "go-hep.org/x/hep/xrootd/xrdsum"

import (
	"crypto/md5"
	"fmt"
	"hash/adler32"
	"hash/crc32"
	"hash/crc64"
)

var (
	crc32cTable = crc32.MakeTable(crc32.Castagnoli)
	crc64Table  = crc64.MakeTable(crc64.ECMA)
)

// CRC32C returns the CRC-32C (Castagnoli) checksum of p, as used for
// kXR_pgread/kXR_pgwrite page and status-frame integrity (RFC 7143).
func CRC32C(p []byte) uint32 { return crc32.Checksum(p, crc32cTable) }

// Adler32 returns the adler32 checksum of p.
func Adler32(p []byte) uint32 { return adler32.Checksum(p) }

// CRC64 returns the CRC-64/XZ checksum of p (ECMA polynomial, reflected),
// the digest the reference C client reports as "crc64".
func CRC64(p []byte) uint64 { return crc64.Checksum(p, crc64Table) }

// Sum returns the lower-case hexadecimal digest of p under the named
// algorithm: "adler32", "crc32c", "crc64" or "md5".
func Sum(algo string, p []byte) (string, error) {
	switch algo {
	case "adler32":
		return fmt.Sprintf("%08x", Adler32(p)), nil
	case "crc32c":
		return fmt.Sprintf("%08x", CRC32C(p)), nil
	case "crc64":
		return fmt.Sprintf("%016x", CRC64(p)), nil
	case "md5":
		return fmt.Sprintf("%x", md5.Sum(p)), nil
	default:
		return "", fmt.Errorf("xrootd: unsupported checksum algorithm %q", algo)
	}
}

// Supported lists the algorithm names accepted by Sum, sorted.
func Supported() []string { return []string{"adler32", "crc32c", "crc64", "md5"} }
