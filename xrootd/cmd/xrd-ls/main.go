// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command xrd-ls lists directory contents on a remote xrootd server.
//
// Usage:
//
//	$> xrd-ls [OPTIONS] <dir-1> [<dir-2> [...]]
//
// Example:
//
//	$> xrd-ls root://server.example.com/some/dir
//	$> xrd-ls -l root://server.example.com/some/dir
//	$> xrd-ls -R root://server.example.com/some/dir
//	$> xrd-ls -l -R root://server.example.com/some/dir
//
// Options:
//
//	-R	list subdirectories recursively
//	-l	use a long listing format
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
	"text/tabwriter"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdcred"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdio"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

const usage = `xrd-ls lists directory contents on a remote xrootd server.

Usage:

 $> xrd-ls [OPTIONS] <dir-1> [<dir-2> [...]]

Example:

 $> xrd-ls root://server.example.com/some/dir
 $> xrd-ls -l root://server.example.com/some/dir
 $> xrd-ls -R root://server.example.com/some/dir
 $> xrd-ls -l -R root://server.example.com/some/dir

Options:
`

func main() {
	os.Exit(run(os.Stdout, os.Stderr, os.Args[1:]))
}

func run(stdout, stderr io.Writer, args []string) int {
	fset := flag.NewFlagSet("xrd-ls", flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {
		fmt.Fprint(stderr, usage)
		fset.PrintDefaults()
	}

	var (
		recFlag      = fset.Bool("R", false, "list subdirectories recursively")
		longFlag     = fset.Bool("l", false, "use a long listing format")
		noPromptFlag = fset.Bool("no-prompt", false, "never ask for a missing credential")
	)

	switch err := fset.Parse(args); {
	case err == nil:
		// ok.
	case errors.Is(err, flag.ErrHelp):
		return 0
	default:
		fmt.Fprintf(stderr, "xrd-ls: could not parse arguments: %+v\n", err)
		return 1
	}

	// This is a command a person runs, so it may ask that person for a
	// credential the server wants and the client cannot find. The prompter
	// declines by itself when there is no terminal, which is what makes it safe
	// to enable unconditionally here.
	if !*noPromptFlag {
		xrootd.SetDefaultCredentialPrompt(xrdcred.NewTerminal())
	}

	if fset.NArg() == 0 {
		fmt.Fprintf(stderr, "xrd-ls: missing directory operand\n\n")
		fset.Usage()
		return 1
	}

	for i, dir := range fset.Args() {
		if i > 0 {
			// separate consecutive files by an empty line
			fmt.Fprintf(stdout, "\n")
		}
		err := xrdls(stdout, dir, *longFlag, *recFlag)
		if err != nil {
			fmt.Fprintf(stderr, "xrd-ls: could not list %q content: %+v\n", dir, err)
			return 1
		}
	}

	return 0
}

func xrdls(w io.Writer, name string, long, recursive bool) error {
	url, err := xrdio.Parse(name)
	if err != nil {
		return fmt.Errorf("could not parse %q: %w", name, err)
	}

	ctx := context.Background()

	c, err := xrootd.NewClient(ctx, url.Addr, url.User)
	if err != nil {
		return fmt.Errorf("could not create client: %w", err)
	}
	defer c.Close()

	fs := c.FS()

	fi, err := fs.Stat(ctx, url.Path)
	if err != nil {
		return fmt.Errorf("could not stat %q: %w", url.Path, err)
	}

	// kXR_stat answers about a path without echoing it, so fi is nameless and
	// url.Path is the whole of what it is called. That is why it is passed as
	// the root below and why format falls back to the root when an entry has no
	// name of its own: what the user asked for by full path is listed by full
	// path, and what was found inside a directory is listed by its name within
	// it, exactly as ls does.
	err = display(ctx, w, fs, url.Path, fi, long, recursive)
	if err != nil {
		return err
	}

	return nil
}

func display(ctx context.Context, w io.Writer, fs xrdfs.FileSystem, root string, fi os.FileInfo, long, recursive bool) error {
	if !fi.IsDir() {
		// A nameless entry is the one the caller named: see run.
		format(w, root, fi, long)
		return nil
	}

	end := ""
	if recursive {
		end = ":"
	}

	dir := xrdproto.JoinPath(root, fi.Name())
	fmt.Fprintf(w, "%s%s\n", dir, end)
	if long {
		fmt.Fprintf(w, "total %d\n", fi.Size())
	}
	ents, err := fs.Dirlist(ctx, dir)
	if err != nil {
		return fmt.Errorf("could not list dir %q: %w", dir, err)
	}
	o := tabwriter.NewWriter(w, 8, 4, 0, ' ', tabwriter.AlignRight)
	for _, e := range ents {
		format(o, dir, e, long)
	}
	o.Flush()
	if recursive {
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			// make an empty line before going into a subdirectory.
			fmt.Fprintf(w, "\n")
			err := display(ctx, w, fs, dir, e, long, recursive)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func format(o io.Writer, root string, fi os.FileInfo, long bool) {
	if !long {
		fmt.Fprintf(o, "%s\n", xrdproto.JoinPath(root, fi.Name()))
		return
	}

	// An entry with no name is the path the caller asked about, which the
	// server stats without echoing; root is what they called it.
	name := fi.Name()
	if name == "" {
		name = root
	}
	fmt.Fprintf(o, "%v\t %d\t %s\t %s\n", fi.Mode(), fi.Size(), fi.ModTime().Format("Jan 02 15:04"), name)
}
