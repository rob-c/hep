// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// A strict, spec-checking XRootD server for the *namespace* surface: the
// requests that name a path rather than move file data (dirlist, open, stat,
// statx, mkdir, mv, chmod, rm, rmdir, truncate, fattr, query, ping).
//
// The rules are the ones conformance_server_test.go works to: request fields
// are sliced out of the raw frame by offset rather than handed to the decoders
// under test, replies are hand-encoded byte by byte rather than produced by the
// marshallers the client will use to read them, and every framing breach is
// recorded rather than answered. A test that passes here cannot be passing
// because an encoder and its decoder share a bug.
//
// The server keeps a real in-memory namespace, so an operation is judged by
// what the server ends up holding, not by the status code it chose to send.

package xrootd

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

// XRootD request ids for the namespace surface, spelled out so the dispatch in
// serveFS reads against the specification rather than against the constants of
// the packages under test.
const (
	confQuery    = 3001
	confChmod    = 3002
	confClose    = 3003
	confDirlist  = 3004
	confMkdir    = 3008
	confMv       = 3009
	confOpen     = 3010
	confPing     = 3011
	confRm       = 3014
	confRmdir    = 3015
	confStat     = 3017
	confFattr    = 3020
	confStatx    = 3022
	confTruncate = 3028
)

// kXR_open option bits, as the client is expected to put them on the wire.
const (
	confOpenCompress = 1 << iota
	confOpenDelete
	confOpenForce
	confOpenNew
	confOpenRead
	confOpenUpdate
	confOpenAsync
	confOpenRefresh
	confOpenMkPath
	confOpenAppend
	confOpenRetStat
	confOpenReplica
	confOpenPOSC
	confOpenNoWait
	confOpenSeqIO

	confOpenKnown = 2*confOpenSeqIO - 1
)

// kXR_fattr subcodes and options, and the query codes this server answers.
const (
	confFattrDel   = 0
	confFattrGet   = 1
	confFattrList  = 2
	confFattrSet   = 3
	confFattrData  = 0x10 // kXR_faData: list should also return values
	confFattrMax   = 16   // kXR_faMaxVars
	confQueryCksum = 3    // kXR_Qcksum
	confQueryCfg   = 7    // kXR_Qconfig
)

// confNode is one namespace entry.
type confNode struct {
	data  []byte
	dir   bool
	mode  uint16
	xattr map[string][]byte
}

// confFS is a conformance server for the namespace surface.
type confFS struct {
	mu    sync.Mutex
	nodes map[string]*confNode

	violations []string
	ops        []uint16

	// paths and opaque record, in order, the namespace name and the CGI of
	// every path field that arrived. They are what the URL/token tests read:
	// the server addresses its namespace by the name alone, so a client that
	// leaves the CGI on the path or invents one shows up here rather than in
	// a status code.
	paths  []string
	opaque []string

	// open file handles, and the next one to hand out.
	handles map[xrdfs.FileHandle]string
	nextFH  byte

	// Response shaping, keyed by request id wherever a per-operation knob
	// would otherwise multiply.
	failNext map[uint16]string // answer the next request of this id with kXR_error
	waitNext map[uint16]bool   // answer the next request of this id with kXR_wait 1
	cutNext  map[uint16]int    // cut this many bytes off the next reply's body
	bodyNext map[uint16][]byte // replace the next reply's body with this one
	noStat   bool              // answer dirlist without stat info, as older servers do
	junk     bool              // precede the next reply with a frame for no one
}

func newConfFS() *confFS {
	return &confFS{
		nodes:    map[string]*confNode{"/": {dir: true, mode: 0o755}},
		handles:  map[xrdfs.FileHandle]string{},
		nextFH:   1,
		failNext: map[uint16]string{},
		waitNext: map[uint16]bool{},
		cutNext:  map[uint16]int{},
		bodyNext: map[uint16][]byte{},
	}
}

func (fs *confFS) flag(format string, args ...any) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.violations = append(fs.violations, fmt.Sprintf(format, args...))
}

// set applies mutate under the lock. The lock is not optional even where the
// test looks single-threaded: the happens-before edge between the test and the
// server goroutine runs through a socket, which the race detector cannot see.
func (fs *confFS) set(mutate func(fs *confFS)) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	mutate(fs)
}

// check fails t when the client committed a protocol breach.
func (fs *confFS) check(t *testing.T) {
	t.Helper()
	for _, v := range fs.breaches() {
		t.Errorf("protocol violation: %s", v)
	}
}

func (fs *confFS) breaches() []string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return append([]string(nil), fs.violations...)
}

func (fs *confFS) opSeq() []uint16 {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return append([]uint16(nil), fs.ops...)
}

func (fs *confFS) opCount(reqID uint16) int {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	var n int
	for _, id := range fs.ops {
		if id == reqID {
			n++
		}
	}
	return n
}

// ---- namespace helpers ----

func (fs *confFS) mkfile(name string, data []byte) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.nodes[name] = &confNode{data: data, mode: 0o644}
}

func (fs *confFS) mkdirAs(name string, mode uint16) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.nodes[name] = &confNode{dir: true, mode: mode}
}

func (fs *confFS) node(name string) *confNode {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.nodes[name]
}

func (fs *confFS) names() []string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	out := make([]string, 0, len(fs.nodes))
	for name := range fs.nodes {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// modeOf, sizeOf and xattrOf are the test-side view of the namespace. They
// take the lock for the same reason set does.
func (fs *confFS) modeOf(name string) uint16 {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if n := fs.nodes[name]; n != nil {
		return n.mode
	}
	return 0
}

func (fs *confFS) sizeOf(name string) int {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if n := fs.nodes[name]; n != nil {
		return len(n.data)
	}
	return -1
}

func (fs *confFS) xattrOf(name, attr string) ([]byte, bool) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	n := fs.nodes[name]
	if n == nil {
		return nil, false
	}
	v, ok := n.xattr[attr]
	return v, ok
}

// children returns the immediate members of dir, sorted.
func (fs *confFS) children(dir string) []string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	var out []string
	for name := range fs.nodes {
		if name != dir && path.Dir(name) == dir {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// ---- reply helpers (hand-encoded, independent of the marshallers) ----

func confOK(w io.Writer, sid xrdproto.StreamID, body []byte) error {
	_, err := w.Write(append(confRespHdr(sid, uint16(xrdproto.Ok), len(body)), body...))
	return err
}

func confErr(w io.Writer, sid xrdproto.StreamID, code int32, msg string) error {
	body := append(append(be32(uint32(code)), msg...), 0)
	_, err := w.Write(append(confRespHdr(sid, uint16(xrdproto.Error), len(body)), body...))
	return err
}

// confMtime is the modification time every entry reports, fixed so that tests
// can assert on it.
const confMtime = 1700000000

// confStatLine encodes a stat body the way the protocol states it: four
// decimal numbers separated by single spaces.
func confStatLine(id, size int64, flags xrdfs.StatFlags, mtime int64) string {
	return strconv.FormatInt(id, 10) + " " + strconv.FormatInt(size, 10) + " " +
		strconv.Itoa(int(flags)) + " " + strconv.FormatInt(mtime, 10)
}

func (fs *confFS) statOf(name string) (string, bool) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	n := fs.nodes[name]
	if n == nil {
		return "", false
	}
	flags := xrdfs.StatIsReadable | xrdfs.StatIsWritable
	if n.dir {
		flags |= xrdfs.StatIsDir | xrdfs.StatIsExecutable
	}
	return confStatLine(int64(len(name)), int64(len(n.data)), flags, confMtime), true
}

// ---- request field accessors ----

func (r confRequest) u16(at int) uint16 { return binary.BigEndian.Uint16(r.frame[at : at+2]) }
func (r confRequest) u8(at int) uint8   { return r.frame[at] }

func (r confRequest) fhandle(at int) xrdfs.FileHandle {
	var fh xrdfs.FileHandle
	copy(fh[:], r.frame[at:at+4])
	return fh
}

// zeroed flags a reserved area that the specification requires to be unused
// but that carries data. A stock server ignores it, so a client writing there
// is relying on a field it does not own.
func (fs *confFS) zeroed(r confRequest, what string, lo, hi int) {
	for _, b := range r.frame[lo:hi] {
		if b != 0 {
			fs.flag("%s: reserved bytes [%d:%d] are not zero: % x", what, lo, hi, r.frame[lo:hi])
			return
		}
	}
}

// wantPath splits the CGI off a path field, records both halves, and returns
// the namespace name. Everything past the first "?" is opaque data the server
// hands to its authorization layer unparsed — it is how a token reaches an
// endpoint — and it is not part of the name being addressed. A path that is
// empty or not absolute is flagged.
func (fs *confFS) wantPath(what, p string) string {
	name, opaque, _ := strings.Cut(p, "?")
	switch {
	case name == "":
		fs.flag("%s: empty path", what)
	case !strings.HasPrefix(name, "/"):
		fs.flag("%s: path %q is not absolute", what, name)
	}

	fs.mu.Lock()
	fs.paths = append(fs.paths, name)
	fs.opaque = append(fs.opaque, opaque)
	fs.mu.Unlock()

	return name
}

// pathSeq returns the namespace names the client addressed, in order.
func (fs *confFS) pathSeq() []string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return append([]string(nil), fs.paths...)
}

// opaqueSeq returns the CGI that arrived with each of those names, in the same
// order. A name that carried none contributes an empty string rather than being
// left out, so the two sequences stay aligned.
func (fs *confFS) opaqueSeq() []string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return append([]string(nil), fs.opaque...)
}

// forget drops the recorded path history, so a test can assert on one operation
// at a time without counting the ones that came before it.
func (fs *confFS) forget() {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.paths = nil
	fs.opaque = nil
}

// shape applies the per-request-id shaping knobs. It reports whether the
// request has already been answered.
func (fs *confFS) shape(conn net.Conn, r confRequest) (done bool, err error) {
	id := r.reqID()

	fs.mu.Lock()
	msg, fail := fs.failNext[id]
	wait := fs.waitNext[id]
	junk := fs.junk
	delete(fs.failNext, id)
	delete(fs.waitNext, id)
	fs.junk = false
	fs.mu.Unlock()

	if junk {
		// A reply for a stream nobody is waiting on must be dropped, not fatal.
		if _, err := conn.Write(append(confRespHdr(xrdproto.StreamID{0xff, 0xff}, uint16(xrdproto.Ok), 3), "no!"...)); err != nil {
			return true, err
		}
	}
	switch {
	case fail:
		return true, confErr(conn, r.sid(), 3011, msg)
	case wait:
		// kXR_wait 1: the client must sleep, then re-send the same request.
		_, err := conn.Write(append(confRespHdr(r.sid(), uint16(xrdproto.Wait), 4), be32(1)...))
		return true, err
	}
	return false, nil
}

// reply sends body, honouring two knobs: a replacement body, which stands for
// a server whose answer is well-framed but malformed, and a short write, where
// the header keeps announcing the full length so that a client decoding what
// arrived rather than what was promised reads a mangled body as complete.
func (fs *confFS) reply(conn net.Conn, r confRequest, body []byte) error {
	fs.mu.Lock()
	cut := fs.cutNext[r.reqID()]
	delete(fs.cutNext, r.reqID())
	if raw, ok := fs.bodyNext[r.reqID()]; ok {
		body = raw
		delete(fs.bodyNext, r.reqID())
	}
	fs.mu.Unlock()

	if cut <= 0 {
		return confOK(conn, r.sid(), body)
	}
	if cut > len(body) {
		cut = len(body)
	}
	if _, err := conn.Write(confRespHdr(r.sid(), uint16(xrdproto.Ok), len(body))); err != nil {
		return err
	}
	if _, err := conn.Write(body[:len(body)-cut]); err != nil {
		return err
	}
	return io.EOF // hang up behind the lie
}

// ---- per-request handlers ----

func (fs *confFS) serveDirlist(conn net.Conn, r confRequest) error {
	fs.zeroed(r, "kXR_dirlist", 4, 19)
	opts := r.u8(19)
	dir := fs.wantPath("kXR_dirlist", string(r.payload))

	switch n := fs.node(dir); {
	case n == nil:
		return confErr(conn, r.sid(), 3011, "no such directory")
	case !n.dir:
		return confErr(conn, r.sid(), 3016, "not a directory")
	}

	kids := fs.children(dir)

	fs.mu.Lock()
	withStat := opts&0x02 != 0 && !fs.noStat
	fs.mu.Unlock()

	if !withStat {
		// A server that does not support stat info answers with bare names,
		// whatever the client asked for.
		names := make([]string, len(kids))
		for i, k := range kids {
			names[i] = path.Base(k)
		}
		return fs.reply(conn, r, append([]byte(strings.Join(names, "\n")), 0))
	}

	// With stat info the reply opens with the "." marker line, then alternates
	// name and stat lines.
	lines := []string{".", "0 0 0 0"}
	for _, k := range kids {
		st, _ := fs.statOf(k)
		lines = append(lines, path.Base(k), st)
	}
	return fs.reply(conn, r, append([]byte(strings.Join(lines, "\n")), 0))
}

func (fs *confFS) serveOpen(conn net.Conn, r confRequest) error {
	mode, opts := r.u16(4), r.u16(6)
	fs.zeroed(r, "kXR_open", 8, 20)
	name := fs.wantPath("kXR_open", string(r.payload))

	if mode&^0o7777 != 0 {
		fs.flag("kXR_open: mode %#o has bits outside the permission mask", mode)
	}
	if opts&^confOpenKnown != 0 {
		fs.flag("kXR_open: unknown option bits %#04x", opts&^confOpenKnown)
	}

	write := opts&(confOpenDelete|confOpenNew|confOpenUpdate|confOpenAppend) != 0
	switch n := fs.node(name); {
	case n != nil && opts&confOpenNew != 0:
		return confErr(conn, r.sid(), 3018, "already exists")
	case n == nil && !write:
		return confErr(conn, r.sid(), 3011, "no such file")
	case n == nil, opts&confOpenDelete != 0:
		if parent := path.Dir(name); fs.node(parent) == nil && opts&confOpenMkPath == 0 {
			return confErr(conn, r.sid(), 3011, "no such parent directory")
		}
		fs.mkfile(name, nil)
	}

	fs.mu.Lock()
	fh := xrdfs.FileHandle{0, 0, 0, fs.nextFH}
	fs.nextFH++
	fs.handles[fh] = name
	fs.mu.Unlock()

	body := append([]byte(nil), fh[:]...)
	if opts&confOpenRetStat != 0 {
		// The stat info follows the compression descriptor, never on its own:
		// a client that skips the descriptor reads the file size out of the
		// compression page size.
		st, _ := fs.statOf(name)
		body = append(body, be32(0)...) // compression page size
		body = append(body, "    "...)  // compression type, 4 bytes
		body = append(body, st...)
	}
	return fs.reply(conn, r, body)
}

func (fs *confFS) serveStat(conn net.Conn, r confRequest) error {
	opts := r.u8(4)
	fs.zeroed(r, "kXR_stat", 5, 16)
	fh := r.fhandle(16)
	name := string(r.payload)

	if opts&^0x01 != 0 {
		fs.flag("kXR_stat: unknown option bits %#02x", opts&^0x01)
	}
	if opts&0x01 != 0 { // kXR_vfs
		if name == "" {
			fs.flag("kXR_stat: virtual-fs query with no path prefix")
		} else {
			fs.wantPath("kXR_stat(vfs)", name)
		}
		return fs.reply(conn, r, []byte("2 1024 50 1 2048 25"))
	}

	switch {
	case name != "":
		if fh != (xrdfs.FileHandle{}) {
			fs.flag("kXR_stat: both a path %q and handle %v were sent", name, fh)
		}
		name = fs.wantPath("kXR_stat", name)
	default:
		fs.mu.Lock()
		target, ok := fs.handles[fh]
		fs.mu.Unlock()
		if !ok {
			fs.flag("kXR_stat: unknown file handle %v", fh)
			return confErr(conn, r.sid(), 3011, "bad handle")
		}
		name = target
	}

	st, ok := fs.statOf(name)
	if !ok {
		return confErr(conn, r.sid(), 3011, "no such file")
	}
	return fs.reply(conn, r, []byte(st))
}

func (fs *confFS) serveStatx(conn net.Conn, r confRequest) error {
	fs.zeroed(r, "kXR_statx", 4, 20)
	raw := string(r.payload)
	if raw == "" {
		fs.flag("kXR_statx: no paths")
	}
	paths := strings.Split(raw, "\n")

	// One flag byte per path, in the order asked: the reply carries no names,
	// so position is the only thing tying an answer to its question.
	body := make([]byte, len(paths))
	for i, p := range paths {
		p = fs.wantPath("kXR_statx", p)
		switch n := fs.node(p); {
		case n == nil:
			body[i] = byte(xrdfs.StatIsOffline)
		case n.dir:
			body[i] = byte(xrdfs.StatIsDir | xrdfs.StatIsExecutable)
		default:
			body[i] = byte(xrdfs.StatIsFile)
		}
	}
	return fs.reply(conn, r, body)
}

func (fs *confFS) serveMkdir(conn net.Conn, r confRequest) error {
	opts := r.u8(4)
	fs.zeroed(r, "kXR_mkdir", 5, 18)
	mode := r.u16(18)
	name := fs.wantPath("kXR_mkdir", string(r.payload))

	if opts&^0x01 != 0 {
		fs.flag("kXR_mkdir: unknown option bits %#02x", opts&^0x01)
	}
	if mode&^0o7777 != 0 {
		fs.flag("kXR_mkdir: mode %#o has bits outside the permission mask", mode)
	}
	if fs.node(name) != nil {
		return confErr(conn, r.sid(), 3018, "already exists")
	}
	if fs.node(path.Dir(name)) == nil {
		// kXR_mkpath is what makes the missing parents legal; without it the
		// server must refuse rather than guess.
		if opts&0x01 == 0 {
			return confErr(conn, r.sid(), 3011, "no such parent directory")
		}
		for _, p := range confAncestors(name) {
			if fs.node(p) == nil {
				fs.mkdirAs(p, mode)
			}
		}
	}
	fs.mkdirAs(name, mode)
	return fs.reply(conn, r, nil)
}

// confAncestors lists the proper ancestors of name, shallowest first.
func confAncestors(name string) []string {
	var (
		out   []string
		cur   string
		clean = path.Clean(name)
	)
	for _, part := range strings.Split(strings.Trim(clean, "/"), "/") {
		if part == "" {
			continue
		}
		cur += "/" + part
		if cur == clean {
			break
		}
		out = append(out, cur)
	}
	return out
}

func (fs *confFS) serveMv(conn net.Conn, r confRequest) error {
	fs.zeroed(r, "kXR_mv", 4, 18)
	oldLen := int(r.u16(18))
	raw := string(r.payload)

	// The two paths share one blob and only the announced length says where
	// the first one ends, so a client that miscounts renames something else.
	if oldLen <= 0 || oldLen >= len(raw) {
		fs.flag("kXR_mv: old-path length %d does not fit a %d-byte body", oldLen, len(raw))
		return confErr(conn, r.sid(), 3011, "bad mv body")
	}
	oldPath, sep, newPath := raw[:oldLen], raw[oldLen], raw[oldLen+1:]
	if sep != ' ' {
		fs.flag("kXR_mv: the paths are separated by %q, want a space", sep)
	}
	oldPath = fs.wantPath("kXR_mv", oldPath)
	newPath = fs.wantPath("kXR_mv", newPath)

	n := fs.node(oldPath)
	if n == nil {
		return confErr(conn, r.sid(), 3011, "no such file")
	}
	fs.set(func(fs *confFS) {
		delete(fs.nodes, oldPath)
		fs.nodes[newPath] = n
	})
	return fs.reply(conn, r, nil)
}

func (fs *confFS) serveChmod(conn net.Conn, r confRequest) error {
	fs.zeroed(r, "kXR_chmod", 4, 18)
	mode := r.u16(18)
	name := fs.wantPath("kXR_chmod", string(r.payload))

	if mode&^0o7777 != 0 {
		fs.flag("kXR_chmod: mode %#o has bits outside the permission mask", mode)
	}
	n := fs.node(name)
	if n == nil {
		return confErr(conn, r.sid(), 3011, "no such file")
	}
	fs.set(func(*confFS) { n.mode = mode })
	return fs.reply(conn, r, nil)
}

func (fs *confFS) serveRm(conn net.Conn, r confRequest, wantDir bool) error {
	what := "kXR_rm"
	if wantDir {
		what = "kXR_rmdir"
	}
	fs.zeroed(r, what, 4, 20)
	name := fs.wantPath(what, string(r.payload))

	switch n := fs.node(name); {
	case n == nil:
		return confErr(conn, r.sid(), 3011, "no such file")
	case n.dir != wantDir:
		return confErr(conn, r.sid(), 3016, "wrong object type")
	case wantDir && len(fs.children(name)) != 0:
		return confErr(conn, r.sid(), 3016, "directory not empty")
	}
	fs.set(func(fs *confFS) { delete(fs.nodes, name) })
	return fs.reply(conn, r, nil)
}

func (fs *confFS) serveTruncate(conn net.Conn, r confRequest) error {
	fh := r.fhandle(4)
	size := r.i64(8)
	fs.zeroed(r, "kXR_truncate", 16, 20)
	name := string(r.payload)

	if size < 0 {
		fs.flag("kXR_truncate: negative size %d", size)
	}
	// The request names its target by handle or by path, never by both: a
	// server has no way to decide which of the two to believe.
	switch {
	case name != "" && fh != (xrdfs.FileHandle{}):
		fs.flag("kXR_truncate: both a path %q and handle %v were sent", name, fh)
		name = fs.wantPath("kXR_truncate", name)
	case name == "" && fh == (xrdfs.FileHandle{}):
		fs.flag("kXR_truncate: neither a path nor a handle was sent")
		return confErr(conn, r.sid(), 3011, "no target")
	case name == "":
		fs.mu.Lock()
		target, ok := fs.handles[fh]
		fs.mu.Unlock()
		if !ok {
			fs.flag("kXR_truncate: unknown file handle %v", fh)
			return confErr(conn, r.sid(), 3011, "bad handle")
		}
		name = target
	default:
		name = fs.wantPath("kXR_truncate", name)
	}

	n := fs.node(name)
	if n == nil {
		return confErr(conn, r.sid(), 3011, "no such file")
	}
	fs.set(func(*confFS) {
		switch {
		case size <= int64(len(n.data)):
			n.data = n.data[:size]
		default:
			n.data = append(n.data, make([]byte, size-int64(len(n.data)))...)
		}
	})
	return fs.reply(conn, r, nil)
}

func (fs *confFS) serveQuery(conn net.Conn, r confRequest) error {
	code := r.u16(4)
	fs.zeroed(r, "kXR_query", 6, 8)
	fs.zeroed(r, "kXR_query", 12, 20)
	args := string(r.payload)

	switch code {
	case confQueryCksum:
		if fh := r.fhandle(8); fh != (xrdfs.FileHandle{}) {
			fs.flag("kXR_query(cksum): a path-based query carries handle %v", fh)
		}
		name := fs.wantPath("kXR_query(cksum)", args)
		n := fs.node(name)
		if n == nil {
			return confErr(conn, r.sid(), 3011, "no such file")
		}
		fs.mu.Lock()
		sum := confAdler(n.data)
		fs.mu.Unlock()
		// "<algorithm> <hex value>", NUL-terminated, as XrdXrootd answers.
		return fs.reply(conn, r, append([]byte(fmt.Sprintf("adler32 %08x", sum)), 0))
	case confQueryCfg:
		return fs.reply(conn, r, []byte("1.2.3\n"))
	default:
		fs.flag("kXR_query: unexpected query code %d", code)
		return confErr(conn, r.sid(), 3011, "unsupported query")
	}
}

// confAdler is an independent Adler-32, so the checksum in the reply is not
// produced by the code that would be used to check it.
func confAdler(p []byte) uint32 {
	var a, b uint32 = 1, 0
	for _, c := range p {
		a = (a + uint32(c)) % 65521
		b = (b + a) % 65521
	}
	return b<<16 | a
}

func (fs *confFS) serveFattr(conn net.Conn, r confRequest) error {
	fh := r.fhandle(4)
	subcode, numAttr, opts := r.u8(8), r.u8(9), r.u8(10)
	fs.zeroed(r, "kXR_fattr", 11, 20)

	if numAttr > confFattrMax {
		fs.flag("kXR_fattr: %d attributes exceeds the maximum of %d", numAttr, confFattrMax)
	}
	if fh != (xrdfs.FileHandle{}) {
		fs.flag("kXR_fattr: a path-based request carries handle %v", fh)
	}

	// A path-based body opens with "path\0"; the attribute vectors follow it.
	body := r.payload
	i := confIndexZero(body)
	if i < 0 {
		fs.flag("kXR_fattr: the body has no NUL-terminated path")
		return confErr(conn, r.sid(), 3011, "bad fattr body")
	}
	name := fs.wantPath("kXR_fattr", string(body[:i]))
	rest := body[i+1:]

	n := fs.node(name)
	if n == nil {
		return confErr(conn, r.sid(), 3011, "no such file")
	}

	switch subcode {
	case confFattrList:
		if numAttr != 0 {
			fs.flag("kXR_fattr list: numattr is %d, want 0", numAttr)
		}
		if len(rest) != 0 {
			fs.flag("kXR_fattr list: %d bytes of attribute vector on a list request", len(rest))
		}
		if opts&confFattrData != 0 {
			fs.flag("kXR_fattr list: attribute values requested, which this server does not serve")
		}
		fs.mu.Lock()
		names := make([]string, 0, len(n.xattr))
		for k := range n.xattr {
			names = append(names, k)
		}
		fs.mu.Unlock()
		sort.Strings(names)
		if len(names) == 0 {
			return fs.reply(conn, r, nil)
		}
		return fs.reply(conn, r, append([]byte(strings.Join(names, "\x00")), 0))

	case confFattrGet, confFattrDel, confFattrSet:
		if numAttr != 1 {
			fs.flag("kXR_fattr: numattr is %d, want 1", numAttr)
		}
		// An nvec entry is [rc u16][name\0]; the rc is the server's to fill in.
		if len(rest) < 3 {
			fs.flag("kXR_fattr: the attribute vector is %d bytes", len(rest))
			return confErr(conn, r.sid(), 3011, "bad fattr body")
		}
		if rc := binary.BigEndian.Uint16(rest[:2]); rc != 0 {
			fs.flag("kXR_fattr: the request's nvec rc is %d, want 0", rc)
		}
		rest = rest[2:]
		j := confIndexZero(rest)
		if j < 0 {
			fs.flag("kXR_fattr: unterminated attribute name")
			return confErr(conn, r.sid(), 3011, "bad fattr body")
		}
		attr := string(rest[:j])
		rest = rest[j+1:]

		switch subcode {
		case confFattrGet:
			if len(rest) != 0 {
				fs.flag("kXR_fattr get: %d trailing bytes after the name vector", len(rest))
			}
			fs.mu.Lock()
			val, ok := n.xattr[attr]
			fs.mu.Unlock()
			if !ok {
				// A missing attribute is reported per attribute, not as a
				// request-level error.
				return fs.reply(conn, r, confFattrReply(attr, 2, nil))
			}
			return fs.reply(conn, r, confFattrReply(attr, 0, val))
		case confFattrDel:
			if len(rest) != 0 {
				fs.flag("kXR_fattr del: %d trailing bytes after the name vector", len(rest))
			}
			fs.set(func(*confFS) { delete(n.xattr, attr) })
			return fs.reply(conn, r, confFattrReply(attr, 0, nil))
		default: // confFattrSet
			// A vvec entry is [vlen i32][value].
			if len(rest) < 4 {
				fs.flag("kXR_fattr set: no value vector")
				return confErr(conn, r.sid(), 3011, "bad fattr body")
			}
			vlen := int(int32(binary.BigEndian.Uint32(rest[:4])))
			rest = rest[4:]
			if vlen < 0 || vlen != len(rest) {
				fs.flag("kXR_fattr set: value length %d does not match the %d bytes that followed", vlen, len(rest))
				return confErr(conn, r.sid(), 3011, "bad fattr body")
			}
			val := append([]byte(nil), rest...)
			fs.set(func(*confFS) {
				if n.xattr == nil {
					n.xattr = map[string][]byte{}
				}
				n.xattr[attr] = val
			})
			return fs.reply(conn, r, confFattrReply(attr, 0, nil))
		}
	default:
		fs.flag("kXR_fattr: unknown subcode %d", subcode)
		return confErr(conn, r.sid(), 3011, "bad subcode")
	}
}

// confFattrReply builds a single-attribute get/set/del reply:
// [errcount u8][numattr u8][rc u16][name\0], followed for a get that found
// something by [vlen i32][value].
func confFattrReply(name string, rc uint16, value []byte) []byte {
	var errcount byte
	if rc != 0 {
		errcount = 1
	}
	out := []byte{errcount, 1, byte(rc >> 8), byte(rc)}
	out = append(out, name...)
	out = append(out, 0)
	if value != nil {
		out = append(out, be32(uint32(len(value)))...)
		out = append(out, value...)
	}
	return out
}

func confIndexZero(p []byte) int {
	for i, c := range p {
		if c == 0 {
			return i
		}
	}
	return -1
}

// serveFS runs the request loop until the client hangs up or the server has
// deliberately broken the connection.
func (fs *confFS) serveFS(conn net.Conn) {
	defer conn.Close()

	for {
		r, err := confTake(conn)
		if err != nil {
			return
		}
		fs.mu.Lock()
		fs.ops = append(fs.ops, r.reqID())
		fs.mu.Unlock()

		if dlen := int(r.i32(20)); dlen != len(r.payload) {
			fs.flag("request %d: dlen %d does not match the %d bytes that followed", r.reqID(), dlen, len(r.payload))
		}

		done, err := fs.shape(conn, r)
		if err != nil {
			return
		}
		if done {
			continue
		}

		switch id := r.reqID(); id {
		case confDirlist:
			err = fs.serveDirlist(conn, r)
		case confOpen:
			err = fs.serveOpen(conn, r)
		case confStat:
			err = fs.serveStat(conn, r)
		case confStatx:
			err = fs.serveStatx(conn, r)
		case confMkdir:
			err = fs.serveMkdir(conn, r)
		case confMv:
			err = fs.serveMv(conn, r)
		case confChmod:
			err = fs.serveChmod(conn, r)
		case confRm:
			err = fs.serveRm(conn, r, false)
		case confRmdir:
			err = fs.serveRm(conn, r, true)
		case confTruncate:
			err = fs.serveTruncate(conn, r)
		case confQuery:
			err = fs.serveQuery(conn, r)
		case confFattr:
			err = fs.serveFattr(conn, r)
		case confPing:
			fs.zeroed(r, "kXR_ping", 4, 20)
			if len(r.payload) != 0 {
				fs.flag("kXR_ping: %d bytes of payload", len(r.payload))
			}
			err = fs.reply(conn, r, nil)
		case confClose:
			fh := r.fhandle(4)
			fs.mu.Lock()
			_, ok := fs.handles[fh]
			delete(fs.handles, fh)
			fs.mu.Unlock()
			if !ok {
				fs.flag("kXR_close: unknown file handle %v", fh)
			}
			err = fs.reply(conn, r, nil)
		default:
			fs.flag("unexpected request id %d", id)
			err = confErr(conn, r.sid(), 3011, "unsupported")
		}
		if err != nil {
			return
		}
	}
}

// confFSClient runs fn against a client wired to a namespace conformance
// server and returns the server, so a test can judge the operation by what the
// server ended up holding. setup seeds the namespace before the client starts.
func confFSClient(t *testing.T, setup func(srv *confFS), fn func(srv *confFS, fs xrdfs.FileSystem, cli *Client)) *confFS {
	t.Helper()

	srv := newConfFS()
	if setup != nil {
		setup(srv)
	}
	testClientWithMockServer(
		func(cancel func(), conn net.Conn) { srv.serveFS(conn) },
		func(cancel func(), client *Client) { fn(srv, client.FS(), client) },
	)
	return srv
}
