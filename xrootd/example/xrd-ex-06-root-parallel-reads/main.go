// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-06-root-parallel-reads keeps several reads in flight at once on one connection.
//
// The protocol multiplexes by stream id, so concurrent reads on one client do
// not need one connection each -- and over a link with 150ms of latency, this
// is the difference between a job that is bandwidth-bound and one that is
// latency-bound.
//
// See also WithSubStreams, which puts bulk payload on a second socket
// (kXR_bind) so a large read cannot stall the request stream behind it.
package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdfs"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cli, err := xrootd.NewClient(ctx, "eospublic.cern.ch:1094", "gopher",
		xrootd.WithSubStreams(1), // payload on its own socket; 0 sends inline
	)
	if err != nil {
		log.Fatalf("could not connect: %+v", err)
	}
	defer cli.Close()

	f, err := cli.FS().Open(ctx, "/eos/opendata/cms/file.root",
		xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
	if err != nil {
		log.Fatalf("could not open: %+v", err)
	}
	defer f.Close(ctx)

	const (
		chunk  = 1 << 20
		chunks = 8
	)
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	bufs := make([][]byte, chunks)
	for i := range chunks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, chunk)
			n, err := f.ReadAtContext(ctx, buf, int64(i)*chunk)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("chunk %d: %w", i, err))
				mu.Unlock()
				return
			}
			bufs[i] = buf[:n]
		}()
	}
	wg.Wait()

	if len(errs) != 0 {
		log.Fatalf("%d of %d reads failed: %v", len(errs), chunks, errs[0])
	}
	var total int
	for _, b := range bufs {
		total += len(b)
	}
	fmt.Printf("read %d bytes in %d parallel chunks\n", total, chunks)
}
