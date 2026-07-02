// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

	return pki
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
