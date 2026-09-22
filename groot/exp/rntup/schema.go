// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rntup

import "fmt"

// StructRole describes what a field is in the schema tree.
type StructRole uint16

const (
	RoleLeaf       StructRole = 0x00 // a plain field, carrying no structure of its own
	RoleCollection StructRole = 0x01 // the parent of a collection, such as a vector
	RoleRecord     StructRole = 0x02 // the parent of a record, such as a struct
	RoleVariant    StructRole = 0x03 // the parent of a variant
	RoleStreamer   StructRole = 0x04 // objects written with the ROOT streamer
)

func (r StructRole) String() string {
	switch r {
	case RoleLeaf:
		return "leaf"
	case RoleCollection:
		return "collection"
	case RoleRecord:
		return "record"
	case RoleVariant:
		return "variant"
	case RoleStreamer:
		return "streamer"
	default:
		return fmt.Sprintf("StructRole(%#x)", uint16(r))
	}
}

// field flags.
const (
	fieldRepetitive = 0x01 // a fixed-size array: every entry holds ArraySize copies
	fieldProjected  = 0x02 // a virtual field, reading another field's columns
	fieldChecksum   = 0x04 // carries the ROOT streamer checksum of its type
	fieldSoA        = 0x08 // a collection held in memory as a struct of arrays
)

// Field describes one node of an RNTuple's schema tree.
type Field struct {
	ID          int    // the field's own ID, its index in the schema
	Parent      int    // the ID of the enclosing field; a top-level field is its own parent
	Version     uint32 // field version, for schema evolution
	TypeVersion uint32
	Role        StructRole
	Flags       uint16

	Name        string
	Type        string // the C++ type name
	TypeAlias   string
	Description string

	ArraySize uint64 // for a fixed-size array, the number of elements
	Source    uint32 // for a projected field, the field it projects
	Checksum  uint32 // the ROOT streamer checksum of the type

	Children []int // the IDs of the subfields, in schema order
	Columns  []int // the IDs of the columns attached to this field

	// Reps groups Columns by representation. A field may be written
	// through more than one alternative set of columns, of which exactly
	// one is live in any given cluster and the rest are suppressed there.
	// Reps[r] lists the columns of the r-th representation, in order.
	Reps [][]int
}

// Repetitive reports whether the field is a fixed-size array.
func (f *Field) Repetitive() bool { return f.Flags&fieldRepetitive != 0 }

// Projected reports whether the field reads another field's columns.
func (f *Field) Projected() bool { return f.Flags&fieldProjected != 0 }

// TopLevel reports whether the field sits at the top of the schema tree.
func (f *Field) TopLevel() bool { return f.Parent == f.ID }

// Column describes one column of on-disk values.
type Column struct {
	ID     int // the column's own ID, its index in the schema
	Type   ColType
	Bits   uint16 // bits on storage per element
	Field  int    // the field the column is attached to
	Flags  uint16
	RepIdx uint16 // which of the field's alternative representations this is

	FirstElement int64   // for a deferred column, where its elements start
	Min, Max     float64 // for a column with a range, the values it may take

}

// Alias is a column that has no pages of its own and reads another
// column's, which is how a projected field presents existing data under a
// different C++ type.
//
// Alias columns are not numbered: only physical columns have IDs, and only
// physical columns are referenced by the footer and the page lists.
type Alias struct {
	Physical int // the column the data actually comes from
	Field    int // the projected field the data is presented as
}

// column flags.
const (
	colDeferred = 0x01 // the first element is not at index zero
	colRange    = 0x02 // the column carries the range of values it may hold
)

// Deferred reports whether the column was added partway through writing, so
// that the entries before FirstElement read as zero.
func (c *Column) Deferred() bool { return c.Flags&colDeferred != 0 }

// Suppressed reports whether the column is deferred and has no pages at all
// up to and including the cluster its first element falls in.
func (c *Column) Suppressed() bool { return c.Deferred() && c.FirstElement < 0 }

// ExtraType carries information some field types need beyond their name,
// such as the ROOT streamer info for streamed objects.
type ExtraType struct {
	ContentID   uint32
	TypeVersion uint32
	TypeName    string
	Content     string
}

// Schema is an RNTuple's description of its fields and columns.
type Schema struct {
	Name        string
	Description string
	Writer      string // the library or program that wrote the data

	Fields  []Field
	Columns []Column // the physical columns, indexed by their column ID
	Aliases []Alias
	Extra   []ExtraType

	Flags []uint64
}

// Field returns the field with the given ID.
func (s *Schema) Field(id int) *Field {
	if id < 0 || id >= len(s.Fields) {
		return nil
	}
	return &s.Fields[id]
}

// TopLevel returns the IDs of the top-level fields, which are the ones an
// entry is made of.
func (s *Schema) TopLevel() []int {
	var out []int
	for i := range s.Fields {
		if s.Fields[i].TopLevel() {
			out = append(out, i)
		}
	}
	return out
}

// Lookup returns the top-level field of the given name.
func (s *Schema) Lookup(name string) *Field {
	for _, id := range s.TopLevel() {
		if s.Fields[id].Name == name {
			return &s.Fields[id]
		}
	}
	return nil
}

// readHeader parses a header envelope into a schema.
func readHeader(r *rbuf) (*Schema, error) {
	var (
		s   Schema
		err error
	)

	s.Flags, err = r.featureFlags()
	if err != nil {
		return nil, err
	}

	s.Name = r.String()
	s.Description = r.String()
	s.Writer = r.String()

	err = s.readDescription(r)
	if err != nil {
		return nil, err
	}

	s.link()
	return &s, r.Err()
}

// readDescription reads the four list frames that describe fields and
// columns. They appear at the end of the header envelope and again, for
// whatever was added partway through writing, in the footer's schema
// extension.
func (s *Schema) readDescription(r *rbuf) error {
	s.readFields(r)
	s.readColumns(r)
	s.readAliasColumns(r)
	s.readExtraTypes(r)
	return r.Err()
}

func (s *Schema) readFields(r *rbuf) {
	list := r.list()
	if r.Err() != nil {
		return
	}

	for i := range int(list.items) {
		rec := r.record()
		if r.Err() != nil {
			return
		}

		f := Field{ID: len(s.Fields)}
		f.Version = r.U32()
		f.TypeVersion = r.U32()
		f.Parent = int(r.U32())
		f.Role = StructRole(r.U16())
		f.Flags = r.U16()

		f.Name = r.String()
		f.Type = r.String()
		f.TypeAlias = r.String()
		f.Description = r.String()

		if f.Flags&fieldRepetitive != 0 {
			f.ArraySize = r.U64()
		}
		if f.Flags&fieldProjected != 0 {
			f.Source = r.U32()
		}
		if f.Flags&fieldChecksum != 0 {
			f.Checksum = r.U32()
		}

		if r.Err() != nil {
			r.setErr(fmt.Errorf("rntup: could not read field %d: %w", i, r.Err()))
			return
		}

		s.Fields = append(s.Fields, f)
		r.done(rec)
	}
	r.done(list)
}

func (s *Schema) readColumns(r *rbuf) {
	list := r.list()
	if r.Err() != nil {
		return
	}

	for i := range int(list.items) {
		rec := r.record()
		if r.Err() != nil {
			return
		}

		c := Column{ID: len(s.Columns)}
		c.Type = ColType(r.U16())
		c.Bits = r.U16()
		c.Field = int(r.U32())
		c.Flags = r.U16()
		c.RepIdx = r.U16()

		if c.Flags&colDeferred != 0 {
			c.FirstElement = r.I64()
		}
		if c.Flags&colRange != 0 {
			c.Min = r.F64()
			c.Max = r.F64()
		}

		if r.Err() != nil {
			r.setErr(fmt.Errorf("rntup: could not read column %d: %w", i, r.Err()))
			return
		}

		s.Columns = append(s.Columns, c)
		r.done(rec)
	}
	r.done(list)
}

// readAliasColumns reads the columns that carry no pages of their own and
// stand in for a physical column, which is how a projected field presents
// another field's data under a different type.
func (s *Schema) readAliasColumns(r *rbuf) {
	list := r.list()
	if r.Err() != nil {
		return
	}

	for range int(list.items) {
		rec := r.record()
		if r.Err() != nil {
			return
		}

		a := Alias{Physical: int(r.U32()), Field: int(r.U32())}
		if r.Err() != nil {
			return
		}

		s.Aliases = append(s.Aliases, a)
		r.done(rec)
	}
	r.done(list)
}

func (s *Schema) readExtraTypes(r *rbuf) {
	list := r.list()
	if r.Err() != nil {
		return
	}

	for range int(list.items) {
		rec := r.record()
		if r.Err() != nil {
			return
		}

		x := ExtraType{
			ContentID:   r.U32(),
			TypeVersion: r.U32(),
			TypeName:    r.String(),
		}
		s.Extra = append(s.Extra, x)
		r.done(rec)
	}
	r.done(list)
}

// link fills in the parent-to-child and field-to-column references the
// serialized form leaves implicit in the ordering.
func (s *Schema) link() {
	for i := range s.Fields {
		s.Fields[i].Children = nil
		s.Fields[i].Columns = nil
	}
	for i := range s.Fields {
		f := &s.Fields[i]
		if f.TopLevel() {
			continue
		}
		if p := s.Field(f.Parent); p != nil {
			p.Children = append(p.Children, f.ID)
		}
	}
	for i := range s.Columns {
		c := &s.Columns[i]
		if f := s.Field(c.Field); f != nil {
			f.Columns = append(f.Columns, c.ID)
		}
	}
	// a projected field reads the physical columns its aliases name, so
	// from here on it is served exactly like the field it projects.
	for _, a := range s.Aliases {
		if f := s.Field(a.Field); f != nil {
			f.Columns = append(f.Columns, a.Physical)
		}
	}

	for i := range s.Fields {
		f := &s.Fields[i]
		f.Reps = nil
		for _, id := range f.Columns {
			r := int(s.Columns[id].RepIdx)
			for len(f.Reps) <= r {
				f.Reps = append(f.Reps, nil)
			}
			f.Reps[r] = append(f.Reps[r], id)
		}
	}
}

// slot returns the columns that can serve the j-th column of the field, one
// per representation. Which of them is live depends on the cluster.
func (f *Field) slot(j int) []int {
	var out []int
	for _, rep := range f.Reps {
		if j < len(rep) {
			out = append(out, rep[j])
		}
	}
	return out
}

// nslots returns how many columns one representation of the field has.
func (f *Field) nslots() int {
	if len(f.Reps) == 0 {
		return 0
	}
	return len(f.Reps[0])
}
