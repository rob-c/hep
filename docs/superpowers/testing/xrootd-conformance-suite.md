# The XRootD conformance suite: what each file is for

Every suite below runs offline and is part of the default `go test ./xrootd/...`.
None of them needs a real XRootD server; the ones that need a *server* start one
in-process. The design rules behind them are in
`docs/superpowers/xrootd-client-lessons-learned.md` §9.

## Wire decoding

| File | Covers |
|------|--------|
| `xrootd/internal/xrdenc/xrdenc_test.go` | The single decode primitive: round trips pinned byte for byte, every read method past the end of the buffer, zero values on failure, the sticky `ErrShortBuffer`, the length-prefixed reader against negative and huge sizes. 100% statement coverage. |
| `xrootd/xrdproto/decode_conformance_test.go` | A table of 58 wire structs — every request, response, framing struct and `xrdfs` value type. Drives every prefix of every fixture, 72 body lengths × 5 fill patterns, and negative / oversized values into 9 length-prefixed fields. Panics are caught and reported as failures; round trips are checked in the same table. |
| `xrootd/xrdproto/frame_conformance_test.go` | Request/response framing: header sizes, `dlen` agreement, the `kXR_writev` exception where segment data sits past the declared payload. Each case also pins the request code, and a completeness test fails when a request package has no case. |
| `xrootd/xrdproto/conformance_constants_test.go` | The numbers themselves, transcribed from the specification rather than read back from the code: response statuses and `kXR_asyncresp`, server error codes, header lengths, the 20-byte handshake byte for byte, the POSIX open modes, the open options, the stat flags, the query codes, the dirlist options, the fattr subcodes, the protocol role flags and the security levels. |
| `xrootd/xrdproto/protocol/conformance_tls_test.go` | The TLS negotiation flags: each `kXR_tls*` bit against its specified value, and one bit at a time through every accessor so none of them reads its neighbour. Plus the `kXR_protocol` response round trip with its security trailer and overrides. |

The point of the decode table is that a decoder is *unsafe by default*: it reads
bytes a server chose. A new `UnmarshalXrd` should be added to the table in the
same commit.

## Client behaviour against a strict server

| File | Covers |
|------|--------|
| `xrootd/conformance_test.go` | Happy paths for read, write, pgread, pgwrite and close-verify, asserted on the bytes the server holds rather than on a status. |
| `xrootd/conformance_vector_test.go` | `kXR_readv` / `kXR_writev`: ranges, reassembly across frames, refusal of a reply that does not account for every segment. |
| `xrootd/conformance_scale_test.go` | Offset/length sweeps, a megabyte in one request, concurrent requests on one connection, out-of-order replies, stream-id reuse. |
| `xrootd/conformance_failclosed_test.go` | The fail-closed half: over-answered reads, oversized bodies refused before allocation, stalled servers bounded by the context, corrupt pages, the pgwrite retry budget, sync/close errors surfacing. |
| `xrootd/conformance_fs_test.go` | The namespace surface — dirlist, open, stat, statx, mkdir, mv, chmod, rm, rmdir, truncate, fattr, query, ping — against the strict namespace server in `conformance_fs_server_test.go`. |
| `xrootd/conformance_fs_failclosed_test.go` | Malformed but well-framed namespace replies, server errors on every operation, `kXR_wait` retries, unsolicited frames. |
| `xrootd/conformance_hostile_test.go` | A server that answers every request with garbage: 14 client calls × 14 body lengths × 4 fill patterns × 3 statuses, plus short redirects and an unbounded `kXR_wait`. Nothing may panic, hang or return data. |
| `xrootd/conformance_opaque_test.go` | Opaque data (CGI) on every path the client sends: open, the whole namespace surface one request at a time, both halves of a rename, the four fattr operations, `MkdirAll` on every level, `RemoveAll` on every child, URLs. Asserts on what the server was addressed with, name and CGI separately, so a client that leaves the token on the name or invents a bare `?` fails. |
| `xrootd/conformance_resilience_test.go` | A server that hangs up, goes silent or half-writes a reply, in the middle of a session and with requests in flight: every waiter must get an error naming the connection, later requests must be refused, an open file must fail with its connection, and a silent server must be bounded by the caller's context. |
| `xrootd/conformance_redirect_test.go` | Two real TCP servers: a redirector and a target that records what arrived. Covers following a redirect, opaque data added to the path, the caller's own opaque data surviving, the login token, session reuse, a dead target, and a redirect cycle ending on the client's limit. |

The strict servers are oracles, not mocks: they reject a non-zero reserved byte,
an unknown file handle, a `dlen` that disagrees with the payload. Two tests
(`TestConformance_ServerCatchesBreaches`, and the breach assertions in the
namespace suite) check the oracle itself, so `srv.check(t)` cannot pass vacuously.

## The layers above the protocol

| File | Covers |
|------|--------|
| `xrootd/xrdio/xrdio_conformance_test.go` | The `io` contracts `xrdio.File` claims: `Read` to EOF, `ReadAt` not moving the position, `Seek` in all three whences (including `SeekEnd`, whose offset counts *forward* from the end), rejection of bad arguments, `fs.File`, open failures. Runs against an in-process server. |
| `xrootd/xrdcopy/xrdcopy_conformance_test.go` | The copy engine end to end in both directions: chunk sizes that divide the file and that do not, empty files, resume from five different partial lengths, overwrite without resume, recursive trees, failures, context cancellation. Uploads are verified on the server's disk, not read back through the client. |
| `xrootd/fshandler_test.go` | The in-process server's handler, including `TestHandler_OpenGrantsWriteAccess`: an option set that creates, truncates or appends must yield a writable descriptor even when `kXR_open_updt` is absent. |
| `xrootd/cmd/xrd-ls/main_test.go` | The listing command offline: file, directory, `-l`, `-R`, and the three ways it can fail. Captures `os.Stdout` through a pipe. |
| `xrootd/cmd/xrd-cp/main_test.go` | The copy command offline: a file, a tree with and without `-r`, and failures. (The pre-existing benchmarks and `TestXrdCp` still use public remote servers.) |

## What is still network-bound

`xrootd/file_test.go` and `xrootd/filesystem_test.go` drive `testClientAddrs`,
which `cxx_server_test.go` points at a public C++ server. They are the parity
oracle against a real implementation and are worth keeping, but they are not
the offline coverage for those surfaces — the conformance suites above are.
`xrootd/it_test.go` and `it_pki_test.go` launch a real `xrootd` and are gated by
`XROOTD_IT=1`.

## Adding to the suite

- A new wire struct goes in the decode table.
- A new request/response pair gets a happy path *and* a fail-closed case: what
  does the client do when the server answers it with the wrong shape?
- A new client behaviour that involves a second connection needs a second real
  server; `net.Pipe` harnesses cannot dial.
- A new constant is transcribed from the specification into
  `conformance_constants_test.go`, not copied from the declaration it checks.
- Keep the round-trip count in mind: sweep realistic sizes over a large fixture
  and pathological ones over a small fixture.
