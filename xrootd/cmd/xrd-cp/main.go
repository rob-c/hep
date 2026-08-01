// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command xrd-cp copies files and directories from a remote xrootd server
// to local storage.
//
// Usage:
//
//	$> xrd-cp [OPTIONS] <src-1> [<src-2> [...]] <dst>
//
// Example:
//
//	$> xrd-cp root://server.example.com/some/file1.txt .
//	$> xrd-cp root://gopher@server.example.com/some/file1.txt .
//	$> xrd-cp root://server.example.com/some/file1.txt foo.txt
//	$> xrd-cp root://server.example.com/some/file1.txt - > foo.txt
//	$> xrd-cp -r root://server.example.com/some/dir .
//	$> xrd-cp -r root://server.example.com/some/dir outdir
//
// Options:
//
//	-r	copy directories recursively
//	-v	enable verbose mode
//	-no-prompt
//		never ask for a missing credential
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	stdpath "path"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdcred"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdio"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

// localName is the name a remote path copies to on disk: its base name with the
// opaque data left behind, so an authorization token does not end up as part of
// a file name.
func localName(src string) string {
	name, _ := xrdproto.SplitPath(src)
	return stdpath.Base(name)
}

const usage = `xrd-cp copies files and directories from a remote xrootd server to local storage.

Usage:

 $> xrd-cp [OPTIONS] <src-1> [<src-2> [...]] <dst>

Example:

 $> xrd-cp root://server.example.com/some/file1.txt .
 $> xrd-cp root://gopher@server.example.com/some/file1.txt .
 $> xrd-cp root://server.example.com/some/file1.txt foo.txt
 $> xrd-cp root://server.example.com/some/file1.txt - > foo.txt
 $> xrd-cp -r root://server.example.com/some/dir .
 $> xrd-cp -r root://server.example.com/some/dir outdir

Options:
`

func main() {
	os.Exit(run(os.Stdout, os.Stderr, os.Args[1:]))
}

func run(stdout, stderr io.Writer, args []string) int {
	fset := flag.NewFlagSet("xrd-cp", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {
		fmt.Fprint(stderr, usage)
		fset.PrintDefaults()
	}

	var (
		recFlag      = fset.Bool("r", false, "copy directories recursively")
		verboseFlag  = fset.Bool("v", false, "enable verbose mode")
		noPromptFlag = fset.Bool("no-prompt", false, "never ask for a missing credential")
	)

	switch err := fset.Parse(args); {
	case err == nil:
		// ok.
	case errors.Is(err, flag.ErrHelp):
		return 0
	default:
		fmt.Fprintf(stderr, "xrd-cp: could not parse arguments: %+v\n", err)
		return 1
	}

	// This is a command a person runs, so it may ask that person for a
	// credential the server wants and the client cannot find. The prompter
	// declines by itself when there is no terminal, which is what makes it safe
	// to enable unconditionally here.
	if !*noPromptFlag {
		xrootd.SetDefaultCredentialPrompt(xrdcred.NewTerminal())
	}

	switch n := fset.NArg(); n {
	case 0:
		fmt.Fprintf(stderr, "xrd-cp: missing file operand\n\n")
		fset.Usage()
		return 1
	case 1:
		fmt.Fprintf(stderr, "xrd-cp: missing destination file operand after %q\n\n", fset.Arg(0))
		fset.Usage()
		return 1
	}

	dst := fset.Arg(fset.NArg() - 1)
	for _, src := range fset.Args()[:fset.NArg()-1] {
		err := xrdcopy(stdout, stderr, dst, src, *recFlag, *verboseFlag)
		if err != nil {
			fmt.Fprintf(stderr, "xrd-cp: could not copy %q to %q: %+v\n", src, dst, err)
			return 1
		}
	}

	return 0
}

func xrdcopy(stdout, stderr io.Writer, dst, srcPath string, recursive, verbose bool) error {
	cli, src, err := xrdremote(srcPath)
	if err != nil {
		return err
	}
	defer cli.Close()

	ctx := context.Background()

	fs := cli.FS()
	var jobs jobs
	var addDir func(root, src string) error

	addDir = func(root, src string) error {
		fi, err := fs.Stat(ctx, src)
		if err != nil {
			return fmt.Errorf("could not stat remote src: %w", err)
		}
		switch {
		case fi.IsDir():
			if !recursive {
				return fmt.Errorf("xrd-cp: -r not specified; omitting directory %q", src)
			}
			dst := stdpath.Join(root, localName(src))
			err = os.MkdirAll(dst, 0755)
			if err != nil {
				return fmt.Errorf("could not create output directory: %w", err)
			}

			ents, err := fs.Dirlist(ctx, src)
			if err != nil {
				return fmt.Errorf("could not list directory: %w", err)
			}
			for _, e := range ents {
				err = addDir(dst, xrdproto.JoinPath(src, e.Name()))
				if err != nil {
					return err
				}
			}
		default:
			jobs.add(job{
				fs:     fs,
				src:    src,
				dst:    stdpath.Join(root, localName(src)),
				stdout: stdout,
			})
		}
		return nil
	}

	fiSrc, err := fs.Stat(ctx, src)
	if err != nil {
		return fmt.Errorf("could not stat remote src: %w", err)
	}

	fiDst, errDst := os.Stat(dst)
	switch {
	case fiSrc.IsDir():
		switch {
		case errDst != nil && os.IsNotExist(errDst):
			err = os.MkdirAll(dst, 0755)
			if err != nil {
				return fmt.Errorf("could not create output directory: %w", err)
			}
			ents, err := fs.Dirlist(ctx, src)
			if err != nil {
				return fmt.Errorf("could not list directory: %w", err)
			}
			for _, e := range ents {
				err = addDir(dst, xrdproto.JoinPath(src, e.Name()))
				if err != nil {
					return err
				}
			}

		case errDst != nil:
			return fmt.Errorf("could not stat local dst: %w", errDst)
		case fiDst.IsDir():
			err = addDir(dst, src)
			if err != nil {
				return err
			}
		}

	default:
		switch {
		case errDst != nil && os.IsNotExist(errDst):
			// ok... dst will be the output file.
		case errDst != nil:
			return fmt.Errorf("could not stat local dst: %w", errDst)
		case fiDst.IsDir():
			dst = stdpath.Join(dst, localName(src))
		}

		jobs.add(job{
			fs:     fs,
			src:    src,
			dst:    dst,
			stdout: stdout,
		})
	}

	n, err := jobs.run(ctx)
	if verbose {
		fmt.Fprintf(stderr, "xrd-cp: transferred %d bytes\n", n)
	}
	return err
}

func xrdremote(name string) (client *xrootd.Client, path string, err error) {
	url, err := xrdio.Parse(name)
	if err != nil {
		return nil, "", fmt.Errorf("could not parse %q: %w", name, err)
	}

	path = url.Path
	client, err = xrootd.NewClient(context.Background(), url.Addr, url.User)
	return client, path, err
}

type job struct {
	fs  xrdfs.FileSystem
	src string
	dst string
	// stdout receives the transfer when the destination is "-".
	stdout io.Writer
}

func (j job) run(ctx context.Context, bufs *copyBuffers) (int, error) {
	var (
		o   io.WriteCloser
		err error
	)
	switch j.dst {
	case "-", "":
		o = nopWriteCloser{j.stdout}
	case ".":
		j.dst = localName(j.src)
		fallthrough
	default:
		o, err = os.Create(j.dst)
		if err != nil {
			return 0, fmt.Errorf("could not create output file: %w", err)
		}
	}
	defer o.Close()

	f, err := xrdio.OpenFrom(j.fs, j.src)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	// A copy that has to ask the server for the size of the file is a copy of
	// a file whose size the server would not say. Zero asks for the smallest
	// buffer, which costs a large transfer some extra round trips and costs a
	// small one nothing.
	var size int64
	if fi, err := f.Stat(); err == nil {
		size = fi.Size()
	}

	n, err := io.CopyBuffer(o, f, bufs.get(size))
	if err != nil {
		return int(n), fmt.Errorf("could not copy to output file: %w", err)
	}

	err = o.Close()
	if err != nil {
		return int(n), fmt.Errorf("could not close output file: %w", err)
	}

	return int(n), nil
}

type jobs struct {
	slice []job
	bufs  copyBuffers
}

// copyBuffers hands out the buffer a transfer copies through. One buffer serves
// every job in the run: only one of them is ever in flight, and a recursive
// copy that allocated a fresh buffer per file would ask the collector to keep
// up with one allocation per file for no gain.
type copyBuffers struct {
	buf []byte
}

// get returns a buffer to copy a file of size bytes through, growing the shared
// one if this file wants more room than the last did.
//
// A buffer larger than the file is memory that is never written to, and a
// buffer much smaller turns one transfer into many reads, each paying a round
// trip to the server. So the buffer follows the file up to a ceiling past which
// a larger one buys no more throughput.
func (c *copyBuffers) get(size int64) []byte {
	const (
		floor = 128 * 1024
		limit = 16 * 1024 * 1024
	)

	n := int64(floor)
	switch {
	case size > limit:
		n = limit
	case size > floor:
		n = size
	}

	if int64(len(c.buf)) < n {
		c.buf = make([]byte, n)
	}
	return c.buf[:n]
}

func (js *jobs) add(j job) {
	js.slice = append(js.slice, j)
}

func (js *jobs) run(ctx context.Context) (int, error) {
	var n int
	for _, j := range js.slice {
		nn, err := j.run(ctx, &js.bufs)
		n += nn
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// nopWriteCloser adapts the command's stdout to the WriteCloser a job writes
// to; closing it must not close the caller's stream.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
