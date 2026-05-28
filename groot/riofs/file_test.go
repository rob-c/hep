// Copyright ©2017 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package riofs_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"go-hep.org/x/hep/groot"
	"go-hep.org/x/hep/groot/internal/rdatatest"
	"go-hep.org/x/hep/groot/internal/rtests"
	"go-hep.org/x/hep/groot/rbase"
	"go-hep.org/x/hep/groot/rcmd"
	"go-hep.org/x/hep/groot/rcont"
	"go-hep.org/x/hep/groot/rdict"
	"go-hep.org/x/hep/groot/riofs"
	"go-hep.org/x/hep/groot/root"
	"go-hep.org/x/hep/groot/rtree"
)

func TestFileSegmentMap(t *testing.T) {
	f, err := groot.Open("../testdata/dirs-6.14.00.root")
	if err != nil {
		t.Fatalf("could not open ROOT file: %+v", err)
	}
	defer f.Close()

	out := new(bytes.Buffer)
	err = f.SegmentMap(out)
	if err != nil {
		t.Fatalf("could not run segment map: %+v", err)
	}

	got := out.String()
	want := `20180703/110855  At:100    N=130       TFile         
20180703/110855  At:230    N=107       TDirectory    
20180703/110855  At:337    N=107       TDirectory    
20180703/110855  At:444    N=107       TDirectory    
20180703/110855  At:551    N=109       TDirectory    
20180703/110855  At:660    N=345       TH1F           CX =  2.82
20180703/110855  At:1005   N=90        TDirectory    
20180703/110855  At:1095   N=100       TDirectory    
20180703/110855  At:1195   N=51        TDirectory    
20180703/110855  At:1246   N=51        TDirectory    
20180703/110855  At:1297   N=196       KeysList      
20180703/110855  At:1493   N=3845      StreamerInfo   CX =  2.44
20180703/110855  At:5338   N=61        FreeSegments  
20180703/110855  At:5399   N=1         END           
`

	if got != want {
		t.Fatalf("invalid segment map:\ngot:\n%v\nwant:\n%v\n", got, want)
	}
}

func TestFileRecords(t *testing.T) {
	f, err := groot.Open("../testdata/dirs-6.14.00.root")
	if err != nil {
		t.Fatalf("could not open ROOT file: %+v", err)
	}
	defer f.Close()

	out := new(bytes.Buffer)
	err = f.Records(out)
	if err != nil {
		t.Fatalf("could not run segment map: %+v", err)
	}

	got := out.String()
	want := `=== file "../testdata/dirs-6.14.00.root" ===
begin: 100
end:   5399
seek-free: 5338 nbytes-free=61 nfree=1
seek-info: 1493 nbytes-info=3845
=== dir "dirs-6.14.00.root" @100 ===
parent:      <nil>
nbytes-keys: 196
nbytes-name: 70
seek-dir:    100
seek-parent: 0
seek-keys:   1297
class:       "TFile"
keys:        3
 key[0]: "dir1"
  === key "dir1" ===
  nbytes:    107
  keylen:    47
  objlen:    60
  cycle:     1
  seek-key:  230
  seek-pdir: 100
  class:     "TDirectoryFile"
  parent:    @100
    === dir "dir1" @230 ===
    parent:      @100
    nbytes-keys: 100
    nbytes-name: 47
    seek-dir:    230
    seek-parent: 100
    seek-keys:   1095
    class:       "TDirectoryFile"
    keys:        1
     key[0]: "dir11"
      === key "dir11" ===
      nbytes:    109
      keylen:    49
      objlen:    60
      cycle:     1
      seek-key:  551
      seek-pdir: 230
      class:     "TDirectoryFile"
      parent:    @230
        === dir "dir11" @551 ===
        parent:      @230
        nbytes-keys: 90
        nbytes-name: 49
        seek-dir:    551
        seek-parent: 100
        seek-keys:   1005
        class:       "TDirectoryFile"
        keys:        1
         key[0]: "h1"
          === key "h1" ===
          nbytes:    345
          keylen:    37
          objlen:    936
          cycle:     1
          seek-key:  660
          seek-pdir: 551
          class:     "TH1F"
          parent:    @551
 key[1]: "dir2"
  === key "dir2" ===
  nbytes:    107
  keylen:    47
  objlen:    60
  cycle:     1
  seek-key:  337
  seek-pdir: 100
  class:     "TDirectoryFile"
  parent:    @100
    === dir "dir2" @337 ===
    parent:      @100
    nbytes-keys: 51
    nbytes-name: 47
    seek-dir:    337
    seek-parent: 100
    seek-keys:   1195
    class:       "TDirectoryFile"
    keys:        0
 key[2]: "dir3"
  === key "dir3" ===
  nbytes:    107
  keylen:    47
  objlen:    60
  cycle:     1
  seek-key:  444
  seek-pdir: 100
  class:     "TDirectoryFile"
  parent:    @100
    === dir "dir3" @444 ===
    parent:      @100
    nbytes-keys: 51
    nbytes-name: 47
    seek-dir:    444
    seek-parent: 100
    seek-keys:   1246
    class:       "TDirectoryFile"
    keys:        0
`

	if got != want {
		t.Fatalf("invalid segment map:\ngot:\n%v\nwant:\n%v\n", got, want)
	}
}

func TestFileDirectory(t *testing.T) {
	for _, fname := range []string{
		"../testdata/small-flat-tree.root",
		rtests.XrdRemote("testdata/small-flat-tree.root"),
	} {
		t.Run(fname, func(t *testing.T) {
			f, err := groot.Open(fname)
			if err != nil {
				t.Fatal(err.Error())
			}
			defer f.Close()

			for _, table := range []struct {
				test  string
				value string
				want  string
			}{
				{"Name", f.Name(), "test-small.root"}, // name when created
				{"Title", f.Title(), "small event file"},
				{"Class", f.Class(), "TFile"},
			} {
				if table.value != table.want {
					t.Fatalf("%v: got=%q, want=%q", table.test, table.value, table.want)
				}
			}

			for _, table := range []struct {
				name string
				want bool
			}{
				{"tree", true},
				{"tree;0", false},
				{"tree;1", true},
				{"tree;9999", true},
				{"tree_nope", false},
				{"tree_nope;0", false},
				{"tree_nope;1", false},
				{"tree_nope;9999", false},
			} {
				_, err := f.Get(table.name)
				if (err == nil) != table.want {
					t.Fatalf("%s: got key (err=%v). want=%v", table.name, err, table.want)
				}
			}

			for _, table := range []struct {
				name string
				want string
			}{
				{"tree", "TTree"},
				{"tree;1", "TTree"},
			} {
				k, err := f.Get(table.name)
				if err != nil {
					t.Fatalf("%s: expected key to exist! (got %v)", table.name, err)
				}

				if k.Class() != table.want {
					t.Fatalf("%s: got key with class=%s (want=%s)", table.name, k.Class(), table.want)
				}
			}

			for _, table := range []struct {
				name string
				want string
			}{
				{"tree", "tree"},
				{"tree;1", "tree"},
			} {
				o, err := f.Get(table.name)
				if err != nil {
					t.Fatalf("%s: expected key to exist! (got %v)", table.name, err)
				}

				k := o.(root.Named)
				if k.Name() != table.want {
					t.Fatalf("%s: got key with name=%s (want=%v)", table.name, k.Name(), table.want)
				}
			}

			for _, table := range []struct {
				name string
				want string
			}{
				{"tree", "my tree title"},
				{"tree;1", "my tree title"},
			} {
				o, err := f.Get(table.name)
				if err != nil {
					t.Fatalf("%s: expected key to exist! (got %v)", table.name, err)
				}

				k := o.(root.Named)
				if k.Title() != table.want {
					t.Fatalf("%s: got key with title=%s (want=%v)", table.name, k.Title(), table.want)
				}
			}
		})
	}
}

func TestFileOpenStreamerInfo(t *testing.T) {
	for _, fname := range []string{
		"../testdata/small-flat-tree.root",
		"../testdata/simple.root",
		rtests.XrdRemote("testdata/small-flat-tree.root"),
		rtests.XrdRemote("testdata/simple.root"),
	} {
		f, err := groot.Open(fname)
		if err != nil {
			t.Errorf("error opening %q: %v\n", fname, err)
			continue
		}
		defer f.Close()

		_ = f.StreamerInfos()
	}
}

func TestOpenEmptyFile(t *testing.T) {
	f, err := groot.Open("../testdata/uproot/issue70.root")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	si := f.StreamerInfos()
	if si != nil {
		t.Fatalf("expected no StreamerInfos in empty file")
	}
}

func TestCreate(t *testing.T) {
	dir, err := os.MkdirTemp("", "riofs-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	for i, tc := range []struct {
		name string
		skip bool
		want []rtests.ROOTer
	}{
		{name: "", want: nil},
		{
			name: "TObjString",
			want: []rtests.ROOTer{rbase.NewObjString("hello")},
		},
		{
			name: "TObjString",
			want: []rtests.ROOTer{rbase.NewObjString("hello"), rbase.NewObjString("world")},
		},
		{
			name: "TObjString",
			want: func() []rtests.ROOTer {
				var out []rtests.ROOTer
				for _, i := range []int{0, 1, 253, 254, 255, 256, 512, 1024} {
					str := strings.Repeat("=", i)
					out = append(out, rbase.NewObjString(str))
				}
				return out
			}(),
		},
		{
			name: "TObject",
			want: []rtests.ROOTer{rbase.NewObject()},
		},
		{
			name: "TNamed",
			want: []rtests.ROOTer{
				rbase.NewNamed("n0", "t0"),
				rbase.NewNamed("n1", "t1"),
				rbase.NewNamed("n2", "t2"),
			},
		},
		{
			name: "TList",
			want: []rtests.ROOTer{rcont.NewList("list-name", []root.Object{
				rbase.NewNamed("n0", "t0"),
				rbase.NewNamed("n1", "t1"),
				rbase.NewNamed("n2", "t2"),
			})},
		},
		{
			name: "TArrayF",
			want: []rtests.ROOTer{
				&rcont.ArrayF{Data: []float32{1, 2, 3, 4, 5, 6}},
			},
		},
		{
			name: "TArrayD",
			want: []rtests.ROOTer{
				&rcont.ArrayD{Data: []float64{1, 2, 3, 4, 5, 6}},
			},
		},
		{
			name: "TArrays",
			want: []rtests.ROOTer{
				&rcont.ArrayF{Data: []float32{1, 2, 3, 4, 5, 6}},
				&rcont.ArrayD{Data: []float64{1, 2, 3, 4, 5, 6}},
			},
		},
	} {
		fname := filepath.Join(dir, fmt.Sprintf("out-%d.root", i))
		t.Run(tc.name, func(t *testing.T) {
			if tc.skip {
				t.Skip()
			}

			w, err := groot.Create(fname)
			if err != nil {
				t.Fatal(err)
			}

			for i := range tc.want {
				var (
					kname = fmt.Sprintf("key-%s-%02d", tc.name, i)
					want  = tc.want[i]
				)

				err = w.Put(kname, want)
				if err != nil {
					t.Fatal(err)
				}
			}

			if got, want := len(w.Keys()), len(tc.want); got != want {
				t.Fatalf("invalid number of keys. got=%d, want=%d", got, want)
			}

			err = w.Close()
			if err != nil {
				t.Fatalf("error closing file: %+v", err)
			}

			r, err := groot.Open(fname)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()

			if got, want := len(r.Keys()), len(tc.want); got != want {
				t.Fatalf("invalid number of keys. got=%d, want=%d", got, want)
			}

			for i := range tc.want {
				var (
					kname = fmt.Sprintf("key-%s-%02d", tc.name, i)
					want  = tc.want[i]
				)

				rgot, err := r.Get(kname)
				if err != nil {
					t.Fatal(err)
				}

				if got := rgot.(rtests.ROOTer); !reflect.DeepEqual(got, want) {
					t.Fatalf("error reading back value[%d].\ngot = %#v\nwant = %#v", i, got, want)
				}
			}

			err = r.Close()
			if err != nil {
				t.Fatalf("error closing file: %+v", err)
			}

			if !rtests.HasROOT {
				t.Logf("skip test with ROOT/C++")
				return
			}

			const rootls = `#include <iostream>
#include "TFile.h"
#include "TNamed.h"

void rootls(const char *fname, const char *kname) {
	auto f = TFile::Open(fname);
	auto o = f->Get(kname);
	if (o == NULL) {
		std:cerr << "could not retrieve [" << kname << "]" << std::endl;
		o->ClassName();
	}
	std::cout << "retrieved: [" << kname << "]" << std::endl;
}
`
			for i := range tc.want {
				kname := fmt.Sprintf("key-%s-%02d", tc.name, i)

				out, err := rtests.RunCxxROOT("rootls", []byte(rootls), fname, kname)
				if err != nil {
					t.Fatalf("ROOT/C++ could not open file %q:\n%s", fname, string(out))
				}
			}
		})
	}
}

func TestOpenBigFile(t *testing.T) {
	ch := make(chan error)
	go func() {
		fname := rtests.XrdRemote("testdata/SMHiggsToZZTo4L.root")
		f, err := riofs.Open(fname)
		if err != nil {
			ch <- err
			return
		}
		defer f.Close()

		o, err := f.Get("Events")
		if err != nil {
			ch <- err
			return
		}

		tree := o.(rtree.Tree)
		if got, want := tree.Entries(), int64(299973); got != want {
			ch <- fmt.Errorf("invalid entries: got=%d, want=%d", got, want)
			return
		}
		ch <- nil
	}()

	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()
	select {
	case err := <-ch:
		if err != nil {
			t.Fatalf("error: %+v", err)
		}
	case <-timeout.C:
		t.Fatalf("timeout")
	}
}

func TestReadOnlyFile(t *testing.T) {
	f, err := groot.Open("../testdata/dirs-6.14.00.root")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	err = f.Put("o1", rbase.NewObjString("v1"))
	if err == nil {
		t.Fatalf("expected an error. got nil")
	}

	o, err := f.Get("dir1")
	if err != nil {
		t.Fatal(err)
	}

	dir1 := o.(riofs.Directory)
	err = dir1.Put("o2", rbase.NewObjString("v2"))
	if err == nil {
		t.Fatalf("expected an error. got nil")
	}

	o, err = dir1.Get("dir11")
	if err != nil {
		t.Fatal(err)
	}

	dir11 := o.(riofs.Directory)
	err = dir11.Put("o3", rbase.NewObjString("v3"))
	if err == nil {
		t.Fatalf("expected an error. got nil")
	}
}

func TestTopLevelString(t *testing.T) {
	f, err := groot.Open("../testdata/string-example.root")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	o, err := riofs.Get[*rbase.String](f, "FileSummaryRecord")
	if err != nil {
		t.Fatal(err)
	}

	got := o.String()
	want := `{"LumiCounter.eventsByRun":{"counts":{},"empty":true,"type":"LumiEventCounter"},"guid":"5FE9437E-D958-11EE-AB88-3CECEF1070AC"}`
	if got != want {
		t.Fatalf("got=%q, want=%q", got, want)
	}
}

// TestVersionSkew tests we correctly detect version skews coming from,
// e.g., a ROOT/C++ class version that groot doesn't know how to handle yet.
//
// We create v1.root with TMyObjString-v1, implemented in ROOT/C++ and in groot.
// We create v2.root with TMyObjString-v2, only implemented in ROOT/C++.
//
// We try to read+copy v1.root: that should obviously succeed.
// We try to read+copy v2.root: that should fail (panic) at the `read` step.
// We try to read+copy v1.root: that should still succeed, and we should still
// get the correct TMyObjString-v1 streamer, for both the TMyObjString value
// contained in the TH1F value, and for the TMyObjString base class of the
// TMyObjRope value also contained in that TH1F value.
//
// Note that when reading the 'v2.root' file, the TMyObjString-v2 streamer is
// loaded into our global registry of streamers, even though we correctly
// detect later on we don't know how to write that class version (through a
// panic). That TMyObjString-v2 streamer shouldn't end up in an output stream
// or file: rbytes.WBuffer will prevent that, and riofs.File.findDepStreamers
// only looks for streamers in the file-local cache of streamers.
func TestVersionSkew(t *testing.T) {
	tmp := t.TempDir()

	const tmyobjstring = rdatatest.MyObjStringSrc

	var (
		v1name = filepath.Join(tmp, "v1.root")
		v2name = filepath.Join(tmp, "v2.root")
	)

	for _, tc := range []struct {
		name string
		vers int
	}{
		{name: v1name, vers: rdatatest.MyObjStringVersion},
		{name: v2name, vers: rdatatest.MyObjStringVersion + 1},
	} {
		out, err := rtests.RunCxxROOT(
			"gentmyobjstr+",
			[]byte(fmt.Sprintf(tmyobjstring, tc.vers)),
			tc.name,
		)
		if err != nil {
			t.Fatalf("could not run ROOT/C++ for %q:\n%s\nerror: %v", tc.name, out, err)
		}
	}

	oname := filepath.Join(tmp, "copy.root")

	{
		err := rcmd.Copy(oname, []string{v1name})
		if err != nil {
			t.Fatalf("could not copy ROOT file: %v", err)
		}

		f, err := groot.Open(oname)
		if err != nil {
			t.Fatalf("could not open ROOT file: %v", err)
		}
		defer f.Close()

		for _, si := range f.StreamerInfos() {
			if si.Name() != "TMyObjString" && si.Name() != "TMyObjRope" {
				continue
			}
			t.Logf("- %q, version=%d\n%s", si.Name(), si.ClassVersion(), si)
		}
	}

	var panicked bool
	func() {
		defer func() {
			err := recover()
			if err == nil {
				return
			}

			const want = `rbytes: invalid version for "TMyObjString": got=2 > max=1`
			var got string
			switch err := err.(type) {
			case error:
				got = err.Error()
			case string:
				got = err
			default:
				t.Errorf("invalid panic message type %T: %v", err, err)
			}

			if got == want {
				panicked = true
				return
			}
			t.Errorf("invalid panic message:\ngot= %q\nwant=%q", got, want)
		}()
		err := rcmd.Copy(oname, []string{v2name})
		if err != nil {
			t.Fatalf("could not copy ROOT file: %v", err)
		}

		f, err := groot.Open(oname)
		if err != nil {
			t.Fatalf("could not open ROOT file: %v", err)
		}
		defer f.Close()

		for _, si := range f.StreamerInfos() {
			if si.Name() != "TMyObjString" && si.Name() != "TMyObjRope" {
				continue
			}
			t.Logf("- %q, version=%d\n%s", si.Name(), si.ClassVersion(), si)
		}
	}()
	if !panicked {
		t.Fatal("reading TMyObjString-v2, should have panicked")
	}

	{
		err := rcmd.Copy(oname, []string{v1name})
		if err != nil {
			t.Fatalf("could not copy ROOT file: %v", err)
		}

		f, err := groot.Open(oname)
		if err != nil {
			t.Fatalf("could not open ROOT file: %v", err)
		}
		defer f.Close()

		want := map[string]int{
			"TMyObjString": rdatatest.MyObjStringVersion,
			"TMyObjRope":   int(((*rdatatest.MyObjRope)(nil)).RVersion()),
		}
		for _, si := range f.StreamerInfos() {
			name := si.Name()
			if _, ok := want[name]; !ok {
				continue
			}
			vers := si.ClassVersion()
			t.Logf("- %q, version=%d\n%s", name, vers, si)
			if got, want := vers, want[si.Name()]; got != want {
				t.Fatalf("invalid %q version: got=%d, want=%d", name, got, want)
			}
			if name == "TMyObjRope" {
				base := si.Elements()[0].(*rdict.StreamerBase)
				t.Logf("base[%s]: %q, vers=%d", name, base.Name(), base.Base())
				if got, want := base.Name(), "TMyObjString"; got != want {
					t.Fatalf("invalid base class for %q: got=%q, want=%q", name, got, want)
				}
				if got, want := base.Base(), want[base.Name()]; got != want {
					t.Fatalf("invalid base class streamer version for %q → %q: got=%d, want=%d",
						name, base.Name(), got, want,
					)
				}
			}
		}
	}

	t.Logf("checking global registry content...")
	for _, si := range rdict.StreamerInfos.Values() {
		if si.Name() == "TMyObjString" || si.Name() == "TMyObjRope" {
			t.Logf("%s", si)
		}
	}
	t.Logf("checking global registry content... [done]")
}
