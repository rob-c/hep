# XRootD Phase 1 — Native Protocol Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Bring go-hep's `root://` client to protocol parity with `libxrdc` for paged I/O (`kXR_pgread`/`kXR_pgwrite` with per-page CRC-32C), extended attributes (`kXR_fattr`), and the checksum surface (query `kXR_Qcksum` + local digests).

**Architecture:** Three protocol additions layered on Phase 0's synchronous-bootstrap client. Paged I/O introduces the `kXR_status` (4007) response framing into the `consume()` read loop — status frames carry their own CRC-protected 16-byte body plus a trailing data length that lives *outside* the response header's `dlen`, so the reader must drain it explicitly. Page encoding/decoding is a shared codec ([CRC-32C BE u32][data ≤ 4096] units, aligned to 4 KiB *file* offsets). fattr and checksum are conventional request/response packages plus `FileSystem`-level helpers.

**Tech Stack:** Go 1.24+, standard library only (`hash/crc32` Castagnoli, `hash/adler32`, `hash/crc64` ECMA, `crypto/md5`). Existing packages: `xrootd`, `xrdproto`, `xrdfs`, `internal/mux`, `internal/xrdenc`.

## Global Constraints

- Module `go-hep.org/x/hep`; Go floor `go 1.24.0`; no third-party dependencies; pure Go.
- New files start with the `Copyright ©2026 The go-hep Authors` BSD license header (same 3 lines as Phase 0 files).
- Every exported identifier gets a doc comment starting with its name; comments state protocol constraints, not narration.
- All I/O takes `context.Context`; errors wrapped with `%w` and the `xrootd:` prefix.
- Exact protocol constants (verified in `/usr/include/xrootd/XProtocol/XProtocol.hh`; `libxrdc` cross-checked):
  - Request IDs: `kXR_fattr=3020`, `kXR_pgwrite=3026`, `kXR_pgread=3030`.
  - `kXR_status=4007` response code; status body is 16 bytes: `crc32c u32 | streamID [2]byte | requestid u8 | resptype u8 | reserved [4]byte | dlen i32`. `resptype`: `kXR_FinalResult=0x00`, `kXR_PartialResult=0x01`, `kXR_ProgressInfo=0x02`. The CRC-32C covers everything after the crc field through the end of the response-header `dlen` (for pg ops: 20 bytes = body remainder 12 + offset 8).
  - pg ops: response-header `dlen` is exactly 24 (16-byte status body + 8-byte file offset); the page data that follows is `dlen` bytes from the *status body*, read separately off the socket (this is how `libxrdc` `read_status_frame` treats it — mirror it strictly and error otherwise).
  - Pages: `kXR_pgPageSZ=4096`; a page unit is `[crc32c(data) BE u32][data]`; units align to 4 KiB **file** offsets, so the first unit of a request starting at unaligned `off` is `4096 - off%4096` bytes (and the last unit is short if the tail is).
  - `ClientPgReadRequest` params (16 bytes): `fhandle[4] | offset i64 | rlen i32`, request `dlen=0`.
  - `ClientPgWriteRequest` params (16 bytes): `fhandle[4] | offset i64 | pathid u8 | reqflags u8 | reserved[2]`, request `dlen=len(page units)`. `kXR_pgRetry=0x01` reqflag exists; Phase 1 does **not** implement retry — a server-reported CRC error is surfaced as an error.
  - fattr subcodes: `kXR_fattrDel=0, kXR_fattrGet=1, kXR_fattrList=2, kXR_fattrSet=3`; limits `kXR_faMaxVars=16`; options `isNew=0x01` (set), `aData=0x10` (list). Params (16 bytes): `fhandle[4] | subcode u8 | numattr u8 | options u8 | reserved[9]`. Path-based body: `path\0` + nvec + (set only) vvec, where an nvec entry is `[u16 BE rc][name\0]` and a vvec entry is `[i32 BE vlen][value]`. Responses: Get/Set/Del = `[u8 errcount][u8 numattr]` + nvec-with-rc (+ vvec for Get); List = NUL-separated name list.
  - Query checksum: `kXR_Qcksum=3` (already `query.Checksum` in go-hep); reply payload is `"<algo> <hexvalue>"`.
  - Checksum algorithms (libxrdc canonical set): `adler32`, `crc32c`, `md5`, plus `crc64` = **CRC-64/XZ** (Go `hash/crc64` with the `ECMA` table).
- After each task: `gofmt -l xrootd/` empty, `go vet ./xrootd/...` clean, `go test ./xrootd/...` green. Go toolchain lives at `~/.local/share/go/bin` (add to PATH).
- Dual-oracle bar: mock tests per task; gated interop tests against a real XRootD server plus `libxrdc` binaries (`client/bin/xrdfs`, `xrdcrc64`) documented in the parity runbook.
- Out of scope (deliberate): `kXR_gpfile` (belongs to the Phase 2 copy engine), `kXR_pgRetry` write-retry, pg data over sub-sessions (`pathid` stays 0), multi-attribute fattr batches (single attribute + list, matching `libxrdc`'s surface).

---

### Task 1: `xrdsum` checksum package

**Files:**
- Create: `xrootd/xrdsum/xrdsum.go`
- Test: `xrootd/xrdsum/xrdsum_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces (package `xrdsum`):
  - `func CRC32C(p []byte) uint32` — CRC-32C (Castagnoli, RFC 7143).
  - `func Adler32(p []byte) uint32`
  - `func CRC64(p []byte) uint64` — CRC-64/XZ (ECMA table), matching `libxrdc`'s canonical `crc64`.
  - `func Sum(algo string, p []byte) (string, error)` — lower-case hex digest for `"adler32"`, `"crc32c"`, `"crc64"`, `"md5"`; unknown algo → error mentioning the algo.
  - `func Supported() []string` — the four names above, sorted.

- [x] **Step 1: Write the failing test**

`xrootd/xrdsum/xrdsum_test.go` — known-answer vectors (the classic `"123456789"` check values):

```go
package xrdsum

import "testing"

func TestKnownAnswers(t *testing.T) {
	data := []byte("123456789")
	if got, want := CRC32C(data), uint32(0xe3069283); got != want {
		t.Fatalf("CRC32C: got=%#x want=%#x", got, want)
	}
	if got, want := Adler32(data), uint32(0x091e01de); got != want {
		t.Fatalf("Adler32: got=%#x want=%#x", got, want)
	}
	if got, want := CRC64(data), uint64(0x995dc9bbdf1939fa); got != want {
		t.Fatalf("CRC64: got=%#x want=%#x", got, want)
	}
}

func TestSum(t *testing.T) {
	data := []byte("123456789")
	for _, tc := range []struct{ algo, want string }{
		{"adler32", "091e01de"},
		{"crc32c", "e3069283"},
		{"crc64", "995dc9bbdf1939fa"},
		{"md5", "25f9e794323b453885f5181f1b624d0b"},
	} {
		got, err := Sum(tc.algo, data)
		if err != nil {
			t.Fatalf("Sum(%q): %v", tc.algo, err)
		}
		if got != tc.want {
			t.Fatalf("Sum(%q): got=%q want=%q", tc.algo, got, tc.want)
		}
	}
	if _, err := Sum("sha1", data); err == nil {
		t.Fatal("Sum(sha1): expected error for unsupported algorithm")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./xrootd/xrdsum/ -v`
Expected: FAIL (package does not exist / build failure).

- [x] **Step 3: Implement**

`xrootd/xrdsum/xrdsum.go`:

```go
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
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./xrootd/xrdsum/ -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
gofmt -w xrootd/xrdsum/ && go vet ./xrootd/xrdsum/
git add xrootd/xrdsum/
git commit -m "xrootd/xrdsum: add adler32/crc32c/crc64/md5 checksum helpers"
```

---

### Task 2: `kXR_status` response body in `xrdproto`

**Files:**
- Create: `xrootd/xrdproto/status.go`
- Test: `xrootd/xrdproto/status_test.go`

**Interfaces:**
- Consumes: `xrdsum.CRC32C` (Task 1), `xrdenc`.
- Produces (package `xrdproto`):
  - `const Status ResponseStatus = 4007` (append to the existing `ResponseStatus` const block in `xrdproto.go`).
  - `const StatusBodyLength = 16`.
  - Resptype constants: `FinalResult uint8 = 0x00`, `PartialResult uint8 = 0x01`, `ProgressInfo uint8 = 0x02`.
  - `type StatusBody struct { CRC32C uint32; StreamID StreamID; RequestID uint8; RespType uint8; Reserved [4]byte; DataLength int32 }`.
  - `func (o *StatusBody) UnmarshalVerifyXrd(frame []byte) error` — `frame` is the whole response-header body (≥16 bytes, e.g. 24 for pg ops); verifies `CRC32C(frame[4:]) == frame[0:4]` and decodes the fixed 16 bytes. CRC mismatch and short frames are errors.
  - `func (o StatusBody) MarshalXrd(wBuffer *xrdenc.WBuffer) error` — writes the 16 bytes with the stored CRC32C (callers computing a frame use `SetCRC`).
  - `func StatusFrame(body StatusBody, info []byte) []byte` — assembles `body`+`info` and stamps the correct CRC over bytes 4..end; used by tests and mock servers.

- [x] **Step 1: Write the failing test**

`xrootd/xrdproto/status_test.go`:

```go
package xrdproto

import (
	"encoding/binary"
	"testing"
)

func TestStatusFrameRoundTrip(t *testing.T) {
	info := make([]byte, 8)
	binary.BigEndian.PutUint64(info, 4096) // pg file offset
	body := StatusBody{
		StreamID:   StreamID{0, 7},
		RequestID:  30, // kXR_pgread - 3000
		RespType:   PartialResult,
		DataLength: 4100,
	}
	frame := StatusFrame(body, info)
	if len(frame) != StatusBodyLength+len(info) {
		t.Fatalf("frame length: got=%d want=%d", len(frame), StatusBodyLength+len(info))
	}

	var got StatusBody
	if err := got.UnmarshalVerifyXrd(frame); err != nil {
		t.Fatalf("UnmarshalVerifyXrd: %v", err)
	}
	if got.StreamID != body.StreamID || got.RespType != PartialResult || got.DataLength != 4100 {
		t.Fatalf("decoded body mismatch: %+v", got)
	}

	// Corrupt one info byte: the CRC covers it, so verification must fail.
	frame[len(frame)-1] ^= 0xff
	if err := got.UnmarshalVerifyXrd(frame); err == nil {
		t.Fatal("UnmarshalVerifyXrd accepted a corrupted frame")
	}
}

func TestStatusBodyShortFrame(t *testing.T) {
	var b StatusBody
	if err := b.UnmarshalVerifyXrd(make([]byte, StatusBodyLength-1)); err == nil {
		t.Fatal("expected error for a short status frame")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./xrootd/xrdproto/ -run TestStatus -v`
Expected: FAIL — `undefined: StatusBody` etc.

- [x] **Step 3: Implement**

Append to the `ResponseStatus` const block in `xrootd/xrdproto/xrdproto.go`:

```go
	// Status indicates that the request submitted was processed and the
	// response is a kXR_status frame: a CRC-protected StatusBody, an
	// operation-specific info tail, and possibly trailing data whose length
	// is carried in the body itself (outside the response-header dlen).
	Status ResponseStatus = 4007
```

Create `xrootd/xrdproto/status.go`:

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // NOTE: actually "package xrdproto" — see file header convention below.
```

The real file content:

```go
package xrdproto // import "go-hep.org/x/hep/xrootd/xrdproto"

import (
	"encoding/binary"
	"fmt"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdsum"
)

// StatusBodyLength is the fixed length of the StatusBody in bytes; an
// operation-specific info tail may follow it inside the response header's
// data length (pg operations append an 8-byte file offset).
const StatusBodyLength = 16

// Response types carried in StatusBody.RespType.
const (
	// FinalResult marks the last kXR_status frame of a response.
	FinalResult uint8 = 0x00
	// PartialResult marks an intermediate frame; more frames follow.
	PartialResult uint8 = 0x01
	// ProgressInfo is a keep-alive progress frame.
	ProgressInfo uint8 = 0x02
)

// StatusBody is the fixed part of a kXR_status response frame. The CRC32C
// field covers every byte of the frame after itself (the body remainder and
// the info tail), per RFC 7143.
type StatusBody struct {
	CRC32C     uint32
	StreamID   StreamID
	RequestID  uint8 // request code minus 3000 (kXR_pgread -> 30)
	RespType   uint8 // FinalResult, PartialResult or ProgressInfo
	Reserved   [4]byte
	DataLength int32 // trailing data bytes that follow the frame on the wire
}

// MarshalXrd implements Marshaler. The stored CRC32C is written as-is;
// use StatusFrame to build a frame with a correctly stamped CRC.
func (o StatusBody) MarshalXrd(wBuffer *xrdenc.WBuffer) error {
	wBuffer.WriteU32(o.CRC32C)
	wBuffer.WriteBytes(o.StreamID[:])
	wBuffer.WriteU8(o.RequestID)
	wBuffer.WriteU8(o.RespType)
	wBuffer.WriteBytes(o.Reserved[:])
	wBuffer.WriteI32(o.DataLength)
	return nil
}

// UnmarshalVerifyXrd decodes the fixed 16-byte body from frame (the complete
// response-header payload, including any info tail) after verifying that the
// leading CRC-32C matches the remainder of the frame.
func (o *StatusBody) UnmarshalVerifyXrd(frame []byte) error {
	if len(frame) < StatusBodyLength {
		return fmt.Errorf("xrootd: kXR_status frame too short: %d bytes (min %d)", len(frame), StatusBodyLength)
	}
	want := binary.BigEndian.Uint32(frame[:4])
	if got := xrdsum.CRC32C(frame[4:]); got != want {
		return fmt.Errorf("xrootd: kXR_status frame CRC mismatch: got=%#08x want=%#08x", got, want)
	}
	o.CRC32C = want
	copy(o.StreamID[:], frame[4:6])
	o.RequestID = frame[6]
	o.RespType = frame[7]
	copy(o.Reserved[:], frame[8:12])
	o.DataLength = int32(binary.BigEndian.Uint32(frame[12:16]))
	return nil
}

// StatusFrame assembles a kXR_status frame from body and the operation
// specific info tail, stamping the CRC-32C over everything after the CRC
// field itself. It is intended for servers and tests.
func StatusFrame(body StatusBody, info []byte) []byte {
	frame := make([]byte, StatusBodyLength+len(info))
	copy(frame[4:6], body.StreamID[:])
	frame[6] = body.RequestID
	frame[7] = body.RespType
	copy(frame[8:12], body.Reserved[:])
	binary.BigEndian.PutUint32(frame[12:16], uint32(body.DataLength))
	copy(frame[16:], info)
	binary.BigEndian.PutUint32(frame[:4], xrdsum.CRC32C(frame[4:]))
	return frame
}
```

> Delete the placeholder note above — the file starts with the standard ©2026 license header and `package xrdproto`. Check `xrdenc.WBuffer` has `WriteU32` (it has `WriteI32`/`WriteU16`/`WriteU8`; verify `WriteU32` exists in `xrootd/internal/xrdenc/xrdenc.go` — if absent, add it there mirroring `WriteI32`, with a one-line doc comment, in this task).

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./xrootd/xrdproto/ -run TestStatus -v` then `go test ./xrootd/xrdproto/...`
Expected: PASS, no regressions.

- [x] **Step 5: Commit**

```bash
gofmt -w xrootd/xrdproto/ xrootd/internal/xrdenc/ && go vet ./xrootd/...
git add xrootd/xrdproto/ xrootd/internal/xrdenc/
git commit -m "xrootd/xrdproto: add kXR_status response code and CRC-verified StatusBody"
```

---

### Task 3: page codec (`internal/pgbuf`)

**Files:**
- Create: `xrootd/internal/pgbuf/pgbuf.go`
- Test: `xrootd/internal/pgbuf/pgbuf_test.go`

**Interfaces:**
- Consumes: `xrdsum.CRC32C`.
- Produces (package `pgbuf`):
  - `const PageSize = 4096`.
  - `func Encode(off int64, data []byte) []byte` — splits data into page units `[crc32c BE u32][chunk]`; the first chunk is `PageSize - off%PageSize` bytes (file-offset alignment), subsequent chunks `PageSize`, the last possibly short.
  - `func Decode(off int64, frame []byte) ([]byte, error)` — inverse; verifies each unit's CRC (error names the failing file offset) and errors on malformed framing (e.g. truncated unit).
  - `func EncodedLen(off int64, n int) int` — bytes Encode will produce for n data bytes at off.

- [x] **Step 1: Write the failing test**

`xrootd/internal/pgbuf/pgbuf_test.go`:

```go
package pgbuf

import (
	"bytes"
	"testing"
)

func TestEncodeDecodeAligned(t *testing.T) {
	data := bytes.Repeat([]byte{0xab}, 2*PageSize+100) // 2 full pages + short tail
	frame := Encode(0, data)
	wantLen := (2*PageSize + 100) + 3*4 // 3 units, 4-byte crc each
	if len(frame) != wantLen {
		t.Fatalf("encoded length: got=%d want=%d", len(frame), wantLen)
	}
	if got := EncodedLen(0, len(data)); got != wantLen {
		t.Fatalf("EncodedLen: got=%d want=%d", got, wantLen)
	}
	back, err := Decode(0, frame)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(back, data) {
		t.Fatal("roundtrip mismatch")
	}
}

func TestEncodeUnalignedFirstPage(t *testing.T) {
	// Starting at offset 4000: first unit carries only 96 bytes to realign.
	data := bytes.Repeat([]byte{0x5c}, 96+PageSize)
	frame := Encode(4000, data)
	wantLen := (96 + 4) + (PageSize + 4)
	if len(frame) != wantLen {
		t.Fatalf("encoded length: got=%d want=%d", len(frame), wantLen)
	}
	back, err := Decode(4000, frame)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(back, data) {
		t.Fatal("roundtrip mismatch")
	}
}

func TestDecodeCorruptPage(t *testing.T) {
	frame := Encode(0, bytes.Repeat([]byte{1}, PageSize))
	frame[10] ^= 0xff
	if _, err := Decode(0, frame); err == nil {
		t.Fatal("Decode accepted a corrupted page")
	}
}

func TestDecodeTruncatedUnit(t *testing.T) {
	frame := Encode(0, bytes.Repeat([]byte{1}, 100))
	if _, err := Decode(0, frame[:3]); err == nil {
		t.Fatal("Decode accepted a truncated unit header")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./xrootd/internal/pgbuf/ -v`
Expected: FAIL (package missing).

- [x] **Step 3: Implement**

`xrootd/internal/pgbuf/pgbuf.go`:

```go
// Package pgbuf implements the page-unit codec shared by kXR_pgread and
// kXR_pgwrite: data is carried as [crc32c BE u32][chunk] units where chunks
// align to PageSize boundaries of the *file* offset, so the first chunk of
// an unaligned request is short.
package pgbuf

import (
	"encoding/binary"
	"fmt"

	"go-hep.org/x/hep/xrootd/xrdsum"
)

// PageSize is kXR_pgPageSZ: the page length pg data units align to.
const PageSize = 4096

// firstChunk returns the length of the first data chunk for a transfer
// starting at file offset off with n bytes remaining.
func firstChunk(off int64, n int) int {
	c := PageSize - int(off%PageSize)
	if c > n {
		c = n
	}
	return c
}

// EncodedLen returns the number of bytes Encode produces for n data bytes
// starting at file offset off.
func EncodedLen(off int64, n int) int {
	if n == 0 {
		return 0
	}
	units := 1
	rest := n - firstChunk(off, n)
	units += rest / PageSize
	if rest%PageSize != 0 {
		units++
	}
	return n + 4*units
}

// Encode splits data into page units with per-page CRC-32C headers.
func Encode(off int64, data []byte) []byte {
	out := make([]byte, 0, EncodedLen(off, len(data)))
	for len(data) > 0 {
		c := firstChunk(off, len(data))
		var crc [4]byte
		binary.BigEndian.PutUint32(crc[:], xrdsum.CRC32C(data[:c]))
		out = append(out, crc[:]...)
		out = append(out, data[:c]...)
		data = data[c:]
		off += int64(c)
	}
	return out
}

// Decode verifies and strips the page-unit framing from frame, which carries
// data starting at file offset off. It returns the reassembled data or an
// error naming the file offset of the first corrupt or malformed unit.
func Decode(off int64, frame []byte) ([]byte, error) {
	out := make([]byte, 0, len(frame))
	for len(frame) > 0 {
		if len(frame) < 4 {
			return nil, fmt.Errorf("xrootd: malformed page unit at offset %d: %d trailing bytes", off, len(frame))
		}
		want := binary.BigEndian.Uint32(frame[:4])
		frame = frame[4:]
		c := PageSize - int(off%PageSize)
		if c > len(frame) {
			c = len(frame)
		}
		if got := xrdsum.CRC32C(frame[:c]); got != want {
			return nil, fmt.Errorf("xrootd: page CRC mismatch at offset %d: got=%#08x want=%#08x", off, got, want)
		}
		out = append(out, frame[:c]...)
		frame = frame[c:]
		off += int64(c)
	}
	return out, nil
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./xrootd/internal/pgbuf/ -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
gofmt -w xrootd/internal/pgbuf/ && go vet ./xrootd/...
git add xrootd/internal/pgbuf/
git commit -m "xrootd/internal/pgbuf: add pg page-unit codec with per-page CRC32C"
```

---

### Task 4: `kXR_status` frames in the session read loop

**Files:**
- Modify: `xrootd/session.go` (the `consume` method)
- Test: `xrootd/session_status_test.go` (create)

**Interfaces:**
- Consumes: `xrdproto.Status`, `xrdproto.StatusBody`, `xrdproto.StatusFrame`, `xrdproto.PartialResult`/`FinalResult` (Task 2); the mock helpers `readBootstrapRequest`/`writeBootstrapResponse` and harness `testClientWithMockServer` (existing).
- Produces: `consume()` handles `Status` frames: verifies the frame CRC, drains `StatusBody.DataLength` trailing bytes off the socket, forwards `frame||trailing` to the mux, and keeps the stream open while `RespType == PartialResult` (also for `ProgressInfo`). Callers therefore receive, per request, the concatenation of complete status frames — each self-describing via its embedded `DataLength`.

- [x] **Step 1: Write the failing test**

`xrootd/session_status_test.go` — a mock server answers one request with two kXR_status frames (partial then final), each with trailing data *outside* the response-header dlen; the client must receive both frames concatenated. Use `ping` as the request vehicle (any request works — status handling is generic):

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"net"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/ping"
)

// rawStatusResponse is a Response that captures the raw bytes handed back by
// the session layer.
type rawStatusResponse struct{ data []byte }

func (r *rawStatusResponse) UnmarshalXrd(rBuffer *xrdenc.RBuffer) error {
	r.data = rBuffer.Bytes()
	return nil
}
func (r *rawStatusResponse) RespID() uint16 { return ping.RequestID }

func TestConsumeStatusFrames(t *testing.T) {
	trailing1 := []byte("0123456789")      // partial frame payload
	trailing2 := []byte("abcdef")          // final frame payload

	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			t.Errorf("could not read request: %v", err)
			return
		}
		var hdr xrdproto.RequestHeader
		_ = hdr.UnmarshalXrd(xrdenc.NewRBuffer(data[:xrdproto.RequestHeaderLength]))

		writeStatus := func(resptype uint8, trailing []byte) {
			frame := xrdproto.StatusFrame(xrdproto.StatusBody{
				StreamID:   hdr.StreamID,
				RespType:   resptype,
				DataLength: int32(len(trailing)),
			}, nil)
			respHdr := xrdproto.ResponseHeader{StreamID: hdr.StreamID, Status: xrdproto.Status, DataLength: int32(len(frame))}
			var w xrdenc.WBuffer
			_ = respHdr.MarshalXrd(&w)
			out := append(w.Bytes(), frame...)
			out = append(out, trailing...) // trailing data lives OUTSIDE the header dlen
			if _, err := conn.Write(out); err != nil {
				cancel()
			}
		}
		writeStatus(xrdproto.PartialResult, trailing1)
		writeStatus(xrdproto.FinalResult, trailing2)
	}

	clientFunc := func(cancel func(), client *Client) {
		var resp rawStatusResponse
		_, err := client.Send(context.Background(), &resp, &ping.Request{})
		if err != nil {
			t.Fatalf("Send: %v", err)
		}
		want := xrdproto.StatusBodyLength + len(trailing1) + xrdproto.StatusBodyLength + len(trailing2)
		if len(resp.data) != want {
			t.Fatalf("received %d bytes, want %d (two frames with trailing data)", len(resp.data), want)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}
```

> Verify at implementation time: `ping.Request{}` satisfies `xrdproto.Request` (check `xrootd/xrdproto/ping/ping.go` for the exact zero-value construction used by `ping_mock_test.go`, and reuse that). `xrdenc.RBuffer.Bytes()` — check it exists (grep `func (r *RBuffer)` in `xrootd/internal/xrdenc/xrdenc.go`); if the remaining-bytes accessor has a different name (e.g. `ReadBytes` into a sized slice with `Len()`), adapt `rawStatusResponse.UnmarshalXrd` accordingly.

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./xrootd/ -run TestConsumeStatusFrames -v`
Expected: FAIL — with no Status handling, the first frame closes the stream after `hdr.dlen` bytes and 10 trailing bytes are misread as the next response header (a hang resolved by the test context/cancel, a length mismatch, or a panic — any failure counts).

- [x] **Step 3: Implement in `consume()`**

In `xrootd/session.go`, inside the `switch header.Status` block of `consume()`, add a `case xrdproto.Status:` **and** change the stream-cleanup condition. The current tail of the loop is:

```go
			if err := sess.mux.SendData(header.StreamID, resp); err != nil { ... }

			if header.Status != xrdproto.OkSoFar {
				sess.cleanupRequest(header.StreamID)
			}
```

New logic (complete replacement for the switch + tail):

```go
			var statusPartial bool
			switch header.Status {
			case xrdproto.Error:
				resp.Err = header.Error(resp.Data)
			case xrdproto.Wait:
				resp.Err = sess.handleWaitResponse(header.StreamID, resp.Data)
				if resp.Err == nil {
					continue
				}
			case xrdproto.Redirect:
				resp.Redirection, resp.Err = mux.ParseRedirection(resp.Data)
			case xrdproto.Status:
				resp.Data, statusPartial, resp.Err = sess.readStatusTail(resp.Data)
			}

			if err := sess.mux.SendData(header.StreamID, resp); err != nil {
				if sess.ctx.Err() != nil {
					continue
				}
				panic(err)
			}

			if header.Status != xrdproto.OkSoFar && !statusPartial {
				sess.cleanupRequest(header.StreamID)
			}
```

And add the helper method to `session.go`:

```go
// readStatusTail completes a kXR_status frame: it verifies the frame's CRC,
// then drains the trailing data announced by StatusBody.DataLength — which
// lives OUTSIDE the response header's data length — off the connection.
// It returns the full frame (body+info+trailing), whether more frames follow
// (kXR_PartialResult or kXR_ProgressInfo), and any error. On error the frame
// is returned as-is so the caller can surface it.
func (sess *cliSession) readStatusTail(frame []byte) ([]byte, bool, error) {
	var body xrdproto.StatusBody
	if err := body.UnmarshalVerifyXrd(frame); err != nil {
		return frame, false, err
	}
	if body.DataLength > 0 {
		tail := make([]byte, body.DataLength)
		if _, err := io.ReadFull(sess.conn, tail); err != nil {
			return frame, false, fmt.Errorf("xrootd: could not read kXR_status trailing data: %w", err)
		}
		frame = append(frame, tail...)
	}
	partial := body.RespType == xrdproto.PartialResult || body.RespType == xrdproto.ProgressInfo
	return frame, partial, nil
}
```

> Add `"io"` to `session.go` imports. Note the `resp.Data` reuse hazard: `ReadResponseWithReuse` may reuse the buffer between loop iterations — check whether `resp.Data` aliases a reused buffer (read `ReadResponseWithReuse` in `xrdproto.go`); the existing mux path already copies via `data = append(data, resp.Data...)` in `session.send`, and `append(frame, tail...)` may grow a fresh array, which is fine. If `ReadResponseWithReuse` reuses `headerBytes` only, no change needed.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./xrootd/ -run TestConsumeStatusFrames -v`, then `go test ./xrootd/...` and `go test -race ./xrootd/ -run TestConsumeStatusFrames`.
Expected: PASS everywhere; no regressions.

- [x] **Step 5: Commit**

```bash
gofmt -w xrootd/session.go xrootd/session_status_test.go && go vet ./xrootd/...
git add xrootd/session.go xrootd/session_status_test.go
git commit -m "xrootd: handle kXR_status framing in the session read loop"
```

---

### Task 5: `xrdproto/pgread`

**Files:**
- Create: `xrootd/xrdproto/pgread/pgread.go`
- Test: `xrootd/xrdproto/pgread/pgread_test.go`
- Modify: `xrootd/xrdproto/signing/signing.go` (add pgread at Pedantic, next to `read.RequestID`)

**Interfaces:**
- Consumes: `xrdproto.StatusBody`/`StatusFrame`, `pgbuf.Decode`/`Encode`, `xrdfs.FileHandle`.
- Produces (package `pgread`):
  - `const RequestID uint16 = 3030`.
  - `type Request struct { Handle xrdfs.FileHandle; Offset int64; ReadLength int32 }` with `ReqID()`, `ShouldSign() bool` (returns `false` — reads sign only when the security level demands, mirroring `read`), `MarshalXrd`/`UnmarshalXrd` writing `fhandle[4] | offset i64 | rlen i32 | dlen i32=0`.
  - `type Response struct { Data []byte; Offset int64 }` with `RespID()` and `UnmarshalXrd` that walks one or more concatenated status frames (`[StatusBody 16][offset i64][page units DataLength bytes]`)... decoding and CRC-verifying the pages of each frame with `pgbuf.Decode` at that frame's offset, appending to `Data`. `Offset` records the first frame's offset. Frames with `DataLength == 0` (e.g. ProgressInfo) are skipped.

- [x] **Step 1: Write the failing test**

`xrootd/xrdproto/pgread/pgread_test.go`:

```go
package pgread

import (
	"bytes"
	"encoding/binary"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/pgbuf"
	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

func TestRequestMarshalRoundTrip(t *testing.T) {
	req := Request{Handle: xrdfs.FileHandle{1, 2, 3, 4}, Offset: 8192, ReadLength: 1 << 20}
	var w xrdenc.WBuffer
	if err := req.MarshalXrd(&w); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(w.Bytes()) != 20 { // 16 params + 4 dlen
		t.Fatalf("marshaled length: got=%d want=20", len(w.Bytes()))
	}
	var back Request
	if err := back.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back != req {
		t.Fatalf("roundtrip mismatch: got=%+v want=%+v", back, req)
	}
	if req.ReqID() != RequestID {
		t.Fatalf("ReqID: got=%d want=%d", req.ReqID(), RequestID)
	}
}

// statusFrameFor builds one pg status frame carrying data at off.
func statusFrameFor(off int64, data []byte, resptype uint8) []byte {
	pages := pgbuf.Encode(off, data)
	info := make([]byte, 8)
	binary.BigEndian.PutUint64(info, uint64(off))
	frame := xrdproto.StatusFrame(xrdproto.StatusBody{
		RequestID:  uint8(RequestID - 3000),
		RespType:   resptype,
		DataLength: int32(len(pages)),
	}, info)
	return append(frame, pages...)
}

func TestResponseUnmarshalTwoFrames(t *testing.T) {
	part1 := bytes.Repeat([]byte{0x11}, pgbuf.PageSize)
	part2 := bytes.Repeat([]byte{0x22}, 100)
	wire := append(
		statusFrameFor(0, part1, xrdproto.PartialResult),
		statusFrameFor(int64(len(part1)), part2, xrdproto.FinalResult)...,
	)

	var resp Response
	if err := resp.UnmarshalXrd(xrdenc.NewRBuffer(wire)); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if want := append(append([]byte{}, part1...), part2...); !bytes.Equal(resp.Data, want) {
		t.Fatalf("data mismatch: got %d bytes want %d", len(resp.Data), len(want))
	}
	if resp.Offset != 0 {
		t.Fatalf("offset: got=%d want=0", resp.Offset)
	}
}

func TestResponseCorruptPage(t *testing.T) {
	wire := statusFrameFor(0, bytes.Repeat([]byte{0x33}, 64), xrdproto.FinalResult)
	wire[len(wire)-1] ^= 0xff // corrupt page data
	var resp Response
	if err := resp.UnmarshalXrd(xrdenc.NewRBuffer(wire)); err == nil {
		t.Fatal("accepted corrupt page data")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./xrootd/xrdproto/pgread/ -v`
Expected: FAIL (package missing).

- [x] **Step 3: Implement**

`xrootd/xrdproto/pgread/pgread.go`:

```go
// Package pgread contains the structures describing the pgread request and
// response (kXR_pgread), the paged read with per-page CRC-32C integrity.
// Responses arrive as kXR_status frames; the session layer concatenates the
// complete frames of one request, and Response.UnmarshalXrd walks them.
package pgread // import "go-hep.org/x/hep/xrootd/xrdproto/pgread"

import (
	"encoding/binary"
	"fmt"

	"go-hep.org/x/hep/xrootd/internal/pgbuf"
	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

// RequestID is the id of the request, it is sent as part of message.
const RequestID uint16 = 3030

// Request holds the pgread request parameters.
type Request struct {
	Handle     xrdfs.FileHandle
	Offset     int64
	ReadLength int32
}

// ReqID implements xrdproto.Request.ReqID.
func (req *Request) ReqID() uint16 { return RequestID }

// ShouldSign implements xrdproto.Request.ShouldSign.
func (req *Request) ShouldSign() bool { return false }

// MarshalXrd implements xrdproto.Marshaler.
func (o Request) MarshalXrd(w *xrdenc.WBuffer) error {
	w.WriteBytes(o.Handle[:])
	w.WriteI64(o.Offset)
	w.WriteI32(o.ReadLength)
	w.WriteI32(0) // dlen: no args
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (o *Request) UnmarshalXrd(r *xrdenc.RBuffer) error {
	r.ReadBytes(o.Handle[:])
	o.Offset = r.ReadI64()
	o.ReadLength = r.ReadI32()
	r.Skip(4)
	return nil
}

// Response is a response for the pgread request: the CRC-verified data and
// the file offset of its first byte.
type Response struct {
	Data   []byte
	Offset int64
}

// RespID implements xrdproto.Response.RespID.
func (resp *Response) RespID() uint16 { return RequestID }

// UnmarshalXrd implements xrdproto.Unmarshaler. It walks the concatenated
// kXR_status frames of a pgread response, verifying and stripping the page
// framing of each.
func (resp *Response) UnmarshalXrd(r *xrdenc.RBuffer) error {
	wire := r.Bytes()
	first := true
	for len(wire) > 0 {
		if len(wire) < xrdproto.StatusBodyLength+8 {
			return fmt.Errorf("xrootd: truncated pgread status frame: %d bytes", len(wire))
		}
		var body xrdproto.StatusBody
		if err := body.UnmarshalVerifyXrd(wire[:xrdproto.StatusBodyLength+8]); err != nil {
			return err
		}
		off := int64(binary.BigEndian.Uint64(wire[xrdproto.StatusBodyLength : xrdproto.StatusBodyLength+8]))
		wire = wire[xrdproto.StatusBodyLength+8:]
		n := int(body.DataLength)
		if n > len(wire) {
			return fmt.Errorf("xrootd: pgread frame announces %d data bytes, %d available", n, len(wire))
		}
		if n > 0 {
			data, err := pgbuf.Decode(off, wire[:n])
			if err != nil {
				return err
			}
			if first {
				resp.Offset = off
				first = false
			}
			resp.Data = append(resp.Data, data...)
		}
		wire = wire[n:]
	}
	return nil
}
```

> Verify `xrdenc.RBuffer` has `Bytes()` (remaining bytes) and `WriteI64`/`ReadI64` exist on the buffers — the `read`/`open` packages use 64-bit offsets, so mirror their calls exactly (grep `ReadI64\|WriteI64` under `xrootd/internal/xrdenc/`). If `Bytes()` is absent, add it to `RBuffer` (returns the unread remainder without consuming) in this task with a doc comment.

In `xrootd/xrdproto/signing/signing.go`, add pgread beside `read.RequestID` in the `Pedantic` block:

```go
		sr.requirements[pgread.RequestID] = xrdproto.SignNeeded
```

with import `"go-hep.org/x/hep/xrootd/xrdproto/pgread"`.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./xrootd/xrdproto/pgread/ -v && go test ./xrootd/...`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
gofmt -w xrootd/xrdproto/ xrootd/internal/xrdenc/ && go vet ./xrootd/...
git add xrootd/xrdproto/pgread/ xrootd/xrdproto/signing/signing.go xrootd/internal/xrdenc/
git commit -m "xrootd/xrdproto/pgread: add kXR_pgread request/response with page CRC verification"
```

---

### Task 6: `xrdproto/pgwrite`

**Files:**
- Create: `xrootd/xrdproto/pgwrite/pgwrite.go`
- Test: `xrootd/xrdproto/pgwrite/pgwrite_test.go`
- Modify: `xrootd/xrdproto/signing/signing.go` (add pgwrite at Intense, next to `write.RequestID`)

**Interfaces:**
- Consumes: `pgbuf.Encode`, `xrdproto.StatusBody`, `xrdfs.FileHandle`.
- Produces (package `pgwrite`):
  - `const RequestID uint16 = 3026`.
  - `type Request struct { Handle xrdfs.FileHandle; Offset int64; Data []byte }` with `ReqID()`, `ShouldSign() bool` (returns `true`, mirroring `write`), and `MarshalXrd` writing `fhandle[4] | offset i64 | pathid u8=0 | reqflags u8=0 | reserved[2] | dlen i32` followed by the page units (`pgbuf.Encode(Offset, Data)`). `UnmarshalXrd` reads the params and decodes the page units back into `Data` (servers/tests need it).
  - `type Response struct { Offset int64 }` — `UnmarshalXrd` parses one status frame `[StatusBody 16][offset i64]` via `UnmarshalVerifyXrd`.

- [x] **Step 1: Write the failing test**

`xrootd/xrdproto/pgwrite/pgwrite_test.go`:

```go
package pgwrite

import (
	"bytes"
	"encoding/binary"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/pgbuf"
	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

func TestRequestMarshalRoundTrip(t *testing.T) {
	data := bytes.Repeat([]byte{0x7e}, pgbuf.PageSize+10)
	req := Request{Handle: xrdfs.FileHandle{9, 8, 7, 6}, Offset: 4096, Data: data}
	var w xrdenc.WBuffer
	if err := req.MarshalXrd(&w); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// 16 params + 4 dlen + encoded page units.
	if want := 20 + pgbuf.EncodedLen(4096, len(data)); len(w.Bytes()) != want {
		t.Fatalf("marshaled length: got=%d want=%d", len(w.Bytes()), want)
	}
	var back Request
	if err := back.UnmarshalXrd(xrdenc.NewRBuffer(w.Bytes())); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Handle != req.Handle || back.Offset != req.Offset || !bytes.Equal(back.Data, data) {
		t.Fatal("roundtrip mismatch")
	}
}

func TestResponseUnmarshal(t *testing.T) {
	info := make([]byte, 8)
	binary.BigEndian.PutUint64(info, 12345)
	frame := xrdproto.StatusFrame(xrdproto.StatusBody{
		RequestID: uint8(RequestID - 3000),
		RespType:  xrdproto.FinalResult,
	}, info)
	var resp Response
	if err := resp.UnmarshalXrd(xrdenc.NewRBuffer(frame)); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Offset != 12345 {
		t.Fatalf("offset: got=%d want=12345", resp.Offset)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./xrootd/xrdproto/pgwrite/ -v`
Expected: FAIL (package missing).

- [x] **Step 3: Implement**

`xrootd/xrdproto/pgwrite/pgwrite.go`:

```go
// Package pgwrite contains the structures describing the pgwrite request and
// response (kXR_pgwrite), the paged write with per-page CRC-32C integrity.
// Phase 1 keeps pathid=0 (data on the control socket) and does not implement
// the kXR_pgRetry recovery flow: a server-detected CRC error is an error.
package pgwrite // import "go-hep.org/x/hep/xrootd/xrdproto/pgwrite"

import (
	"encoding/binary"
	"fmt"

	"go-hep.org/x/hep/xrootd/internal/pgbuf"
	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

// RequestID is the id of the request, it is sent as part of message.
const RequestID uint16 = 3026

// Request holds the pgwrite request parameters. Data is the plain file
// content; the page-unit framing (with per-page CRC-32C) is applied during
// marshaling.
type Request struct {
	Handle xrdfs.FileHandle
	Offset int64
	Data   []byte
}

// ReqID implements xrdproto.Request.ReqID.
func (req *Request) ReqID() uint16 { return RequestID }

// ShouldSign implements xrdproto.Request.ShouldSign.
func (req *Request) ShouldSign() bool { return true }

// MarshalXrd implements xrdproto.Marshaler.
func (o Request) MarshalXrd(w *xrdenc.WBuffer) error {
	pages := pgbuf.Encode(o.Offset, o.Data)
	w.WriteBytes(o.Handle[:])
	w.WriteI64(o.Offset)
	w.WriteU8(0) // pathid: control socket
	w.WriteU8(0) // reqflags: no retry
	w.Next(2)    // reserved
	w.WriteI32(int32(len(pages)))
	w.WriteBytes(pages)
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (o *Request) UnmarshalXrd(r *xrdenc.RBuffer) error {
	r.ReadBytes(o.Handle[:])
	o.Offset = r.ReadI64()
	r.Skip(4) // pathid, reqflags, reserved
	n := int(r.ReadI32())
	pages := make([]byte, n)
	r.ReadBytes(pages)
	data, err := pgbuf.Decode(o.Offset, pages)
	if err != nil {
		return err
	}
	o.Data = data
	return nil
}

// Response is a response for the pgwrite request: the file offset the
// server acknowledged, carried in a CRC-protected kXR_status frame.
type Response struct {
	Offset int64
}

// RespID implements xrdproto.Response.RespID.
func (resp *Response) RespID() uint16 { return RequestID }

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (resp *Response) UnmarshalXrd(r *xrdenc.RBuffer) error {
	frame := r.Bytes()
	if len(frame) < xrdproto.StatusBodyLength+8 {
		return fmt.Errorf("xrootd: truncated pgwrite status frame: %d bytes", len(frame))
	}
	var body xrdproto.StatusBody
	if err := body.UnmarshalVerifyXrd(frame[:xrdproto.StatusBodyLength+8]); err != nil {
		return err
	}
	resp.Offset = int64(binary.BigEndian.Uint64(frame[xrdproto.StatusBodyLength : xrdproto.StatusBodyLength+8]))
	return nil
}
```

> Verify `xrdenc.WBuffer.Next(n)` pads with zero bytes (it is used exactly so in `protocol.Request.MarshalXrd`) and that `RBuffer.ReadBytes` fills the given slice. Both patterns exist in `xrootd/xrdproto/protocol/protocol.go` — mirror them.

In `xrootd/xrdproto/signing/signing.go`, add pgwrite beside `write.RequestID` in the `Intense` block:

```go
		sr.requirements[pgwrite.RequestID] = xrdproto.SignNeeded
```

with import `"go-hep.org/x/hep/xrootd/xrdproto/pgwrite"`.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./xrootd/xrdproto/pgwrite/ -v && go test ./xrootd/...`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
gofmt -w xrootd/xrdproto/ && go vet ./xrootd/...
git add xrootd/xrdproto/pgwrite/ xrootd/xrdproto/signing/signing.go
git commit -m "xrootd/xrdproto/pgwrite: add kXR_pgwrite request/response with page framing"
```

---

### Task 7: client `PgReadAt`/`PgWriteAt` + optional `xrdfs` interfaces

**Files:**
- Modify: `xrootd/file.go`
- Modify: `xrootd/xrdfs/file.go` (append optional interfaces; do NOT touch the existing `File` interface)
- Test: `xrootd/pg_mock_test.go` (create)

**Interfaces:**
- Consumes: `pgread.Request/Response`, `pgwrite.Request/Response`, the `file` struct in `xrootd/file.go` (read it first: requests are issued via `f.fs.c.Send(ctx, resp, req)` with `f.handle` — mirror `ReadAtContext`/`VerifyWriteAt` exactly).
- Produces:
  - In `xrdfs` (interface-upgrade pattern, keeps existing implementers compiling):
    ```go
    // PgReader is implemented by files that support paged reads with
    // per-page CRC-32C integrity (kXR_pgread).
    type PgReader interface {
        PgReadAt(ctx context.Context, p []byte, off int64) (int, error)
    }

    // PgWriter is implemented by files that support paged writes with
    // per-page CRC-32C integrity (kXR_pgwrite).
    type PgWriter interface {
        PgWriteAt(ctx context.Context, p []byte, off int64) error
    }
    ```
  - On the client `file`: `func (f *file) PgReadAt(ctx context.Context, p []byte, off int64) (int, error)` and `func (f *file) PgWriteAt(ctx context.Context, p []byte, off int64) error`.

- [x] **Step 1: Write the failing test**

`xrootd/pg_mock_test.go` — mock server answers a pgread with two status frames and a pgwrite with an ack frame (reuse the harness). Read `xrootd/file_mock_test.go` first for how a `file` value is constructed against the mock (it builds `file{fs: &fileSystem{...}, handle: ...}`; mirror that construction exactly — copy the pattern from an existing test like `TestFile_ReadAt_Mock` if present, otherwise from `file_mock_test.go`'s first test):

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/pgbuf"
	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/pgread"
	"go-hep.org/x/hep/xrootd/xrdproto/pgwrite"
)

func writePgStatus(conn net.Conn, streamID xrdproto.StreamID, reqID uint16, resptype uint8, off int64, pages []byte) error {
	info := make([]byte, 8)
	binary.BigEndian.PutUint64(info, uint64(off))
	frame := xrdproto.StatusFrame(xrdproto.StatusBody{
		StreamID:   streamID,
		RequestID:  uint8(reqID - 3000),
		RespType:   resptype,
		DataLength: int32(len(pages)),
	}, info)
	respHdr := xrdproto.ResponseHeader{StreamID: streamID, Status: xrdproto.Status, DataLength: int32(len(frame))}
	var w xrdenc.WBuffer
	if err := respHdr.MarshalXrd(&w); err != nil {
		return err
	}
	out := append(w.Bytes(), frame...)
	out = append(out, pages...)
	_, err := conn.Write(out)
	return err
}

func TestFile_PgReadAt_Mock(t *testing.T) {
	want := bytes.Repeat([]byte{0x42}, pgbuf.PageSize+100)

	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		var gotReq pgread.Request
		gotHdr, err := unmarshalRequest(data, &gotReq)
		if err != nil {
			cancel()
			t.Errorf("bad pgread request: %v", err)
			return
		}
		if gotReq.Offset != 0 || int(gotReq.ReadLength) != len(want) {
			cancel()
			t.Errorf("pgread params: off=%d rlen=%d", gotReq.Offset, gotReq.ReadLength)
			return
		}
		// Two frames: first page, then the tail.
		if err := writePgStatus(conn, gotHdr.StreamID, pgread.RequestID, xrdproto.PartialResult, 0, pgbuf.Encode(0, want[:pgbuf.PageSize])); err != nil {
			cancel()
			return
		}
		if err := writePgStatus(conn, gotHdr.StreamID, pgread.RequestID, xrdproto.FinalResult, pgbuf.PageSize, pgbuf.Encode(pgbuf.PageSize, want[pgbuf.PageSize:])); err != nil {
			cancel()
			return
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		f := file{fs: client.FS().(*fileSystem), handle: xrdfs.FileHandle{0, 0, 0, 0}}
		got := make([]byte, len(want))
		n, err := f.PgReadAt(context.Background(), got, 0)
		if err != nil {
			t.Fatalf("PgReadAt: %v", err)
		}
		if n != len(want) || !bytes.Equal(got, want) {
			t.Fatalf("PgReadAt data mismatch: n=%d", n)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}

func TestFile_PgWriteAt_Mock(t *testing.T) {
	payload := bytes.Repeat([]byte{0x5a}, 100)

	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		var gotReq pgwrite.Request
		gotHdr, err := unmarshalRequest(data, &gotReq)
		if err != nil {
			cancel()
			t.Errorf("bad pgwrite request: %v", err)
			return
		}
		if !bytes.Equal(gotReq.Data, payload) || gotReq.Offset != 4096 {
			cancel()
			t.Errorf("pgwrite payload mismatch")
			return
		}
		if err := writePgStatus(conn, gotHdr.StreamID, pgwrite.RequestID, xrdproto.FinalResult, 4096, nil); err != nil {
			cancel()
			return
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		f := file{fs: client.FS().(*fileSystem), handle: xrdfs.FileHandle{0, 0, 0, 0}}
		if err := f.PgWriteAt(context.Background(), payload, 4096); err != nil {
			t.Fatalf("PgWriteAt: %v", err)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}
```

> Verify the `file` struct's field names in `xrootd/file.go` (`fs`, `handle`) and whether `client.FS()` returns `*fileSystem` — `file_mock_test.go` constructs files against the mock somehow; copy its exact construction. Adjust the two `clientFunc`s accordingly.

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./xrootd/ -run 'TestFile_PgReadAt_Mock|TestFile_PgWriteAt_Mock' -v`
Expected: FAIL — `f.PgReadAt undefined`.

- [x] **Step 3: Implement**

Append to `xrootd/xrdfs/file.go` the two optional interfaces exactly as in the Produces block above (with `"context"` already imported there).

Append to `xrootd/file.go`:

```go
// PgReadAt implements xrdfs.PgReader: it reads len(p) bytes into p starting
// at offset off using kXR_pgread, verifying the per-page CRC-32C of every
// page received.
func (f *file) PgReadAt(ctx context.Context, p []byte, off int64) (int, error) {
	resp := pgread.Response{}
	req := pgread.Request{Handle: f.handle, Offset: off, ReadLength: int32(len(p))}
	_, err := f.fs.c.Send(ctx, &resp, &req)
	if err != nil {
		return 0, err
	}
	n := copy(p, resp.Data)
	return n, nil
}

// PgWriteAt implements xrdfs.PgWriter: it writes p at offset off using
// kXR_pgwrite, attaching a CRC-32C to every page sent.
func (f *file) PgWriteAt(ctx context.Context, p []byte, off int64) error {
	resp := pgwrite.Response{}
	req := pgwrite.Request{Handle: f.handle, Offset: off, Data: p}
	_, err := f.fs.c.Send(ctx, &resp, &req)
	return err
}
```

with imports `pgread`/`pgwrite` added to `file.go`. Mirror the exact `Send` call pattern used by `ReadAtContext` in the same file (verify the field is `f.fs.c` — adjust if the receiver names differ).

Add a compile-time assertion near the existing ones (grep `var _ xrdfs.File` in `file.go`; add below it):

```go
var (
	_ xrdfs.PgReader = (*file)(nil)
	_ xrdfs.PgWriter = (*file)(nil)
)
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./xrootd/ -run 'TestFile_PgReadAt_Mock|TestFile_PgWriteAt_Mock' -v && go test ./xrootd/...`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
gofmt -w xrootd/ && go vet ./xrootd/...
git add xrootd/file.go xrootd/xrdfs/file.go xrootd/pg_mock_test.go
git commit -m "xrootd: add PgReadAt/PgWriteAt paged I/O with per-page CRC32C"
```

---

### Task 8: `xrdproto/fattr` + filesystem xattr methods

**Files:**
- Create: `xrootd/xrdproto/fattr/fattr.go`
- Test: `xrootd/xrdproto/fattr/fattr_test.go`
- Modify: `xrootd/filesystem.go` (xattr methods)
- Test: `xrootd/fattr_mock_test.go` (create)

**Interfaces:**
- Consumes: `xrdenc`, `xrdfs.FileHandle`.
- Produces (package `fattr`):
  - `const RequestID uint16 = 3020`; subcodes `Del uint8 = 0`, `Get uint8 = 1`, `List uint8 = 2`, `Set uint8 = 3`; `const MaxVars = 16`; options `IsNew uint8 = 0x01`, `AData uint8 = 0x10`.
  - `type Request struct { Handle xrdfs.FileHandle; Subcode uint8; NumAttr uint8; Options uint8; Body []byte }` — `MarshalXrd` writes `fhandle[4] | subcode | numattr | options | reserved[9] | dlen | body`; `UnmarshalXrd` inverse. `ReqID()`, `ShouldSign() bool` returning `true` (attribute mutation is Modifies-class; Get/List are harmless to sign).
  - Body builders (path-based, one attribute, mirroring `libxrdc`):
    - `func GetRequest(path, name string) *Request` — body `path\0` + nvec entry `[u16 rc=0][name\0]`, Subcode=Get, NumAttr=1.
    - `func SetRequest(path, name string, value []byte, isNew bool) *Request` — Get body + vvec `[i32 BE len][value]`, Subcode=Set, Options `IsNew` when isNew.
    - `func DelRequest(path, name string) *Request` — Subcode=Del.
    - `func ListRequest(path string) *Request` — body `path\0`, Subcode=List, NumAttr=0.
  - Response decoders:
    - `type Response struct { ErrCount uint8; NumAttr uint8; Raw []byte }` with `RespID()` and `UnmarshalXrd` capturing the raw payload (first two bytes + rest).
    - `func (resp *Response) Attr() (name string, rc uint16, value []byte, err error)` — parses `[u8 errcount][u8 numattr]` + one nvec entry `[u16 BE rc][name\0]` + (when present) one vvec entry `[i32 BE vlen][value]`. `rc != 0` (a kXR error code like AttrNotFound) is returned for the caller to surface.
    - `func (resp *Response) Names() ([]string, error)` — List reply: NUL-separated names in `Raw` (List responses have no errcount/numattr prefix — Raw is the whole payload; see Step 1 golden bytes).
- Produces (package `xrootd`, on `*fileSystem`):
  - `func (fs *fileSystem) GetXAttr(ctx context.Context, path, name string) ([]byte, error)`
  - `func (fs *fileSystem) SetXAttr(ctx context.Context, path, name string, value []byte) error`
  - `func (fs *fileSystem) DelXAttr(ctx context.Context, path, name string) error`
  - `func (fs *fileSystem) ListXAttr(ctx context.Context, path string) ([]string, error)`
  - These are methods on the concrete `*fileSystem`, NOT additions to the `xrdfs.FileSystem` interface; add an optional `xrdfs.XAttrFS` interface in `xrootd/xrdfs/fs.go` declaring the four methods, and a compile-time `var _ xrdfs.XAttrFS = (*fileSystem)(nil)` assertion.

- [x] **Step 1: Write the failing test (wire golden bytes)**

`xrootd/xrdproto/fattr/fattr_test.go`:

```go
package fattr

import (
	"bytes"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
)

func TestGetRequestWire(t *testing.T) {
	req := GetRequest("/a/f.root", "user.tag")
	var w xrdenc.WBuffer
	if err := req.MarshalXrd(&w); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := w.Bytes()
	// params: fhandle[4]=0 subcode=Get numattr=1 options=0 reserved[9]
	wantParams := append([]byte{0, 0, 0, 0, Get, 1, 0}, make([]byte, 9)...)
	if !bytes.Equal(got[:16], wantParams) {
		t.Fatalf("params mismatch:\ngot= % x\nwant=% x", got[:16], wantParams)
	}
	// body: "/a/f.root\0" + [u16 rc=0] + "user.tag\0"
	wantBody := append([]byte("/a/f.root\x00"), 0, 0)
	wantBody = append(wantBody, []byte("user.tag\x00")...)
	if !bytes.Equal(got[20:], wantBody) {
		t.Fatalf("body mismatch:\ngot= % x\nwant=% x", got[20:], wantBody)
	}
}

func TestSetRequestWire(t *testing.T) {
	req := SetRequest("/a", "user.k", []byte("v1"), true)
	if req.Subcode != Set || req.Options != IsNew || req.NumAttr != 1 {
		t.Fatalf("set request fields: %+v", req)
	}
	// body tail: vvec = [i32 BE 2]["v1"]
	wantTail := []byte{0, 0, 0, 2, 'v', '1'}
	if !bytes.HasSuffix(req.Body, wantTail) {
		t.Fatalf("vvec tail mismatch: % x", req.Body)
	}
}

func TestResponseAttr(t *testing.T) {
	// errcount=0 numattr=1, nvec [rc=0]["user.tag\0"], vvec [len=3]["abc"]
	raw := []byte{0, 1, 0, 0}
	raw = append(raw, []byte("user.tag\x00")...)
	raw = append(raw, 0, 0, 0, 3, 'a', 'b', 'c')
	resp := Response{Raw: raw}
	name, rc, value, err := resp.Attr()
	if err != nil {
		t.Fatalf("Attr: %v", err)
	}
	if name != "user.tag" || rc != 0 || string(value) != "abc" {
		t.Fatalf("Attr: name=%q rc=%d value=%q", name, rc, value)
	}
}

func TestResponseNames(t *testing.T) {
	resp := Response{Raw: []byte("user.a\x00user.b\x00")}
	names, err := resp.Names()
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	if len(names) != 2 || names[0] != "user.a" || names[1] != "user.b" {
		t.Fatalf("Names: %v", names)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./xrootd/xrdproto/fattr/ -v`
Expected: FAIL (package missing).

- [x] **Step 3: Implement `fattr` package**

`xrootd/xrdproto/fattr/fattr.go`:

```go
// Package fattr contains the structures describing the fattr request
// (kXR_fattr): extended-attribute get/set/delete/list on a path. The wire
// vectors follow XProtocol.hh: an nvec entry is [u16 rc][name\0] and a vvec
// entry is [i32 vlen][value], both big-endian; a path-based request body is
// "path\0" + nvec + (set only) vvec.
package fattr // import "go-hep.org/x/hep/xrootd/xrdproto/fattr"

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdfs"
)

// RequestID is the id of the request, it is sent as part of message.
const RequestID uint16 = 3020

// Subcodes selecting the fattr operation.
const (
	Del  uint8 = 0
	Get  uint8 = 1
	List uint8 = 2
	Set  uint8 = 3
)

// MaxVars is the maximum number of attributes per request (kXR_faMaxVars).
const MaxVars = 16

// Request options.
const (
	// IsNew requires that the attribute does not already exist (set only).
	IsNew uint8 = 0x01
	// AData asks list to also return attribute values.
	AData uint8 = 0x10
)

// Request holds the fattr request parameters. Body carries the path and
// attribute vectors; the builders below assemble it.
type Request struct {
	Handle  xrdfs.FileHandle
	Subcode uint8
	NumAttr uint8
	Options uint8
	Body    []byte
}

// ReqID implements xrdproto.Request.ReqID.
func (req *Request) ReqID() uint16 { return RequestID }

// ShouldSign implements xrdproto.Request.ShouldSign.
func (req *Request) ShouldSign() bool { return true }

// MarshalXrd implements xrdproto.Marshaler.
func (o Request) MarshalXrd(w *xrdenc.WBuffer) error {
	w.WriteBytes(o.Handle[:])
	w.WriteU8(o.Subcode)
	w.WriteU8(o.NumAttr)
	w.WriteU8(o.Options)
	w.Next(9)
	w.WriteI32(int32(len(o.Body)))
	w.WriteBytes(o.Body)
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (o *Request) UnmarshalXrd(r *xrdenc.RBuffer) error {
	r.ReadBytes(o.Handle[:])
	o.Subcode = r.ReadU8()
	o.NumAttr = r.ReadU8()
	o.Options = r.ReadU8()
	r.Skip(9)
	n := int(r.ReadI32())
	o.Body = make([]byte, n)
	r.ReadBytes(o.Body)
	return nil
}

func pathNameBody(path, name string) []byte {
	body := append([]byte(path), 0)
	body = append(body, 0, 0) // nvec rc placeholder
	body = append(body, []byte(name)...)
	return append(body, 0)
}

// GetRequest builds a path-based fattr get for a single attribute.
func GetRequest(path, name string) *Request {
	return &Request{Subcode: Get, NumAttr: 1, Body: pathNameBody(path, name)}
}

// DelRequest builds a path-based fattr delete for a single attribute.
func DelRequest(path, name string) *Request {
	return &Request{Subcode: Del, NumAttr: 1, Body: pathNameBody(path, name)}
}

// SetRequest builds a path-based fattr set for a single attribute; isNew
// requires the attribute to not exist yet.
func SetRequest(path, name string, value []byte, isNew bool) *Request {
	body := pathNameBody(path, name)
	var vlen [4]byte
	binary.BigEndian.PutUint32(vlen[:], uint32(len(value)))
	body = append(body, vlen[:]...)
	body = append(body, value...)
	req := &Request{Subcode: Set, NumAttr: 1, Body: body}
	if isNew {
		req.Options = IsNew
	}
	return req
}

// ListRequest builds a path-based fattr list request.
func ListRequest(path string) *Request {
	return &Request{Subcode: List, Body: append([]byte(path), 0)}
}

// Response is a response for the fattr request. Raw holds the payload;
// Attr and Names decode it for get/set/del and list respectively.
type Response struct {
	ErrCount uint8
	NumAttr  uint8
	Raw      []byte
}

// RespID implements xrdproto.Response.RespID.
func (resp *Response) RespID() uint16 { return RequestID }

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (resp *Response) UnmarshalXrd(r *xrdenc.RBuffer) error {
	resp.Raw = make([]byte, r.Len())
	r.ReadBytes(resp.Raw)
	if len(resp.Raw) >= 2 {
		resp.ErrCount = resp.Raw[0]
		resp.NumAttr = resp.Raw[1]
	}
	return nil
}

// Attr decodes a get/set/del reply for a single attribute: the per-attribute
// status code rc (0 on success, a kXR error code otherwise) and, for get,
// the attribute value.
func (resp *Response) Attr() (name string, rc uint16, value []byte, err error) {
	raw := resp.Raw
	if len(raw) < 2+2+1 {
		return "", 0, nil, fmt.Errorf("xrootd: fattr response too short: %d bytes", len(raw))
	}
	raw = raw[2:] // errcount, numattr
	rc = binary.BigEndian.Uint16(raw[:2])
	raw = raw[2:]
	i := bytes.IndexByte(raw, 0)
	if i < 0 {
		return "", 0, nil, fmt.Errorf("xrootd: fattr response: unterminated attribute name")
	}
	name = string(raw[:i])
	raw = raw[i+1:]
	if len(raw) >= 4 {
		n := int(binary.BigEndian.Uint32(raw[:4]))
		raw = raw[4:]
		if n > len(raw) {
			return "", 0, nil, fmt.Errorf("xrootd: fattr value length %d exceeds %d remaining bytes", n, len(raw))
		}
		value = raw[:n]
	}
	return name, rc, value, nil
}

// Names decodes a list reply: NUL-separated attribute names.
func (resp *Response) Names() ([]string, error) {
	raw := strings.TrimRight(string(resp.Raw), "\x00")
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\x00"), nil
}
```

> `RBuffer.Len()` exists (used by `protocol.Response.UnmarshalXrd`). Note `Names` reads `resp.Raw` whole — List replies have no errcount/numattr prefix, so `UnmarshalXrd`'s opportunistic prefix decode is harmless there (callers of Names ignore those fields).

- [x] **Step 4: Run wire tests, then write and pass the filesystem mock test**

Run: `go test ./xrootd/xrdproto/fattr/ -v` → PASS.

Then add the optional interface to `xrootd/xrdfs/fs.go`:

```go
// XAttrFS is implemented by filesystems that support extended attributes
// (kXR_fattr).
type XAttrFS interface {
	GetXAttr(ctx context.Context, path, name string) ([]byte, error)
	SetXAttr(ctx context.Context, path, name string, value []byte) error
	DelXAttr(ctx context.Context, path, name string) error
	ListXAttr(ctx context.Context, path string) ([]string, error)
}
```

Append to `xrootd/filesystem.go` (mirror the `Dirlist` method's Send pattern):

```go
// GetXAttr returns the value of the named extended attribute of path.
func (fs *fileSystem) GetXAttr(ctx context.Context, path, name string) ([]byte, error) {
	var resp fattr.Response
	if _, err := fs.c.Send(ctx, &resp, fattr.GetRequest(path, name)); err != nil {
		return nil, err
	}
	_, rc, value, err := resp.Attr()
	if err != nil {
		return nil, err
	}
	if rc != 0 {
		return nil, fmt.Errorf("xrootd: fattr get %q on %q failed with status code %d", name, path, rc)
	}
	return value, nil
}

// SetXAttr sets the named extended attribute of path to value.
func (fs *fileSystem) SetXAttr(ctx context.Context, path, name string, value []byte) error {
	var resp fattr.Response
	if _, err := fs.c.Send(ctx, &resp, fattr.SetRequest(path, name, value, false)); err != nil {
		return err
	}
	_, rc, _, err := resp.Attr()
	if err != nil {
		return err
	}
	if rc != 0 {
		return fmt.Errorf("xrootd: fattr set %q on %q failed with status code %d", name, path, rc)
	}
	return nil
}

// DelXAttr removes the named extended attribute of path.
func (fs *fileSystem) DelXAttr(ctx context.Context, path, name string) error {
	var resp fattr.Response
	if _, err := fs.c.Send(ctx, &resp, fattr.DelRequest(path, name)); err != nil {
		return err
	}
	_, rc, _, err := resp.Attr()
	if err != nil {
		return err
	}
	if rc != 0 {
		return fmt.Errorf("xrootd: fattr del %q on %q failed with status code %d", name, path, rc)
	}
	return nil
}

// ListXAttr lists the extended attribute names of path.
func (fs *fileSystem) ListXAttr(ctx context.Context, path string) ([]string, error) {
	var resp fattr.Response
	if _, err := fs.c.Send(ctx, &resp, fattr.ListRequest(path)); err != nil {
		return nil, err
	}
	return resp.Names()
}
```

with imports (`fattr`, `fmt`) added, plus the compile-time assertion next to the file's existing ones:

```go
var _ xrdfs.XAttrFS = (*fileSystem)(nil)
```

`xrootd/fattr_mock_test.go` (one representative round trip; the wire details are already covered by the golden tests):

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"net"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/fattr"
)

func TestFileSystem_GetXAttr_Mock(t *testing.T) {
	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		var gotReq fattr.Request
		gotHdr, err := unmarshalRequest(data, &gotReq)
		if err != nil || gotReq.Subcode != fattr.Get {
			cancel()
			t.Errorf("bad fattr request: subcode=%d err=%v", gotReq.Subcode, err)
			return
		}
		// reply: errcount=0 numattr=1 [rc=0]["user.tag\0"][len=3]["abc"]
		raw := []byte{0, 1, 0, 0}
		raw = append(raw, []byte("user.tag\x00")...)
		raw = append(raw, 0, 0, 0, 3, 'a', 'b', 'c')
		resp := fattr.Response{Raw: raw}
		if err := xrdproto.WriteResponse(conn, gotHdr.StreamID, xrdproto.Ok, rawMarshaler(raw)); err != nil {
			cancel()
		}
		_ = resp
	}

	clientFunc := func(cancel func(), client *Client) {
		fs := client.FS().(*fileSystem)
		value, err := fs.GetXAttr(context.Background(), "/a/f.root", "user.tag")
		if err != nil {
			t.Fatalf("GetXAttr: %v", err)
		}
		if string(value) != "abc" {
			t.Fatalf("GetXAttr value: %q", value)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}
```

> `rawMarshaler` is a tiny test helper (add to `bootstrap_testutil_test.go`): `type rawMarshaler []byte` with `MarshalXrd(w *xrdenc.WBuffer) error { w.WriteBytes(m); return nil }` — check whether `fattr.Response` itself marshals `Raw` correctly first; if `Response.MarshalXrd` is added for the server side, use it instead. Add whichever compiles cleanly.

Run: `go test ./xrootd/ -run TestFileSystem_GetXAttr_Mock -v && go test ./xrootd/...`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
gofmt -w xrootd/ && go vet ./xrootd/...
git add xrootd/xrdproto/fattr/ xrootd/filesystem.go xrootd/xrdfs/fs.go xrootd/fattr_mock_test.go xrootd/bootstrap_testutil_test.go
git commit -m "xrootd: add kXR_fattr extended-attribute support (get/set/del/list)"
```

---

### Task 9: filesystem `Checksum` via query `kXR_Qcksum`

**Files:**
- Modify: `xrootd/filesystem.go`
- Modify: `xrootd/xrdfs/fs.go` (optional interface)
- Test: `xrootd/checksum_mock_test.go` (create)

**Interfaces:**
- Consumes: the existing `xrootd/xrdproto/query` package (`query.Checksum = 3`; read `query.go` first for the exact `Request` shape — it has `Query uint16` + args payload; mirror how an existing caller builds it, or construct `query.Request{Query: query.Checksum, Args: []byte(path)}` if those are the field names).
- Produces:
  - `xrdfs.ChecksumFS` optional interface: `Checksum(ctx context.Context, path string) (algo, value string, err error)`.
  - `func (fs *fileSystem) Checksum(ctx context.Context, path string) (string, string, error)` — sends the Qcksum query, parses the `"<algo> <hexvalue>"` reply (fields split on whitespace, trailing NUL trimmed), errors on malformed replies.

- [x] **Step 1: Write the failing test**

`xrootd/checksum_mock_test.go`:

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"net"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/query"
)

func TestFileSystem_Checksum_Mock(t *testing.T) {
	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		var gotReq query.Request
		gotHdr, err := unmarshalRequest(data, &gotReq)
		if err != nil {
			cancel()
			t.Errorf("bad query request: %v", err)
			return
		}
		if err := xrdproto.WriteResponse(conn, gotHdr.StreamID, xrdproto.Ok, rawMarshaler("adler32 03d74a07\x00")); err != nil {
			cancel()
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		fs := client.FS().(*fileSystem)
		algo, value, err := fs.Checksum(context.Background(), "/a/f.root")
		if err != nil {
			t.Fatalf("Checksum: %v", err)
		}
		if algo != "adler32" || value != "03d74a07" {
			t.Fatalf("Checksum: algo=%q value=%q", algo, value)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}
```

> `rawMarshaler` accepts a string here — make it `type rawMarshaler []byte` and pass `rawMarshaler("adler32 03d74a07\x00")` (string→[]byte conversion applies). Verify `query.Request`'s field names against `xrootd/xrdproto/query/query.go` before writing the client code, and check whether an existing test (e.g. `xrootd/xrdproto/query`'s own tests or `filesystem_test.go`) shows the canonical construction.

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./xrootd/ -run TestFileSystem_Checksum_Mock -v`
Expected: FAIL — `fs.Checksum undefined`.

- [x] **Step 3: Implement**

Add to `xrootd/xrdfs/fs.go`:

```go
// ChecksumFS is implemented by filesystems that can report a server-side
// file checksum (query kXR_Qcksum).
type ChecksumFS interface {
	Checksum(ctx context.Context, path string) (algo, value string, err error)
}
```

Append to `xrootd/filesystem.go`:

```go
// Checksum asks the server for the checksum of path (query kXR_Qcksum) and
// returns the algorithm name and hexadecimal value the server reports.
func (fs *fileSystem) Checksum(ctx context.Context, path string) (string, string, error) {
	var resp query.Response
	req := query.Request{Query: query.Checksum, Args: []byte(path)}
	if _, err := fs.c.Send(ctx, &resp, &req); err != nil {
		return "", "", err
	}
	fields := strings.Fields(strings.TrimRight(string(resp.Data), "\x00"))
	if len(fields) != 2 {
		return "", "", fmt.Errorf("xrootd: malformed checksum reply %q for %q", resp.Data, path)
	}
	return fields[0], fields[1], nil
}
```

> The `query.Request`/`query.Response` field names above are the expected ones — verify against `xrootd/xrdproto/query/query.go` and adjust (the response may expose `Data []byte` or similar). Add `strings` to imports and the assertion `var _ xrdfs.ChecksumFS = (*fileSystem)(nil)`.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./xrootd/ -run TestFileSystem_Checksum_Mock -v && go test ./xrootd/...`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
gofmt -w xrootd/ && go vet ./xrootd/...
git add xrootd/filesystem.go xrootd/xrdfs/fs.go xrootd/checksum_mock_test.go
git commit -m "xrootd: add server-side Checksum query (kXR_Qcksum)"
```

---

### Task 10: dual-oracle interop tests + parity runbook update

**Files:**
- Create: `xrootd/phase1_interop_test.go`
- Modify: `docs/superpowers/testing/xrootd-phase-0-parity.md` → extend (or create `docs/superpowers/testing/xrootd-phase-1-parity.md`, preferred: new file)

**Interfaces:**
- Consumes: `Dial`, `fileSystem.Checksum/GetXAttr/SetXAttr/ListXAttr/DelXAttr`, `file.PgReadAt/PgWriteAt`, `xrdsum.Sum`.
- Produces: env-gated tests (skip without `XROOTD_P1_SERVER`) that, against a real server: open a file, `PgReadAt` it fully, compare `xrdsum.Sum("adler32", data)` with the server's `Checksum` reply; round-trip an xattr. Plus a runbook documenting the `libxrdc` cross-checks.

- [x] **Step 1: Write the gated test**

`xrootd/phase1_interop_test.go`:

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"os"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdsum"
)

// TestPhase1Interop exercises pgread, checksum and fattr against a real
// XRootD server. Skipped unless XROOTD_P1_SERVER is set, e.g.
//
//	XROOTD_P1_SERVER=root://server:1094 XROOTD_P1_PATH=//tmp/f.dat \
//	go test ./xrootd/ -run TestPhase1Interop -v
func TestPhase1Interop(t *testing.T) {
	server := os.Getenv("XROOTD_P1_SERVER")
	if server == "" {
		t.Skip("set XROOTD_P1_SERVER to run the phase-1 interop test")
	}
	path := os.Getenv("XROOTD_P1_PATH")
	if path == "" {
		t.Skip("set XROOTD_P1_PATH to a readable file on the server")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	be, err := Dial(ctx, server, os.Getenv("USER"))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer be.Close()

	fs := be.FS()

	t.Run("pgread-vs-checksum", func(t *testing.T) {
		st, err := fs.Stat(ctx, path)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		f, err := fs.Open(ctx, path, xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer f.Close(ctx)

		pg, ok := f.(xrdfs.PgReader)
		if !ok {
			t.Fatal("file does not implement xrdfs.PgReader")
		}
		data := make([]byte, st.Size())
		n, err := pg.PgReadAt(ctx, data, 0)
		if err != nil {
			t.Fatalf("PgReadAt: %v", err)
		}
		data = data[:n]

		cks, ok := fs.(xrdfs.ChecksumFS)
		if !ok {
			t.Skip("filesystem does not implement ChecksumFS")
		}
		algo, want, err := cks.Checksum(ctx, path)
		if err != nil {
			t.Skipf("server checksum unavailable: %v", err)
		}
		got, err := xrdsum.Sum(algo, data)
		if err != nil {
			t.Skipf("local digest for %q unavailable: %v", algo, err)
		}
		if got != want {
			t.Fatalf("checksum mismatch (%s): local=%s server=%s", algo, got, want)
		}
	})

	t.Run("fattr-roundtrip", func(t *testing.T) {
		xa, ok := fs.(xrdfs.XAttrFS)
		if !ok {
			t.Fatal("filesystem does not implement xrdfs.XAttrFS")
		}
		if err := xa.SetXAttr(ctx, path, "user.gohep", []byte("p1")); err != nil {
			t.Skipf("server rejects fattr set (may be disabled): %v", err)
		}
		v, err := xa.GetXAttr(ctx, path, "user.gohep")
		if err != nil {
			t.Fatalf("GetXAttr: %v", err)
		}
		if string(v) != "p1" {
			t.Fatalf("xattr value: %q", v)
		}
		if err := xa.DelXAttr(ctx, path, "user.gohep"); err != nil {
			t.Fatalf("DelXAttr: %v", err)
		}
	})
}
```

> Verify `xrdfs.OpenModeOwnerRead`/`OpenOptionsOpenRead` constant names in `xrootd/xrdfs/` (grep `OpenMode` and `OpenOptions` consts) and `f.Close(ctx)`'s signature against `xrdfs.File`.

- [x] **Step 2: Verify it skips cleanly**

Run: `go test ./xrootd/ -run TestPhase1Interop -v`
Expected: PASS with `--- SKIP`.

- [x] **Step 3: Write the parity runbook**

Create `docs/superpowers/testing/xrootd-phase-1-parity.md`:

```markdown
# Phase 1 Parity Verification (pg I/O, fattr, checksums)

## Oracle 1 — official XRootD server

```sh
export XROOTD_P1_SERVER=root://HOST:1094      # or roots:// for TLS
export XROOTD_P1_PATH=//tmp/testfile.dat
go test ./xrootd/ -run TestPhase1Interop -v
```

Expected: pgread returns CRC-verified data whose local adler32 matches the
server-reported kXR_Qcksum value; the fattr round trip succeeds (or skips
where the server disables xattrs).

## Oracle 2 — libxrdc cross-checks

All binaries under /home/rcurrie/HEP-x/nginx-xrootd/client/bin:

- Checksum parity: `xrdadler32 root://HOST//tmp/testfile.dat` and
  `xrdcrc64 root://HOST//tmp/testfile.dat` must equal
  `xrdsum.Sum("adler32"|"crc64", <bytes read by go-hep PgReadAt>)`.
- pg I/O cross-client: a file written by go-hep `PgWriteAt` must read back
  byte-identical through `xrdcp` (and vice versa), and both clients must
  agree with the server checksum.
- fattr: attributes set via go-hep `SetXAttr` must be visible to the C
  client's fattr helpers (`xrdfs` xattr subcommands) and vice versa.

## Regression

`go test ./xrootd/...` stays green; pg mock tests cover CRC corruption,
unaligned first pages, multi-frame responses, and status-frame CRC failures.
```

- [x] **Step 4: Final phase verification**

Run: `gofmt -l xrootd/` (empty), `go vet ./xrootd/...` (clean), `go test ./xrootd/...` (green), `go test -race ./xrootd/` (green).

- [x] **Step 5: Commit**

```bash
git add xrootd/phase1_interop_test.go docs/superpowers/testing/xrootd-phase-1-parity.md
git commit -m "xrootd: add phase-1 interop tests and parity runbook"
```

---

## Self-Review

**Spec coverage (roadmap Phase 1):**
- "pgread/pgwrite with per-page CRC32C and kXR_status response framing" → Tasks 2 (status body), 3 (page codec), 4 (read-loop framing), 5 (pgread), 6 (pgwrite), 7 (client API). ✓
- "fattr: get/set/list/delete" → Task 8. ✓
- "Checksum surface: query kXR_Qcksum plus adler32/crc32c/crc64 helpers and a Checksum API" → Tasks 1 (xrdsum) and 9 (query). `verifyw` crc32 already exists. ✓
- "Close remaining request-kind gaps (e.g. gpfile)" → explicitly deferred to Phase 2 (copy engine) in Global Constraints; `kXR_pgRetry` and pg-over-subsession likewise. Recorded as scope decisions, not omissions. ✓
- Dual-oracle testing + code-quality bar → Task 10 + per-task Steps 4/5. ✓

**Placeholder scan:** all code blocks are complete; the flagged verify-notes name the exact file and symbol to check (harness field names, `xrdenc` buffer methods, `query.Request` fields, `xrdfs` Open constants) — these are verification steps, not gaps. The one intentionally-deleted stray comment in Task 2 Step 3 is instructed to be removed. ✓

**Type consistency:** `StatusBody`/`StatusFrame`/`UnmarshalVerifyXrd`, `pgbuf.Encode/Decode/EncodedLen/PageSize`, `pgread.Request{Handle,Offset,ReadLength}`, `pgwrite.Request{Handle,Offset,Data}`, `fattr` builders/decoders, `PgReadAt/PgWriteAt`, `XAttrFS`/`ChecksumFS`/`PgReader`/`PgWriter` are used with identical names and signatures across Tasks 2–10. `RequestID-3000` byte convention appears identically in Tasks 5, 6, 7. ✓

**Known risks called out for the implementer:** (a) `xrdenc` may lack `WriteU32`/`WriteI64`/`RBuffer.Bytes()` — each task that needs one says to add it there if missing; (b) the mock-harness `file` construction in Task 7 must be copied from `file_mock_test.go`, not invented; (c) `query.Request` field names in Task 9 must be read before use; (d) the strict `dlen==24` pg framing mirrors `libxrdc` — if a real server sends info inside `dlen`, the interop test will surface it (fail loudly rather than desync).
