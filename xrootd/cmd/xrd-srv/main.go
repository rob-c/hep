// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command xrd-srv serves data from a local filesystem over the XRootD protocol.
package main // import "go-hep.org/x/hep/xrootd/cmd/xrd-srv"

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"

	"go-hep.org/x/hep/xrootd"
)

const usage = `xrd-srv serves data from a local filesystem over the XRootD protocol.

Usage:

 $> xrd-srv [OPTIONS] <base-dir>

Example:

 $> xrd-srv /tmp
 $> xrd-srv -addr=0.0.0.0:1094 /tmp

Options:
`

func main() {
	os.Exit(run(os.Stdout, os.Stderr, os.Args[1:]))
}

func run(stdout, stderr io.Writer, args []string) int {
	fset := flag.NewFlagSet("xrd-srv", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {
		fmt.Fprint(stderr, usage)
		fset.PrintDefaults()
	}

	addr := fset.String("addr", "0.0.0.0:1094", "listen to the provided address")

	switch err := fset.Parse(args); {
	case err == nil:
		// ok.
	case errors.Is(err, flag.ErrHelp):
		return 0
	default:
		fmt.Fprintf(stderr, "xrd-srv: could not parse arguments: %+v\n", err)
		return 1
	}

	if fset.NArg() != 1 {
		fmt.Fprintf(stderr, "xrd-srv: missing base dir operand\n\n")
		fset.Usage()
		return 1
	}

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintf(stderr, "xrd-srv: could not listen on %q: %+v\n", *addr, err)
		return 1
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	defer signal.Stop(quit)

	return serve(stdout, stderr, listener, fset.Arg(0), quit)
}

// serve runs a server over listener until quit fires, then shuts it down. It is
// kept apart from run so the serving loop can be driven without a signal.
func serve(stdout, stderr io.Writer, listener net.Listener, baseDir string, quit <-chan os.Signal) int {
	srv := xrootd.NewServer(xrootd.NewFSHandler(baseDir), func(err error) {
		fmt.Fprintf(stderr, "xrd-srv: an error occured: %+v\n", err)
	})

	errc := make(chan error, 1)
	go func() {
		fmt.Fprintf(stdout, "xrd-srv: listening on %v...\n", listener.Addr())
		errc <- srv.Serve(listener)
	}()

	select {
	case err := <-errc:
		if err != nil && !errors.Is(err, xrootd.ErrServerClosed) {
			fmt.Fprintf(stderr, "xrd-srv: could not serve: %+v\n", err)
			return 1
		}
	case <-quit:
		if err := srv.Shutdown(context.Background()); err != nil {
			fmt.Fprintf(stderr, "xrd-srv: could not shutdown: %+v\n", err)
			return 1
		}
	}

	return 0
}
