// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the security negotiation a login reply starts.
//
// A server answers kXR_login with a security-information string listing the
// protocols it will accept, in its own order of preference, each with its own
// parameters: "&P=krb5,host.example.org&P=unix". The client walks that list and
// stops at the first protocol it can complete. Everything about that walk is
// observable to a server operator and to nobody else, which is why it is worth
// pinning: a client that gives up on the first unknown protocol, or that hands
// a provider the wrong parameters, still authenticates fine against a server
// that happens to offer only one.

package xrootd

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/auth"
)

// confAuther is a provider whose whole behaviour is stated up front: what it is
// called, what it does when asked for a request, and what it sends. The
// credentials double as the instruction to the mock server below.
type confAuther struct {
	name  string
	creds string // credentials to send; also what the mock server keys on
	fail  error  // if non-nil, Request fails instead of producing anything

	// params records what the negotiation handed over, so that a provider
	// which is given the protocol name back, or is given nothing, is caught.
	params []string
	calls  int
}

func (a *confAuther) Provider() string { return a.name }

func (a *confAuther) Request(params []string) (*auth.Request, error) {
	a.calls++
	a.params = params
	if a.fail != nil {
		return nil, a.fail
	}
	var typ [4]byte
	copy(typ[:], a.name)
	return &auth.Request{Type: typ, Credentials: a.creds}, nil
}

// confAuthServer answers auth requests until the connection goes away. The
// credentials say what to answer: "ok" is accepted, anything else refused. That
// keeps the server ignorant of how many protocols the client will try, which is
// the point — the order and the number of attempts are the client's decision.
func confAuthServer(t *testing.T) func(cancel func(), conn net.Conn) {
	t.Helper()
	return func(cancel func(), conn net.Conn) {
		for {
			data, err := xrdproto.ReadRequest(conn)
			if err != nil {
				return
			}
			var (
				hdr xrdproto.RequestHeader
				req auth.Request
			)
			rbuf := xrdenc.NewRBuffer(data)
			if err := hdr.UnmarshalXrd(rbuf); err != nil {
				cancel()
				return
			}
			if err := req.UnmarshalXrd(rbuf); err != nil {
				cancel()
				return
			}
			var werr error
			switch req.Credentials {
			case "ok":
				werr = xrdproto.WriteResponse(conn, hdr.StreamID, xrdproto.Ok, nil)
			default:
				werr = xrdproto.WriteResponse(conn, hdr.StreamID, xrdproto.Error, xrdproto.ServerError{
					Code:    xrdproto.NotAuthorized,
					Message: "credentials refused",
				})
			}
			if werr != nil {
				return
			}
		}
	}
}

// confAuthRun negotiates secinfo against a server that accepts the credentials
// "ok", with the given providers registered, and reports what happened.
func confAuthRun(t *testing.T, secinfo string, provs ...*confAuther) error {
	t.Helper()
	var got error
	testClientWithMockServer(confAuthServer(t), func(cancel func(), client *Client) {
		client.auths = make(map[string]auth.Auther, len(provs))
		for _, p := range provs {
			client.auths[p.name] = p
		}
		sess := client.sessions[client.initialSessionID]
		got = sess.auth(context.Background(), []byte(secinfo))
	})
	return got
}

func TestConformance_TheClientTakesTheFirstProtocolItCanComplete(t *testing.T) {
	krb5 := &confAuther{name: "krb5", creds: "no"}
	unix := &confAuther{name: "unix", creds: "ok"}
	host := &confAuther{name: "host", creds: "ok"}

	if err := confAuthRun(t, "&P=krb5&P=unix&P=host", krb5, unix, host); err != nil {
		t.Fatalf("could not authenticate: %v", err)
	}
	if krb5.calls != 1 {
		t.Fatalf("the first protocol offered was tried %d times, want 1", krb5.calls)
	}
	if unix.calls != 1 {
		t.Fatalf("the protocol that works was tried %d times, want 1", unix.calls)
	}
	// The walk stops at the first success: a server that lists host last is
	// telling the client it is the least preferred identity, and a client
	// that kept going would end up logged in as the weaker one.
	if host.calls != 0 {
		t.Fatalf("negotiation continued past the protocol that worked (%d calls)", host.calls)
	}
}

func TestConformance_AnUnknownProtocolIsSkippedRatherThanFatal(t *testing.T) {
	// Servers routinely offer protocols this client has no implementation
	// for. That is not an error until nothing is left to try.
	unix := &confAuther{name: "unix", creds: "ok"}
	if err := confAuthRun(t, "&P=gsi,x&P=sss&P=unix", unix); err != nil {
		t.Fatalf("could not authenticate past unknown protocols: %v", err)
	}
	if unix.calls != 1 {
		t.Fatalf("the implemented protocol was tried %d times, want 1", unix.calls)
	}
}

func TestConformance_AProviderThatCannotProduceCredentialsIsSkipped(t *testing.T) {
	// A krb5 provider with no ticket, or a token provider with no token,
	// fails before anything reaches the wire. The next protocol still gets
	// its turn.
	krb5 := &confAuther{name: "krb5", fail: fmt.Errorf("no credentials cache")}
	unix := &confAuther{name: "unix", creds: "ok"}

	if err := confAuthRun(t, "&P=krb5&P=unix", krb5, unix); err != nil {
		t.Fatalf("could not authenticate past a provider with no credentials: %v", err)
	}
	if krb5.calls != 1 || unix.calls != 1 {
		t.Fatalf("attempts: krb5=%d unix=%d, want 1 and 1", krb5.calls, unix.calls)
	}
}

func TestConformance_AFailedNegotiationNamesEveryProtocolItTried(t *testing.T) {
	// When nothing works the client has to say why, per protocol: "login
	// failed" is not actionable, whereas "krb5: no credentials cache, unix:
	// refused" tells the operator which half to fix.
	krb5 := &confAuther{name: "krb5", fail: fmt.Errorf("no credentials cache")}
	unix := &confAuther{name: "unix", creds: "no"}

	err := confAuthRun(t, "&P=krb5&P=gsi&P=unix", krb5, unix)
	if err == nil {
		t.Fatal("negotiation succeeded with no usable protocol")
	}
	for _, want := range []string{"krb5", "gsi", "unix", "no credentials cache", "no gsi credential was found"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the failure does not mention %q: %v", want, err)
		}
	}
}

func TestConformance_AProviderIsHandedTheParametersTheServerSent(t *testing.T) {
	// The parameters after the protocol name are the server's, and each
	// provider parses its own: krb5 gets the service principal, sss gets the
	// key id. The protocol name itself is not one of them.
	unix := &confAuther{name: "unix", creds: "ok"}
	if err := confAuthRun(t, "&P=unix,host.example.org,v:1", unix); err != nil {
		t.Fatalf("could not authenticate: %v", err)
	}
	if got, want := unix.params, []string{"host.example.org", "v:1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("provider was handed %q, want %q", got, want)
	}
}

func TestConformance_AProtocolWithNoParametersIsHandedNone(t *testing.T) {
	unix := &confAuther{name: "unix", creds: "ok"}
	if err := confAuthRun(t, "&P=unix", unix); err != nil {
		t.Fatalf("could not authenticate: %v", err)
	}
	if got := unix.params; len(got) != 0 {
		t.Fatalf("provider was handed %q, want nothing", got)
	}
}

func TestConformance_TheSecurityInformationIsReadTheWayServersWriteIt(t *testing.T) {
	// Stock servers lead with "&" and prefix every entry with "P=". Both are
	// framing rather than content, and a client that treats either as part of
	// the protocol name finds no provider at all.
	for _, secinfo := range []string{
		"&P=unix",
		"P=unix",
		"&P=unix&P=host",
	} {
		t.Run(secinfo, func(t *testing.T) {
			unix := &confAuther{name: "unix", creds: "ok"}
			if err := confAuthRun(t, secinfo, unix); err != nil {
				t.Fatalf("could not authenticate against %q: %v", secinfo, err)
			}
		})
	}
}

func TestConformance_AnEmptySecurityInformationAuthenticatesNobody(t *testing.T) {
	// A login reply with no security information is a server that named no
	// protocol. There is nothing to try, and the client must say so rather
	// than proceed unauthenticated.
	unix := &confAuther{name: "unix", creds: "ok"}
	if err := confAuthRun(t, "", unix); err == nil {
		t.Fatal("negotiation succeeded against a server that offered no protocol")
	}
	if unix.calls != 0 {
		t.Fatalf("a protocol the server never offered was tried %d times", unix.calls)
	}
}

// confRoundAuther is a provider that keeps asking for another round. rounds
// counts how many challenges it answered; next is what More returns.
type confRoundAuther struct {
	name   string
	rounds int
	stop   int // return nil from More on this round; 0 means never
	failAt int // return an error from More on this round; 0 means never
	last   []byte
}

func (a *confRoundAuther) Provider() string { return a.name }

func (a *confRoundAuther) Request(params []string) (*auth.Request, error) {
	return &auth.Request{Type: [4]byte{'m', 'o', 'r', 'e'}, Credentials: "more"}, nil
}

func (a *confRoundAuther) More(challenge []byte) (*auth.Request, error) {
	a.rounds++
	a.last = append([]byte(nil), challenge...)
	switch {
	case a.failAt != 0 && a.rounds == a.failAt:
		return nil, fmt.Errorf("challenge %d rejected", a.rounds)
	case a.stop != 0 && a.rounds == a.stop:
		return nil, nil
	}
	return &auth.Request{Type: [4]byte{'m', 'o', 'r', 'e'}, Credentials: "more"}, nil
}

// confMoreServer answers every auth request with kXR_authmore, forever.
func confMoreServer(conn net.Conn) {
	for {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			return
		}
		var hdr xrdproto.RequestHeader
		if err := hdr.UnmarshalXrd(xrdenc.NewRBuffer(data[:xrdproto.RequestHeaderLength])); err != nil {
			return
		}
		if err := xrdproto.WriteResponse(conn, hdr.StreamID, xrdproto.AuthMore, rawBytes([]byte("challenge"))); err != nil {
			return
		}
	}
}

func TestConformance_ASingleRoundProviderCannotAnswerAChallenge(t *testing.T) {
	// kXR_authmore asks the provider to continue. One that has no way to
	// continue must say so, rather than resend the credentials the server
	// has already rejected and loop.
	prov := &confAuther{name: "unix", creds: "unix"}
	var got error
	testClientWithMockServer(
		func(cancel func(), conn net.Conn) { confMoreServer(conn) },
		func(cancel func(), client *Client) {
			sess := client.sessions[client.initialSessionID]
			req, _ := prov.Request(nil)
			got = sess.runAuth(context.Background(), prov, req)
		},
	)
	if got == nil {
		t.Fatal("a single-round provider claimed to have answered a challenge")
	}
	if !strings.Contains(got.Error(), "single-round") || !strings.Contains(got.Error(), "unix") {
		t.Fatalf("the failure does not name the provider that cannot continue: %v", got)
	}
}

func TestConformance_AnEndlessChallengeExchangeIsBounded(t *testing.T) {
	// A server that answers every round with kXR_authmore would keep a
	// willing provider going forever. The exchange is bounded so that a
	// misconfigured or hostile server costs a bounded amount of work.
	prov := &confRoundAuther{name: "more"}
	var got error
	testClientWithMockServer(
		func(cancel func(), conn net.Conn) { confMoreServer(conn) },
		func(cancel func(), client *Client) {
			sess := client.sessions[client.initialSessionID]
			req, _ := prov.Request(nil)
			got = sess.runAuth(context.Background(), prov, req)
		},
	)
	if got == nil {
		t.Fatal("an endless challenge exchange completed")
	}
	if !strings.Contains(got.Error(), "did not complete") {
		t.Fatalf("the failure does not say the exchange ran out of rounds: %v", got)
	}
	if prov.rounds != maxAuthRounds {
		// The bound counts exchanges, so the provider answers exactly that
		// many challenges before the client gives up.
		t.Fatalf("the provider was consulted %d times, want %d", prov.rounds, maxAuthRounds)
	}
	if string(prov.last) != "challenge" {
		t.Fatalf("the provider was handed %q, want the server's challenge", prov.last)
	}
}

func TestConformance_AProviderMayEndTheExchangeItself(t *testing.T) {
	// A provider that has nothing further to send ends the exchange
	// successfully: some protocols finish on the client side, with the
	// server's last kXR_authmore carrying the data the client needed.
	prov := &confRoundAuther{name: "more", stop: 2}
	var got error
	testClientWithMockServer(
		func(cancel func(), conn net.Conn) { confMoreServer(conn) },
		func(cancel func(), client *Client) {
			sess := client.sessions[client.initialSessionID]
			req, _ := prov.Request(nil)
			got = sess.runAuth(context.Background(), prov, req)
		},
	)
	if got != nil {
		t.Fatalf("a provider that ended the exchange was reported as a failure: %v", got)
	}
	if prov.rounds != 2 {
		t.Fatalf("the provider was consulted %d times, want 2", prov.rounds)
	}
}

func TestConformance_AProviderThatRejectsAChallengeStopsTheExchange(t *testing.T) {
	prov := &confRoundAuther{name: "more", failAt: 1}
	var got error
	testClientWithMockServer(
		func(cancel func(), conn net.Conn) { confMoreServer(conn) },
		func(cancel func(), client *Client) {
			sess := client.sessions[client.initialSessionID]
			req, _ := prov.Request(nil)
			got = sess.runAuth(context.Background(), prov, req)
		},
	)
	if got == nil {
		t.Fatal("a rejected challenge was reported as success")
	}
	if !strings.Contains(got.Error(), "challenge 1 rejected") {
		t.Fatalf("the provider's reason was lost: %v", got)
	}
}
