// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance at scale, driven against the strict server in
// conformance_server_test.go.
//
// The tests elsewhere in this package check one operation at a time with a
// well-chosen offset. These sweep the parameter space instead — every
// combination of offset and length around the 4 KiB page grid, segment counts
// up to the vector limit, a megabyte in one frame — and then put the session
// under concurrent load. Page-boundary arithmetic and stream demultiplexing
// are exactly the two things a hand-picked case set misses: they are correct
// for the cases picked and wrong one byte to either side.

package xrootd

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

// confGridOffsets and confGridLengths straddle every 4 KiB page boundary in
// confContent, which is 10000 bytes: two whole pages and a short third.
var (
	confGridOffsets = []int64{0, 1, 7, 4095, 4096, 4097, 8191, 8192, 8193, 9999}
	confGridLengths = []int{1, 7, 4095, 4096, 4097, 8192}
)

// confWant returns the bytes confContent holds for [off, off+n), clamped at
// end of file, which is what every read path must produce.
func confWant(off int64, n int) []byte {
	if off >= int64(len(confContent)) {
		return nil
	}
	hi := off + int64(n)
	if hi > int64(len(confContent)) {
		hi = int64(len(confContent))
	}
	return confContent[off:hi]
}

// TestConformance_Read_OffsetLengthSweep reads every offset/length pair on the
// page grid. A read is a byte range with no alignment rule, so any dependence
// on the page size is a bug — and it only shows up on one side of a boundary.
func TestConformance_Read_OffsetLengthSweep(t *testing.T) {
	srv := confClient(t, confContent, func(srv *confServer, f *file) {
		for _, off := range confGridOffsets {
			for _, n := range confGridLengths {
				want := confWant(off, n)
				p := make([]byte, n)
				got, err := f.ReadAt(p, off)
				if err != nil && got == len(want) && got < n {
					err = nil // a short read at end of file reports io.EOF
				}
				if err != nil {
					t.Fatalf("ReadAt(%d bytes at %d): %v", n, off, err)
				}
				if got != len(want) {
					t.Fatalf("ReadAt(%d bytes at %d): got %d bytes, want %d", n, off, got, len(want))
				}
				if !bytes.Equal(p[:got], want) {
					t.Fatalf("ReadAt(%d bytes at %d): the bytes differ from the file content", n, off)
				}
			}
		}
	})
	srv.check(t)
}

// TestConformance_PgRead_OffsetLengthSweep is the same sweep through kXR_pgread,
// where the page grid is not incidental: the reply is split into units aligned
// to the FILE offset, so the first unit of an unaligned read is short and every
// checksum covers a different number of bytes.
func TestConformance_PgRead_OffsetLengthSweep(t *testing.T) {
	ctx := context.Background()
	srv := confClient(t, confContent, func(srv *confServer, f *file) {
		for _, off := range confGridOffsets {
			for _, n := range confGridLengths {
				want := confWant(off, n)
				p := make([]byte, n)
				got, err := f.PgReadAt(ctx, p, off)
				if err != nil {
					t.Fatalf("PgReadAt(%d bytes at %d): %v", n, off, err)
				}
				if got != len(want) {
					t.Fatalf("PgReadAt(%d bytes at %d): got %d bytes, want %d", n, off, got, len(want))
				}
				if !bytes.Equal(p[:got], want) {
					t.Fatalf("PgReadAt(%d bytes at %d): the bytes differ from the file content", n, off)
				}
			}
		}
	})
	srv.check(t)
}

// TestConformance_Write_OffsetLengthSweep writes at every grid position and
// reads the range back out of the server, so the assertion is on the bytes
// that landed rather than on the status.
func TestConformance_Write_OffsetLengthSweep(t *testing.T) {
	for _, off := range confGridOffsets {
		for _, n := range []int{1, 7, 4095, 4096, 4097} {
			t.Run(fmt.Sprintf("%d-at-%d", n, off), func(t *testing.T) {
				payload := confBytes(n)
				want := append([]byte(nil), confContent...)
				if need := off + int64(n); need > int64(len(want)) {
					want = append(want, make([]byte, need-int64(len(want)))...)
				}
				copy(want[off:], payload)

				srv := confClient(t, confContent, func(srv *confServer, f *file) {
					if _, err := f.WriteAt(payload, off); err != nil {
						t.Fatalf("WriteAt: %v", err)
					}
				})
				srv.check(t)
				if !bytes.Equal(srv.content(), want) {
					t.Fatal("the file the server holds differs from what was written")
				}
			})
		}
	}
}

// TestConformance_PgWrite_OffsetLengthSweep is the same through kXR_pgwrite,
// which frames the payload into checksummed units the server verifies. An
// unaligned write must still produce exactly the bytes handed to it.
func TestConformance_PgWrite_OffsetLengthSweep(t *testing.T) {
	ctx := context.Background()
	for _, off := range confGridOffsets {
		for _, n := range []int{1, 7, 4095, 4096, 4097} {
			t.Run(fmt.Sprintf("%d-at-%d", n, off), func(t *testing.T) {
				payload := confBytes(n)
				want := append([]byte(nil), confContent...)
				if need := off + int64(n); need > int64(len(want)) {
					want = append(want, make([]byte, need-int64(len(want)))...)
				}
				copy(want[off:], payload)

				srv := confClient(t, confContent, func(srv *confServer, f *file) {
					if err := f.PgWriteAt(ctx, payload, off); err != nil {
						t.Fatalf("PgWriteAt: %v", err)
					}
				})
				srv.check(t)
				if !bytes.Equal(srv.content(), want) {
					t.Fatal("the file the server holds differs from what was written")
				}
			})
		}
	}
}

// TestConformance_ReadV_SegmentCountSweep walks the segment count up to the
// protocol limit. The reply carries no request-side index — the segments are
// matched by position — so a count that overruns a buffer or a header that is
// miscounted shows up as data from the wrong offset, not as an error.
func TestConformance_ReadV_SegmentCountSweep(t *testing.T) {
	ctx := context.Background()
	srv := confClient(t, confContent, func(srv *confServer, f *file) {
		for _, nsegs := range []int{1, 2, 3, 15, 16, 17, 255, 256, 257, 1023, xrdproto.MaxVectorSegments} {
			segs := make([]xrdfs.ReadVSegment, nsegs)
			for i := range segs {
				// Spread the segments over the file, varying the length so a
				// reply that shifts by one segment cannot still match.
				segs[i] = xrdfs.ReadVSegment{
					Offset: int64((i * 7) % (len(confContent) - 64)),
					Length: 1 + i%17,
				}
			}

			got, err := f.ReadVAt(ctx, segs)
			if err != nil {
				t.Fatalf("ReadVAt with %d segments: %v", nsegs, err)
			}
			if len(got) != nsegs {
				t.Fatalf("ReadVAt with %d segments returned %d", nsegs, len(got))
			}
			for i, seg := range segs {
				want := confContent[seg.Offset : seg.Offset+int64(seg.Length)]
				if !bytes.Equal(got[i], want) {
					t.Fatalf("%d segments: segment %d (offset %d, length %d) is not the file content",
						nsegs, i, seg.Offset, seg.Length)
				}
			}
		}
	})
	srv.check(t)
}

// TestConformance_WriteV_SegmentCountSweep does the same for kXR_writev, whose
// segment data travels past the declared payload length: the more segments
// there are, the more bytes the server must find outside the frame.
func TestConformance_WriteV_SegmentCountSweep(t *testing.T) {
	ctx := context.Background()
	for _, nsegs := range []int{1, 2, 16, 17, 256, 1023, xrdproto.MaxVectorSegments} {
		t.Run(fmt.Sprintf("%d-segments", nsegs), func(t *testing.T) {
			segs := make([]xrdfs.WriteVSegment, nsegs)
			want := append([]byte(nil), confContent...)
			for i := range segs {
				off := int64(i * 9)
				data := confBytes(1 + i%13)
				segs[i] = xrdfs.WriteVSegment{Offset: off, Data: data}
				copy(want[off:], data)
			}

			srv := confClient(t, confContent, func(srv *confServer, f *file) {
				if err := f.WriteVAt(ctx, segs); err != nil {
					t.Fatalf("WriteVAt with %d segments: %v", nsegs, err)
				}
			})
			srv.check(t)
			if !bytes.Equal(srv.content(), want) {
				t.Fatalf("%d segments: the file the server holds differs from what the vector wrote", nsegs)
			}
			if got, want := srv.opCount(3031), 1; got != want {
				t.Fatalf("%d segments took %d kXR_writev requests, want %d", nsegs, got, want)
			}
		})
	}
}

// TestConformance_MegabyteInOneRequest checks a payload far past any plausible
// socket buffer, in both directions and in one request each. A frame this size
// is written and read in many syscalls, so a client that assumes one write is
// one frame fails here and nowhere smaller.
func TestConformance_MegabyteInOneRequest(t *testing.T) {
	const size = 1<<20 + 1234 // deliberately not a round number of pages
	content := confBytes(size)

	srv := confClient(t, content, func(srv *confServer, f *file) {
		p := make([]byte, size)
		n, err := f.ReadAt(p, 0)
		if err != nil {
			t.Fatalf("ReadAt: %v", err)
		}
		if n != size || !bytes.Equal(p, content) {
			t.Fatalf("read %d of %d bytes, and they differ from the file content", n, size)
		}

		payload := confBytes(size)
		for i := range payload {
			payload[i] ^= 0xff
		}
		if _, err := f.WriteAt(payload, 0); err != nil {
			t.Fatalf("WriteAt: %v", err)
		}
		if got, want := srv.opCount(3019), 1; got != want {
			t.Fatalf("a megabyte took %d kXR_write requests, want %d", got, want)
		}
		if !bytes.Equal(srv.content(), payload) {
			t.Fatal("the file the server holds differs from what was written")
		}
	})
	srv.check(t)
}

// TestConformance_ConcurrentRequestsOnOneConnection puts many requests in
// flight at once. Everything shares one socket and one mux, so the only thing
// keeping the replies apart is the stream id: a reply routed to the wrong
// waiter shows up as one caller getting another's bytes.
func TestConformance_ConcurrentRequestsOnOneConnection(t *testing.T) {
	const workers = 64
	ctx := context.Background()

	srv := confClient(t, confContent, func(srv *confServer, f *file) {
		var (
			wg   sync.WaitGroup
			mu   sync.Mutex
			errs []error
		)
		fail := func(format string, args ...any) {
			mu.Lock()
			defer mu.Unlock()
			errs = append(errs, fmt.Errorf(format, args...))
		}

		for i := range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// A distinct range per worker, so a misrouted reply cannot
				// look like the right answer.
				off := int64(i * 137)
				n := 1 + i%251

				switch i % 3 {
				case 0:
					p := make([]byte, n)
					got, err := f.ReadAt(p, off)
					switch {
					case err != nil:
						fail("worker %d: ReadAt: %w", i, err)
					case !bytes.Equal(p[:got], confWant(off, n)):
						fail("worker %d: ReadAt(%d at %d) returned another range's bytes", i, n, off)
					}
				case 1:
					p := make([]byte, n)
					got, err := f.PgReadAt(ctx, p, off)
					switch {
					case err != nil:
						fail("worker %d: PgReadAt: %w", i, err)
					case !bytes.Equal(p[:got], confWant(off, n)):
						fail("worker %d: PgReadAt(%d at %d) returned another range's bytes", i, n, off)
					}
				default:
					segs := []xrdfs.ReadVSegment{
						{Offset: off, Length: n},
						{Offset: off + 1, Length: n},
					}
					got, err := f.ReadVAt(ctx, segs)
					switch {
					case err != nil:
						fail("worker %d: ReadVAt: %w", i, err)
					case !bytes.Equal(got[0], confWant(off, n)):
						fail("worker %d: ReadVAt segment 0 returned another range's bytes", i)
					case !bytes.Equal(got[1], confWant(off+1, n)):
						fail("worker %d: ReadVAt segment 1 returned another range's bytes", i)
					}
				}
			}()
		}
		wg.Wait()

		for _, err := range errs {
			t.Error(err)
		}
	})
	srv.check(t)
	if got, want := len(srv.opSeq()), workers; got != want {
		t.Fatalf("the server saw %d requests, want %d: each call is one round trip", got, want)
	}
}

// TestConformance_RepliesMayArriveOutOfOrder holds a batch of requests back and
// answers them last-first. Nothing in the protocol promises a reply order, so a
// client that pairs the n-th reply with the n-th request works against a
// single-threaded server and hands back the wrong bytes against a real one.
func TestConformance_RepliesMayArriveOutOfOrder(t *testing.T) {
	const batch = 8

	srv := confClient(t, confContent, func(srv *confServer, f *file) {
		srv.set(func(srv *confServer) { srv.reorder = batch })

		var (
			wg   sync.WaitGroup
			mu   sync.Mutex
			errs []error
		)
		for i := range batch {
			wg.Add(1)
			go func() {
				defer wg.Done()
				off, n := int64(i*512), 64+i
				p := make([]byte, n)
				got, err := f.ReadAt(p, off)
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err != nil:
					errs = append(errs, fmt.Errorf("read %d: %w", i, err))
				case !bytes.Equal(p[:got], confWant(off, n)):
					errs = append(errs, fmt.Errorf("read %d got the bytes of another request", i))
				}
			}()
		}
		wg.Wait()
		// Let the server stop holding requests back before the file is closed.
		srv.set(func(srv *confServer) { srv.reorder = 0 })

		for _, err := range errs {
			t.Error(err)
		}
	})
	srv.check(t)
}

// TestConformance_StreamIDsSurviveTheSecondByte runs enough requests for the
// mux to allocate past id 255. The stream id is two bytes, and a client that
// writes or matches only the low one is correct for the first 256 requests of
// a session and then starts crossing replies over.
func TestConformance_StreamIDsSurviveTheSecondByte(t *testing.T) {
	const requests = 600

	srv := confClient(t, confContent, func(srv *confServer, f *file) {
		for i := range requests {
			off := int64(i % (len(confContent) - 32))
			p := make([]byte, 32)
			if _, err := f.ReadAt(p, off); err != nil {
				t.Fatalf("read %d of %d: %v", i, requests, err)
			}
			if !bytes.Equal(p, confWant(off, 32)) {
				t.Fatalf("read %d of %d returned the wrong range", i, requests)
			}
		}
	})
	srv.check(t)
	if got, want := srv.opCount(3013), requests; got != want {
		t.Fatalf("the server saw %d reads, want %d", got, want)
	}
}
