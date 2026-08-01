// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/auth"
)

// CredentialRequest describes a credential the server asked for and the client
// does not have.
type CredentialRequest struct {
	// Provider is the XRootD security protocol the server offered ("gsi",
	// "ztn", "krb5", "sss").
	Provider string
	// Params are the protocol parameters the server sent with it, such as the
	// GSI "c:" cryptomodule and "ca:" issuer hash.
	Params []string
	// Offered names every security protocol the server listed, in its own order
	// of preference and including Provider. Any one of them is enough, which is
	// what makes a question about the first one answerable with "no".
	Offered []string
	// Server is the address the client is authenticating to, which is what
	// makes a prompt answerable: the credential a user needs depends on which
	// storage element is asking.
	Server string
	// Missing describes what was looked for and where, when the provider could
	// say. It is nil for a provider this client does not implement at all.
	Missing *auth.Missing
}

// CredentialPrompter supplies a credential the client is missing.
//
// It is consulted at exactly one moment: the server has offered a security
// protocol, and the client has no credential for it. Returning an error — which
// includes "the user declined" — leaves the client to carry on down the
// server's list exactly as it would have without a prompter, so a prompter can
// never turn a session that would have worked into one that does not.
//
// Implementations must not block indefinitely: the context carries the caller's
// own deadline, and a prompt that outlives it strands whatever asked for the
// connection.
type CredentialPrompter interface {
	PromptCredential(ctx context.Context, req CredentialRequest) (auth.Auther, error)
}

// CredentialPrompterFunc adapts a function to CredentialPrompter.
type CredentialPrompterFunc func(ctx context.Context, req CredentialRequest) (auth.Auther, error)

// PromptCredential implements CredentialPrompter.
func (f CredentialPrompterFunc) PromptCredential(ctx context.Context, req CredentialRequest) (auth.Auther, error) {
	return f(ctx, req)
}

// WithCredentialPrompt makes the client ask p for any credential it is missing.
//
// A client with no prompter — the default, and the only sensible default for a
// library — never prompts: a batch job or a server that blocked on a terminal
// read nobody is watching would hang rather than fail. Interactive programs opt
// in, either here or process-wide with SetDefaultCredentialPrompt.
func WithCredentialPrompt(p CredentialPrompter) Option {
	return func(client *Client) error {
		client.prompter = p
		return nil
	}
}

var defaultPrompt struct {
	mu sync.RWMutex
	p  CredentialPrompter
}

// SetDefaultCredentialPrompt installs a prompter for every client that does not
// carry its own.
//
// This is process-wide state, deliberately: whether there is a user to ask is a
// property of the process rather than of any one client, and the alternative is
// threading an option through every layer that happens to open a connection —
// xrdio, xrdcopy, the commands — so that a program can say the one thing it
// knows. Commands call this in main; libraries should not call it at all.
//
// Passing nil restores the non-interactive default.
func SetDefaultCredentialPrompt(p CredentialPrompter) {
	defaultPrompt.mu.Lock()
	defer defaultPrompt.mu.Unlock()
	defaultPrompt.p = p
}

// DefaultCredentialPrompt returns the process-wide prompter, or nil.
func DefaultCredentialPrompt() CredentialPrompter {
	defaultPrompt.mu.RLock()
	defer defaultPrompt.mu.RUnlock()
	return defaultPrompt.p
}

// credentialPrompter returns the prompter this client should use, if any.
func (client *Client) credentialPrompter() CredentialPrompter {
	if client.prompter != nil {
		return client.prompter
	}
	return DefaultCredentialPrompt()
}

// promptFor asks the user for the credential provider needs, and remembers the
// answer.
//
// The answer is cached on the client because a prompt is a question to a human:
// a redirector sends the client to another data server, that session
// authenticates too, and a client that asked again for each one would ask a
// dozen times for the same proxy. So the cache is what makes a credential
// obtained once serve every later session. It also remembers a refusal, so a
// user who declined is not asked again on the next redirect.
//
// The cache lives under its own mutex rather than in client.auths because a
// session authenticates while getSession holds client.mu, and the answer has to
// be recorded from inside that.
func (client *Client) promptFor(ctx context.Context, req CredentialRequest) (auth.Auther, error) {
	client.promptMu.Lock()
	defer client.promptMu.Unlock()

	if client.prompted == nil {
		client.prompted = make(map[string]promptResult)
	}
	if got, ok := client.prompted[req.Provider]; ok {
		if got.auther == nil {
			return nil, got.err
		}
		return got.auther, nil
	}

	p := client.credentialPrompter()
	if p == nil {
		if req.Missing != nil {
			return nil, req.Missing
		}
		return nil, fmt.Errorf("no %s credential was found and there is nobody to ask", req.Provider)
	}

	auther, err := p.PromptCredential(ctx, req)
	switch {
	case err != nil:
		client.prompted[req.Provider] = promptResult{err: err}
		return nil, err
	case auther == nil:
		err = fmt.Errorf("no %s credential was supplied", req.Provider)
		client.prompted[req.Provider] = promptResult{err: err}
		return nil, err
	}

	client.prompted[req.Provider] = promptResult{auther: auther}
	return auther, nil
}

// skipReason is what the user is told about a protocol that was not used.
//
// It prefers the credential over the prompt. "no terminal to prompt on" is a
// true statement about why nobody was asked, and a useless one to read at the
// end of a failed transfer: what the user has to act on is that the proxy in
// /tmp expired, which is equally true whether or not there was anyone to ask.
// The prompter's own error is still the one the negotiation reports per
// protocol, so nothing is lost — only reordered by usefulness.
func skipReason(miss *auth.Missing, err error) error {
	if miss != nil {
		return miss
	}
	return err
}

// promptResult is a prompter's answer, remembered so the user is asked once.
type promptResult struct {
	auther auth.Auther
	err    error
}

// noteAuth records that the session authenticated as provider.
func (client *Client) noteAuth(provider string) {
	client.promptMu.Lock()
	defer client.promptMu.Unlock()
	client.usedAuth = provider
}

// noteUnavailable records a security protocol the server offered that the
// client could not use, and why.
func (client *Client) noteUnavailable(provider string, why error) {
	client.promptMu.Lock()
	defer client.promptMu.Unlock()
	if client.unusedAuth == nil {
		client.unusedAuth = make(map[string]error)
	}
	if _, dup := client.unusedAuth[provider]; !dup {
		client.unusedAuth[provider] = why
	}
}

// explainAuth adds the credential note to an authorization failure, leaving
// every other error — and every error at all when the client had a credential
// for everything the server offered — exactly as it was.
//
// The wrapper keeps the server error reachable through errors.As and
// errors.Is, so a caller that tests for a permission failure is unaffected by
// the extra sentence.
func (client *Client) explainAuth(err error) error {
	var srv xrdproto.ServerError
	if !errors.As(err, &srv) {
		return err
	}
	switch srv.Code {
	case xrdproto.NotAuthorized, xrdproto.AuthFailed:
	default:
		return err
	}
	note := client.credentialNote()
	if note == "" {
		return err
	}
	return &credentialError{err: err, note: note}
}

// credentialError is a server authorization failure with the client-side
// explanation appended.
type credentialError struct {
	err  error
	note string
}

func (e *credentialError) Error() string { return e.err.Error() + "\n" + e.note }
func (e *credentialError) Unwrap() error { return e.err }

// credentialNote explains an authorization failure in terms of the credentials
// this client had.
//
// It exists because of how the failure actually arrives. When the strong
// protocol a site expects has no credential, the client does not fail: it walks
// on down the server's list and logs in as "unix", which succeeds. The refusal
// comes later, on the first request, as kXR_NotAuthorized — a message about a
// path, arriving long after the decision that caused it, and naming an identity
// the user never chose. This is the missing sentence.
func (client *Client) credentialNote() string {
	client.promptMu.Lock()
	defer client.promptMu.Unlock()

	if len(client.unusedAuth) == 0 {
		return ""
	}

	providers := make([]string, 0, len(client.unusedAuth))
	for p := range client.unusedAuth {
		providers = append(providers, p)
	}
	sort.Strings(providers)

	var b strings.Builder
	b.WriteString("xrootd: the request was refused as unauthorized")
	if client.usedAuth != "" {
		fmt.Fprintf(&b, " for the identity %q", client.usedAuth)
	}
	b.WriteString(".\nThe server also offered:")
	for _, p := range providers {
		why := client.unusedAuth[p]
		fmt.Fprintf(&b, "\n  %s: %v", p, why)
		if m := auth.AsMissing(why); m != nil && m.Hint != "" {
			fmt.Fprintf(&b, "\n    to get one: %s", m.Hint)
		}
	}
	return b.String()
}
