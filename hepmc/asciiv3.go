// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// HepMC3 Asciiv3 reader.
//
// The v3 grammar differs from every v2 flavour: the run info (weight names,
// tools, run attributes) precedes the first event instead of riding on each
// 'E' line, the graph is expressed as vertices listing their incoming
// particles plus particles naming their parent, and everything that v2 kept
// in dedicated records (cross-section, PDF info, signal process) arrives as
// 'A' attribute lines. Decoding maps all of that back onto the v2 Event
// model this package exposes, so a caller reads a v3 file exactly as they
// read a v2 one.

package hepmc

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"go-hep.org/x/hep/fmom"
)

// v3vtx is a vertex line, kept verbatim until the whole event has been read:
// its incoming list may name particles that appear later in the listing.
type v3vtx struct {
	id     int
	status int
	in     []int
	pos    fmom.PxPyPzE
}

// v3particle is a particle line. parent < 0 names the production vertex,
// parent > 0 names the mother particle whose (possibly elided) end vertex is
// the production vertex, parent == 0 means beam or vacuum.
type v3particle struct {
	id     int
	parent int
	pdg    int64
	mom    fmom.PxPyPzE
	mass   float64
	status int
}

// v3attr is an attribute line, buffered because the writer emits them right
// after 'E', before the vertices and particles they may refer to.
type v3attr struct {
	name string
	toks tokens
}

func (dec *Decoder) decodeASCIIv3(evt *Event) error {
	if dec.v3.done {
		return io.EOF
	}

	var (
		seenE   bool
		vtxs    []v3vtx
		parts   []v3particle
		weights []float64
		attrs   = make(map[int][]v3attr) // owner id; 0 is the event itself
	)

	finish := func() error {
		return dec.finishASCIIv3(evt, vtxs, parts, weights, attrs)
	}

	for {
		var (
			toks tokens
			err  error
		)
		switch {
		case dec.v3.hasHeld:
			toks, dec.v3.hasHeld = dec.v3.held, false
		default:
			toks, err = dec.readline()
			if err != nil {
				if err == io.EOF && seenE {
					// A listing truncated before its end marker still
					// carries a complete last event.
					dec.v3.done = true
					return finish()
				}
				return err
			}
		}
		if len(toks.toks) == 0 {
			continue
		}
		raw := toks
		switch key := toks.next(); key {
		case "":
			// empty line

		case endASCIIv3:
			dec.v3.done = true
			if !seenE {
				return io.EOF
			}
			return finish()

		case "E":
			if seenE {
				// the first line of the next event: replay it on the
				// next Decode call.
				dec.v3.held, dec.v3.hasHeld = raw, true
				return finish()
			}
			seenE = true
			evt.EventNumber, err = toks.int()
			if err != nil {
				return fmt.Errorf("hepmc: could not decode event number: %w", err)
			}
			// The vertex and particle counts re-derive from the V and P
			// lines below; the optional "@ x y z t" event shift has no
			// HepMC2 equivalent.

		case "U":
			err = dec.decodeUnits(evt, raw)
			if err != nil {
				return err
			}

		case "W":
			switch {
			case seenE:
				// event weight values
				weights = make([]float64, 0, len(toks.toks)-toks.pos)
				for toks.pos < len(toks.toks) {
					v, err := toks.float64()
					if err != nil {
						return fmt.Errorf("hepmc: could not decode weight value: %w", err)
					}
					weights = append(weights, v)
				}
			default:
				// run info: the weight names, valid for every event
				dec.v3.names = append([]string(nil), toks.toks[toks.pos:]...)
			}

		case "T":
			// run info: a tool description; no HepMC2 equivalent

		case "A":
			if !seenE {
				// run-info attribute; no HepMC2 equivalent
				continue
			}
			id, err := toks.int()
			if err != nil {
				return fmt.Errorf("hepmc: could not decode attribute owner: %w", err)
			}
			attrs[id] = append(attrs[id], v3attr{name: toks.next(), toks: toks})

		case "V":
			var v v3vtx
			v.id, err = toks.int()
			if err != nil {
				return fmt.Errorf("hepmc: could not decode vertex id: %w", err)
			}
			v.status, err = toks.int()
			if err != nil {
				return fmt.Errorf("hepmc: could not decode vertex status: %w", err)
			}
			for toks.pos < len(toks.toks) {
				switch tok := toks.next(); {
				case tok == "@":
					var pos [4]float64
					for i := range pos {
						pos[i], err = toks.float64()
						if err != nil {
							return fmt.Errorf("hepmc: could not decode vertex position: %w", err)
						}
					}
					v.pos = fmom.NewPxPyPzE(pos[0], pos[1], pos[2], pos[3])
				case strings.HasPrefix(tok, "["):
					list := strings.Trim(tok, "[]")
					if list == "" {
						continue
					}
					for _, s := range strings.Split(list, ",") {
						pid, err := strconv.Atoi(s)
						if err != nil {
							return fmt.Errorf("hepmc: could not decode vertex %d incoming list: %w", v.id, err)
						}
						v.in = append(v.in, pid)
					}
				}
			}
			vtxs = append(vtxs, v)

		case "P":
			var (
				p   v3particle
				mom [4]float64
			)
			p.id, err = toks.int()
			if err != nil {
				return fmt.Errorf("hepmc: could not decode particle id: %w", err)
			}
			p.parent, err = toks.int()
			if err != nil {
				return fmt.Errorf("hepmc: could not decode particle parent: %w", err)
			}
			p.pdg, err = toks.int64()
			if err != nil {
				return fmt.Errorf("hepmc: could not decode particle pdg id: %w", err)
			}
			for i := range mom {
				mom[i], err = toks.float64()
				if err != nil {
					return fmt.Errorf("hepmc: could not decode particle momentum: %w", err)
				}
			}
			p.mom = fmom.NewPxPyPzE(mom[0], mom[1], mom[2], mom[3])
			p.mass, err = toks.float64()
			if err != nil {
				return fmt.Errorf("hepmc: could not decode particle mass: %w", err)
			}
			p.status, err = toks.int()
			if err != nil {
				return fmt.Errorf("hepmc: could not decode particle status: %w", err)
			}
			parts = append(parts, p)

		default:
			// "HepMC::Version" and whatever future headers appear

		}
	}
}

// finishASCIIv3 turns the buffered lines of one event into the wired v2
// graph: vertices and particles by barcode, incoming lists resolved to
// EndVertex links, parents resolved to ProdVertex links — re-creating the
// vertices the writer elided — and the attributes mapped onto the Event
// fields v2 carried natively.
func (dec *Decoder) finishASCIIv3(evt *Event, vtxs []v3vtx, parts []v3particle, weights []float64, attrs map[int][]v3attr) error {
	evt.Vertices = make(map[int]*Vertex, len(vtxs))
	evt.Particles = make(map[int]*Particle, len(parts))

	minVtx := 0
	for _, v := range vtxs {
		if _, dup := evt.Vertices[v.id]; dup {
			return fmt.Errorf("hepmc: duplicate vertex id %d", v.id)
		}
		if v.id < minVtx {
			minVtx = v.id
		}
		evt.Vertices[v.id] = &Vertex{
			Position: v.pos,
			ID:       v.status,
			Event:    evt,
			Barcode:  v.id,
		}
	}
	for _, p := range parts {
		if _, dup := evt.Particles[p.id]; dup {
			return fmt.Errorf("hepmc: duplicate particle id %d", p.id)
		}
		evt.Particles[p.id] = &Particle{
			Momentum:      p.mom,
			PdgID:         p.pdg,
			Status:        p.status,
			GeneratedMass: p.mass,
			Barcode:       p.id,
		}
	}

	for _, v := range vtxs {
		vtx := evt.Vertices[v.id]
		for _, pid := range v.in {
			p, ok := evt.Particles[pid]
			if !ok {
				return fmt.Errorf("hepmc: vertex %d lists unknown particle %d", v.id, pid)
			}
			p.EndVertex = vtx
			vtx.ParticlesIn = append(vtx.ParticlesIn, p)
		}
	}

	implicit := minVtx - 1
	for _, pr := range parts {
		p := evt.Particles[pr.id]
		switch {
		case pr.parent < 0:
			vtx, ok := evt.Vertices[pr.parent]
			if !ok {
				return fmt.Errorf("hepmc: particle %d produced at unknown vertex %d", pr.id, pr.parent)
			}
			p.ProdVertex = vtx
			vtx.ParticlesOut = append(vtx.ParticlesOut, p)
		case pr.parent > 0:
			mother, ok := evt.Particles[pr.parent]
			if !ok {
				return fmt.Errorf("hepmc: particle %d names unknown mother %d", pr.id, pr.parent)
			}
			vtx := mother.EndVertex
			if vtx == nil {
				// the writer elided a vertex nothing else refers to;
				// re-create it between mother and child.
				vtx = &Vertex{Event: evt, Barcode: implicit}
				evt.Vertices[implicit] = vtx
				implicit--
				mother.EndVertex = vtx
				vtx.ParticlesIn = append(vtx.ParticlesIn, mother)
			}
			p.ProdVertex = vtx
			vtx.ParticlesOut = append(vtx.ParticlesOut, p)
		}
	}

	for _, vtx := range evt.Vertices {
		sort.Sort(Particles(vtx.ParticlesIn))
		sort.Sort(Particles(vtx.ParticlesOut))
	}

	// beams are the status-4 particles, in listing order
	for _, pr := range parts {
		if pr.status != 4 {
			continue
		}
		switch p := evt.Particles[pr.id]; {
		case evt.Beams[0] == nil:
			evt.Beams[0] = p
		case evt.Beams[1] == nil:
			evt.Beams[1] = p
		}
	}

	if len(weights) > 0 || len(dec.v3.names) > 0 {
		evt.Weights = Weights{
			Slice: weights,
			Map:   make(map[string]int, len(dec.v3.names)),
		}
		for i, n := range dec.v3.names {
			evt.Weights.Map[n] = i
		}
	}

	for id, as := range attrs {
		for i := range as {
			err := applyASCIIv3Attr(evt, id, &as[i])
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// applyASCIIv3Attr maps one attribute back onto the field HepMC2 kept for
// it. Attributes with no v2 home — including GenHeavyIon, whose v3 encoding
// is version-dependent — are dropped rather than guessed at.
func applyASCIIv3Attr(evt *Event, id int, a *v3attr) error {
	if id != 0 {
		return applyASCIIv3ParticleAttr(evt, id, a)
	}
	var err error
	switch a.name {
	case "GenCrossSection":
		var xs CrossSection
		xs.Value, err = a.toks.float64()
		if err == nil {
			xs.Error, err = a.toks.float64()
		}
		if err != nil {
			return fmt.Errorf("hepmc: could not decode GenCrossSection: %w", err)
		}
		evt.CrossSection = &xs

	case "GenPdfInfo":
		var pdf PdfInfo
		for _, dst := range []any{
			&pdf.ID1, &pdf.ID2, &pdf.X1, &pdf.X2,
			&pdf.ScalePDF, &pdf.Pdf1, &pdf.Pdf2,
			&pdf.LHAPdf1, &pdf.LHAPdf2,
		} {
			switch dst := dst.(type) {
			case *int:
				*dst, err = a.toks.int()
			case *float64:
				*dst, err = a.toks.float64()
			}
			if err != nil {
				return fmt.Errorf("hepmc: could not decode GenPdfInfo: %w", err)
			}
		}
		evt.PdfInfo = &pdf

	case "signal_process_id":
		evt.SignalProcessID, err = a.toks.int()
		if err != nil {
			return fmt.Errorf("hepmc: could not decode signal_process_id: %w", err)
		}

	case "signal_process_vertex", "signal_vertex_id":
		bc, err := a.toks.int()
		if err != nil {
			return fmt.Errorf("hepmc: could not decode %s: %w", a.name, err)
		}
		vtx, ok := evt.Vertices[bc]
		if !ok {
			return fmt.Errorf("hepmc: signal process vertex %d does not exist", bc)
		}
		evt.SignalVertex = vtx

	case "mpi":
		evt.Mpi, err = a.toks.int()
		if err != nil {
			return fmt.Errorf("hepmc: could not decode mpi: %w", err)
		}

	case "event_scale":
		evt.Scale, err = a.toks.float64()
		if err != nil {
			return fmt.Errorf("hepmc: could not decode event_scale: %w", err)
		}

	case "alphaQCD":
		evt.AlphaQCD, err = a.toks.float64()
		if err != nil {
			return fmt.Errorf("hepmc: could not decode alphaQCD: %w", err)
		}

	case "alphaQED":
		evt.AlphaQED, err = a.toks.float64()
		if err != nil {
			return fmt.Errorf("hepmc: could not decode alphaQED: %w", err)
		}

	case "random_states":
		for a.toks.pos < len(a.toks.toks) {
			v, err := a.toks.int64()
			if err != nil {
				return fmt.Errorf("hepmc: could not decode random_states: %w", err)
			}
			evt.RandomStates = append(evt.RandomStates, v)
		}
	}
	return nil
}

// applyASCIIv3ParticleAttr restores the per-particle state the v2→v3
// conversion moved into attributes: polarization as theta/phi, flow codes as
// flowN. Vertex attributes (id < 0) have no v2 home.
func applyASCIIv3ParticleAttr(evt *Event, id int, a *v3attr) error {
	if id < 0 {
		return nil
	}
	p, ok := evt.Particles[id]
	if !ok {
		return fmt.Errorf("hepmc: attribute %q names unknown particle %d", a.name, id)
	}
	var err error
	switch {
	case a.name == "theta":
		p.Polarization.Theta, err = a.toks.float64()
	case a.name == "phi":
		p.Polarization.Phi, err = a.toks.float64()
	case strings.HasPrefix(a.name, "flow"):
		idx, cerr := strconv.Atoi(a.name[len("flow"):])
		if cerr != nil {
			return nil // not a flow code after all
		}
		var code int
		code, err = a.toks.int()
		if err == nil {
			if p.Flow.Icode == nil {
				p.Flow.Icode = make(map[int]int)
				p.Flow.Particle = p
			}
			p.Flow.Icode[idx] = code
		}
	}
	if err != nil {
		return fmt.Errorf("hepmc: could not decode particle attribute %q: %w", a.name, err)
	}
	return nil
}
