// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrootd

import (
	"context"
	"net"
	"reflect"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd/xrdfs"
	"go-hep.org/x/hep/xrootd/xrdproto"
	"go-hep.org/x/hep/xrootd/xrdproto/locate"
	"go-hep.org/x/hep/xrootd/xrdproto/prepare"
	"go-hep.org/x/hep/xrootd/xrdproto/query"
)

func TestConformance_ALocateAnswerSaysWhichEndpointHoldsTheData(t *testing.T) {
	// The answer a manager gives mixes endpoints that hold the file with ones
	// that only know where it is, and replicas that are online with ones that
	// are not. A client that cannot tell them apart reads from a manager or
	// reports a tape-resident replica as ready.
	const answer = "Sr140.105.1.1:1094 Mw[::1]:1095 sr198.51.100.7:1095\x00\x00"

	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		var got locate.Request
		hdr, err := unmarshalRequest(data, &got)
		if err != nil {
			cancel()
			t.Errorf("could not unmarshal locate request: %v", err)
			return
		}
		want := locate.Request{Options: uint16(xrdfs.LocateRefresh | xrdfs.LocatePreferName), Path: "/eos/f.root"}
		if !reflect.DeepEqual(got, want) {
			cancel()
			t.Errorf("locate request:\ngot = %#v\nwant = %#v", got, want)
			return
		}
		if err := xrdproto.WriteResponse(conn, hdr.StreamID, xrdproto.Ok, locate.Response{Data: []byte(answer)}); err != nil {
			cancel()
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		fs := client.FS().(*fileSystem)
		got, err := fs.Locate(context.Background(), "/eos/f.root", xrdfs.LocateRefresh|xrdfs.LocatePreferName)
		if err != nil {
			t.Fatalf("Locate: %v", err)
		}
		want := []xrdfs.Location{
			{Addr: "140.105.1.1:1094", Kind: 'S', Access: 'r'},
			{Addr: "[::1]:1095", Kind: 'M', Access: 'w'},
			{Addr: "198.51.100.7:1095", Kind: 's', Access: 'r'},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("locations:\ngot = %v\nwant = %v", got, want)
		}
		switch {
		case !got[0].IsServer() || got[0].IsPending() || got[0].CanWrite():
			t.Fatalf("the data server was read as %v", got[0])
		case !got[1].IsManager() || !got[1].CanWrite():
			t.Fatalf("the manager was read as %v", got[1])
		case !got[2].IsServer() || !got[2].IsPending():
			t.Fatalf("the pending replica was read as %v", got[2])
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}

func TestConformance_APrepareRequestComesBackWithItsHandle(t *testing.T) {
	// The handle is what a later cancellation names. A client that drops it
	// leaves a tape system staging files for a job that has already died.
	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		var got prepare.Request
		hdr, err := unmarshalRequest(data, &got)
		if err != nil {
			cancel()
			t.Errorf("could not unmarshal prepare request: %v", err)
			return
		}
		want := prepare.Request{
			Options:  byte(xrdfs.PrepareStage | xrdfs.PrepareNotify),
			Priority: 2,
			Paths:    []string{"/eos/a.root", "/eos/b.root"},
		}
		if !reflect.DeepEqual(got, want) {
			cancel()
			t.Errorf("prepare request:\ngot = %#v\nwant = %#v", got, want)
			return
		}
		if err := xrdproto.WriteResponse(conn, hdr.StreamID, xrdproto.Ok, prepare.Response{Data: []byte("d41d8cd9\n\x00\x00")}); err != nil {
			cancel()
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		fs := client.FS().(*fileSystem)
		got, err := fs.Prepare(context.Background(),
			[]string{"/eos/a.root", "/eos/b.root"},
			xrdfs.PrepareStage|xrdfs.PrepareNotify, 2,
		)
		if err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		if want := "d41d8cd9"; got != want {
			t.Fatalf("prepare handle: got = %q, want = %q", got, want)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}

func TestConformance_AQueryAsksWhatItWasAskedToAsk(t *testing.T) {
	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		var got query.Request
		hdr, err := unmarshalRequest(data, &got)
		if err != nil {
			cancel()
			t.Errorf("could not unmarshal query request: %v", err)
			return
		}
		if got.Query != uint16(xrdfs.QuerySpace) || string(got.Args) != "/eos/atlas" {
			cancel()
			t.Errorf("query request: query=%d args=%q", got.Query, got.Args)
			return
		}
		answer := "oss.cgroup=public&oss.space=1000&oss.free=400\x00\x00\x00"
		if err := xrdproto.WriteResponse(conn, hdr.StreamID, xrdproto.Ok, query.Response{Data: []byte(answer)}); err != nil {
			cancel()
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		fs := client.FS().(*fileSystem)
		got, err := fs.Query(context.Background(), xrdfs.QuerySpace, "/eos/atlas")
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		// The padding the protocol adds is not part of the answer.
		if want := "oss.cgroup=public&oss.space=1000&oss.free=400"; got != want {
			t.Fatalf("query answer:\ngot = %q\nwant = %q", got, want)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}

func TestConformance_AConfigAnswerKeepsItsPlaceWhenAValueIsMissing(t *testing.T) {
	// kXR_Qconfig answers with values only, one line each, in the order asked.
	// A name the server does not know still gets its line, and a client that
	// skips the empty line pairs every later value with the wrong name.
	names := []string{"version", "sitename", "role"}

	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		var got query.Request
		hdr, err := unmarshalRequest(data, &got)
		if err != nil {
			cancel()
			t.Errorf("could not unmarshal query request: %v", err)
			return
		}
		if got.Query != query.Config || string(got.Args) != "version\nsitename\nrole" {
			cancel()
			t.Errorf("config request: query=%d args=%q", got.Query, got.Args)
			return
		}
		if err := xrdproto.WriteResponse(conn, hdr.StreamID, xrdproto.Ok, query.Response{Data: []byte("v5.6.3\n\nmanager\x00")}); err != nil {
			cancel()
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		fs := client.FS().(*fileSystem)
		got, err := fs.QueryConfig(context.Background(), names...)
		if err != nil {
			t.Fatalf("QueryConfig: %v", err)
		}
		want := map[string]string{"version": "v5.6.3", "role": "manager"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("config:\ngot = %v\nwant = %v", got, want)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}

func TestConformance_AConfigQueryWithNoNamesAsksForTheVersion(t *testing.T) {
	// The cheapest question there is, and the one every server answers: it is
	// what "which server is this?" has to turn into.
	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		var got query.Request
		hdr, err := unmarshalRequest(data, &got)
		if err != nil {
			cancel()
			t.Errorf("could not unmarshal query request: %v", err)
			return
		}
		if string(got.Args) != "version" {
			cancel()
			t.Errorf("config request asked for %q, want %q", got.Args, "version")
			return
		}
		if err := xrdproto.WriteResponse(conn, hdr.StreamID, xrdproto.Ok, query.Response{Data: []byte("v5.6.3\x00")}); err != nil {
			cancel()
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		fs := client.FS().(*fileSystem)
		got, err := fs.QueryConfig(context.Background())
		if err != nil {
			t.Fatalf("QueryConfig: %v", err)
		}
		if want := (map[string]string{"version": "v5.6.3"}); !reflect.DeepEqual(got, want) {
			t.Fatalf("config:\ngot = %v\nwant = %v", got, want)
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}

// refusingServer answers whatever it is asked with a server error.
func refusingServer(t *testing.T) func(cancel func(), conn net.Conn) {
	t.Helper()
	return func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		sid := xrdproto.StreamID{data[0], data[1]}
		err = xrdproto.WriteResponse(conn, sid, xrdproto.Error, xrdproto.ServerError{
			Code:    xrdproto.NotAuthorized,
			Message: "no",
		})
		if err != nil {
			cancel()
		}
	}
}

func TestConformance_AnAdminCallReportsTheServersRefusal(t *testing.T) {
	// Every one of these has a value to return as well as an error, and a
	// caller that is handed a zero value with a nil error reads an empty
	// locate answer as "no replicas anywhere" rather than as "not allowed".
	for _, tc := range []struct {
		name string
		call func(*fileSystem) error
	}{
		{"locate", func(fs *fileSystem) error {
			locs, err := fs.Locate(context.Background(), "/eos/f.root", xrdfs.LocateNone)
			if err == nil && locs != nil {
				t.Errorf("a refused locate returned %v", locs)
			}
			return err
		}},
		{"prepare", func(fs *fileSystem) error {
			h, err := fs.Prepare(context.Background(), []string{"/eos/f.root"}, xrdfs.PrepareStage, 1)
			if err == nil && h != "" {
				t.Errorf("a refused prepare returned %q", h)
			}
			return err
		}},
		{"query", func(fs *fileSystem) error {
			_, err := fs.Query(context.Background(), xrdfs.QuerySpace, "/eos")
			return err
		}},
		{"config", func(fs *fileSystem) error {
			cfg, err := fs.QueryConfig(context.Background(), "version")
			if err == nil && cfg != nil {
				t.Errorf("a refused config query returned %v", cfg)
			}
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testClientWithMockServer(refusingServer(t), func(cancel func(), client *Client) {
				err := tc.call(client.FS().(*fileSystem))
				if err == nil {
					t.Fatalf("a refused %s reported success", tc.name)
				}
				if !strings.Contains(err.Error(), "no") {
					t.Fatalf("the error does not carry the server's message: %v", err)
				}
			})
		})
	}
}

func TestConformance_AMalformedLocateAnswerIsAnError(t *testing.T) {
	serverFunc := func(cancel func(), conn net.Conn) {
		data, err := xrdproto.ReadRequest(conn)
		if err != nil {
			cancel()
			return
		}
		var got locate.Request
		hdr, err := unmarshalRequest(data, &got)
		if err != nil {
			cancel()
			return
		}
		if err := xrdproto.WriteResponse(conn, hdr.StreamID, xrdproto.Ok, locate.Response{Data: []byte("S\x00")}); err != nil {
			cancel()
		}
	}

	clientFunc := func(cancel func(), client *Client) {
		fs := client.FS().(*fileSystem)
		if _, err := fs.Locate(context.Background(), "/eos/f.root", xrdfs.LocateNone); err == nil {
			t.Fatalf("a truncated locate token was accepted")
		}
	}

	testClientWithMockServer(serverFunc, clientFunc)
}
