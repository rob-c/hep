// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdfs

import (
	"context"
	"fmt"
	"strings"
)

// LocateOptions are the options to apply when locating a path.
type LocateOptions uint16

const (
	// LocateAddPeers adds eligible peers to the answer.
	LocateAddPeers LocateOptions = 1 << 0
	// LocateRefresh updates cached information on the path's location.
	LocateRefresh LocateOptions = 1 << 7
	// LocatePreferName asks for host names rather than addresses.
	LocatePreferName LocateOptions = 1 << 8
	// LocateNoWait asks for whatever is known now rather than the best answer.
	LocateNoWait LocateOptions = 1 << 13
	// LocateNone applies no option.
	LocateNone LocateOptions = 0
)

// Location is one endpoint reported by a locate request.
//
// A locate answer distinguishes an endpoint that holds the data from one that
// merely knows where it is, and a replica that is online from one that has to
// be staged first. A client that treats them alike opens a file on a manager,
// is redirected, and pays a round trip it could have skipped — or reports as
// available a replica that is still on tape.
type Location struct {
	// Addr is the endpoint, as "host:port".
	Addr string
	// Kind is the node type: 'S'/'s' for a data server, 'M'/'m' for a manager.
	// The lower-case forms mean the same thing, pending.
	Kind byte
	// Access is 'r' when the endpoint offers the path read-only and 'w' when it
	// can also be written.
	Access byte
}

// IsManager reports whether the endpoint routes to a data server rather than
// holding the data itself.
func (l Location) IsManager() bool { return l.Kind == 'M' || l.Kind == 'm' }

// IsServer reports whether the endpoint holds the data.
func (l Location) IsServer() bool { return l.Kind == 'S' || l.Kind == 's' }

// IsPending reports whether the endpoint is known but not yet online, which is
// what a staged-out replica looks like.
func (l Location) IsPending() bool { return l.Kind == 's' || l.Kind == 'm' }

// CanWrite reports whether the endpoint offers the path for writing.
func (l Location) CanWrite() bool { return l.Access == 'w' }

func (l Location) String() string {
	kind, access := "server", "read-only"
	if l.IsManager() {
		kind = "manager"
	}
	if l.CanWrite() {
		access = "read-write"
	}
	if l.IsPending() {
		kind = "pending " + kind
	}
	return fmt.Sprintf("Location(%s, %s, %s)", l.Addr, kind, access)
}

// ParseLocations decodes the body of a locate response: space-separated
// "XY<host:port>" tokens, where X is the node type and Y the access mode.
func ParseLocations(raw []byte) ([]Location, error) {
	text := strings.TrimRight(string(raw), "\x00")
	var out []Location
	for _, tok := range strings.Fields(text) {
		if len(tok) < 3 {
			return nil, fmt.Errorf("xrootd: malformed locate token %q", tok)
		}
		out = append(out, Location{Kind: tok[0], Access: tok[1], Addr: tok[2:]})
	}
	return out, nil
}

// PrepareOptions are the options to apply to each path of a prepare request.
type PrepareOptions byte

const (
	// PrepareCancel cancels an earlier prepare request, named by its handle.
	PrepareCancel PrepareOptions = 1
	// PrepareNotify asks for a message when the path has been processed.
	PrepareNotify PrepareOptions = 2
	// PrepareNoErrors suppresses the notification for preparation errors.
	PrepareNoErrors PrepareOptions = 4
	// PrepareStage brings the path online if it is not already.
	PrepareStage PrepareOptions = 8
	// PrepareWrite prepares the path for writing.
	PrepareWrite PrepareOptions = 16
	// PrepareColocate co-locates the staged paths, if at all possible.
	PrepareColocate PrepareOptions = 32
	// PrepareRefresh refreshes the access time even when the location is known.
	PrepareRefresh PrepareOptions = 64
	// PrepareNone applies no option.
	PrepareNone PrepareOptions = 0
)

// QueryCode names the kind of question a query request asks.
type QueryCode uint16

const (
	QueryStats          QueryCode = 1  // QueryStats asks for server statistics.
	QueryPrepare        QueryCode = 2  // QueryPrepare asks whether a prepare request has completed.
	QueryChecksum       QueryCode = 3  // QueryChecksum asks for a file checksum.
	QueryXAttr          QueryCode = 4  // QueryXAttr asks for a file's extended attributes.
	QuerySpace          QueryCode = 5  // QuerySpace asks for logical space statistics.
	QueryCancelChecksum QueryCode = 6  // QueryCancelChecksum cancels a checksum computation.
	QueryConfig         QueryCode = 7  // QueryConfig asks for server configuration values.
	QueryVisa           QueryCode = 8  // QueryVisa asks for a file's visa attributes.
	QueryOpaque1        QueryCode = 16 // QueryOpaque1 asks an implementation-dependent question.
	QueryOpaque2        QueryCode = 32 // QueryOpaque2 asks an implementation-dependent question.
	QueryOpaque3        QueryCode = 64 // QueryOpaque3 asks an implementation-dependent question.
)

// LocateFS is implemented by filesystems that can report where the replicas of
// a path live (kXR_locate).
type LocateFS interface {
	Locate(ctx context.Context, path string, opts LocateOptions) ([]Location, error)
}

// PrepareFS is implemented by filesystems that can stage paths in advance
// (kXR_prepare). Prepare returns the handle the server gave the request, which
// is what a later cancellation or status query names.
type PrepareFS interface {
	Prepare(ctx context.Context, paths []string, opts PrepareOptions, prio uint8) (string, error)
}

// QueryFS is implemented by filesystems that answer server questions
// (kXR_query).
type QueryFS interface {
	// Query asks the server one question and returns its answer with the
	// trailing NULs removed.
	Query(ctx context.Context, code QueryCode, args string) (string, error)

	// QueryConfig looks up server configuration values, one per name asked.
	// A name the server has no value for is absent from the result.
	QueryConfig(ctx context.Context, names ...string) (map[string]string, error)
}

// ParseConfig pairs the answer to a kXR_Qconfig query with the names that were
// asked, which is the only thing that makes the answer readable: the server
// sends one value per line, in the order asked, with no names at all.
//
// The split is on "\n" rather than by non-empty lines so that a name the server
// has no value for consumes its own line and leaves the rest lined up with
// theirs. Such a name is left out of the result, the way a missing key is
// absent from a map.
func ParseConfig(names []string, raw []byte) map[string]string {
	values := strings.Split(strings.TrimRight(string(raw), "\x00"), "\n")
	out := make(map[string]string, len(names))
	for i, name := range names {
		if i >= len(values) {
			break
		}
		if v := strings.TrimSpace(values[i]); v != "" {
			out[name] = v
		}
	}
	return out
}
