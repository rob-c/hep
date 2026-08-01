// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

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
	`c:\winnt\krb5.ini`,
	`c:\windows\krb5.ini`,
	`c:\ProgramData\MIT\Kerberos5\krb5.ini`,
}

func cachePath() string {
	// Windows has no agreed-upon cache location the way Unix does: Kerberos
	// for Windows keeps its cache behind an API rather than in a file, and
	// MIT ships no current build. KRB5CCNAME is therefore the supported way
	// to point this client at a cache on Windows, with the Unix convention
	// under %TEMP% as the only fallback worth guessing.
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
