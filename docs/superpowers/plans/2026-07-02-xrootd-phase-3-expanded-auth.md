# XRootD Phase 3 — Expanded Auth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Add three single-round authentication/credential mechanisms to go-hep's XRootD client — WLCG bearer tokens (`ztn`), Simple Shared Secret (`sss`), and S3 credential discovery (for the Phase 4 S3 backend) — each byte-compatible with the reference `libxrdc` client.

**Architecture:** `ztn` and `sss` are new `auth.Auther` implementations under `xrdproto/auth/`, following the exact pattern of the existing `unix`/`host`/`krb5` providers: a `Provider()` name, a 4-byte credtype, and a `Request(params)` that returns the `kXR_auth` payload. Both are single-round (no `kXR_authmore` continuation), so they need no changes to the session read loop. S3 credentials are not an XRootD wire protocol — they are credential *sourcing* (access key / secret discovery) that the Phase 4 S3 backend will consume, so they live in a small standalone package with a discovery-precedence resolver.

**Tech Stack:** Go 1.24+; standard library plus the already-present `golang.org/x/crypto/blowfish` (for SSS Blowfish-CFB64) — go.mod already requires `golang.org/x/crypto v0.43.0` and `github.com/jcmturner/gokrb5/v8` (precedent for third-party auth deps). No new dependencies.

## Global Constraints

- Module `go-hep.org/x/hep`; Go floor `go 1.24.0`; no *new* third-party dependencies (blowfish is already vendored via `golang.org/x/crypto`); pure Go, no cgo/OpenSSL.
- New files start with the `Copyright ©2026 The go-hep Authors` BSD license header (3 lines, matching existing files).
- Every exported identifier gets a doc comment starting with its name; comments state protocol constraints, not narration.
- Errors wrapped with `%w` and the package's `auth/<name>:` prefix (matching `auth/krb5:` convention).
- The `auth.Auther` interface (unchanged): `Provider() string` and `Request(params []string) (*auth.Request, error)`. An `auth.Request` has `Type [4]byte` (the credtype) and `Credentials string` (the raw payload, which may contain NUL/binary bytes — `krb5` already relies on this).
- Exact wire facts (verified in `/home/rcurrie/HEP-x/nginx-xrootd/client/lib/sec/*` and `src/protocol/sss.h`, cross-checked against XrdSecsss / XrdSecztn):
  - **ztn**: credtype `{'z','t','n',0}`; payload = `"ztn\0"` + JWT (server skips the 4-byte tag and strips trailing whitespace/NULs). Discovery order: `$BEARER_TOKEN` (literal token), `$BEARER_TOKEN_FILE`, `$XDG_RUNTIME_DIR/bt_u<uid>`, `/tmp/bt_u<uid>`. Single round.
  - **sss**: credtype `{'s','s','s',0}`; payload is a 16-byte outer header + Blowfish-CFB64-encrypted body. Outer header (16 B): `'s','s','s',0` | version=1 | spare=0 | kn_size=0 | enc=`'0'` | key_id (8 B big-endian). Encrypted body = BF-CFB64( cleartext + IEEE-CRC32-BE ), where cleartext = 40-byte data header [32-byte random nonce | gen_time uint32 BE at [32:36] | zero [36:39] | opt=`0x00` USEDATA at [39]] + NAME TLV [`0x01` | `0x00` | len(uint8, includes NUL) | username bytes + NUL]. `gen_time = uint32(now - 1222183880)`. IEEE CRC-32 (poly 0xedb88320, `hash/crc32.ChecksumIEEE`) over the cleartext only, appended big-endian, then the whole (cleartext+crc) is BF-CFB64-encrypted with an all-zero 8-byte IV, padding off, variable key length. Single round (USEDATA self-contained). Keytab line format: `0 N:<id> k:<hexkey> u:<user> g:<group> n:<name> [e:<exp>]`; keytab discovery: `$XrdSecSSSKT`, `$XrdSecsssKT`, `~/.xrd/sss.keytab`; use the first non-expired key.
  - **s3 credentials**: discovery precedence (highest first): explicit access+secret pair passed in; `$AWS_ACCESS_KEY_ID`+`$AWS_SECRET_ACCESS_KEY`; `~/.aws/credentials` `[default]` section (`aws_access_key_id`/`aws_secret_access_key`). Both access key AND secret required; a partial pair is "not available". Static keys, no expiry.
- After each task: `gofmt -l xrootd/` empty, `go vet ./xrootd/...` clean, `go test ./xrootd/...` green. Go toolchain at `~/.local/share/go/bin` (add to PATH: `export PATH=~/.local/share/go/bin:$PATH`).
- Dual-oracle bar: unit tests per task; a gated interop test against a real XRootD server plus a runbook naming the `libxrdc` cross-checks (`client/bin/xrdcp` with `XrdSecPROTOCOL=ztn`/`sss`).

## Deferred to Phase 3b (its own spec + plan): GSI / X.509 proxy

GSI is deliberately **out of scope for this plan**. It is a multi-round
(`kXR_authmore`) challenge/response: round 1 `kXGC_certreq` → server `kXGS_cert`;
round 2 client sends a Diffie-Hellman public key, a selected cipher, and an
AES-256-CBC-wrapped inner buffer carrying the X.509 proxy PEM, keyed by
`SHA256(DH shared secret)`. Reproducing it in pure Go requires: (a) adding
`kXR_authmore` (4007→**4002**) continuation handling to the session read loop and
a multi-round `Auther` interface, and (b) reverse-engineering the exact
bucket-TLV wire format from `/home/rcurrie/HEP-x/nginx-xrootd/src/gsi/*` and
`src/protocol/gsi.h` — a body of wire detail that cannot be responsibly specified
from the C headers' summaries alone. It is a subsystem in its own right and gets a
dedicated spec/plan (`brainstorming` → `writing-plans`) that begins by reading
`src/protocol/gsi.h` and `src/gsi/gsi_core.*`.

---

### Task 1: `ztn` bearer-token auth module

**Files:**
- Create: `xrootd/xrdproto/auth/token/token.go`
- Test: `xrootd/xrdproto/auth/token/token_test.go`

**Interfaces:**
- Consumes: `auth.Auther`, `auth.Request` (existing).
- Produces (package `token`):
  - `var Type = [4]byte{'z', 't', 'n', 0}`.
  - `type Auth struct { Token string }` implementing `auth.Auther`: `Provider() string` returns `"ztn"`; `Request(params []string) (*auth.Request, error)` returns `&auth.Request{Type: Type, Credentials: "ztn\x00" + a.Token}` (error if `Token` is empty).
  - `func Discover() (string, error)` — resolves a token via `$BEARER_TOKEN`, `$BEARER_TOKEN_FILE`, `$XDG_RUNTIME_DIR/bt_u<uid>`, `/tmp/bt_u<uid>` (in that order), trimming trailing whitespace/NULs; error if none found.
  - `var Default auth.Auther` — set by `init()` to `&Auth{Token: t}` when `Discover()` succeeds, else nil (mirrors `krb5.Default`).

- [x] **Step 1: Write the failing test**

`xrootd/xrdproto/auth/token/token_test.go`:

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package token_test

import (
	"os"
	"path/filepath"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdproto/auth/token"
)

func TestProviderAndRequest(t *testing.T) {
	a := token.Auth{Token: "header.payload.sig"}
	if got, want := a.Provider(), "ztn"; got != want {
		t.Fatalf("provider: got=%q want=%q", got, want)
	}
	req, err := a.Request(nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if req.Type != token.Type {
		t.Fatalf("type: got=%v want=%v", req.Type, token.Type)
	}
	if want := "ztn\x00header.payload.sig"; req.Credentials != want {
		t.Fatalf("credentials: got=%q want=%q", req.Credentials, want)
	}
}

func TestEmptyTokenErrors(t *testing.T) {
	a := token.Auth{}
	if _, err := a.Request(nil); err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestDiscoverBearerToken(t *testing.T) {
	t.Setenv("BEARER_TOKEN", "  tok-from-env\n")
	t.Setenv("BEARER_TOKEN_FILE", "")
	got, err := token.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got != "tok-from-env" {
		t.Fatalf("discover env: got=%q want=%q", got, "tok-from-env")
	}
}

func TestDiscoverBearerTokenFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tok")
	if err := os.WriteFile(p, []byte("tok-from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEARER_TOKEN", "")
	t.Setenv("BEARER_TOKEN_FILE", p)
	got, err := token.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got != "tok-from-file" {
		t.Fatalf("discover file: got=%q want=%q", got, "tok-from-file")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./xrootd/xrdproto/auth/token/ -v`
Expected: FAIL (package does not exist).

- [x] **Step 3: Implement**

`xrootd/xrdproto/auth/token/token.go`:

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package token implements the XRootD "ztn" security provider: a WLCG bearer
// token (JWT) sent as the kXR_auth payload in a single round. See the XRootD
// XrdSecztn protocol.
package token // import "go-hep.org/x/hep/xrootd/xrdproto/auth/token"

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"go-hep.org/x/hep/xrootd/xrdproto/auth"
)

// Type is the 4-byte credential type for the ztn provider.
var Type = [4]byte{'z', 't', 'n', 0}

// Default is the token provider discovered from the environment, or nil when
// no bearer token could be found.
var Default auth.Auther

func init() {
	if tok, err := Discover(); err == nil {
		Default = &Auth{Token: tok}
	}
}

// Auth is the "ztn" security provider carrying a bearer token.
type Auth struct {
	Token string // the JWT to present
}

// Provider returns the name of the security provider.
func (*Auth) Provider() string { return "ztn" }

// Request forms a kXR_auth request carrying the bearer token. The payload is
// the 4-byte "ztn\0" tag followed by the raw token.
func (a *Auth) Request(params []string) (*auth.Request, error) {
	if a.Token == "" {
		return nil, fmt.Errorf("auth/token: empty bearer token")
	}
	return &auth.Request{Type: Type, Credentials: "ztn\x00" + a.Token}, nil
}

// Discover locates a bearer token, trying $BEARER_TOKEN (a literal token),
// $BEARER_TOKEN_FILE, $XDG_RUNTIME_DIR/bt_u<uid> and /tmp/bt_u<uid> in order.
// Trailing whitespace and NUL bytes are trimmed.
func Discover() (string, error) {
	if v := strings.TrimRight(os.Getenv("BEARER_TOKEN"), " \t\r\n\x00"); v != "" {
		return v, nil
	}
	var paths []string
	if p := os.Getenv("BEARER_TOKEN_FILE"); p != "" {
		paths = append(paths, p)
	}
	uid := strconv.Itoa(os.Geteuid())
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		paths = append(paths, xdg+"/bt_u"+uid)
	}
	paths = append(paths, "/tmp/bt_u"+uid)

	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if tok := strings.TrimRight(string(raw), " \t\r\n\x00"); tok != "" {
			return tok, nil
		}
	}
	return "", fmt.Errorf("auth/token: no bearer token found")
}

var _ auth.Auther = (*Auth)(nil)
```

> `os.Geteuid()` returns -1 on Windows; the `/tmp/bt_u-1` path simply won't exist there, which is fine (discovery falls through to an error). No special-casing needed.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./xrootd/xrdproto/auth/token/ -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
gofmt -w xrootd/xrdproto/auth/token/ && go vet ./xrootd/xrdproto/auth/token/
git add xrootd/xrdproto/auth/token/
git commit -m "xrootd/xrdproto/auth/token: add ztn bearer-token auth provider"
```

---

### Task 2: `sss` keytab reader

Split from the auth module because the keytab parser is independently testable and has one responsibility.

**Files:**
- Create: `xrootd/xrdproto/auth/sss/keytab.go`
- Test: `xrootd/xrdproto/auth/sss/keytab_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces (package `sss`):
  - `type Key struct { ID int64; Key []byte; User, Group, Name string; Expiry int64 }`.
  - `func (k Key) Expired(now time.Time) bool` — true when `Expiry != 0 && now.Unix() >= Expiry`.
  - `func ParseKeytab(r io.Reader) ([]Key, error)` — parses lines `0 N:<id> k:<hexkey> u:<user> g:<group> n:<name> [e:<exp>]`; skips blank lines and lines starting with `#`; hex-decodes `k:`.
  - `func LoadKeytab() ([]Key, error)` — reads the file at `$XrdSecSSSKT`, else `$XrdSecsssKT`, else `~/.xrd/sss.keytab`; error if none exists.
  - `func FirstLiveKey(keys []Key, now time.Time) (Key, error)` — the first non-expired key; error if none.

- [x] **Step 1: Write the failing test**

`xrootd/xrdproto/auth/sss/keytab_test.go`:

```go
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
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./xrootd/xrdproto/auth/sss/ -run 'TestParseKeytab|TestFirstLiveKey' -v`
Expected: FAIL (package missing).

- [x] **Step 3: Implement**

`xrootd/xrdproto/auth/sss/keytab.go`:

```go
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
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./xrootd/xrdproto/auth/sss/ -run 'TestParseKeytab|TestFirstLiveKey' -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
gofmt -w xrootd/xrdproto/auth/sss/ && go vet ./xrootd/xrdproto/auth/sss/
git add xrootd/xrdproto/auth/sss/keytab.go xrootd/xrdproto/auth/sss/keytab_test.go
git commit -m "xrootd/xrdproto/auth/sss: add SSS keytab reader"
```

---

### Task 3: `sss` credential builder + auth module

**Files:**
- Create: `xrootd/xrdproto/auth/sss/sss.go`
- Test: `xrootd/xrdproto/auth/sss/sss_test.go`

**Interfaces:**
- Consumes: `Key`/`LoadKeytab`/`FirstLiveKey` (Task 2), `auth.Auther`/`auth.Request`, `golang.org/x/crypto/blowfish`.
- Produces (package `sss`):
  - `var Type = [4]byte{'s', 's', 's', 0}`.
  - `const baseTime = 1222183880` (SSS epoch).
  - `func buildCredential(key Key, username string, nonce [32]byte, genTime uint32) ([]byte, error)` — assembles the exact outer header + BF-CFB64(cleartext+IEEE-CRC32) blob.
  - `type Auth struct { Key Key; User string }` implementing `auth.Auther`: `Provider()` returns `"sss"`; `Request(params)` builds a credential with a fresh random 32-byte nonce and `genTime = uint32(time.Now().Unix() - baseTime)`, returning `&auth.Request{Type: Type, Credentials: string(blob)}`.
  - `func New() (*Auth, error)` — loads the keytab, picks the first live key, and uses the current OS user (falling back to `"xrd"`) as the NAME.
  - `var Default auth.Auther` — set by `init()` to `New()`'s result when it succeeds, else nil.

- [x] **Step 1: Write the failing test (round-trip against Blowfish decrypt)**

`xrootd/xrdproto/auth/sss/sss_test.go` — encrypt with `buildCredential`, then independently decrypt with `blowfish` + CFB64 and assert the exact cleartext layout and CRC:

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sss

import (
	"bytes"
	"crypto/cipher"
	"encoding/binary"
	"hash/crc32"
	"testing"

	"golang.org/x/crypto/blowfish"
)

func TestBuildCredentialLayout(t *testing.T) {
	key := Key{ID: 0x0102030405060708, Key: []byte("0123456789abcdef0123456789abcdef")}
	var nonce [32]byte
	for i := range nonce {
		nonce[i] = byte(i)
	}
	blob, err := buildCredential(key, "alice", nonce, 42)
	if err != nil {
		t.Fatalf("buildCredential: %v", err)
	}

	// Outer header (16 bytes).
	if !bytes.Equal(blob[:4], []byte{'s', 's', 's', 0}) {
		t.Fatalf("magic: % x", blob[:4])
	}
	if blob[4] != 1 || blob[5] != 0 || blob[6] != 0 || blob[7] != '0' {
		t.Fatalf("header bytes: % x", blob[4:8])
	}
	if got := binary.BigEndian.Uint64(blob[8:16]); got != uint64(key.ID) {
		t.Fatalf("key id: got=%#x want=%#x", got, uint64(key.ID))
	}

	// Decrypt the body with Blowfish-CFB64, zero IV, to check the cleartext.
	bc, err := blowfish.NewCipher(key.Key)
	if err != nil {
		t.Fatalf("blowfish: %v", err)
	}
	body := blob[16:]
	plain := make([]byte, len(body))
	cipher.NewCFBDecrypter(bc, make([]byte, blowfish.BlockSize)).XORKeyStream(plain, body)

	if !bytes.Equal(plain[:32], nonce[:]) {
		t.Fatalf("nonce mismatch")
	}
	if got := binary.BigEndian.Uint32(plain[32:36]); got != 42 {
		t.Fatalf("gen_time: got=%d want=42", got)
	}
	if plain[39] != 0x00 {
		t.Fatalf("opt byte: got=%#x want=0", plain[39])
	}
	// NAME TLV at [40:]: type=1, 0, len, "alice\0".
	if plain[40] != 0x01 || plain[41] != 0x00 {
		t.Fatalf("TLV tag: % x", plain[40:42])
	}
	nlen := int(plain[42])
	if want := len("alice") + 1; nlen != want {
		t.Fatalf("TLV len: got=%d want=%d", nlen, want)
	}
	name := plain[43 : 43+nlen]
	if string(name) != "alice\x00" {
		t.Fatalf("TLV name: %q", name)
	}
	// Trailing IEEE-CRC32 (big-endian) over the cleartext.
	clearLen := 43 + nlen
	crc := binary.BigEndian.Uint32(plain[clearLen : clearLen+4])
	if want := crc32.ChecksumIEEE(plain[:clearLen]); crc != want {
		t.Fatalf("crc: got=%#x want=%#x", crc, want)
	}
}

func TestAuthProviderRequest(t *testing.T) {
	a := Auth{Key: Key{ID: 1, Key: bytes.Repeat([]byte{0xab}, 32)}, User: "bob"}
	if got, want := a.Provider(), "sss"; got != want {
		t.Fatalf("provider: got=%q want=%q", got, want)
	}
	req, err := a.Request(nil)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if req.Type != Type {
		t.Fatalf("type: %v", req.Type)
	}
	if len(req.Credentials) < 16 || req.Credentials[:4] != "sss\x00" {
		t.Fatalf("credentials prefix: %q", req.Credentials[:4])
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./xrootd/xrdproto/auth/sss/ -run 'TestBuildCredentialLayout|TestAuthProviderRequest' -v`
Expected: FAIL (`buildCredential`/`Auth` undefined).

- [x] **Step 3: Implement**

`xrootd/xrdproto/auth/sss/sss.go`:

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sss // import "go-hep.org/x/hep/xrootd/xrdproto/auth/sss"

import (
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os/user"
	"time"

	"golang.org/x/crypto/blowfish"

	"go-hep.org/x/hep/xrootd/xrdproto/auth"
)

// Type is the 4-byte credential type for the sss provider.
var Type = [4]byte{'s', 's', 's', 0}

// baseTime is the SSS timestamp epoch (2008-09-23T13:51:20Z); gen_time is
// seconds since this epoch, keeping the uint32 field valid past 2038.
const baseTime = 1222183880

// dataHdrLen is the fixed cleartext prefix: 32-byte nonce + 4-byte gen_time +
// 3 reserved + 1 option byte.
const dataHdrLen = 40

// typeName is the identity TLV tag for the login name.
const typeName = 0x01

// optUseData marks a self-contained credential (identity inline).
const optUseData = 0x00

// buildCredential assembles an SSS credential blob for key: a 16-byte outer
// header followed by the Blowfish-CFB64 encryption of (cleartext + IEEE-CRC32).
func buildCredential(key Key, username string, nonce [32]byte, genTime uint32) ([]byte, error) {
	if len(key.Key) == 0 {
		return nil, fmt.Errorf("auth/sss: empty key")
	}
	if username == "" {
		username = "xrd"
	}
	name := append([]byte(username), 0) // NUL-terminated; len includes the NUL
	if len(name) > 64 {
		name = name[:64]
		name[63] = 0
	}

	// Cleartext: 40-byte data header + NAME TLV.
	clear := make([]byte, 0, dataHdrLen+3+len(name))
	hdr := make([]byte, dataHdrLen)
	copy(hdr[:32], nonce[:])
	binary.BigEndian.PutUint32(hdr[32:36], genTime)
	hdr[39] = optUseData
	clear = append(clear, hdr...)
	clear = append(clear, typeName, 0x00, byte(len(name)))
	clear = append(clear, name...)

	// plain = cleartext + IEEE-CRC32 (big-endian).
	plain := make([]byte, len(clear)+4)
	copy(plain, clear)
	binary.BigEndian.PutUint32(plain[len(clear):], crc32.ChecksumIEEE(clear))

	// Blowfish-CFB64 encrypt with an all-zero 8-byte IV, no padding.
	bc, err := blowfish.NewCipher(key.Key)
	if err != nil {
		return nil, fmt.Errorf("auth/sss: blowfish: %w", err)
	}
	enc := make([]byte, len(plain))
	cipher.NewCFBEncrypter(bc, make([]byte, blowfish.BlockSize)).XORKeyStream(enc, plain)

	// 16-byte outer header + encrypted body.
	out := make([]byte, 16+len(enc))
	out[0], out[1], out[2], out[3] = 's', 's', 's', 0
	out[4] = 1   // version
	out[5] = 0   // spare
	out[6] = 0   // kn_size: no named key
	out[7] = '0' // enc marker: Blowfish-CFB64
	binary.BigEndian.PutUint64(out[8:16], uint64(key.ID))
	copy(out[16:], enc)
	return out, nil
}

// Auth is the "sss" security provider carrying a shared-secret key.
type Auth struct {
	Key  Key    // the shared key from the keytab
	User string // the login name embedded in the credential
}

// Provider returns the name of the security provider.
func (*Auth) Provider() string { return "sss" }

// Request forms a kXR_auth request carrying a freshly minted SSS credential.
func (a *Auth) Request(params []string) (*auth.Request, error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("auth/sss: could not read nonce: %w", err)
	}
	genTime := uint32(time.Now().Unix() - baseTime)
	blob, err := buildCredential(a.Key, a.User, nonce, genTime)
	if err != nil {
		return nil, err
	}
	return &auth.Request{Type: Type, Credentials: string(blob)}, nil
}

// New builds an Auth from the first live keytab key and the current OS user.
func New() (*Auth, error) {
	keys, err := LoadKeytab()
	if err != nil {
		return nil, err
	}
	k, err := FirstLiveKey(keys, time.Now())
	if err != nil {
		return nil, err
	}
	name := "xrd"
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
	}
	return &Auth{Key: k, User: name}, nil
}

// Default is the sss provider discovered from the ambient keytab, or nil when
// no keytab/live key is available.
var Default auth.Auther

func init() {
	if a, err := New(); err == nil {
		Default = a
	}
}

var _ auth.Auther = (*Auth)(nil)
```

> `crypto/cipher.NewCFBEncrypter` with an 8-byte (block-size) zero IV over a Blowfish cipher reproduces OpenSSL's `EVP_bf_cfb64` (CFB64 == full-block CFB for a 64-bit block cipher). The go vet "CFB is unauthenticated" concern does not apply — this is the mandated SSS wire format, not a new protocol choice; note it in a comment if a linter flags it.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./xrootd/xrdproto/auth/sss/ -v`
Expected: PASS (both keytab and credential tests).

- [x] **Step 5: Commit**

```bash
gofmt -w xrootd/xrdproto/auth/sss/ && go vet ./xrootd/xrdproto/auth/sss/
git add xrootd/xrdproto/auth/sss/sss.go xrootd/xrdproto/auth/sss/sss_test.go
git commit -m "xrootd/xrdproto/auth/sss: add SSS credential builder and auth provider"
```

---

### Task 4: register `ztn` and `sss` in the client's default providers

**Files:**
- Modify: `xrootd/auth.go`
- Test: `xrootd/auth_providers_test.go` (create)

**Interfaces:**
- Consumes: `token.Default`, `sss.Default` (Tasks 1, 3); the existing `defaultProviders` slice and `initSecurityProviders` (which skips nil entries — verified in `client.go`).
- Produces: `defaultProviders` includes `token.Default` and `sss.Default`; a client built with no options exposes `"ztn"`/`"sss"` in `client.auths` when those Defaults are non-nil.

- [x] **Step 1: Write the failing test**

`xrootd/auth_providers_test.go`:

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"testing"

	"go-hep.org/x/hep/xrootd/xrdproto/auth/sss"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/token"
)

func TestDefaultProvidersRegistersTokenAndSSS(t *testing.T) {
	client := &Client{auths: map[string]auth_Auther{}}
	_ = client
	// Register explicit providers to prove wiring is scheme-correct, independent
	// of ambient discovery (token.Default/sss.Default may be nil in CI).
	c := &Client{auths: map[string]authAlias{}}
	_ = c
}
```

> The above scaffolding is a placeholder pattern; replace it with the real test below in Step 3's iteration — see the actual assertion. The real test (write THIS one, not the scaffold):

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"testing"

	"go-hep.org/x/hep/xrootd/xrdproto/auth/sss"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/token"
)

func TestDefaultProvidersIncludeTokenAndSSS(t *testing.T) {
	found := map[string]bool{}
	for _, p := range defaultProviders {
		if p != nil {
			found[p.Provider()] = true
		}
	}
	// Register explicit instances so the test is independent of ambient
	// discovery (the package-level Defaults may be nil without a token/keytab).
	client := &Client{auths: map[string]auther{}}
	client.addAuth(&token.Auth{Token: "x"})
	client.addAuth(&sss.Auth{Key: sss.Key{ID: 1, Key: []byte("k")}, User: "u"})
	if _, ok := client.auths["ztn"]; !ok {
		t.Fatal("ztn provider not registered via addAuth")
	}
	if _, ok := client.auths["sss"]; !ok {
		t.Fatal("sss provider not registered via addAuth")
	}
}
```

> Use the real second snippet. The map value type in `Client.auths` is `auth.Auther`; import it as needed — check `client.go` (`auths map[string]auth.Auther`) and write `map[string]auth.Auther` with the `auth` import. Delete the first scaffold snippet entirely; it exists only to make the "write a failing test" contrast explicit and MUST NOT be committed.

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./xrootd/ -run TestDefaultProvidersIncludeTokenAndSSS -v`
Expected: FAIL initially only if the imports/registration are absent; since `addAuth` already exists, the assertions on `addAuth` pass, but the test also documents intent. To make this a true red→green, first assert on `defaultProviders` membership (which is what Step 3 changes):

Replace the two `client.auths` assertions' role: the meaningful red is on defaults. Final test body:

```go
func TestDefaultProvidersIncludeTokenAndSSS(t *testing.T) {
	// token.Default and sss.Default are added to defaultProviders even when nil
	// (nil entries are skipped by initSecurityProviders). Assert the slice
	// references them by identity so the wiring cannot silently regress.
	hasToken, hasSSS := false, false
	for _, p := range defaultProviders {
		switch p {
		case token.Default:
			hasToken = true
		case sss.Default:
			hasSSS = true
		}
	}
	if !hasToken {
		t.Fatal("token.Default not in defaultProviders")
	}
	if !hasSSS {
		t.Fatal("sss.Default not in defaultProviders")
	}
}
```

Run: `go test ./xrootd/ -run TestDefaultProvidersIncludeTokenAndSSS -v`
Expected: FAIL — `token`/`sss` not imported/added yet (compile error, then assertion).

> Write only this final version of the test in the file.

- [x] **Step 3: Implement**

In `xrootd/auth.go`, add imports and the two providers:

```go
import (
	"bytes"
	"context"
	"fmt"

	"go-hep.org/x/hep/xrootd/xrdproto/auth"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/host"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/krb5"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/sss"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/token"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/unix"
)

// defaultProviders is the list of authentication providers a xrootd client will
// use by default. nil entries (a provider whose ambient discovery failed) are
// skipped by initSecurityProviders.
var defaultProviders = []auth.Auther{
	krb5.Default,
	token.Default,
	sss.Default,
	unix.Default,
	host.Default,
}
```

> Ordering places credentialed providers (krb5, ztn, sss) before the weak host/unix identities, matching how a server typically prefers stronger auth; the actual selection is still driven by the server's offered list in `sess.auth`.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./xrootd/ -run TestDefaultProvidersIncludeTokenAndSSS -v && go test ./xrootd/...`
Expected: PASS, no regressions.

- [x] **Step 5: Commit**

```bash
gofmt -w xrootd/auth.go xrootd/auth_providers_test.go && go vet ./xrootd/...
git add xrootd/auth.go xrootd/auth_providers_test.go
git commit -m "xrootd: register ztn and sss in default auth providers"
```

---

### Task 5: S3 credential discovery (for the Phase 4 S3 backend)

**Files:**
- Create: `xrootd/internal/s3cred/s3cred.go`
- Test: `xrootd/internal/s3cred/s3cred_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces (package `s3cred`):
  - `type Credentials struct { AccessKey, Secret string }`.
  - `type Provider struct { AccessKey, Secret string }` — explicit override pair (either or both may be empty).
  - `func (p Provider) Resolve() (Credentials, error)` — precedence: explicit `p.AccessKey`+`p.Secret`, then `$AWS_ACCESS_KEY_ID`+`$AWS_SECRET_ACCESS_KEY`, then `~/.aws/credentials` `[default]`. Both parts required at a given level; error if none complete.
  - `func parseAWSCredentials(r io.Reader, profile string) (Credentials, error)` — minimal INI reader for the named profile's `aws_access_key_id`/`aws_secret_access_key`.

- [x] **Step 1: Write the failing test**

`xrootd/internal/s3cred/s3cred_test.go`:

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package s3cred

import (
	"strings"
	"testing"
)

func TestResolveExplicit(t *testing.T) {
	c, err := Provider{AccessKey: "AK", Secret: "SK"}.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.AccessKey != "AK" || c.Secret != "SK" {
		t.Fatalf("got %+v", c)
	}
}

func TestResolveEnv(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "envAK")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "envSK")
	c, err := Provider{}.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.AccessKey != "envAK" || c.Secret != "envSK" {
		t.Fatalf("got %+v", c)
	}
}

func TestResolvePartialIsUnavailable(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "only-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("HOME", t.TempDir()) // no ~/.aws/credentials
	if _, err := (Provider{}).Resolve(); err == nil {
		t.Fatal("expected error for a partial credential pair")
	}
}

func TestParseAWSCredentials(t *testing.T) {
	ini := `[other]
aws_access_key_id = OTHER
aws_secret_access_key = othersecret

[default]
aws_access_key_id = DEFAULTAK
aws_secret_access_key = defaultsecret
`
	c, err := parseAWSCredentials(strings.NewReader(ini), "default")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c.AccessKey != "DEFAULTAK" || c.Secret != "defaultsecret" {
		t.Fatalf("got %+v", c)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./xrootd/internal/s3cred/ -v`
Expected: FAIL (package missing).

- [x] **Step 3: Implement**

`xrootd/internal/s3cred/s3cred.go`:

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package s3cred resolves S3 access-key/secret credentials for the XRootD S3
// backend. It mirrors the discovery precedence of the reference C client:
// explicit values, then the AWS environment variables, then the AWS shared
// credentials file. Both the access key and the secret are required.
package s3cred // import "go-hep.org/x/hep/xrootd/internal/s3cred"

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Credentials is a resolved S3 access-key/secret pair.
type Credentials struct {
	AccessKey string
	Secret    string
}

// Provider resolves S3 credentials. AccessKey and Secret, when both set, take
// precedence over the environment and the AWS shared credentials file.
type Provider struct {
	AccessKey string
	Secret    string
}

// Resolve returns the first complete credential pair from, in order: the
// explicit Provider fields; $AWS_ACCESS_KEY_ID + $AWS_SECRET_ACCESS_KEY; the
// [default] profile of ~/.aws/credentials.
func (p Provider) Resolve() (Credentials, error) {
	if p.AccessKey != "" && p.Secret != "" {
		return Credentials{AccessKey: p.AccessKey, Secret: p.Secret}, nil
	}
	if ak, sk := os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY"); ak != "" && sk != "" {
		return Credentials{AccessKey: ak, Secret: sk}, nil
	}
	home, err := os.UserHomeDir()
	if err == nil {
		f, err := os.Open(filepath.Join(home, ".aws", "credentials"))
		if err == nil {
			defer f.Close()
			if c, err := parseAWSCredentials(f, "default"); err == nil && c.AccessKey != "" && c.Secret != "" {
				return c, nil
			}
		}
	}
	return Credentials{}, fmt.Errorf("s3cred: no complete S3 credential pair found")
}

// parseAWSCredentials reads the named profile's aws_access_key_id and
// aws_secret_access_key from an AWS shared-credentials INI stream.
func parseAWSCredentials(r io.Reader, profile string) (Credentials, error) {
	var c Credentials
	inProfile := false
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inProfile = strings.TrimSpace(line[1:len(line)-1]) == profile
			continue
		}
		if !inProfile {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "aws_access_key_id":
			c.AccessKey = strings.TrimSpace(v)
		case "aws_secret_access_key":
			c.Secret = strings.TrimSpace(v)
		}
	}
	if err := sc.Err(); err != nil {
		return Credentials{}, fmt.Errorf("s3cred: could not read credentials file: %w", err)
	}
	return c, nil
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./xrootd/internal/s3cred/ -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
gofmt -w xrootd/internal/s3cred/ && go vet ./xrootd/...
git add xrootd/internal/s3cred/
git commit -m "xrootd/internal/s3cred: add S3 credential discovery for the S3 backend"
```

---

### Task 6: interop test + parity runbook

**Files:**
- Create: `xrootd/phase3_interop_test.go`
- Create: `docs/superpowers/testing/xrootd-phase-3-parity.md`

**Interfaces:**
- Consumes: `Dial`/`WithAuth` (Phase 0), `token.Auth`, `sss.New`.
- Produces: a gated test (skips without `XROOTD_P3_SERVER`) that stats a path using an explicitly chosen provider; a runbook naming the `libxrdc` cross-checks.

- [x] **Step 1: Write the gated test**

`xrootd/phase3_interop_test.go`:

```go
// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"os"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/xrdproto/auth/sss"
	"go-hep.org/x/hep/xrootd/xrdproto/auth/token"
)

// TestPhase3Interop authenticates against a real XRootD server using the auth
// provider named by XROOTD_P3_PROVIDER (ztn or sss). Skipped unless
// XROOTD_P3_SERVER and XROOTD_P3_PATH are set.
func TestPhase3Interop(t *testing.T) {
	server := os.Getenv("XROOTD_P3_SERVER")
	path := os.Getenv("XROOTD_P3_PATH")
	if server == "" || path == "" {
		t.Skip("set XROOTD_P3_SERVER and XROOTD_P3_PATH to run the phase-3 interop test")
	}

	var opt Option
	switch os.Getenv("XROOTD_P3_PROVIDER") {
	case "ztn":
		tok, err := token.Discover()
		if err != nil {
			t.Skipf("no bearer token: %v", err)
		}
		opt = WithAuth(&token.Auth{Token: tok})
	case "sss":
		a, err := sss.New()
		if err != nil {
			t.Skipf("no sss keytab: %v", err)
		}
		opt = WithAuth(a)
	default:
		t.Skip("set XROOTD_P3_PROVIDER to ztn or sss")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	be, err := Dial(ctx, server, os.Getenv("USER"), opt)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer be.Close()

	fi, err := be.FS().Stat(ctx, path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	t.Logf("authenticated ok: name=%s size=%d", fi.Name(), fi.Size())
}
```

- [x] **Step 2: Verify it skips cleanly**

Run: `go test ./xrootd/ -run TestPhase3Interop -v`
Expected: PASS with `--- SKIP`.

- [x] **Step 3: Write the parity runbook**

Create `docs/superpowers/testing/xrootd-phase-3-parity.md`:

```markdown
# Phase 3 Parity Verification (expanded auth)

## Oracle 1 — official XRootD server

```sh
export XROOTD_P3_SERVER=roots://HOST:1094   # roots:// so TLS protects the token
export XROOTD_P3_PATH=//store/test/file.root

# ztn (bearer token)
export BEARER_TOKEN="$(cat /path/to/token)"
XROOTD_P3_PROVIDER=ztn go test ./xrootd/ -run TestPhase3Interop -v

# sss (shared secret)
export XrdSecSSSKT=/path/to/sss.keytab
XROOTD_P3_PROVIDER=sss go test ./xrootd/ -run TestPhase3Interop -v
```

Expected: the stat succeeds, proving the server accepted the credential.

## Oracle 2 — libxrdc / stock XRootD client cross-check

Same server, same credential, via the C client (binaries under
/home/rcurrie/HEP-x/nginx-xrootd/client/bin):

```sh
XrdSecPROTOCOL=ztn xrdfs roots://HOST:1094 stat /store/test/file.root
XrdSecPROTOCOL=sss XrdSecSSSKT=/path/to/sss.keytab xrdfs root://HOST:1094 stat /store/test/file.root
```

Both clients must be accepted by the same server with the same credential. For
sss, a credential minted by go-hep and one minted by the C client must both
decrypt against the same keytab key (the server is the decryptor).

## S3 credentials

`s3cred` has no server round trip in this phase; its discovery precedence is
covered by unit tests and will be exercised end-to-end by the Phase 4 S3 backend.

## Regression

`go test ./xrootd/...` stays green; unix/host/krb5 auth is unchanged (ztn/sss
are additive and selected only when the server offers them).
```

- [x] **Step 4: Final phase verification**

Run: `gofmt -l xrootd/` (empty), `go vet ./xrootd/...` (clean), `go test ./xrootd/...` (green), `go test -race ./xrootd/` (green).

- [x] **Step 5: Commit**

```bash
git add xrootd/phase3_interop_test.go docs/superpowers/testing/xrootd-phase-3-parity.md
git commit -m "xrootd: add phase-3 auth interop test and parity runbook"
```

---

## Self-Review

**Spec coverage (roadmap Phase 3 — Expanded auth):**
- "bearer/scitokens" → Task 1 (`ztn`/token). ✓
- "sss" → Tasks 2 (keytab) + 3 (credential builder + provider). ✓
- "s3 credentials" → Task 5 (`s3cred` discovery, consumed by Phase 4). ✓
- "Built as additional `auth.Auther` implementations under `xrdproto/auth/`, enabled by Phase 0 TLS" → Tasks 1, 3 follow the exact `unix`/`host`/`krb5` pattern; registered in Task 4. ✓ (ztn/sss are typically used over `roots://`; the runbook uses TLS.)
- "gsi/x509 (proxy certs) — the big one" → **explicitly deferred** to a Phase 3b spec/plan, with the reason stated (multi-round `kXR_authmore` + bucket-TLV/DH/AES wire format needing `src/gsi` reverse-engineering). This is a scoping decision recorded in the plan, not a silent omission. ✓

**Placeholder scan:** Task 4 Step 1 deliberately shows a throwaway scaffold and then instructs (twice, emphatically) to write only the final test version and delete the scaffold — this is a teaching contrast, and Step 2 pins down the exact committed test. All other code blocks are complete and self-contained. No "TBD"/"add error handling"/"similar to Task N". ✓

**Type consistency:** `auth.Request{Type, Credentials}`, `Type = [4]byte{...}`, `Auth`/`Provider()`/`Request()`, `sss.Key`/`buildCredential`/`New`/`FirstLiveKey`/`ParseKeytab`, `s3cred.Provider`/`Credentials`/`Resolve`/`parseAWSCredentials`, `token.Discover`/`Default` are used identically across tasks. `defaultProviders` element type is `auth.Auther`. ✓

**Verification-dependent assumptions flagged inline:** (a) `Client.auths` map value type is `auth.Auther` (confirm in `client.go`); (b) `initSecurityProviders` skips nil entries (confirmed earlier in `client.go`); (c) `auth.Request.Credentials` carries binary/NUL bytes fine (krb5 precedent); (d) `blowfish.NewCipher` accepts variable key lengths ≤ 56 bytes (SSS keys are 32) — the credential test cross-checks encryption via an independent decrypt, so a mismatch fails loudly.
