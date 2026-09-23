// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package riofs

import "fmt"

// WriteBlob writes raw bytes into the file as a key that no directory lists,
// and returns the offset the bytes themselves landed at.
//
// ROOT calls these RBlob keys. They are how an RNTuple's pages and envelopes
// are kept: nothing looks them up by name, and everything that refers to one
// does so by the offset this returns.
//
// The bytes are written exactly as given. Whatever compression they want has
// to be applied before they get here, since what refers to them records the
// size they take on disk.
func WriteBlob(f *File, data []byte) (int64, error) {
	if f == nil {
		return 0, fmt.Errorf("riofs: no file to write a blob into")
	}

	k, err := newKeyFromBuf(
		&f.dir, "", "", "RBlob", 1, data, f,
		[]KeyOption{WithKeyCompression(0)},
	)
	if err != nil {
		return 0, fmt.Errorf("riofs: could not make a blob key: %w", err)
	}

	_, err = k.writeFile(f)
	if err != nil {
		return 0, fmt.Errorf("riofs: could not write a blob: %w", err)
	}

	return k.seekkey + int64(k.keylen), nil
}
