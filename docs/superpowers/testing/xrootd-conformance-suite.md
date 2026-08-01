# The XRootD conformance suite: what each file is for

Every suite below runs offline and is part of the default `go test ./xrootd/...`.
None of them needs a real XRootD server; the ones that need a *server* start one
in-process. The design rules behind them are in
`docs/superpowers/xrootd-client-lessons-learned.md` §9.

Together they hold `./xrootd/...` at **91.0% statement coverage**, measured the
only way that is meaningful across a tree of packages that test each other:

```
go test ./xrootd/... -coverpkg=./xrootd/... -coverprofile=cov.out -count=1 -timeout 550s
go tool cover -func=cov.out | tail -1
```

Without `-coverpkg` each package is credited only for the code it exercises in
itself, which understates a suite whose whole design is to drive one package
through another. The largest remaining gap is `xrdproto/auth/krb5`, which needs
a live KDC; the rest is scattered single-statement error branches.

## Wire decoding

| File | Covers |
|------|--------|
| `xrootd/internal/xrdenc/xrdenc_test.go` | The single decode primitive: round trips pinned byte for byte, every read method past the end of the buffer, zero values on failure, the sticky `ErrShortBuffer`, the length-prefixed reader against negative and huge sizes. 100% statement coverage. |
| `xrootd/xrdproto/decode_conformance_test.go` | A table of 58 wire structs — every request, response, framing struct and `xrdfs` value type. Drives every prefix of every fixture, 72 body lengths × 5 fill patterns, and negative / oversized values into 9 length-prefixed fields. Panics are caught and reported as failures; round trips are checked in the same table. |
| `xrootd/xrdproto/frame_conformance_test.go` | Request/response framing: header sizes, `dlen` agreement, the `kXR_writev` exception where segment data sits past the declared payload. Each case also pins the request code, and a completeness test fails when a request package has no case. |
| `xrootd/xrdproto/conformance_constants_test.go` | The numbers themselves, transcribed from the specification rather than read back from the code: response statuses and `kXR_asyncresp`, server error codes, header lengths, the 20-byte handshake byte for byte, the POSIX open modes, the open options, the stat flags, the query codes, the dirlist options, the fattr subcodes, the protocol role flags and the security levels. |
| `xrootd/xrdproto/conformance_errors_test.go` | The error vocabulary: all 34 `kXR_*` codes (3000–3033) against their specified values and names, transcribed independently, with a completeness check so a code added without a specified value fails. Then what a caller can decide from one — which codes mean `fs.ErrNotExist`, `fs.ErrExist`, `fs.ErrPermission`, `fs.ErrInvalid`, and, listed just as explicitly, which must *not*: a checksum mismatch is not a missing file and a full disk is not a bad argument. Plus an unknown code from a newer server reported as it arrived, the mapping surviving `%w` and `errors.Join`, and a decoded error response carrying both code and message. |
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
| `xrootd/conformance_errors_test.go` | What a failure looks like by the time it reaches a caller. Every namespace call at a path that is not there must come back as `fs.ErrNotExist` *and* still yield the `kXR_NotFound` through `errors.As` — one `fmt.Errorf` without `%w` anywhere on the way up turns both into a silent false. Plus the existing-path half, the negative cases (a listing of a plain file is not a missing path), `kXR_statx` reporting absence as a flag rather than an error, and the cross-transport property: the same question asked over `root://` and over `https://` classifies identically. |
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
| `xrootd/cmd/xrd-ls/main_test.go` | The listing command offline: file, directory, `-l`, `-R`, and the three ways it can fail. |
| `xrootd/cmd/xrd-cp/main_test.go` | The copy command offline: a file, a tree with and without `-r`, and failures. (The pre-existing benchmarks and `TestXrdCp` still use public remote servers.) |
| `xrootd/xrdio/conformance_openfrom_test.go` | `OpenFrom`, which borrows a filesystem handle instead of dialling one: reading through it, the borrowed session surviving the file's `Close`, a missing file reported at open, and `Write`/`WriteAt` refused on the read-only handle it returns. |
| `xrootd/xrdcopy/conformance_identity_test.go` | Who the copy logs in as (option, then URL, then `$USER`, then `nobody`) and when it declares a transfer corrupt: silent for a server with no checksum, a refused query or an algorithm this client cannot compute, and failing only on a real digest mismatch. |
| `xrootd/xrdfs/conformance_compression_test.go` | `FileCompression` on the wire: eight bytes, big-endian page size then a fixed four-byte algorithm name, and a short buffer reported rather than half-decoded. It shares the `kXR_open` response area with the file handle. |
| `xrootd/xrdsum/conformance_test.go` | That `Supported()` and `Sum()` agree in both directions, and that digests are fixed-width lower-case hex — XRootD compares checksums as strings, so a dropped leading zero fails to match the server that produced it. |
| `xrootd/conformance_client_options_test.go` | The client's security posture before it connects: the default providers registered under their protocol names, `WithAuth` adding and replacing by name, `WithTLS`/`WithInsecureTLS`/`WithTLSConfig` composing, and the effective TLS config naming the dialled host while leaving the caller's own config untouched. |
| `xrootd/xrdcopy/conformance_verify_test.go` | The parts of checksum verification the identity suite does not reach: every algorithm `xrdsum.Supported()` claims is actually compared and actually rejects a wrong digest of the same width; a local file that cannot be read is an error rather than a pass; and the server query is *bounded* — the copy imposes a deadline when the caller gave none, and leaves the caller's own deadline exactly as it found it. An unbounded checksum query hangs a copy that has already transferred every byte. |
| `xrootd/xrdcopy/conformance_failure_test.go` | What a refused copy says and what it leaves behind. Seven refusals — a missing remote source, a directory without `Recursive` at either end, a local-to-local directory copy, an unreachable source server and an unreachable destination server — each naming both the end that failed and the operation. Plus: a failed copy creates no destination it never opened, an unparseable URL is rejected before any connection is made (as source *and* as destination), and a recursive copy of an empty tree succeeds rather than reporting nothing to do as an error. |
| `xrootd/xrdcopy/conformance_upload_test.go` | The upload direction of resume, which is the half that is easy to get subtly wrong: the offset the client seeks to locally and the offset it writes to remotely have to agree, and a resume that seeks the source but writes from zero produces a file of exactly the right length holding the tail twice — which no size check catches. Also: a complete upload is not re-sent (checked on the modification time, so a retry loop cannot become an infinite transfer), a copy without `Resume` replaces a longer stale destination outright, an uploaded tree keeps its shape including an empty file three levels down, and an unreadable source leaves nothing at the far end. |

## Security providers

An authentication provider fails *late*: a credential built from the wrong key,
or carrying a truncated login name, is a well-formed message that the server
rejects as an authorization error with nothing pointing back at the keytab or
the proxy file. These suites check the assembly, not just the pieces.

| File | Covers |
|------|--------|
| `xrootd/xrdproto/auth/gsi/conformance_handshake_test.go` | The unsigned-DH GSI handshake verified from the *server's* side: the test derives the session key from what the client actually sent, decrypts the response, and checks the proof of possession against the certificate the client handed over. A per-piece unit test passes on a client that builds every bucket correctly and assembles them wrongly; this one does not. Plus the certificate request echoing the server's `c:`/`ca:` choices, a provider with no proxy declining to start a handshake at all, and six challenges this client must refuse — truncated, a proxy request, the wrong step, no public key, a malformed key, and a signed-DH challenge it cannot answer. |
| `xrootd/xrdproto/auth/krb5/conformance_test.go` | Where the ticket cache is found — `$KRB5CCNAME` with and without the `FILE:` prefix, and the conventional `krb5cc_<uid>` in the temporary directory when the environment says nothing — the provider naming itself the way the server advertises it, a handshake with no service name refused rather than guessed at, and a cache that is not there reported as a failure rather than silently treated as an empty one. The handshake itself needs a live KDC and is not covered offline. |
| `xrootd/xrdproto/auth/sss/conformance_test.go` | Where the keytab is found and what the credential says. Both environment spellings are honoured (`XrdSecSSSKT` and `XrdSecsssKT`, with the documented one winning when both are set), a stale variable pointing at a removed keytab is skipped rather than fatal, and `~/.xrd/sss.keytab` is the last resort — but a keytab that *exists* and is malformed is an error, because falling through would hide a configuration mistake behind whichever credential happened to work. Then the credential itself, read back the way a server reads it: the key it names, the login name it carries — `xrd` when the client offers none, cut to 63 bytes when it is longer than the field — a fresh nonce every time, and the expiry boundary that decides whether a client rotating on the hour sends a key the server has just dropped. |

## Command-line tools

The three commands are the only client surface a user meets directly, and their
contract is the shell's: what goes on stdout, what goes on stderr, and the exit
status. Each `main` is now `os.Exit(run(os.Stdout, os.Stderr, os.Args[1:]))`
over a `run(stdout, stderr io.Writer, args []string) int` — the pattern already
used by `groot/cmd/root-ls` — so the whole command is reachable from a test
without a subprocess, a pipe or a signal.

| File | Covers |
|------|--------|
| `xrootd/cmd/xrd-ls/conformance_cli_test.go` | Listing to a caller-supplied writer, the flags reaching the listing (`-l`, `-R` and both together — the long recursive format prints a directory heading and then base names, which is easy to assert wrongly), several operands listed in order and separated, five ways to fail with a non-zero status and a message on stderr, the usage not being an error, and the first failing operand stopping the run. |
| `xrootd/cmd/xrd-cp/conformance_cli_test.go` | The shell contract of a copy: success is *quiet* (anything on stdout would corrupt `xrd-cp src - \| ...`), `-` writes the file to stdout, the last operand is the destination, `.` means the working directory, `-v` reports the byte count on stderr where it cannot be mistaken for data, `-r` is opt-in and refused with a message naming the missing flag, six failures exit non-zero, and the first failing source stops the run. |
| `xrootd/cmd/xrd-srv/conformance_test.go` | The serving command: four bad argument sets refused *before* a port is bound — validating after binding leaves a socket held open on a run that was never going to serve anything — and then the real thing, a `serve` split out from `run` so the loop can be driven with a synthetic quit channel: a served directory read over `root://` by a real client, an interrupt shutting the server down rather than dropping connections, and a listener that dies underneath the loop reported as a failure rather than a clean exit. |

## HTTP data access

The HTTP transports are the same client surface reached over a different
protocol, so they get the same treatment: an in-memory WebDAV server
(`xrdhttp/fs_test.go`) plays the oracle, and the tests assert on the requests
that arrived, not only on the values that came back.

| File | Covers |
|------|--------|
| `xrootd/xrdhttp/fs_test.go` | The `xrdfs.FileSystem` view over WebDAV: the write/read round trip, an update starting from the existing content, the namespace surface, statx and missing files, truncate, and the operations HTTP cannot support — plus the in-memory DAV server the rest of the package is tested against. |
| `xrootd/xrdhttp/webdav_test.go` | The PROPFIND parser, and `hrefPath` in `conformance_file_test.go`: servers answer with an absolute URL or a bare path, and a listing that fails to reduce them lists the collection as one of its own members. |
| `xrootd/xrdhttp/conformance_file_test.go` | The file surface and the client options. A positional read is a ranged GET and not a download; a short read reports `io.EOF`; a buffered write reads back without touching the network; `Sync` uploads and a following `Close` does not upload again; `Truncate` grows and shrinks; a read-only file refuses writes; `Stat` refreshes `Info`; `CloseVerify` can fail. `RemoveDir` refuses a non-empty collection where `RemoveAll` does not. `WithTimeout` bounds a stalled server, and the TLS options decide who is trusted — including that the default trusts nobody it has no reason to. |
| `xrootd/xrdhttp/mtls_test.go` | The x509 access path: a client certificate presented to a server that demands one, and the subject the server sees. |
| `xrootd/xrdhttp/auth_test.go` | Bearer tokens, including the refusal to send one to a cleartext endpoint. |
| `xrootd/xrdhttp/tpc_test.go` | Third-party copy over HTTP: the push handshake, the `TransferHeader` credentials, and a failure announced in the body of a 2xx response. |
| `xrootd/xrdhttp/conformance_transfer_test.go` | What the client does with the answers an endpoint gives it. A server that ignores `Range` answers 200 with the whole object, so the bytes are right but start at zero — the client skips forward rather than returning the head of the file for every offset; a `Content-Range` on a 200 means the range was honoured after all. Plus a zero-length read making no request, a known size declared as `Content-Length` against an unknown one streaming chunked, a redirect to the data node followed and a redirect loop bounded, a bearer token not following a redirect to another host, and the five error statuses reported rather than returned as data — with 404 an answer for HEAD and DELETE and a failure for everything else. |
| `xrootd/xrdhttp/conformance_target_test.go` | Which URL each operation is addressed to. Nine awkward names — spaces, `+`, `%`, `?`, `#`, `&`, `:`, unicode — through HEAD, GET, ranged GET, PUT, DELETE, PROPFIND, MKCOL and MOVE, asserted on the path the server decoded and on the escaping in the request line, with MOVE's `Destination` parsed back as a URL. Plus resolution against a base URL with a path prefix, and the listing side: an href is a URI reference, so it is decoded before it is compared to the requested path or handed back as a name. |
| `xrootd/xrdhttp/conformance_hostile_test.go` | The PROPFIND parser against a server that answers badly: thirteen malformed documents, an entity-expansion bomb, an external entity naming a local file and a URL, an endless well-formed body, and a parseable document under the wrong status. Nothing may panic, hang, allocate without bound or fetch what the document points at. |
| `xrootd/xrdhttp/conformance_errors_test.go` | What a refusal means. Every status the client can meet, checked against *all four* of `fs.ErrNotExist`, `fs.ErrExist`, `fs.ErrPermission` and `ErrNotSupported` rather than only the one it should be — including the statuses that mean none of them, where a full disk or an overloaded gateway must not be read as a statement about the file. The verb is part of the mapping: 405 on MKCOL is "the collection is already there" and on any other verb is "this server does not speak WebDAV". Then the same through the real client: 404 on every namespace call, 401/403 not readable as a missing path, `MkdirAll` continuing past a collection that exists and stopping on a missing parent, and the status itself recoverable with `errors.As` after the layers above have wrapped it. |
| `xrootd/xrdhttp/conformance_transport_test.go` | The one error that arrives with no status line to map. Every other HTTP suite puts a server behind the client; this one removes it. A transport failure is the most easily dropped error there is — a `Stat` returning a zero `FileInfo` and a nil error on a dead endpoint reads exactly like a file that is not there, and a `Create` returning nil reads like an upload that worked. Eight client calls and ten `xrdfs.FileSystem` calls against an endpoint that was listening and is not any more, each having to fail and name what it was working on; a failed stat not reporting the file as present; a context cancelled before the call is made; `Statx` answering per path (offline flags) rather than failing the batch and losing the answers for the paths that did work; and `root://`, `file://` and scheme-less URLs refused at `Dial` rather than dialled as if they were HTTP. |
| `xrootd/xrds3/conformance_test.go` | The part of a SigV4 signature nobody can see: the canonical query string is sorted by name, then by value, and percent-encoded — it is hashed, not sent, so a disagreement surfaces only as a signature mismatch. Plus `WithRegion` reaching the credential scope and `WithHTTPClient` being what the request actually goes through. |
| `xrootd/xrds3/conformance_errors_test.go` | What an S3 endpoint's answers mean. Five verbs against an unexpected 500, each failure naming the verb, the key and the status; a missing object being absence rather than failure; a delete of something already gone succeeding on 200, 204 *and* 404, because endpoints disagree about which one they send; all four success statuses accepted on PUT; a read past the end of an object reported as EOF whether the endpoint says 416 or returns a short 206; a whole-object range accepted from an endpoint that ignores `Range` entirely; an empty read touching the network not at all; an unreachable endpoint naming the verb it was attempting; a PUT of unknown length still signed, as `UNSIGNED-PAYLOAD`; and the key being path-style whatever the caller writes. |

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
- A new client option is tested for what it *changes*, at the place the change
  is used — the effective TLS config, the provider map, the request that
  arrived — not for the field it happens to set.
- Keep the round-trip count in mind: sweep realistic sizes over a large fixture
  and pathological ones over a small fixture.
- An HTTP behaviour is tested against more than one *shape* of server. The
  endpoints in this ecosystem disagree — about ranges, about hrefs, about which
  status carries a failure — and a test written against one cooperative handler
  passes on a client that only works with that handler.
- Anything parsed from the network gets a bound and a hostile case. The size
  limit belongs to the package, not the test; where a test has to lower it to
  stay fast, it also asserts the shipped value is still large enough for real
  data.
- A new error classification is tested against every value it could answer, not
  only the one it should. A mapping is useful because of the questions it says
  no to; a test that only asserts the yes passes on an error that claims to be
  everything.
- A command is written as `run(stdout, stderr io.Writer, args []string) int` and
  tested through that, not through a subprocess. The `flag.FlagSet` is built
  *inside* `run` rather than at package level, so a second call in the same test
  binary does not inherit the first one's flag values. Work that only ends on a
  signal — `xrd-srv`'s serving loop — is split into its own function taking a
  quit channel, so a test can stop it without signalling the test process.
- A credential is verified by *using* it, not by inspecting it. Build the
  provider's output, then take the server's side: derive the key from what was
  actually sent, decrypt, check the signature against the certificate that
  arrived. Per-piece assertions pass on a provider that gets every field right
  and assembles them wrongly, which is the failure that actually happens.
- Before writing a new file, `grep '^func Test'` the target package. Three of
  the suites above started as duplicates of coverage that already existed one
  file over; what survived was only the part that was genuinely new.
- A behaviour both transports have is tested on both, in one table, with the
  answers compared to each other. `xrootd/conformance_errors_test.go` does this
  by running the same `xrdfs.FileSystem` calls over the native mock and over an
  HTTP server, and comparing the classification rather than the message —
  agreement between two unrelated wire formats does not happen by itself.
