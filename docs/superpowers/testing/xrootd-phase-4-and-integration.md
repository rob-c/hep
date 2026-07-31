# Phase 4 (alt protocols) + real-XRootD integration

## What is implemented and verified

- **HTTP/HTTPS backend** (`xrootd/xrdhttp`): ranged GET, HEAD/stat, PUT, DELETE,
  and X.509 mutual-TLS (`WithClientCertificate`, `WithRootCAs`,
  `WithInsecureTLS`). Unit-tested against `httptest`, including a mutual-TLS test
  that generates a CA/server/client cert chain and asserts the server sees the
  client subject.
- **WebDAV** (`xrootd/xrdhttp`): PROPFIND at `Depth: 0` and `Depth: 1` with
  multistatus XML parsing, plus MKCOL and MOVE. Exposed as a full
  `xrdfs.FileSystem` through `(*xrdhttp.Client).FS()`.
- **HTTP bearer tokens** (`xrootd/xrdhttp`): `WithBearerToken`,
  `WithDiscoveredBearerToken` (WLCG search order, opt-in), attached to every
  verb. Dial refuses to pair a token with a cleartext `http://` endpoint unless
  `WithInsecureBearerToken` is passed.
- **HTTP third-party copy** (`xrootd/xrdhttp`): WLCG `COPY` in both push and
  pull mode, with `TransferHeaderAuthorization` for the remote credential,
  performance-marker progress, and outcome parsing — a `failure:` line under a
  2xx status is an error, and a body with no terminal line is
  `ErrTPCNoOutcome`.
- **S3 backend** (`xrootd/xrds3`): AWS SigV4-signed GET (ranged), HEAD, PUT,
  DELETE; credentials from `xrootd/internal/s3cred`.
- **Real-XRootD integration harness** (`xrootd/it_test.go`, gated by
  `XROOTD_IT=1`): builds a Globus-style grid PKI with openssl, launches a real
  `xrootd` with root:// and XrdHttp-over-TLS, and verifies the go-hep client.

## Protocol and credential coverage

`xrootd.Dial` is the single entry point: the URL scheme selects the transport and
returns a protocol-neutral `Backend`. `xrdio.Open` goes through it, so the same
schemes work there.

| Ask | Scheme / mechanism | Status |
|-----|--------------------|--------|
| `root://`, `xroot://` | native XRootD, cleartext | supported |
| `roots://`, `xroots://` | native XRootD with in-protocol TLS (`kXR_protocol` → TLS → login) | supported |
| XrdHttp | `http://`, `https://` — ranged GET, HEAD, PUT, DELETE | supported |
| WebDAV | `dav://`, `davs://` (rewritten to `http`/`https`); PROPFIND, MKCOL, MOVE, DELETE | supported |
| token auth | native `ztn` (ambient discovery) and HTTP `Authorization: Bearer` | supported |
| X.509 auth | native `gsi` (unsigned-DH; ambient `/tmp/x509up_u<uid>` proxy) and HTTPS mutual TLS | supported |
| TPC | native (`xrootd/xrdcopy`, `tpc.dst`/`tpc.src` opaque) and HTTP-TPC (`COPY`, push and pull) | supported |
| vector I/O | `kXR_readv` / `kXR_writev` | **not implemented** (see lessons-learned §4.5) |

Operations with no HTTP equivalent return `xrdhttp.ErrNotSupported` rather than
silently doing nothing: chmod, virtual-filesystem stat, checksum-verified write,
and truncation to a non-zero size. A file opened for writing over HTTP buffers in
memory and is uploaded by a single PUT on sync/close, so a write is not durable
until close and the close error must be checked.

## Running the integration test

Requires `xrootd` (tested with v5.9.5), `openssl`, and `libXrdHttp-5.so`.

```sh
export PATH=$HOME/.local/share/go/bin:$PATH
XROOTD_IT=1 go test ./xrootd/ -run TestIntegrationRealServer -v
```

Verified legs:

| Leg | Status | What it proves |
|-----|--------|----------------|
| `root-anon` | PASS | go-hep client stats and reads a file over root:// from a real server |
| `xrdhttps-x509` | PASS | `xrdhttp` reads a file over HTTPS presenting an X.509 client cert (mutual TLS), server verified against the grid CA |
| `copy-engine` | PASS | `xrdcopy` downloads (with checksum verify), uploads, and reads back |
| `copy-resume` | PASS | `xrdcopy` resumes a partial download to full content; no-ops a complete file |
| `root-gsi` | PASS | the pure-Go GSI client completes the two-round handshake against a GSI-configured xrootd and reads a file end-to-end |
| `copy-tpc` | PASS | native third-party copy: the destination server pulls a file directly from the source (no bytes through the client), driven by the go-hep client |

## Phase 2 — copy engine (`xrootd/xrdcopy`)

Working and verified: download, upload, local copy, recursive trees,
post-transfer checksum verification, **resumable transfers**, and native
**third-party copy** (`copy-tpc` leg passes against real stock XRootD).

Native **TPC** (`xrdcopy.TPC`) reproduces the stock client protocol as traced
from `xrdcp --tpc` byte-for-byte: the placement open, the source coordinator
open (`tpc.dst`/`tpc.key`/`tpc.stage=copy`, mode 0, `open_read|retstat|async`),
and the destination puller open (full `oss.asize`/`tpc.dlg`/`tpc.lfn`/… opaque,
mode 0644, `delete|open_updt|retstat|async`). The opaque is byte-identical to
stock and the open flags map to the correct `kXR_*` bits.

The full working sequence (confirmed by reading the XRootD C++ sources under
`/tmp/xrootd-src`, not guessing):

1. Placement open on the source (`tpc.stage=placement`), then close.
2. Source coordinator open (read, `tpc.dst`/`tpc.key`/`tpc.stage=copy`) — kept
   open so the key registration stays live (`XrdOfsTPC::Authorize`).
3. Destination puller open (write, full opaque) — `XrdOfsTPC::Validate` sets up
   the copy job but does **not** run it.
4. **Two syncs on the destination**: `XrdOfsFile::sync` calls
   `XrdOfsTPCJob::Sync`, whose first call starts the copy (pgm `xrdcp`) and whose
   second waits for completion.
5. The completion reply is delivered **asynchronously**: the server sends a
   `kXR_attn` response (action `kXR_asynresp`) wrapping the real reply for the
   sync's stream (`XrdXrootdResponse` / `XrdXrootdTransit::Attn`). The go-hep
   session now unwraps this and dispatches it to the waiting stream.

Missing any one of these (the placement, the correct flags, the double-sync, or
the async-response unwrap) leaves the destination file empty or the sync hung —
which is exactly the sequence of dead ends this took to work through.

Fixed along the way: a `mux.SendData` deadlock (a blocking channel send under
the mutex froze the whole mux when a reader went away), plus
`kXR_authmore`/`kXR_waitresp`/`kXR_attn` handling.

Note on the x509 leg: it verifies mutual-TLS access (the client presents its
X.509 user cert and the server certificate is CA-verified). Enforced x509 →
identity *authorization* mapping (rejecting anonymous access) needs additional
server config (`http.secxtractor` + an `acc.authdb`); the harness serves an
anon-readable file, so it proves the transport + client-cert path, not authz
rejection.

## GSI / X.509 proxy (root://+gsi) — working (unsigned-DH path)

GSI is a multi-round (`kXR_authmore`) exchange, implemented in
`xrootd/xrdproto/auth/gsi` and driven by the `auth.Continuer` interface:

1. client → `kXGC_certreq` (crypto module, version, CA hash, a random tag);
2. server → `kXGS_cert` as `kXR_authmore` (its DH public blob, cipher/digest
   lists, and a random tag to sign);
3. client → `kXGC_cert`: the client DH public blob, the chosen cipher, and an
   AES-128-CBC-encrypted inner buffer carrying the X.509 proxy chain and the
   server tag signed with the proxy key (proof of possession).

### Implemented

- **`kXR_authmore` (4002) multi-round transport** in `cliSession` (via
  `auth.Continuer`); single-round providers are unaffected.
- **GSI codec** (`gsi.go`): XrdSutBuffer framing, buckets, and `BuildCertReq`.
- **GSI crypto** (`crypto.go`): finite-field Diffie-Hellman in the group the
  server advertises (its PEM parameters are echoed verbatim), the session key as
  the leading key bytes of the raw shared secret (unsigned-DH `HasPad=0`),
  AES-128-CBC with a zero IV and PKCS#7 padding, and RSA PKCS#1 v1.5
  proof-of-possession over the raw tag.
- **Provider** (`provider.go`): `Auth` implements `auth.Auther`+`auth.Continuer`;
  `LoadProxy` reads a combined proxy PEM; `DefaultProxyPath` resolves
  `$X509_USER_PROXY` or `/tmp/x509up_u<uid>`.

The client advertises version 10300 to select the **unsigned-DH** path, which
avoids RSA-verifying signed DH parameters and uses a zero IV. Crypto primitives
are unit-tested (DH agreement, blob round-trip, AES, RSA POP), and the harness
`root-gsi` leg authenticates and reads against real XRootD v5.9.5.

### Not implemented

- The **signed-DH** path (server versions that only send `kXRS_cipher`
  RSA-signed DH params) — the client forces unsigned-DH, which modern servers
  still support.
- **X.509 delegation** (`kXGS_pxyreq` → `kXGC_sigpxy`): a delegation challenge
  is rejected with a clear error.

Server config for the leg (see `writeGSIConfig` in `it_test.go`):
`sec.protocol <libdir> gsi -certdir:<caDir> -cert:<hostCert> -key:<hostKey>
-gridmap:<file> -gmapopt:1 -crl:0`, and a proxy minted from the user cert.
