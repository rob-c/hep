// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rhist_test

import (
	"path/filepath"
	"testing"

	"go-hep.org/x/hep/groot"
	"go-hep.org/x/hep/groot/rhist"
	"go-hep.org/x/hep/groot/root"
	"go-hep.org/x/hep/hbook"
)

// TestH3File writes every flavour of 3-dim histogram to a ROOT file and reads
// it back, which is what puts the streamers, and the ROOT class versions they
// carry, to work.
func TestH3File(t *testing.T) {
	h := hbook.NewH3D(3, 0, 3, 2, 0, 2, 2, 0, 2)
	h.Ann["name"] = "h3"
	h.Fill(0.5, 0.5, 0.5, 1)
	h.Fill(1.5, 1.5, 1.5, 2)
	h.Fill(-1, -1, -1, 3)

	objs := map[string]root.Object{
		"h3c": rhist.NewH3CFrom(h),
		"h3s": rhist.NewH3SFrom(h),
		"h3i": rhist.NewH3IFrom(h),
		"h3f": rhist.NewH3FFrom(h),
		"h3d": rhist.NewH3DFrom(h),
	}

	fname := filepath.Join(t.TempDir(), "h3.root")
	f, err := groot.Create(fname)
	if err != nil {
		t.Fatalf("could not create ROOT file: %+v", err)
	}
	for k, v := range objs {
		err = f.Put(k, v)
		if err != nil {
			t.Fatalf("could not write %q: %+v", k, err)
		}
	}
	err = f.Close()
	if err != nil {
		t.Fatalf("could not close ROOT file: %+v", err)
	}

	r, err := groot.Open(fname)
	if err != nil {
		t.Fatalf("could not open ROOT file: %+v", err)
	}
	defer r.Close()

	for _, tc := range []struct {
		key   string
		class string
	}{
		{"h3c", "TH3C"},
		{"h3s", "TH3S"},
		{"h3i", "TH3I"},
		{"h3f", "TH3F"},
		{"h3d", "TH3D"},
	} {
		obj, err := r.Get(tc.key)
		if err != nil {
			t.Fatalf("could not read %q: %+v", tc.key, err)
		}
		h3, ok := obj.(rhist.H3)
		if !ok {
			t.Fatalf("%q: %T does not implement rhist.H3", tc.key, obj)
		}
		if got, want := obj.(root.Object).Class(), tc.class; got != want {
			t.Fatalf("%q: class got=%q, want=%q", tc.key, got, want)
		}
		if got, want := h3.Entries(), 3.0; got != want {
			t.Fatalf("%q: entries got=%v, want=%v", tc.key, got, want)
		}
		if got, want := h3.SumW(), 6.0; got != want {
			t.Fatalf("%q: sumw got=%v, want=%v", tc.key, got, want)
		}
	}

	// the file must carry the streamers ROOT needs to make sense of it.
	want := map[string]int{
		"TH3": 6, "TH3C": 4, "TH3S": 4, "TH3I": 4, "TH3F": 4, "TH3D": 4,
		"TAtt3D": 1,
	}
	got := make(map[string]int)
	for _, si := range r.StreamerInfos() {
		if _, ok := want[si.Name()]; ok {
			got[si.Name()] = si.ClassVersion()
		}
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("streamer %q: version got=%d, want=%d", k, got[k], v)
		}
	}
}
