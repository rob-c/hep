# Phase 3 Parity Verification (expanded auth)

## Oracle 1 — official XRootD server

```sh
export XROOTD_P3_SERVER=roots://HOST:1094   # roots:// so TLS protects the token
export XROOTD_P3_PATH=//store/test/file.root

# ztn (bearer token)
export BEARER_TOKEN="$(cat /path/to/token)"
XROOTD_P3_PROVIDER=ztn go test ./xrootd/ -run TestPhase3Interop -v

# sss (shared secret)
export XrdSecSSSKT=/path/to/sss.keytab
XROOTD_P3_PROVIDER=sss go test ./xrootd/ -run TestPhase3Interop -v
```

Expected: the stat succeeds, proving the server accepted the credential.

## Oracle 2 — libxrdc / stock XRootD client cross-check

Same server, same credential, via the C client (binaries under
/home/rcurrie/HEP-x/nginx-xrootd/client/bin):

```sh
XrdSecPROTOCOL=ztn xrdfs roots://HOST:1094 stat /store/test/file.root
XrdSecPROTOCOL=sss XrdSecSSSKT=/path/to/sss.keytab xrdfs root://HOST:1094 stat /store/test/file.root
```

Both clients must be accepted by the same server with the same credential. For
sss, a credential minted by go-hep and one minted by the C client must both
decrypt against the same keytab key (the server is the decryptor).

## S3 credentials

`s3cred` has no server round trip in this phase; its discovery precedence is
covered by unit tests and will be exercised end-to-end by the Phase 4 S3 backend.

## Regression

`go test ./xrootd/...` stays green; unix/host/krb5 auth is unchanged (ztn/sss
are additive and selected only when the server offers them).
