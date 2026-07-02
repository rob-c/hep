# Phase 4 (alt protocols) + real-XRootD integration

## What is implemented and verified

- **HTTP/HTTPS backend** (`xrootd/xrdhttp`): ranged GET, HEAD/stat, PUT, DELETE,
  and X.509 mutual-TLS (`WithClientCertificate`, `WithRootCAs`,
  `WithInsecureTLS`). Unit-tested against `httptest`, including a mutual-TLS test
  that generates a CA/server/client cert chain and asserts the server sees the
  client subject.
- **WebDAV listing** (`xrootd/xrdhttp`): PROPFIND `Depth: 1` with multistatus
  XML parsing (`Dirlist`).
- **S3 backend** (`xrootd/xrds3`): AWS SigV4-signed GET (ranged), HEAD, PUT,
  DELETE; credentials from `xrootd/internal/s3cred`.
- **Real-XRootD integration harness** (`xrootd/it_test.go`, gated by
  `XROOTD_IT=1`): builds a Globus-style grid PKI with openssl, launches a real
  `xrootd` with root:// and XrdHttp-over-TLS, and verifies the go-hep client.

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
| `copy-tpc` | opt-in | native TPC (`XROOTD_IT_TPC=1`); client follows the stock protocol but the pgm-model pull does not yet complete — see Phase 2 note |

## Phase 2 — copy engine (`xrootd/xrdcopy`)

Working and verified: download, upload, local copy, recursive trees,
post-transfer checksum verification, and **resumable transfers**. Native
**TPC** is implemented per the stock client protocol (placement, source
coordinator open with `tpc.dst`/`tpc.key`/`tpc.stage=copy`, destination puller
open with the full `oss.asize`/`tpc.dlg`/`tpc.lfn` opaque) but the pgm-model
pull does not yet complete against the test servers (the file arrives empty),
so its harness leg is opt-in. Reference `xrdcp --tpc` succeeds against the same
servers, so the gap is a client-protocol detail. Fixed along the way: a
`mux.SendData` deadlock (blocking channel send under the mutex froze the whole
mux) and `kXR_authmore`/`kXR_waitresp` handling.

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
