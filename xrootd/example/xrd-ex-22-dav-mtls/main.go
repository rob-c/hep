// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-22-dav-mtls authenticates over HTTPS with an X.509 client certificate.
//
// This is what a VOMS proxy is, at the TLS layer: a short-lived certificate
// and its key in one PEM file. The private key never leaves this process, and
// this library never asks for its passphrase -- creating a proxy is delegated
// to voms-proxy-init with the real terminal wired through.
package main

import (
	"context"
	"crypto/x509"
	"fmt"
	"log"
	"os"
	"time"

	"go-hep.org/x/hep/xrootd/xrdhttp"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	// A VOMS proxy holds certificate and key in the same file.
	proxy := fmt.Sprintf("/tmp/x509up_u%d", os.Getuid())
	cert, err := xrdhttp.LoadX509KeyPair(proxy, proxy)
	if err != nil {
		log.Fatalf("could not load the proxy at %s: %+v", proxy, err)
	}

	// The grid's own CAs, which are not in the system pool.
	pool := x509.NewCertPool()
	pem, err := os.ReadFile("/etc/grid-security/certificates/ca-bundle.pem")
	if err != nil {
		log.Fatalf("could not read the CA bundle: %+v", err)
	}
	if !pool.AppendCertsFromPEM(pem) {
		log.Fatal("no certificates found in the CA bundle")
	}

	cli, err := xrdhttp.Dial("https://webdav.example.com:2880/atlas/rucio",
		xrdhttp.WithClientCertificate(cert),
		xrdhttp.WithRootCAs(pool),
	)
	if err != nil {
		log.Fatalf("could not connect: %+v", err)
	}

	entries, err := cli.Dirlist(ctx, "user/analysis")
	if err != nil {
		log.Fatalf("could not list: %+v", err)
	}
	fmt.Printf("%d entries\n", len(entries))
}
