// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gpfile

import (
	"errors"
	"strings"
	"testing"
)

func TestRequestIDMatchesTheProtocol(t *testing.T) {
	// kXR_gpfile is 3005 whether or not anything answers it, and the number is
	// what a decoder of somebody else's traffic needs to name the opcode.
	if RequestID != 3005 {
		t.Fatalf("kXR_gpfile is %d, want 3005", RequestID)
	}
}

func TestRequestRefusesRatherThanGuessing(t *testing.T) {
	err := Request()
	if err == nil {
		t.Fatal("kXR_gpfile was accepted, want a refusal")
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("the failure is %v, want ErrUnsupported", err)
	}
	// A caller that already handles "this server cannot do that" has to handle
	// this without knowing this package exists.
	if !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("the failure is %v, want it to wrap errors.ErrUnsupported", err)
	}
	if !strings.Contains(err.Error(), "readv") {
		t.Fatalf("the failure says %q, want it to name the replacement", err)
	}
}
