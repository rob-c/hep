# groot

[![GoDoc](https://godoc.org/go-hep.org/x/hep/groot?status.svg)](https://godoc.org/go-hep.org/x/hep/groot)

Experimental, pure-Go package to read and write ROOT files, without having ROOT installed.

## Installation

```sh
go get go-hep.org/x/hep/groot
```

## Getting the numbers out

If what you want is a column of a `TTree`, start at
[`groot/rdata`](rdata): it finds the tree, reads a column as a slice of
numbers, and takes a local path, a URL on grid storage or a pattern matching
many files.

```go
t, err := rdata.Open("data.root")
defer t.Close()

pt, err := t.Floats("pt")
```

`groot` and [`groot/rtree`](rtree) are underneath it, for everything else a
ROOT file can hold.

## Documentation

``groot`` documentation can be found over there:

https://godoc.org/go-hep.org/x/hep/groot
