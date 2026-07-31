// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for the error a server sends back. The code is the only
// machine-readable part of a failure — the message is free text a site
// administrator may have rewritten — so what a caller can decide from it is
// worth pinning exactly: which codes exist, what they are called, and which of
// them mean the same thing as the standard library's error values.

package xrdproto

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
)

// The codes and their names, transcribed from the protocol specification
// rather than read back from the declarations they check. The list ends at
// 3033 because that is the last code the reference clients agree on; a server
// newer than this table is handled by the unknown-code case below.
var confErrorCodes = []struct {
	code ServerErrorCode
	num  int32
	name string
}{
	{ArgInvalid, 3000, "kXR_ArgInvalid"},
	{ArgMissing, 3001, "kXR_ArgMissing"},
	{ArgTooLong, 3002, "kXR_ArgTooLong"},
	{FileLocked, 3003, "kXR_FileLocked"},
	{FileNotOpen, 3004, "kXR_FileNotOpen"},
	{FSError, 3005, "kXR_FSError"},
	{InvalidRequest, 3006, "kXR_InvalidRequest"},
	{IOError, 3007, "kXR_IOError"},
	{NoMemory, 3008, "kXR_NoMemory"},
	{NoSpace, 3009, "kXR_NoSpace"},
	{NotAuthorized, 3010, "kXR_NotAuthorized"},
	{NotFound, 3011, "kXR_NotFound"},
	{InternalServerError, 3012, "kXR_ServerError"},
	{Unsupported, 3013, "kXR_Unsupported"},
	{NoServer, 3014, "kXR_noserver"},
	{NotFile, 3015, "kXR_NotFile"},
	{IsDirectory, 3016, "kXR_isDirectory"},
	{Cancelled, 3017, "kXR_Cancelled"},
	{ItExists, 3018, "kXR_ItExists"},
	{ChkSumErr, 3019, "kXR_ChkSumErr"},
	{InProgress, 3020, "kXR_inProgress"},
	{OverQuota, 3021, "kXR_overQuota"},
	{SigVerErr, 3022, "kXR_SigVerErr"},
	{DecryptErr, 3023, "kXR_DecryptErr"},
	{Overloaded, 3024, "kXR_Overloaded"},
	{FSReadOnly, 3025, "kXR_fsReadOnly"},
	{BadPayload, 3026, "kXR_BadPayload"},
	{AttrNotFound, 3027, "kXR_AttrNotFound"},
	{TLSRequired, 3028, "kXR_TLSRequired"},
	{NoReplicas, 3029, "kXR_noReplicas"},
	{AuthFailed, 3030, "kXR_AuthFailed"},
	{Impossible, 3031, "kXR_Impossible"},
	{Conflict, 3032, "kXR_Conflict"},
	{TooManyErrs, 3033, "kXR_TooManyErrs"},
}

// TestConformance_EveryErrorCodeHasItsSpecifiedValueAndName checks the numbers
// on the wire and the names a reader sees. A code declared one off from the
// specification turns one failure into another — an authorization refusal read
// as a missing file — and nothing in a round trip would notice, because both
// ends of a Go-only test would agree on the wrong number.
func TestConformance_EveryErrorCodeHasItsSpecifiedValueAndName(t *testing.T) {
	for _, tc := range confErrorCodes {
		if got := int32(tc.code); got != tc.num {
			t.Errorf("%s is %d, want %d", tc.name, got, tc.num)
		}
		if got, want := tc.code.String(), fmt.Sprintf("%s (%d)", tc.name, tc.num); got != want {
			t.Errorf("code %d reads as %q, want %q", tc.num, got, want)
		}
	}

	// The table above must not fall behind the declarations: a code added to
	// the package without a specified value here is a code nobody checked.
	if got, want := len(serverErrorNames), len(confErrorCodes); got != want {
		t.Errorf("the package names %d codes, the specification table has %d", got, want)
	}
	for _, tc := range confErrorCodes {
		if _, ok := serverErrorNames[tc.code]; !ok {
			t.Errorf("%s has no name in the package", tc.name)
		}
	}
}

// TestConformance_AnUnknownCodeIsReportedAsItArrived covers the server this
// client is older than. An unrecognised code must not be swallowed, renamed or
// rounded to a neighbour: the number is the only thing that can be looked up.
func TestConformance_AnUnknownCodeIsReportedAsItArrived(t *testing.T) {
	for _, code := range []ServerErrorCode{0, 1, 2999, 3034, 3999, 10000, -1} {
		err := ServerError{Code: code, Message: "something happened"}
		if got, want := code.String(), fmt.Sprintf("error %d", int32(code)); got != want {
			t.Errorf("code %d reads as %q, want %q", code, got, want)
		}
		if got := err.Error(); !strings.Contains(got, fmt.Sprint(int32(code))) {
			t.Errorf("the error %q does not carry the code %d", got, code)
		}
		// An unknown code means nothing in particular, so it must not answer
		// yes to any of the questions a caller asks.
		for _, target := range []error{fs.ErrNotExist, fs.ErrExist, fs.ErrPermission, fs.ErrInvalid} {
			if errors.Is(err, target) {
				t.Errorf("the unknown code %d claims to be %v", code, target)
			}
		}
	}
}

// TestConformance_AServerErrorMeansWhatTheStandardLibraryMeans is the mapping
// a caller actually programs against. Without it, "does this file exist" is a
// string match on a message the server chose.
func TestConformance_AServerErrorMeansWhatTheStandardLibraryMeans(t *testing.T) {
	// Every code, with the standard error it is (or is not) equivalent to.
	// A code appearing in no list is one that means nothing more specific than
	// "the request failed", which is a deliberate answer rather than an
	// oversight: fs.ErrInvalid on a server that ran out of disk would send a
	// caller looking for a bug in its own arguments.
	want := map[error][]ServerErrorCode{
		fs.ErrNotExist:   {NotFound, NoServer, NoReplicas},
		fs.ErrExist:      {ItExists},
		fs.ErrPermission: {NotAuthorized, AuthFailed, TLSRequired},
		fs.ErrInvalid:    {ArgInvalid, ArgMissing, ArgTooLong, InvalidRequest, FileNotOpen, BadPayload, Impossible},
	}

	for target, codes := range want {
		set := make(map[ServerErrorCode]bool, len(codes))
		for _, code := range codes {
			set[code] = true
		}
		for _, tc := range confErrorCodes {
			err := ServerError{Code: tc.code, Message: "as reported"}
			got := errors.Is(err, target)
			if got != set[tc.code] {
				t.Errorf("errors.Is(%s, %v) is %v, want %v", tc.name, target, got, set[tc.code])
			}
		}
	}

	// The codes that are emphatically *not* one of these. Each of these has
	// been mapped by some client at some point, and each mapping misleads:
	// a checksum mismatch is not a missing file, and a full disk is not a bad
	// argument.
	for _, tc := range []struct {
		code   ServerErrorCode
		notFor error
	}{
		{ChkSumErr, fs.ErrNotExist},
		{NoSpace, fs.ErrInvalid},
		{OverQuota, fs.ErrPermission},
		{IsDirectory, fs.ErrNotExist},
		{NotFile, fs.ErrNotExist},
		{AttrNotFound, fs.ErrNotExist},
		{FSReadOnly, fs.ErrPermission},
		{Unsupported, fs.ErrInvalid},
		{Cancelled, fs.ErrInvalid},
		{InProgress, fs.ErrExist},
	} {
		if errors.Is(ServerError{Code: tc.code}, tc.notFor) {
			t.Errorf("%v claims to be %v", tc.code, tc.notFor)
		}
	}
}

// TestConformance_AWrappedServerErrorIsStillRecognisable checks the property
// that makes the mapping usable at all: every layer between the socket and the
// caller adds context, and the answer must survive all of it.
func TestConformance_AWrappedServerErrorIsStillRecognisable(t *testing.T) {
	base := ServerError{Code: NotFound, Message: "no such file or directory"}

	for _, tc := range []struct {
		desc string
		err  error
	}{
		{desc: "as returned", err: base},
		{desc: "wrapped once", err: fmt.Errorf("xrootd: open %q: %w", "/store/f.root", base)},
		{desc: "wrapped twice", err: fmt.Errorf("copy: %w", fmt.Errorf("open: %w", base))},
		{desc: "wrapped in a joined error", err: errors.Join(errors.New("and something else"), base)},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			if !errors.Is(tc.err, fs.ErrNotExist) {
				t.Errorf("%v is not recognisable as a missing file", tc.err)
			}
			// errors.As has to reach the code itself, which is what a caller
			// needs when the four standard values are not specific enough.
			var srv ServerError
			if !errors.As(tc.err, &srv) {
				t.Fatalf("%v does not carry a ServerError", tc.err)
			}
			if srv.Code != NotFound {
				t.Errorf("the recovered code is %v, want %v", srv.Code, NotFound)
			}
			if srv.Message != base.Message {
				t.Errorf("the recovered message is %q, want %q", srv.Message, base.Message)
			}
		})
	}
}

// TestConformance_AnErrorResponseDecodesToTheCodeAndMessage closes the loop
// between the wire and the mapping: the classification is only as good as the
// decode that produced the code.
func TestConformance_AnErrorResponseDecodesToTheCodeAndMessage(t *testing.T) {
	for _, tc := range confErrorCodes {
		var buf xrdenc.WBuffer
		if err := (ServerError{Code: tc.code, Message: "the server's own words"}).MarshalXrd(&buf); err != nil {
			t.Fatalf("MarshalXrd: %v", err)
		}

		hdr := ResponseHeader{Status: Error, DataLength: int32(len(buf.Bytes()))}
		err := hdr.Error(buf.Bytes())
		if err == nil {
			t.Fatalf("%s: an error response decoded to no error", tc.name)
		}

		var srv ServerError
		if !errors.As(err, &srv) {
			t.Fatalf("%s: the decoded error is %T, want a ServerError", tc.name, err)
		}
		if srv.Code != tc.code {
			t.Errorf("the decoded code is %v, want %v", srv.Code, tc.code)
		}
		if srv.Message != "the server's own words" {
			t.Errorf("the decoded message is %q, want the server's own words", srv.Message)
		}
		if !strings.Contains(err.Error(), tc.name) {
			t.Errorf("the error reads %q, want it to name %s", err.Error(), tc.name)
		}
	}

	// A response that is not an error decodes to no error at all, whatever it
	// happens to carry.
	hdr := ResponseHeader{Status: Ok, DataLength: 4}
	if err := hdr.Error([]byte{0x00, 0x00, 0x0b, 0xbb}); err != nil {
		t.Errorf("a successful response produced the error %v", err)
	}
}
