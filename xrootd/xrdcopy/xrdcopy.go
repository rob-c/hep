// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package xrdcopy is a copy engine for XRootD: it transfers files between the
// local filesystem and a remote XRootD server (download and upload), copies
// directory trees recursively, and can verify a transfer against the server's
// checksum. It is the programmatic core of an xrdcp-equivalent.
package xrdcopy // import "go-hep.org/x/hep/xrootd/xrdcopy"

import (
	"context"
	"fmt"
	"io"
	"os"
	stdpath "path"
	"path/filepath"
	"time"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdio"
	"go-hep.org/x/hep/xrootd/xrdsum"
)

// DefaultChunkSize is the transfer buffer size used when Options.ChunkSize is 0.
const DefaultChunkSize = 16 << 20

// Options configures a Copy.
type Options struct {
	// ChunkSize is the transfer buffer size in bytes (default DefaultChunkSize).
	ChunkSize int
	// Recursive enables copying directory trees.
	Recursive bool
	// Verify, when true, compares the downloaded content against the server's
	// checksum (when the server reports one); a mismatch fails the copy.
	Verify bool
	// Resume, when true, continues a partially transferred file from the size
	// already present at the destination instead of starting over.
	Resume bool
	// Username is the XRootD login name (defaults to the URL user, then the OS user).
	Username string
}

func (o Options) chunk() int {
	if o.ChunkSize > 0 {
		return o.ChunkSize
	}
	return DefaultChunkSize
}

// Copy transfers src to dst. Each of src and dst is either a local path or an
// XRootD URL (root://, roots://, xroot://, xroots://). Local↔remote transfers
// (download and upload) and local↔local copies are supported; remote↔remote
// (third-party copy) is not yet implemented.
func Copy(ctx context.Context, dst, src string, opts Options) error {
	srcRemote, srcURL, err := remoteURL(src)
	if err != nil {
		return err
	}
	dstRemote, dstURL, err := remoteURL(dst)
	if err != nil {
		return err
	}

	switch {
	case srcRemote && !dstRemote:
		return download(ctx, srcURL, dst, opts)
	case !srcRemote && dstRemote:
		return upload(ctx, src, dstURL, opts)
	case !srcRemote && !dstRemote:
		return localCopy(dst, src, opts)
	default:
		return TPC(ctx, dst, src, opts)
	}
}

// remoteURL reports whether name is a remote XRootD URL and, if so, returns its
// parsed form.
func remoteURL(name string) (bool, xrootd.URL, error) {
	u, err := xrootd.ParseURL(name)
	if err != nil {
		return false, xrootd.URL{}, err
	}
	switch u.Scheme {
	case "root", "roots", "xroot", "xroots":
		return true, u, nil
	default:
		return false, xrootd.URL{}, nil
	}
}

func (o Options) user(u xrootd.URL) string {
	switch {
	case o.Username != "":
		return o.Username
	case u.User != "":
		return u.User
	default:
		if v := os.Getenv("USER"); v != "" {
			return v
		}
		return "nobody"
	}
}

func download(ctx context.Context, src xrootd.URL, dst string, opts Options) error {
	client, err := xrootd.NewClient(ctx, src.Addr, opts.user(src))
	if err != nil {
		return fmt.Errorf("xrdcopy: could not connect to %q: %w", src.Addr, err)
	}
	defer client.Close()
	fs := client.FS()

	st, err := fs.Stat(ctx, src.Path)
	if err != nil {
		return fmt.Errorf("xrdcopy: could not stat %q: %w", src.Path, err)
	}
	if st.IsDir() {
		if !opts.Recursive {
			return fmt.Errorf("xrdcopy: %q is a directory (use Recursive)", src.Path)
		}
		return downloadDir(ctx, fs, src.Path, dst, opts)
	}
	return downloadFile(ctx, fs, src.Path, dst, opts)
}

func downloadDir(ctx context.Context, fs xrdfs.FileSystem, remoteDir, localDir string, opts Options) error {
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return err
	}
	entries, err := fs.Dirlist(ctx, remoteDir)
	if err != nil {
		return fmt.Errorf("xrdcopy: could not list %q: %w", remoteDir, err)
	}
	for _, e := range entries {
		rpath := stdpath.Join(remoteDir, e.Name())
		lpath := filepath.Join(localDir, e.Name())
		if e.IsDir() {
			if err := downloadDir(ctx, fs, rpath, lpath, opts); err != nil {
				return err
			}
			continue
		}
		if err := downloadFile(ctx, fs, rpath, lpath, opts); err != nil {
			return err
		}
	}
	return nil
}

func downloadFile(ctx context.Context, fs xrdfs.FileSystem, remotePath, localPath string, opts Options) error {
	f, err := xrdio.OpenFrom(fs, remotePath)
	if err != nil {
		return err
	}
	defer f.Close()

	rst, err := f.Stat()
	if err != nil {
		return fmt.Errorf("xrdcopy: could not stat %q: %w", remotePath, err)
	}
	remoteSize := rst.Size()

	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}

	// Resume: continue from the bytes already present locally.
	var offset int64
	if opts.Resume {
		var complete bool
		if offset, complete = resumeOffset(localPath, remoteSize); complete {
			return nil
		}
	}

	flags := os.O_WRONLY | os.O_CREATE
	if offset > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	out, err := os.OpenFile(localPath, flags, 0o644)
	if err != nil {
		return fmt.Errorf("xrdcopy: could not open %q: %w", localPath, err)
	}
	src := io.NewSectionReader(f, offset, remoteSize-offset)
	if _, err := io.CopyBuffer(out, src, make([]byte, opts.chunk())); err != nil {
		out.Close()
		return fmt.Errorf("xrdcopy: could not copy %q: %w", remotePath, err)
	}
	if err := out.Close(); err != nil {
		return err
	}

	if opts.Verify {
		if err := verifyChecksum(ctx, fs, remotePath, localPath); err != nil {
			return err
		}
	}
	return nil
}

// verifyChecksum compares the local file's digest with the server-reported
// checksum. It is a no-op (nil) when the server does not implement checksums or
// reports an algorithm the client cannot compute.
func verifyChecksum(ctx context.Context, fs xrdfs.FileSystem, remotePath, localPath string) error {
	cks, ok := fs.(xrdfs.ChecksumFS)
	if !ok {
		return nil
	}
	// Bound the checksum query so a server that accepts but never answers it
	// cannot hang the copy.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
	}
	algo, want, err := cks.Checksum(ctx, remotePath)
	if err != nil {
		return nil // server does not provide a checksum for this file
	}
	data, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}
	got, err := xrdsum.Sum(algo, data)
	if err != nil {
		return nil // client cannot compute this algorithm; skip
	}
	if got != want {
		return fmt.Errorf("xrdcopy: checksum mismatch for %q (%s): local=%s server=%s", remotePath, algo, got, want)
	}
	return nil
}

func upload(ctx context.Context, src string, dst xrootd.URL, opts Options) error {
	client, err := xrootd.NewClient(ctx, dst.Addr, opts.user(dst))
	if err != nil {
		return fmt.Errorf("xrdcopy: could not connect to %q: %w", dst.Addr, err)
	}
	defer client.Close()
	fs := client.FS()

	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if !opts.Recursive {
			return fmt.Errorf("xrdcopy: %q is a directory (use Recursive)", src)
		}
		return uploadDir(ctx, fs, src, dst.Path, opts)
	}
	return uploadFile(ctx, fs, src, dst.Path, opts)
}

func uploadDir(ctx context.Context, fs xrdfs.FileSystem, localDir, remoteDir string, opts Options) error {
	if err := fs.MkdirAll(ctx, remoteDir, xrdfs.OpenModeOwnerRead|xrdfs.OpenModeOwnerWrite|xrdfs.OpenModeOwnerExecute); err != nil {
		return fmt.Errorf("xrdcopy: could not mkdir %q: %w", remoteDir, err)
	}
	entries, err := os.ReadDir(localDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		lpath := filepath.Join(localDir, e.Name())
		rpath := stdpath.Join(remoteDir, e.Name())
		if e.IsDir() {
			if err := uploadDir(ctx, fs, lpath, rpath, opts); err != nil {
				return err
			}
			continue
		}
		if err := uploadFile(ctx, fs, lpath, rpath, opts); err != nil {
			return err
		}
	}
	return nil
}

func uploadFile(ctx context.Context, fs xrdfs.FileSystem, localPath, remotePath string, opts Options) error {
	in, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer in.Close()
	lst, err := in.Stat()
	if err != nil {
		return err
	}

	// Resume: continue from the bytes already present at the destination.
	var off int64
	options := xrdfs.OpenOptionsNew | xrdfs.OpenOptionsDelete | xrdfs.OpenOptionsMkPath
	if opts.Resume {
		if rst, err := fs.Stat(ctx, remotePath); err == nil {
			var complete bool
			off, complete = resumeOffsetSize(rst.Size(), lst.Size())
			if complete {
				return nil
			}
			if off > 0 {
				options = xrdfs.OpenOptionsOpenUpdate | xrdfs.OpenOptionsMkPath
			}
		}
	}
	if off > 0 {
		if _, err := in.Seek(off, io.SeekStart); err != nil {
			return err
		}
	}

	f, err := fs.Open(ctx, remotePath,
		xrdfs.OpenModeOwnerRead|xrdfs.OpenModeOwnerWrite, options)
	if err != nil {
		return fmt.Errorf("xrdcopy: could not create %q: %w", remotePath, err)
	}
	defer f.Close(ctx)

	buf := make([]byte, opts.chunk())
	for {
		n, rerr := in.Read(buf)
		if n > 0 {
			if werr := f.WriteAtContext(ctx, buf[:n], off); werr != nil {
				return fmt.Errorf("xrdcopy: could not write %q: %w", remotePath, werr)
			}
			off += int64(n)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	return f.Sync(ctx)
}

// resumeOffset reports the resume offset for a destination file at path given a
// source of srcSize bytes: the destination's current size when it is a partial
// prefix, and complete=true when it already matches srcSize.
func resumeOffset(path string, srcSize int64) (offset int64, complete bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	return resumeOffsetSize(fi.Size(), srcSize)
}

// resumeOffsetSize is resumeOffset for a known destination size.
func resumeOffsetSize(dstSize, srcSize int64) (offset int64, complete bool) {
	switch {
	case dstSize == srcSize && srcSize > 0:
		return 0, true
	case dstSize > 0 && dstSize < srcSize:
		return dstSize, false
	default:
		return 0, false
	}
}

func localCopy(dst, src string, opts Options) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("xrdcopy: local directory copy is not supported")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.CopyBuffer(out, in, make([]byte, opts.chunk())); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
