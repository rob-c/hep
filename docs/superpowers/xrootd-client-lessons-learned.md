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

---

## 9. Test harness and environment (unglamorous but load-bearing)

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
- [ ] `kXR_status` trailing data drained via the body's own `dlen`
  (`client/lib/ops_file_pg.c`).
- [ ] `kXR_waitresp` handled; deferred completion delivered as
  `kXR_attn`/`kXR_asynresp` and unwrapped (`client/lib/aio*.c`; server
  `XrdXrootdResponse.cc`).
- [ ] pg page framing aligned to file offsets; per-page CRC-32C.
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
