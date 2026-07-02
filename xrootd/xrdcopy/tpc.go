// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdcopy // import "go-hep.org/x/hep/xrootd/xrdcopy"

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"time"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdfs"
)

// tpcTransferTimeout bounds the deferred destination pull of a third-party copy.
const tpcTransferTimeout = 5 * time.Minute

// genTPCKey mints a random third-party-copy rendezvous key (24 lowercase hex
// characters / 12 bytes), matching the stock XRootD client.
func genTPCKey() (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

// hostPort splits an XRootD address into host and port, defaulting to 1094.
func hostPort(addr string) (string, int) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, 1094
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		return host, 1094
	}
	return host, p
}

// TPC performs a server-side third-party copy from src to dst: the destination
// server pulls the file directly from the source, so the bytes do not transit
// this process. Both src and dst must be XRootD URLs.
//
// It follows the stock XRootD client protocol: a placement probe of the source
// (for its size), a source open registering the rendezvous key
// (tpc.stage=copy), and a destination open carrying the full TPC opaque; the
// destination open pulls from the source and its reply is deferred (kXR_waitresp)
// until the transfer completes.
func TPC(ctx context.Context, dst, src string, opts Options) error {
	sRemote, su, err := remoteURL(src)
	if err != nil {
		return err
	}
	dRemote, du, err := remoteURL(dst)
	if err != nil {
		return err
	}
	if !sRemote || !dRemote {
		return fmt.Errorf("xrdcopy: TPC requires two XRootD URLs")
	}

	key, err := genTPCKey()
	if err != nil {
		return fmt.Errorf("xrdcopy: could not generate TPC key: %w", err)
	}
	sHost, sPort := hostPort(su.Addr)
	srcHP := fmt.Sprintf("%s:%d", sHost, sPort)
	dHost, _ := hostPort(du.Addr)

	srcClient, err := xrootd.NewClient(ctx, su.Addr, opts.user(su))
	if err != nil {
		return fmt.Errorf("xrdcopy: could not connect to source %q: %w", su.Addr, err)
	}
	defer srcClient.Close()
	srcFS := srcClient.FS()

	// 1. Learn the source size for the destination's oss.asize hint.
	st, err := srcFS.Stat(ctx, su.Path)
	if err != nil {
		return fmt.Errorf("xrdcopy: TPC source stat failed: %w", err)
	}
	size := st.Size()

	// Placement probe: open the source with tpc.stage=placement and close it,
	// as the stock client does to prepare the transfer.
	if pf, err := srcFS.Open(ctx, su.Path+"?tpc.stage=placement", 0,
		xrdfs.OpenOptionsOpenRead|xrdfs.OpenOptionsReturnStatus|xrdfs.OpenOptionsAsync); err == nil {
		pf.Close(ctx)
	}

	dstClient, err := xrootd.NewClient(ctx, du.Addr, opts.user(du))
	if err != nil {
		return fmt.Errorf("xrdcopy: could not connect to destination %q: %w", du.Addr, err)
	}
	defer dstClient.Close()

	// 2. Source open (coordinator): registers the key, naming the destination
	//    host (tpc.dst). The handle is kept open for the whole transfer so the
	//    registration stays live; closing it early would unregister the key
	//    before the destination pulls.
	srcOpaque := fmt.Sprintf("tpc.dst=%s&tpc.key=%s&tpc.stage=copy", dHost, key)
	sf, err := srcFS.Open(ctx, su.Path+"?"+srcOpaque, 0,
		xrdfs.OpenOptionsOpenRead|xrdfs.OpenOptionsReturnStatus|xrdfs.OpenOptionsAsync)
	if err != nil {
		return fmt.Errorf("xrdcopy: TPC source coordinator open failed: %w", err)
	}
	defer sf.Close(ctx)

	// 3. Destination open (puller): carries the full TPC opaque. The destination
	//    connects to the source, pulls the bytes, and defers this open's reply
	//    until the transfer completes.
	dstOpaque := fmt.Sprintf(
		"oss.asize=%d&tpc.dlg=%s&tpc.dlgon=0&tpc.key=%s&tpc.lfn=%s&tpc.spr=root&tpc.src=%s&tpc.stage=copy&tpc.tpr=root",
		size, srcHP, key, su.Path, srcHP)

	// The destination open must use the update+delete flags the TPC handler
	// recognises (a create/new open is treated as a plain write, not a pull),
	// with mode 0644 to match the stock client.
	const mode0644 = xrdfs.OpenModeOwnerRead | xrdfs.OpenModeOwnerWrite |
		xrdfs.OpenModeGroupRead | xrdfs.OpenModeOtherRead
	pullCtx, cancel := context.WithTimeout(ctx, tpcTransferTimeout)
	defer cancel()
	df, err := dstClient.FS().Open(pullCtx, du.Path+"?"+dstOpaque, mode0644,
		xrdfs.OpenOptionsDelete|xrdfs.OpenOptionsOpenUpdate|xrdfs.OpenOptionsReturnStatus|xrdfs.OpenOptionsAsync)
	if err != nil {
		return fmt.Errorf("xrdcopy: TPC transfer failed: %w", err)
	}
	return df.Close(ctx)
}
