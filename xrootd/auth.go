// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"bytes"
	"context"
	"fmt"

	"go-hep.org/x/hep/xrootd/xrdproto/auth"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/gsi"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/host"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/krb5"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/sss"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/token"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/unix"
)

// defaultProviders is the list of authentication providers a xrootd client will
// use by default. Credentialed providers (krb5, gsi, ztn, sss) precede the weaker
// host/unix identities; nil entries (a provider whose ambient discovery failed)
// are skipped by initSecurityProviders. The actual choice is still driven by
// the server's offered list in cliSession.auth.
var defaultProviders = []auth.Auther{
	krb5.Default,
	gsi.Default,
	token.Default,
	sss.Default,
	unix.Default,
	host.Default,
}

func (sess *cliSession) auth(ctx context.Context, securityInformation []byte) error {
	securityInformation = bytes.TrimLeft(securityInformation, "&")
	providerInfos := bytes.Split(securityInformation, []byte{'&'})

	// The whole list is parsed before any of it is tried, because a user who is
	// asked for one credential needs to know what the alternatives are: "this
	// server also accepts gsi" turns "I have no Kerberos ticket" from a dead end
	// into an answer.
	protos := make([][]string, 0, len(providerInfos))
	offered := make([]string, 0, len(providerInfos))
	for _, providerInfo := range providerInfos {
		providerInfo = bytes.TrimLeft(providerInfo, "P=")[:]
		paramsData := bytes.Split(providerInfo, []byte{','})
		params := make([]string, len(paramsData))
		for i := range paramsData {
			params[i] = string(paramsData[i])
		}
		protos = append(protos, params)
		offered = append(offered, params[0])
	}

	var errs []error
	for _, params := range protos {
		provider := params[0]
		params = params[1:]

		auther, ok := sess.client.auths[provider]
		if !ok {
			// The server offers a protocol this client has no credential for.
			// Ask, if anyone is there to be asked; a refusal — or no prompter at
			// all — leaves the loop to try the next protocol exactly as before,
			// so the outcome is never worse than not having asked.
			miss := missingCredential(provider)
			var err error
			auther, err = sess.client.promptFor(ctx, CredentialRequest{
				Provider: provider,
				Params:   params,
				Offered:  offered,
				Server:   sess.sessionID,
				Missing:  miss,
			})
			if err != nil {
				sess.client.noteUnavailable(provider, skipReason(miss, err))
				errs = append(errs, fmt.Errorf("xrootd: could not authorize using %s: %w", provider, err))
				continue
			}
		}
		r, err := auther.Request(params)
		if err != nil {
			sess.client.noteUnavailable(provider, err)
			errs = append(errs, fmt.Errorf("xrootd: could not authorize using %s: %w", provider, err))
			continue
		}
		if err := sess.runAuth(ctx, auther, r); err != nil {
			sess.client.noteUnavailable(provider, err)
			errs = append(errs, fmt.Errorf("xrootd: could not authorize using %s: %w", provider, err))
			continue
		}
		sess.client.noteAuth(provider)
		return nil
	}

	return fmt.Errorf("xrootd: could not authorize:\n%v", errs)
}

// missingCredential reports what a security provider looked for and did not
// find, when the provider is one this client implements. It is nil for a
// protocol the client does not know at all, which is a different situation and
// reads differently to a user: nothing they can supply would help.
func missingCredential(provider string) *auth.Missing {
	var err error
	switch provider {
	case "gsi":
		err = gsi.DefaultErr
	case "ztn":
		err = token.DefaultErr
	case "krb5":
		err = krb5.DefaultErr
	case "sss":
		err = sss.DefaultErr
	}
	return auth.AsMissing(err)
}

// maxAuthRounds bounds a multi-round authentication exchange to guard against a
// server that never completes it.
const maxAuthRounds = 16

// runAuth drives a (possibly multi-round) authentication exchange for one
// provider: it sends the initial request and, while the server responds with
// kXR_authmore and the provider is a Continuer, feeds each challenge back to
// obtain the next request.
func (sess *cliSession) runAuth(ctx context.Context, auther auth.Auther, req *auth.Request) error {
	cont, _ := auther.(auth.Continuer)
	for round := 0; round < maxAuthRounds; round++ {
		more, challenge, err := sess.authRound(ctx, req)
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
		if cont == nil {
			return fmt.Errorf("xrootd: server asked for more authentication but %q is single-round", auther.Provider())
		}
		next, err := cont.More(challenge)
		if err != nil {
			return err
		}
		if next == nil {
			return nil
		}
		req = next
	}
	return fmt.Errorf("xrootd: authentication did not complete within %d rounds", maxAuthRounds)
}
