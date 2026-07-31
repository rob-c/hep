// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// A strict, spec-checking XRootD server for conformance testing.
//
// Unlike the permissive mocks elsewhere in this package, this server PARSES
// what the client sends the way stock XrdXrootd does: every framing rule the
// protocol states is checked, and any breach is recorded in violations, which
// the tests assert stays empty. It also keeps a real in-memory file, so a
// write path is verified by reading back the bytes the server actually stored
// rather than by trusting an "ok" status.
//
// Decoding here is deliberately independent of the xrdproto encoders under
// test — request fields are sliced out of the raw frame by offset, and page
// units and CRCs are re-derived from hash/crc32 — so a test cannot pass by
// agreeing with the encoder's own bugs.

package xrootd

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"net"
	"sync"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

const (
	confPage       = 4096 // kXR_pgPageSZ
	confReqHdrLen  = 24   // streamid[2] reqid[2] params[16] dlen[4]
	confRespHdrLen = 8    // streamid[2] status[2] dlen[4]
	confStatusLen  = 16   // kXR_status fixed body
	confPGWCSEHdr  = 8    // cseCRC[4] dlFirst[2] dlLast[2]
)

// confHandle is the file handle the conformance server issues; a request
// carrying any other handle is a violation.
var confHandle = xrdfs.FileHandle{0, 0, 0, 7}

// confBytes returns n deterministic bytes.
func confBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((37*i + 11) % 256)
	}
	return b
}

// confContent is 10000 deterministic bytes: two full 4 KiB pages plus a short
// one, so unaligned and partial-page paths are exercised by construction.
var confContent = confBytes(10000)

var confCastagnoli = crc32.MakeTable(crc32.Castagnoli)

func confCRC(p []byte) uint32 { return crc32.Checksum(p, confCastagnoli) }

// confServer is a conformance server: one in-memory file, plus response
// shaping knobs the tests flip between operations.
type confServer struct {
	mu   sync.Mutex
	data []byte

	// violations collects every protocol breach seen from the client.
	violations []string
	// ops records the request id of each request served, in order, so tests
	// can assert on the sequence a client emitted (a kXR_sync before a
	// kXR_close, a retry after a checksum error).
	ops []uint16

	// response shaping
	readChunk     int            // >0: split read replies into OkSoFar chunks
	waitOnce      bool           // answer the next read with kXR_wait 1, then normally
	asyncRead     bool           // deliver the next read via kXR_attn/kXR_asynresp
	unsolicited   bool           // precede the next reply with a frame for no one
	overAnswer    int            // >0: return this many bytes MORE than requested
	hugeDlen      bool           // claim a body past MaxResponseLength, then hang up
	stall         bool           // accept the request and never answer
	corruptPage   bool           // flip a bit in the next pgread reply's CRC
	shortPgUnit   bool           // announce a page unit that is cut off
	badOnce       map[int64]bool // pgwrite pages reported corrupt on the first try
	badAlways     map[int64]bool // pgwrite pages reported corrupt forever
	readvDropLast bool           // answer a readv without its last segment
	readvShortSeg bool           // return one byte less than asked for in every segment
	failSync      bool
	failClose     bool
}

func newConfServer(data []byte) *confServer {
	return &confServer{
		data:      append([]byte(nil), data...),
		badOnce:   make(map[int64]bool),
		badAlways: make(map[int64]bool),
	}
}

// flag records a protocol breach. The tests fail on a non-empty list.
func (srv *confServer) flag(format string, args ...any) {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	srv.violations = append(srv.violations, fmt.Sprintf(format, args...))
}

// reset clears the shaping knobs and the recorded history, keeping the file.
func (srv *confServer) reset() {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	srv.violations = nil
	srv.ops = nil
	srv.readChunk = 0
	srv.waitOnce = false
	srv.asyncRead = false
	srv.unsolicited = false
	srv.overAnswer = 0
	srv.hugeDlen = false
	srv.stall = false
	srv.corruptPage = false
	srv.shortPgUnit = false
	clear(srv.badOnce)
	clear(srv.badAlways)
	srv.readvDropLast = false
	srv.readvShortSeg = false
	srv.failSync = false
	srv.failClose = false
}

// check fails t when the client committed a protocol breach.
func (srv *confServer) check(t *testing.T) {
	t.Helper()
	srv.mu.Lock()
	defer srv.mu.Unlock()
	for _, v := range srv.violations {
		t.Errorf("protocol violation: %s", v)
	}
}

// set applies mutate to the shaping knobs under the lock. Tests flip knobs from
// the client goroutine while serve reads them from the server one, and the
// happens-before edge between them runs through a socket, which the race
// detector cannot see.
func (srv *confServer) set(mutate func(srv *confServer)) {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	mutate(srv)
}

// breaches returns the violations recorded so far. It exists so a test can
// assert the server DOES catch a breach, which is what makes check meaningful.
func (srv *confServer) breaches() []string {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	return append([]string(nil), srv.violations...)
}

// opCount returns how many times the client sent the given request id.
func (srv *confServer) opCount(reqID uint16) int {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	var n int
	for _, id := range srv.ops {
		if id == reqID {
			n++
		}
	}
	return n
}

// opSeq returns the request ids the server saw, in order.
func (srv *confServer) opSeq() []uint16 {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	return append([]uint16(nil), srv.ops...)
}

// content returns a copy of the bytes the server currently holds.
func (srv *confServer) content() []byte {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	return append([]byte(nil), srv.data...)
}

// ---- byte helpers (independent of the xrdproto encoders under test) ----

func be32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func be64(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

func confRespHdr(sid xrdproto.StreamID, status uint16, dlen int) []byte {
	h := make([]byte, confRespHdrLen)
	copy(h[0:2], sid[:])
	binary.BigEndian.PutUint16(h[2:4], status)
	binary.BigEndian.PutUint32(h[4:8], uint32(dlen))
	return h
}

func (srv *confServer) writeOK(w io.Writer, sid xrdproto.StreamID, body []byte) error {
	_, err := w.Write(append(confRespHdr(sid, uint16(xrdproto.Ok), len(body)), body...))
	return err
}

func (srv *confServer) writeErr(w io.Writer, sid xrdproto.StreamID, code int32, msg string) error {
	// errnum[4] followed by a NUL-terminated message, as kXR_error specifies.
	body := append(append(be32(uint32(code)), msg...), 0)
	_, err := w.Write(append(confRespHdr(sid, uint16(xrdproto.Error), len(body)), body...))
	return err
}

// confPageUnits splits data into [crc32c][page] units aligned to the FILE
// offset: the first unit runs only to the next 4 KiB boundary. Re-derived
// from crc32 rather than reusing pgbuf, which is what these tests check.
func confPageUnits(data []byte, offset int64) []byte {
	var out []byte
	for pos := 0; pos < len(data); {
		n := confPage - int((offset+int64(pos))%confPage)
		if n > len(data)-pos {
			n = len(data) - pos
		}
		page := data[pos : pos+n]
		out = append(out, be32(confCRC(page))...)
		out = append(out, page...)
		pos += n
	}
	return out
}

// confStatus builds one kXR_status frame: the response header, the CRC'd
// 16-byte body, the 8-byte offset info tail, then the trailing payload.
func confStatus(sid xrdproto.StreamID, reqID uint16, respType uint8, offset int64, trailer []byte) []byte {
	body := make([]byte, confStatusLen+8)
	copy(body[4:6], sid[:])
	body[6] = uint8(reqID - 3000)
	body[7] = respType
	binary.BigEndian.PutUint32(body[12:16], uint32(len(trailer)))
	binary.BigEndian.PutUint64(body[16:24], uint64(offset))
	binary.BigEndian.PutUint32(body[0:4], confCRC(body[4:]))

	out := confRespHdr(sid, uint16(xrdproto.Status), len(body))
	out = append(out, body...)
	return append(out, trailer...)
}

// ---- request intake ----

// confRequest is one raw request: the 24-byte frame plus its payload, kept
// undecoded so handlers slice fields out by offset.
type confRequest struct {
	frame   []byte
	payload []byte
}

func (r confRequest) sid() xrdproto.StreamID {
	return xrdproto.StreamID{r.frame[0], r.frame[1]}
}
func (r confRequest) reqID() uint16 { return binary.BigEndian.Uint16(r.frame[2:4]) }
func (r confRequest) i64(at int) int64 {
	return int64(binary.BigEndian.Uint64(r.frame[at : at+8]))
}
func (r confRequest) i32(at int) int32 {
	return int32(binary.BigEndian.Uint32(r.frame[at : at+4]))
}

// take reads one request: the fixed header plus exactly dlen payload bytes.
func confTake(conn net.Conn) (confRequest, error) {
	frame := make([]byte, confReqHdrLen)
	if _, err := io.ReadFull(conn, frame); err != nil {
		return confRequest{}, err
	}
	dlen := int(int32(binary.BigEndian.Uint32(frame[20:24])))
	if dlen < 0 {
		return confRequest{}, fmt.Errorf("negative dlen %d", dlen)
	}
	var payload []byte
	if dlen > 0 {
		payload = make([]byte, dlen)
		if _, err := io.ReadFull(conn, payload); err != nil {
			return confRequest{}, err
		}
	}
	return confRequest{frame: frame, payload: payload}, nil
}

// checkHandle verifies the file handle a request carries at byte offset at.
func (srv *confServer) checkHandle(r confRequest, what string, at int) {
	var fh xrdfs.FileHandle
	copy(fh[:], r.frame[at:at+4])
	if fh != confHandle {
		srv.flag("%s: unknown fhandle %v", what, fh)
	}
}

func (srv *confServer) apply(offset int64, b []byte) {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if need := offset + int64(len(b)); int64(len(srv.data)) < need {
		srv.data = append(srv.data, make([]byte, need-int64(len(srv.data)))...)
	}
	copy(srv.data[offset:], b)
}

// slice returns the stored bytes for [offset, offset+n), clamped to the file.
func (srv *confServer) slice(offset int64, n int) []byte {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if offset < 0 || offset >= int64(len(srv.data)) || n <= 0 {
		return nil
	}
	hi := offset + int64(n)
	if hi > int64(len(srv.data)) {
		hi = int64(len(srv.data))
	}
	return append([]byte(nil), srv.data[offset:hi]...)
}

// ---- per-request handlers ----

func (srv *confServer) serveRead(conn net.Conn, r confRequest) error {
	srv.checkHandle(r, "kXR_read", 4)
	offset, rlen := r.i64(8), r.i32(16)
	if offset < 0 {
		srv.flag("kXR_read: negative offset %d", offset)
	}
	if rlen < 0 {
		srv.flag("kXR_read: negative rlen %d", rlen)
	}

	srv.mu.Lock()
	wait, huge, stall := srv.waitOnce, srv.hugeDlen, srv.stall
	async, unsol := srv.asyncRead, srv.unsolicited
	chunk, over := srv.readChunk, srv.overAnswer
	srv.waitOnce, srv.hugeDlen, srv.asyncRead, srv.unsolicited = false, false, false, false
	srv.mu.Unlock()

	switch {
	case wait:
		// kXR_wait 1: the client must sleep, then re-send the same request.
		_, err := conn.Write(append(confRespHdr(r.sid(), uint16(xrdproto.Wait), 4), be32(1)...))
		return err
	case huge:
		// Announce a body past the cap, then hang up behind the lie: a client
		// that trusts the header allocates before it ever sees the bytes.
		if _, err := conn.Write(confRespHdr(r.sid(), uint16(xrdproto.Ok), xrdproto.MaxResponseLength+1)); err != nil {
			return err
		}
		return io.EOF
	case stall:
		return nil // accept and never answer
	}

	data := srv.slice(offset, int(rlen))
	if over > 0 {
		data = append(data, make([]byte, over)...)
	}
	if unsol {
		// A frame for a stream nobody is waiting on must be dropped, not fatal.
		if _, err := conn.Write(append(confRespHdr(xrdproto.StreamID{0xff, 0xff}, uint16(xrdproto.Ok), 3), "no!"...)); err != nil {
			return err
		}
	}
	if async {
		// Deferred completion: kXR_attn wrapping kXR_asynresp for this stream.
		inner := append(be32(uint32(xrdproto.AsyncResp)), make([]byte, 4)...)
		inner = append(inner, confRespHdr(r.sid(), uint16(xrdproto.Ok), len(data))...)
		inner = append(inner, data...)
		_, err := conn.Write(append(confRespHdr(xrdproto.StreamID{0, 0}, uint16(xrdproto.Attn), len(inner)), inner...))
		return err
	}
	if chunk > 0 && len(data) > 0 {
		pos := 0
		for pos+chunk <= len(data) {
			if _, err := conn.Write(append(confRespHdr(r.sid(), uint16(xrdproto.OkSoFar), chunk), data[pos:pos+chunk]...)); err != nil {
				return err
			}
			pos += chunk
		}
		return srv.writeOK(conn, r.sid(), data[pos:])
	}
	return srv.writeOK(conn, r.sid(), data)
}

func (srv *confServer) serveWrite(conn net.Conn, r confRequest) error {
	srv.checkHandle(r, "kXR_write", 4)
	offset := r.i64(8)
	if offset < 0 {
		srv.flag("kXR_write: negative offset %d", offset)
	}
	srv.apply(offset, r.payload)
	return srv.writeOK(conn, r.sid(), nil)
}

// confVecSegs decodes the descriptor block shared by kXR_readv and kXR_writev:
// one 16-byte entry of fhandle | length | offset per segment. A payload that is
// not a whole number of entries is what a stock server answers kXR_ArgInvalid
// to, and is the failure a client that counts data inside dlen produces.
func (srv *confServer) confVecSegs(r confRequest, what string) []struct {
	Length int32
	Offset int64
} {
	if len(r.payload)%16 != 0 {
		srv.flag("%s: payload of %d bytes is not a whole number of 16-byte descriptors", what, len(r.payload))
		return nil
	}
	segs := make([]struct {
		Length int32
		Offset int64
	}, len(r.payload)/16)
	for i := range segs {
		p := r.payload[i*16:]
		var fh xrdfs.FileHandle
		copy(fh[:], p[:4])
		if fh != confHandle {
			srv.flag("%s: segment %d has unknown fhandle %v", what, i, fh)
		}
		segs[i].Length = int32(binary.BigEndian.Uint32(p[4:8]))
		segs[i].Offset = int64(binary.BigEndian.Uint64(p[8:16]))
		if segs[i].Length < 0 {
			srv.flag("%s: segment %d has negative length %d", what, i, segs[i].Length)
		}
		if segs[i].Offset < 0 {
			srv.flag("%s: segment %d has negative offset %d", what, i, segs[i].Offset)
		}
	}
	return segs
}

func (srv *confServer) serveReadV(conn net.Conn, r confRequest) error {
	// params[0:15] are reserved and params[15] is the path id; a client on one
	// connection owns none of them.
	if !bytes.Equal(r.frame[4:20], make([]byte, 16)) {
		srv.flag("kXR_readv: reserved params are not zero: % x", r.frame[4:20])
	}
	segs := srv.confVecSegs(r, "kXR_readv")
	if segs == nil {
		return srv.writeErr(conn, r.sid(), 3025, "Read vector is invalid")
	}

	srv.mu.Lock()
	drop, short, chunk := srv.readvDropLast, srv.readvShortSeg, srv.readChunk
	srv.readvDropLast, srv.readvShortSeg = false, false
	srv.mu.Unlock()

	if drop && len(segs) > 0 {
		segs = segs[:len(segs)-1]
	}

	var body []byte
	for _, seg := range segs {
		data := srv.slice(seg.Offset, int(seg.Length))
		if short && len(data) > 0 {
			data = data[:len(data)-1]
		}
		hdr := make([]byte, 16)
		copy(hdr, confHandle[:])
		binary.BigEndian.PutUint32(hdr[4:], uint32(len(data)))
		binary.BigEndian.PutUint64(hdr[8:], uint64(seg.Offset))
		body = append(append(body, hdr...), data...)
	}

	if chunk > 0 && len(body) > chunk {
		// The reply is a byte stream: an OkSoFar boundary may fall anywhere,
		// including inside a segment header.
		pos := 0
		for pos+chunk <= len(body) {
			if _, err := conn.Write(append(confRespHdr(r.sid(), uint16(xrdproto.OkSoFar), chunk), body[pos:pos+chunk]...)); err != nil {
				return err
			}
			pos += chunk
		}
		return srv.writeOK(conn, r.sid(), body[pos:])
	}
	return srv.writeOK(conn, r.sid(), body)
}

// serveWriteV reads the segment data itself: a vector write's dlen covers only
// the descriptors, so the bytes are still on the connection when the framing
// read returns.
func (srv *confServer) serveWriteV(conn net.Conn, r confRequest) error {
	if !bytes.Equal(r.frame[5:20], make([]byte, 15)) {
		srv.flag("kXR_writev: reserved params are not zero: % x", r.frame[5:20])
	}
	segs := srv.confVecSegs(r, "kXR_writev")
	if segs == nil {
		return srv.writeErr(conn, r.sid(), 3031, "Write vector is invalid")
	}
	if opts := r.frame[4]; opts&^0x01 != 0 {
		srv.flag("kXR_writev: unknown option bits %#x", opts)
	}

	// All-or-nothing: everything is read and staged before anything is stored.
	staged := make([][]byte, len(segs))
	for i, seg := range segs {
		buf := make([]byte, seg.Length)
		if _, err := io.ReadFull(conn, buf); err != nil {
			srv.flag("kXR_writev: segment %d promised %d bytes: %v", i, seg.Length, err)
			return err
		}
		staged[i] = buf
	}
	for i, seg := range segs {
		srv.apply(seg.Offset, staged[i])
	}
	return srv.writeOK(conn, r.sid(), nil)
}

func (srv *confServer) servePgRead(conn net.Conn, r confRequest) error {
	srv.checkHandle(r, "kXR_pgread", 4)
	offset, rlen := r.i64(8), r.i32(16)

	srv.mu.Lock()
	short, corrupt := srv.shortPgUnit, srv.corruptPage
	srv.shortPgUnit, srv.corruptPage = false, false
	srv.mu.Unlock()

	if short {
		// A page unit cut off mid-CRC: the client must refuse, not guess.
		_, err := conn.Write(confStatus(r.sid(), 3030, xrdproto.FinalResult, offset, []byte{0x00, 0x00}))
		return err
	}

	data := srv.slice(offset, int(rlen))
	if len(data) == 0 {
		_, err := conn.Write(confStatus(r.sid(), 3030, xrdproto.FinalResult, offset, nil))
		return err
	}

	// One status frame per page unit; the last one Final.
	type pgFrame struct {
		off   int64
		units []byte
	}
	var frames []pgFrame
	for pos, pgoff := 0, offset; pos < len(data); {
		n := confPage - int(pgoff%confPage)
		if n > len(data)-pos {
			n = len(data) - pos
		}
		frames = append(frames, pgFrame{off: pgoff, units: confPageUnits(data[pos:pos+n], pgoff)})
		pos += n
		pgoff += int64(n)
	}
	if corrupt {
		frames[0].units[0] ^= 0xff // a bit-flip in the first page's CRC
	}
	for i, fr := range frames {
		respType := uint8(xrdproto.PartialResult)
		if i == len(frames)-1 {
			respType = xrdproto.FinalResult
		}
		if _, err := conn.Write(confStatus(r.sid(), 3030, respType, fr.off, fr.units)); err != nil {
			return err
		}
	}
	return nil
}

// pgWritePages validates a kXR_pgwrite payload as page units aligned to the
// request's file offset, verifying every CRC-32C, and stores the pages. It
// returns the file offsets the payload covered, or ok=false when the framing
// or a checksum is wrong.
func (srv *confServer) pgWritePages(offset int64, payload []byte) (offsets []int64, ok bool) {
	for pos, pgoff := 0, offset; pos < len(payload); {
		if len(payload)-pos < 4 {
			srv.flag("kXR_pgwrite: page unit at %d has no CRC", pgoff)
			return nil, false
		}
		n := confPage - int(pgoff%confPage)
		if n > len(payload)-pos-4 {
			n = len(payload) - pos - 4
		}
		page := payload[pos+4 : pos+4+n]
		if got, want := binary.BigEndian.Uint32(payload[pos:pos+4]), confCRC(page); got != want {
			srv.flag("kXR_pgwrite: CRC-32C mismatch on the page at %d: got=%#08x want=%#08x", pgoff, got, want)
			return nil, false
		}
		srv.apply(pgoff, page)
		offsets = append(offsets, pgoff)
		pos += 4 + n
		pgoff += int64(n)
	}
	return offsets, true
}

func (srv *confServer) servePgWrite(conn net.Conn, r confRequest) error {
	srv.checkHandle(r, "kXR_pgwrite", 4)
	offset := r.i64(8)
	retry := r.frame[17]&0x01 != 0 // reqflags: kXR_pgRetry

	offsets, ok := srv.pgWritePages(offset, r.payload)
	if !ok {
		return srv.writeErr(conn, r.sid(), 3000, "bad page payload")
	}
	if retry {
		if len(offsets) > 1 {
			srv.flag("kXR_pgRetry resent %d pages; a retry covers one page", len(offsets))
		}
		if offset%confPage != 0 {
			srv.flag("kXR_pgRetry offset %d is not page aligned", offset)
		}
	}

	srv.mu.Lock()
	var bad []int64
	for _, off := range offsets {
		switch {
		case srv.badAlways[off]:
			bad = append(bad, off)
		case srv.badOnce[off]:
			delete(srv.badOnce, off)
			bad = append(bad, off)
		}
	}
	srv.mu.Unlock()

	var trailer []byte
	if len(bad) > 0 {
		trailer = make([]byte, confPGWCSEHdr)
		for _, off := range bad {
			trailer = append(trailer, be64(uint64(off))...)
		}
	}
	_, err := conn.Write(confStatus(r.sid(), 3026, xrdproto.FinalResult, offset, trailer))
	return err
}

// serve runs the request loop until the client hangs up.
func (srv *confServer) serve(conn net.Conn) {
	for {
		r, err := confTake(conn)
		if err != nil {
			return
		}
		// Note: a stream id of 0 is NOT flagged. The id is opaque and
		// client-chosen, and this mux allocates from 0; an attn frame carries
		// its own outer id but is dispatched on the id it wraps, so there is
		// no collision. (The Julia suite starts its ids at 1 and flags 0 —
		// a house rule, not a protocol one.)
		srv.mu.Lock()
		srv.ops = append(srv.ops, r.reqID())
		srv.mu.Unlock()

		switch id := r.reqID(); id {
		case 3013: // kXR_read
			err = srv.serveRead(conn, r)
		case 3019: // kXR_write
			err = srv.serveWrite(conn, r)
		case 3030: // kXR_pgread
			err = srv.servePgRead(conn, r)
		case 3026: // kXR_pgwrite
			err = srv.servePgWrite(conn, r)
		case 3025: // kXR_readv
			err = srv.serveReadV(conn, r)
		case 3031: // kXR_writev
			err = srv.serveWriteV(conn, r)
		case 3016: // kXR_sync
			srv.checkHandle(r, "kXR_sync", 4)
			srv.mu.Lock()
			fail := srv.failSync
			srv.mu.Unlock()
			if fail {
				err = srv.writeErr(conn, r.sid(), 3016, "sync failed")
			} else {
				err = srv.writeOK(conn, r.sid(), nil)
			}
		case 3003: // kXR_close
			srv.checkHandle(r, "kXR_close", 4)
			srv.mu.Lock()
			fail := srv.failClose
			srv.mu.Unlock()
			if fail {
				err = srv.writeErr(conn, r.sid(), 3003, "close failed")
			} else {
				err = srv.writeOK(conn, r.sid(), nil)
			}
		case 3028: // kXR_truncate
			srv.checkHandle(r, "kXR_truncate", 4)
			size := r.i64(8)
			if size < 0 {
				srv.flag("kXR_truncate: negative size %d", size)
			}
			srv.mu.Lock()
			if size < int64(len(srv.data)) {
				srv.data = srv.data[:size]
			}
			srv.mu.Unlock()
			err = srv.writeOK(conn, r.sid(), nil)
		default:
			srv.flag("unexpected request id %d", id)
			err = srv.writeErr(conn, r.sid(), 3000, "unsupported")
		}
		if err != nil {
			return
		}
	}
}

// confClient runs fn against a client wired to a conformance server holding
// data. The returned server is safe to inspect after fn returns.
func confClient(t *testing.T, data []byte, fn func(srv *confServer, f *file)) *confServer {
	t.Helper()

	srv := newConfServer(data)
	var got *confServer
	testClientWithMockServer(
		func(cancel func(), conn net.Conn) { srv.serve(conn) },
		func(cancel func(), client *Client) {
			f := &file{
				fs:        client.FS().(*fileSystem),
				handle:    confHandle,
				sessionID: client.initialSessionID,
			}
			fn(srv, f)
			got = srv
		},
	)
	return got
}
