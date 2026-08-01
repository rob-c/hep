// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !windows

package krb5

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// configCandidates lists where a Kerberos configuration is kept when
// $KRB5_CONFIG does not say, most likely first.
var configCandidates = []string{
	"/etc/krb5.conf",
	"/etc/krb5/krb5.conf",
}

func cachePath() string {
	if v := os.Getenv("KRB5CCNAME"); v != "" {
		if strings.HasPrefix(v, "FILE:") {
			v = string(v[len("FILE:"):])
		}
		return v
	}

	usr, err := user.Current()
	if err != nil {
		return ""
	}

	v := filepath.Join(os.TempDir(), fmt.Sprintf("krb5cc_%s", usr.Uid))
	return v
}
