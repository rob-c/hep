// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gridPKI is a Globus-style test PKI (CA + host cert + user cert) laid out the
// way the reference XRootD server and its GSI/HTTPS security expect: an OpenSSL
// hash directory for the CA plus a signing-policy file. It mirrors the nginx
// test suite's pki_helpers.blitz_test_pki, built here with openssl so the
// harness is self-contained.
type gridPKI struct {
	caDir      string // OpenSSL hash dir containing <hash>.0 and signing-policy
	caCert     string
	caKey      string
	serverCert string
	serverKey  string
	userCert   string
	userKey    string
}

func openssl(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("openssl", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("openssl %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func opensslOut(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("openssl", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("openssl %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// buildGridPKI creates the CA, host and user certificates under dir.
func buildGridPKI(t *testing.T, dir string) gridPKI {
	t.Helper()

	const (
		caSubj   = "/DC=test/DC=xrootd/CN=Test XRootD CA"
		srvSubj  = "/DC=test/DC=xrootd/CN=localhost"
		userSubj = "/DC=test/DC=xrootd/CN=Test User/CN=12345"
	)

	caDir := filepath.Join(dir, "ca")
	for _, d := range []string{caDir, filepath.Join(dir, "server"), filepath.Join(dir, "user")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	pki := gridPKI{
		caDir:      caDir,
		caCert:     filepath.Join(caDir, "ca.pem"),
		caKey:      filepath.Join(caDir, "ca.key"),
		serverCert: filepath.Join(dir, "server", "host.pem"),
		serverKey:  filepath.Join(dir, "server", "host.key"),
		userCert:   filepath.Join(dir, "user", "user.pem"),
		userKey:    filepath.Join(dir, "user", "user.key"),
	}

	// CA: self-signed, key-cert-sign.
	openssl(t, "genrsa", "-out", pki.caKey, "2048")
	openssl(t, "req", "-new", "-x509", "-key", pki.caKey, "-out", pki.caCert,
		"-days", "2", "-sha256", "-subj", caSubj,
		"-addext", "basicConstraints=critical,CA:TRUE",
		"-addext", "keyUsage=critical,keyCertSign,cRLSign")

	// OpenSSL hash-dir links + Globus signing-policy so GSI/HTTPS CA verify works.
	hash := opensslOut(t, "x509", "-in", pki.caCert, "-noout", "-subject_hash")
	if err := os.Symlink("ca.pem", filepath.Join(caDir, hash+".0")); err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
	policy := strings.Join([]string{
		"access_id_CA    X509    '" + caSubj + "'",
		"pos_rights      globus  CA:sign",
		"cond_subjects   globus  '\"/DC=test/DC=xrootd/*\"'",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(caDir, hash+".signing_policy"), []byte(policy), 0o644); err != nil {
		t.Fatal(err)
	}

	// Host cert with SAN covering localhost and the loopback addresses.
	sanCfg := filepath.Join(dir, "server", "san.cnf")
	if err := os.WriteFile(sanCfg,
		[]byte("subjectAltName=DNS:localhost,IP:127.0.0.1,IP:::1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	signCert(t, pki, srvSubj, pki.serverKey, pki.serverCert, sanCfg, filepath.Join(dir, "server", "host.csr"))

	// User cert (end-entity) for x509/gsi client auth.
	signCert(t, pki, userSubj, pki.userKey, pki.userCert, "", filepath.Join(dir, "user", "user.csr"))

	// openssl stamps NotBefore at the current second; wait out that second so a
	// TLS handshake microseconds later never sees a not-yet-valid certificate.
	time.Sleep(2 * time.Second)

	return pki
}

// buildProxy mints an RFC 3820 proxy certificate signed by the user
// certificate: an ephemeral key, subject = user DN + "/CN=<serial>", and a
// critical proxyCertInfo extension so XrdSecgsi accepts it. It writes a combined
// proxy file (proxy cert + key + issuer chain) and returns its path.
func buildProxy(t *testing.T, dir string, pki gridPKI) string {
	t.Helper()
	const userSubj = "/DC=test/DC=xrootd/CN=Test User/CN=12345"

	proxyDir := filepath.Join(dir, "user")
	proxyKey := filepath.Join(proxyDir, "proxykey.pem")
	proxyCert := filepath.Join(proxyDir, "proxycert.pem")
	proxyReq := filepath.Join(proxyDir, "proxy.csr")
	extFile := filepath.Join(proxyDir, "proxy.ext")

	if err := os.WriteFile(extFile, []byte(
		"keyUsage=critical,digitalSignature,keyEncipherment\n"+
			"proxyCertInfo=critical,language:id-ppl-inheritAll\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	openssl(t, "genrsa", "-out", proxyKey, "2048")
	openssl(t, "req", "-new", "-key", proxyKey, "-out", proxyReq,
		"-subj", userSubj+"/CN=1234567")
	openssl(t, "x509", "-req", "-in", proxyReq,
		"-CA", pki.userCert, "-CAkey", pki.userKey, "-CAcreateserial",
		"-out", proxyCert, "-days", "1", "-sha256",
		"-extfile", extFile)

	// Combined proxy: proxy cert, proxy key, then the issuer (user) cert.
	var combined []byte
	for _, p := range []string{proxyCert, proxyKey, pki.userCert} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		combined = append(combined, b...)
	}
	out := filepath.Join(proxyDir, "proxy_std.pem")
	if err := os.WriteFile(out, combined, 0o600); err != nil {
		t.Fatal(err)
	}
	return out
}

// signCert generates a key + CSR for subj and signs it with the CA. When
// extFile is non-empty its extensions (e.g. subjectAltName) are applied.
func signCert(t *testing.T, pki gridPKI, subj, keyOut, certOut, extFile, csr string) {
	t.Helper()
	openssl(t, "genrsa", "-out", keyOut, "2048")
	openssl(t, "req", "-new", "-key", keyOut, "-out", csr, "-subj", subj)

	args := []string{
		"x509", "-req", "-in", csr, "-CA", pki.caCert, "-CAkey", pki.caKey,
		"-CAcreateserial", "-out", certOut, "-days", "2", "-sha256",
	}
	if extFile != "" {
		args = append(args, "-extfile", extFile)
	}
	openssl(t, args...)
}
