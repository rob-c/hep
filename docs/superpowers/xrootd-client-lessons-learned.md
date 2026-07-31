# Lessons learned: building a pure-Go XRootD client to parity

This captures the hard-won lessons from implementing the go-hep XRootD client and
its CLI/library tooling to feature-parity with the reference C client
(`nginx-xrootd/client`, "libxrdc") and interoperating with the official XRootD
C++ server. Each lesson names where the corresponding logic lives so it can be
checked against `nginx-xrootd` (and, where the behaviour is server-side, against
the stock XRootD C++ sources).

Scope of the work these lessons come from: TLS + a protocol-neutral VFS layer;
paged I/O (`kXR_pgread`/`kXR_pgwrite`), extended attributes (`kXR_fattr`) and
checksums; expanded auth (`ztn` tokens, `sss`, and full GSI/X.509); HTTP/HTTPS
(+x509), WebDAV and S3 backends; a copy engine with recursive, resumable and
third-party copy; and a real-XRootD integration harness driven by a Globus grid
PKI.

---

## 1. Meta-lessons (the ones that mattered most)

### 1.1 The server source is the only reliable spec; the wire is not enough

Repeatedly, matching the *observable* request byte-for-byte was **not** sufficient
to reproduce a behaviour, because the behaviour depended on things not visible in
a single request: a later trigger, an async reply, or a capability negotiated at
login. The definitive answers came from reading the server code, not from
`tcpdump` or protocol traces.

- The clearest case: native TPC. The go-hep destination open matched stock
  `xrdcp` byte-for-byte (opaque, flags, mode) and still produced an empty file.
  The answer was only in the C++: the transfer is triggered by **sync**, and the
  completion arrives as an **async `kXR_attn`** reply — neither visible in the
  open request. See §6.
- **Check against nginx-xrootd:** the project's own clean-room notes
  (`docs/refactor/phase-37-clean-room-log.md`, the "wire facts from src/protocol
  headers only" comments) reflect the same discipline — the wire constants are
  pinned in shared headers (`src/protocol/*.h`), not re-derived per side.

**Takeaway to verify:** anywhere libxrdc's behaviour depends on a *server* state
machine (TPC rendezvous, GSI rounds, deferred replies), confirm the client was
written against the server's decision logic, not just a captured exchange.

### 1.2 Two independent oracles catch different bugs

Every feature was validated against **both** the official XRootD server/tools
*and* the reference C client. The official server catches client-side wire bugs;
the C client catches semantic divergences. Neither alone is enough.

- **Check against nginx-xrootd:** `tests/official_interop_lib.py` encodes exactly
  this quadrant model (our client → stock server, stock client → our server,
  etc.). The go-hep effort reached the same conclusion independently.

### 1.3 "Byte-identical request" ≠ "same behaviour"

Behaviour can hinge on: (a) login-time capability negotiation, (b) a subsequent
operation that triggers the real work, (c) async/out-of-band replies. A client
that only replays a captured request will silently misbehave on all three.

---

## 2. Bootstrap, TLS and connection setup

### 2.1 TLS must be negotiated *after* `kXR_protocol` but *before* `kXR_login`

The whole session bootstrap had to be reordered so `kXR_protocol` (advertising
`kXR_ableTLS`/`kXR_wantTLS`) runs immediately after the initial handshake and
before login, and the optional TLS upgrade slots in between — otherwise
credentials could travel in the clear. This forced a **synchronous** bootstrap
(handshake + protocol read directly off the socket) *before* the async read-loop
goroutine takes ownership of the connection.

- TLS decision, matching libxrdc: upgrade when `kXR_gotoTLS` is set *or* the
  client wanted TLS and the server has it (`kXR_haveTLS`); never silently
  downgrade.
- **Check against nginx-xrootd:** `client/lib/conn.c` (the `goto_tls`/`have_tls`
  decision, `do_handshake` sending handshake + `kXR_protocol` together) and
  `client/lib/tls.c` (upgrade after the protocol reply, before login).

### 2.2 The `kXR_protocol` response flags overflow a signed 32-bit field

`kXR_haveTLS = 0x80000000` sits in the sign bit. Accessors must test the flags
through `uint32`, not the signed on-the-wire `int32`, or the check silently
fails.

- **Check against nginx-xrootd:** wherever the server/client reads
  `ServerProtocolBody.flags` — confirm unsigned handling of the top bits.

---

## 3. Response framing: the traps

### 3.1 `kXR_status` (4007) carries a data length *outside* the response header

For `kXR_pgread`/`kXR_pgwrite`, the reply is a `kXR_status` frame whose
`ServerResponseBody_Status` has its **own** `dlen`, and the page data that follows
is counted by *that* body field — **not** by the response header's `dlen`. The
read loop must drain those trailing bytes explicitly, or the stream desyncs and
every subsequent response is garbage.

- The status body's CRC-32C covers everything after the CRC field (body remainder
  + info tail).
- **Check against nginx-xrootd:** `client/lib/ops_file_pg.c` — `read_status_frame`
  reads the 24-byte body then the separate page-data length; it documents this as
  "trailing data lives OUTSIDE the header dlen". This is the single easiest place
  to desync a hand-written client.

### 3.2 `kXR_waitresp` (4006) and `kXR_attn`/`kXR_asynresp` are real reply paths

Deferred replies (TPC completion, any `SFS_STARTED` callback) do **not** arrive as
a normal response on the request's stream:

- `kXR_waitresp` is a placeholder — keep the stream open and wait for the real
  reply.
- The real reply may arrive **asynchronously** as `kXR_attn` (status 4001) with
  action `kXR_asynresp` (5008), wrapping the actual `{streamid, status, dlen,
  data}` for another stream. The client must unwrap it and dispatch to that
  stream.
- **Check against XRootD C++:** `XrdXrootd/XrdXrootdResponse.cc` (`Send` for
  `kXR_attn`/`kXR_asynresp`, the `atnHdr`/`act`/`rsvd`/`theHdr` layout) and
  `XrdXrootd/XrdXrootdXeq.cc` (`rc == SFS_STARTED` → `Response.Send(kXR_waitresp,
  …)`). **Check against nginx-xrootd:** confirm the server emits the same
  `kXR_attn` wrapper for deferred completions, and that libxrdc's async manager
  (`client/lib/aio*.c`) unwraps it.

### 3.3 Unknown/late frames must be ignored, not fatal

A frame for a stream with no registered waiter is provably unsolicited (a request
always claims its stream *before* sending), so it must be dropped — not panic the
session. This surfaced only once timed-out requests and deferred replies were in
play.

### 3.4 A response length is untrusted input, and a per-frame cap is not enough

Every length on the wire is chosen by the peer and is read *before* any of the
body is validated, so each one is an allocation the server gets to size. Two
distinct bounds are needed, and neither substitutes for the other:

- **Per frame.** The response header's `dlen` (and, for `kXR_status`, the body's
  own trailing `dlen`) is refused above `xrdproto.MaxResponseLength` (64 MiB)
  rather than passed to `make`. A negative `dlen` must be rejected explicitly —
  the field is a *signed* 32-bit integer, so a peer can announce one.
- **Per request.** A frame cap alone still lets a server answer with an endless
  stream of individually-small `kXR_oksofar` frames; the accumulating loop grows
  the heap until the process dies, and it never terminates. So a request that
  knows how much it asked for states it (`xrdproto.ResponseLimiter`) and the
  session stops accumulating past that: a read cannot legitimately return more
  than its own length, and a `pgread` cannot exceed its length plus one CRC and
  one status header per page.

The bound applies to data-bearing statuses only. An error body is a *message*,
and truncating one hides the very failure the caller needs to see.

- **Check against nginx-xrootd:** `client/lib/brix_ops.h` / `brix.h`
  (`DLEN_MAX`, `VEC_MAXSEGS`, `VEC_MAXBYTES`) — libxrdc applies the same bounds
  before touching the wire.

---

## 4. Paged I/O, fattr, checksums

### 4.1 pg page framing aligns to *file* offsets, not buffer offsets

Each page unit is `[crc32c BE u32][data ≤ 4096]`, and units align to 4 KiB
boundaries of the **file** offset — so the first unit of an unaligned request is
short (`4096 - off%4096`), and the last may be short too.

- **Check against nginx-xrootd:** `client/lib/ops_file_pg.c` (`decode_pages` /
  `xrdp_pg_decode`, file-offset alignment) and `src/protocol` page constants.

### 4.2 The checksum algorithm names and polynomials are exact

`crc64` means **CRC-64/XZ** (ECMA polynomial), not any other CRC-64; `crc32c` is
Castagnoli (RFC 7143), distinct from the IEEE CRC-32 used by SSS. Getting the
polynomial wrong yields a plausible-but-wrong digest that only fails on interop.

- **Check against nginx-xrootd:** `client/lib/checksum.c` (algo names) and
  `client/apps/xrdcrc64.c` ("CRC-64/XZ … canonical crc64").

### 4.3 `kXR_fattr` vectors are position- and endianness-specific

Request body is `path\0` + nvec + (set only) vvec, where an nvec entry is
`[u16 BE rc][name\0]` and a vvec entry is `[i32 BE vlen][value]`; responses lead
with `[u8 errcount][u8 numattr]`. List replies are NUL-separated names with **no**
count prefix.

- **Check against nginx-xrootd:** `client/lib/fattr.c` (the nvec/vvec builder and
  response decoder) — the header comment spells out the exact layout.

### 4.4 `kXR_pgwrite` reports corrupt pages; it does not reject them

The obvious reading of a checksum-error reply — "the write failed, return an
error" — is wrong, and wrong in the dangerous direction. The server **stores
every page it receives**, including the ones whose CRC-32C did not match, then
names them in a checksum-error (CSE) trailer. Those pages are corrupt *on disk*
until the client resends them. Treating the reply as a plain failure leaves the
file silently damaged even though the caller saw an error and may well retry the
whole write elsewhere.

The recovery flow is therefore part of the write, not an optional extra:

- The trailer is `cseCRC[4] dlFirst[2] dlLast[2]`, then one big-endian `int64`
  file offset per corrupt page. It arrives as the `kXR_status` body's trailing
  data, i.e. *after* the 8-byte offset info tail.
- Each named page is retransmitted **on its own** (sliced at file-offset page
  boundaries, so the first page of an unaligned request is short) with
  `kXR_pgRetry` set in the request flags.
- Retries are bounded (3). Corruption that survives three independent attempts
  is not a transient wire error, and retrying forever converts a broken link
  into a hang.
- A page offset outside the request is refused rather than sliced — otherwise a
  hostile server picks the client's memory to read.

Only once every page is accepted may the write return success; that is what
makes a successful `PgWriteAt` mean "intact on the server" rather than merely
"delivered".

- **Check against nginx-xrootd:** `client/lib/ops_file_pg.c`
  (`pgwrite_handle_cse`) and the `kXR_pgRetry` flag definition.

### 4.5 Vector I/O (`kXR_readv` / `kXR_writev`): the length field that is not in the frame

Scatter-gather I/O is what makes reading a scattered set of branches from a
remote file affordable — one round trip for a whole list of disjoint ranges
instead of one per range. It is implemented in `xrootd/xrdproto/readv` and
`xrootd/xrdproto/writev`, reached through the optional `xrdfs.VectorReader` and
`xrdfs.VectorWriter` interfaces. Both request kinds share a 16-byte descriptor,
`fhandle[4] | length[4] | offset[8]`, and both hide something in it.

- **`kXR_writev`'s `dlen` covers only the descriptor block.** The concatenated
  segment data streams *after* the frame, outside the length the header
  declares. Stock servers enforce `dlen % 16 == 0` and answer `kXR_ArgInvalid:
  "Write vector is invalid"` to a request that counts the data in, so the wrong
  framing fails loudly rather than corrupting a file — but only against a stock
  server. libxrdc counted the data inside `dlen` (which only its own server
  accepted) until this was confirmed against stock xrootd 5.8. A mock that
  reads `dlen` bytes and stops will happily accept either form, which is why
  the conformance server here reads the trailer off the connection itself and
  the `root-vector` integration leg runs against real xrootd.
- **This also puts `kXR_writev` out of reach of `kXR_sigver`.** A signature
  covers the request frame and its `dlen` bytes; a vector write's data is not
  in either. Signing one produces a hash the server cannot reproduce, so the
  request is deliberately absent from the signing requirements table.
- **A `kXR_readv` reply interleaves a 16-byte echo header with the data, per
  segment,** and the length in that header is what was *actually* read. The
  reply carries no request-side index, so a server that stops early simply
  sends fewer segments and every later segment shifts onto the wrong range: a
  reply that does not account for every requested segment — with exactly the
  bytes asked for, at the offset asked for — is a *stopped* transfer, not a
  short one, and must fail rather than hand back a prefix. The same reasoning
  rules out accepting a short segment in the middle.
- **The reply is a byte stream, not a sequence of segments.** An `OkSoFar`
  boundary can fall inside an echo header, so decoding frame by frame loses the
  segment that straddles one; the frames are concatenated first and walked
  after.
- **The bounds belong on this side of the wire.** Segment count and aggregate
  payload are checked before the request is assembled (`xrdproto.ValidateVector`,
  1024 segments and 256 MiB, the bounds libxrdc applies), and the reply carries
  a [`ResponseLimiter`](#34-a-response-length-is-untrusted-input-and-a-per-frame-cap-is-not-enough)
  bound of `16 × nsegs + Σ rlen`. The decode side needs the same bound for a
  different reason: a `writev` frame's declared lengths decide how much is read
  from the connection *next*, so they are bounded before anything is reserved
  for them.

- **Check against nginx-xrootd:** `src/protocols/root/protocol/readv_seg.h`
  (the pack/unpack pair) and the `readahead_list` / `write_list` structs in
  `src/protocols/root/protocol/wire_write_extended_requests.h`.

---

## 5. Authentication

### 5.1 Single-round vs multi-round is an interface decision, not a detail

`unix`/`host`/`krb5`/`ztn`/`sss` are single-round; GSI is multi-round via
`kXR_authmore` (4002). Modelling the multi-round case as an *optional* interface
(`Continuer`) kept the common providers simple while enabling GSI, and required
carrying `kXR_authmore` through the mux and read loop.

- **Check against nginx-xrootd:** the `sec_module` callback shape in
  `client/lib/sec/*.c` (`gsi_first`/`gsi_more` vs the single-shot providers).

### 5.2 `ztn` and `sss` wire facts are small but unforgiving

- `ztn`: payload is `"ztn\0"` + JWT; discovery trims **trailing** whitespace only
  (`rstrip`), not leading. Discovery order: `$BEARER_TOKEN`, `$BEARER_TOKEN_FILE`,
  `$XDG_RUNTIME_DIR/bt_u<uid>`, `/tmp/bt_u<uid>`.
- `sss`: a 16-byte outer header (`sss\0` ver spare kn `enc='0'` keyid-BE) + a
  Blowfish-CFB64-encrypted body of (cleartext + IEEE-CRC32-BE); cleartext is a
  40-byte data header (32 nonce + gen_time BE + `USEDATA` opt) + a NAME TLV. The
  IV is zero, padding off, key length variable; `gen_time = now - 1222183880`.
- **Check against nginx-xrootd:** `client/lib/sec/sec_token.c`,
  `client/lib/sec/sec_sss.c`, `src/protocol/sss.h` and `src/compat/sss_bf.c`
  (the exact byte offsets used here came from `xrootd_sss_build_credential`).

### 5.3 GSI: choose the unsigned-DH path and reproduce the primitives exactly

GSI is the largest single subsystem. The tractable route is to advertise a
pre-`DHsigned` version (< 10400) so the server uses the **unsigned-DH** path (a
plain `kXRS_puk` public blob, no RSA-signed DH params, zero IV). Then:

- Diffie-Hellman: **echo the server's PEM group parameters verbatim** and only
  extract p/g for the modexp; the DH public blob is PEM params + `---BPUB---` +
  **uppercase** hex of the public value + `---EPUB---`.
- Session key = the **first key_len bytes of the raw shared secret**
  (`dh_pad=0` → leading zeros stripped, i.e. `big.Int.Bytes()`); AES-128-CBC,
  zero IV, PKCS#7.
- Proof of possession = RSA **PKCS#1 v1.5 over the raw rtag** (no digest), i.e.
  `EVP_PKEY_sign` with no md → Go `SignPKCS1v15(…, crypto.Hash(0), rtag)`.
- The message is an XrdSutBuffer: `"gsi\0"` + step(BE) + buckets
  (`[type BE][len BE][data]`) + a `kXRS_none` terminator.
- **Check against nginx-xrootd:** `src/auth/gsi/gsi_core.c`
  (`xrootd_gsi_build_cert_response`), `gsi_dh.c`, `gsi_cipher.c`
  (`xrootd_gsi_cipher_session_key`, `..._encrypt`), `gsi_rsa.c`
  (`xrootd_gsi_rsa_sign_raw`), `gsi_buf.c` (the bucket codec),
  `src/protocol/gsi.h` (step/bucket constants), and `client/lib/sec/sec_gsi.c`
  (the round orchestration + `XRDC_GSI_VERSION` version override).

### 5.4 Not implemented (deliberate, and worth checking libxrdc's coverage)

Signed-DH and X.509 **delegation** (`kXGS_pxyreq`/`kXGC_sigpxy`) were left out; the
client forces unsigned-DH and rejects a delegation challenge. libxrdc *does*
implement delegation (`client/lib/sec/sec_gsi.c::gsi_sigpxy`,
`src/auth/gsi/delegation.c`) — a place where the C client is ahead.

---

## 6. Third-party copy (the hardest, and most instructive)

The full working native-TPC sequence, none of which is visible from the wire
alone:

1. **Placement** open on the source (`?tpc.stage=placement`), then close.
2. **Source coordinator** open (read; `tpc.dst=<host>&tpc.key=K&tpc.stage=copy`),
   **kept open** so the key registration stays live.
3. **Destination puller** open (write, mode 0644, flags `delete|open_updt|
   retstat|async`; opaque `oss.asize`/`tpc.dlg`/`tpc.dlgon=0`/`tpc.key`/`tpc.lfn`/
   `tpc.spr`/`tpc.src`/`tpc.stage=copy`/`tpc.tpr`). This only **sets up** the job.
4. **Two syncs on the destination** — the first *starts* the copy, the second
   *waits* for completion.
5. The completion is an **async `kXR_attn`/`kXR_asynresp`** reply (see §3.2).

Key gotchas:

- The opaque is **not** the libxrdc dialect. libxrdc's own remote→remote helper
  (`client/lib/copy_remote.c`, `copy_tpc`) targets the *nginx server's* TPC and
  uses `tpc.src=root://host//path` + a double-sync; the **stock** server wants
  `tpc.src=host:port` + `tpc.lfn=/path` + the `tpc.dlg`/`tpc.tpr` set. Match the
  server you're actually talking to.
- The **source coordinator open uses `tpc.dst` (dest host only)**, not `tpc.src`;
  using `tpc.src` there triggers `conflicting tpc cgi`
  (`XrdOfs/XrdOfsTPC.cc::Authorize`, "Origin: dst and key required but org may not
  be specified").
- Destination role requires `is_write && tpcKey`; source role requires
  `!is_write && tpcKey`. If the dest open isn't seen as write, it's mis-routed to
  the *source* path and silently creates an empty file.
- **Check against XRootD C++:** `XrdOfs/XrdOfs.cc` (open: `tpcKey && isRW` →
  `XrdOfsTPC::Validate`; `tpcKey && !isRW` → `Authorize`; `crMask`/`opMask`),
  `XrdOfs/XrdOfsTPC.cc::Validate`, `XrdOfs/XrdOfsTPCJob.cc::Sync` (the
  start-then-wait double-sync), `XrdOuc/XrdOucTPC.cc` (the `tpc.*` key names),
  `XrdXrootd/XrdXrootdXeq.cc::do_Open` (kXR flag → `SFS_O_*` translation; note
  `kXR_delete` **overwrites** `openopts` to `SFS_O_TRUNC`).
- **Check against nginx-xrootd:** `src/tpc/parse.c` (role detection by
  `tpc.src`/`tpc.dst`/`tpc.key`), `src/protocols/root/read/open_request.c` (the
  `is_write && tpc.has_src` branch), `src/tpc/key_registry.c`,
  and whether the nginx server also drives completion via `kXR_attn`.

---

## 7. Concurrency (the bugs that only show under load / deferral)

### 7.1 A blocking channel send under a mutex freezes everything

The mux held its lock while doing a blocking channel send to a request's waiter.
When a reader had gone away (a timed-out request, or a deferred reply whose caller
returned), that send blocked **forever while holding the lock** — freezing the
whole mux, so even `Claim()` hung and unrelated context-bounded operations ignored
their deadlines. Fix: capture the channel under the lock, release it, then send
(with an escape on session close).

- This is a general lesson for any per-connection multiplexer: **never do a
  blocking send while holding the dispatch lock.**
- **Check against nginx-xrootd:** the async event loop (`client/lib/aio*.c`,
  `reqmap_*`/`areq_*`) — how it dispatches a frame to a waiter without stalling
  the reader thread; confirm there's no equivalent lock-held-across-blocking-send.

### 7.2 Fixed stream IDs on a shared multiplexer collide

Sub-sessions (data sockets, created for writes) shared the parent's mux but did a
handshake on the fixed stream ID `{0,0}`. Once the main-session bootstrap stopped
*reserving* `{0,0}`, the sub-session handshake collided with a regular request's
`{0,0}`. Fix: sub-sessions handshake synchronously too, so no fixed ID ever
reaches the shared mux.

### 7.3 Claim-before-send is the invariant that makes "drop unknown frames" safe

Because a request registers its mux waiter before the request is written, any
inbound frame with no waiter cannot be an awaited reply — so dropping it is
correct. State this invariant explicitly; it justifies the whole unsolicited-frame
policy (§3.3).

### 7.4 Unclaiming a waiter is not "delete from a map"; it closes a channel a sender may hold

The fix in §7.1 moved the blocking send out from under the mux lock, which left a
second hazard behind it: `Unclaim` closes the waiter's channel, and the reader
goroutine can be parked inside exactly that send. Closing a channel a sender still
holds is not a race-detector nicety — it is `panic: send on closed channel`, and it
takes the process down.

The window opens whenever a caller *abandons* a request while frames for it are
still arriving: a context deadline, a response the client refused on size grounds,
a session close. Those are precisely the fail-closed paths, which is why the bug
survived a green happy-path suite and surfaced the day fail-closed tests existed.

The fix is a per-stream handshake rather than a bare close:

- the waiter owns a `done` channel and a `sending` counter alongside its data channel;
- `SendData` increments `sending` **under the same lock** that removes the waiter,
  then selects over `ch <- data`, `<-done` and `<-quit`;
- `Unclaim` deletes the waiter, closes `done` to release a parked sender, waits for
  `sending` to drain, and only then closes the channel.

Registering under the lock is what makes it airtight: an `Unclaim` racing a delivery
either fails to find the waiter at all (nothing in flight) or is guaranteed to see
the in-flight count.

- **General lesson:** in a multiplexer, the *close* side of a channel handoff needs
  as much care as the send side. "Who is allowed to close, and how do they know no
  one is sending?" should have a written answer.
- **Check against nginx-xrootd:** `reqmap_*`/`areq_*` teardown — how a cancelled or
  timed-out request is retired while the reader thread may still be completing it.

---

## 8. Alternative protocols and API shape

### 8.1 The XRootD-shaped filesystem interface does not fit HTTP/S3 cleanly

`xrdfs.FileSystem` is a 16-method XRootD-centric interface; forcing HTTP/S3/WebDAV
through it is awkward. Better to give each alt-protocol a natural client
(`xrdhttp`, `xrds3`) and adapt where it maps, rather than implement 16 methods of
mostly-errors. libxrdc reaches the same shape via its VFS backends
(`client/lib/vfs*.c`, `vfs_s3*.c`).

### 8.2 Extend with optional interfaces, not by widening the core interface

`PgReader`/`PgWriter`, `XAttrFS`, `ChecksumFS` were added as **optional**
interfaces that a concrete type may implement, keeping existing `xrdfs.File`/
`FileSystem` implementers compiling. This is the low-risk way to grow a published
API.

### 8.3 S3 SigV4 is worth isolating and known-answer testing

The canonical-request / string-to-sign / signing-key chain is easy to get subtly
wrong; keep it in one file with a known-answer test (the empty-payload SHA-256 is
a free checkpoint), and defer byte-exact validation to a real S3/MinIO endpoint.

### 8.4 HTTP-TPC returns 2xx on *acceptance*; the outcome is in the body

This is the single most dangerous trap in the HTTP family. A WLCG `COPY` returns
`200`/`202` as soon as the active endpoint has **accepted** the transfer. The
transfer then runs, emitting a stream of performance markers, and finishes with a
terminal line:

```
Perf Marker
	Timestamp: 1700000000
	Stripe Index: 0
	Stripe Bytes Transferred: 1048576
	Total Stripe Count: 1
End
failure: unable to open destination
```

A client that returns success on the status code alone silently turns every
failed copy into a successful one. Three outcomes must be distinguished, not two:

- a terminal `success:` line — the copy completed;
- a terminal `failure: <reason>` line — the exchange succeeded and the *copy*
  failed; this is a distinct error type (`TPCError`), not a transport error;
- **the stream ending with neither** — the connection was cut mid-copy and the
  true state is unknown. It must be its own error (`ErrTPCNoOutcome`), because
  reporting it as either success or failure is a guess.

Prefer **pull** (COPY to the destination with a `Source:` header) over push where
both work: the endpoint doing the writing is the one that knows whether the write
succeeded.

The remote credential travels in `TransferHeaderAuthorization: Bearer <tok>`,
which the active endpoint strips the prefix from and replays as `Authorization`
against its peer; `Credential: none` tells it not to look for a delegated X.509
proxy instead. The client's own `Authorization` header authenticates the *active*
endpoint and is a different credential.

### 8.5 A bearer token is sent unprompted, so its handling differs from native auth

In the native protocol the server names the providers it accepts before the
client offers anything. HTTP has no such round: the request carries the
credential outright. Two consequences:

- **Never send a bearer token to a cleartext `http://` endpoint.** Refuse at dial
  time and make the caller pass an explicit override for tests. A token is a
  credential anyone who observes it can replay.
- **Ambient discovery must be opt-in.** The WLCG search order
  (`$BEARER_TOKEN`, `$BEARER_TOKEN_FILE`, `$XDG_RUNTIME_DIR/bt_u<uid>`,
  `/tmp/bt_u<uid>`) is right for native `ztn`, where the server asked; for HTTP
  it means shipping whatever token is lying around to whatever host was dialled,
  which has to be the caller's decision.

Route every request through one `do` helper that attaches the credential, so it
cannot be present on `GET` and forgotten on `PROPFIND` or `COPY`. That is a
one-line invariant with a cheap test: drive every verb and assert the header.

X.509 is the mirror image. `gsi` was implemented but never listed in the default
provider chain, so an ambient `/tmp/x509up_u<uid>` proxy was never offered — an
implementation the chain never reaches is indistinguishable from no
implementation at all. Discovery there *is* appropriate, because the server names
`gsi` first.

### 8.6 Emulated filesystem semantics must be refused, not faked

Mapping `xrdfs` onto HTTP/WebDAV leaves gaps. Each one is a decision:

- **No random-access write.** A file opened for writing buffers and is uploaded
  by a single `PUT` on sync/close. Document that a write is not durable until
  close and that the close error must be checked.
- **`DELETE` on a collection is recursive in WebDAV**, but `RemoveDir` in XRootD
  removes an *empty* directory. Check emptiness first, or a typo deletes a tree.
- **`MKCOL` on an existing collection answers `405`/`409`.** `MkdirAll` must
  treat that as success, and only that.
- **Truncate to non-zero, chmod, virtual-FS stat have no equivalent.** Return a
  distinguishable `ErrNotSupported` — a method that silently does nothing is
  worse than one that says it cannot.
- **A collection may not answer `HEAD`.** Fall back to a `Depth: 0` PROPFIND
  before concluding the path is absent.

### 8.7 The URL scheme is the transport selector; dropping it downgrades silently

`xrdio.Open` parsed the URL and then handed only `host:port` to the native
client. Two bugs fell out of the same line: an `https://` URL was dialled as if
it were `root://`, and — worse — a `roots://` URL lost its scheme and so
connected **in cleartext**, because the TLS decision is derived from the scheme.
Pass the whole URL to the dispatcher and let one place decide the transport.

---

## 9. Test harness and environment (unglamorous but load-bearing)

### 9.1 Conformance testing: a strict server, an independent decoder, and a fail-closed half

Permissive mocks answer whatever the client asks and prove very little. What earns
confidence is a *conformance server* built on three rules:

- **Decode independently of the code under test.** Request fields are sliced out of
  the raw frame by byte offset and page CRCs are re-derived from `hash/crc32`, never
  by calling the encoder being tested. Otherwise a test passes by agreeing with the
  encoder's own bug — the two-oracle lesson of §1.2, applied inside one repository.
- **Assert on stored bytes and the operation sequence, not on status codes.** A write
  path is verified by reading back what the server actually holds, and the server
  records every request id it saw so a test can assert on ordering (a `kXR_sync`
  before a `kXR_close`) and on counts (an initial `kXR_pgwrite` plus *exactly one*
  retry — no blind resend of the whole request).
- **Record violations rather than answering wrongly.** The server keeps a list of
  every framing rule the client broke and answers normally; the test fails on a
  non-empty list. This catches breaches that a lenient reply would hide.

The second half is **fail-closed** testing: drive the same server into one specific
misbehaviour per case — over-answering, announcing a body past the response limit and
hanging up behind the lie, stalling forever, corrupting a page CRC, cutting a page
unit short, reporting a page corrupt forever — and assert the client fails with a
*diagnosable* error rather than returning plausible bytes. Every serious bug found
late in this work lived on a fail-closed path (§3.4, §4.4, §7.4); none of them were
reachable from a happy-path test.

Two habits make the suite trustworthy rather than decorative:

- **Test the oracle.** A test that asserts "the strict server recorded no violation"
  is worthless if the server cannot detect one. Include cases that feed it a wrong
  file handle and a deliberately mis-CRC'd page and assert it *does* flag them.
- **Prove non-vacuity by breaking the fix.** After each guard landed, stub it out and
  confirm the suite goes red. The unbounded-accumulation guard, the pgwrite retry
  loop and the unclaim handshake were each verified this way; the last one turned a
  silent `WARNING: DATA RACE` into a reproducible `panic: send on closed channel`.

Finally, **run the suite under `-race`.** The concurrency bug in §7.4 is invisible
without it and fatal with it.

### 9.2 The namespace surface needs its own conformance server

Data I/O is where the interesting framing lives, so it is where a conformance
server gets built first — and then the *namespace* requests (dirlist, open,
stat, statx, mkdir, mv, chmod, rm, rmdir, truncate, fattr, query, ping) keep
being tested with permissive mocks that echo whatever the encoder produced.
They deserve the same three rules, and applying them turns up a different class
of rule to check:

- **A field the client does not own.** Most of these requests are 16 bytes of
  parameters of which 11–16 are reserved. A strict server rejects a non-zero
  reserved byte, which is the only way to catch an encoder that writes an option
  into the wrong offset and happens to be ignored by the server you tested
  against. Blanking `wBuffer.Next(16)` down to `Next(15)` plus a stray byte is
  the cheapest way to confirm the check is live.
- **Requests where one field decides how another is read.** `kXR_mv` puts both
  paths in one blob and separates them by a length the client computes; `kXR_stat`
  and `kXR_truncate` each take *either* a path *or* a file handle and a server has
  no rule for choosing when both arrive; `kXR_fattr` nests `[rc u16][name\0]` and
  `[vlen i32][value]` vectors behind a NUL-terminated path. Each of these is a
  place where a client can be self-consistently wrong.
- **Option bits that change the meaning of success.** `kXR_mkpath` is the only
  thing separating `Mkdir` from `MkdirAll`, so a client that always sets it
  silently deepens the namespace instead of failing. The server has to refuse the
  missing parent for the test to have anything to observe.
- **Replies that are well-framed but not well-formed.** A directory listing with
  stat info pairs its lines, a stat body has four fields, a checksum reply has
  exactly two. Answering `kXR_ok` with a body that breaks one of those is a
  distinct failure mode from a short read, and it needs its own knob — replace the
  reply body — rather than the truncate-and-hang-up knob used for framing.
- **Per-attribute status codes are not request status.** `kXR_fattr` reports a
  missing attribute as `kXR_ok` carrying a non-zero `rc` inside the body. A client
  that only checks the response status returns an empty value for an attribute
  that does not exist.

Assert on the sequence too where the operation has no request of its own:
`RemoveAll` is a walk made of stat, dirlist and removals, and the only thing
proving it is bottom-up is that every `kXR_rmdir` lands after the removals of
that directory's members.

### 9.3 Environment: the setup costs that masquerade as client bugs

- **Reuse the grid PKI layout the server expects.** A Globus CA needs the OpenSSL
  hash-dir symlinks (`<subject_hash>.0`) and a signing-policy file, a host cert
  with a SAN covering `localhost`+loopback, and RFC-3820 proxies with a critical
  `proxyCertInfo` extension. **Check against nginx-xrootd:** `tests/pki_helpers.py`
  (`blitz_test_pki`), `utils/make_proxy.py`, `tests/configs/xrootd_ref_gsi.conf`,
  `tests/lib/refxrootd.sh`.
- **Server config gotchas that cost real time:** GSI needs a grid-mapfile or
  `-gmapopt` that tolerates its absence, and CRL checking disabled when there's no
  CRL dir; native TPC needs `ofs.tpc … pgm xrdcp --server` and write-exported
  paths; the admin-socket path (`adminpath`) has a UNIX-socket length limit, so
  deep temp paths make the server fail to start.
- **Kill child servers deterministically.** A test killed by an outer `timeout`
  leaves orphaned `xrootd` processes holding ports; dozens accumulated and caused
  "hangs" that looked like client bugs. Bound every operation with a context and
  reap children in cleanup.
- **Guard against the cert-validity window.** OpenSSL stamps `NotBefore` at the
  current second; a TLS handshake microseconds later can see a not-yet-valid cert.
  Back-date or settle briefly after minting.
- **Gate expensive/flaky interop behind an env flag** and skip cleanly when the
  server/tool is absent, so the normal `go test` stays green and offline.

---

## 10. Summary checklist to run against nginx-xrootd

For each item, confirm libxrdc/the nginx server does the same thing (or note where
it deliberately differs):

- [ ] TLS negotiated between `kXR_protocol` and login; no silent cleartext
  downgrade (`client/lib/conn.c`, `tls.c`).
- [ ] A cancelled or timed-out request is retired without the reader thread
  completing into freed/closed state (§7.4) — `reqmap_*`/`areq_*` teardown.
- [ ] `kXR_status` trailing data drained via the body's own `dlen`
  (`client/lib/ops_file_pg.c`).
- [ ] `kXR_waitresp` handled; deferred completion delivered as
  `kXR_attn`/`kXR_asynresp` and unwrapped (`client/lib/aio*.c`; server
  `XrdXrootdResponse.cc`).
- [ ] pg page framing aligned to file offsets; per-page CRC-32C.
- [ ] Response lengths bounded per frame *and* per request; negative `dlen`
  rejected (`client/lib/brix_ops.h`: `DLEN_MAX`, `VEC_MAXSEGS`,
  `VEC_MAXBYTES`).
- [ ] `kXR_pgwrite` CSE trailer parsed and each named page resent with
  `kXR_pgRetry` under a bounded budget (`client/lib/ops_file_pg.c`:
  `pgwrite_handle_cse`).
- [ ] `crc64` = CRC-64/XZ; `crc32c` = Castagnoli; SSS CRC = IEEE.
- [ ] `fattr` nvec/vvec endianness and the count-prefixed vs NUL-list responses.
- [ ] `sss` blob byte layout and Blowfish-CFB64 zero-IV encoding
  (`src/compat/sss_bf.c`).
- [ ] GSI unsigned-DH: verbatim PEM params, raw-shared-secret key, raw-rtag
  PKCS#1 signature, XrdSutBuffer bucket codec (`src/auth/gsi/*`).
- [ ] TPC: source coordinator uses `tpc.dst`; dest is a write open; **sync**
  triggers the transfer; completion is async (`XrdOfs*`, `src/tpc/*`).
- [ ] No blocking send under the dispatch lock; fixed stream IDs never collide on
  a shared mux (`client/lib/aio*.c`).
- [ ] HTTP-TPC: the `COPY` body is parsed and a `failure:` line under a 2xx
  status is an error; a body with no terminal line is neither success nor
  failure (§8.4).
- [ ] Bearer tokens are refused on cleartext `http://`, attached to *every*
  verb, and never discovered implicitly (§8.5).
- [ ] The URL scheme selects the transport at one place; `roots://` cannot lose
  its TLS by being reduced to `host:port` (§8.7).
