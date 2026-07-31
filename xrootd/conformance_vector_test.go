// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for scatter-gather I/O (kXR_readv, kXR_writev), driven against
// the strict server in conformance_server_test.go. The happy paths assert on
// the bytes the server holds or sent; the fail-closed half drives it into the
// two ways a vector reply can lie about which range the data came from.

package xrootd

import (
	"bytes"
	"context"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
)

// TestConformance_ReadV_Ranges checks the whole point of a vector read: many
// disjoint ranges answered in one round trip, in the order asked for.
func TestConformance_ReadV_Ranges(t *testing.T) {
	segs := []xrdfs.ReadVSegment{
		{Offset: 0, Length: 16},
		{Offset: 4096, Length: 1000},
		{Offset: 100, Length: 1},
		{Offset: int64(len(confContent) - 8), Length: 8},
	}
	want := [][]byte{
		confContent[:16],
		confContent[4096:5096],
		confContent[100:101],
		confContent[len(confContent)-8:],
	}

	srv := confClient(t, confContent, func(srv *confServer, f *file) {
		got, err := f.ReadVAt(context.Background(), segs)
		if err != nil {
			t.Fatalf("ReadVAt: %v", err)
		}
		if len(got) != len(want) {
			t.Fatalf("got %d segments, want %d", len(got), len(want))
		}
		for i := range want {
			if !bytes.Equal(got[i], want[i]) {
				t.Fatalf("segment %d: the bytes differ from the file content at offset %d", i, segs[i].Offset)
			}
		}
	})
	srv.check(t)
	if got, want := srv.opCount(3025), 1; got != want {
		t.Fatalf("got %d kXR_readv requests, want %d: the whole vector is one round trip", got, want)
	}
}

// TestConformance_ReadV_ReassemblesAcrossFrames checks the reply is treated as
// a byte stream: an OkSoFar boundary may fall inside a segment header, so a
// client that decodes frame by frame loses the segment that straddles one.
func TestConformance_ReadV_ReassemblesAcrossFrames(t *testing.T) {
	srv := confClient(t, confContent, func(srv *confServer, f *file) {
		srv.set(func(srv *confServer) { srv.readChunk = 37 }) // co-prime with 16

		got, err := f.ReadVAt(context.Background(), []xrdfs.ReadVSegment{
			{Offset: 0, Length: 100},
			{Offset: 200, Length: 100},
			{Offset: 400, Length: 100},
		})
		if err != nil {
			t.Fatalf("ReadVAt: %v", err)
		}
		for i, off := range []int64{0, 200, 400} {
			if !bytes.Equal(got[i], confContent[off:off+100]) {
				t.Fatalf("segment %d was not reassembled correctly", i)
			}
		}
	})
	srv.check(t)
}

// TestConformance_WriteV_StoresEverySegment reads the bytes back out of the
// server: an "ok" status says the frame was accepted, not that the data landed
// where the descriptors said it should.
func TestConformance_WriteV_StoresEverySegment(t *testing.T) {
	want := append([]byte(nil), confContent...)
	copy(want[0:], "AAAA")
	copy(want[4096:], "BBBBBBBB")
	copy(want[9000:], "C")

	srv := confClient(t, confContent, func(srv *confServer, f *file) {
		err := f.WriteVAt(context.Background(), []xrdfs.WriteVSegment{
			{Offset: 0, Data: []byte("AAAA")},
			{Offset: 4096, Data: []byte("BBBBBBBB")},
			{Offset: 9000, Data: []byte("C")},
		})
		if err != nil {
			t.Fatalf("WriteVAt: %v", err)
		}
	})
	srv.check(t)
	if !bytes.Equal(srv.content(), want) {
		t.Fatal("the file the server holds differs from what the vector wrote")
	}
	if got, want := srv.opCount(3031), 1; got != want {
		t.Fatalf("got %d kXR_writev requests, want %d", got, want)
	}
}

// TestConformance_WriteV_RoundTripsThroughReadV writes a vector and reads the
// same ranges back, so the two codecs check each other rather than a constant.
func TestConformance_WriteV_RoundTripsThroughReadV(t *testing.T) {
	writes := []xrdfs.WriteVSegment{
		{Offset: 1, Data: []byte("one")},
		{Offset: 5000, Data: bytes.Repeat([]byte{0xa5}, 300)},
		{Offset: 12, Data: []byte{0x00}},
	}

	srv := confClient(t, confContent, func(srv *confServer, f *file) {
		ctx := context.Background()
		if err := f.WriteVAt(ctx, writes); err != nil {
			t.Fatalf("WriteVAt: %v", err)
		}

		reads := make([]xrdfs.ReadVSegment, len(writes))
		for i, w := range writes {
			reads[i] = xrdfs.ReadVSegment{Offset: w.Offset, Length: len(w.Data)}
		}
		got, err := f.ReadVAt(ctx, reads)
		if err != nil {
			t.Fatalf("ReadVAt: %v", err)
		}
		for i := range writes {
			if !bytes.Equal(got[i], writes[i].Data) {
				t.Fatalf("segment %d did not survive the round trip", i)
			}
		}
	})
	srv.check(t)
}

// TestConformance_ReadV_StoppedTransferIsRefused covers the one thing a vector
// reply cannot do partially. The reply carries no request-side index, so a
// missing segment shifts every later one onto the wrong range: the prefix is
// not usable and must not be returned.
func TestConformance_ReadV_StoppedTransferIsRefused(t *testing.T) {
	srv := confClient(t, confContent, func(srv *confServer, f *file) {
		srv.set(func(srv *confServer) { srv.readvDropLast = true })

		got, err := f.ReadVAt(context.Background(), []xrdfs.ReadVSegment{
			{Offset: 0, Length: 10},
			{Offset: 100, Length: 10},
		})
		wantErr(t, err, "ReadVAt", "asked for 2 segments and got 1 back")
		if got != nil {
			t.Fatal("a stopped transfer returned data")
		}
	})
	srv.check(t)
}

// TestConformance_ReadV_ShortSegmentIsRefused is the subtler half: every
// segment is present, but one is short. Returning it would hand the caller a
// buffer whose tail is missing with no way to notice.
func TestConformance_ReadV_ShortSegmentIsRefused(t *testing.T) {
	srv := confClient(t, confContent, func(srv *confServer, f *file) {
		srv.set(func(srv *confServer) { srv.readvShortSeg = true })

		_, err := f.ReadVAt(context.Background(), []xrdfs.ReadVSegment{
			{Offset: 0, Length: 10},
		})
		wantErr(t, err, "ReadVAt", "came back with 9 bytes, not the 10 asked for")
	})
	srv.check(t)
}

// TestConformance_Vector_BoundsAreCheckedBeforeTheWire checks the limits are
// enforced client-side: an over-long vector must not be assembled, let alone
// sent, and the server must see nothing.
func TestConformance_Vector_BoundsAreCheckedBeforeTheWire(t *testing.T) {
	srv := confClient(t, confContent, func(srv *confServer, f *file) {
		ctx := context.Background()
		readv := func(segs []xrdfs.ReadVSegment) error {
			_, err := f.ReadVAt(ctx, segs)
			return err
		}

		wantErr(t, readv(nil), "ReadVAt", "with no segments")
		wantErr(t, f.WriteVAt(ctx, nil), "WriteVAt", "with no segments")

		tooMany := make([]xrdfs.ReadVSegment, xrdproto.MaxVectorSegments+1)
		wantErr(t, readv(tooMany), "ReadVAt", "exceeds the 1024-segment limit")

		huge := []xrdfs.ReadVSegment{
			{Offset: 0, Length: xrdproto.MaxVectorBytes},
			{Offset: 0, Length: 1},
		}
		wantErr(t, readv(huge), "ReadVAt", "exceeds the 268435456-byte limit")

		wantErr(t, readv([]xrdfs.ReadVSegment{{Offset: 0, Length: -1}}), "ReadVAt", "invalid length")
	})
	srv.check(t)
	if got := srv.opSeq(); len(got) != 0 {
		t.Fatalf("the server saw %v; a request that fails its own bounds must not reach the wire", got)
	}
}
