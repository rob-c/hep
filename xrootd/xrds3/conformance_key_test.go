// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for object keys that cannot become a URL.
//
// The key is pasted into the endpoint URL, and S3 keys are far more permissive
// than URLs: they are arbitrary byte strings, so a name a user typed or a
// dataset catalogue produced can contain a percent sign that is not an escape.
// Every entry point builds its request from that string, and the failure has to
// come back from the one that was called — a client that ignored the error and
// went on with a nil request would panic inside net/http, several frames away
// from the key that caused it.

package xrds3

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/s3cred"
)

func TestConformance_AKeyThatCannotBeAURLFailsOnEveryOperation(t *testing.T) {
	// A stray percent sign: not a valid escape sequence, and therefore not a
	// valid URL.
	const key = "datasets/run%zz/file.root"

	c := New("https://s3.example.org", "hep", s3cred.Credentials{
		AccessKey: "AKIAIOSFODNN7EXAMPLE",
		Secret:    "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
	})
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"stat", func() error {
			_, _, err := c.Stat(ctx, key)
			return err
		}},
		{"read", func() error {
			_, err := c.ReadAll(ctx, key)
			return err
		}},
		{"read at an offset", func() error {
			_, err := c.ReadAt(ctx, make([]byte, 4), key, 0)
			return err
		}},
		{"put", func() error {
			return c.Put(ctx, key, bytes.NewReader([]byte("go-hep")), 6)
		}},
		{"remove", func() error {
			return c.Remove(ctx, key)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatalf("%q was accepted as an object key", key)
			}
			if !strings.Contains(err.Error(), "invalid URL escape") {
				t.Fatalf("the failure says %q, want it to name the malformed escape", err)
			}
		})
	}
}

func TestConformance_AnEmptyReadIsNotARequest(t *testing.T) {
	// ReadAt with nowhere to put the bytes must not put a range request on the
	// wire: "bytes=0--1" is not a range, and a server is entitled to answer it
	// with the whole object.
	c := New("https://s3.invalid", "hep", s3cred.Credentials{AccessKey: "a", Secret: "b"})

	n, err := c.ReadAt(context.Background(), nil, "file.root", 0)
	if err != nil {
		t.Fatalf("an empty read failed: %v", err)
	}
	if n != 0 {
		t.Fatalf("an empty read reports %d bytes", n)
	}
}
