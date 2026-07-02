// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sss

import (
	"strings"
	"testing"
	"time"
)

func TestParseKeytab(t *testing.T) {
	in := `# comment
0 N:1 k:0011223344 u:alice g:atlas n:mykey

0 N:2 k:aabbccdd u:bob g:cms n:other e:100
`
	keys, err := ParseKeytab(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseKeytab: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("got %d keys, want 2", len(keys))
	}
	if keys[0].ID != 1 || keys[0].User != "alice" || keys[0].Group != "atlas" || keys[0].Name != "mykey" {
		t.Fatalf("key0 fields: %+v", keys[0])
	}
	if want := []byte{0x00, 0x11, 0x22, 0x33, 0x44}; string(keys[0].Key) != string(want) {
		t.Fatalf("key0 bytes: % x", keys[0].Key)
	}
	if keys[1].Expiry != 100 {
		t.Fatalf("key1 expiry: got=%d want=100", keys[1].Expiry)
	}
}

func TestFirstLiveKey(t *testing.T) {
	now := time.Unix(500, 0)
	keys := []Key{
		{ID: 1, Expiry: 100}, // expired
		{ID: 2, Expiry: 0},   // never expires
	}
	k, err := FirstLiveKey(keys, now)
	if err != nil {
		t.Fatalf("FirstLiveKey: %v", err)
	}
	if k.ID != 2 {
		t.Fatalf("got key %d, want 2", k.ID)
	}
	if _, err := FirstLiveKey(keys[:1], now); err == nil {
		t.Fatal("expected error when all keys expired")
	}
}
