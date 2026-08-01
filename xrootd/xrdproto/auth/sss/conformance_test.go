// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for how an sss provider finds a keytab and what it puts on the
// wire.
//
// The credential layout is pinned byte-for-byte in sss_test.go. What is pinned
// here is everything around it: where the keytab is looked for and in what
// order, which key in it is used, and what a credential says about the identity
// it carries. A provider that quietly picks an expired key, or truncates a long
// login name into a different name, does not fail here — it fails at the
// server, as an authorization error with nothing pointing back at the keytab.

package sss

import (
	"crypto/cipher"
	"encoding/binary"
	"hash/crc32"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/blowfish"
)

// hexKey is a 16-byte key written the way a keytab writes it.
const hexKey = "000102030405060708090a0b0c0d0e0f"

// sssKeytab writes a keytab holding the given lines and returns its path.
func sssKeytab(t *testing.T, lines ...string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "sss.keytab")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatalf("could not write the keytab: %v", err)
	}
	return path
}

// sssIdentity reads a credential back the way a server does: unwrap the outer
// header, decrypt the body with the shared key, check the trailing CRC and
// return the key id it names and the login name it carries.
func sssIdentity(t *testing.T, key Key, blob string) (id uint64, name string) {
	t.Helper()

	if len(blob) < 16+dataHdrLen+3 {
		t.Fatalf("the credential is %d bytes, too short to hold an identity", len(blob))
	}
	id = binary.BigEndian.Uint64([]byte(blob[8:16]))

	bc, err := blowfish.NewCipher(key.Key)
	if err != nil {
		t.Fatalf("could not build the cipher: %v", err)
	}
	body := []byte(blob[16:])
	plain := make([]byte, len(body))
	cipher.NewCFBDecrypter(bc, make([]byte, blowfish.BlockSize)).XORKeyStream(plain, body)

	if plain[dataHdrLen] != typeName {
		t.Fatalf("the credential carries tag %#x, want a name", plain[dataHdrLen])
	}
	n := int(plain[dataHdrLen+2])
	if got := len(plain); got < dataHdrLen+3+n+4 {
		t.Fatalf("the credential is %d bytes, too short for a %d-byte name", got, n)
	}
	clearLen := dataHdrLen + 3 + n
	if got, want := binary.BigEndian.Uint32(plain[clearLen:clearLen+4]), crc32.ChecksumIEEE(plain[:clearLen]); got != want {
		t.Fatalf("the credential does not check out: crc=%#x want=%#x", got, want)
	}
	return id, strings.TrimSuffix(string(plain[dataHdrLen+3:clearLen]), "\x00")
}

func TestConformance_TheKeytabIsLookedForWhereXRootDPutsIt(t *testing.T) {
	// XrdSecSSSKT is the spelling the XRootD documentation uses and XrdSecsssKT
	// the one several deployments set; a client honouring only one of them
	// silently falls back to no credential at all on the other half.
	for _, env := range []string{"XrdSecSSSKT", "XrdSecsssKT"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv("XrdSecSSSKT", "")
			t.Setenv("XrdSecsssKT", "")
			t.Setenv(env, sssKeytab(t, "0 u:gopher g:hep N:42 k:"+hexKey+" n:go-hep e:0"))

			keys, err := LoadKeytab()
			if err != nil {
				t.Fatalf("could not load the keytab from %s: %v", env, err)
			}
			if len(keys) != 1 {
				t.Fatalf("the keytab holds %d keys, want 1", len(keys))
			}
			got := keys[0]
			if got.ID != 42 || got.User != "gopher" || got.Group != "hep" || got.Name != "go-hep" {
				t.Fatalf("the key was parsed as %+v", got)
			}
			if len(got.Key) != 16 {
				t.Fatalf("the key is %d bytes, want 16", len(got.Key))
			}
		})
	}
}

func TestConformance_TheDocumentedSpellingWins(t *testing.T) {
	// With both set, the documented name decides — otherwise which keytab is
	// used depends on nothing the operator can see.
	t.Setenv("XrdSecSSSKT", sssKeytab(t, "0 N:1 k:"+hexKey))
	t.Setenv("XrdSecsssKT", sssKeytab(t, "0 N:2 k:"+hexKey))

	keys, err := LoadKeytab()
	if err != nil {
		t.Fatalf("could not load the keytab: %v", err)
	}
	if keys[0].ID != 1 {
		t.Fatalf("the key came from the wrong keytab: id=%d", keys[0].ID)
	}
}

func TestConformance_AKeytabThatIsNotThereIsSkippedNotFatal(t *testing.T) {
	// A stale variable pointing at a removed keytab must not stop the search:
	// the next location may well hold a usable one.
	t.Setenv("XrdSecSSSKT", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("XrdSecsssKT", sssKeytab(t, "0 N:7 k:"+hexKey))

	keys, err := LoadKeytab()
	if err != nil {
		t.Fatalf("could not load the keytab: %v", err)
	}
	if keys[0].ID != 7 {
		t.Fatalf("the key came from the wrong keytab: id=%d", keys[0].ID)
	}
}

func TestConformance_AKeytabThatCannotBeParsedIsAnError(t *testing.T) {
	// A keytab that exists but is malformed is a configuration mistake, and
	// falling through to the next location would hide it behind whichever
	// credential happened to work.
	for _, tc := range []struct {
		name string
		line string
		want string
	}{
		{"a key that is not hex", "0 N:1 k:zzzz", "bad hex key"},
		{"a line with no key at all", "0 N:1 u:gopher", "without a key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XrdSecSSSKT", sssKeytab(t, tc.line))
			t.Setenv("XrdSecsssKT", sssKeytab(t, "0 N:9 k:"+hexKey))

			_, err := LoadKeytab()
			if err == nil {
				t.Fatal("a malformed keytab was accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the failure does not say what is wrong: %v", err)
			}
		})
	}
}

func TestConformance_CommentsAndBlankLinesAreNotKeys(t *testing.T) {
	t.Setenv("XrdSecSSSKT", sssKeytab(t,
		"# an sss keytab",
		"",
		"   ",
		"0 N:1 k:"+hexKey+" u:gopher",
		"# trailing comment",
	))
	t.Setenv("XrdSecsssKT", "")

	keys, err := LoadKeytab()
	if err != nil {
		t.Fatalf("could not load the keytab: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("the keytab parsed to %d keys, want 1", len(keys))
	}
}

func TestConformance_TheFirstLiveKeyIsTheOneUsed(t *testing.T) {
	// Keytabs are rotated by appending, so the file routinely holds expired
	// keys ahead of the current one.
	now := time.Now()
	t.Setenv("XrdSecSSSKT", sssKeytab(t,
		"0 N:1 k:"+hexKey+" e:"+strconv.FormatInt(now.Add(-time.Hour).Unix(), 10),
		"0 N:2 k:"+hexKey+" e:"+strconv.FormatInt(now.Add(time.Hour).Unix(), 10),
		"0 N:3 k:"+hexKey,
	))
	t.Setenv("XrdSecsssKT", "")

	a, err := New()
	if err != nil {
		t.Fatalf("could not build an sss provider: %v", err)
	}
	if a.Key.ID != 2 {
		t.Fatalf("the provider picked key %d, want the first live one (2)", a.Key.ID)
	}
	if a.User == "" {
		t.Fatal("the provider carries no login name")
	}
	if got := a.Provider(); got != "sss" {
		t.Fatalf("the provider calls itself %q, want %q", got, "sss")
	}

	// And the credential it mints names that key, so the server can find the
	// matching secret in its own keytab rather than guessing.
	req, err := a.Request(nil)
	if err != nil {
		t.Fatalf("could not build a credential: %v", err)
	}
	if req.Type != Type {
		t.Fatalf("the request is typed %q, want %q", req.Type, Type)
	}
	id, name := sssIdentity(t, a.Key, req.Credentials)
	if id != uint64(a.Key.ID) {
		t.Fatalf("the credential names key %d, want %d", id, a.Key.ID)
	}
	if name != a.User {
		t.Fatalf("the credential names %q, want %q", name, a.User)
	}
}

func TestConformance_AKeytabWithNoLiveKeyProducesNoProvider(t *testing.T) {
	// Offering sss with an expired key fails the login outright instead of
	// letting the client fall back to whatever else the server advertised.
	t.Setenv("XrdSecSSSKT", sssKeytab(t, "0 N:1 k:"+hexKey+" e:1"))
	t.Setenv("XrdSecsssKT", "")

	switch _, err := New(); {
	case err == nil:
		t.Fatal("an expired keytab produced a provider")
	case !strings.Contains(err.Error(), "no live key"):
		t.Fatalf("the failure does not say the keys are expired: %v", err)
	}
}

func TestConformance_NoKeytabAnywhereProducesNoProvider(t *testing.T) {
	t.Setenv("XrdSecSSSKT", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("XrdSecsssKT", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("HOME", t.TempDir())

	switch _, err := New(); {
	case err == nil:
		t.Fatal("a provider was built with no keytab at all")
	case !strings.Contains(err.Error(), "no keytab"):
		t.Fatalf("the failure does not say the keytab is missing: %v", err)
	}
}

func TestConformance_TheHomeKeytabIsTheLastResort(t *testing.T) {
	// With nothing in the environment, the provider still comes up from the
	// conventional location — this is the unconfigured case that has to work.
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".xrd"), 0700); err != nil {
		t.Fatalf("could not create the .xrd directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".xrd", "sss.keytab"),
		[]byte("0 N:11 k:"+hexKey+" u:gopher\n"), 0600); err != nil {
		t.Fatalf("could not write the keytab: %v", err)
	}
	t.Setenv("XrdSecSSSKT", "")
	t.Setenv("XrdSecsssKT", "")
	t.Setenv("HOME", home)

	keys, err := LoadKeytab()
	if err != nil {
		t.Fatalf("could not load the home keytab: %v", err)
	}
	if keys[0].ID != 11 {
		t.Fatalf("the key came from somewhere else: id=%d", keys[0].ID)
	}
}

func TestConformance_ACredentialNeedsAKey(t *testing.T) {
	// The zero Auth is what a caller holds after a keytab that failed to load;
	// minting from it would put an unencryptable credential on the wire.
	a := &Auth{}
	switch _, err := a.Request(nil); {
	case err == nil:
		t.Fatal("a credential was minted with no key")
	case !strings.Contains(err.Error(), "empty key"):
		t.Fatalf("the failure does not say the key is missing: %v", err)
	}
}

func TestConformance_AKeyTheCipherCannotUseIsRefused(t *testing.T) {
	// Blowfish takes 1..56 bytes. A keytab holding a longer key is a mistake
	// worth reporting, not something to silently truncate.
	a := &Auth{Key: Key{ID: 1, Key: make([]byte, 128)}, User: "gopher"}
	switch _, err := a.Request(nil); {
	case err == nil:
		t.Fatal("an unusable key produced a credential")
	case !strings.Contains(err.Error(), "blowfish"):
		t.Fatalf("the failure does not say the cipher refused the key: %v", err)
	}
}

func TestConformance_ACredentialAlwaysCarriesALoginName(t *testing.T) {
	// The name is what the server authorizes. XrdSecsss uses "xrd" when the
	// client offers none, and the field holds 64 bytes including the
	// terminator — a longer name is cut, not allowed to run over.
	key := Key{ID: 1, Key: []byte("0123456789abcdef")}

	for _, tc := range []struct {
		name string
		user string
		want string
	}{
		{"no name at all", "", "xrd"},
		{"a plain name", "gopher", "gopher"},
		{"a name past the field width", strings.Repeat("g", 200), strings.Repeat("g", 63)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Auth{Key: key, User: tc.user}
			req, err := a.Request(nil)
			if err != nil {
				t.Fatalf("could not build a credential: %v", err)
			}
			if _, got := sssIdentity(t, key, req.Credentials); got != tc.want {
				t.Fatalf("the credential names %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConformance_EveryCredentialIsFresh(t *testing.T) {
	// The nonce is what stops a captured credential being replayed, so two
	// credentials minted from one key must not be the same bytes.
	a := &Auth{Key: Key{ID: 1, Key: []byte("0123456789abcdef")}, User: "gopher"}

	first, err := a.Request(nil)
	if err != nil {
		t.Fatalf("could not build a credential: %v", err)
	}
	second, err := a.Request(nil)
	if err != nil {
		t.Fatalf("could not build a credential: %v", err)
	}
	if first.Credentials == second.Credentials {
		t.Fatal("two credentials from the same key are identical")
	}
}

func TestConformance_AKeyIsLiveUntilTheInstantItExpires(t *testing.T) {
	// The boundary decides whether a client rotating on the hour sends a key
	// the server has just dropped.
	k := Key{Expiry: 1000}
	for _, tc := range []struct {
		name string
		now  time.Time
		want bool
	}{
		{"a second before", time.Unix(999, 0), false},
		{"the instant itself", time.Unix(1000, 0), true},
		{"a second after", time.Unix(1001, 0), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := k.Expired(tc.now); got != tc.want {
				t.Fatalf("the key reports expired=%v at %v, want %v", got, tc.now.Unix(), tc.want)
			}
		})
	}
	if (Key{}).Expired(time.Unix(1<<40, 0)) {
		t.Fatal("a key with no expiry went stale")
	}
}

func TestConformance_TheKeytabLocationsAreTheOnesThatWereSearched(t *testing.T) {
	// The list a failure reports has to be the list the loader actually walks,
	// or a user is told to look in a place the client never opened. Both come
	// from KeytabLocations, which is the point of it being one function.
	a := filepath.Join(t.TempDir(), "a.keytab")
	b := filepath.Join(t.TempDir(), "b.keytab")
	t.Setenv("XrdSecSSSKT", a)
	t.Setenv("XrdSecsssKT", b)

	got := KeytabLocations()
	if len(got) < 2 || got[0] != a || got[1] != b {
		t.Fatalf("the locations are %q, want %q and %q first", got, a, b)
	}
	if home, err := os.UserHomeDir(); err == nil {
		if want := filepath.Join(home, ".xrd", "sss.keytab"); got[len(got)-1] != want {
			t.Fatalf("the last location is %q, want %q", got[len(got)-1], want)
		}
	}
}

func TestConformance_AKeytabTheUserNamesIsUsedOnItsOwnTerms(t *testing.T) {
	// A user who names a keytab has said which file to use, so the environment
	// must not redirect it — but the rules for reading it are the same rules,
	// down to skipping expired keys.
	t.Setenv("XrdSecSSSKT", sssKeytab(t, "0 N:1 k:"+hexKey))
	named := sssKeytab(t,
		"0 N:2 k:"+hexKey+" e:1",
		"0 N:3 k:"+hexKey+" e:0",
	)

	a, err := NewFromKeytab(named)
	if err != nil {
		t.Fatalf("could not use the keytab: %v", err)
	}
	if a.Key.ID != 3 {
		t.Fatalf("key %d was used, want the first live key of the named keytab", a.Key.ID)
	}
	if a.User == "" {
		t.Fatal("the credential carries no login name")
	}
}

func TestConformance_AKeytabTheUserNamesAndCannotBeUsedIsAnError(t *testing.T) {
	for _, tc := range []struct {
		name string
		path func(t *testing.T) string
	}{
		{"no such file", func(t *testing.T) string { return filepath.Join(t.TempDir(), "absent") }},
		{"not a keytab", func(t *testing.T) string { return sssKeytab(t, "0 N:1 nokeyhere") }},
		{"nothing live", func(t *testing.T) string { return sssKeytab(t, "0 N:1 k:"+hexKey+" e:1") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewFromKeytab(tc.path(t)); err == nil {
				t.Fatal("an unusable keytab produced a credential")
			}
		})
	}
}
