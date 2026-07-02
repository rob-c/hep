# Phase 1 Parity Verification (pg I/O, fattr, checksums)

## Oracle 1 — official XRootD server

```sh
export XROOTD_P1_SERVER=root://HOST:1094      # or roots:// for TLS
export XROOTD_P1_PATH=//tmp/testfile.dat
go test ./xrootd/ -run TestPhase1Interop -v
```

Expected: pgread returns CRC-verified data whose local adler32 matches the
server-reported kXR_Qcksum value; the fattr round trip succeeds (or skips
where the server disables xattrs).

## Oracle 2 — libxrdc cross-checks

All binaries under /home/rcurrie/HEP-x/nginx-xrootd/client/bin:

- Checksum parity: `xrdadler32 root://HOST//tmp/testfile.dat` and
  `xrdcrc64 root://HOST//tmp/testfile.dat` must equal
  `xrdsum.Sum("adler32"|"crc64", <bytes read by go-hep PgReadAt>)`.
- pg I/O cross-client: a file written by go-hep `PgWriteAt` must read back
  byte-identical through `xrdcp` (and vice versa), and both clients must
  agree with the server checksum.
- fattr: attributes set via go-hep `SetXAttr` must be visible to the C
  client's fattr helpers (`xrdfs` xattr subcommands) and vice versa.

## Regression

`go test ./xrootd/...` stays green; pg mock tests cover CRC corruption,
unaligned first pages, multi-frame responses, and status-frame CRC failures.
