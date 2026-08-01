// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for asking a user for a credential the client does not have.
//
// The behaviour being pinned is a chain of decisions, and each link is load
// bearing. A server names the protocols it accepts; the client has a credential
// for none of them; and rather than fail, it logs in as "unix" and is refused
// on the first request that touches a file. The prompt exists to interrupt that
// chain at the only point where it can still be fixed. What must never happen
// is the prompt making things worse: a declined question, a program with no
// terminal, or no prompter at all has to leave the negotiation walking the
// server's list exactly as it did before, and the explanation that gets attached
// to the eventual refusal has to leave the server's own error reachable.
//
// The prompter in these tests is a function, never a terminal: what is under
// test is when the client asks, how often, and what it does with the answer.

package xrootd

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/auth"
)

// confPrompter records every request it is handed and answers with a fixed
// outcome.
type confPrompter struct {
	auther auth.Auther
	err    error

	reqs []CredentialRequest
}

func (p *confPrompter) PromptCredential(ctx context.Context, req CredentialRequest) (auth.Auther, error) {
	p.reqs = append(p.reqs, req)
	return p.auther, p.err
}

// confPromptRun negotiates secinfo with p installed as the client's prompter
// and provs as its credentials, against the server that accepts "ok".
func confPromptRun(t *testing.T, secinfo string, p CredentialPrompter, provs ...*confAuther) (*Client, error) {
	t.Helper()
	var (
		got  error
		seen *Client
	)
	testClientWithMockServer(confAuthServer(t), func(cancel func(), client *Client) {
		client.auths = make(map[string]auth.Auther, len(provs))
		for _, prov := range provs {
			client.auths[prov.name] = prov
		}
		client.prompter = p
		sess := client.sessions[client.initialSessionID]
		sess.sessionID = client.initialSessionID
		got = sess.auth(context.Background(), []byte(secinfo))
		seen = client
	})
	return seen, got
}

func TestConformance_AProtocolWithNoCredentialIsAskedFor(t *testing.T) {
	// The server wants a bearer token and the client has none. Without a prompt
	// this is the end of ztn; with one it is a question, and the answer is used
	// for the rest of the negotiation.
	p := &confPrompter{auther: &confAuther{name: "ztn", creds: "ok"}}

	client, err := confPromptRun(t, "&P=ztn,v:1", p)
	if err != nil {
		t.Fatalf("could not authenticate with the credential that was supplied: %v", err)
	}
	if len(p.reqs) != 1 {
		t.Fatalf("the user was asked %d times, want once", len(p.reqs))
	}

	req := p.reqs[0]
	switch {
	case req.Provider != "ztn":
		t.Errorf("the prompt named provider %q, want %q", req.Provider, "ztn")
	case req.Server != client.initialSessionID:
		// Which server is asking is half the question: a user with several
		// credentials picks between them by who wants one.
		t.Errorf("the prompt named server %q, want %q", req.Server, client.initialSessionID)
	case len(req.Params) != 1 || req.Params[0] != "v:1":
		t.Errorf("the prompt carried parameters %q, want [v:1]", req.Params)
	}
}

func TestConformance_AClientWithNoPrompterAsksNobodyAndCarriesOn(t *testing.T) {
	// This is the default, and the whole reason the default is safe: a batch job
	// gets exactly the behaviour it had before the prompt existed, which is to
	// walk on to the next protocol.
	unix := &confAuther{name: "unix", creds: "ok"}

	client, err := confPromptRun(t, "&P=ztn&P=unix", nil, unix)
	if err != nil {
		t.Fatalf("could not authenticate: %v", err)
	}
	if unix.calls != 1 {
		t.Fatalf("the protocol the client did have credentials for was tried %d times, want once", unix.calls)
	}
	if _, ok := client.unusedAuth["ztn"]; !ok {
		t.Fatal("the protocol that was skipped was not recorded")
	}
	if got := client.usedAuth; got != "unix" {
		t.Fatalf("the client recorded logging in as %q, want %q", got, "unix")
	}
}

func TestConformance_ADeclinedCredentialLeavesTheNegotiationAsItWas(t *testing.T) {
	// A user who says no has not asked for the transfer to fail differently.
	// The refusal is recorded, and the client goes on to the next protocol.
	p := &confPrompter{err: errors.New("not now")}
	unix := &confAuther{name: "unix", creds: "ok"}

	client, err := confPromptRun(t, "&P=ztn&P=unix", p, unix)
	if err != nil {
		t.Fatalf("a declined prompt broke a negotiation that could still succeed: %v", err)
	}
	if unix.calls != 1 {
		t.Fatalf("the protocol that could still work was tried %d times, want once", unix.calls)
	}
	if got := client.unusedAuth["ztn"]; got == nil {
		t.Fatal("the protocol that was declined was not recorded")
	}
}

func TestConformance_TheReasonAProtocolWasSkippedIsTheCredentialNotThePrompt(t *testing.T) {
	// "no terminal to prompt on" is a true statement about why nobody was asked
	// and a useless one to read at the end of a failed transfer. What the user
	// has to act on is the credential, which is missing either way.
	miss := &auth.Missing{Provider: "gsi", What: "X.509 proxy", Searched: []string{"/tmp/x509up_u0"}}
	if got := skipReason(miss, errors.New("no terminal to prompt on")); got != error(miss) {
		t.Errorf("the reason is %v, want the missing credential", got)
	}

	// A protocol this client cannot supply has no credential to describe, so
	// whatever the prompter said is all there is.
	own := errors.New("the prompter said no")
	if got := skipReason(nil, own); got != own {
		t.Errorf("the reason is %v, want the prompter's own error", got)
	}
}

func TestConformance_APrompterThatSuppliesNothingIsNotACredential(t *testing.T) {
	// (nil, nil) is a prompter bug, not a credential. It must not reach
	// Request, where it would be a nil-pointer dereference inside the
	// negotiation.
	p := &confPrompter{}
	unix := &confAuther{name: "unix", creds: "ok"}

	client, err := confPromptRun(t, "&P=ztn&P=unix", p, unix)
	if err != nil {
		t.Fatalf("could not authenticate: %v", err)
	}
	if unix.calls != 1 {
		t.Fatalf("the protocol that could still work was tried %d times, want once", unix.calls)
	}
	if got := client.unusedAuth["ztn"]; got == nil {
		t.Fatal("the protocol the prompter did not supply was not recorded")
	}

	// The client asked, and must remember that it asked: a prompter bug is not
	// a reason to put the same question again on the next redirect.
	if _, err := client.promptFor(context.Background(), CredentialRequest{Provider: "ztn"}); err == nil {
		t.Fatal("nothing at all was accepted as a credential")
	}
	if len(p.reqs) != 1 {
		t.Fatalf("the user was asked %d times, want once", len(p.reqs))
	}
}

func TestConformance_AUserIsAskedOncePerClient(t *testing.T) {
	// A redirector answers, and the client authenticates all over again against
	// the data server it was sent to. Asking per session would ask a user for
	// the same proxy once per storage node.
	p := &confPrompter{auther: &confAuther{name: "ztn", creds: "ok"}}
	client := &Client{prompter: p, prompted: make(map[string]promptResult)}

	for range 3 {
		a, err := client.promptFor(context.Background(), CredentialRequest{Provider: "ztn"})
		if err != nil {
			t.Fatalf("could not obtain the credential: %v", err)
		}
		if a == nil {
			t.Fatal("the remembered answer was nil")
		}
	}
	if len(p.reqs) != 1 {
		t.Fatalf("the user was asked %d times, want once", len(p.reqs))
	}
}

func TestConformance_AUserWhoSaidNoIsNotAskedAgain(t *testing.T) {
	p := &confPrompter{err: errors.New("no")}
	client := &Client{prompter: p, prompted: make(map[string]promptResult)}

	for range 3 {
		if _, err := client.promptFor(context.Background(), CredentialRequest{Provider: "gsi"}); err == nil {
			t.Fatal("a prompter that refused supplied a credential")
		}
	}
	if len(p.reqs) != 1 {
		t.Fatalf("the user was asked %d times after saying no, want once", len(p.reqs))
	}
}

func TestConformance_WithNoPrompterTheMissingCredentialIsTheReason(t *testing.T) {
	// Nobody to ask is not a new kind of failure: the reason the protocol was
	// skipped is still what the provider looked for and did not find.
	miss := &auth.Missing{Provider: "ztn", What: "bearer token", Searched: []string{"/tmp/bt_u0"}}
	client := &Client{}

	_, err := client.promptFor(context.Background(), CredentialRequest{Provider: "ztn", Missing: miss})
	if got := auth.AsMissing(err); got != miss {
		t.Fatalf("the failure was %v, want the provider's own Missing", err)
	}
}

func TestConformance_AProcessWidePrompterAnswersForAClientWithNone(t *testing.T) {
	// xrdio and xrdcopy build their own clients, so a command cannot pass an
	// option to them. This is how it says once, for the whole process, that
	// there is a user to ask.
	t.Cleanup(func() { SetDefaultCredentialPrompt(nil) })

	p := &confPrompter{auther: &confAuther{name: "ztn", creds: "ok"}}
	SetDefaultCredentialPrompt(p)

	if got := DefaultCredentialPrompt(); got != CredentialPrompter(p) {
		t.Fatalf("the default prompter is %v, want the one that was installed", got)
	}
	client := &Client{}
	if _, err := client.promptFor(context.Background(), CredentialRequest{Provider: "ztn"}); err != nil {
		t.Fatalf("the process-wide prompter was not consulted: %v", err)
	}

	// A client with its own prompter is not overridden by it.
	own := &confPrompter{auther: &confAuther{name: "ztn", creds: "ok"}}
	client = &Client{prompter: own}
	if _, err := client.promptFor(context.Background(), CredentialRequest{Provider: "ztn"}); err != nil {
		t.Fatalf("the client's own prompter was not consulted: %v", err)
	}
	if len(own.reqs) != 1 {
		t.Fatalf("the client's own prompter was asked %d times, want once", len(own.reqs))
	}
}

func TestConformance_APrompterFuncIsAPrompter(t *testing.T) {
	want := &confAuther{name: "ztn", creds: "ok"}
	var p CredentialPrompter = CredentialPrompterFunc(func(ctx context.Context, req CredentialRequest) (auth.Auther, error) {
		return want, nil
	})
	got, err := p.PromptCredential(context.Background(), CredentialRequest{Provider: "ztn"})
	if err != nil {
		t.Fatalf("could not prompt: %v", err)
	}
	if got != auth.Auther(want) {
		t.Fatalf("the adapter returned %v, want the function's own answer", got)
	}
}

func TestConformance_TheOptionInstallsAPrompter(t *testing.T) {
	p := &confPrompter{}
	client := &Client{}
	if err := WithCredentialPrompt(p)(client); err != nil {
		t.Fatalf("could not apply the option: %v", err)
	}
	if client.credentialPrompter() != CredentialPrompter(p) {
		t.Fatal("the option did not install the prompter")
	}
}

func TestConformance_OnlyProvidersThisClientImplementsSayWhatIsMissing(t *testing.T) {
	// "gsi" can say it looked in /tmp/x509up_u1000 and found nothing. A protocol
	// this client has never heard of cannot say anything, and pretending
	// otherwise would tell a user to go and find a credential that would not
	// have been used anyway.
	if got := missingCredential("nosuchsec"); got != nil {
		t.Fatalf("an unimplemented protocol reported %v, want nothing", got)
	}
	for _, provider := range []string{"gsi", "ztn", "krb5", "sss"} {
		// Whether the credential is actually present depends on the machine the
		// test runs on; what must hold is that the answer describes the protocol
		// that was asked about.
		got := missingCredential(provider)
		if got == nil {
			continue
		}
		if got.Provider != provider {
			t.Errorf("the missing credential for %q describes %q", provider, got.Provider)
		}
		if got.What == "" {
			t.Errorf("the missing credential for %q names nothing a user could go and get", provider)
		}
	}
}

func TestConformance_AnUnauthorizedRequestNamesTheCredentialThatWasSkipped(t *testing.T) {
	// This is the sentence the whole feature exists to produce. The server says
	// "unauthorized identity used"; only the client knows that it offered "unix"
	// because the proxy in /tmp had expired.
	client := &Client{}
	client.noteAuth("unix")
	client.noteUnavailable("gsi", &auth.Missing{
		Provider: "gsi",
		What:     "X.509 proxy",
		Searched: []string{"/tmp/x509up_u1000"},
		Hint:     "voms-proxy-init -voms lhcb",
		Err:      errors.New("the proxy expired at 2026-07-30T09:00:00Z"),
	})

	srv := xrdproto.ServerError{Code: xrdproto.NotAuthorized, Message: "unauthorized identity used"}
	err := client.explainAuth(srv)

	for _, want := range []string{
		"unauthorized identity used", // the server's own words are kept
		`"unix"`,                     // what the client did log in as
		"gsi",                        // what it could not
		"/tmp/x509up_u1000",          // where it looked
		"expired",                    // why what it found was no good
		"voms-proxy-init -voms lhcb", // and what to do about it
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the explanation does not mention %q:\n%v", want, err)
		}
	}

	// A caller that tests for the server's error still finds it.
	var got xrdproto.ServerError
	if !errors.As(err, &got) {
		t.Fatalf("the server error is no longer reachable through the explanation: %v", err)
	}
	if got.Code != xrdproto.NotAuthorized {
		t.Fatalf("the server error became %v", got.Code)
	}
}

func TestConformance_ARequestThatFailedForOtherReasonsIsNotExplainedAway(t *testing.T) {
	// The note answers exactly one question: "why was I not authorized?". A file
	// that is not there is not that question, and an unauthorized request from a
	// client that had every credential the server offered is not either.
	client := &Client{}
	client.noteUnavailable("gsi", errors.New("no proxy"))

	for _, tc := range []struct {
		name string
		err  error
	}{
		{"a different server error", xrdproto.ServerError{Code: xrdproto.NotFound, Message: "no such file"}},
		{"a local error", errors.New("connection reset")},
		{"no error at all", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := client.explainAuth(tc.err); got != tc.err {
				t.Fatalf("the error was rewritten to %v, want %v", got, tc.err)
			}
		})
	}

	clean := &Client{}
	srv := xrdproto.ServerError{Code: xrdproto.NotAuthorized, Message: "unauthorized"}
	if got := clean.explainAuth(srv); got.Error() != srv.Error() {
		t.Fatalf("a client that skipped no protocol added %q", got)
	}
}

func TestConformance_EachSkippedProtocolIsReportedOnceInTheOrderAUserReadsThem(t *testing.T) {
	client := &Client{}
	client.noteUnavailable("ztn", errors.New("first reason"))
	client.noteUnavailable("ztn", errors.New("second reason"))
	client.noteUnavailable("krb5", errors.New("no ticket"))

	note := client.credentialNote()
	if strings.Contains(note, "second reason") {
		// The first failure is the one that happened; a later session repeating
		// it must not overwrite it, and must not add a second line.
		t.Errorf("a protocol was reported twice:\n%v", note)
	}
	if i, j := strings.Index(note, "krb5"), strings.Index(note, "ztn"); i > j {
		t.Errorf("the protocols are not in a stable order:\n%v", note)
	}
}

func TestConformance_APromptIsToldEveryProtocolTheServerNamed(t *testing.T) {
	// Which credential to supply is a choice between the protocols on offer, so
	// the prompt has to carry the whole list and not just the one it is asking
	// about.
	p := &confPrompter{err: errors.New("no")}
	unix := &confAuther{name: "unix", creds: "ok"}

	if _, err := confPromptRun(t, "&P=krb5&P=gsi&P=unix", p, unix); err != nil {
		t.Fatalf("could not authenticate: %v", err)
	}
	if len(p.reqs) != 2 {
		t.Fatalf("the user was asked about %d protocols, want krb5 and gsi", len(p.reqs))
	}
	want := []string{"krb5", "gsi", "unix"}
	for _, req := range p.reqs {
		if !slices.Equal(req.Offered, want) {
			t.Errorf("the prompt for %s carried %q, want %q", req.Provider, req.Offered, want)
		}
	}
}
