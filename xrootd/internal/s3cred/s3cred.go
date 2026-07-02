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
