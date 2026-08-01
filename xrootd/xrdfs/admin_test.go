// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdfs_test

import (
	"reflect"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdfs"
)

func TestParseLocations(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want []xrdfs.Location
		err  bool
	}{
		{
			name: "server and manager",
			raw:  "Sr140.105.1.1:1094 Mw[::1]:1095\x00\x00\x00",
			want: []xrdfs.Location{
				{Addr: "140.105.1.1:1094", Kind: 'S', Access: 'r'},
				{Addr: "[::1]:1095", Kind: 'M', Access: 'w'},
			},
		},
		{
			name: "pending replica",
			raw:  "sw198.51.100.7:1095",
			want: []xrdfs.Location{{Addr: "198.51.100.7:1095", Kind: 's', Access: 'w'}},
		},
		{
			// A path nothing holds is an empty answer, not an error: it is what
			// a locate on a file that does not exist anywhere looks like.
			name: "nowhere",
			raw:  "\x00",
		},
		{
			name: "truncated token",
			raw:  "Sr",
			err:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := xrdfs.ParseLocations([]byte(tc.raw))
			switch {
			case tc.err && err == nil:
				t.Fatalf("%q was accepted, giving %v", tc.raw, got)
			case tc.err:
				return
			case err != nil:
				t.Fatalf("could not parse %q: %v", tc.raw, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("locations:\ngot = %v\nwant = %v", got, tc.want)
			}
		})
	}
}

func TestLocationString(t *testing.T) {
	for _, tc := range []struct {
		loc  xrdfs.Location
		want string
	}{
		{xrdfs.Location{Addr: "a:1094", Kind: 'S', Access: 'r'}, "Location(a:1094, server, read-only)"},
		{xrdfs.Location{Addr: "b:1094", Kind: 'M', Access: 'w'}, "Location(b:1094, manager, read-write)"},
		{xrdfs.Location{Addr: "c:1094", Kind: 's', Access: 'r'}, "Location(c:1094, pending server, read-only)"},
		{xrdfs.Location{Addr: "d:1094", Kind: 'm', Access: 'w'}, "Location(d:1094, pending manager, read-write)"},
	} {
		if got := tc.loc.String(); got != tc.want {
			t.Fatalf("location:\ngot = %s\nwant = %s", got, tc.want)
		}
	}
}

func TestParseConfig(t *testing.T) {
	names := []string{"version", "sitename", "role", "cms"}

	for _, tc := range []struct {
		name  string
		raw   string
		want  map[string]string
		names []string
	}{
		{
			// The empty line is what keeps "manager" paired with "role": a
			// parser that skipped it would hand it to "sitename" instead.
			name: "a value the server does not have",
			raw:  "v5.6.3\n\nmanager\nyes\x00",
			want: map[string]string{"version": "v5.6.3", "role": "manager", "cms": "yes"},
		},
		{
			// A server that answers fewer lines than were asked is not an
			// error: the names it said nothing about are simply absent.
			name: "a short answer",
			raw:  "v5.6.3\nRAL",
			want: map[string]string{"version": "v5.6.3", "sitename": "RAL"},
		},
		{
			name: "nothing at all",
			raw:  "",
			want: map[string]string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := xrdfs.ParseConfig(names, []byte(tc.raw))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("config:\ngot = %v\nwant = %v", got, tc.want)
			}
		})
	}
}
