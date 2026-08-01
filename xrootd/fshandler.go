// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd // import "go-hep.org/x/hep/xrootd"

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sync"
	"syscall"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/clone"
	"go-hep.org/x/hep/xrootd/xrdproto/dirlist"
	"go-hep.org/x/hep/xrootd/xrdproto/mkdir"
	"go-hep.org/x/hep/xrootd/xrdproto/mv"
	"go-hep.org/x/hep/xrootd/xrdproto/open"
	"go-hep.org/x/hep/xrootd/xrdproto/read"
	"go-hep.org/x/hep/xrootd/xrdproto/rm"
	"go-hep.org/x/hep/xrootd/xrdproto/rmdir"
	"go-hep.org/x/hep/xrootd/xrdproto/stat"
	xrdsync "go-hep.org/x/hep/xrootd/xrdproto/sync"
	"go-hep.org/x/hep/xrootd/xrdproto/truncate"
	"go-hep.org/x/hep/xrootd/xrdproto/write"
	"go-hep.org/x/hep/xrootd/xrdproto/xrdclose"
	"go-hep.org/x/hep/xrootd/xrdsum"
)

// fshandler implements server.Handler API by making request to the backing filesystem at basePath.
type fshandler struct {
	Handler
	basePath string

	// map + RWMutex works a bit faster and with significant lower memory usage under Linux
	// than sync.Map for given scenarios (write to map once per session and a lot of reads per session).
	mu       sync.RWMutex
	sessions map[[16]byte]*srvSession
}

type srvSession struct {
	mu      sync.Mutex
	handles map[xrdfs.FileHandle]*srvFile

	// next is the handle the next open gets. Handles are handed out in order
	// rather than guessed at random: a counter cannot collide with a live
	// handle, so an open never fails for a reason the caller cannot act on,
	// and a failing test names the same handle every run.
	//
	// It starts at one so that the zero handle, which is what an uninitialized
	// request carries, is never a handle this server issued.
	next uint32
}

// srvFile is an open file together with the part of how it was opened that the
// request handlers still need afterwards.
type srvFile struct {
	*os.File

	// appending records a kXR_open_apnd open. Such a file is written to with
	// Write rather than WriteAt: os refuses WriteAt on an O_APPEND
	// descriptor, and the protocol says a write on a file opened for append
	// lands at the end whatever offset the request carries.
	appending bool
}

// NewFSHandler creates a Handler that passes requests to the backing filesystem at basePath.
func NewFSHandler(basePath string) Handler {
	return &fshandler{
		Handler:  Default(),
		basePath: basePath,
		sessions: make(map[[16]byte]*srvSession),
	}
}

// realPath maps the path field of a request onto the backing filesystem.
//
// The opaque data a path carries is not part of the name: a server splits it
// off and hands it to its authorization layer unparsed. A file whose request
// arrived with a token is therefore the same file as one whose request did
// not, and a handler that joins the whole field onto its base path serves a
// client that authenticates nothing and no one else.
func (h *fshandler) realPath(p string) string {
	name, _ := xrdproto.SplitPath(p)
	return path.Join(h.basePath, name)
}

// osError turns a failure from the backing filesystem into the error code an
// XRootD server answers with. Answering kXR_IOError for everything leaves a
// client unable to tell "it is already there" from "the disk is broken", and
// kXR_ItExists in particular is the *successful* outcome of the only open that
// creates without truncating: a client asked to touch a file it already has
// would otherwise be told the server has an I/O problem.
//
// The mapping is the reference server's mapError(), by way of nginx-xrootd's
// core/compat/error_mapping.c.
func osError(err error) xrdproto.ServerError {
	code := xrdproto.IOError
	switch {
	case errors.Is(err, fs.ErrNotExist):
		code = xrdproto.NotFound
	case errors.Is(err, fs.ErrExist), isNotEmpty(err):
		// The reference maps EEXIST and ENOTEMPTY to the same code: removing a
		// directory that still holds something reports kXR_ItExists rather than
		// a filesystem error, and has done since before it was a good idea.
		code = xrdproto.ItExists
	case errors.Is(err, fs.ErrPermission):
		code = xrdproto.NotAuthorized
	case errors.Is(err, syscall.ENOTDIR):
		// A path with a file in the middle of it: the namespace is wrong, not
		// the storage. syscall.ENOTDIR is the one errno every port defines.
		code = xrdproto.FSError
	case isNoSpace(err):
		code = xrdproto.NoSpace
	case errors.Is(err, errors.ErrUnsupported):
		code = xrdproto.Unsupported
	case errors.Is(err, fs.ErrInvalid):
		code = xrdproto.ArgInvalid
	}
	return xrdproto.ServerError{
		Code:    code,
		Message: fmt.Sprintf("An IO error occurred: %v", err),
	}
}

// Dirlist implements server.Handler.Dirlist.
func (h *fshandler) Dirlist(sessionID [16]byte, request *dirlist.Request) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
	// A checksum listing is a stat listing: the digest is appended to the stat
	// line, so a request that asked for kXR_dcksm alone is answered as though
	// it had asked for kXR_dstat too.
	withCksum := request.Options&dirlist.WithChecksum != 0
	algo := dirlist.DefaultChecksumAlgo
	if withCksum {
		var err error
		algo, err = dirlist.ChecksumAlgo(request.Path)
		if err != nil {
			// The reference server refuses the whole listing rather than
			// answering with a digest nobody asked for: a caller comparing
			// sha256 against an adler32 it was silently given would find every
			// file corrupt.
			return xrdproto.ServerError{
				Code:    xrdproto.InternalServerError,
				Message: err.Error(),
			}, xrdproto.Error
		}
	}

	dir := h.realPath(request.Path)
	files, err := os.ReadDir(dir)
	if err != nil {
		return osError(err), xrdproto.Error
	}

	resp := &dirlist.Response{
		WithStatInfo: request.Options&dirlist.WithStatInfo != 0 || withCksum,
		WithChecksum: withCksum,
		Entries:      make([]xrdfs.EntryStat, 0, len(files)),
	}

	for _, file := range files {
		info, err := file.Info()
		if err != nil {
			return osError(err), xrdproto.Error
		}
		var entry xrdfs.EntryStat
		switch {
		case withCksum:
			entry = xrdfs.EntryStatExtendedFrom(info)
			entry.Checksum = algo + ":" + h.checksum(path.Join(dir, info.Name()), info, algo)
		default:
			entry = xrdfs.EntryStatFrom(info)
		}
		entry.HasStatInfo = resp.WithStatInfo
		resp.Entries = append(resp.Entries, entry)
	}

	return resp, xrdproto.Ok
}

// checksum returns the hexadecimal digest of the named file under algo, or
// "none" for an entry that cannot have one: a directory, something that is
// neither a file nor a directory, or a file this server could not read.
//
// A listing is not failed over one unreadable entry. The client asked what is
// in the directory, and answering "everything except the one file whose
// permissions are wrong" would hide the rest of the directory behind it.
func (h *fshandler) checksum(name string, info os.FileInfo, algo string) string {
	const none = "none"

	if !info.Mode().IsRegular() {
		return none
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return none
	}
	sum, err := xrdsum.Sum(algo, data)
	if err != nil {
		return none
	}
	return sum
}

// Open implements server.Handler.Open.
func (h *fshandler) Open(sessionID [16]byte, request *open.Request) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
	// kXR_open_updt, kXR_new, kXR_delete and kXR_open_apnd all write to the
	// file. The protocol has no write-only mode and os.O_RDONLY is zero, so
	// an option set that asks for any of them but not for kXR_open_updt has
	// to be widened here: otherwise the file is opened read-only and the
	// first kXR_write fails with EBADF, halfway into the transfer.
	const writes = xrdfs.OpenOptionsOpenUpdate | xrdfs.OpenOptionsOpenAppend |
		xrdfs.OpenOptionsNew | xrdfs.OpenOptionsDelete

	var flag int
	switch {
	case request.Options&writes != 0:
		flag |= os.O_RDWR
	default:
		flag |= os.O_RDONLY
	}
	if request.Options&xrdfs.OpenOptionsOpenAppend != 0 {
		flag |= os.O_APPEND
	}
	if request.Options&xrdfs.OpenOptionsNew != 0 || request.Options&xrdfs.OpenOptionsDelete != 0 {
		flag |= os.O_CREATE
		if request.Options&xrdfs.OpenOptionsDelete == 0 {
			flag |= os.O_EXCL
		} else {
			flag |= os.O_TRUNC
		}
	}

	filePath := h.realPath(request.Path)
	if request.Options&xrdfs.OpenOptionsMkPath != 0 {
		// The mode of the request is the mode of the file, not of the directories
		// leading to it: creating them with, say, 0600 would leave the open that
		// asked for them failing on a path it could not enter. The reference
		// creates the chain with 0755, as does this.
		if err := os.MkdirAll(path.Dir(filePath), 0755); err != nil {
			return osError(err), xrdproto.Error
		}
	}

	file, err := os.OpenFile(filePath, flag, os.FileMode(request.Mode))
	if err != nil {
		return osError(err), xrdproto.Error
	}

	h.mu.RLock()
	sess, ok := h.sessions[sessionID]
	h.mu.RUnlock()
	if !ok {
		h.mu.Lock()
		// Check that there was no change in state during h.mu.RUnlock and h.mu.Lock.
		sess, ok = h.sessions[sessionID]
		if !ok {
			sess = &srvSession{handles: make(map[xrdfs.FileHandle]*srvFile)}
			h.sessions[sessionID] = sess
		}
		h.mu.Unlock()
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.next++
	if sess.next == 0 {
		// The counter wrapped, so the next handle would be one this session has
		// already issued. There are four billion of them, so a session that
		// gets here has leaked every one it ever opened.
		file.Close()
		return xrdproto.ServerError{
			Code:    xrdproto.InvalidRequest,
			Message: "handle limit exceeded",
		}, xrdproto.Error
	}
	var handle xrdfs.FileHandle
	binary.BigEndian.PutUint32(handle[:], sess.next)

	resp := open.Response{FileHandle: handle}
	// The compression fields precede the stat information on the wire, so a
	// response carrying stat must carry them too or the client reads the stat
	// as a page size. A file this handler serves is stored as it was written,
	// which a zero page size and an empty algorithm name say.
	if request.Options&(xrdfs.OpenOptionsCompress|xrdfs.OpenOptionsReturnStatus) != 0 {
		resp.Compression = &xrdfs.FileCompression{}
	}
	if request.Options&xrdfs.OpenOptionsReturnStatus != 0 {
		st, err := file.Stat()
		if err != nil {
			file.Close()
			return osError(err), xrdproto.Error
		}
		es := xrdfs.EntryStatFrom(st)
		resp.Stat = &es
	}
	sess.handles[handle] = &srvFile{
		File:      file,
		appending: request.Options&xrdfs.OpenOptionsOpenAppend != 0,
	}

	return resp, xrdproto.Ok
}

// virtualStat answers a kXR_stat carrying kXR_vfs from the filesystem holding
// path, which is what a storage element's own server does: the numbers a client
// sizes a transfer against have to come from the disk the transfer lands on.
//
// One node can provide the space, this one, and none of it is staging space:
// nothing here fetches a file from tape.
func virtualStat(path string) (xrdfs.VirtualFSStat, error) {
	free, total, err := diskSpace(path)
	if err != nil {
		return xrdfs.VirtualFSStat{}, err
	}

	const mb = 1024 * 1024
	vfs := xrdfs.VirtualFSStat{
		NumberRW: 1,
		FreeRW:   int(free / mb),
	}
	if total != 0 {
		// Utilization is of the partition the free figure describes, so it is
		// measured against the same figure: space this caller cannot write to
		// is space in use as far as this caller is concerned.
		vfs.UtilizationRW = int((total - free) * 100 / total)
	}
	return vfs, nil
}

// Close implements server.Handler.Close.
func (h *fshandler) Close(sessionID [16]byte, request *xrdclose.Request) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
	h.mu.RLock()
	sess, ok := h.sessions[sessionID]
	h.mu.RUnlock()
	if !ok {
		// This situation can appear if user tries to close without opening any file at all.
		return xrdproto.ServerError{
			Code:    xrdproto.InvalidRequest,
			Message: fmt.Sprintf("Invalid file handle: %v", request.Handle),
		}, xrdproto.Error
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	file, ok := sess.handles[request.Handle]
	if !ok {
		return xrdproto.ServerError{
			Code:    xrdproto.InvalidRequest,
			Message: fmt.Sprintf("Invalid file handle: %v", request.Handle),
		}, xrdproto.Error
	}
	delete(sess.handles, request.Handle)
	err := file.Close()
	if err != nil {
		return osError(err), xrdproto.Error
	}
	return nil, xrdproto.Ok
}

// Read implements server.Handler.Read.
func (h *fshandler) Read(sessionID [16]byte, request *read.Request) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
	file := h.getFile(sessionID, request.Handle)
	if file == nil {
		return xrdproto.ServerError{
			Code:    xrdproto.InvalidRequest,
			Message: fmt.Sprintf("Invalid file handle: %v", request.Handle),
		}, xrdproto.Error
	}

	buf := make([]byte, request.Length)
	n, err := file.ReadAt(buf, request.Offset)
	if err != nil && err != io.EOF {
		return osError(err), xrdproto.Error
	}

	return read.Response{Data: buf[:n]}, xrdproto.Ok
}

// Write implements server.Handler.Write.
func (h *fshandler) Write(sessionID [16]byte, request *write.Request) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
	file := h.getFile(sessionID, request.Handle)
	if file == nil {
		return xrdproto.ServerError{
			Code:    xrdproto.InvalidRequest,
			Message: fmt.Sprintf("Invalid file handle: %v", request.Handle),
		}, xrdproto.Error
	}

	var err error
	switch {
	case file.appending:
		_, err = file.Write(request.Data)
	default:
		_, err = file.WriteAt(request.Data, request.Offset)
	}
	if err != nil {
		return osError(err), xrdproto.Error
	}

	return nil, xrdproto.Ok
}

// Clone implements CloneHandler.Clone.
func (h *fshandler) Clone(sessionID [16]byte, request *clone.Request) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
	dst := h.getFile(sessionID, request.Dst)
	if dst == nil {
		return xrdproto.ServerError{
			Code:    xrdproto.InvalidRequest,
			Message: fmt.Sprintf("Invalid file handle: %v", request.Dst),
		}, xrdproto.Error
	}
	switch {
	case len(request.Items) == 0:
		return xrdproto.ServerError{
			Code:    xrdproto.ArgMissing,
			Message: "clone list is missing",
		}, xrdproto.Error
	case len(request.Items) > clone.MaxItems:
		return xrdproto.ServerError{
			Code:    xrdproto.ArgTooLong,
			Message: "too many clone items",
		}, xrdproto.Error
	}

	// The whole list is checked before any of it is copied. A clone is not
	// atomic, so a range that is refused half-way through leaves the earlier
	// ranges in the destination: the ones that can be refused before anything
	// has moved are refused there.
	srcs := make([]*srvFile, len(request.Items))
	for i, item := range request.Items {
		if err := item.Validate(); err != nil {
			return xrdproto.ServerError{
				Code:    xrdproto.ArgInvalid,
				Message: "clone offset/length out of range",
			}, xrdproto.Error
		}
		srcs[i] = h.getFile(sessionID, item.Src)
		if srcs[i] == nil {
			return xrdproto.ServerError{
				Code:    xrdproto.InvalidRequest,
				Message: fmt.Sprintf("Invalid file handle: %v", item.Src),
			}, xrdproto.Error
		}
	}

	for i, item := range request.Items {
		if item.SrcLength == 0 {
			// An empty range copies nothing, and is not the caller's mistake.
			continue
		}
		if err := copyRange(dst, srcs[i], item); err != nil {
			return xrdproto.ServerError{
				Code:    xrdproto.IOError,
				Message: "clone copy failed",
			}, xrdproto.Error
		}
	}

	return &clone.Response{}, xrdproto.Ok
}

// copyRange copies one range of a clone request between two open files.
func copyRange(dst, src *srvFile, item clone.Item) error {
	const chunk = 1 << 20

	for done := int64(0); done < item.SrcLength; {
		n := min(item.SrcLength-done, chunk)
		buf := make([]byte, n)
		read, err := src.ReadAt(buf, item.SrcOffset+done)
		if read > 0 {
			if _, err := dst.WriteAt(buf[:read], item.DstOffset+done); err != nil {
				return err
			}
			done += int64(read)
		}
		if err != nil {
			// A source range that runs past the end of its file is short, and a
			// short clone is a failed one: the destination would be left with a
			// hole where the caller expects data.
			return err
		}
	}
	return nil
}

func (h *fshandler) getFile(sessionID [16]byte, handle xrdfs.FileHandle) *srvFile {
	h.mu.RLock()
	sess, ok := h.sessions[sessionID]
	h.mu.RUnlock()
	if !ok {
		return nil
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	file, ok := sess.handles[handle]
	if !ok {
		return nil
	}
	return file
}

// Stat implements server.Handler.Stat.
func (h *fshandler) Stat(sessionID [16]byte, request *stat.Request) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
	if request.Options&stat.OptionsVFS != 0 {
		// A virtual stat is about the storage behind a path, not the path, so
		// an absent path asks about the export as a whole rather than being an
		// error. A handle names a file, and the filesystem it was opened on is
		// the one to report.
		path := h.basePath
		switch {
		case len(request.Path) != 0:
			path = h.realPath(request.Path)
		case request.FileHandle != (xrdfs.FileHandle{}):
			file := h.getFile(sessionID, request.FileHandle)
			if file == nil {
				return xrdproto.ServerError{
					Code:    xrdproto.InvalidRequest,
					Message: fmt.Sprintf("Invalid file handle: %v", request.FileHandle),
				}, xrdproto.Error
			}
			path = file.Name()
		}

		vfs, err := virtualStat(path)
		if err != nil {
			return osError(err), xrdproto.Error
		}
		return vfs, xrdproto.Ok
	}

	var fi os.FileInfo
	var err error
	if len(request.Path) == 0 {
		file := h.getFile(sessionID, request.FileHandle)
		if file == nil {
			return xrdproto.ServerError{
				Code:    xrdproto.InvalidRequest,
				Message: fmt.Sprintf("Invalid file handle: %v", request.FileHandle),
			}, xrdproto.Error
		}
		fi, err = file.Stat()
	} else {
		fi, err = os.Stat(h.realPath(request.Path))
	}

	if err != nil {
		return osError(err), xrdproto.Error
	}

	return stat.DefaultResponse{EntryStat: xrdfs.EntryStatFrom(fi)}, xrdproto.Ok
}

// Truncate implements server.Handler.Truncate.
func (h *fshandler) Truncate(sessionID [16]byte, request *truncate.Request) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
	var err error
	if len(request.Path) == 0 {
		file := h.getFile(sessionID, request.Handle)
		if file == nil {
			return xrdproto.ServerError{
				Code:    xrdproto.InvalidRequest,
				Message: fmt.Sprintf("Invalid file handle: %v", request.Handle),
			}, xrdproto.Error
		}
		err = file.Truncate(request.Size)
	} else {
		err = os.Truncate(h.realPath(request.Path), request.Size)
	}

	if err != nil {
		return osError(err), xrdproto.Error
	}

	return nil, xrdproto.Ok
}

// Sync implements server.Handler.Sync.
func (h *fshandler) Sync(sessionID [16]byte, request *xrdsync.Request) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
	file := h.getFile(sessionID, request.Handle)
	if file == nil {
		return xrdproto.ServerError{
			Code:    xrdproto.InvalidRequest,
			Message: fmt.Sprintf("Invalid file handle: %v", request.Handle),
		}, xrdproto.Error
	}

	if err := file.Sync(); err != nil {
		return osError(err), xrdproto.Error
	}

	return nil, xrdproto.Ok
}

// Rename implements server.Handler.Rename.
func (h *fshandler) Rename(sessionID [16]byte, request *mv.Request) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
	if err := os.Rename(h.realPath(request.OldPath), h.realPath(request.NewPath)); err != nil {
		return osError(err), xrdproto.Error
	}

	return nil, xrdproto.Ok
}

// Mkdir implements server.Handler.Mkdir.
func (h *fshandler) Mkdir(sessionID [16]byte, request *mkdir.Request) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
	mkdirFunc := os.Mkdir
	if request.Options&mkdir.OptionsMakePath != 0 {
		mkdirFunc = os.MkdirAll
	}

	if err := mkdirFunc(h.realPath(request.Path), os.FileMode(request.Mode)); err != nil {
		return osError(err), xrdproto.Error
	}
	return nil, xrdproto.Ok
}

// Remove implements server.Handler.Remove.
func (h *fshandler) Remove(sessionID [16]byte, request *rm.Request) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
	if err := os.Remove(h.realPath(request.Path)); err != nil {
		return osError(err), xrdproto.Error
	}
	return nil, xrdproto.Ok
}

// RemoveDir implements server.Handler.RemoveDir.
func (h *fshandler) RemoveDir(sessionID [16]byte, request *rmdir.Request) (xrdproto.Marshaler, xrdproto.ResponseStatus) {
	if err := os.Remove(h.realPath(request.Path)); err != nil {
		return osError(err), xrdproto.Error
	}
	return nil, xrdproto.Ok
}

// CloseSession implements server.Handler.CloseSession.
func (h *fshandler) CloseSession(sessionID [16]byte) error {
	h.mu.Lock()
	sess, ok := h.sessions[sessionID]
	if !ok {
		// That means that no files were opened in that session and we have nothing to clear.
		h.mu.Unlock()
		return nil
	}
	delete(h.sessions, sessionID)
	h.mu.Unlock()
	sess.mu.Lock()
	defer sess.mu.Unlock()

	var err error
	for _, f := range sess.handles {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	return err
}
