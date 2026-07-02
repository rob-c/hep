// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sss implements the XRootD "sss" (Simple Shared Secret) security
// provider: a self-contained credential encrypted with a pre-shared symmetric
// key from a keytab. See the XRootD XrdSecsss protocol.
package sss // import "go-hep.org/x/hep/xrootd/xrdproto/auth/sss"

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Key is one entry of an SSS keytab.
type Key struct {
	ID     int64  // wire key id (N:)
	Key    []byte // raw symmetric key bytes (k:)
	User   string // u:
	Group  string // g:
	Name   string // n:
	Expiry int64  // e: epoch seconds, 0 means never
}

// Expired reports whether the key is expired at now.
func (k Key) Expired(now time.Time) bool {
	return k.Expiry != 0 && now.Unix() >= k.Expiry
}

// ParseKeytab parses an SSS keytab. Each key line has the form
// "0 N:<id> k:<hexkey> u:<user> g:<group> n:<name> [e:<exp>]". Blank lines and
// lines beginning with '#' are ignored.
func ParseKeytab(r io.Reader) ([]Key, error) {
	var keys []Key
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		var k Key
		for _, f := range fields {
			tag, val, ok := strings.Cut(f, ":")
			if !ok {
				continue // the leading "0" flag field
			}
			switch tag {
			case "N":
				k.ID, _ = strconv.ParseInt(val, 10, 64)
			case "k":
				raw, err := hex.DecodeString(val)
				if err != nil {
					return nil, fmt.Errorf("auth/sss: bad hex key: %w", err)
				}
				k.Key = raw
			case "u":
				k.User = val
			case "g":
				k.Group = val
			case "n":
				k.Name = val
			case "e":
				k.Expiry, _ = strconv.ParseInt(val, 10, 64)
			}
		}
		if len(k.Key) == 0 {
			return nil, fmt.Errorf("auth/sss: keytab line without a key: %q", line)
		}
		keys = append(keys, k)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("auth/sss: could not read keytab: %w", err)
	}
	return keys, nil
}

// LoadKeytab reads the SSS keytab from $XrdSecSSSKT, then $XrdSecsssKT, then
// ~/.xrd/sss.keytab.
func LoadKeytab() ([]Key, error) {
	var paths []string
	for _, env := range []string{"XrdSecSSSKT", "XrdSecsssKT"} {
		if p := os.Getenv(env); p != "" {
			paths = append(paths, p)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".xrd", "sss.keytab"))
	}
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		keys, err := ParseKeytab(f)
		f.Close()
		if err != nil {
			return nil, err
		}
		return keys, nil
	}
	return nil, fmt.Errorf("auth/sss: no keytab found")
}

// FirstLiveKey returns the first non-expired key.
func FirstLiveKey(keys []Key, now time.Time) (Key, error) {
	for _, k := range keys {
		if !k.Expired(now) {
			return k, nil
		}
	}
	return Key{}, fmt.Errorf("auth/sss: no live key in keytab")
}
