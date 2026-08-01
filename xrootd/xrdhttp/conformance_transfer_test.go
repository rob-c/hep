// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the transfer itself: what the client does with the answers a
// real HTTP endpoint gives it. The WebDAV servers in this ecosystem are not a
// single implementation — dCache, EOS, XRootD's own HTTP layer and a plain
// nginx in front of a filesystem all answer differently, and the ones that
// answer badly do it silently. A read that returns the wrong bytes without an
// error is worse than a read that fails.

package xrdhttp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

var transferContent = []byte("0123456789abcdefghijklmnopqrstuvwxyz")

// TestConformance_AServerThatIgnoresRangesStillGivesTheRightBytes is the case
// that cannot be caught by looking at an error: a server with no range support
// answers 200 with the whole object, and a client that reads the first len(p)
// bytes of that body hands back the beginning of the file for every offset.
// Every byte after the first chunk is then quietly wrong.
func TestConformance_AServerThatIgnoresRangesStillGivesTheRightBytes(t *testing.T) {
	ctx := context.Background()

	for _, srvKind := range []struct {
		name    string
		handler func(w http.ResponseWriter, r *http.Request)
	}{
		{
			name: "a server with no range support",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", strconv.Itoa(len(transferContent)))
				w.WriteHeader(http.StatusOK)
				w.Write(transferContent)
			},
		},
		{
			name: "a server that answers the range",
			handler: func(w http.ResponseWriter, r *http.Request) {
				first, last, ok := parseRangeHeader(t, r.Header.Get("Range"))
				if !ok {
					t.Errorf("the client sent no byte range: %q", r.Header.Get("Range"))
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if last >= len(transferContent) {
					last = len(transferContent) - 1
				}
				if first > last {
					w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
					return
				}
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", first, last, len(transferContent)))
				w.WriteHeader(http.StatusPartialContent)
				w.Write(transferContent[first : last+1])
			},
		},
		{
			// Some servers answer a range with 200 but send only the range,
			// declaring it in Content-Range. Trusting the header rather than
			// the status is what keeps both of those servers readable.
			name: "a server that sends a range under a 200",
			handler: func(w http.ResponseWriter, r *http.Request) {
				first, last, ok := parseRangeHeader(t, r.Header.Get("Range"))
				if !ok || first >= len(transferContent) {
					w.WriteHeader(http.StatusOK)
					return
				}
				if last >= len(transferContent) {
					last = len(transferContent) - 1
				}
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", first, last, len(transferContent)))
				w.WriteHeader(http.StatusOK)
				w.Write(transferContent[first : last+1])
			},
		},
	} {
		t.Run(srvKind.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(srvKind.handler))
			defer srv.Close()

			c, err := Dial(srv.URL, Unbounded())
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}

			for _, off := range []int64{0, 1, 7, 15, int64(len(transferContent)) - 4} {
				p := make([]byte, 4)
				n, err := c.ReadAt(ctx, p, "/f.bin", off)
				if err != nil {
					t.Fatalf("ReadAt(%d): %v", off, err)
				}
				if want := transferContent[off : off+4]; !bytes.Equal(p[:n], want) {
					t.Errorf("ReadAt(%d) is %q, want %q", off, p[:n], want)
				}
			}

			// The end of the object behaves the same way whichever server it
			// is: a straddling read is short with io.EOF, and one that starts
			// past the end reads nothing.
			p := make([]byte, 8)
			n, err := c.ReadAt(ctx, p, "/f.bin", int64(len(transferContent))-3)
			if n != 3 || err != io.EOF {
				t.Errorf("a straddling read returned (%d, %v), want (3, io.EOF)", n, err)
			}
			if n, err := c.ReadAt(ctx, p, "/f.bin", int64(len(transferContent))+10); n != 0 || err != io.EOF {
				t.Errorf("a read past the end returned (%d, %v), want (0, io.EOF)", n, err)
			}
		})
	}
}

func parseRangeHeader(t *testing.T, hdr string) (first, last int, ok bool) {
	t.Helper()
	spec, found := strings.CutPrefix(hdr, "bytes=")
	if !found {
		return 0, 0, false
	}
	lo, hi, found := strings.Cut(spec, "-")
	if !found {
		return 0, 0, false
	}
	first, err := strconv.Atoi(lo)
	if err != nil {
		return 0, 0, false
	}
	last, err = strconv.Atoi(hi)
	if err != nil {
		return 0, 0, false
	}
	return first, last, true
}

// TestConformance_AnEmptyReadAsksTheServerNothing pins the degenerate case: a
// zero-length ReadAt is a no-op, not a request for "bytes=0--1", which a strict
// server answers with 416 and a lenient one with the whole file.
func TestConformance_AnEmptyReadAsksTheServerNothing(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Write(transferContent)
	}))
	defer srv.Close()

	c, err := Dial(srv.URL, Unbounded())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	n, err := c.ReadAt(context.Background(), nil, "/f.bin", 4)
	if n != 0 || err != nil {
		t.Errorf("an empty read returned (%d, %v), want (0, nil)", n, err)
	}
	if requests != 0 {
		t.Errorf("an empty read made %d requests, want none", requests)
	}
}

// TestConformance_AWriteDeclaresItsLengthWhenItKnowsIt covers the other
// direction. A PUT with a known size must declare Content-Length: an XRootD
// HTTP endpoint uses it to reserve the file and to detect a truncated upload,
// and a chunked body deprives it of both. When the size is genuinely unknown,
// chunked is the only honest encoding — declaring a wrong length is worse.
func TestConformance_AWriteDeclaresItsLengthWhenItKnowsIt(t *testing.T) {
	type upload struct {
		length   int64
		chunked  bool
		received []byte
	}
	var (
		mu   sync.Mutex
		last upload
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		last = upload{length: r.ContentLength, received: body}
		for _, te := range r.TransferEncoding {
			if te == "chunked" {
				last.chunked = true
			}
		}
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c, err := Dial(srv.URL, Unbounded())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	ctx := context.Background()

	t.Run("a known size is declared", func(t *testing.T) {
		if err := c.Create(ctx, "/f.bin", bytes.NewReader(transferContent), int64(len(transferContent))); err != nil {
			t.Fatalf("Create: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		if last.chunked {
			t.Error("a write of known size was sent chunked")
		}
		if last.length != int64(len(transferContent)) {
			t.Errorf("the server was told %d bytes, want %d", last.length, len(transferContent))
		}
		if !bytes.Equal(last.received, transferContent) {
			t.Errorf("the server received %q, want %q", last.received, transferContent)
		}
	})

	t.Run("an unknown size streams", func(t *testing.T) {
		// A reader whose length the caller cannot know: exactly the case
		// size = -1 exists for.
		src := io.MultiReader(bytes.NewReader(transferContent[:10]), bytes.NewReader(transferContent[10:]))
		if err := c.Create(ctx, "/g.bin", src, -1); err != nil {
			t.Fatalf("Create: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		if !bytes.Equal(last.received, transferContent) {
			t.Errorf("the server received %q, want %q", last.received, transferContent)
		}
		if last.length >= 0 && !last.chunked {
			t.Errorf("a write of unknown size declared %d bytes", last.length)
		}
	})

	t.Run("an empty file is still a request", func(t *testing.T) {
		if err := c.Create(ctx, "/empty.bin", strings.NewReader(""), 0); err != nil {
			t.Fatalf("Create: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(last.received) != 0 {
			t.Errorf("the server received %q, want nothing", last.received)
		}
	})
}

// TestConformance_ARedirectIsFollowedAndBounded covers what every real
// deployment does: a manager answers with a redirect to the node holding the
// data. Following it is the whole point; following it forever is a hang, and a
// redirect that carries the caller's bearer token to a host it was not issued
// for is a credential leak.
func TestConformance_ARedirectIsFollowedAndBounded(t *testing.T) {
	ctx := context.Background()

	t.Run("a redirect to the data node is followed", func(t *testing.T) {
		data := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/store/f.bin" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Write(transferContent)
		}))
		defer data.Close()

		mgr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, data.URL+"/store/f.bin", http.StatusFound)
		}))
		defer mgr.Close()

		c, err := Dial(mgr.URL, Unbounded())
		if err != nil {
			t.Fatalf("Dial: %v", err)
		}
		got, err := c.ReadAll(ctx, "/f.bin")
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if !bytes.Equal(got, transferContent) {
			t.Errorf("read %q through the redirect, want %q", got, transferContent)
		}
	})

	t.Run("a redirect loop ends in an error", func(t *testing.T) {
		// A relative Location resolves against the host that sent it, which is
		// how a manager pointing at itself produces an endless loop.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/again", http.StatusFound)
		}))
		defer srv.Close()

		// Unbounded: the loop is what is under test, not the retry schedule
		// that would otherwise walk into it five times over.
		c, err := Dial(srv.URL, Unbounded())
		if err != nil {
			t.Fatalf("Dial: %v", err)
		}
		if _, err := c.ReadAll(ctx, "/f.bin"); err == nil {
			t.Error("a redirect loop returned no error; the client is following it without a budget")
		}
	})

	t.Run("a bearer token does not follow a redirect off its host", func(t *testing.T) {
		// The two servers must differ by host, not only by port, for the
		// question to mean anything — a credential is scoped to a host. Both
		// addresses are loopback, so no name resolution is involved.
		ln, err := net.Listen("tcp", "127.0.0.2:0")
		if err != nil {
			t.Skipf("no second loopback address available: %v", err)
		}

		var elsewhere struct {
			sync.Mutex
			auth string
		}
		other := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			elsewhere.Lock()
			elsewhere.auth = r.Header.Get("Authorization")
			elsewhere.Unlock()
			w.Write(transferContent)
		}))
		other.Listener.Close()
		other.Listener = ln
		other.Start()
		defer other.Close()

		target := other.URL + "/f.bin"
		mgr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target, http.StatusFound)
		}))
		defer mgr.Close()

		c, err := Dial(mgr.URL, Unbounded(), WithBearerToken("s3cr3t"), WithInsecureBearerToken())
		if err != nil {
			t.Fatalf("Dial: %v", err)
		}
		if _, err := c.ReadAll(ctx, "/f.bin"); err != nil {
			t.Fatalf("ReadAll: %v", err)
		}

		elsewhere.Lock()
		defer elsewhere.Unlock()
		if strings.Contains(elsewhere.auth, "s3cr3t") {
			t.Errorf("the token was presented to another host: %q", elsewhere.auth)
		}
	})
}

// TestConformance_AnErrorStatusIsReportedNotReturnedAsData checks the statuses
// a caller has to be able to act on. A 404 read that returned an empty slice
// and no error would look like an empty file, and a 403 upload that returned
// nil would look like a successful one.
func TestConformance_AnErrorStatusIsReportedNotReturnedAsData(t *testing.T) {
	ctx := context.Background()

	for _, status := range []int{
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusInternalServerError,
		http.StatusServiceUnavailable,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				io.WriteString(w, "the body of an error is not the file\n")
			}))
			defer srv.Close()

			c, err := Dial(srv.URL, Unbounded())
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}

			if got, err := c.ReadAll(ctx, "/f.bin"); err == nil {
				t.Errorf("a %d read returned %q and no error", status, got)
			}
			if _, err := c.ReadAt(ctx, make([]byte, 4), "/f.bin", 0); err == nil {
				t.Errorf("a %d ranged read returned no error", status)
			}
			if err := c.Create(ctx, "/f.bin", strings.NewReader("x"), 1); err == nil {
				t.Errorf("a %d upload returned no error", status)
			}
			// A HEAD is the one place 404 is an answer rather than a failure:
			// it is how the client asks whether a path exists.
			fi, err := c.Stat(ctx, "/f.bin")
			switch status {
			case http.StatusNotFound:
				if err != nil || fi.Exists {
					t.Errorf("a 404 HEAD returned (%+v, %v), want a non-existent file and no error", fi, err)
				}
			default:
				if err == nil {
					t.Errorf("a %d HEAD returned no error", status)
				}
			}
			// A DELETE of something that is not there is a success: the
			// postcondition the caller asked for already holds.
			switch err := c.Remove(ctx, "/f.bin"); status {
			case http.StatusNotFound:
				if err != nil {
					t.Errorf("deleting a missing path failed: %v", err)
				}
			default:
				if err == nil {
					t.Errorf("a %d delete returned no error", status)
				}
			}
		})
	}
}
