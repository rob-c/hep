// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for S3 credential resolution.
//
// The order is the one every AWS client uses, and it is an order rather than a
// search because each step is more explicit than the last: what the caller
// passed in beats what the environment says, which beats what is on disk. A
// client that reversed it would ignore the credentials a job was launched with
// and sign with whatever was left in the developer's home directory — which
// fails as "access denied" against a bucket the job can plainly reach.

package s3cred

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearAWSEnv removes what Resolve consults, so a test only sees what it sets.
func clearAWSEnv(t *testing.T) {
	t.Helper()

	for _, env := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"} {
		t.Setenv(env, "")
	}
}

// awsHome points $HOME at a fresh directory and writes content to
// ~/.aws/credentials if it is not empty.
func awsHome(t *testing.T, content string) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)

	if content == "" {
		return
	}
	dir := filepath.Join(home, ".aws")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("could not create %q: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "credentials"), []byte(content), 0o600); err != nil {
		t.Fatalf("could not write the credentials file: %v", err)
	}
}

func TestConformance_TheSharedCredentialsFileIsTheLastResort(t *testing.T) {
	// The file is read only when nothing more specific was given. It is also the
	// step most likely to be absent, malformed, or half filled in, and none of
	// those may be mistaken for a credential.
	for _, tc := range []struct {
		name    string
		content string
		access  string
		secret  string
		ok      bool
	}{
		{
			"a complete default profile",
			"[default]\naws_access_key_id = AKIAGOPHER\naws_secret_access_key = s3cr3t\n",
			"AKIAGOPHER", "s3cr3t", true,
		},
		{
			"no file at all",
			"", "", "", false,
		},
		{
			"a file with no default profile",
			"[prod]\naws_access_key_id = AKIAGOPHER\naws_secret_access_key = s3cr3t\n",
			"", "", false,
		},
		{
			"a default profile with only half a pair",
			"[default]\naws_access_key_id = AKIAGOPHER\n",
			"", "", false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearAWSEnv(t)
			awsHome(t, tc.content)

			got, err := Provider{}.Resolve()
			switch {
			case tc.ok && err != nil:
				t.Fatalf("could not resolve a credential: %v", err)
			case !tc.ok && err == nil:
				t.Fatalf("an incomplete configuration resolved to %+v", got)
			case !tc.ok:
				if !strings.Contains(err.Error(), "no complete S3 credential pair") {
					t.Fatalf("the failure does not say what is missing: %v", err)
				}
				return
			}
			if got.AccessKey != tc.access || got.Secret != tc.secret {
				t.Fatalf("resolved %+v, want %q/%q", got, tc.access, tc.secret)
			}
		})
	}
}

func TestConformance_TheMoreExplicitCredentialWins(t *testing.T) {
	// All three sources hold a different pair. Resolving to anything but the
	// most explicit one available means a job cannot override what its host
	// happens to have configured.
	const (
		fileContent = "[default]\naws_access_key_id = FROM_FILE\naws_secret_access_key = file\n"
	)

	t.Run("the explicit fields win", func(t *testing.T) {
		clearAWSEnv(t)
		awsHome(t, fileContent)
		t.Setenv("AWS_ACCESS_KEY_ID", "FROM_ENV")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "env")

		got, err := Provider{AccessKey: "EXPLICIT", Secret: "given"}.Resolve()
		if err != nil {
			t.Fatalf("could not resolve a credential: %v", err)
		}
		if got.AccessKey != "EXPLICIT" {
			t.Fatalf("resolved %q", got.AccessKey)
		}
	})

	t.Run("then the environment", func(t *testing.T) {
		clearAWSEnv(t)
		awsHome(t, fileContent)
		t.Setenv("AWS_ACCESS_KEY_ID", "FROM_ENV")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "env")

		got, err := Provider{}.Resolve()
		if err != nil {
			t.Fatalf("could not resolve a credential: %v", err)
		}
		if got.AccessKey != "FROM_ENV" {
			t.Fatalf("resolved %q", got.AccessKey)
		}
	})

	t.Run("then the file", func(t *testing.T) {
		clearAWSEnv(t)
		awsHome(t, fileContent)

		got, err := Provider{}.Resolve()
		if err != nil {
			t.Fatalf("could not resolve a credential: %v", err)
		}
		if got.AccessKey != "FROM_FILE" {
			t.Fatalf("resolved %q", got.AccessKey)
		}
	})

	t.Run("half an explicit pair is not explicit", func(t *testing.T) {
		// A caller that filled in only the access key has not configured
		// anything; falling back is what lets it be completed elsewhere.
		clearAWSEnv(t)
		awsHome(t, fileContent)

		got, err := Provider{AccessKey: "EXPLICIT"}.Resolve()
		if err != nil {
			t.Fatalf("could not resolve a credential: %v", err)
		}
		if got.AccessKey != "FROM_FILE" {
			t.Fatalf("resolved %q", got.AccessKey)
		}
	})
}
