// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdcred // import "go-hep.org/x/hep/xrootd/xrdcred"

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdproto/auth"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/gsi"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/krb5"
)

// haveTool reports whether name can be run.
func haveTool(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// run executes a credential tool with the user's terminal attached.
//
// The tool gets the real terminal, not a pipe, because these tools ask for
// things this package must not: voms-proxy-init wants the passphrase of the
// user's private key. Handing the terminal over means the passphrase goes from
// the user to the tool that needs it and never passes through this process at
// all — no buffer to clear, nothing to leak into an error message. It is also
// why the command is printed before it runs: the user is about to type a
// passphrase, and is entitled to see what they are typing it into.
func (t *Terminal) run(ctx context.Context, name string, args ...string) error {
	t.printf("  running: %s %s\n", name, strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = t.in()
	cmd.Stdout = t.out()
	cmd.Stderr = t.out()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xrdcred: %s failed: %w", name, err)
	}
	return nil
}

// runVOMSProxyInit creates an X.509 proxy with the stock tool and then loads
// whatever it produced.
func runVOMSProxyInit(ctx context.Context, t *Terminal, req xrootd.CredentialRequest) (auth.Auther, error) {
	vo, err := t.ask(ctx, "VO (blank for a plain proxy)", guessVO())
	if err != nil {
		return nil, err
	}

	args := []string{}
	if vo != "" {
		args = append(args, "-voms", vo)
	}
	if err := t.run(ctx, "voms-proxy-init", args...); err != nil {
		return nil, err
	}

	// voms-proxy-init writes to $X509_USER_PROXY or the conventional path; ask
	// the gsi provider where that is rather than assuming.
	return openProxy(t, gsi.DefaultProxyPath())
}

// guessVO offers the VO the user most likely wants, taken from $VOMS_VO or from
// the name of a single VOMS configuration directory if there is exactly one. A
// wrong guess costs a keystroke; no guess costs the user a trip to find out
// what their VO is called.
func guessVO() string {
	if vo := os.Getenv("VOMS_VO"); vo != "" {
		return vo
	}
	for _, dir := range []string{"/etc/vomses", "/etc/grid-security/vomsdir"} {
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var names []string
		for _, e := range ents {
			if name := e.Name(); !strings.HasPrefix(name, ".") {
				names = append(names, strings.TrimSuffix(name, filepath.Ext(name)))
			}
		}
		if len(names) == 1 {
			return names[0]
		}
	}
	return ""
}

// runKinit obtains a Kerberos ticket with the stock tool, then re-discovers the
// credential cache it wrote.
func runKinit(ctx context.Context, t *Terminal, req xrootd.CredentialRequest) (auth.Auther, error) {
	principal, err := t.ask(ctx, "principal (blank for the default)", os.Getenv("KRB5_PRINCIPAL"))
	if err != nil {
		return nil, err
	}

	var args []string
	if principal != "" {
		args = append(args, principal)
	}
	if err := t.run(ctx, "kinit", args...); err != nil {
		return nil, err
	}

	a, err := krb5.WithCredCache()
	if err != nil {
		return nil, fmt.Errorf("xrdcred: kinit ran but no usable ticket was found: %w", err)
	}
	return a, nil
}

// expand resolves a leading "~" and any environment variables in a path the
// user typed, which is how they expect a path they typed to behave.
func expand(path string) string {
	path = os.ExpandEnv(path)
	switch {
	case path == "~":
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	case strings.HasPrefix(path, "~/"):
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
