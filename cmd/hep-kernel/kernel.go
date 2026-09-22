// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/go-zeromq/zmq4"

	"go-hep.org/x/hep/cmd/internal/hepsh"
)

// kernel is a running Jupyter kernel.
type kernel struct {
	conn connection
	key  []byte

	shell   zmq4.Socket
	control zmq4.Socket
	iopub   zmq4.Socket
	stdin   zmq4.Socket
	hb      zmq4.Socket

	sess *hepsh.Session

	// out collects what the interpreted code prints, so that it can be sent
	// as the cell's output rather than to this process' own stdout.
	out bytes.Buffer
	err bytes.Buffer

	// mu keeps two requests from being executed at once. A notebook may
	// send an interrupt on the control socket while the shell socket is
	// busy, and the session is not safe to use from two goroutines.
	mu sync.Mutex

	count int // how many cells have been executed
}

func run(fname string) error {
	conn, err := readConnection(fname)
	if err != nil {
		return err
	}

	k := &kernel{conn: conn, key: []byte(conn.Key)}

	k.sess, err = hepsh.New(&k.out, &k.err)
	if err != nil {
		return fmt.Errorf("could not start the session: %w", err)
	}

	err = k.listen()
	if err != nil {
		return err
	}
	defer k.close()

	ctx := context.Background()

	// the heartbeat is a bare echo, and is how a client tells whether this
	// process is still alive.
	go k.beat()

	// the control socket carries shutdown and interrupt, and is read apart
	// from the shell socket so that it answers even while a cell runs.
	go k.serve(ctx, k.control)

	k.serve(ctx, k.shell)
	return nil
}

func (k *kernel) listen() error {
	ctx := context.Background()

	for _, s := range []struct {
		sock *zmq4.Socket
		new  func() zmq4.Socket
		port int
		name string
	}{
		{&k.shell, func() zmq4.Socket { return zmq4.NewRouter(ctx) }, k.conn.ShellPort, "shell"},
		{&k.control, func() zmq4.Socket { return zmq4.NewRouter(ctx) }, k.conn.ControlPort, "control"},
		{&k.stdin, func() zmq4.Socket { return zmq4.NewRouter(ctx) }, k.conn.StdinPort, "stdin"},
		{&k.iopub, func() zmq4.Socket { return zmq4.NewPub(ctx) }, k.conn.IOPubPort, "iopub"},
		{&k.hb, func() zmq4.Socket { return zmq4.NewRep(ctx) }, k.conn.HeartbeatPort, "heartbeat"},
	} {
		sock := s.new()
		err := sock.Listen(k.conn.addr(s.port))
		if err != nil {
			return fmt.Errorf("could not listen on the %s socket: %w", s.name, err)
		}
		*s.sock = sock
	}

	return nil
}

func (k *kernel) close() {
	for _, s := range []zmq4.Socket{k.shell, k.control, k.stdin, k.iopub, k.hb} {
		if s != nil {
			_ = s.Close()
		}
	}
}

// beat echoes whatever arrives, which is all a heartbeat is.
func (k *kernel) beat() {
	for {
		msg, err := k.hb.Recv()
		if err != nil {
			return
		}
		if err := k.hb.Send(msg); err != nil {
			return
		}
	}
}

// serve reads requests off a socket and answers them until it closes.
func (k *kernel) serve(ctx context.Context, sock zmq4.Socket) {
	for {
		raw, err := sock.Recv()
		if err != nil {
			return
		}

		msg, err := decodeMessage(raw.Frames, k.key)
		if err != nil {
			// a message that does not verify is dropped without an answer:
			// answering it would be answering whoever forged it.
			fmt.Fprintf(os.Stderr, "hep-kernel: %+v\n", err)
			continue
		}

		if k.handle(sock, msg) {
			return
		}
	}
}

// handle answers one request, and says whether the kernel should stop.
func (k *kernel) handle(sock zmq4.Socket, msg *message) bool {
	switch msg.Header.MsgType {
	case "kernel_info_request":
		k.send(sock, msg.reply("kernel_info_reply", kernelInfo()))

	case "execute_request":
		k.execute(sock, msg)

	case "is_complete_request":
		code, _ := msg.Content["code"].(string)
		status := "complete"
		if hepsh.Unbalanced(code) {
			status = "incomplete"
		}
		k.send(sock, msg.reply("is_complete_reply", map[string]any{
			"status": status,
			"indent": "",
		}))

	case "shutdown_request":
		restart, _ := msg.Content["restart"].(bool)
		k.send(sock, msg.reply("shutdown_reply", map[string]any{"restart": restart}))
		return true

	case "interrupt_request":
		// There is nothing to interrupt: the interpreter runs a cell to its
		// end. Saying so is better than not replying, which hangs the client.
		k.send(sock, msg.reply("interrupt_reply", map[string]any{"status": "ok"}))

	case "comm_info_request":
		k.send(sock, msg.reply("comm_info_reply", map[string]any{"comms": map[string]any{}}))

	case "complete_request":
		// no completion yet: an empty list is the protocol's way of saying
		// there is nothing to offer.
		code, _ := msg.Content["code"].(string)
		pos, _ := msg.Content["cursor_pos"].(float64)
		k.send(sock, msg.reply("complete_reply", map[string]any{
			"status":       "ok",
			"matches":      []string{},
			"cursor_start": int(pos),
			"cursor_end":   int(pos),
			"metadata":     map[string]any{},
		}))
		_ = code
	}

	return false
}

func kernelInfo() map[string]any {
	return map[string]any{
		"status":                 "ok",
		"protocol_version":       protocolVersion,
		"implementation":         "hep-kernel",
		"implementation_version": "0.1.0",
		"banner": "Go with go-hep, interpreted. " +
			"hbook, hplot, groot, rtree, rhist, fit and minuit are already imported.",
		"language_info": map[string]any{
			"name":           "go",
			"version":        "1.x",
			"mimetype":       "text/x-go",
			"file_extension": ".go",
		},
		"help_links": []map[string]any{
			{"text": "go-hep", "url": "https://go-hep.org"},
			{"text": "ROOT to go-hep", "url": "https://github.com/rob-c/hep/blob/main/ROOT-TO-GO-HEP.md"},
		},
	}
}

// execute runs a cell and reports what it did.
func (k *kernel) execute(sock zmq4.Socket, msg *message) {
	k.mu.Lock()
	defer k.mu.Unlock()

	code, _ := msg.Content["code"].(string)
	silent, _ := msg.Content["silent"].(bool)

	k.publish(msg, "status", map[string]any{"execution_state": "busy"})
	defer k.publish(msg, "status", map[string]any{"execution_state": "idle"})

	if strings.TrimSpace(code) == "" {
		k.send(sock, msg.reply("execute_reply", map[string]any{
			"status":           "ok",
			"execution_count":  k.count,
			"payload":          []any{},
			"user_expressions": map[string]any{},
		}))
		return
	}

	k.count++
	if !silent {
		k.publish(msg, "execute_input", map[string]any{
			"code":            code,
			"execution_count": k.count,
		})
	}

	k.out.Reset()
	k.err.Reset()

	val, err := k.sess.Eval(code)

	// whatever the cell printed goes out first, in the order it was printed.
	if s := k.out.String(); s != "" {
		k.publish(msg, "stream", map[string]any{"name": "stdout", "text": s})
	}
	if s := k.err.String(); s != "" {
		k.publish(msg, "stream", map[string]any{"name": "stderr", "text": s})
	}

	if err != nil {
		k.publish(msg, "error", map[string]any{
			"ename":     "error",
			"evalue":    err.Error(),
			"traceback": []string{err.Error()},
		})
		k.send(sock, msg.reply("execute_reply", map[string]any{
			"status":          "error",
			"execution_count": k.count,
			"ename":           "error",
			"evalue":          err.Error(),
			"traceback":       []string{err.Error()},
		}))
		return
	}

	if val != "" && !silent {
		k.publish(msg, "execute_result", map[string]any{
			"execution_count": k.count,
			"data":            map[string]any{"text/plain": val},
			"metadata":        map[string]any{},
		})
	}

	k.send(sock, msg.reply("execute_reply", map[string]any{
		"status":           "ok",
		"execution_count":  k.count,
		"payload":          []any{},
		"user_expressions": map[string]any{},
	}))
}

// publish broadcasts a message on the iopub socket, which is where a client
// looks for output, and where every client watching this kernel sees it.
func (k *kernel) publish(parent *message, kind string, content map[string]any) {
	msg := parent.reply(kind, content)
	// on iopub the routing prefix is the message kind, which is what lets a
	// client subscribe to some kinds and not others.
	msg.IDs = [][]byte{[]byte(kind)}
	k.send(k.iopub, msg)
}

func (k *kernel) send(sock zmq4.Socket, msg *message) {
	raw, err := msg.encode(k.key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hep-kernel: could not encode a %s: %+v\n", msg.Header.MsgType, err)
		return
	}
	if err := sock.Send(raw); err != nil {
		fmt.Fprintf(os.Stderr, "hep-kernel: could not send a %s: %+v\n", msg.Header.MsgType, err)
	}
}
