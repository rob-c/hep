// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for reading the HepMC3 Asciiv3 format through the same Decoder
// that reads every HepMC2 flavour. The fixture exercises what makes v3 a
// different grammar: run-info weight names, attribute lines standing in for
// the v2 records, an explicit incoming-particle graph, and a vertex the
// writer elided because only a mother-child link implies it.

package hepmc_test

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"go-hep.org/x/hep/hepmc"
)

func decodeASCIIv3File(t *testing.T, name string) []*hepmc.Event {
	t.Helper()
	f, err := os.Open(name)
	if err != nil {
		t.Fatalf("could not open %q: %+v", name, err)
	}
	defer f.Close()

	var evts []*hepmc.Event
	dec := hepmc.NewDecoder(f)
	for {
		var evt hepmc.Event
		err := dec.Decode(&evt)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("could not decode event %d: %+v", len(evts)+1, err)
		}
		evts = append(evts, &evt)
	}
	return evts
}

func TestDecodeASCIIv3(t *testing.T) {
	evts := decodeASCIIv3File(t, "testdata/test.hepmc3")
	if got, want := len(evts), 2; got != want {
		t.Fatalf("got %d events, want %d", got, want)
	}

	evt := evts[0]
	if got, want := evt.EventNumber, 1; got != want {
		t.Errorf("event number: got %d, want %d", got, want)
	}
	if got, want := evt.MomentumUnit.String(), "GEV"; got != want {
		t.Errorf("momentum unit: got %q, want %q", got, want)
	}
	if got, want := evt.LengthUnit.String(), "MM"; got != want {
		t.Errorf("length unit: got %q, want %q", got, want)
	}

	// 3 explicit vertices + the one implied by particles 7,8 naming mother 6
	if got, want := len(evt.Vertices), 4; got != want {
		t.Errorf("got %d vertices, want %d", got, want)
	}
	if got, want := len(evt.Particles), 8; got != want {
		t.Errorf("got %d particles, want %d", got, want)
	}

	// run-info weight names apply to every event's weight values
	if got, want := len(evt.Weights.Slice), 2; got != want {
		t.Fatalf("got %d weights, want %d", got, want)
	}
	if got, want := evt.Weights.At("nominal"), 1.0; got != want {
		t.Errorf("weight nominal: got %v, want %v", got, want)
	}
	if got, want := evt.Weights.At("alt"), 0.5; got != want {
		t.Errorf("weight alt: got %v, want %v", got, want)
	}

	// the attribute lines land on the fields v2 kept for them
	if evt.CrossSection == nil || evt.CrossSection.Value != 32 || evt.CrossSection.Error != 1 {
		t.Errorf("cross-section: got %#v, want value=32 error=1", evt.CrossSection)
	}
	pdf := evt.PdfInfo
	if pdf == nil {
		t.Fatal("no pdf info")
	}
	if pdf.ID1 != 1 || pdf.ID2 != 2 || pdf.X1 != 0.35 || pdf.X2 != 0.2 ||
		pdf.ScalePDF != 82 || pdf.Pdf1 != 1.1 || pdf.Pdf2 != 1.2 ||
		pdf.LHAPdf1 != 10042 || pdf.LHAPdf2 != 10042 {
		t.Errorf("pdf info: got %#v", pdf)
	}
	if got, want := evt.SignalProcessID, 20; got != want {
		t.Errorf("signal process id: got %d, want %d", got, want)
	}
	if evt.SignalVertex == nil || evt.SignalVertex.Barcode != -3 {
		t.Errorf("signal vertex: got %#v, want barcode -3", evt.SignalVertex)
	}
	if got, want := evt.Mpi, 2; got != want {
		t.Errorf("mpi: got %d, want %d", got, want)
	}
	if got, want := evt.AlphaQCD, 0.118; got != want {
		t.Errorf("alphaQCD: got %v, want %v", got, want)
	}
	if got, want := evt.AlphaQED, 7.297e-3; got != want {
		t.Errorf("alphaQED: got %v, want %v", got, want)
	}
	if got, want := evt.Scale, 91.0; got != want {
		t.Errorf("event scale: got %v, want %v", got, want)
	}
	if len(evt.RandomStates) != 2 || evt.RandomStates[0] != 12345 || evt.RandomStates[1] != 67890 {
		t.Errorf("random states: got %v, want [12345 67890]", evt.RandomStates)
	}

	// beams are the status-4 particles
	if evt.Beams[0] == nil || evt.Beams[0].Barcode != 1 ||
		evt.Beams[1] == nil || evt.Beams[1].Barcode != 2 {
		t.Errorf("beams: got %v, %v, want barcodes 1 and 2", evt.Beams[0], evt.Beams[1])
	}

	// explicit graph: vertex -3 consumes particles 3,4 and produces 5,6
	vtx := evt.Vertices[-3]
	if vtx == nil {
		t.Fatal("no vertex -3")
	}
	if got, want := vtx.ID, 5; got != want {
		t.Errorf("vertex -3 status: got %d, want %d", got, want)
	}
	if vtx.Position.X() != 0.1 || vtx.Position.Y() != 0.2 ||
		vtx.Position.Z() != 0.3 || vtx.Position.T() != 0.4 {
		t.Errorf("vertex -3 position: got %v", vtx.Position)
	}
	if len(vtx.ParticlesIn) != 2 || vtx.ParticlesIn[0].Barcode != 3 || vtx.ParticlesIn[1].Barcode != 4 {
		t.Errorf("vertex -3 incoming: got %v", barcodesOf(vtx.ParticlesIn))
	}
	if len(vtx.ParticlesOut) != 2 || vtx.ParticlesOut[0].Barcode != 5 || vtx.ParticlesOut[1].Barcode != 6 {
		t.Errorf("vertex -3 outgoing: got %v", barcodesOf(vtx.ParticlesOut))
	}

	// implicit graph: 7 and 8 name mother 6, so 6 decays at a re-created
	// vertex that produces exactly them
	p6, p7, p8 := evt.Particles[6], evt.Particles[7], evt.Particles[8]
	if p6.EndVertex == nil {
		t.Fatal("particle 6 has no end vertex")
	}
	if p7.ProdVertex != p6.EndVertex || p8.ProdVertex != p6.EndVertex {
		t.Error("particles 7 and 8 are not produced at particle 6's end vertex")
	}
	implicit := p6.EndVertex
	if got, want := implicit.Barcode, -4; got != want {
		t.Errorf("implicit vertex barcode: got %d, want %d", got, want)
	}
	if len(implicit.ParticlesIn) != 1 || implicit.ParticlesIn[0] != p6 {
		t.Errorf("implicit vertex incoming: got %v", barcodesOf(implicit.ParticlesIn))
	}

	// particle attributes restore polarization and flow
	p3 := evt.Particles[3]
	if p3.Polarization.Theta != 1.5 || p3.Polarization.Phi != 0.25 {
		t.Errorf("particle 3 polarization: got %#v", p3.Polarization)
	}
	if got, want := p3.Flow.Icode[1], 231; got != want {
		t.Errorf("particle 3 flow(1): got %d, want %d", got, want)
	}
	if got, want := p3.PdgID, int64(1); got != want {
		t.Errorf("particle 3 pdg: got %d, want %d", got, want)
	}
	if p3.Momentum.Px() != 0.75 || p3.Momentum.E() != 1500 {
		t.Errorf("particle 3 momentum: got %v", p3.Momentum)
	}
	if got, want := evt.Particles[6].GeneratedMass, 80.4; got != want {
		t.Errorf("particle 6 mass: got %v, want %v", got, want)
	}

	// the second event is independent of the first, but the run-info
	// weight names still apply to it
	evt2 := evts[1]
	if got, want := evt2.EventNumber, 2; got != want {
		t.Errorf("event number: got %d, want %d", got, want)
	}
	if got, want := len(evt2.Vertices), 1; got != want {
		t.Errorf("got %d vertices, want %d", got, want)
	}
	if got, want := len(evt2.Particles), 2; got != want {
		t.Errorf("got %d particles, want %d", got, want)
	}
	if evt2.Beams[0] == nil || evt2.Beams[0].Barcode != 1 || evt2.Beams[1] != nil {
		t.Errorf("beams: got %v, %v, want barcode 1 and nil", evt2.Beams[0], evt2.Beams[1])
	}
	if _, ok := evt2.Weights.Map["nominal"]; !ok {
		t.Error("run-info weight names did not reach the second event")
	}
}

func TestDecodeASCIIv3Truncated(t *testing.T) {
	// a listing cut off before its end marker still carries a complete
	// last event
	data, err := os.ReadFile("testdata/test.hepmc3")
	if err != nil {
		t.Fatalf("could not read fixture: %+v", err)
	}
	cut := bytes.LastIndex(data, []byte("HepMC::Asciiv3-END_EVENT_LISTING"))
	dec := hepmc.NewDecoder(bytes.NewReader(data[:cut]))

	var n int
	for {
		var evt hepmc.Event
		err := dec.Decode(&evt)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("could not decode event %d: %+v", n+1, err)
		}
		n++
	}
	if got, want := n, 2; got != want {
		t.Fatalf("got %d events, want %d", got, want)
	}
}

func TestDecodeASCIIv3RoundTripThroughV2(t *testing.T) {
	// what came out of a v3 file must survive the v2 encode/decode cycle:
	// the two formats describe the same event model
	evts := decodeASCIIv3File(t, "testdata/test.hepmc3")

	buf := new(bytes.Buffer)
	enc := hepmc.NewEncoder(buf)
	for _, evt := range evts {
		if err := enc.Encode(evt); err != nil {
			t.Fatalf("could not encode event %d: %+v", evt.EventNumber, err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("could not close encoder: %+v", err)
	}

	dec := hepmc.NewDecoder(buf)
	for _, want := range evts {
		var got hepmc.Event
		if err := dec.Decode(&got); err != nil {
			t.Fatalf("could not decode re-encoded event %d: %+v", want.EventNumber, err)
		}
		if got.EventNumber != want.EventNumber {
			t.Errorf("event number: got %d, want %d", got.EventNumber, want.EventNumber)
		}
		if len(got.Vertices) != len(want.Vertices) {
			t.Errorf("event %d: got %d vertices, want %d", want.EventNumber, len(got.Vertices), len(want.Vertices))
		}
		if len(got.Particles) != len(want.Particles) {
			t.Errorf("event %d: got %d particles, want %d", want.EventNumber, len(got.Particles), len(want.Particles))
		}
		if want.CrossSection != nil && (got.CrossSection == nil || *got.CrossSection != *want.CrossSection) {
			t.Errorf("event %d: cross-section did not round-trip", want.EventNumber)
		}
		if len(got.Weights.Slice) != len(want.Weights.Slice) {
			t.Errorf("event %d: got %d weights, want %d", want.EventNumber, len(got.Weights.Slice), len(want.Weights.Slice))
		}
	}
}

func TestDecodeASCIIv3Malformed(t *testing.T) {
	const header = "HepMC::Asciiv3-START_EVENT_LISTING\n"
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "a vertex naming an absent particle",
			body: "E 1 1 0\nU GEV MM\nV -1 0 [7]\nHepMC::Asciiv3-END_EVENT_LISTING\n",
		},
		{
			name: "a particle from an absent vertex",
			body: "E 1 1 1\nU GEV MM\nP 1 -9 11 0 0 1 1 0 1\nHepMC::Asciiv3-END_EVENT_LISTING\n",
		},
		{
			name: "a particle naming an absent mother",
			body: "E 1 0 1\nU GEV MM\nP 2 1 11 0 0 1 1 0 1\nHepMC::Asciiv3-END_EVENT_LISTING\n",
		},
		{
			name: "an unreadable momentum",
			body: "E 1 0 1\nU GEV MM\nP 1 0 11 zero 0 1 1 0 1\nHepMC::Asciiv3-END_EVENT_LISTING\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dec := hepmc.NewDecoder(strings.NewReader(header + tc.body))
			var evt hepmc.Event
			if err := dec.Decode(&evt); err == nil {
				t.Fatal("a malformed listing decoded without error")
			}
		})
	}
}

func barcodesOf(ps []*hepmc.Particle) []int {
	bcs := make([]int, len(ps))
	for i, p := range ps {
		bcs[i] = p.Barcode
	}
	return bcs
}
