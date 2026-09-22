// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-zeromq/zmq4"
	uuid "github.com/hashicorp/go-uuid"
)

// protocolVersion is the Jupyter message protocol this kernel speaks.
const protocolVersion = "5.3"

// delimiter separates the routing prefix of a message from the message
// itself, and is the one fixed thing in the wire format.
const delimiter = "<IDS|MSG>"

// msgHeader is who sent a message, when, and what kind it is.
type msgHeader struct {
	MsgID    string `json:"msg_id"`
	Username string `json:"username"`
	Session  string `json:"session"`
	Date     string `json:"date"`
	MsgType  string `json:"msg_type"`
	Version  string `json:"version"`
}

// message is one Jupyter message, taken apart.
type message struct {
	IDs      [][]byte // the routing prefix, to be echoed back
	Header   msgHeader
	Parent   msgHeader
	Metadata map[string]any
	Content  map[string]any
}

// newMessage starts a reply of the given kind to a message.
func (m *message) reply(kind string, content map[string]any) *message {
	return &message{
		IDs: m.IDs,
		Header: msgHeader{
			MsgID:    newID(),
			Username: m.Header.Username,
			Session:  m.Header.Session,
			Date:     time.Now().UTC().Format(time.RFC3339Nano),
			MsgType:  kind,
			Version:  protocolVersion,
		},
		Parent:   m.Header,
		Metadata: map[string]any{},
		Content:  content,
	}
}

// decodeMessage takes a message off the wire and checks its signature.
//
// The signature is what stops anything but the client that wrote the
// connection file from running code in this process, so a message that fails
// it is dropped rather than answered.
func decodeMessage(frames [][]byte, key []byte) (*message, error) {
	var (
		ids []byte
		at  = -1
	)
	for i, f := range frames {
		if string(f) == delimiter {
			at = i
			break
		}
	}
	if at < 0 {
		return nil, fmt.Errorf("hep-kernel: message has no %q delimiter", delimiter)
	}
	_ = ids

	if len(frames) < at+6 {
		return nil, fmt.Errorf("hep-kernel: message is too short (%d frames)", len(frames))
	}

	var (
		sig     = frames[at+1]
		header  = frames[at+2]
		parent  = frames[at+3]
		meta    = frames[at+4]
		content = frames[at+5]
	)

	if len(key) > 0 {
		want := sign(key, header, parent, meta, content)
		if !hmac.Equal([]byte(want), sig) {
			return nil, fmt.Errorf("hep-kernel: message signature does not match")
		}
	}

	msg := &message{IDs: frames[:at]}

	for _, tc := range []struct {
		raw []byte
		dst any
	}{
		{header, &msg.Header},
		{parent, &msg.Parent},
		{meta, &msg.Metadata},
		{content, &msg.Content},
	} {
		if len(tc.raw) == 0 {
			continue
		}
		if err := json.Unmarshal(tc.raw, tc.dst); err != nil {
			// an empty parent header arrives as "{}" and sometimes as
			// something that will not fit a header: neither is fatal.
			continue
		}
	}

	return msg, nil
}

// encode puts a message on the wire, signed.
func (m *message) encode(key []byte) (zmq4.Msg, error) {
	header, err := json.Marshal(m.Header)
	if err != nil {
		return zmq4.Msg{}, err
	}
	parent, err := json.Marshal(m.Parent)
	if err != nil {
		return zmq4.Msg{}, err
	}
	meta, err := json.Marshal(m.Metadata)
	if err != nil {
		return zmq4.Msg{}, err
	}
	content, err := json.Marshal(m.Content)
	if err != nil {
		return zmq4.Msg{}, err
	}

	frames := make([][]byte, 0, len(m.IDs)+6)
	frames = append(frames, m.IDs...)
	frames = append(frames,
		[]byte(delimiter),
		[]byte(sign(key, header, parent, meta, content)),
		header, parent, meta, content,
	)

	return zmq4.NewMsgFrom(frames...), nil
}

// sign returns the HMAC-SHA256 of a message's four JSON parts, in the order
// the protocol lays them out.
func sign(key []byte, parts ...[]byte) string {
	if len(key) == 0 {
		return ""
	}
	mac := hmac.New(sha256.New, key)
	for _, p := range parts {
		mac.Write(p)
	}
	return hex.EncodeToString(mac.Sum(nil))
}

// newID returns an identifier for a message.
//
// A message whose identifier repeats confuses a client that is matching
// replies to requests, so a failure here falls back on the clock rather than
// on a constant.
func newID() string {
	id, err := uuid.GenerateUUID()
	if err != nil {
		return fmt.Sprintf("hep-kernel-%d", time.Now().UnixNano())
	}
	return id
}
