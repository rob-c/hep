// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-23-native-auth authenticates over the native protocol, with a token, with GSI, and by asking.
//
// Unlike HTTP, the server says what it accepts first, so the client can pick.
// With no option at all it tries what it can find: unix, then a GSI proxy,
// then a token, then kerberos, then sss. WithAuth pins one instead.
//
// WithCredentialPrompt is the interactive path: when nothing usable is found,
// the terminal asks. Creating a proxy is delegated to the real
// voms-proxy-init with the real terminal wired through, so the private key
// passphrase never passes through this process -- and a pasted token is never
// echoed, nor quoted back into an error message.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdcred"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/gsi"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/token"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// (a) A bearer token over the native protocol ("ztn").
	tok, err := token.Discover()
	if err != nil {
		log.Printf("no token found: %v", err)
	} else {
		cli, err := xrootd.NewClient(ctx, "storage.example.org:1094", "gopher",
			xrootd.WithTLS(), // ztn requires an encrypted connection
			xrootd.WithAuth(&token.Auth{Token: tok}),
		)
		if err != nil {
			log.Fatalf("could not connect with a token: %+v", err)
		}
		cli.Close()
		fmt.Println("token auth ok")
	}

	// (b) A VOMS proxy ("gsi").
	proxy, err := gsi.LoadProxy(gsi.DefaultProxyPath())
	if err != nil {
		log.Printf("no proxy found: %v", err)
	} else {
		cli, err := xrootd.NewClient(ctx, "storage.example.org:1094", "gopher",
			xrootd.WithAuth(proxy),
		)
		if err != nil {
			log.Fatalf("could not connect with a proxy: %+v", err)
		}
		cli.Close()
		fmt.Println("gsi auth ok")
	}

	// (c) Ask the user, if nothing usable is lying around.
	cli, err := xrootd.NewClient(ctx, "storage.example.org:1094", "gopher",
		xrootd.WithCredentialPrompt(xrdcred.NewTerminal()),
	)
	if err != nil {
		log.Fatalf("could not connect: %+v", err)
	}
	defer cli.Close()
	fmt.Println("connected")
}
