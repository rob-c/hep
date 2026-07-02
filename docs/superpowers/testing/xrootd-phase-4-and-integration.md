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

## GSI / X.509 proxy (root://+gsi) — partial

GSI is a multi-round (`kXR_authmore`) exchange:

1. client → `kXGC_certreq`; server → `kXGS_cert` (its DH public key + cert +
   cipher list), delivered as `kXR_authmore`.
2. client → `kXGC_cert`: the client DH public key, a selected cipher, and an
   AES-256-CBC-wrapped inner buffer carrying the X.509 proxy PEM, with the
   signing key derived as `SHA256(DH shared secret)`.

### Built and tested

- **`kXR_authmore` (4002) multi-round transport** in `cliSession`: the status
  is carried through the mux and read loop, and multi-round exchanges are driven
  via the `auth.Continuer` interface (`More(challenge)`). Proven by a two-round
  mock test (`authmore_mock_test.go`); single-round providers are unaffected.
- **GSI XrdSutBuffer codec** (`xrootd/xrdproto/auth/gsi`): message framing
  (`gsi\0` + step + buckets + terminator), bucket encode/decode/find, and the
  round-1 `kXGC_certreq` builder — no cryptography. Unit-tested with round-trip
  and structural assertions.

### Remaining (the round-2 crypto kernel)

The `root-gsi` harness leg is still skipped: round 2 is not implemented. It is a
~5,000-line body of OpenSSL-heavy logic in the reference sources
(`/home/rcurrie/HEP-x/nginx-xrootd/src/auth/gsi/*.c`: cert_response.c,
gsi_core.c, gsi_dh.c, gsi_cipher.c, gsi_rsa.c, parse_x509.c, proxy_req.c) that
must be reproduced to byte-match official `XrdSecgsi`:

- parse the server's `kXGS_cert` (DH public key / signed DH params, server cert
  chain, cipher + digest lists, the random tag to sign);
- Diffie-Hellman agreement and derivation of the AES-256-CBC session key as
  `SHA256(shared secret)`;
- proof-of-possession: sign the server's `rtag` with the proxy's RSA key;
- assemble the encrypted `kXRS_main` and the outer `kXGC_cert` carrying the
  client DH public key, selected cipher, and X.509 proxy chain;
- X.509 proxy loading (the nginx suite's `utils/make_proxy.py` mints a
  compatible proxy at `/tmp/x509up_u<uid>`).

Go has the needed primitives in the standard library (`crypto/rsa`,
`crypto/aes`, `crypto/x509`, `crypto/sha256`, `math/big` for finite-field DH),
so no cgo is required — but matching the exact wire encoding is the work.

Once implemented, the harness's `root-gsi` subtest replaces its `t.Skip` with a
real transfer: configure the server with
`sec.protocol gsi -certdir:<caDir> -cert:<hostCert> -key:<hostKey>` (see
`tests/configs/xrootd_ref_gsi.conf`) and dial with the GSI provider.
```
