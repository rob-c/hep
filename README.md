hep
===

[![go.dev reference](https://pkg.go.dev/badge/go-hep.org/x/hep)](https://pkg.go.dev/go-hep.org/x/hep)
[![License](https://img.shields.io/badge/License-BSD--3-blue.svg)](https://go-hep.org/license)
[![DOI](https://zenodo.org/badge/DOI/10.5281/zenodo.597940.svg)](https://doi.org/10.5281/zenodo.597940)
[![JOSS Paper](http://joss.theoj.org/papers/0b007c81073186f7c61f95ea26ad7971/status.svg)](http://joss.theoj.org/papers/0b007c81073186f7c61f95ea26ad7971)

> **About this fork.** Upstream go-hep has moved to
> [Codeberg](https://codeberg.org/go-hep/hep), which has demonstrably worse
> uptime than GitHub and an aggressive, somewhat euro-centric set of anti-LLM
> policies. This fork ignores both, for the sake of ***GETTING THE JOB DONE***.
> Work lands here when it works and is tested; we do not kick the can down the
> road for the sake of posturing.
>
> `main` is where the work is — you are looking at it. The
> [`upstream`](https://github.com/rob-c/hep/tree/upstream) branch is a plain
> mirror of Codeberg's `main`, refreshed daily and never written to by hand, so
> the two can always be compared. See [FORK.md](FORK.md).

What this fork adds
-------------------

## A pure-Go XRootD client that actually does the job

Upstream's `xrootd` package could open a file and read it. Grid storage asks
for a good deal more than that, and everything below is new here:

**One function per job, for people whose job is physics.** The
[`xrd`](xrootd/xrd) package is the whole of grid storage with nothing to set up
first — no connections to open, no `context`, no options:

```go
err = xrd.Check(dir)                // is it reachable, am I allowed, is it there?
runs, err := xrd.Lines(dir + "/runs.txt")
files, err := xrd.Glob(dir + "/run*/AOD*.root")
size, err := xrd.Size(dir)          // the whole tree, one call
here, err := xrd.DownloadAll(files, "data")   // four at a time, resumable
err = xrd.WriteFile(out, summary)   // creates the directories it needs
```

Every one of those names can be a plain local path instead of a URL, so a
program written against a file on your laptop runs against the grid by changing
the string — errors included, which say the same thing about a missing file
either way. Connections are opened once and reused, a connection that dies is
replaced without you hearing about it, credentials are found where the
command-line tools look for them, and when something does go wrong the error
says what to check — the directory above, your token, or the port. The packages
underneath are still there for when you need the control.

And none of it has to be a program at all: `xrd-fs` does the same jobs from a
shell — `check`, `stat`, `du`, `cat`, `find`, `mkdir`, `rm`, `mv` — with
`xrd-ls` for listings and `xrd-cp` for copies.

**Protocol.** In-protocol TLS (`roots://`, `xroots://`) negotiated before login;
vector I/O (`kXR_readv`, `kXR_writev`); paged I/O with per-page CRC-32C
(`kXR_pgread`, `kXR_pgwrite`) and recovery of a corrupt page; extended
attributes (`kXR_fattr`); server-side checksums (`kXR_Qcksum`, `kXR_dcksm`);
`kXR_clone`; checkpoints; deep locate; prepare/evict for tape; `kXR_status`
framing; an admin surface; and the `XRD_*` environment.

**Beyond `root://`.** `xrdhttp` speaks HTTPS and WebDAV — ranged reads, uploads,
PROPFIND listings, HTTP third-party copy, X.509 mutual TLS — and `xrds3` speaks
S3 with SigV4. One `Backend` interface and a scheme-aware `Dial` sit over all of
them, so a listing or a read is the same code for `root://`, `davs://`,
`https://` and `s3://`.

**Authentication that grid sites actually run.** GSI (including the unsigned-DH
kernel and multi-round `kXR_authmore`), bearer tokens (`ztn`), `sss` keytabs,
and X.509 proxies. Credentials are discovered the way the stock tools discover
them; a missing one is asked for up front rather than turning into a protocol
error later, and proxy creation is delegated to `voms-proxy-init` so no
passphrase is ever handled here. A URL that names no user — which is nearly
every URL — logs in as `$XRD_USERNAME`, or as the person running the program,
rather than asserting an empty name at a site that maps it to nothing.

**Copies.** `xrdcopy` does resumable transfers and native third-party copy,
verified end-to-end against a real XRootD server.

**ROOT files read *and written* over the network.** `xrdio` opens remote files
with the `os.O_*` flags — create, truncate, append, exclusive create, parent
directories made on the way — and `groot.Create("root://server//out.root")`
writes a ROOT file straight to storage, seeking back over its own header the
way a local write does. Upstream could only ever read a remote file, and only
over `root://`; here `roots://`, `xroot://`, `xroots://`, `https://` and
`davs://` all go both ways (over HTTP the file is buffered and PUT whole, which
is HTTP's limitation, and is documented where you meet it).

**Safe by default, which is the point.** The failure a beginner meets on grid
storage is not a refused connection — it is a path that goes quiet while both
ends still believe it is up, and a read that blocks for the better part of an
hour. `xrootd.NewClient` and `xrdhttp.Dial` apply stream timeouts, connection
windows, bounded retries and keep-alives *without being asked*, and only ever
retry what is safe to repeat — never a `COPY`, a `POST` or a body that cannot be
produced twice. Response sizes from the network are bounded before allocation.
Transport errors are stripped of query, fragment and userinfo before they are
returned, because that is where `?authz=Bearer eyJ...` lives, and errors get
logged. `Unbounded()` removes all of it for callers whose context is their own
deadline.

**Thirty-two runnable programs.** [`xrootd/example`](xrootd/example) — one
directory each, from a first ranged read to token-authenticated WebDAV, tape
recall and third-party copy. If you have never written Go before, read
[32](xrootd/example/xrd-ex-32-simple-api) and stop there; otherwise read 01, 12
and 18.

**A conformance suite** covering every client surface, including a fail-closed
half that asserts what the client refuses to do, plus fixes for the client and
server bugs it found along the way.

## The numbers out of a ROOT file, in one call

Storage was only half the wall. Getting a column out of a `TTree` meant
`groot.Open`, a key lookup, a type assertion, a `[]rtree.ReadVar` bound to
pointers of exactly the right Go type, a reader and a callback — five concepts
before the first number. The [`rdata`](groot/rdata) package is that job in one
line:

```go
t, err := rdata.Open("root://server//data/run*.root")  // a path, a URL, or a pattern
defer t.Close()

pt, err := t.Floats("pt")           // one number per event, whatever the file stores it as
jets, err := t.Arrays("jet_pt")     // several per event; the length is how many there were
names := t.Columns()                // for a file nobody documented

err = rdata.Save("out.root", "results",  // and the answer goes back out the same way
    rdata.Column{Name: "mass", Data: masses},
)
```

The tree need not be named when the file holds one, which is the usual case;
when it holds more, the error says what they are called, sub-directories
included. Many files — named, or matched by a pattern, local or on the grid —
read end to end as one table. `Each` walks an event at a time and holds
nothing, for when the file is larger than the machine. And because the audience
is people who have not written Go before, a wrong call says which call would
have worked: ask for text as a number and the error names `Strings`; ask for a
jet collection as one number and it names `Arrays`.

Underneath, it is still `groot` and `rtree`, and both are one import away for
the cases this deliberately does not cover. One thing did have to be fixed on
the way down: opening a local ROOT file that was not there reported *no ROOT
plugin for scheme=""* instead of the reason the filesystem gave, so
`errors.Is(err, fs.ErrNotExist)` — the check every Go program makes — was
false. It is now the filesystem's own error.

## Elsewhere in Go-HEP

- **fastjet** — jet areas (active, with explicit ghosts), median background
  estimation and subtraction, and an *O*(*N*²) strategy that makes them
  affordable.
- **fads** — jet areas and background density in `FastJetFinder`, which
  previously declared the Delphes properties and panicked behind all three of
  them; also a fix for per-jet extents that were accumulated across the event.
- **hbook** — efficiencies carrying the uncertainty a ratio of counts actually
  has, rather than a Poisson one.
- **hplot** — uncertainty bands around pre-, mid- and post-step lines.
- **hepmc** — reads HepMC3 Asciiv3 event listings.
- **groot, hbook** — merging 2-dimensional histograms and multigraphs; remote
  files opened through the hardened XRootD transports.
- **heppdt** — the constituent-flavour predicates.
- **rio, lcio** — a corrupt stream is reported instead of mis-read; the LCIO
  index writer exists, and its 64-bit-offset control-word tests are no longer
  always false.

Everything is tested; `gofmt`, `go vet` and `go test ./...` are clean.

Go-HEP
------

`hep` is a set of libraries and tools to perform High Energy Physics analyses with ease and [Go](https://golang.org)

See [go-hep.org](https://go-hep.org) for more informations.

## Forum

Drop an email at [~sbinet/go-hep@lists.sr.ht](mailto:~sbinet/go-hep@lists.sr.ht) or visit the web interface [lists.sr.ht/~sbinet/go-hep](https://lists.sr.ht/~sbinet/go-hep) to discuss about `Go-HEP` or ask for help.


## License

`hep` is released under the `BSD-3` license.

## Documentation

Documentation for `hep` is served by [GoDoc](https://pkg.go.dev/go-hep.org/x/hep).

## Contributing

Guidelines for contributing to [go-hep](https://go-hep.org) are available here:
 [go-hep.org/contributing](https://go-hep.org/contributing)
 
### Contributors

This project exists thanks to all the people who contribute. 

# Motivations

Writing analyses in HEP involves many steps and one needs a few tools to
successfully carry out such an endeavour.
But - at minima - one needs to be able to read (and possibly write) ROOT files
to be able to interoperate with the rest of the HEP community or to insert
one's work into an already existing analysis pipeline.

Go-HEP provides this necessary interoperability layer, in the Go programming
language.
This allows physicists to leverage the great concurrency primitives of Go,
together with the surrounding tooling and software engineering ecosystem of Go,
to implement physics analyses.

## Content

Go-HEP currently sports the following packages:

- [go-hep.org/x/hep/brio](https://go-hep.org/x/hep/brio): a toolkit to generate serialization code
- [go-hep.org/x/hep/fads](https://go-hep.org/x/hep/fads): a fast detector simulation toolkit
- [go-hep.org/x/hep/fastjet](https://go-hep.org/x/hep/fastjet): a jet clustering algorithms package (WIP)
- [go-hep.org/x/hep/fit](https://go-hep.org/x/hep/fit): a fitting function toolkit (WIP)
- [go-hep.org/x/hep/fmom](https://go-hep.org/x/hep/fmom): a 4-vectors library
- [go-hep.org/x/hep/fwk](https://go-hep.org/x/hep/fwk): a concurrency-enabled framework
- [go-hep.org/x/hep/groot](https://go-hep.org/x/hep/groot): a pure [Go](https://golang.org) package for [ROOT](https://root.cern.ch) I/O (WIP)
- [go-hep.org/x/hep/hbook](https://go-hep.org/x/hep/hbook): histograms and n-tuples (WIP)
- [go-hep.org/x/hep/hplot](https://go-hep.org/x/hep/hplot): interactive plotting (WIP)
- [go-hep.org/x/hep/hepmc](https://go-hep.org/x/hep/hepmc): `HepMC` in pure [Go](https://golang.org) (EDM + I/O)
- [go-hep.org/x/hep/hepevt](https://go-hep.org/x/hep/hepevt): `HEPEVT` bindings
- [go-hep.org/x/hep/heppdt](https://go-hep.org/x/hep/heppdt): `HEP` particle data table
- [go-hep.org/x/hep/lcio](https://go-hep.org/x/hep/lcio): read/write support for `LCIO` event data model
- [go-hep.org/x/hep/lhef](https://go-hep.org/x/hep/lhef): Les Houches Event File format
- [go-hep.org/x/hep/rio](https://go-hep.org/x/hep/rio): `go-hep` record oriented I/O
- [go-hep.org/x/hep/sio](https://go-hep.org/x/hep/sio): basic, low-level, serial I/O used by `LCIO`
- [go-hep.org/x/hep/slha](https://go-hep.org/x/hep/slha): `SUSY` Les Houches Accord I/O
- [go-hep.org/x/hep/xrootd](https://go-hep.org/x/hep/xrootd): [XRootD](http://xrootd.org) client in pure [Go](https://golang.org)

## Installation

Go-HEP packages are installable via the `go get` command:

```sh
$ go get go-hep.org/x/hep/fads
```

Just select the package you are interested in and `go get` will take care of fetching, building and installing it, as well as its dependencies, recursively.
