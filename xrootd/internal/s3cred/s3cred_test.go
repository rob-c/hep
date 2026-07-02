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
