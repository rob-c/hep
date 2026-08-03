// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command xrd-fs does the everyday things to files on remote storage, from a
// shell, without writing a program.
//
// Usage:
//
//	$> xrd-fs <command> [options] <name> [<name> [...]]
//
// Commands:
//
//	check	say whether a name can be reached and used
//	stat	size, modification time and kind
//	du	total size of a file or of everything under a directory
//	cat	write the contents of a file to the terminal
//	find	every file under a directory, however deep
//	mkdir	create a directory, and any of its parents that are missing
//	rm	delete a file, or with -r a directory and its contents
//	mv	rename a file or directory on one server
//
// Example:
//
//	$> xrd-fs check root://server.example.com//store/user/gopher
//	$> xrd-fs du root://server.example.com//store/user/gopher
//	$> xrd-fs cat root://server.example.com//store/user/gopher/runs.txt
//	$> xrd-fs rm "root://server.example.com//store/user/gopher/*.tmp"
//
// Every name may be a URL or a path on this machine, so the same command works
// on both. A name may also be a pattern: "*" and "?" match inside one path
// component, "**" matches across them and "[abc]" is a character class. Quote
// it, or the local shell will try to expand it against the local filesystem
// first.
//
// Listing a directory is xrd-ls, and copying is xrd-cp.
//
// Options:
//
//	-b	print sizes in bytes rather than rounded (du, stat)
//	-r	delete a directory and everything under it (rm)
//	-no-prompt
//		never ask for a missing credential
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrd"
	"go-hep.org/x/hep/xrootd/xrdcred"
)

const usage = `xrd-fs does the everyday things to files on remote storage.

Usage:

 $> xrd-fs <command> [options] <name> [<name> [...]]

Commands:

 check   say whether a name can be reached and used
 stat    size, modification time and kind
 du      total size of a file or of everything under a directory
 cat     write the contents of a file to the terminal
 find    every file under a directory, however deep
 mkdir   create a directory, and any of its parents that are missing
 rm      delete a file, or with -r a directory and its contents
 mv      rename a file or directory on one server

Example:

 $> xrd-fs check root://server.example.com//store/user/gopher
 $> xrd-fs du root://server.example.com//store/user/gopher
 $> xrd-fs cat root://server.example.com//store/user/gopher/runs.txt
 $> xrd-fs rm "root://server.example.com//store/user/gopher/*.tmp"

Every name may be a URL or a path on this machine. A name may also be a
pattern: "*" and "?" match inside one path component, "**" matches across them
and "[abc]" is a character class. Quote it, or the local shell will try to
expand it against the local filesystem first.

Listing a directory is xrd-ls, and copying is xrd-cp.

Options:
`

func main() {
	os.Exit(run(os.Stdout, os.Stderr, os.Args[1:]))
}

func run(stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(stderr, "xrd-fs: missing command\n\n%s", usage)
		return 1
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	case "check", "stat", "du", "cat", "find", "mkdir", "rm", "mv":
		// ok.
	default:
		fmt.Fprintf(stderr, "xrd-fs: %q is not one of its commands\n\n%s", cmd, usage)
		return 1
	}

	fset := flag.NewFlagSet("xrd-fs "+cmd, flag.ContinueOnError)
	fset.SetOutput(stderr)
	fset.Usage = func() {
		fmt.Fprint(stderr, usage)
		fset.PrintDefaults()
	}

	var (
		noPromptFlag = fset.Bool("no-prompt", false, "never ask for a missing credential")
		recFlag      = fset.Bool("r", false, "delete a directory and everything under it")
		bytesFlag    = fset.Bool("b", false, "print sizes in bytes rather than rounded")
	)

	switch err := fset.Parse(rest); {
	case err == nil:
		// ok.
	case errors.Is(err, flag.ErrHelp):
		return 0
	default:
		fmt.Fprintf(stderr, "xrd-fs: could not parse arguments: %+v\n", err)
		return 1
	}

	names := fset.Args()
	if len(names) == 0 {
		fmt.Fprintf(stderr, "xrd-fs: %s needs something to work on\n\n%s", cmd, usage)
		return 1
	}

	// This is a command a person runs, so it may ask that person for a
	// credential the server wants and the client cannot find. The prompter
	// declines by itself when there is no terminal, which is what makes it
	// safe to enable unconditionally here.
	if !*noPromptFlag {
		xrootd.SetDefaultCredentialPrompt(xrdcred.NewTerminal())
	}
	defer xrd.Close()

	// The errors from the xrd package name the operation, the file and what to
	// check, so they are printed as they are rather than wrapped in another
	// layer of prefix.
	err := dispatch(stdout, cmd, names, options{recursive: *recFlag, exact: *bytesFlag})
	if err != nil {
		fmt.Fprintf(stderr, "%+v\n", err)
		return 1
	}
	return 0
}

// options are the flags that reach a command.
type options struct {
	recursive bool // rm: delete a directory and its contents
	exact     bool // du, stat: sizes in bytes
}

func dispatch(w io.Writer, cmd string, names []string, opts options) error {
	switch cmd {
	case "check":
		return check(w, names)
	case "stat":
		return stat(w, names, opts)
	case "du":
		return du(w, names, opts)
	case "cat":
		return cat(w, names)
	case "find":
		return find(w, names)
	case "mkdir":
		return mkdir(names)
	case "rm":
		return rm(names, opts)
	case "mv":
		return mv(names)
	}
	panic("xrd-fs: unreachable command " + cmd) // run has already refused anything else
}

func check(w io.Writer, names []string) error {
	for _, name := range names {
		if err := xrd.Check(name); err != nil {
			return err
		}
		fmt.Fprintf(w, "%s: ok\n", name)
	}
	return nil
}

func stat(w io.Writer, names []string, opts options) error {
	names, err := expand(names)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	defer tw.Flush()

	for _, name := range names {
		fi, err := xrd.Stat(name)
		if err != nil {
			return err
		}
		kind := "file"
		if fi.IsDir() {
			kind = "dir"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", kind, size(fi.Size(), opts.exact), fi.ModTime().Format(time.RFC3339), name)
	}
	return nil
}

func du(w io.Writer, names []string, opts options) error {
	names, err := expand(names)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	defer tw.Flush()

	for _, name := range names {
		n, err := xrd.Size(name)
		if err != nil {
			return err
		}
		fmt.Fprintf(tw, "%s\t%s\n", size(n, opts.exact), name)
	}
	return nil
}

func cat(w io.Writer, names []string) error {
	names, err := expand(names)
	if err != nil {
		return err
	}

	for _, name := range names {
		f, err := xrd.Open(name)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, f)
		f.Close()
		if err != nil {
			return fmt.Errorf("xrd-fs: could not read %q: %w", name, err)
		}
	}
	return nil
}

func find(w io.Writer, names []string) error {
	for _, name := range names {
		files, err := xrd.Find(name)
		if err != nil {
			return err
		}
		for _, file := range files {
			fmt.Fprintln(w, file)
		}
	}
	return nil
}

func mkdir(names []string) error {
	for _, name := range names {
		if err := xrd.Mkdir(name); err != nil {
			return err
		}
	}
	return nil
}

func rm(names []string, opts options) error {
	names, err := expand(names)
	if err != nil {
		return err
	}

	for _, name := range names {
		var err error
		switch {
		case opts.recursive:
			err = xrd.RemoveAll(name)
		default:
			err = xrd.Remove(name)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func mv(names []string) error {
	if len(names) != 2 {
		return fmt.Errorf("xrd-fs: mv takes the old name and the new one, and was given %d", len(names))
	}
	return xrd.Rename(names[0], names[1])
}

// expand turns the operands that are patterns into the names they match, and
// leaves the rest alone. A pattern that matches nothing is an error here,
// where it is not in the library: a shell command that was asked to delete
// "*.tmp" and found none should say so rather than succeed silently.
func expand(names []string) ([]string, error) {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if !strings.ContainsAny(name, "*?[") {
			out = append(out, name)
			continue
		}
		matches, err := xrd.Glob(name)
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("xrd-fs: nothing matches %q", name)
		}
		out = append(out, matches...)
	}
	return out, nil
}

// size is a number of bytes as a person reads it, or exactly, when they have
// asked for the number itself.
func size(n int64, exact bool) string {
	const unit = 1024
	if exact || n < unit {
		return fmt.Sprintf("%d B", n)
	}

	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
