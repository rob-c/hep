// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/auth"
)

// A Client to xrootd server which allows to send requests and receive responses.
// Concurrent requests are supported.
// Zero value is invalid, Client should be instantiated using NewClient.
type Client struct {
	cancel   context.CancelFunc
	auths    map[string]auth.Auther
	username string
	// initialSessionID is the sessionID of the server which is used as default
	// for all requests that don't specify sessionID explicitly.
	// Any failed request with another sessionID should be redirected to the initialSessionID.
	// See http://xrootd.org/doc/dev45/XRdv310.pdf, page 11 for details.
	initialSessionID string
	mu               sync.RWMutex
	sessions         map[string]*cliSession

	maxRedirections int

	// maxSubs bounds the parallel data connections opened to one server.
	maxSubs int

	// dialTimeout bounds establishing a connection; zero leaves it to the
	// operating system.
	dialTimeout time.Duration
	// waitCap is the longest a single kXR_wait may park a request.
	waitCap time.Duration
	// streamTimeout is how long a connection may go silent while a request is
	// outstanding on it; zero tolerates any silence.
	streamTimeout time.Duration
	// connRetry is how many further attempts a failed connection gets.
	connRetry int
	// keepAlive is the TCP keepalive schedule; the zero value leaves keepalives
	// as the system has them.
	keepAlive net.KeepAliveConfig

	tlsConfig   *tls.Config // TLS configuration for in-protocol TLS upgrades; nil means defaults.
	wantTLS     bool        // request TLS during protocol negotiation (roots:// or WithTLS).
	insecureTLS bool        // skip server-certificate verification (testing only).

	// errorHandler receives the failures that happen where no caller is
	// waiting to be told about them; nil discards them.
	errorHandler ErrorHandler

	// prompter obtains a credential the client is missing; nil never prompts.
	prompter CredentialPrompter
	// promptMu guards the fields below, which are written from every session's
	// login and read when a request comes back unauthorized.
	promptMu sync.Mutex
	// prompted remembers each answer so a user is asked once per client rather
	// than once per redirect.
	prompted map[string]promptResult
	// usedAuth is the security provider the last session logged in with.
	usedAuth string
	// unusedAuth records the providers the server offered that this client had
	// no credential for, and why.
	unusedAuth map[string]error
}

// Option configures an XRootD client.
type Option func(*Client) error

// WithErrorHandler sets what a client does with a failure that happens where
// nobody is waiting for it: a request retried onto a connection that has since
// died, a data connection the server would not bind, a response that arrives
// for a caller who has already gone.
//
// None of these can be returned to anyone — that is what makes them worth
// reporting. Without a handler they are dropped, which is the default because a
// library that writes to standard error is a library that has decided how the
// program logs.
func WithErrorHandler(h ErrorHandler) Option {
	return func(client *Client) error {
		client.errorHandler = h
		return nil
	}
}

// WithAuth adds an authentication mechanism to the XRootD client.
// If an authentication mechanism was already registered for that provider,
// it will be silently replaced.
func WithAuth(a auth.Auther) Option {
	return func(client *Client) error {
		return client.addAuth(a)
	}
}

func (client *Client) addAuth(auth auth.Auther) error {
	client.auths[auth.Provider()] = auth
	return nil
}

func (client *Client) initSecurityProviders() {
	for _, provider := range defaultProviders {
		if provider == nil {
			continue
		}
		client.auths[provider.Provider()] = provider
	}
}

// NewClient creates a new xrootd client that connects to the given address using username.
// Options opts configure the client and are applied in the order they were specified.
// When the context expires, a response handling is stopped, however, it is
// necessary to call Cancel to correctly free resources.
func NewClient(ctx context.Context, address string, username string, opts ...Option) (*Client, error) {
	dialAddr, schemeTLS, err := addressAndTLS(address)
	if err != nil {
		return nil, err
	}
	address = dialAddr

	ctx, cancel := context.WithCancel(ctx)

	client := &Client{
		cancel:          cancel,
		auths:           make(map[string]auth.Auther),
		username:        username,
		sessions:        make(map[string]*cliSession),
		maxRedirections: 10,
		maxSubs:         defaultSubStreams,
		waitCap:         maxWaitDuration,
		prompted:        make(map[string]promptResult),
	}

	client.initSecurityProviders()

	// The environment is applied first so that a caller's own options win: a
	// program that configures its client explicitly should behave the same
	// whatever shell it was started from.
	for _, opt := range append(envOptions(), opts...) {
		if opt == nil {
			continue
		}
		if err := opt(client); err != nil {
			client.Close()
			return nil, err
		}
	}

	// Options run first so an explicit WithInsecureTLS/WithTLSConfig applies;
	// a roots:// or xroots:// scheme additively requests TLS.
	if schemeTLS {
		client.wantTLS = true
	}

	_, err = client.getSession(ctx, address, "")
	if err != nil {
		client.Close()
		return nil, err
	}

	return client, nil
}

// Close closes the connection. Any blocked operation will be unblocked and return error.
func (client *Client) Close() error {
	if client == nil {
		return os.ErrInvalid
	}
	defer client.cancel()

	client.mu.Lock()
	defer client.mu.Unlock()

	var errs []error
	for _, session := range client.sessions {
		err := session.Close()
		if err != nil {
			errs = append(errs, err)
		}
	}
	if errs != nil {
		return fmt.Errorf("xrootd: could not close client: %v", errs)
	}
	return nil
}

// Send sends the request to the server and stores the response inside the resp.
// If the resp is nil, then no response is stored.
// Send returns a session id which identifies the server that provided response.
func (client *Client) Send(ctx context.Context, resp xrdproto.Response, req xrdproto.Request) (string, error) {
	if client == nil {
		return "", os.ErrInvalid
	}

	return client.sendSession(ctx, client.initialSessionID, resp, req)
}

// A reopener re-establishes, at another server, the file that a redirected
// request refers to. It is implemented by the open file itself, which is the
// only thing that knows the path and the options it was opened with.
//
// reopen returns the handle the file was given at the server it ended up on,
// and the id of that server: opening it may itself be redirected once more.
type reopener interface {
	reopen(ctx context.Context, sessionID, opaque string) (xrdfs.FileHandle, string, error)
}

func (client *Client) sendSession(ctx context.Context, sessionID string, resp xrdproto.Response, req xrdproto.Request) (string, error) {
	return client.sendSessionFile(ctx, sessionID, resp, req, nil)
}

// sendSessionFile is sendSession for a request that may name an open file. re
// re-opens that file wherever the request is redirected to; it is nil for a
// request that names no file, and a redirected request that names one without
// it is an error rather than a request sent with a handle no server would
// recognize.
func (client *Client) sendSessionFile(ctx context.Context, sessionID string, resp xrdproto.Response, req xrdproto.Request, re reopener) (string, error) {
	client.mu.RLock()
	session, ok := client.sessions[sessionID]
	client.mu.RUnlock()
	if !ok {
		// The session is gone because its connection failed and it was
		// dropped, and the id is the address of the server it was talking to.
		// Dialling it again is what a caller would otherwise have to do by
		// hand, and it is what makes a server restart survivable: the request
		// that arrives next reconnects instead of failing for ever.
		var err error
		session, err = client.getSession(ctx, sessionID, "")
		if err != nil {
			return "", fmt.Errorf("xrootd: could not reconnect to %q: %w", sessionID, err)
		}
	}

	redirection, err := session.Send(ctx, resp, req)
	if err != nil {
		return sessionID, err
	}

	for cnt := client.maxRedirections; redirection != nil && cnt > 0; cnt-- {
		sessionID = redirection.Addr
		if fp, ok := req.(xrdproto.FilepathRequest); ok {
			addOpaque(fp, redirection.Opaque)
		}
		session, err = client.getSession(ctx, sessionID, redirection.Token)
		if err != nil {
			return sessionID, err
		}

		// A file handle names state on the server that issued it, and the
		// server this request is being moved to has none of that state. The
		// file is opened there and the request pointed at the handle that open
		// returns; a redirect means the request was not carried out, so
		// nothing is done twice by re-issuing it.
		//
		// re says the request came from an open file, which is the only thing
		// that fills a handle in. The request types that can carry one are also
		// usable by path — a kXR_stat or a kXR_truncate names either — and
		// those have nothing to re-open.
		if fh, ok := req.(xrdproto.FilehandleRequest); ok && re != nil {
			handle, id, err := re.reopen(ctx, sessionID, redirection.Opaque)
			if err != nil {
				return sessionID, err
			}
			if id != sessionID {
				// Opening was itself redirected: the handle belongs to
				// wherever it ended up, and so must the request that uses it.
				sessionID = id
				session, err = client.getSession(ctx, sessionID, redirection.Token)
				if err != nil {
					return sessionID, err
				}
			}
			fh.SetHandle(handle)
		}

		redirection, err = session.Send(ctx, resp, req)
		if err != nil {
			return sessionID, err
		}
	}

	if redirection != nil {
		err = fmt.Errorf("xrootd: received %d redirections in a row, aborting request", client.maxRedirections)
	}

	return sessionID, err
}

// addOpaque adds the opaque data a redirect carried to the request that is
// being re-issued. The protocol says that data is added to the file name, so it
// is appended rather than assigned: overwriting would drop the caller's own
// opaque data — the authorization token an open usually travels with — the
// moment a namespace server redirected the request. A redirect with no opaque
// data leaves the path alone, down to the "?" that would otherwise be appended
// to it.
func addOpaque(req xrdproto.FilepathRequest, opaque string) {
	if opaque == "" {
		return
	}
	if cur := req.Opaque(); cur != "" {
		opaque = cur + "&" + opaque
	}
	req.SetOpaque(opaque)
}

// forget drops sess from the table of live sessions, so the next request meant
// for that server dials a new connection rather than writing to a socket that
// is already gone.
//
// A session that is already replaced is left alone: the connection that died is
// not necessarily the one the table now holds for that address.
func (client *Client) forget(sess *cliSession) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.sessions[sess.sessionID] == sess {
		delete(client.sessions, sess.sessionID)
	}
}

func (client *Client) getSession(ctx context.Context, address, token string) (*cliSession, error) {
	client.mu.RLock()
	v, ok := client.sessions[address]
	client.mu.RUnlock()
	if ok {
		return v, nil
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	// Another caller may have connected to the same server while this one was
	// waiting for the lock. Dialling again would leave that connection in the
	// table unreferenced: open, logged in, and never closed.
	if v, ok := client.sessions[address]; ok {
		return v, nil
	}
	session, err := newSession(ctx, address, client.username, token, client)
	if err != nil {
		return nil, err
	}
	client.sessions[address] = session

	if len(client.initialSessionID) == 0 {
		client.initialSessionID = address
	}
	// The initial session stays the one the client was created against, and is
	// deliberately not moved to whatever a redirect led to: it is the manager
	// that knows where things are, and a request with no session of its own is
	// meant to start there so that it can be sent somewhere else. Pointing it
	// at a data server would have every later request begin at a server that
	// holds one file and can redirect nothing.
	// See http://xrootd.org/doc/dev45/XRdv310.pdf, p. 11 for details.

	return session, nil
}
