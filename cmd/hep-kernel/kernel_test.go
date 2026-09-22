// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSignRoundTrip checks a message this kernel encoded is one it will
// accept, signature and all.
func TestSignRoundTrip(t *testing.T) {
	key := []byte("a-secret-from-the-connection-file")

	msg := &message{
		IDs:      [][]byte{[]byte("routing")},
		Header:   msgHeader{MsgID: "1", MsgType: "execute_request", Version: protocolVersion},
		Parent:   msgHeader{},
		Metadata: map[string]any{"a": "b"},
		Content:  map[string]any{"code": "40 + 2"},
	}

	raw, err := msg.encode(key)
	if err != nil {
		t.Fatalf("could not encode: %+v", err)
	}

	got, err := decodeMessage(raw.Frames, key)
	if err != nil {
		t.Fatalf("could not decode what we encoded: %+v", err)
	}

	if got.Header.MsgType != msg.Header.MsgType {
		t.Errorf("msg type: got=%q, want=%q", got.Header.MsgType, msg.Header.MsgType)
	}
	if got.Content["code"] != "40 + 2" {
		t.Errorf("content: got=%v", got.Content)
	}
	if len(got.IDs) != 1 || string(got.IDs[0]) != "routing" {
		t.Errorf("routing prefix: got=%q", got.IDs)
	}
}

// TestBadSignatureIsRefused checks a message signed with the wrong key is
// refused. That signature is the only thing between this process and anything
// else that can reach the port, so it has to be checked and not merely
// computed.
func TestBadSignatureIsRefused(t *testing.T) {
	msg := &message{
		Header:   msgHeader{MsgID: "1", MsgType: "execute_request"},
		Metadata: map[string]any{},
		Content:  map[string]any{"code": "os.Exit(1)"},
	}

	raw, err := msg.encode([]byte("the-wrong-key"))
	if err != nil {
		t.Fatalf("could not encode: %+v", err)
	}

	_, err = decodeMessage(raw.Frames, []byte("the-right-key"))
	if err == nil {
		t.Fatal("a message signed with the wrong key was accepted")
	}
	if !strings.Contains(err.Error(), "signature") {
		t.Errorf("error %q does not mention the signature", err)
	}
}

// TestUnsignedConnection checks that a connection file with no key at all --
// which Jupyter allows -- skips the check rather than failing it.
func TestUnsignedConnection(t *testing.T) {
	msg := &message{
		Header:   msgHeader{MsgID: "1", MsgType: "kernel_info_request"},
		Metadata: map[string]any{},
		Content:  map[string]any{},
	}

	raw, err := msg.encode(nil)
	if err != nil {
		t.Fatalf("could not encode: %+v", err)
	}
	if _, err := decodeMessage(raw.Frames, nil); err != nil {
		t.Fatalf("an unsigned message was refused: %+v", err)
	}
}

func TestNoDelimiter(t *testing.T) {
	_, err := decodeMessage([][]byte{[]byte("a"), []byte("b")}, nil)
	if err == nil {
		t.Fatal("a message with no delimiter was accepted")
	}
}

func TestReadConnection(t *testing.T) {
	dir := t.TempDir()
	fname := filepath.Join(dir, "connection.json")

	err := os.WriteFile(fname, []byte(`{
		"transport": "tcp",
		"ip": "127.0.0.1",
		"shell_port": 1,
		"iopub_port": 2,
		"stdin_port": 3,
		"control_port": 4,
		"hb_port": 5,
		"key": "abc",
		"signature_scheme": "hmac-sha256"
	}`), 0644)
	if err != nil {
		t.Fatalf("could not write the connection file: %+v", err)
	}

	c, err := readConnection(fname)
	if err != nil {
		t.Fatalf("could not read it: %+v", err)
	}

	if got, want := c.addr(c.ShellPort), "tcp://127.0.0.1:1"; got != want {
		t.Errorf("address: got=%q, want=%q", got, want)
	}
	if got, want := c.Key, "abc"; got != want {
		t.Errorf("key: got=%q, want=%q", got, want)
	}

	t.Run("unknown scheme", func(t *testing.T) {
		bad := filepath.Join(dir, "bad.json")
		err := os.WriteFile(bad, []byte(`{"signature_scheme":"hmac-md5"}`), 0644)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := readConnection(bad); err == nil {
			t.Fatal("a signature scheme this kernel cannot speak was accepted")
		}
	})
}

// TestInstall checks the kernel spec written is one Jupyter can read, and
// that it points at this executable.
func TestInstall(t *testing.T) {
	dir := t.TempDir()

	got, err := installKernel(dir)
	if err != nil {
		t.Fatalf("could not install: %+v", err)
	}

	raw, err := os.ReadFile(filepath.Join(got, "kernel.json"))
	if err != nil {
		t.Fatalf("could not read the spec: %+v", err)
	}

	var spec kernelSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("the spec is not valid JSON: %+v", err)
	}

	if got, want := spec.Language, "go"; got != want {
		t.Errorf("language: got=%q, want=%q", got, want)
	}
	if got, want := spec.DisplayName, "Go (go-hep)"; got != want {
		t.Errorf("display name: got=%q, want=%q", got, want)
	}
	if len(spec.Argv) != 2 || spec.Argv[1] != "{connection_file}" {
		t.Errorf("argv: got=%v, want it to end in {connection_file}", spec.Argv)
	}
	if !filepath.IsAbs(spec.Argv[0]) {
		t.Errorf("argv[0]=%q is not an absolute path", spec.Argv[0])
	}
}

// TestKernelInfo checks the reply a client reads first is well formed.
func TestKernelInfo(t *testing.T) {
	info := kernelInfo()

	if got, want := info["protocol_version"], protocolVersion; got != want {
		t.Errorf("protocol version: got=%v, want=%v", got, want)
	}
	if got, want := info["status"], "ok"; got != want {
		t.Errorf("status: got=%v, want=%v", got, want)
	}

	lang, ok := info["language_info"].(map[string]any)
	if !ok {
		t.Fatalf("no language_info")
	}
	if got, want := lang["name"], "go"; got != want {
		t.Errorf("language: got=%v, want=%v", got, want)
	}

	// it has to survive being turned into JSON, which is how it travels.
	if _, err := json.Marshal(info); err != nil {
		t.Fatalf("kernel info does not marshal: %+v", err)
	}
}

// TestExecuteThroughTheSession checks the kernel's session runs Go and keeps
// what it was told, which is what a notebook is for.
func TestExecuteThroughTheSession(t *testing.T) {
	k := &kernel{}

	var err error
	k.sess, err = newTestSession(&k.out, &k.err)
	if err != nil {
		t.Fatalf("could not start a session: %+v", err)
	}

	for _, tc := range []struct {
		src  string
		want string
	}{
		{"h := hbook.NewH1D(10, 0, 10)", ""},
		{"for i := 0; i < 50; i++ { h.Fill(float64(i%10)+0.5, 1) }", ""},
		{"h.Entries()", "(int64) 50"},
		// 50 entries at the bin centres 0.5 .. 9.5, five at each: the
		// mean of those ten centres is 5.
		{"h.XMean()", "(float64) 5"},
	} {
		got, err := k.sess.Eval(tc.src)
		if err != nil {
			t.Fatalf("%q: %+v", tc.src, err)
		}
		if got != tc.want {
			t.Errorf("%q: got=%q, want=%q", tc.src, got, tc.want)
		}
	}
}
