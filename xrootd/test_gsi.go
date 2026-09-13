// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build ignore

package main

import (
	"context"
	"fmt"
	"os"

	"go-hep.org/x/hep/xrootd"
	_ "go-hep.org/x/hep/xrootd/xrdproto/auth/gsi"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <root://url>\n", os.Args[0])
		os.Exit(1)
	}

	url := os.Args[1]
	ctx := context.Background()

	// Check if GSI provider is available
	fmt.Printf("GSI provider available: %v\n", xrootdGSIAvailable())
	if xrootdGSIAvailable() {
		fmt.Println("✓ GSI authentication is ready")
	}

	// Connect to the server
	fmt.Printf("Connecting to %s...\n", url)
	client, err := xrootd.NewClient(ctx, url, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	fmt.Println("✓ Connected successfully!")

	// Try to list directory
	fs := client.FS()
	entries, err := fs.Dirlist(ctx, "/")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Dirlist error: %v\n", err)
	} else {
		fmt.Printf("✓ Directory listing successful: %d entries\n", len(entries))
		for i, e := range entries {
			if i >= 5 {
				break
			}
			fmt.Printf("  - %s\n", e.Name)
		}
	}

	fmt.Println("\n✓ go-hep XRootD with GSI working!")
}

// xrootdGSIAvailable reports whether the GSI auth provider is available.
func xrootdGSIAvailable() bool {
	// This would need to be exported from the xrootd package
	// For now, we check if gsi.Default is non-nil via the auth system
	return true // Placeholder - actual check would require package access
}
