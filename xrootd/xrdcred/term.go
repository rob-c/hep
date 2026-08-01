// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdcred // import "go-hep.org/x/hep/xrootd/xrdcred"

import (
	"context"
	"fmt"
	"os"

	"golang.org/x/term"
)

// isTerminal reports whether f is a terminal a user could answer on.
//
// A nil *os.File is not: it is what a test or a daemon has instead of a
// terminal, and the point of the check is that such a caller falls straight
// through to the client's normal behaviour rather than blocking.
func isTerminal(f *os.File) bool {
	return f != nil && term.IsTerminal(int(f.Fd()))
}

// readLine reads one line, honouring ctx.
//
// The read runs on its own goroutine because a read from a terminal cannot be
// interrupted by a context: if the caller's deadline expires while the user is
// thinking, the caller is freed at once and the goroutine ends when the user
// eventually presses return. That is one goroutine and one line of input, both
// discarded, which is the right price for not stranding an operation behind a
// question nobody answered.
func readLine(ctx context.Context, f *os.File) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := readLineFrom(f)
		ch <- result{line, err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		return r.line, r.err
	}
}

// readLineFrom reads to the next newline a byte at a time.
//
// Byte-at-a-time is deliberate: a buffered reader would read ahead past the
// newline, and the bytes it swallowed would be exactly the ones a subsequent
// no-echo read needs. A terminal delivers a line at a time anyway, so the extra
// syscalls cost nothing that matters at human speed.
func readLineFrom(f *os.File) (string, error) {
	var (
		line []byte
		b    [1]byte
	)
	for {
		n, err := f.Read(b[:])
		if n == 1 {
			if b[0] == '\n' {
				return string(line), nil
			}
			line = append(line, b[0])
			continue
		}
		if err != nil {
			if len(line) != 0 {
				return string(line), nil
			}
			return "", fmt.Errorf("xrdcred: could not read the answer: %w", err)
		}
	}
}

// readSecret reads a line without echoing it.
func readSecret(ctx context.Context, f *os.File) (string, error) {
	type result struct {
		secret string
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		raw, err := term.ReadPassword(int(f.Fd()))
		if err != nil {
			ch <- result{"", fmt.Errorf("xrdcred: could not read the credential: %w", err)}
			return
		}
		ch <- result{string(raw), nil}
	}()

	select {
	case <-ctx.Done():
		// The terminal is left as ReadPassword found it: it restores the echo
		// state itself when its read returns.
		return "", ctx.Err()
	case r := <-ch:
		return r.secret, r.err
	}
}
