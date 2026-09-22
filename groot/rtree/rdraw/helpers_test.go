// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rdraw_test

import (
	"fmt"
	"strings"
)

func fmtType(v any) string { return fmt.Sprintf("%T", v) }

func contains(s, sub string) bool { return strings.Contains(s, sub) }
