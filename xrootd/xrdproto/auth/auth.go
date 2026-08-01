// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package auth contains the structures describing auth request.
package auth // import "go-hep.org/x/hep/xrootd/xrdproto/auth"

import (
	"errors"
	"strings"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
)

// RequestID is the id of the request, it is sent as part of message.
// See xrootd protocol specification for details: http://xrootd.org/doc/dev45/XRdv310.pdf, 2.3 Client Request Format.
const RequestID uint16 = 3000

// Request holds the auth request parameters.
type Request struct {
	_           [12]byte
	Type        [4]byte
	Credentials string
}

// ReqID implements xrdproto.Request.ReqID.
func (req *Request) ReqID() uint16 { return RequestID }

// ShouldSign implements xrdproto.Request.ShouldSign.
func (req *Request) ShouldSign() bool { return false }

// MarshalXrd implements xrdproto.Marshaler.
func (o Request) MarshalXrd(wBuffer *xrdenc.WBuffer) error {
	wBuffer.Next(12)
	wBuffer.WriteBytes(o.Type[:])
	wBuffer.WriteStr(o.Credentials)
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (o *Request) UnmarshalXrd(rBuffer *xrdenc.RBuffer) error {
	rBuffer.Skip(12)
	rBuffer.ReadBytes(o.Type[:])
	o.Credentials = rBuffer.ReadStr()
	return rBuffer.Err()
}

// Auther is the interface that must be implemented by a security provider.
type Auther interface {
	Provider() string                          // Provider returns the name of the security provider.
	Request(params []string) (*Request, error) // Request forms an authorization Request according to passed parameters.
}

// Continuer is an optional interface implemented by multi-round security
// providers (such as GSI). After the initial Request, when the server responds
// with kXR_authmore carrying challenge, More is called with that challenge and
// returns the next request to send. It returns a nil request when the exchange
// is complete.
type Continuer interface {
	More(challenge []byte) (*Request, error)
}

// Missing reports that a security provider has no credential to offer.
//
// It is what a provider says instead of "not found". A credential is looked for
// in a fixed list of places, and which of them were consulted is the whole
// question a user has when a transfer is refused: "no bearer token found" sends
// them to read the client's source, while the four paths that were tried tells
// them immediately that the pilot wrote the token somewhere else. Hint carries
// the command that produces the credential, because for X.509 and Kerberos that
// command is the answer often enough to be worth printing.
type Missing struct {
	// Provider is the XRootD security protocol name ("gsi", "ztn", "krb5", "sss").
	Provider string
	// What is the credential, named the way a user would name it.
	What string
	// Searched lists the locations that were consulted, in the order they were
	// consulted. It is empty for a provider whose discovery is not a file search.
	Searched []string
	// Hint is the command that obtains the credential, if there is one.
	Hint string
	// Err is the underlying failure: a credential that was found and could not
	// be used (expired, malformed) rather than one that was not there at all.
	Err error
}

func (m *Missing) Error() string {
	var b strings.Builder
	b.WriteString("auth/")
	b.WriteString(m.Provider)
	b.WriteString(": no ")
	b.WriteString(m.What)
	if m.Err != nil {
		b.WriteString(": ")
		b.WriteString(m.Err.Error())
	}
	if len(m.Searched) > 0 {
		b.WriteString(" (looked in ")
		b.WriteString(strings.Join(m.Searched, ", "))
		b.WriteString(")")
	}
	return b.String()
}

func (m *Missing) Unwrap() error { return m.Err }

// AsMissing returns the *Missing carried by err, or nil.
func AsMissing(err error) *Missing {
	var m *Missing
	if errors.As(err, &m) {
		return m
	}
	return nil
}
