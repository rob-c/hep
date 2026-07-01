# Pure-Go XRootD Client Parity Roadmap

**Date:** 2026-07-01
**Status:** Approved design
**Target package:** `go-hep.org/x/hep/xrootd` (Go 1.24)

## 0. Acknowledgment

This roadmap stands on substantial prior work: the design, implementation, and
hard-won operational understanding of the native C client library `libxrdc`
(`/home/rcurrie/HEP-x/nginx-xrootd/client`). That codebase established the
protocol behaviors, edge cases, and VFS/copy/auth architecture this pure-Go
effort deliberately mirrors. Where this plan looks obvious, it is because the C
client already did the exploratory work of discovering what correct XRootD
client behavior actually requires. The Go implementation is a re-expression of
that understanding, and `libxrdc` (alongside the official XRootD implementation)
serves as a reference oracle throughout.

## 1. Goal

Extend go-hep's existing pure-Go `xrootd` package so its **client** reaches
functional parity with the native C client library `libxrdc`
(`/home/rcurrie/HEP-x/nginx-xrootd/client`) across four capability areas:

1. Native `root://` protocol parity
2. A copy engine (xrdcp-equivalent)
3. Alternative protocols (HTTP/HTTPS, S3, WebDAV) — **full parity, not read-only**
4. Expanded authentication

The work stays **pure Go** and **idiomatic**: concurrency uses goroutines and the
existing `internal/mux`, not a port of the C client's epoll/io_uring engine.

### Non-goals (explicitly deferred)

- FUSE mount driver (`xrootdfs`)
- Diagnostics tool suite (`xrddiag`, `xrd_doctor`, `storascan`, `metabench`,
  `clockskew`, `netdiag`, `mpxstats`, `xrdqstats`)
- An explicit epoll/io_uring async I/O engine (Go's runtime provides async I/O)

## 2. Current Baseline

Confirmed by inspection of `go-hep.org/x/hep/xrootd`:

**Present:** root:// client + server; `xrdfs` (`FileSystem`/`File`) and `xrdio`;
`internal/mux` stream multiplexing; requests for open, read, write, sync, stat,
statx, dirlist, mkdir, rmdir, rm, mv, chmod, truncate, locate, prepare, query,
protocol, ping, login, auth, bind, admin, endsess, decrypt, sigver, verifyw;
auth mechanisms unix, host, krb5; CLIs `xrd-cp`, `xrd-ls`, `xrd-client`,
`xrd-srv`.

**Absent (the gap to close):**

- TLS transport (`xroots://`) and TLS protocol negotiation
- `pgread` / `pgwrite` (per-page CRC32c + `kXR_status` framing)
- `fattr` (extended attributes: get/set/list/del)
- Checksum helpers (adler32 / crc32c / crc64) beyond the `query` constants and
  `verifyw`'s crc32
- Auth: gsi/x509 proxies, sss, bearer/scitokens, S3 credentials
- Any HTTP/HTTPS, S3, or WebDAV backend
- Any copy engine: chunked pump, TPC, recursive, resumable, ZIP

## 3. Architecture Principle: Protocol-Neutral VFS

All higher-level features (copy engine, CLIs) target a **protocol-neutral backend
interface**, so `root://`, `roots://`, `http(s)://`, `s3://`, and `dav://` are
interchangeable backends behind one API. This mirrors the C client's `vfs.c`
design, expressed as a Go interface.

Sketch (final signatures decided in each phase's own plan):

```go
// Backend is a protocol-neutral file/object store.
type Backend interface {
    Open(ctx context.Context, name string, mode OpenMode) (File, error)
    Stat(ctx context.Context, name string) (EntryStat, error)
    Remove(ctx context.Context, name string) error
    // ... mkdir, dirlist, rename, checksum, etc.
}

// A scheme router maps a URL to its Backend.
func Dial(ctx context.Context, rawurl string, opts ...Option) (Backend, error)
```

The native `root://` backend wraps the existing `Client`/`xrdfs`. HTTP/S3/WebDAV
backends (Phase 4) implement the same interface and therefore reuse the copy
engine and CLIs unchanged.

Each phase below is delivered as its **own** spec → plan → implementation cycle.

## 4. Phases

### Phase 0 — Foundations (blocker for most later work)

- **TLS / `xroots://`**: TLS handshake plus `kXR_protocol` TLS negotiation
  (`reqver` / security level fields), enabling encrypted transport. Prerequisite
  for gsi/token auth and HTTPS.
- **Scheme router + `Backend` interface**: the URL parser recognizing `root`,
  `roots`, `xroot`, `xroots` (and later `http(s)`, `s3`, `dav`), plus the
  `Backend`/VFS interface that Phases 2–4 target.

**Depends on:** nothing. **Unblocks:** Phases 1–4.

### Phase 1 — Native protocol parity (`root://`)

- `pgread` / `pgwrite` with per-page CRC32c and `kXR_status` response framing.
- `fattr`: get / set / list / delete extended attributes.
- Checksum surface: `query kXR_Qcksum` plus adler32 / crc32c / crc64 helpers and
  a `Checksum` API (`verifyw` crc32 already exists).
- Close remaining request-kind gaps and round out `xrdfs` accordingly.

**Depends on:** Phase 0 (router; TLS optional here). **Unblocks:** Phase 2.

### Phase 2 — Copy engine (xrdcp-equivalent)

- Chunked transfer pump with configurable parallel streams; local↔remote and
  remote↔remote directions.
- Third-party copy (TPC) via the rendezvous-key handshake.
- Recursive tree copy, resumable transfers, and post-transfer checksum verify.
- ZIP central-directory read + member extract, and ZIP write.
- Layer the new capabilities onto the existing `cmd/xrd-cp`.

**Depends on:** Phases 0, 1. **Unblocks:** exercised by Phase 4 backends.

### Phase 3 — Expanded auth

- gsi/x509 proxy certificates (the largest item), sss, bearer/scitokens, and S3
  credentials. Each is an additional `auth.Auther` under `xrdproto/auth/`,
  enabled by Phase 0 TLS.

**Depends on:** Phase 0 (TLS). **Parallelizable with:** Phase 4.

### Phase 4 — Alternative protocols (full parity)

- HTTP/HTTPS backend: ranged GET, chunked/resumable PUT, implementing the Phase 0
  `Backend` interface.
- S3 backend: signed requests (auth uses Phase 3 S3 credentials).
- WebDAV backend: PROPFIND listing plus GET/PUT.
- Because these sit behind the `Backend` interface, the copy engine and CLIs work
  over them without modification.

**Depends on:** Phases 0, 2, and (for S3/WebDAV auth) Phase 3.
**Parallelizable with:** Phase 3.

## 5. Sequencing

```
Phase 0 ──► Phase 1 ──► Phase 2 ──┐
   │                              ├──► Phase 4 (HTTP/S3/WebDAV)
   └────────────► Phase 3 ────────┘
```

`0 → 1 → 2` delivers a fully capable native client quickly. Phase 3 (auth) and
Phase 4 (alt protocols) both build on Phase 0's TLS + VFS groundwork and may then
proceed in parallel; Phase 4's S3/WebDAV auth consumes Phase 3 credentials.

## 6. Testing Strategy — Dual-Oracle Parity

Correctness is verified against **two independent reference oracles**, and a
feature is not "done" until it agrees with both:

1. **The official XRootD implementation** — the upstream C++ `xrootd` server (and
   its `xrdcp`/`xrdfs` clients) define the canonical wire protocol.
2. **The `libxrdc` C client** — the reference for client-side behavior, edge
   cases, and the VFS/copy/auth semantics this port mirrors.

Layered test approach:

- **Unit / mock layer.** Extend go-hep's existing mock-server harness
  (`*_mock_test.go`) for request/response coverage of each new request kind,
  including malformed-frame and error-path cases. Golden byte-level tests assert
  exact on-the-wire marshaling of every new request/response struct.
- **Interop layer (official XRootD).** Following the existing
  `cxx_server_test.go` / `go_server_test.go` patterns, run the go-hep client
  against a real upstream XRootD server to validate TLS negotiation,
  pgread/pgwrite CRC framing, fattr, checksums, and TPC. Skip cleanly (not fail)
  when the server binary is unavailable, but run in CI where it is present.
- **Cross-client parity (libxrdc).** For operations both clients perform (copy,
  checksum, pgread/pgwrite, recursive listing), assert byte-for-byte identical
  results — e.g. a file copied by go-hep and by `xrdcp`/`libxrdc` must have
  identical bytes and matching server-reported checksums; a `pgwrite` from go-hep
  must be readable by the C client and vice versa.
- **Alt-protocol layer (Phase 4).** Test HTTP/S3/WebDAV backends against local
  test servers (an httptest server, a MinIO-style S3 endpoint) plus a real server
  interop check where available.

Each phase's plan defines concrete acceptance criteria; a phase is "done" only
when its interop and cross-client parity tests pass against real servers, not
just the mock.

## 6a. Code Quality Bar

Every phase's implementation must meet these standards (enforced in review and,
where possible, in CI):

- **Formatting:** `gofmt`/`goimports` clean; no lint findings from `go vet` and
  the repo's existing linters. Code reads as senior-engineer output.
- **Documentation:** Every exported identifier has a complete doc comment
  following Go conventions (starting with the identifier name). Package-level doc
  comments explain purpose and usage. Non-obvious protocol constraints (e.g. why
  a CRC is per-page, why a field is big-endian) are documented at the point they
  matter — comments state constraints, not narration.
- **Idiom & structure:** Idiomatic Go — `context.Context` plumbed through all I/O,
  errors wrapped with `%w`, no naked returns in long functions, table-driven
  tests, small single-responsibility files consistent with the existing
  `xrdproto/*` layout. Follow existing go-hep patterns strictly rather than
  introducing new ones.
- **API design:** New public surface mirrors existing go-hep `xrootd` conventions
  (e.g. `MarshalXrd`/`UnmarshalXrd`, `Auther`) so the package stays cohesive.

## 7. Deliverables per Phase

Each phase produces: a dedicated spec, an implementation plan, the Go code under
`xrootd/` (new `xrdproto/*` subpackages and/or backend packages), tests (mock +
interop), and CLI updates where applicable. This document is the umbrella
roadmap; it is not itself an implementation plan.
