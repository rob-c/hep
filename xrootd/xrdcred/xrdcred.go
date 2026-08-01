// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package xrdcred asks a user, at a terminal, for a credential the XRootD
// client is missing.
//
// An XRootD server announces the security protocols it accepts, and a client
// with no credential for any of them does not fail: it logs in with the weakest
// identity the server offered — usually "unix" — and the refusal arrives much
// later, on the first real request, as kXR_NotAuthorized. That error names a
// path and an identity the user never chose, and says nothing about the proxy
// that expired an hour ago. This package closes that gap at the only moment
// when it can still be fixed: while the client is logging in.
//
// It is opt-in. A library that prompted by default would hang a batch job on a
// terminal read nobody is watching, so a program says so explicitly:
//
//	xrootd.SetDefaultCredentialPrompt(xrdcred.NewTerminal())
//
// and the prompter still declines — leaving the client to behave exactly as it
// does today — whenever there is no terminal on the other end.
package xrdcred // import "go-hep.org/x/hep/xrootd/xrdcred"

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdproto/auth"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/gsi"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/sss"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/token"
)

// ErrNoTerminal is returned when there is nobody to ask: the input is not a
// terminal, so a prompt would block forever on a pipe or a log file.
var ErrNoTerminal = fmt.Errorf("xrdcred: no terminal to prompt on")

// ErrDeclined is returned when the user chose to carry on without supplying the
// credential.
var ErrDeclined = fmt.Errorf("xrdcred: the credential was declined")

// Terminal prompts for missing credentials on a terminal.
//
// The zero value is usable and reads from os.Stdin, writing to os.Stderr —
// stderr rather than stdout so that prompting never corrupts the output of a
// command whose result is being piped somewhere.
type Terminal struct {
	// In is the terminal to read from. Nil means os.Stdin.
	In *os.File
	// Out is where prompts are written. Nil means os.Stderr.
	Out io.Writer
	// NoExec forbids running credential tools (voms-proxy-init, kinit), leaving
	// only the answers the user can type. Set it where spawning a process is
	// not acceptable.
	NoExec bool

	// mu serializes prompts: a client that opens several sessions at once —
	// which happens the moment a redirector answers — would otherwise interleave
	// two questions on one terminal, and the user's answer would go to whichever
	// read won.
	mu sync.Mutex
}

// NewTerminal returns a Terminal reading from os.Stdin.
func NewTerminal() *Terminal { return &Terminal{} }

var _ xrootd.CredentialPrompter = (*Terminal)(nil)

func (t *Terminal) in() *os.File {
	if t.In != nil {
		return t.In
	}
	return os.Stdin
}

func (t *Terminal) out() io.Writer {
	if t.Out != nil {
		return t.Out
	}
	return os.Stderr
}

func (t *Terminal) printf(format string, args ...any) {
	fmt.Fprintf(t.out(), format, args...)
}

// PromptCredential implements xrootd.CredentialPrompter.
func (t *Terminal) PromptCredential(ctx context.Context, req xrootd.CredentialRequest) (auth.Auther, error) {
	if !isTerminal(t.in()) {
		return nil, ErrNoTerminal
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	opts := t.options(req.Provider)
	if len(opts) == 0 {
		return nil, fmt.Errorf("xrdcred: %s is not a security protocol this client can supply", req.Provider)
	}

	t.describe(req)
	opt, err := t.choose(ctx, opts)
	if err != nil {
		return nil, err
	}
	a, err := opt.run(ctx, t, req)
	if err != nil {
		return nil, err
	}
	t.printf("\n")
	return a, nil
}

// describe states what is missing, where it was looked for, and who is asking,
// before any question is put. A user who is about to be asked for an X.509
// proxy generally knows they have one; what they do not know is that the client
// looked somewhere else for it.
func (t *Terminal) describe(req xrootd.CredentialRequest) {
	what := req.Provider
	if req.Missing != nil && req.Missing.What != "" {
		what = req.Missing.What
	}
	t.printf("\n%s requires a %s (%s), which this client does not have.\n", req.Server, what, req.Provider)
	if req.Missing != nil {
		if len(req.Missing.Searched) != 0 {
			t.printf("  looked in: %s\n", strings.Join(req.Missing.Searched, ", "))
		}
		if req.Missing.Err != nil {
			// Something was there and could not be used — an expired proxy, an
			// unreadable keytab. That is a different problem to having none, and
			// the user has to be told which one they have.
			t.printf("  problem:   %s\n", req.Missing.Err)
		}
	}
	// A server usually names several protocols, and the client asks about each
	// one it cannot supply, in the server's order. Without this line a user
	// holding a perfectly good X.509 proxy, asked first for a Kerberos ticket,
	// has no way to know that saying no leads to the question they can answer.
	if others := othersThan(req.Provider, req.Offered); len(others) != 0 {
		t.printf("  this server also accepts: %s — any one of them is enough.\n", strings.Join(others, ", "))
	}
}

// othersThan returns the offered protocols apart from provider.
func othersThan(provider string, offered []string) []string {
	var others []string
	for _, p := range offered {
		if p != provider && p != "" {
			others = append(others, p)
		}
	}
	return others
}

// option is one answer the user may give.
type option struct {
	key  string // what they type
	text string // what it does
	run  func(ctx context.Context, t *Terminal, req xrootd.CredentialRequest) (auth.Auther, error)
}

// options lists the ways this provider's credential can be supplied. The order
// is the order they are offered, and the first is the default.
func (t *Terminal) options(provider string) []option {
	decline := option{key: "n", text: "carry on without it", run: declined}

	switch provider {
	case "gsi":
		var opts []option
		if !t.NoExec && haveTool("voms-proxy-init") {
			opts = append(opts, option{key: "1", text: "create a proxy now (voms-proxy-init)", run: runVOMSProxyInit})
		}
		opts = append(opts,
			option{key: "2", text: "use a proxy file I name", run: loadProxyFile},
			decline,
		)
		return opts

	case "ztn":
		return []option{
			{key: "1", text: "paste the token (it will not be shown)", run: pasteToken},
			{key: "2", text: "read the token from a file I name", run: loadTokenFile},
			decline,
		}

	case "krb5":
		var opts []option
		if !t.NoExec && haveTool("kinit") {
			opts = append(opts, option{key: "1", text: "get a ticket now (kinit)", run: runKinit})
		}
		opts = append(opts, decline)
		if len(opts) == 1 {
			// Nothing but "no" is not a question worth asking.
			return nil
		}
		return opts

	case "sss":
		return []option{
			{key: "1", text: "use a keytab file I name", run: loadKeytabFile},
			decline,
		}
	}
	return nil
}

// choose puts the menu and reads one answer, repeating until it gets one it
// understands. An empty line takes the first option, which is the one a user
// almost always wants.
func (t *Terminal) choose(ctx context.Context, opts []option) (option, error) {
	for {
		t.printf("\n")
		for _, o := range opts {
			t.printf("  [%s] %s\n", o.key, o.text)
		}
		t.printf("choice [%s]: ", opts[0].key)

		line, err := readLine(ctx, t.in())
		if err != nil {
			return option{}, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return opts[0], nil
		}
		for _, o := range opts {
			if strings.EqualFold(line, o.key) {
				return o, nil
			}
		}
		t.printf("  %q is not one of the choices.\n", line)
	}
}

// askPath asks for a file name.
//
// There is no default, deliberately. The conventional location is exactly what
// was searched and did not work, so offering it back is offering the user the
// answer that is already known to be wrong. A blank answer is a decline, since
// pressing return at a question you cannot answer means "get on with it".
func (t *Terminal) askPath(ctx context.Context, prompt string) (string, error) {
	path, err := t.ask(ctx, prompt+" (blank to skip)", "")
	switch {
	case err != nil:
		return "", err
	case path == "":
		return "", ErrDeclined
	}
	return expand(path), nil
}

// ask puts a single question and returns the answer, trimmed.
func (t *Terminal) ask(ctx context.Context, prompt, def string) (string, error) {
	switch {
	case def != "":
		t.printf("%s [%s]: ", prompt, def)
	default:
		t.printf("%s: ", prompt)
	}
	line, err := readLine(ctx, t.in())
	if err != nil {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def, nil
	}
	return line, nil
}

func declined(ctx context.Context, t *Terminal, req xrootd.CredentialRequest) (auth.Auther, error) {
	return nil, ErrDeclined
}

// pasteToken reads a bearer token without echoing it.
//
// The token is never printed back, not in a confirmation and not in an error:
// it is a bearer credential, and a terminal keeps its scrollback.
func pasteToken(ctx context.Context, t *Terminal, req xrootd.CredentialRequest) (auth.Auther, error) {
	t.printf("token: ")
	raw, err := readSecret(ctx, t.in())
	t.printf("\n")
	if err != nil {
		return nil, err
	}
	tok := strings.TrimRight(raw, " \t\r\n\x00")
	if tok == "" {
		return nil, fmt.Errorf("xrdcred: no token was entered")
	}
	t.printf("  got a token of %d characters.\n", len(tok))
	return &token.Auth{Token: tok}, nil
}

func loadTokenFile(ctx context.Context, t *Terminal, req xrootd.CredentialRequest) (auth.Auther, error) {
	path, err := t.askPath(ctx, "path to the token file")
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("xrdcred: could not read the token file: %w", err)
	}
	tok := strings.TrimRight(string(raw), " \t\r\n\x00")
	if tok == "" {
		return nil, fmt.Errorf("xrdcred: %s holds no token", path)
	}
	return &token.Auth{Token: tok}, nil
}

func loadProxyFile(ctx context.Context, t *Terminal, req xrootd.CredentialRequest) (auth.Auther, error) {
	path, err := t.askPath(ctx, "path to the proxy")
	if err != nil {
		return nil, err
	}
	return openProxy(t, path)
}

// openProxy loads a proxy and reports what it is, because a proxy file is
// opaque and the mistake it usually hides — this is yesterday's proxy — is
// visible only in its expiry.
func openProxy(t *Terminal, path string) (auth.Auther, error) {
	a, err := gsi.LoadProxy(path)
	if err != nil {
		return nil, fmt.Errorf("xrdcred: could not use the proxy: %w", err)
	}
	switch expiry, err := a.NotAfter(); {
	case err != nil:
		return nil, fmt.Errorf("xrdcred: could not read the proxy expiry: %w", err)
	default:
		t.printf("  using %s, valid until %s.\n", path, expiry.Local().Format("2006-01-02 15:04:05"))
	}
	return a, nil
}

func loadKeytabFile(ctx context.Context, t *Terminal, req xrootd.CredentialRequest) (auth.Auther, error) {
	path, err := t.askPath(ctx, "path to the sss keytab")
	if err != nil {
		return nil, err
	}
	// The loader applies to a named keytab exactly the rules it applies to an
	// ambient one, rather than growing a second way to read a keytab.
	a, err := sss.NewFromKeytab(path)
	if err != nil {
		return nil, fmt.Errorf("xrdcred: could not use the keytab: %w", err)
	}
	t.printf("  using %s, key %d.\n", path, a.Key.ID)
	return a, nil
}
