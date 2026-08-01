// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the XRD_* environment variables.
//
// A site configures its jobs through the environment, once, for whatever client
// they happen to use. A client that ignores it is configured by nobody: the
// redirect limit, the connection window and the TLS requirement a site set for
// libXrdCl have to reach this client too, or a job that works with one client
// fails with the other for reasons nothing in its own configuration explains.

package xrootd

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/login"
	"go-hep.org/x/hep/xrootd/xrdproto/protocol"
)

// envClient runs a whole bootstrap and hands back the client it produced,
// together with the login request the server saw.
func envClient(t *testing.T, username string, opts ...Option) (*Client, login.Request) {
	t.Helper()

	got := make(chan login.Request, 1)
	addr := bootServer(t, func(conn net.Conn) {
		bootHandshake(t, conn)
		bootProtocol(t, conn, protocol.Response{BinaryProtocolVersion: 0x310, Flags: protocol.IsServer})

		hdr, data := readBootstrapRequest(conn)
		var req login.Request
		_ = req.UnmarshalXrd(xrdenc.NewRBuffer(data))
		got <- req
		writeBootstrapResponse(conn, hdr.StreamID, login.Response{})

		// Hold the connection open: closing it here would tear down the
		// session the test is about to inspect.
		_, _ = conn.Read(make([]byte, 1))
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := NewClient(ctx, addr, username, opts...)
	if err != nil {
		t.Fatalf("could not connect: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	select {
	case req := <-got:
		return client, req
	case <-time.After(10 * time.Second):
		t.Fatal("the server never saw a login")
		return nil, login.Request{}
	}
}

func loginName(req login.Request) string {
	return string(bytes.TrimRight(req.Username[:], "\x00"))
}

func TestConformance_TheEnvironmentNamesTheUserWhenTheCallerDoesNot(t *testing.T) {
	t.Setenv(EnvUsername, "fromenv")

	_, req := envClient(t, "")
	if got := loginName(req); got != "fromenv" {
		t.Fatalf("the login asserted %q, want %q", got, "fromenv")
	}
}

func TestConformance_AnExplicitUserNameOverrulesTheEnvironment(t *testing.T) {
	// A caller that names a user has made a decision. The shell the process was
	// started from does not get to overrule it, or a program cannot rely on its
	// own configuration.
	t.Setenv(EnvUsername, "fromenv")

	_, req := envClient(t, "gopher")
	if got := loginName(req); got != "gopher" {
		t.Fatalf("the login asserted %q, want %q", got, "gopher")
	}
}

func TestConformance_AnOptionOverrulesTheEnvironment(t *testing.T) {
	t.Setenv(EnvRedirectLimit, "1")
	t.Setenv(EnvConnectionWindow, "7")

	client, _ := envClient(t, "gopher", WithRedirectLimit(5))
	if got, want := client.maxRedirections, 5; got != want {
		t.Fatalf("redirect limit: got = %d, want = %d", got, want)
	}
	// The variable the caller said nothing about still applies.
	if got, want := client.dialTimeout, 7*time.Second; got != want {
		t.Fatalf("connection window: got = %v, want = %v", got, want)
	}
}

func TestConformance_TheEnvironmentConfiguresTheClient(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want func(*Client) (string, bool)
	}{
		{
			name: "a required TLS",
			env:  map[string]string{EnvRequireTLS: "1"},
			want: func(c *Client) (string, bool) { return "wantTLS", c.wantTLS },
		},
		{
			name: "a required TLS, spelled as the C client spells it",
			env:  map[string]string{EnvRequireTLS: " TRUE "},
			want: func(c *Client) (string, bool) { return "wantTLS", c.wantTLS },
		},
		{
			name: "a TLS that is not required",
			env:  map[string]string{EnvRequireTLS: "0"},
			want: func(c *Client) (string, bool) { return "no wantTLS", !c.wantTLS },
		},
		{
			// "false" is not a spelling of "yes", and neither is anything else
			// the C client does not accept.
			name: "a variable set to something else entirely",
			env:  map[string]string{EnvRequireTLS: "maybe"},
			want: func(c *Client) (string, bool) { return "no wantTLS", !c.wantTLS },
		},
		{
			name: "an unverified certificate",
			env:  map[string]string{EnvTLSNoCertVerify: "yes"},
			want: func(c *Client) (string, bool) { return "insecureTLS", c.insecureTLS },
		},
		{
			name: "a connection window",
			env:  map[string]string{EnvConnectionWindow: "30"},
			want: func(c *Client) (string, bool) { return "dialTimeout", c.dialTimeout == 30*time.Second },
		},
		{
			name: "a request timeout",
			env:  map[string]string{EnvRequestTimeout: "90"},
			want: func(c *Client) (string, bool) { return "waitCap", c.waitCap == 90*time.Second },
		},
		{
			name: "a redirect limit",
			env:  map[string]string{EnvRedirectLimit: "3"},
			want: func(c *Client) (string, bool) { return "maxRedirections", c.maxRedirections == 3 },
		},
		{
			// An empty variable is one that was unset by a script, not one that
			// asks for zero.
			name: "a variable set to nothing",
			env:  map[string]string{EnvRedirectLimit: "", EnvConnectionWindow: ""},
			want: func(c *Client) (string, bool) {
				return "the defaults", c.maxRedirections == 10 && c.dialTimeout == 0
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			client := &Client{maxRedirections: 10, waitCap: maxWaitDuration}
			for _, opt := range envOptions() {
				if opt == nil {
					continue
				}
				if err := opt(client); err != nil {
					t.Fatalf("could not apply the environment: %v", err)
				}
			}
			if what, ok := tc.want(client); !ok {
				t.Fatalf("the environment did not produce %s: %+v", what, tc.env)
			}
		})
	}
}

func TestConformance_ANumberTheEnvironmentCannotHoldIsAnError(t *testing.T) {
	// The C client counts seconds and takes no unit, so "30s" is exactly the
	// kind of thing that ends up in one of these variables. Ignoring it would
	// leave the user believing a setting is in force that is not.
	for _, name := range []string{EnvConnectionWindow, EnvRequestTimeout, EnvRedirectLimit} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "30s")

			var err error
			client := &Client{}
			for _, opt := range envOptions() {
				if opt == nil {
					continue
				}
				if err = opt(client); err != nil {
					break
				}
			}
			if err == nil {
				t.Fatalf("%s=30s was accepted", name)
			}
			if !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "30s") {
				t.Fatalf("the error does not say what was wrong: %v", err)
			}
		})
	}
}

func TestConformance_AnOptionRefusesWhatItCannotMean(t *testing.T) {
	client := &Client{}
	if err := WithRedirectLimit(-1)(client); err == nil {
		t.Fatal("a negative redirect limit was accepted")
	}
	if err := WithRequestTimeout(0)(client); err == nil {
		t.Fatal("a request timeout of zero was accepted")
	}
	// A negative window is no window, which is the default: it disables the
	// bound rather than making every connection fail instantly.
	if err := WithConnectionWindow(-time.Second)(client); err != nil {
		t.Fatalf("a negative connection window: %v", err)
	}
	if client.dialTimeout != 0 {
		t.Fatalf("a negative connection window became %v", client.dialTimeout)
	}
	if err := WithUsername("gopher")(client); err != nil || client.username != "gopher" {
		t.Fatalf("WithUsername: %v, username = %q", err, client.username)
	}
}

func TestConformance_TheConnectionWindowBoundsTheConnection(t *testing.T) {
	// The window bounds establishing the connection and nothing else. It is
	// applied to a context of its own: a window applied to the session's
	// context would tear the session down as soon as it elapsed, which is the
	// kind of bug that shows up as a connection that works for a minute.
	addr := bootServer(t, func(conn net.Conn) { _, _ = conn.Read(make([]byte, 1)) })

	var d net.Dialer
	conn, err := dial(context.Background(), &d, addr, &Client{dialTimeout: time.Nanosecond})
	if err == nil {
		conn.Close()
		t.Fatal("a connection window of a nanosecond was not applied")
	}
	if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("the connection failed for the wrong reason: %v", err)
	}

	// The same address, with no window, connects.
	conn, err = dial(context.Background(), &d, addr, &Client{})
	if err != nil {
		t.Fatalf("could not connect without a window: %v", err)
	}
	conn.Close()

	// A session built outside a Client has no window to consult.
	conn, err = dial(context.Background(), &d, addr, nil)
	if err != nil {
		t.Fatalf("could not connect without a client: %v", err)
	}
	conn.Close()
}

func TestConformance_TheRequestTimeoutCapsAWaitTheServerAsksFor(t *testing.T) {
	// A server can park a request for as long as the 32-bit field allows. The
	// cap is what turns that into a bounded wait, and a configured cap has to
	// be the one that applies — otherwise the setting is decorative and the
	// request is held for the default hour.
	var w xrdenc.WBuffer
	if err := (xrdproto.WaitResponse{Duration: time.Hour}).MarshalXrd(&w); err != nil {
		t.Fatalf("could not marshal the wait response: %v", err)
	}

	parked := make(chan struct{})
	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		sid := xrdproto.StreamID{data[0], data[1]}
		if err := xrdproto.WriteResponse(conn, sid, xrdproto.Wait, xrdproto.WaitResponse{Duration: time.Hour}); err != nil {
			cancel()
			return
		}
		// The re-issued request is the whole answer: it says the client waited
		// the capped time rather than the hour it was asked for.
		if _, err := xrdproto.ReadRequest(conn); err != nil {
			cancel()
			return
		}
		close(parked)
		if err := xrdproto.WriteResponse(conn, sid, xrdproto.Ok, nil); err != nil {
			cancel()
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		client.waitCap = 10 * time.Millisecond

		ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if err := client.FS().RemoveFile(ctx, "/tmp/file"); err != nil {
			t.Errorf("RemoveFile: %v", err)
		}
		select {
		case <-parked:
		default:
			t.Error("the request was never re-issued")
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}
