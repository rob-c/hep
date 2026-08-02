// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rcont

import (
	"testing"

	"go-hep.org/x/hep/groot/rbase"
	"go-hep.org/x/hep/groot/root"
)

func TestListLast(t *testing.T) {
	list := NewList("list", nil)
	if got, want := list.Last(), -1; got != want {
		t.Errorf("empty list: got last=%d, want %d", got, want)
	}
	list.Append(rbase.NewNamed("a", "a"))
	list.Append(rbase.NewNamed("b", "b"))
	if got, want := list.Last(), 1; got != want {
		t.Errorf("got last=%d, want %d", got, want)
	}
	if got, want := list.At(list.Last()).(root.Named).Name(), "b"; got != want {
		t.Errorf("got last element %q, want %q", got, want)
	}
}
