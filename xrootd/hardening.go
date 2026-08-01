// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// The failure a wide-area network actually produces is rarely a closed socket.
// A path through a firewall that forgets the flow, a router that drops the
// return leg, a storage node that is swapped out mid-transfer: in every one of
// them the connection stays open as far as both kernels are concerned and no
// byte ever arrives again. TCP alone answers that after minutes to hours, if at
// all, and a caller waiting on a read has no way to tell it apart from a server
// that is merely slow.
//
// The options here are what makes that case bounded rather than indefinite:
// a stream timeout that decides when silence has gone on too long, keepalives
// that make the kernel probe rather than wait, and a bounded retry of the
// connection itself, which is the one thing that can safely be repeated because
// nothing has been sent on it yet.
const (
	// EnvStreamTimeout bounds, in seconds, how long a connection may go silent
	// while a request is outstanding.
	EnvStreamTimeout = "XRD_STREAMTIMEOUT"
	// EnvConnectionRetry bounds how many times a failed connection is retried.
	EnvConnectionRetry = "XRD_CONNECTIONRETRY"
	// EnvKeepAlive asks for TCP keepalive probes on every connection.
	EnvKeepAlive = "XRD_TCPKEEPALIVE"
	// EnvKeepAliveTime sets, in seconds, how long a connection may be idle
	// before the first keepalive probe is sent.
	EnvKeepAliveTime = "XRD_TCPKEEPALIVETIME"
	// EnvKeepAliveInterval sets, in seconds, the gap between keepalive probes.
	EnvKeepAliveInterval = "XRD_TCPKEEPALIVEINTVL"
	// EnvKeepAliveProbes sets how many unanswered keepalive probes end the
	// connection.
	EnvKeepAliveProbes = "XRD_TCPKEEPALIVEPROBES"
)

// The settings Hardened applies. They are the C++ client's defaults where it
// has one, which is what a site's firewall and load-balancer timeouts have been
// tuned against.
const (
	// hardenedStreamTimeout is how long a server may say nothing while it owes
	// an answer. Long enough that a busy disk server reading from tape is not
	// cut off, short enough that a black-holed path is noticed within a minute
	// rather than at the end of the job.
	hardenedStreamTimeout = 60 * time.Second
	// hardenedConnectionWindow bounds one connection attempt. A grid endpoint
	// behind a manager can take a few seconds to answer; thirty is generous
	// and still leaves room for the retries below inside a job's patience.
	hardenedConnectionWindow = 30 * time.Second
	// hardenedConnectionRetry is how many further attempts a failed connection
	// gets. Five is the C++ client's XRD_CONNECTIONRETRY default.
	hardenedConnectionRetry = 5
	// The keepalive schedule: probe after half a minute of silence, then every
	// ten seconds, and give up after three unanswered probes. Chosen to fire
	// well inside the idle timeout of a typical stateful firewall, so the flow
	// is refreshed rather than quietly dropped.
	hardenedKeepAliveIdle     = 30 * time.Second
	hardenedKeepAliveInterval = 10 * time.Second
	hardenedKeepAliveProbes   = 3
)

// Hardened configures a client for a network that cannot be trusted to fail
// loudly: a stream timeout, a bounded connection window, retried connections,
// and TCP keepalives.
//
// [NewClient] applies it already. It is the default because the caller who has
// not thought about how a wide-area path fails is precisely the caller who
// should not discover it as a program that never returns: without a stream
// timeout, a connection that stops forwarding while both kernels still believe
// it is open blocks a read until TCP gives up, which on Linux is the better
// part of an hour. A default that hangs is not a neutral choice.
//
// It is named, and exported, so that it can be spelled out where the reader of
// a program needs to see it, and so that the settings it applies have somewhere
// to be documented. Passing it explicitly changes nothing.
//
// It is an ordinary option, so anything applied after it wins:
//
//	cli, err := xrootd.NewClient(ctx, addr, user,
//		xrootd.WithStreamTimeout(5*time.Minute), // a site that stages from tape
//	)
//
// [Unbounded] is the way back to no bounds at all.
func Hardened() Option {
	return func(client *Client) error {
		for _, opt := range []Option{
			WithStreamTimeout(hardenedStreamTimeout),
			WithConnectionWindow(hardenedConnectionWindow),
			WithConnectionRetry(hardenedConnectionRetry),
			WithKeepAlive(hardenedKeepAliveIdle, hardenedKeepAliveInterval, hardenedKeepAliveProbes),
		} {
			if err := opt(client); err != nil {
				return err
			}
		}
		return nil
	}
}

// Unbounded removes every bound [Hardened] applies: no stream timeout, no
// connection window, no connection retry, no keepalives. An operation then
// waits for as long as the caller's context allows, and a connection that has
// stopped forwarding is noticed when TCP notices it.
//
// This is what the client did before the bounds became the default, and there
// are callers who want it: a test that means to observe a hang, a program on a
// local network where a bounded wait would only turn a slow server into a
// failed one, a caller whose own context is the only deadline it wants. It is
// deliberately not the default, because wanting it requires knowing it exists.
//
// Like any option it can be followed by another, so a caller can drop the
// bounds and then put one back:
//
//	cli, err := xrootd.NewClient(ctx, addr, user,
//		xrootd.Unbounded(),
//		xrootd.WithKeepAlive(30*time.Second, 10*time.Second, 3),
//	)
func Unbounded() Option {
	return func(client *Client) error {
		client.streamTimeout = 0
		client.dialTimeout = 0
		client.connRetry = 0
		client.keepAlive = net.KeepAliveConfig{}
		return nil
	}
}

// WithStreamTimeout sets how long a connection may go silent while the client
// is waiting for an answer on it. A zero or negative duration disables the
// bound, which is where it is by default.
//
// This is the only thing that detects a path that has gone away without
// closing: the socket stays open, the read blocks, and the request behind it
// waits for as long as the caller's context allows. When the timeout elapses
// with a request outstanding the connection is torn down and everything waiting
// on it is failed, so a caller is told rather than left hanging.
//
// An idle connection is not affected. Silence with nothing outstanding is what
// a connection between transfers looks like, and closing it would mean
// reconnecting for the next read.
func WithStreamTimeout(d time.Duration) Option {
	return func(client *Client) error {
		client.streamTimeout = max(d, 0)
		return nil
	}
}

// WithConnectionRetry sets how many further attempts a failed connection gets,
// spaced by an exponential backoff with jitter. Zero, the default, makes one
// attempt and reports what went wrong.
//
// Retrying is safe here and nowhere else in the protocol: nothing has been sent
// on a connection that was never established, so no request can be executed
// twice. A request that failed on a live connection is not retried, because the
// client cannot know whether the server carried it out before the answer was
// lost.
//
// The caller's context still bounds the whole thing — a deadline that expires
// between attempts ends them.
func WithConnectionRetry(n int) Option {
	return func(client *Client) error {
		if n < 0 {
			return fmt.Errorf("xrootd: connection retry count %d is negative", n)
		}
		client.connRetry = n
		return nil
	}
}

// WithKeepAlive turns on TCP keepalive probes: the connection is probed after
// idle of silence, again every interval, and is dropped after probes of them go
// unanswered. A zero or negative idle turns keepalives off, which is where they
// are by default.
//
// Keepalives are what tells the kernel to ask; they do not bound anything on
// their own, and a middlebox that answers probes on behalf of a path it has
// stopped forwarding defeats them entirely. WithStreamTimeout is the detector.
// What keepalives add is traffic on an otherwise idle connection, which is what
// keeps a stateful firewall from forgetting the flow in the first place.
//
// Not every platform honours every field; those it cannot set are left as the
// system has them.
func WithKeepAlive(idle, interval time.Duration, probes int) Option {
	return func(client *Client) error {
		if probes < 0 {
			return fmt.Errorf("xrootd: keepalive probe count %d is negative", probes)
		}
		if idle <= 0 {
			client.keepAlive = net.KeepAliveConfig{}
			return nil
		}
		client.keepAlive = net.KeepAliveConfig{
			Enable:   true,
			Idle:     idle,
			Interval: interval,
			Count:    probes,
		}
		return nil
	}
}

// hardeningEnvOptions returns the network settings the environment asks for.
func hardeningEnvOptions() []Option {
	return []Option{
		envSeconds(EnvStreamTimeout, WithStreamTimeout),
		envCount(EnvConnectionRetry, WithConnectionRetry),
		envKeepAlive(),
	}
}

// envKeepAlive reads the four variables that describe a keepalive schedule as
// the one option they configure. The schedule is only applied when
// XRD_TCPKEEPALIVE asks for it: a lone XRD_TCPKEEPALIVETIME is a tuning for
// keepalives that were never turned on, and the C++ client reads it the same
// way.
func envKeepAlive() Option {
	if !envBool(EnvKeepAlive) {
		return nil
	}
	idle, err := envDurationOr(EnvKeepAliveTime, hardenedKeepAliveIdle)
	if err != nil {
		return func(*Client) error { return err }
	}
	interval, err := envDurationOr(EnvKeepAliveInterval, hardenedKeepAliveInterval)
	if err != nil {
		return func(*Client) error { return err }
	}
	probes := hardenedKeepAliveProbes
	if v, ok := os.LookupEnv(EnvKeepAliveProbes); ok && strings.TrimSpace(v) != "" {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return envError(EnvKeepAliveProbes, v)
		}
		probes = n
	}
	return WithKeepAlive(idle, interval, probes)
}

// envDurationOr reads a whole number of seconds from the named variable,
// falling back to def when it is unset.
func envDurationOr(name string, def time.Duration) (time.Duration, error) {
	v, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(v) == "" {
		return def, nil
	}
	secs, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0, fmt.Errorf("xrootd: %s=%q is not a number", name, v)
	}
	return time.Duration(secs) * time.Second, nil
}

// The backoff between connection attempts.
const (
	// connectBackoffBase is the pause before the second attempt. Short enough
	// that a server restarting is caught on the way back up, long enough that a
	// client is not hammering a host that is refusing connections.
	connectBackoffBase = 250 * time.Millisecond
	// connectBackoffMax caps the doubling, so a generous retry count does not
	// turn into a wait measured in minutes.
	connectBackoffMax = 5 * time.Second
	// connectBackoffJitter is how much of each pause is randomised, as a
	// fraction. Without it, a job that lost its storage element brings every
	// one of its clients back at the same instant.
	connectBackoffJitter = 0.25
)

// connectBackoff returns the pause before attempt n+1, counting the first
// attempt as 0.
func connectBackoff(n int) time.Duration {
	d := connectBackoffBase
	for range n {
		d *= 2
		if d >= connectBackoffMax {
			d = connectBackoffMax
			break
		}
	}
	jitter := connectBackoffJitter * float64(d) * (2*rand.Float64() - 1)
	return max(time.Duration(float64(d)+jitter), 0)
}

// streamTimeout is the silence this session tolerates, which is the client's
// when it has one: a session made outside a Client has no configuration to
// consult and tolerates any.
func (sess *cliSession) streamTimeout() time.Duration {
	if sess.client != nil {
		return sess.client.streamTimeout
	}
	return 0
}

// hasPending reports whether any request is still waiting for an answer on this
// session. It is what separates a connection that has gone away from one that
// is merely between transfers.
func (sess *cliSession) hasPending() bool {
	sess.mu.RLock()
	defer sess.mu.RUnlock()
	return len(sess.requests) > 0
}

// countingReader counts the bytes that reached the caller, so a read that
// failed part way through a response frame can be told from one that failed
// with nothing received.
//
// io.ReadFull reports a deadline the same way in both cases, and they are not
// the same thing: nothing arrived means the connection was idle and the
// deadline can simply be pushed out, whereas a frame that stopped half way
// means the stream is out of step with the sender and the only correct move is
// to drop it.
type countingReader struct {
	r io.Reader
	n int
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	cr.n += n
	return n, err
}

// dialOnce connects to addr, bounded by the client's connection window when it
// has one. The bound is applied to a context of its own: the session's context
// outlives the connection attempt, and cancelling it here would tear down the
// session as soon as the window elapsed.
func dialOnce(ctx context.Context, d *net.Dialer, addr string, window time.Duration) (net.Conn, error) {
	if window > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, window)
		defer cancel()
	}
	return d.DialContext(ctx, "tcp", addr)
}
