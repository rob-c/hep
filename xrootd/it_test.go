// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdhttp"
)

// TestIntegrationRealServer launches a real XRootD server with the grid PKI and
// verifies the go-hep client against it. It is gated by XROOTD_IT=1 and skips
// when xrootd/openssl are unavailable, so it never runs in the normal suite.
//
//	XROOTD_IT=1 go test ./xrootd/ -run TestIntegrationRealServer -v
func TestIntegrationRealServer(t *testing.T) {
	if os.Getenv("XROOTD_IT") != "1" {
		t.Skip("set XROOTD_IT=1 to run the real-XRootD integration test")
	}
	for _, bin := range []string{"xrootd", "openssl"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not found in PATH: %v", bin, err)
		}
	}
	httpLib := firstExisting(
		"/usr/lib64/libXrdHttp-5.so",
		"/usr/lib/libXrdHttp-5.so",
		"/usr/lib/x86_64-linux-gnu/libXrdHttp-5.so",
	)
	if httpLib == "" {
		t.Skip("libXrdHttp-5.so not found")
	}

	dir := t.TempDir()
	pki := buildGridPKI(t, dir)

	// Seed a data file served over both root:// and https.
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const payload = "go-hep xrootd integration payload\n"
	if err := os.WriteFile(filepath.Join(dataDir, "hello.txt"), []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	rootPort := freePort(t)
	httpPort := freePort(t)
	cfg := writeXrootdConfig(t, dir, xrootdParams{
		dataDir:    dataDir,
		adminDir:   mkTmpDir(t, dir, "admin"),
		runDir:     mkTmpDir(t, dir, "run"),
		rootPort:   rootPort,
		httpPort:   httpPort,
		httpLib:    httpLib,
		serverCert: pki.serverCert,
		serverKey:  pki.serverKey,
		caDir:      pki.caDir,
	})

	launchXrootd(t, cfg, dir, rootPort, httpPort)

	t.Run("root-anon", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		c, err := NewClient(ctx, fmt.Sprintf("localhost:%d", rootPort), "gopher")
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		defer c.Close()
		fs := c.FS()
		fi, err := fs.Stat(ctx, "/hello.txt")
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if fi.Size() != int64(len(payload)) {
			t.Fatalf("size: got=%d want=%d", fi.Size(), len(payload))
		}
		f, err := fs.Open(ctx, "/hello.txt", xrdfs.OpenModeOwnerRead, xrdfs.OpenOptionsOpenRead)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer f.Close(ctx)
		buf := make([]byte, fi.Size())
		if _, err := f.ReadAt(buf, 0); err != nil {
			t.Fatalf("ReadAt: %v", err)
		}
		if string(buf) != payload {
			t.Fatalf("root:// content mismatch: %q", buf)
		}
	})

	t.Run("xrdhttps-x509", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		caPEM, err := os.ReadFile(pki.caCert)
		if err != nil {
			t.Fatal(err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			t.Fatal("could not add CA to pool")
		}
		cert, err := tls.LoadX509KeyPair(pki.userCert, pki.userKey)
		if err != nil {
			t.Fatalf("load user cert: %v", err)
		}

		c, err := xrdhttp.Dial(fmt.Sprintf("https://localhost:%d", httpPort),
			xrdhttp.WithRootCAs(pool),
			xrdhttp.WithClientCertificate(cert),
		)
		if err != nil {
			t.Fatalf("xrdhttp.Dial: %v", err)
		}
		got, err := c.ReadAll(ctx, "/hello.txt")
		if err != nil {
			t.Fatalf("https ReadAll: %v", err)
		}
		if string(got) != payload {
			t.Fatalf("https content mismatch: %q", got)
		}
	})

	t.Run("root-gsi", func(t *testing.T) {
		t.Skip("root://+gsi requires the GSI client (Phase 3b), not yet implemented")
	})
}

type xrootdParams struct {
	dataDir, adminDir, runDir    string
	rootPort, httpPort           int
	httpLib                      string
	serverCert, serverKey, caDir string
}

func writeXrootdConfig(t *testing.T, dir string, p xrootdParams) string {
	t.Helper()
	cfg := fmt.Sprintf(`all.role server
all.export /
oss.localroot %[1]s
all.adminpath %[2]s
all.pidpath %[3]s
xrd.port %[4]d
xrd.protocol XrdHttp:%[5]d %[6]s
http.cert %[7]s
http.key %[8]s
http.cadir %[9]s
http.desthttps no
http.selfhttps2http no
`,
		p.dataDir, p.adminDir, p.runDir, p.rootPort,
		p.httpPort, p.httpLib, p.serverCert, p.serverKey, p.caDir)
	path := filepath.Join(dir, "xrootd.cfg")
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func launchXrootd(t *testing.T, cfg, dir string, ports ...int) {
	t.Helper()
	logf := filepath.Join(dir, "xrootd.log")
	lw, err := os.Create(logf)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("xrootd", "-c", cfg, "-n", "it")
	cmd.Stdout = lw
	cmd.Stderr = lw
	if err := cmd.Start(); err != nil {
		t.Fatalf("start xrootd: %v", err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		lw.Close()
		if t.Failed() {
			if b, err := os.ReadFile(logf); err == nil {
				t.Logf("xrootd log:\n%s", b)
			}
		}
	})

	deadline := time.Now().Add(20 * time.Second)
	for _, port := range ports {
		for {
			if time.Now().After(deadline) {
				b, _ := os.ReadFile(logf)
				t.Fatalf("xrootd did not open port %d in time\nlog:\n%s", port, b)
			}
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
			if err == nil {
				conn.Close()
				break
			}
			time.Sleep(150 * time.Millisecond)
		}
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func mkTmpDir(t *testing.T, base, name string) string {
	t.Helper()
	p := filepath.Join(base, name)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
