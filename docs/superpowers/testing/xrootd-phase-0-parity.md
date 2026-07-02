# Phase 0 Parity Verification (TLS + VFS)

Two oracles must agree with the go-hep client before Phase 0 counts as done.

## Oracle 1 — official XRootD server (interop)

Prerequisite: an XRootD server with TLS enabled (config `xrd.tls` cert/key and
`xrd.tlsca`), reachable at `roots://HOST:1094`.

```sh
export XROOTD_TLS_SERVER=roots://HOST:1094
export XROOTD_TLS_PATH=//store/test/file.root
# For a self-signed test cert:
export XROOTD_TLS_INSECURE=1
go test ./xrootd/ -run TestTLSInterop -v
```

Expected: the stat succeeds over a TLS-upgraded connection. Capture a packet
trace (`tcpdump`) if you need to confirm the bytes after the `kXR_protocol`
reply are TLS records, not cleartext XRootD frames.

## Oracle 2 — libxrdc C client (behavioral cross-check)

Run the same operation with the reference C client and confirm identical
results:

```sh
# Native C client (roots:// forces TLS, same negotiation path go-hep now uses):
/home/rcurrie/HEP-x/nginx-xrootd/client/bin/xrdfs roots://HOST:1094 stat /store/test/file.root
```

Compare: both clients must successfully negotiate TLS and return the same stat
metadata (size, flags) for the same path. Once Phase 1 lands, checksum values
must also match between the go-hep client and `xrdfs`/`xrdcp`.

## Cleartext regression

`root://HOST:1094` (no TLS) must continue to work unchanged:

```sh
go test ./xrootd/...   # full suite, all green
```

The suite exercises the cleartext bootstrap against mock servers and, where
reachable, the public `ccxrootdgotest.in2p3.fr` test endpoints.
