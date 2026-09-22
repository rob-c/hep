// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rntup

// ClusterGroup points at the page list envelope describing a run of
// clusters, and says which entries those clusters cover.
type ClusterGroup struct {
	MinEntry  uint64 // the lowest entry number any of its clusters holds
	Span      uint64 // how many entries the group covers
	NClusters uint32

	PageList envelopeLink
}

// AttributeSet names an RNTuple of user metadata linked to this one.
type AttributeSet struct {
	SchemaMajor uint16
	SchemaMinor uint16
	AnchorLen   uint32
	Anchor      locator
	Name        string
}

// Footer describes an RNTuple's clusters, along with whatever was added to
// the schema after the header was written.
type Footer struct {
	Flags          []uint64
	HeaderChecksum uint64

	Groups     []ClusterGroup
	Attributes []AttributeSet
}

// readFooter parses a footer envelope, folding any schema extension into
// the schema the header gave.
func readFooter(r *rbuf, s *Schema) (*Footer, error) {
	var (
		f   Footer
		err error
	)

	f.Flags, err = r.featureFlags()
	if err != nil {
		return nil, err
	}
	f.HeaderChecksum = r.U64()

	// the schema extension holds whatever fields and columns were added
	// partway through writing. It reads exactly like the tail of the
	// header, and the IDs carry on from where the header left off.
	ext := r.record()
	if r.Err() != nil {
		return nil, r.Err()
	}
	err = s.readDescription(r)
	if err != nil {
		return nil, err
	}
	s.link()
	r.done(ext)

	f.readGroups(r)
	f.readAttributes(r)

	return &f, r.Err()
}

func (f *Footer) readGroups(r *rbuf) {
	list := r.list()
	if r.Err() != nil {
		return
	}

	for range int(list.items) {
		rec := r.record()
		if r.Err() != nil {
			return
		}

		g := ClusterGroup{
			MinEntry:  r.U64(),
			Span:      r.U64(),
			NClusters: r.U32(),
		}
		g.PageList = r.envelopeLink()
		if r.Err() != nil {
			return
		}

		f.Groups = append(f.Groups, g)
		r.done(rec)
	}
	r.done(list)
}

func (f *Footer) readAttributes(r *rbuf) {
	// the list of linked attribute sets was added in 1.1.0.0, so a footer
	// written before that simply ends here.
	if r.Pos() >= len(r.p) {
		return
	}

	list := r.list()
	if r.Err() != nil {
		return
	}

	for range int(list.items) {
		rec := r.record()
		if r.Err() != nil {
			return
		}

		a := AttributeSet{
			SchemaMajor: r.U16(),
			SchemaMinor: r.U16(),
			AnchorLen:   r.U32(),
		}
		a.Anchor = r.locator()
		a.Name = r.String()
		if r.Err() != nil {
			return
		}

		f.Attributes = append(f.Attributes, a)
		r.done(rec)
	}
	r.done(list)
}
