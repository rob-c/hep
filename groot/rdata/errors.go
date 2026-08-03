// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rdata

import (
	"fmt"
	"strings"
)

// Error is what this package returns when something goes wrong. It says what
// was being done, to what, and why — and, when the answer is a name that was
// spelled differently in the file, what the names actually are.
//
// The underlying error is kept, so the standard tests still work:
//
//	if errors.Is(err, fs.ErrNotExist) { ... }
type Error struct {
	Op   string // the operation: "read", "write"
	Name string // the file, tree or column it was attempted on
	Tree string // the tree it belongs to, when that is not the name itself
	Err  error  // what went wrong

	// hint is the sentence somebody meeting this failure for the first time
	// would otherwise have to ask a colleague for.
	hint string

	// cols and trees are what was there instead, for the two failures that
	// are nearly always a name spelled differently than the file spells it.
	cols  []string
	trees []string
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "rdata: could not %s %q", e.Op, e.Name)
	if e.Tree != "" && e.Tree != e.Name {
		fmt.Fprintf(&b, " of tree %q", e.Tree)
	}
	fmt.Fprintf(&b, ": %v", e.Err)

	switch {
	case e.hint != "":
		fmt.Fprintf(&b, " (%s)", e.hint)
	case len(e.cols) > 0:
		fmt.Fprintf(&b, " (the columns are: %s)", names(e.cols))
	case len(e.trees) > 0:
		fmt.Fprintf(&b, " (the trees in it are: %s)", names(e.trees))
	}
	return b.String()
}

func (e *Error) Unwrap() error { return e.Err }

// names lists what a file holds, at a length somebody can still read. A tree
// of two thousand branches is not unusual and printing all of them helps
// nobody.
func names(all []string) string {
	const max = 24
	if len(all) <= max {
		return strings.Join(all, ", ")
	}
	return fmt.Sprintf("%s, … and %d more", strings.Join(all[:max], ", "), len(all)-max)
}
