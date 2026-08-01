// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux

package xrdcred

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd"
	"golang.org/x/sys/unix"
)

// pty opens a pseudo-terminal and returns both ends.
//
// The rest of this suite drives the prompter through a pipe, which answers
// every question except the one that matters most: a pipe is not a terminal, so
// it never reaches the code that only runs on one — the terminal check itself,
// and the no-echo read that a pasted bearer token depends on. Those are exactly
// the paths whose failure is silent, and a real terminal is the only way to see
// them.
func pty(t *testing.T) (master, slave *os.File) {
	t.Helper()

	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminals on this machine: %v", err)
	}
	t.Cleanup(func() { master.Close() })

	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatalf("could not unlock the terminal: %v", err)
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatalf("could not name the terminal: %v", err)
	}

	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("could not open the terminal: %v", err)
	}
	t.Cleanup(func() { slave.Close() })

	return master, slave
}

// typed sends what a user would type, once the prompt has been put.
func typed(t *testing.T, master *os.File, text string) {
	t.Helper()
	go func() {
		if _, err := master.WriteString(text); err != nil {
			t.Errorf("could not type %q: %v", text, err)
		}
	}()
}

func TestConformance_ARealTerminalIsWhereTheQuestionIsPut(t *testing.T) {
	master, slave := pty(t)

	out := new(strings.Builder)
	term := &Terminal{In: slave, Out: out, NoExec: true}
	typed(t, master, "n\n")

	_, err := term.PromptCredential(context.Background(), xrootd.CredentialRequest{
		Provider: "gsi",
		Server:   "eos.example.org:1094",
		Offered:  []string{"gsi", "unix"},
	})
	if err != ErrDeclined {
		t.Fatalf("the answer gave %v, want %v", err, ErrDeclined)
	}
	if got := out.String(); !strings.Contains(got, "eos.example.org:1094") {
		t.Fatalf("the question does not say who is asking:\n%s", got)
	}
}

func TestConformance_APastedTokenIsNeverShownOnTheTerminal(t *testing.T) {
	// A bearer token is the whole credential and a terminal keeps its
	// scrollback, so it must not be echoed as it is typed. Nothing but a real
	// terminal can show that: on a pipe there is no echo to switch off, so the
	// test would pass on a prompter that never switched it off at all.
	master, slave := pty(t)

	const secret = "eyJhbGciOiJub25lIn0.token"
	out := new(strings.Builder)
	term := &Terminal{In: slave, Out: out}
	typed(t, master, secret+"\n")

	a, err := pasteToken(context.Background(), term, xrootd.CredentialRequest{Provider: "ztn"})
	if err != nil {
		t.Fatalf("the token was not accepted: %v", err)
	}
	req, err := a.Request(nil)
	if err != nil {
		t.Fatalf("the credential could not be used: %v", err)
	}
	if got, want := req.Credentials, "ztn\x00"+secret; got != want {
		t.Fatalf("the credential carries %q, want %q", got, want)
	}
	if strings.Contains(out.String(), secret) {
		t.Fatalf("the token was printed:\n%s", out)
	}
	if got := out.String(); !strings.Contains(got, fmt.Sprintf("%d characters", len(secret))) {
		t.Fatalf("the terminal does not confirm the token was read:\n%s", got)
	}

	// What the terminal echoed back is what a passer-by would have seen. The
	// read is non-blocking because the answer this test wants is usually that
	// there is nothing to read at all.
	// The descriptor is taken once: os.File.Fd puts it back into blocking mode
	// every time it is called, so asking for it again would undo this.
	fd := int(master.Fd())
	if err := unix.SetNonblock(fd, true); err != nil {
		t.Fatalf("could not bound the read: %v", err)
	}
	buf := make([]byte, 256)
	n, err := unix.Read(fd, buf)
	switch {
	case err == unix.EAGAIN, n <= 0:
	case err != nil:
		t.Fatalf("could not read back what the terminal echoed: %v", err)
	case strings.Contains(string(buf[:n]), secret):
		t.Fatalf("the terminal echoed the token: %q", buf[:n])
	}
}

func TestConformance_ATerminalWithNoneNamedIsTheProcessTerminal(t *testing.T) {
	// The zero Terminal is the one a command builds, and it has to find the
	// process's own terminal rather than nothing at all.
	master, slave := pty(t)

	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatalf("could not redirect the output: %v", err)
	}

	oldIn, oldErr := os.Stdin, os.Stderr
	os.Stdin, os.Stderr = slave, stderr
	t.Cleanup(func() { os.Stdin, os.Stderr = oldIn, oldErr })

	typed(t, master, "n\n")
	if _, err := NewTerminal().PromptCredential(context.Background(), xrootd.CredentialRequest{
		Provider: "sss",
		Server:   "eos.example.org:1094",
	}); err != ErrDeclined {
		t.Fatalf("the answer gave %v, want %v", err, ErrDeclined)
	}

	got, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatalf("could not read what was printed: %v", err)
	}
	if !strings.Contains(string(got), "sss") {
		// stderr, not stdout: a prompt on stdout corrupts the output of the
		// command whose result is being piped somewhere.
		t.Fatalf("the question did not go to stderr:\n%s", got)
	}
}
