// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// xrd-ex-24-s3 uses the same client surface against S3 object storage.
//
// Credentials resolve in the order every AWS client uses -- explicit fields,
// then $AWS_ACCESS_KEY_ID/$AWS_SECRET_ACCESS_KEY, then the [default] profile of
// ~/.aws/credentials. It is an ORDER rather than a search: each step is more
// explicit than the last, and a client that reversed it would ignore the
// credentials a job was launched with and sign with whatever was left in a
// developer's home directory.
package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"time"

	"go-hep.org/x/hep/xrootd/xrds3"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	creds, err := xrds3.Provider{}.Resolve()
	if err != nil {
		log.Fatalf("could not resolve S3 credentials: %+v", err)
	}

	cli := xrds3.New("https://s3.example.org", "my-bucket", creds,
		xrds3.WithRegion("us-east-1"),
	)

	body := []byte("hello from go-hep\n")
	if err := cli.Put(ctx, "analysis/note.txt", bytes.NewReader(body), int64(len(body))); err != nil {
		log.Fatalf("could not put: %+v", err)
	}

	size, exists, err := cli.Stat(ctx, "analysis/note.txt")
	if err != nil {
		log.Fatalf("could not stat: %+v", err)
	}
	fmt.Printf("exists=%v size=%d\n", exists, size)

	// A ranged read, as over WebDAV.
	buf := make([]byte, 5)
	n, err := cli.ReadAt(ctx, buf, "analysis/note.txt", 0)
	if err != nil {
		log.Fatalf("could not read: %+v", err)
	}
	fmt.Printf("read %q (%d bytes)\n", buf[:n], n)

	if err := cli.Remove(ctx, "analysis/note.txt"); err != nil {
		log.Fatalf("could not remove: %+v", err)
	}
}
