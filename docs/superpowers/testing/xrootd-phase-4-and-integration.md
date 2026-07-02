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
| `root-gsi` | SKIP | requires the GSI client (see below) |

Note on the x509 leg: it verifies mutual-TLS access (the client presents its
X.509 user cert and the server certificate is CA-verified). Enforced x509 →
identity *authorization* mapping (rejecting anonymous access) needs additional
server config (`http.secxtractor` + an `acc.authdb`); the harness serves an
anon-readable file, so it proves the transport + client-cert path, not authz
rejection.

## Outstanding: GSI / X.509 proxy (root://+gsi) — Phase 3b

The `root-gsi` leg is present but skipped because the GSI security provider is
not implemented. GSI is a multi-round (`kXR_authmore`) exchange:

1. client → `kXGC_certreq`; server → `kXGS_cert` (its DH public key + cert +
   cipher list), delivered as `kXR_authmore`.
2. client → `kXGC_cert`: the client DH public key, a selected cipher, and an
   AES-256-CBC-wrapped inner buffer carrying the X.509 proxy PEM, with the
   signing key derived as `SHA256(DH shared secret)`.

Implementing it requires:

- `kXR_authmore` (4002) continuation support in `cliSession` (the read loop and
  a multi-round `Auther` interface) — not yet present.
- The bucket-TLV wire format, DH exchange, and AES wrapping, reverse-engineered
  from the reference sources at
  `/home/rcurrie/HEP-x/nginx-xrootd/src/gsi/*.c` (auth.c, cert_response.c,
  gsi_core.c, gsi_dh.c, gsi_cipher.c, delegation.c, proxy_req.c) and
  `src/protocol/gsi.h`.
- X.509 proxy generation/loading (the nginx suite's `utils/make_proxy.py`
  produces a compatible proxy).

Once implemented, the harness's `root-gsi` subtest replaces its `t.Skip` with a
real transfer: configure the server with
`sec.protocol gsi -certdir:<caDir> -cert:<hostCert> -key:<hostKey>` (see
`tests/configs/xrootd_ref_gsi.conf`) and dial with the GSI provider.
```
