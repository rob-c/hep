// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"go-hep.org/x/hep/xrootd/internal/mux"
	"go-hep.org/x/hep/xrootd/internal/xrdenc"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/protocol"
	"go-hep.org/x/hep/xrootd/xrdproto/signing"
	"go-hep.org/x/hep/xrootd/xrdproto/sigver"
)

// cliSession is a connection to the specific XRootD server
// which allows to send requests and receive responses.
// Concurrent requests are supported.
// Zero value is invalid, cliSession should be instantiated using newSession.
//
// The cliSession is used by the Client to send requests to the particular server
// specified by the name and port. If the current server cannot
// handle a request, it responds with the redirect to the new server.
// After that, Client obtains a session associated with that server and
// re-issues the request. Stream ID may be different during these 2 requests
// because it is used to identify requests among one particular server
// and is not shared between servers in any way.
//
// If the request that supports sending data over a separate socket is issued,
// the session tries to obtain a sub-session to the same server using a `bind` request.
// If the connection is successful, the request is sent specifying that socket for the data exchange.
// Otherwise, a default socket connected to the server is used.
type cliSession struct {
	ctx              context.Context
	cancel           context.CancelFunc
	conn             net.Conn
	mux              *mux.Mux
	protocolVersion  int32
	signRequirements signing.Requirements
	seqID            int64
	// signKey is the session key the security provider agreed with the server,
	// and is what kXR_sigver signatures are keyed with. It is nil for a session
	// that authenticated with a provider agreeing no secret (unix, host, sss,
	// ztn), and such a session cannot sign.
	signKey  []byte
	mu       sync.RWMutex
	requests map[xrdproto.StreamID]pendingRequest

	subCreateMu sync.Mutex   // subCreateMu is used to serialize the creation of sub-sessions.
	subsMu      sync.RWMutex // subsMu is used to serialize the access to the subs map.
	subs        map[xrdproto.PathID]*cliSession

	// maxSubs bounds the parallel data connections this session opens to its
	// server; see WithSubStreams.
	maxSubs   int
	freeSubs  chan xrdproto.PathID
	isSub     bool // indicates whether this session is a sub-session.
	client    *Client
	sessionID string
	addr      string
	loginID   [16]byte
	pathID    xrdproto.PathID

	wantTLS      bool              // client requested TLS (roots:// or WithTLS)
	protocolInfo protocol.Response // cached kXR_protocol response from bootstrap
}

// pendingRequest is a request that has been sent to the remote server.
type pendingRequest struct {
	// Header is the header part of the request.
	// It may contain all of the request content if there is no data that is
	// intended to be sent over a separate socket.
	Header []byte

	// Data is the data part of the request that is intended to be sent over a separate socket.
	Data []byte

	// PathID is the identifier of the socket which should be used to read or write a data.
	PathID xrdproto.PathID
}

func newSession(ctx context.Context, address, username, token string, client *Client) (*cliSession, error) {
	ctx, cancel := context.WithCancel(ctx)

	var d net.Dialer
	addr := parseAddr(address)
	conn, err := dial(ctx, &d, addr, client)
	if err != nil {
		cancel()
		return nil, err
	}

	maxSubs := defaultSubStreams
	if client != nil {
		maxSubs = client.maxSubs
	}

	sess := &cliSession{
		ctx:    ctx,
		cancel: cancel,
		conn:   conn,
		mux:    mux.New(),
		subs:   make(map[xrdproto.PathID]*cliSession),
		// Buffered to hold every path the session may open, so releasing one
		// never blocks and never has to be deferred to a goroutine: a path
		// that is not back in the pool by the time the next request looks for
		// one has a second connection opened in its place, and a loop of small
		// writes would open the whole allowance one write at a time.
		freeSubs:  make(chan xrdproto.PathID, maxSubs),
		requests:  make(map[xrdproto.StreamID]pendingRequest),
		client:    client,
		sessionID: addr,
		addr:      addr,
		maxSubs:   maxSubs,
	}
	// client is nil when a bare session is created outside of a Client
	// (some tests do this); such sessions cannot request TLS themselves
	// but still honour a server-mandated kXR_gotoTLS in upgradeTLS.
	if client != nil {
		sess.wantTLS = client.wantTLS
	}

	// Bootstrap runs synchronously so that consume() does not race the socket
	// during the handshake, protocol negotiation, and TLS upgrade: TLS replaces
	// sess.conn in place, which is only safe while no other goroutine reads it.
	//
	// It reads the socket directly, ahead of the read loop that applies the
	// stream timeout, so the bound has to be applied here as well: a host that
	// completes the TCP connection and then says nothing — a load balancer in
	// front of a dead backend is the usual way — would otherwise hold the
	// handshake for as long as the caller's context allows.
	bootCtx := ctx
	if t := sess.streamTimeout(); t > 0 {
		var cancel context.CancelFunc
		bootCtx, cancel = context.WithTimeout(ctx, t)
		defer cancel()
	}

	if err := sess.handshakeBootstrap(bootCtx); err != nil {
		sess.Close()
		return nil, err
	}

	protocolInfo, err := sess.protocolBootstrap(bootCtx)
	if err != nil {
		sess.Close()
		return nil, err
	}
	sess.protocolInfo = protocolInfo
	sess.signRequirements = signing.New(protocolInfo.SecurityLevel, protocolInfo.SecurityOverrides)

	// TLS decision, mirroring the reference C client: upgrade when the server
	// mandates it (kXR_gotoTLS) or when the client wanted TLS and the server
	// supports it; refuse to continue in cleartext when TLS was wanted but the
	// server offers none (no silent downgrade).
	if sess.protocolInfo.NeedsTLS(sess.wantTLS) {
		if err := sess.upgradeTLS(bootCtx); err != nil {
			sess.Close()
			return nil, err
		}
	} else if sess.wantTLS && !sess.protocolInfo.HasTLS() {
		sess.Close()
		return nil, fmt.Errorf("xrootd: TLS requested but server %q offers no TLS", sess.addr)
	}

	go sess.consume()

	securityInfo, err := sess.Login(ctx, username, token)
	if err != nil {
		sess.Close()
		return nil, err
	}
	sess.loginID = securityInfo.SessionID

	if len(securityInfo.SecurityInformation) > 0 {
		if err := sess.auth(ctx, securityInfo.SecurityInformation); err != nil {
			sess.Close()
			return nil, err
		}
	}

	return sess, nil
}

// bootstrapExchange writes a single request and synchronously reads exactly one
// response frame directly off sess.conn. It is used only during session
// bootstrap (protocol negotiation and, for TLS, the pre-login exchanges),
// before the consume() read-loop goroutine takes ownership of the socket.
func (sess *cliSession) bootstrapExchange(ctx context.Context, streamID xrdproto.StreamID, req xrdproto.Request, resp xrdproto.Response) error {
	var wBuffer xrdenc.WBuffer
	header := xrdproto.RequestHeader{StreamID: streamID, RequestID: req.ReqID()}
	if err := header.MarshalXrd(&wBuffer); err != nil {
		return err
	}
	if err := req.MarshalXrd(&wBuffer); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := sess.conn.SetDeadline(deadline); err != nil {
			return err
		}
		defer sess.conn.SetDeadline(time.Time{})
	}
	if _, err := sess.conn.Write(wBuffer.Bytes()); err != nil {
		return fmt.Errorf("xrootd: could not send bootstrap request: %w", err)
	}

	var respHeader xrdproto.ResponseHeader
	headerBytes := make([]byte, xrdproto.ResponseHeaderLength)
	data, err := xrdproto.ReadResponseWithReuse(sess.conn, headerBytes, &respHeader)
	if err != nil {
		return fmt.Errorf("xrootd: could not read bootstrap response: %w", err)
	}
	if respHeader.Status == xrdproto.Error {
		return respHeader.Error(data)
	}
	if resp == nil {
		return nil
	}
	return resp.UnmarshalXrd(xrdenc.NewRBuffer(data))
}

// Close closes the connection. Any blocked operation will be unblocked and return error.
func (sess *cliSession) Close() error {
	if sess == nil {
		return os.ErrInvalid
	}

	sess.cancel()

	var errs []error
	for _, child := range sess.subs {
		err := child.Close()
		if err != nil {
			errs = append(errs, err)
		}
	}

	if !sess.isSub {
		sess.mux.Close()
	}

	// The client's table is not touched here: Close is what Client.Close calls
	// for every session it holds, and a session that removed itself would be
	// mutating the table that call is walking. A session that dies on its own
	// is dropped by handleReadError, which is the case where the table would
	// otherwise keep a corpse.
	err := sess.conn.Close()
	if err != nil {
		errs = append(errs, err)
	}
	if errs != nil {
		return fmt.Errorf("xrootd: errors occured during closing of the session: %v", errs)
	}
	return nil
}

// handleReadError handles an error encountered while reading and parsing a response.
// If the current session is the initial one, its connection is gone and every
// request waiting on it is failed. Otherwise, the current session is closed and
// all requests are redirected to the initial session.
// See http://xrootd.org/doc/dev45/XRdv310.pdf, p. 11 for details.
func (sess *cliSession) handleReadError(err error) {
	// Whatever happens next, this session is finished: its connection failed
	// and nothing can be sent on it again. Dropping it from the client means
	// the request that comes after reconnects instead of being handed the same
	// dead socket for the rest of the program's life.
	sess.client.forget(sess)

	if sess.sessionID == sess.client.initialSessionID {
		// There is nowhere to redirect these requests to: every other
		// session is reached through this one. Failing the callers is the
		// only honest answer, and it is the one thing that must not be got
		// wrong — a server closing its end of a socket is routine (an idle
		// timeout, a restart, a network blip), and it used to panic and
		// take the caller's program down with it.
		sess.failPending(err)
		sess.Close()
		return
	}
	sess.mu.RLock()
	resp := mux.ServerResponse{Redirection: &mux.Redirection{Addr: sess.client.initialSessionID}}
	for streamID := range sess.requests {
		if err := sess.mux.SendData(streamID, resp); err != nil {
			// The caller is not reachable through the mux any more, so it
			// cannot be told; it is already being failed by the close below.
			sess.logf("xrootd: could not redirect the request waiting on stream %v of %q: %v", streamID, sess.sessionID, err)
		}
	}
	sess.mu.RUnlock()
	sess.Close()
}

// failPending answers every request still waiting on this session with err.
//
// Closing the session would release them too, but with the mux's own "close was
// called" message: the error the caller sees should say what actually happened
// to the connection.
func (sess *cliSession) failPending(err error) {
	sess.mu.RLock()
	ids := make([]xrdproto.StreamID, 0, len(sess.requests))
	for streamID := range sess.requests {
		ids = append(ids, streamID)
	}
	sess.mu.RUnlock()

	resp := mux.ServerResponse{Err: fmt.Errorf(
		"xrootd: connection to %q was lost: %w", sess.conn.RemoteAddr(), err,
	)}
	for _, streamID := range ids {
		// Nothing to do about a failure here: the caller this was meant for
		// is exactly who we would have reported it to.
		_ = sess.mux.SendData(streamID, resp)
	}
}

// dial connects to addr, bounded by the client's connection window and retried
// as many times as the client allows.
//
// A connection that was never established has carried no request, so repeating
// the attempt cannot repeat any work: this is the one retry in the client that
// is safe without knowing what the server did. Without a retry count — the
// default — it is a single attempt and the error is the dialler's own, which is
// what a caller looking for "connection refused" expects to find.
func dial(ctx context.Context, d *net.Dialer, addr string, client *Client) (net.Conn, error) {
	var (
		window  time.Duration
		tries   = 1
		lastErr error
	)
	if client != nil {
		window = client.dialTimeout
		tries += client.connRetry
		if client.keepAlive.Enable {
			d.KeepAliveConfig = client.keepAlive
		}
	}

	for i := range tries {
		if i > 0 {
			timer := time.NewTimer(connectBackoff(i - 1))
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, fmt.Errorf("xrootd: could not connect to %q after %d attempts: %w", addr, i, lastErr)
			case <-timer.C:
			}
		}
		conn, err := dialOnce(ctx, d, addr, window)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			// The caller gave up, or its deadline passed. Trying again would
			// only fail the same way, and the error already says so.
			break
		}
	}
	if tries == 1 {
		return nil, lastErr
	}
	return nil, fmt.Errorf("xrootd: could not connect to %q after %d attempts: %w", addr, tries, lastErr)
}

// maxWaitDuration caps how long a "kXR_wait" may park a request. The delay is
// chosen by the server and the protocol puts no ceiling on it, so an unbounded
// wait — 68 years is expressible in the 32-bit field — would let a server hold
// a request and the goroutine behind it forever. The cap is a client policy:
// a server that really needs longer can ask again when the wait expires.
const maxWaitDuration = time.Hour

// waitCap is the cap this session applies, which is the client's when it has
// one: a session made outside a Client has no configuration to consult and
// falls back to the default.
func (sess *cliSession) waitCap() time.Duration {
	if sess.client != nil && sess.client.waitCap > 0 {
		return sess.client.waitCap
	}
	return maxWaitDuration
}

// handleWaitResponse handles a "kXR_wait" response by re-issuing the request with streamID
// after the number of seconds encoded in data.
// See http://xrootd.org/doc/dev45/XRdv310.pdf, p. 35 for the specification of the response.
func (sess *cliSession) handleWaitResponse(streamID xrdproto.StreamID, data []byte) error {
	var resp xrdproto.WaitResponse
	rBuffer := xrdenc.NewRBuffer(data)
	if err := resp.UnmarshalXrd(rBuffer); err != nil {
		return err
	}

	sess.mu.RLock()
	req, ok := sess.requests[streamID]
	sess.mu.RUnlock()
	if !ok {
		return fmt.Errorf("xrootd: could not find a request with stream id equal to %v", streamID)
	}

	wait := min(max(resp.Duration, 0), sess.waitCap())

	go func(req pendingRequest) {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-sess.ctx.Done():
			// The session is going away: the request is cleaned up by
			// whoever closed it, and re-issuing it would write to a
			// connection that is already down.
			return
		case <-timer.C:
		}
		if err := sess.writeRequest(req); err != nil {
			resp := mux.ServerResponse{Err: fmt.Errorf("xrootd: could not send data to the server: %w", err)}
			if err := sess.mux.SendData(streamID, resp); err != nil {
				sess.logf("xrootd: could not report the failed retry of stream %v to %q: %v", streamID, sess.sessionID, err)
			}
			sess.cleanupRequest(streamID)
		}
	}(req)

	return nil
}

func (sess *cliSession) consume() {
	var header xrdproto.ResponseHeader
	var headerBytes = make([]byte, xrdproto.ResponseHeaderLength)
	var resp mux.ServerResponse

	// The connection is settled by the time this runs: the handshake, the
	// login and any TLS upgrade all complete before the read loop is started,
	// so sess.conn is not swapped underneath it.
	conn := sess.conn
	timeout := sess.streamTimeout()
	rd := &countingReader{r: conn}

	for {
		select {
		case <-sess.ctx.Done():
			// The session is closing. Whatever is still waiting is released by
			// the close itself — failPending for the session whose connection
			// died, the mux for one the caller shut down — so reading on would
			// only be reading a socket that is about to be closed underneath.
			return
		default:
			var err error
			rd.n = 0
			if timeout > 0 {
				// Ignored deliberately: a connection that will not take a
				// deadline is one the read below is about to fail on anyway,
				// and that error is the one worth reporting.
				_ = conn.SetReadDeadline(time.Now().Add(timeout))
			}
			resp.Data, err = xrdproto.ReadResponseWithReuse(rd, headerBytes, &header)
			if err != nil {
				if sess.ctx.Err() != nil {
					// something happened to the context.
					// ignore this error.
					return
				}
				if errors.Is(err, os.ErrDeadlineExceeded) {
					if rd.n == 0 && !sess.hasPending() {
						// Silence with nothing outstanding is what a connection
						// between transfers looks like. Push the deadline out
						// and keep the connection, rather than reconnecting for
						// the next read.
						continue
					}
					// Either a request is owed an answer that never came, or a
					// response stopped part way through and the stream is now
					// out of step with the sender. Neither can be recovered on
					// this connection.
					err = fmt.Errorf("xrootd: %q sent nothing for %v: %w", sess.addr, timeout, err)
				}
				sess.handleReadError(err)
				// The read failed, so the header and the payload hold
				// whatever the previous response left in them: there is
				// nothing here to dispatch, and the session that would
				// have carried the next one is either closed or being
				// redirected.
				return
			}
			resp.Err = nil
			resp.Redirection = nil
			resp.AuthMore = false

			var keepStream bool
			switch header.Status {
			case xrdproto.Error:
				resp.Err = header.Error(resp.Data)
			case xrdproto.Wait:
				resp.Err = sess.handleWaitResponse(header.StreamID, resp.Data)
				if resp.Err == nil {
					continue
				}
			case xrdproto.WaitResp:
				// The response is deferred; the real one follows on this stream.
				// Keep the stream open and do not deliver this placeholder.
				continue
			case xrdproto.Attn:
				// An asynchronous attention response. If it wraps a delayed
				// reply (kXR_asynresp) for another stream, unwrap and dispatch
				// it there; otherwise ignore it.
				sess.handleAttn(resp.Data)
				continue
			case xrdproto.Redirect:
				resp.Redirection, resp.Err = mux.ParseRedirection(resp.Data)
			case xrdproto.AuthMore:
				resp.AuthMore = true
			case xrdproto.Status:
				resp.Data, keepStream, resp.Err = sess.readStatusTail(resp.Data)
			}

			if err := sess.mux.SendData(header.StreamID, resp); err != nil {
				// No waiter is registered for this stream ID. Because a request
				// always claims its stream (registering the waiter) before it is
				// sent, such a frame cannot be a response awaited by anyone: it
				// is unsolicited (e.g. a late duplicate or a kXR_attn). Drop it
				// rather than crashing the session.
				continue
			}

			if header.Status != xrdproto.OkSoFar && !keepStream {
				sess.cleanupRequest(header.StreamID)
			}
		}
	}
}

// readStatusTail completes a kXR_status frame: it verifies the frame's CRC,
// then drains the trailing data announced by StatusBody.DataLength — which
// lives OUTSIDE the response header's data length — off the connection.
// It returns the full frame (body+info+trailing), whether more frames follow
// (kXR_PartialResult or kXR_ProgressInfo), and any error. On error the frame
// is returned as-is so the caller can surface it.
func (sess *cliSession) readStatusTail(frame []byte) ([]byte, bool, error) {
	var body xrdproto.StatusBody
	if err := body.UnmarshalVerifyXrd(frame); err != nil {
		return frame, false, err
	}
	if body.DataLength > xrdproto.MaxResponseLength {
		return frame, false, fmt.Errorf("xrootd: kXR_status trailing data of %d bytes exceeds the %d-byte limit", body.DataLength, xrdproto.MaxResponseLength)
	}
	if body.DataLength > 0 {
		tail := make([]byte, body.DataLength)
		if _, err := io.ReadFull(sess.conn, tail); err != nil {
			return frame, false, fmt.Errorf("xrootd: could not read kXR_status trailing data: %w", err)
		}
		frame = append(frame, tail...)
	}
	partial := body.RespType == xrdproto.PartialResult || body.RespType == xrdproto.ProgressInfo
	return frame, partial, nil
}

// handleAttn processes a kXR_attn attention response. When the body carries an
// asynchronous delayed response (action code kXR_asynresp), it unwraps the
// embedded response header + data and dispatches it to the target stream, as
// if it had arrived normally. The body layout is: action(int32),
// reserved(int32), then a ServerResponseHeader followed by that many data
// bytes. Non-asynresp attentions are ignored.
func (sess *cliSession) handleAttn(body []byte) {
	const prefix = 8 // action(4) + reserved(4)
	if len(body) < prefix+xrdproto.ResponseHeaderLength {
		return
	}
	if int32(binary.BigEndian.Uint32(body[:4])) != xrdproto.AsyncResp {
		return
	}

	var hdr xrdproto.ResponseHeader
	if err := hdr.UnmarshalXrd(xrdenc.NewRBuffer(body[prefix : prefix+xrdproto.ResponseHeaderLength])); err != nil {
		return
	}
	data := body[prefix+xrdproto.ResponseHeaderLength:]
	if int(hdr.DataLength) <= len(data) {
		data = data[:hdr.DataLength]
	}

	var resp mux.ServerResponse
	switch hdr.Status {
	case xrdproto.Error:
		resp.Err = hdr.Error(data)
	case xrdproto.Redirect:
		resp.Redirection, resp.Err = mux.ParseRedirection(data)
	default:
		resp.Data = data
	}
	// Deliver to the waiting stream (ignore if none is registered).
	_ = sess.mux.SendData(hdr.StreamID, resp)
	if hdr.Status != xrdproto.OkSoFar {
		sess.cleanupRequest(hdr.StreamID)
	}
}

func (sess *cliSession) cleanupRequest(streamID xrdproto.StreamID) {
	sess.mux.Unclaim(streamID)
	sess.mu.Lock()
	delete(sess.requests, streamID)
	sess.mu.Unlock()
}

func (sess *cliSession) writeRequest(request pendingRequest) error {
	if request.PathID == 0 {
		request.Header = append(request.Header, request.Data...)
	}

	if _, err := sess.conn.Write(request.Header); err != nil {
		return err
	}

	if request.PathID != 0 && len(request.Data) > 0 {
		sess.subsMu.RLock()
		conn, ok := sess.subs[request.PathID]
		sess.subsMu.RUnlock()
		if !ok {
			return fmt.Errorf("xrootd: connection with wrong pathID = %v was requested", request.PathID)
		}
		if _, err := conn.conn.Write(request.Data); err != nil {
			return err
		}
	}
	return nil
}

// send writes a request and accumulates the frames of its response until a
// terminal one arrives.
//
// maxBytes bounds the accumulated body; 0 means unbounded. The bound matters
// because the loop below is driven by the server: a peer that answers with an
// endless stream of OkSoFar frames — each one individually small enough to
// pass the per-frame limit — otherwise grows the client's heap until the
// process dies. Requests that know how much data they asked for state it via
// xrdproto.ResponseLimiter.
func (sess *cliSession) send(ctx context.Context, streamID xrdproto.StreamID, responseChannel mux.DataRecvChan, header, body []byte, pathID xrdproto.PathID, maxBytes int64) ([]byte, *mux.Redirection, bool, error) {
	if pathID == 0 {
		header = append(header, body...)
	}
	request := pendingRequest{Header: header, Data: body, PathID: pathID}
	sess.mu.Lock()
	sess.requests[streamID] = request
	sess.mu.Unlock()

	if err := sess.writeRequest(request); err != nil {
		return nil, nil, false, err
	}

	var data []byte
	var authMore bool

	for {
		select {
		case resp, more := <-responseChannel:
			if !more {
				return data, nil, authMore, nil
			}

			if resp.Err != nil {
				return nil, resp.Redirection, false, resp.Err
			}

			if resp.Redirection != nil {
				return nil, resp.Redirection, false, nil
			}

			if resp.AuthMore {
				authMore = true
			}
			if maxBytes > 0 && int64(len(data))+int64(len(resp.Data)) > maxBytes {
				return nil, nil, false, fmt.Errorf("xrootd: response exceeds the %d-byte limit for this request", maxBytes)
			}
			data = append(data, resp.Data...)
		case <-ctx.Done():
			if err := ctx.Err(); err != nil {
				return nil, nil, false, err
			}
		}
	}
}

// authRound performs one round of a (possibly multi-round) authentication
// exchange: it sends req and returns the server's response payload along with
// whether the server asked for more (kXR_authmore). Auth requests are never
// signed and never carry a separate data socket.
func (sess *cliSession) authRound(ctx context.Context, req xrdproto.Request) (more bool, data []byte, err error) {
	streamID, responseChannel, err := sess.mux.Claim()
	if err != nil {
		return false, nil, err
	}
	var wBuffer xrdenc.WBuffer
	header := xrdproto.RequestHeader{StreamID: streamID, RequestID: req.ReqID()}
	if err = header.MarshalXrd(&wBuffer); err != nil {
		return false, nil, err
	}
	if err = req.MarshalXrd(&wBuffer); err != nil {
		return false, nil, err
	}
	data, _, more, err = sess.send(ctx, streamID, responseChannel, wBuffer.Bytes(), nil, 0, 0)
	return more, data, err
}

// Send sends the request to the server and stores the response inside the resp.
func (sess *cliSession) Send(ctx context.Context, resp xrdproto.Response, req xrdproto.Request) (*mux.Redirection, error) {
	streamID, responseChannel, err := sess.mux.Claim()
	if err != nil {
		return nil, err
	}

	var wBuffer xrdenc.WBuffer
	header := xrdproto.RequestHeader{StreamID: streamID, RequestID: req.ReqID()}
	if err = header.MarshalXrd(&wBuffer); err != nil {
		return nil, err
	}

	var pathID xrdproto.PathID = 0
	var pathData []byte
	if dr, ok := req.(xrdproto.DataRequest); ok {
		// A session configured with no data paths sends everything inline.
		// Asking for one would fail on every request and report a refusal
		// nobody made.
		if sess.maxSubs > 0 {
			var err error
			pathID, err = sess.claimPathID(ctx)
			if err != nil {
				// A data path is an optimisation, not a requirement: the
				// request still goes out, with its data on the connection the
				// header travels on. It is worth reporting, though — a server
				// refusing every kXR_bind is why a transfer that should
				// saturate the link does not.
				sess.logf("xrootd: could not open a data connection to %q, sending the data inline: %v", sess.sessionID, err)
				pathID = 0
			}
			defer sess.unclaimPathID(pathID)
		}
		// The path id is set either way: zero names the connection the request
		// travels on, and a request that carries no path id at all is a
		// shorter frame than the server expects to read.
		dr.SetPathID(pathID)
		pathData = dr.PathData()
	}

	if err = req.MarshalXrd(&wBuffer); err != nil {
		return nil, err
	}
	data := wBuffer.Bytes()

	if sess.signRequirements.Needed(req) {
		data, err = sess.sign(streamID, req.ReqID(), data)
		if err != nil {
			return nil, err
		}
	}

	var maxBytes int64
	if lim, ok := req.(xrdproto.ResponseLimiter); ok {
		maxBytes = lim.MaxResponseLength()
	}

	data, redirection, _, err := sess.send(ctx, streamID, responseChannel, data, pathData, pathID, maxBytes)
	if err != nil || redirection != nil || resp == nil {
		return redirection, sess.client.explainAuth(err)
	}

	return nil, resp.UnmarshalXrd(xrdenc.NewRBuffer(data))
}

// sendHere issues a request that asks about this connection and nothing else —
// a login, a protocol negotiation, a bind, a ping.
//
// A redirect is an answer to "where is this file"; it is no answer to "what
// version do you speak" or "are you still there". Following one would ask a
// different server a question about a connection it knows nothing about, and
// ignoring one leaves the caller with a zero response it cannot tell from a
// real one — a bind that never happened reads as path 0, the connection the
// request was meant to establish. It is reported as the failure it is.
func (sess *cliSession) sendHere(ctx context.Context, resp xrdproto.Response, req xrdproto.Request) error {
	redirection, err := sess.Send(ctx, resp, req)
	if err != nil {
		return err
	}
	if redirection != nil {
		return fmt.Errorf("xrootd: server %q answered request %d with a redirect to %q, which says nothing about this connection",
			sess.sessionID, req.ReqID(), redirection.Addr)
	}
	return nil
}

func (sess *cliSession) claimPathID(ctx context.Context) (xrdproto.PathID, error) {
	select {
	case child := <-sess.freeSubs:
		return child, nil
	default:
		sess.subCreateMu.Lock()
		defer sess.subCreateMu.Unlock()

		sess.subsMu.RLock()
		if len(sess.subs) >= sess.maxSubs {
			sess.subsMu.RUnlock()
			return 0, fmt.Errorf("xrootd: could not claimPathID: all of %d connections are taken", sess.maxSubs)
		}
		sess.subsMu.RUnlock()

		ds, err := newSubSession(ctx, sess)
		if err != nil {
			return 0, err
		}
		sess.subsMu.Lock()
		sess.subs[ds.pathID] = ds
		sess.subsMu.Unlock()

		return ds.pathID, nil
	}
}

// unclaimPathID returns a data path to the pool, where the next request that
// needs one will find it.
func (sess *cliSession) unclaimPathID(pathID xrdproto.PathID) {
	if pathID == 0 {
		return
	}
	select {
	case sess.freeSubs <- pathID:
	default:
		// Unreachable: the pool holds one slot per path this session is
		// allowed to open. Dropping the id rather than blocking is still the
		// right answer if that ever stops being true — a lost path costs one
		// connection, a blocked one costs the request.
	}
}

// logf reports a failure that has no caller to return it to. It is a no-op
// unless the client was given an error handler; see WithErrorHandler.
func (sess *cliSession) logf(format string, args ...any) {
	if sess.client == nil || sess.client.errorHandler == nil {
		return
	}
	sess.client.errorHandler(fmt.Errorf(format, args...))
}

func (sess *cliSession) sign(streamID xrdproto.StreamID, requestID uint16, data []byte) ([]byte, error) {
	if len(sess.signKey) == 0 {
		// Signing without a key would be a formality: every byte a signature
		// covers is on the wire, so an unkeyed digest of them proves only that
		// the sender could read the request they are sending. The server
		// rejects it, and it is a clearer failure to say why here than to have
		// the request come back unauthorized for no visible reason.
		return nil, fmt.Errorf(
			"xrootd: server %q requires request %d to be signed, but the session established no signing key (only gsi does)",
			sess.sessionID, requestID,
		)
	}
	seqID := atomic.AddInt64(&sess.seqID, 1)
	signRequest := sigver.NewRequest(sess.signKey, requestID, seqID, data)
	header := xrdproto.RequestHeader{StreamID: streamID, RequestID: signRequest.ReqID()}

	var wBuffer xrdenc.WBuffer
	if err := header.MarshalXrd(&wBuffer); err != nil {
		return nil, err
	}
	if err := signRequest.MarshalXrd(&wBuffer); err != nil {
		return nil, err
	}
	wBuffer.WriteBytes(data)

	return wBuffer.Bytes(), nil
}

func newSubSession(ctx context.Context, parent *cliSession) (*cliSession, error) {
	ctx, cancel := context.WithCancel(ctx)

	var d net.Dialer
	conn, err := dial(ctx, &d, parent.addr, parent.client)
	if err != nil {
		cancel()
		return nil, err
	}

	sess := &cliSession{
		ctx:       ctx,
		cancel:    cancel,
		conn:      conn,
		mux:       parent.mux,
		subs:      make(map[xrdproto.PathID]*cliSession),
		requests:  make(map[xrdproto.StreamID]pendingRequest),
		client:    parent.client,
		sessionID: parent.addr,
		addr:      parent.addr,
		isSub:     true,
	}

	// The handshake runs synchronously before consume() so the fixed stream ID
	// {0,0} it uses never reaches the shared mux (which the parent and all
	// sub-sessions use); otherwise it would collide with a regular request's
	// mux-claimed {0,0}. As on the main session, it reads the socket ahead of
	// the loop that applies the stream timeout, so the bound is applied here.
	bootCtx := ctx
	if t := sess.streamTimeout(); t > 0 {
		var cancel context.CancelFunc
		bootCtx, cancel = context.WithTimeout(ctx, t)
		defer cancel()
	}
	if err := sess.handshakeBootstrap(bootCtx); err != nil {
		sess.Close()
		return nil, err
	}

	go sess.consume()

	pathID, err := sess.bind(ctx, parent.loginID)
	if err != nil {
		sess.Close()
		return nil, err
	}

	sess.pathID = pathID
	return sess, nil
}
