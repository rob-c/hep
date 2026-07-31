// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseTPCBody(t *testing.T) {
	const markers = "Perf Marker\n" +
		"\tTimestamp: 1700000000\n" +
		"\tStripe Index: 0\n" +
		"\tStripe Bytes Transferred: 1048576\n" +
		"\tTotal Stripe Count: 1\n" +
		"End\n"

	for _, tc := range []struct {
		name    string
		body    string
		want    error // errors.Is target, or nil
		reason  string
		markers int
	}{
		{
			name:    "success after markers",
			body:    markers + markers + "success: Created\n",
			markers: 2,
		},
		{
			name: "bare success",
			body: "success\n",
		},
		{
			// The status line was 2xx and the transfer still failed. Reading
			// the body is the only way to know.
			name:   "failure is an error",
			body:   markers + "failure: unable to open destination\n",
			want:   new(TPCError),
			reason: "unable to open destination",

			markers: 1,
		},
		{
			// The connection dropped mid-copy. Neither outcome was announced,
			// so neither may be reported.
			name: "no outcome",
			body: markers,
			want: ErrTPCNoOutcome,

			markers: 1,
		},
		{
			name: "empty body",
			body: "",
			want: ErrTPCNoOutcome,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []TPCMarker
			err := parseTPCBody(strings.NewReader(tc.body), func(m TPCMarker) {
				got = append(got, m)
			})

			switch tc.want.(type) {
			case nil:
				if err != nil {
					t.Fatalf("got error %v, want success", err)
				}
			case *TPCError:
				var terr *TPCError
				if !errors.As(err, &terr) {
					t.Fatalf("got %v, want a *TPCError", err)
				}
				if terr.Reason != tc.reason {
					t.Fatalf("got reason %q, want %q", terr.Reason, tc.reason)
				}
			default:
				if !errors.Is(err, tc.want) {
					t.Fatalf("got %v, want %v", err, tc.want)
				}
			}

			if len(got) != tc.markers {
				t.Fatalf("got %d performance markers, want %d", len(got), tc.markers)
			}
			if len(got) == 0 {
				return
			}
			want := TPCMarker{
				Timestamp:        time.Unix(1700000000, 0),
				StripeIndex:      0,
				BytesTransferred: 1048576,
				TotalStripes:     1,
			}
			if got[0] != want {
				t.Fatalf("got marker %+v, want %+v", got[0], want)
			}
		})
	}
}

func TestTPCRequest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		push   bool
		hdr    string // the header naming the remote side
		remote string
	}{
		{name: "push", push: true, hdr: "Destination", remote: "https://dst.example.org/out"},
		{name: "pull", hdr: "Source", remote: "https://src.example.org/in"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got *http.Request
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Clone(context.Background())
				w.WriteHeader(http.StatusAccepted)
				w.Write([]byte("success: Created\n"))
			}))
			defer srv.Close()

			c, err := Dial(srv.URL, WithBearerToken("local-tok"), WithInsecureBearerToken())
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}

			opts := TPCOptions{
				RemoteToken:                 "remote-tok",
				Overwrite:                   true,
				RequireChecksumVerification: true,
			}
			if tc.push {
				err = c.Push(context.Background(), "/file", tc.remote, opts)
			} else {
				err = c.Pull(context.Background(), "/file", tc.remote, opts)
			}
			if err != nil {
				t.Fatalf("third-party copy: %v", err)
			}

			if got.Method != MethodCopy {
				t.Fatalf("got method %q, want %q", got.Method, MethodCopy)
			}
			for _, kv := range [][2]string{
				{tc.hdr, tc.remote},
				{"Overwrite", "T"},
				{"RequireChecksumVerification", "true"},
				// The remote credential travels under its own header; the
				// client's own token authenticates the active endpoint.
				{"TransferHeaderAuthorization", "Bearer remote-tok"},
				{"Credential", "none"},
				{"Authorization", "Bearer local-tok"},
			} {
				if v := got.Header.Get(kv[0]); v != kv[1] {
					t.Fatalf("header %s: got %q, want %q", kv[0], v, kv[1])
				}
			}
		})
	}
}

// TestTPCAcceptedThenFailed is the trap HTTP-TPC sets: the status line says the
// copy was accepted, the body says it failed.
func TestTPCAcceptedThenFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("failure: destination is read-only\n"))
	}))
	defer srv.Close()

	c, err := Dial(srv.URL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	err = c.Pull(context.Background(), "/file", "https://src.example.org/in", TPCOptions{})
	var terr *TPCError
	if !errors.As(err, &terr) {
		t.Fatalf("got %v, want a *TPCError", err)
	}
	if terr.Reason != "destination is read-only" {
		t.Fatalf("got reason %q", terr.Reason)
	}
}

func TestTPCRefusedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c, err := Dial(srv.URL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if err := c.Push(context.Background(), "/file", "https://dst.example.org/out", TPCOptions{}); err == nil {
		t.Fatal("a refused COPY was reported as a successful copy")
	}
}

// TestTPCNoTokenSendsNoTransferHeader guards against leaking a credential
// header the caller never asked for.
func TestTPCNoTokenSendsNoTransferHeader(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Write([]byte("success\n"))
	}))
	defer srv.Close()

	c, err := Dial(srv.URL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if err := c.Pull(context.Background(), "/file", "https://src.example.org/in", TPCOptions{}); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	for _, k := range []string{"TransferHeaderAuthorization", "Credential", "Authorization"} {
		if v := got.Get(k); v != "" {
			t.Fatalf("header %s was sent as %q by an unauthenticated client", k, v)
		}
	}
	if got.Get("Overwrite") != "F" {
		t.Fatalf("Overwrite: got %q, want %q", got.Get("Overwrite"), "F")
	}
}
