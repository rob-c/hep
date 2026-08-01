# xrootd examples

Thirty small programs, one directory each. Every one compiles and passes
`go vet` as written; change the endpoint and the path at the top and run it.

```sh
go run ./xrootd/example/xrd-ex-01-root-read
```

If you read three, read **01**, **12** and **18**.

## a) Files over `root://`

| | |
|---|---|
| [xrd-ex-01-root-read](xrd-ex-01-root-read) | connect, stat, open, read a range |
| [xrd-ex-02-root-url](xrd-ex-02-root-url) | `xrdio.Open` on a whole URL — note the double slash |
| [xrd-ex-03-root-write](xrd-ex-03-root-write) | create with parents, write, sync, close with a length check |
| [xrd-ex-04-root-download](xrd-ex-04-root-download) | stream a file to disk, removing the partial on failure |
| [xrd-ex-05-root-vector-read](xrd-ex-05-root-vector-read) | scattered ranges in **one** round trip |
| [xrd-ex-06-root-parallel-reads](xrd-ex-06-root-parallel-reads) | 8 concurrent readers over one connection |
| [xrd-ex-07-root-pgread](xrd-ex-07-root-pgread) | per-page CRC-32C, checked on arrival |
| [xrd-ex-08-root-checksum](xrd-ex-08-root-checksum) | server-side checksum, verified against the bytes you got |
| [xrd-ex-09-root-namespace-ops](xrd-ex-09-root-namespace-ops) | mkdir, touch, rename, truncate, remove |
| [xrd-ex-10-root-xattr](xrd-ex-10-root-xattr) | extended attributes — and why their errors hide inside a success |

## b) Listing files and folders

| | |
|---|---|
| [xrd-ex-11-list-dir](xrd-ex-11-list-dir) | one directory, with stat info in the same reply |
| [xrd-ex-12-list-walk](xrd-ex-12-list-walk) | a whole subtree, tolerating what it cannot read |
| [xrd-ex-13-list-glob](xrd-ex-13-list-glob) | `*`, `?`, `[a-z]` and `**` |
| [xrd-ex-14-list-statx](xrd-ex-14-list-statx) | stat a **list** of paths in one request |
| [xrd-ex-15-list-checksums](xrd-ex-15-list-checksums) | a directory listing with checksums attached |
| [xrd-ex-16-list-locate](xrd-ex-16-list-locate) | which servers actually hold a file |
| [xrd-ex-17-list-tape](xrd-ex-17-list-tape) | find what is on tape, then ask for it back |
| [xrd-ex-25-any-url](xrd-ex-25-any-url) | one listing code path for `root://`, `davs://` and `https://` |
| [xrd-ex-27-query-space](xrd-ex-27-query-space) | free space, server config, monitoring identity |

## c) Tokens and WebDAV

| | |
|---|---|
| [xrd-ex-18-dav-token-read](xrd-ex-18-dav-token-read) | discover a bearer token, ranged read over `davs://` |
| [xrd-ex-19-dav-token-upload](xrd-ex-19-dav-token-upload) | upload with a token from your own environment variable |
| [xrd-ex-20-dav-list](xrd-ex-20-dav-list) | PROPFIND, then walk and glob over the same client |
| [xrd-ex-21-dav-tpc](xrd-ex-21-dav-tpc) | third-party copy — two endpoints, two credentials |
| [xrd-ex-22-dav-mtls](xrd-ex-22-dav-mtls) | X.509 proxy as a client certificate |
| [xrd-ex-23-native-auth](xrd-ex-23-native-auth) | tokens and GSI over the **native** protocol, and prompting |
| [xrd-ex-24-s3](xrd-ex-24-s3) | the same shape against S3 object storage |

## The network between you and the storage

| | |
|---|---|
| [xrd-ex-26-clone-checkpoint](xrd-ex-26-clone-checkpoint) | server-side assembly, made all-or-nothing |
| [xrd-ex-28-hardened-native](xrd-ex-28-hardened-native) | every native bound, one at a time, with the reason |
| [xrd-ex-29-hardened-http](xrd-ex-29-hardened-http) | what the HTTP transport retries, and what it never will |
| [xrd-ex-30-env-and-errors](xrd-ex-30-env-and-errors) | `XRD_*` configuration, and telling a 404 from a dead link |

Notice what is *not* in any of the first twenty-seven: an option asking for the
bounds. `xrootd.NewClient` and `xrdhttp.Dial` apply them, and the two programs
that spell them out do so to explain them, not because anything needs writing.

**The failure worth planning for is silence, not an error.** A refused
connection is reported immediately and is not the problem. The problem is a path
that stops forwarding while both ends still believe the connection is up — a
firewall that dropped the flow from its table, a load balancer that failed over,
a route that got black-holed. Nothing is closed, nothing is refused, and a read
blocks until TCP gives up, which on Linux is the better part of an hour. That is
what a beginner's first program against grid storage hits, and it looks like a
hang with no output rather than like an error.

Native (`root://`, `roots://`), all four applied by `NewClient`, each also
readable from its environment variable, each overridable by naming it after:

| Option | Env | Default |
|---|---|---|
| `WithStreamTimeout` | `XRD_STREAMTIMEOUT` | 60s |
| `WithConnectionWindow` | `XRD_CONNECTIONWINDOW` | 30s |
| `WithConnectionRetry` | `XRD_CONNECTIONRETRY` | 5 |
| `WithKeepAlive` | `XRD_TCPKEEPALIVE`, `…TIME`, `…INTVL`, `…PROBES` | 30s / 10s / 3 |

The stream timeout is the interesting one, because a read deadline alone is not
enough: `io.ReadFull` returns the identical `os.ErrDeadlineExceeded` for three
different situations.

| Bytes read | Request outstanding | What it means | What the client does |
|---|---|---|---|
| 0 | no | an idle connection | keep it, carry on reading |
| 0 | yes | the server never answered | fail the waiters, name the silence |
| >0 | either | a frame stopped half-way | fail the waiters, name the silence |

Telling them apart needs a byte-counting reader around the socket and a look at
the pending-request map. Without it you get to choose between tearing down every
idle connection and hanging forever on a dead one.

Only the **connection** is retried. Nothing has been sent when a dial fails, so
repeating it cannot make anything happen twice; a *request* is a different
matter, since the server may have carried it out and lost the answer.

HTTP (`https://`, `davs://`) — `Dial` applies five attempts and a five-minute
per-attempt timeout, sized to cover a whole ranged read. Resent: transport
failures that happened before any response was read, and 429/500/502/503/504.
Never resent: `COPY` (its first attempt may still be running, and two servers
writing one file is worse than a failure), `POST`, `PATCH`, `MKCOL`, and any
request whose body cannot be produced a second time.

`xrootd.Unbounded()` and `xrdhttp.Unbounded()` remove all of it, for the caller
whose own context is the only deadline they want.

Errors are safe to log. `net/http` records the request URL verbatim in the
`*url.Error` it builds for a transport failure — query string and all, which is
exactly where WebDAV and XRootD put `?authz=Bearer%20eyJ...`. Every transport
error is stripped of its query, its fragment and any userinfo password before it
is returned.
