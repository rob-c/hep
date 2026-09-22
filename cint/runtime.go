// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cint

import "runtime"

func runtimeVersion() string { return runtime.Version() }

func callerFile() (uintptr, string, int, bool) { return runtime.Caller(0) }
