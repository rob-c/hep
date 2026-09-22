// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command hep-kernel is a Jupyter kernel running Go with go-hep loaded.
//
// It is the session hep-shell gives you at a terminal, behind a notebook: the
// same interpreter, the same packages imported before the first cell, the
// same rule about an expression printing its value.
//
//	In [1]: h := hbook.NewH1D(100, -5, 5)
//	In [2]: for i := 0; i < 1000; i++ { h.Fill(rnd.NormFloat64(), 1) }
//	In [3]: h.XMean()
//	(float64) 0.021
//
// # Installing it
//
//	go install go-hep.org/x/hep/cmd/hep-kernel@latest
//	hep-kernel -install
//
// which writes a kernel spec where Jupyter looks for one. After that "Go
// (go-hep)" is in the kernel list.
//
// # What it speaks
//
// Version 5.3 of the Jupyter messaging protocol, over ZeroMQ, in pure Go:
// the sockets are github.com/go-zeromq/zmq4, which needs no libzmq. Every
// message is signed with the HMAC key from the connection file, and one that
// does not verify is dropped rather than answered — that signature is the
// only thing between this process and anything else that can reach the port.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
)

func main() {
	var (
		install = flag.Bool("install", false, "install the kernel spec and leave")
		prefix  = flag.String("prefix", "", "where to install the kernel spec (default: the user's Jupyter directory)")
	)
	flag.Parse()

	if *install {
		dir, err := installKernel(*prefix)
		if err != nil {
			fmt.Fprintf(os.Stderr, "hep-kernel: could not install: %+v\n", err)
			os.Exit(1)
		}
		fmt.Printf("installed the go-hep kernel in %s\n", dir)
		return
	}

	if flag.NArg() != 1 {
		fmt.Fprintf(os.Stderr, `hep-kernel is started by Jupyter, not by hand.

To make it available:
	hep-kernel -install

To run it by hand anyway, give it a connection file:
	hep-kernel connection.json
`)
		os.Exit(2)
	}

	err := run(flag.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "hep-kernel: %+v\n", err)
		os.Exit(1)
	}
}

// connection is the file Jupyter writes to say where to listen and how to
// sign.
type connection struct {
	Transport       string `json:"transport"`
	IP              string `json:"ip"`
	ShellPort       int    `json:"shell_port"`
	IOPubPort       int    `json:"iopub_port"`
	StdinPort       int    `json:"stdin_port"`
	ControlPort     int    `json:"control_port"`
	HeartbeatPort   int    `json:"hb_port"`
	Key             string `json:"key"`
	SignatureScheme string `json:"signature_scheme"`
}

func (c connection) addr(port int) string {
	return fmt.Sprintf("%s://%s:%d", c.Transport, c.IP, port)
}

func readConnection(fname string) (connection, error) {
	var c connection

	raw, err := os.ReadFile(fname)
	if err != nil {
		return c, fmt.Errorf("could not read the connection file: %w", err)
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, fmt.Errorf("could not parse the connection file: %w", err)
	}

	switch c.SignatureScheme {
	case "", "hmac-sha256":
		// the only one this speaks, and the only one in use.
	default:
		return c, fmt.Errorf(
			"connection file asks for the %q signature scheme, which this kernel does not speak",
			c.SignatureScheme,
		)
	}

	return c, nil
}

// kernelSpec is the file Jupyter reads to learn that this kernel exists.
type kernelSpec struct {
	Argv        []string `json:"argv"`
	DisplayName string   `json:"display_name"`
	Language    string   `json:"language"`
}

// installKernel writes a kernel spec where Jupyter will find it.
func installKernel(prefix string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("could not find this executable: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return "", err
	}

	if prefix == "" {
		prefix, err = jupyterDir()
		if err != nil {
			return "", err
		}
	}

	dir := filepath.Join(prefix, "kernels", "gohep")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("could not make %s: %w", dir, err)
	}

	spec := kernelSpec{
		// {connection_file} is what Jupyter replaces with the path to it.
		Argv:        []string{exe, "{connection_file}"},
		DisplayName: "Go (go-hep)",
		Language:    "go",
	}

	raw, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return "", err
	}

	err = os.WriteFile(filepath.Join(dir, "kernel.json"), append(raw, '\n'), 0644)
	if err != nil {
		return "", fmt.Errorf("could not write the kernel spec: %w", err)
	}

	return dir, nil
}

// jupyterDir returns the per-user directory Jupyter keeps kernels in.
func jupyterDir() (string, error) {
	if dir := os.Getenv("JUPYTER_DATA_DIR"); dir != "" {
		return dir, nil
	}

	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("could not find the current user: %w", err)
	}

	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(u.HomeDir, "Library", "Jupyter"), nil
	case "windows":
		if dir := os.Getenv("APPDATA"); dir != "" {
			return filepath.Join(dir, "jupyter"), nil
		}
		return filepath.Join(u.HomeDir, "AppData", "Roaming", "jupyter"), nil
	default:
		if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
			return filepath.Join(dir, "jupyter"), nil
		}
		return filepath.Join(u.HomeDir, ".local", "share", "jupyter"), nil
	}
}
