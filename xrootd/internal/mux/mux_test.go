// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mux // import "go-hep.org/x/hep/xrootd/internal/mux"

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdproto"
)

func TestMux_Claim(t *testing.T) {
	m := New()
	defer m.Close()
	claimedIds := map[xrdproto.StreamID]bool{}

	for range streamIDPoolSize {
		id, channel, err := m.Claim()

		if err != nil {
			t.Fatalf("could not Claim streamID: %v", err)
		}

		if channel == nil {
			t.Fatal("channel should not be nil")
		}

		if claimedIds[id] {
			t.Fatalf("should not claim id %s, which was already claimed", id)
		}

		claimedIds[id] = true
	}

	for id := range claimedIds {
		m.Unclaim(id)
	}
}

func TestMux_Claim_AfterUnclaim(t *testing.T) {
	m := New()
	defer m.Close()
	claimedIds := map[xrdproto.StreamID]bool{}

	for range streamIDPoolSize {
		id, _, _ := m.Claim()
		claimedIds[id] = true
	}

	wantID := xrdproto.StreamID{13, 14}
	m.Unclaim(wantID)

	gotID, channel, err := m.Claim()

	if err != nil {
		t.Fatalf("could not Claim streamID: %v", err)
	}

	if channel == nil {
		t.Fatal("channel should not be nil")
	}

	if !reflect.DeepEqual(gotID, wantID) {
		t.Fatalf("invalid claim\ngot = %v\nwant = %v", gotID, wantID)
	}

	for id := range claimedIds {
		m.Unclaim(id)
	}
}

func TestMux_ClaimWithID_WhenIDIsFree(t *testing.T) {
	m := New()
	defer m.Close()

	streamID := xrdproto.StreamID{13, 14}
	channel, err := m.ClaimWithID(streamID)

	if err != nil {
		t.Fatalf("could not Claim streamID: %v", err)
	}

	if channel == nil {
		t.Fatal("channel should not be nil")
	}

	m.Unclaim(streamID)
}

func TestMux_ClaimWithID_WhenIDIsTakenByClaimWithID(t *testing.T) {
	m := New()
	defer m.Close()
	streamID := xrdproto.StreamID{13, 14}
	_, err := m.ClaimWithID(streamID)
	if err != nil {
		t.Fatalf("could not claim stream %v: %+v", streamID, err)
	}

	_, err = m.ClaimWithID(streamID)
	if err == nil {
		t.Fatal("should not be able to ClaimWithID when that id is already claimed")
	}

	m.Unclaim(streamID)
}

func TestMux_ClaimWithID_WhenIDIsTakenByClaim(t *testing.T) {
	m := New()
	defer m.Close()
	id, _, _ := m.Claim()

	_, err := m.ClaimWithID(id)

	if err == nil {
		t.Fatal("should not be able to ClaimWithID when that id is already claimed")
	}

	m.Unclaim(id)
}

func TestMux_Claim_WhenIDIsTakenByClaimWithID(t *testing.T) {
	m := New()
	defer m.Close()
	takenID := xrdproto.StreamID{0, 0}
	_, err := m.ClaimWithID(takenID)
	if err != nil {
		t.Fatalf("could not claim stream %v: %+v", takenID, err)
	}

	id, channel, err := m.Claim()

	if err != nil {
		t.Fatalf("could not Claim streamID: %v", err)
	}

	if channel == nil {
		t.Fatal("channel should not be nil")
	}

	if reflect.DeepEqual(id, takenID) {
		t.Fatalf("invalid claim: id %v was already taken", takenID)
	}

	m.Unclaim(takenID)
	m.Unclaim(id)
}

func TestMux_SendData_WhenIDIsTaken(t *testing.T) {
	m := New()
	defer m.Close()
	takenID := xrdproto.StreamID{0, 0}
	want := ServerResponse{}
	var got ServerResponse

	errch := make(chan error)
	channel, _ := m.ClaimWithID(takenID)
	go func() {
		err := m.SendData(takenID, want)
		if err != nil {
			errch <- fmt.Errorf("could not SendData: %w", err)
		}
	}()

	select {
	case got = <-channel:
	case err := <-errch:
		if err != nil {
			t.Fatalf("error: %+v", err)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("invalid data\ngot = %v\nwant = %v", got, want)
	}

	m.Unclaim(takenID)
}

func TestMux_SendData_WhenIDIsNotTaken(t *testing.T) {
	m := New()
	defer m.Close()
	notTakenID := xrdproto.StreamID{0, 0}

	err := m.SendData(notTakenID, ServerResponse{})

	if err == nil {
		t.Fatal("should not be able to SenData when id is unclaimed")
	}
}

func TestMux_Close_WhenAlreadyClosed(t *testing.T) {
	m := New()
	m.Close()
	m.Close()
}

func TestMux_Unclaim_WhenNotClaimed(t *testing.T) {
	m := New()
	defer m.Close()
	m.Unclaim(xrdproto.StreamID{0, 0})
}

func TestMux_Claim_WhenClosed(t *testing.T) {
	m := New()
	m.Close()
	_, _, err := m.Claim()
	if err == nil {
		t.Fatal("should not be able to Claim when mux is closed")
	}
}

func TestMux_ClaimWithID_WhenClosed(t *testing.T) {
	m := New()
	m.Close()
	_, err := m.ClaimWithID(xrdproto.StreamID{0, 0})
	if err == nil {
		t.Fatal("should not be able to ClaimWithID when mux is closed")
	}
}

func BenchmarkMux_Claim(b *testing.B) {
	m := New()
	defer m.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id, _, err := m.Claim()
		if err != nil {
			b.Error(err)
		}
		m.Unclaim(id)
	}
}

func BenchmarkMux_SendData(b *testing.B) {
	m := New()
	defer m.Close()
	id, ch, _ := m.Claim()
	done := make(chan struct{})
	response := ServerResponse{Data: []byte{0, 1, 2, 3, 4, 5}}

	go func() {
		for {
			select {
			case <-ch:
			case <-done:
				return
			}
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := m.SendData(id, response)
		if err != nil {
			b.Error(err)
		}
	}

	m.Unclaim(id)
	close(done)
}

// TestUnclaimWithDeliveryInFlight covers the case a caller creates by giving up
// on a request — a context deadline, a response the client refused — while the
// reader goroutine is still delivering a frame for that stream. Unclaim must
// wait for the delivery to leave the select before closing the channel;
// closing it underneath the sender panics with "send on closed channel".
func TestUnclaimWithDeliveryInFlight(t *testing.T) {
	m := New()
	defer m.Close()

	for range 200 {
		id, _, err := m.Claim()
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}

		// Nobody ever reads this channel, so the send parks. Whether SendData
		// finds the waiter at all depends on which side wins the race, and both
		// outcomes are fine; what must not happen is a panic or a hang.
		started, sent := make(chan struct{}), make(chan struct{})
		go func() {
			defer close(sent)
			close(started)
			_ = m.SendData(id, ServerResponse{Data: []byte{1, 2, 3}})
		}()

		<-started
		m.Unclaim(id)
		<-sent
	}
}

// TestCloseWithDeliveryInFlight is the same hazard reached through Close,
// which unclaims every outstanding stream at once.
func TestCloseWithDeliveryInFlight(t *testing.T) {
	m := New()

	var ids []xrdproto.StreamID
	for range 16 {
		id, _, err := m.Claim()
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		ids = append(ids, id)
	}

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.SendData(id, ServerResponse{Data: []byte{1}})
		}()
	}

	m.Close()
	wg.Wait()
}

// TestParseRedirection pins the redirect body format: a 4-byte port followed by
// the host, with the opaque data and the login token appended after '?'.
func TestParseRedirection(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  []byte
		want Redirection
	}{
		{
			name: "host and port",
			raw:  append([]byte{0, 0, 0x04, 0x46}, "example.org"...),
			want: Redirection{Addr: "example.org:1094"},
		},
		{
			name: "with opaque data",
			raw:  append([]byte{0, 0, 0x04, 0x46}, "example.org?xrd.k=v"...),
			want: Redirection{Addr: "example.org:1094", Opaque: "xrd.k=v"},
		},
		{
			name: "with opaque data and token",
			raw:  append([]byte{0, 0, 0x04, 0x46}, "example.org?xrd.k=v?tok"...),
			want: Redirection{Addr: "example.org:1094", Opaque: "xrd.k=v", Token: "tok"},
		},
		{
			name: "no host",
			raw:  []byte{0, 0, 0x04, 0x46},
			want: Redirection{Addr: ":1094"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRedirection(tc.raw)
			if err != nil {
				t.Fatalf("could not parse a well-formed redirect: %v", err)
			}
			if *got != tc.want {
				t.Fatalf("redirect is %+v, want %+v", *got, tc.want)
			}
		})
	}
}

// TestParseRedirectionRefusesShortBodies: a redirect is answered by connecting
// to whatever it names, and it is parsed on the connection-reading goroutine
// before the peer has authenticated itself. A body too short to hold the port
// must be refused rather than read past.
func TestParseRedirectionRefusesShortBodies(t *testing.T) {
	for n := range 4 {
		got, err := ParseRedirection(make([]byte, n))
		if err == nil {
			t.Errorf("a %d-byte redirect body was parsed into %+v", n, got)
		}
	}
}
