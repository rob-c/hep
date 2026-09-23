// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rntup

import (
	"encoding/binary"
	"fmt"
	"math"
	"reflect"

	"github.com/zeebo/xxh3"
	"go-hep.org/x/hep/groot/internal/rcompress"
	"go-hep.org/x/hep/groot/riofs"
)

// WriteVar binds a Go value to a field of the RNTuple being written.
type WriteVar struct {
	Name  string
	Value any // a pointer to the value written for each entry
}

// WriteOption configures a Writer.
type WriteOption func(*writeConfig)

type writeConfig struct {
	entries uint64 // how many entries to gather before starting a new cluster
	descr   string
	compr   int32 // ROOT's encoding: the algorithm times a hundred, plus the level
	plain   bool  // write the plain column encodings rather than the split ones
}

func newWriteConfig(opts []WriteOption) *writeConfig {
	cfg := &writeConfig{
		entries: 64000,
		compr:   rcompress.Settings{Alg: rcompress.ZLIB, Lvl: 1}.Compression(),
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// Compression sets how the pages and the envelopes are compressed, in
// ROOT's encoding: the algorithm times a hundred plus the level, so 101 is
// zlib at its fastest and 505 is zstd in the middle of its range. Zero
// writes everything as it stands.
//
// Pages are compressed one at a time, and one that does not come out smaller
// is stored as it was, which is what tells a reader to take it as it finds
// it.
func Compression(compr int32) WriteOption {
	return func(cfg *writeConfig) { cfg.compr = compr }
}

// ClusterSize sets how many entries are gathered before a cluster is
// written out. A reader skips whole clusters to reach an entry range, so
// this is the granularity of reading part of an RNTuple.
func ClusterSize(entries uint64) WriteOption {
	return func(cfg *writeConfig) {
		if entries > 0 {
			cfg.entries = entries
		}
	}
}

// Description sets the description the RNTuple carries.
func Description(s string) WriteOption {
	return func(cfg *writeConfig) { cfg.descr = s }
}

// PlainEncoding writes the plain column encodings rather than the split
// ones.
//
// The split encodings are what is written otherwise, and what ROOT writes:
// they rearrange a page so that the bytes of its elements sit beside the
// bytes that resemble them, which costs nothing and compresses a great deal
// better. Both are in the specification and a reader has to take either, so
// this is here for comparing the two rather than because anything needs it.
func PlainEncoding() WriteOption {
	return func(cfg *writeConfig) { cfg.plain = true }
}

// Writer writes an RNTuple into a ROOT file.
//
// The fields are fixed when the writer is made, from the Go types bound to
// them, and every call to Write appends one entry from whatever those values
// hold at the time:
//
//	var (
//		n  int32
//		xs []float64
//	)
//	w, err := rntup.Create("out.root", "ntuple", []rntup.WriteVar{
//		{Name: "n", Value: &n},
//		{Name: "xs", Value: &xs},
//	})
//	...
//	for i := range 1000 {
//		n, xs = int32(i), []float64{float64(i)}
//		err = w.Write()
//	}
//	err = w.Close()
//
// Close is what writes the footer and the anchor, so an RNTuple whose writer
// was not closed is not readable.
type Writer struct {
	f    *riofs.File
	own  bool // the writer opened the file and must close it
	name string
	cfg  *writeConfig

	schema *Schema
	fields []fieldWriter
	cols   []*colBuf

	entries  uint64 // entries written in all
	inClust  uint64 // entries written into the cluster being gathered
	clusters []Cluster

	hdr    envelopeLink
	hdrSum uint64 // the header's checksum, which the footer and the page list repeat
	closed bool
}

// Create makes a ROOT file holding one RNTuple.
func Create(path, name string, wvars []WriteVar, opts ...WriteOption) (*Writer, error) {
	f, err := riofs.Create(path)
	if err != nil {
		return nil, fmt.Errorf("rntup: could not create %q: %w", path, err)
	}

	w, err := NewWriter(f, name, wvars, opts...)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	w.own = true
	return w, nil
}

// NewWriter writes an RNTuple into an already-open file.
//
// The file is not closed when the writer is, but it does have to be closed
// afterwards for the RNTuple to be readable.
func NewWriter(f *riofs.File, name string, wvars []WriteVar, opts ...WriteOption) (*Writer, error) {
	if len(wvars) == 0 {
		return nil, fmt.Errorf("rntup: an RNTuple needs at least one field")
	}

	w := &Writer{
		f:    f,
		name: name,
		cfg:  newWriteConfig(opts),
		schema: &Schema{
			Name:   name,
			Writer: "go-hep",
		},
	}
	w.schema.Description = w.cfg.descr

	for _, wvar := range wvars {
		if wvar.Name == "" {
			return nil, fmt.Errorf("rntup: a field with no name")
		}
		rv := reflect.ValueOf(wvar.Value)
		if rv.Kind() != reflect.Pointer || rv.IsNil() {
			return nil, fmt.Errorf(
				"rntup: field %q must be bound to a non-nil pointer, got %T",
				wvar.Name, wvar.Value,
			)
		}

		fw, err := w.addField(wvar.Name, rv.Elem().Type(), -1)
		if err != nil {
			return nil, err
		}
		fw.bind(rv.Elem())
		w.fields = append(w.fields, fw)
	}

	w.schema.link()

	err := w.writeHeader()
	if err != nil {
		return nil, err
	}
	return w, nil
}

// Entries returns how many entries have been written.
func (w *Writer) Entries() uint64 { return w.entries }

// Schema returns the description of the fields and columns being written.
func (w *Writer) Schema() *Schema { return w.schema }

// ---------------------------------------------------------------- schema

// colBuf gathers one column's elements until the cluster is written out.
type colBuf struct {
	col *Column

	data []byte // the elements, encoded
	n    uint64 // how many of them this cluster holds

	// first is the element this cluster's elements start at, counted from
	// the start of the column.
	first uint64

	// running is the number of elements the child of a collection has
	// taken in this cluster, which is what an index column records.
	running uint64

	// bits holds a boolean column's values until they are packed, since
	// they take less than a byte each on disk.
	bits []bool
}

func (w *Writer) addColumn(typ ColType, bits uint16, field int) *colBuf {
	c := Column{ID: len(w.schema.Columns), Type: typ, Bits: bits, Field: field}
	w.schema.Columns = append(w.schema.Columns, c)

	cb := &colBuf{col: &w.schema.Columns[len(w.schema.Columns)-1]}
	w.cols = append(w.cols, cb)
	return cb
}

// addField adds a field, and the columns under it, for a Go type.
func (w *Writer) addField(name string, typ reflect.Type, parent int) (fieldWriter, error) {
	id := len(w.schema.Fields)
	if parent < 0 {
		parent = id // a top-level field is its own parent
	}

	cname, err := cppName(typ)
	if err != nil {
		return nil, fmt.Errorf("rntup: field %q: %w", name, err)
	}

	f := Field{ID: id, Parent: parent, Name: name, Type: cname}
	w.schema.Fields = append(w.schema.Fields, f)
	self := &w.schema.Fields[id]

	switch typ.Kind() {
	case reflect.Bool, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		ct, bits, err := colTypeOf(typ, w.cfg.plain)
		if err != nil {
			return nil, fmt.Errorf("rntup: field %q: %w", name, err)
		}
		return &scalarWriter{buf: w.addColumn(ct, bits, id), kind: typ.Kind()}, nil

	case reflect.String:
		self.Role = RoleLeaf
		return &stringWriter{
			index: w.addColumn(w.indexType(), 64, id),
			chars: w.addColumn(ColChar, 8, id),
		}, nil

	case reflect.Slice:
		self.Role = RoleCollection
		index := w.addColumn(w.indexType(), 64, id)
		elem, err := w.addField("_0", typ.Elem(), id)
		if err != nil {
			return nil, err
		}
		return &sliceWriter{index: index, elem: elem}, nil

	case reflect.Array:
		self.Role = RoleLeaf
		self.Flags |= fieldRepetitive
		self.ArraySize = uint64(typ.Len())
		elem, err := w.addField("_0", typ.Elem(), id)
		if err != nil {
			return nil, err
		}
		return &arrayWriter{n: typ.Len(), elem: elem}, nil

	case reflect.Struct:
		self.Role = RoleRecord
		rec := &recordWriter{}
		for i := range typ.NumField() {
			sf := typ.Field(i)
			if sf.PkgPath != "" {
				continue // unexported: nothing to write
			}
			mname := sf.Name
			if tag, ok := sf.Tag.Lookup("rntup"); ok && tag != "" {
				mname = tag
			}
			m, err := w.addField(mname, sf.Type, id)
			if err != nil {
				return nil, err
			}
			rec.members = append(rec.members, member{index: i, w: m})
		}
		if len(rec.members) == 0 {
			return nil, fmt.Errorf("rntup: field %q is a struct with nothing in it to write", name)
		}
		return rec, nil
	}

	return nil, fmt.Errorf("rntup: field %q: nothing is known about how to write a %s", name, typ)
}

// indexType returns the column collection offsets are kept in.
func (w *Writer) indexType() ColType {
	if w.cfg.plain {
		return ColIndex64
	}
	return ColSplitIndex64
}

// colTypeOf returns the column a Go type's values are kept in.
//
// The split encodings are used unless the plain ones were asked for. A type
// that takes a single byte has no split form, there being nothing to
// rearrange.
func colTypeOf(typ reflect.Type, plain bool) (ColType, uint16, error) {
	switch typ.Kind() {
	case reflect.Bool:
		return ColBit, 1, nil
	case reflect.Int8:
		return ColInt8, 8, nil
	case reflect.Uint8:
		return ColUInt8, 8, nil
	}

	var split, flat ColType
	var bits uint16
	switch typ.Kind() {
	case reflect.Int16:
		split, flat, bits = ColSplitInt16, ColInt16, 16
	case reflect.Uint16:
		split, flat, bits = ColSplitUInt16, ColUInt16, 16
	case reflect.Int32:
		split, flat, bits = ColSplitInt32, ColInt32, 32
	case reflect.Uint32:
		split, flat, bits = ColSplitUInt32, ColUInt32, 32
	case reflect.Int64:
		split, flat, bits = ColSplitInt64, ColInt64, 64
	case reflect.Uint64:
		split, flat, bits = ColSplitUInt64, ColUInt64, 64
	case reflect.Float32:
		split, flat, bits = ColSplitReal32, ColReal32, 32
	case reflect.Float64:
		split, flat, bits = ColSplitReal64, ColReal64, 64
	default:
		return 0, 0, fmt.Errorf("nothing is known about how to store a %s", typ)
	}

	if plain {
		return flat, bits, nil
	}
	return split, bits, nil
}

// cppName returns the C++ type a Go type stands for, which is what the
// schema records and what another reader will go by.
func cppName(typ reflect.Type) (string, error) {
	switch typ.Kind() {
	case reflect.Bool:
		return "bool", nil
	case reflect.Int8:
		return "std::int8_t", nil
	case reflect.Uint8:
		return "std::uint8_t", nil
	case reflect.Int16:
		return "std::int16_t", nil
	case reflect.Uint16:
		return "std::uint16_t", nil
	case reflect.Int32:
		return "std::int32_t", nil
	case reflect.Uint32:
		return "std::uint32_t", nil
	case reflect.Int64:
		return "std::int64_t", nil
	case reflect.Uint64:
		return "std::uint64_t", nil
	case reflect.Float32:
		return "float", nil
	case reflect.Float64:
		return "double", nil
	case reflect.String:
		return "std::string", nil
	case reflect.Slice:
		elem, err := cppName(typ.Elem())
		if err != nil {
			return "", err
		}
		return "std::vector<" + elem + ">", nil
	case reflect.Array:
		elem, err := cppName(typ.Elem())
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("std::array<%s,%d>", elem, typ.Len()), nil
	case reflect.Struct:
		name := typ.Name()
		if name == "" {
			return "", fmt.Errorf("a struct with no name has no C++ type to record")
		}
		return name, nil
	}
	return "", fmt.Errorf("nothing is known about how to store a %s", typ)
}

// ---------------------------------------------------------------- fields

// fieldWriter appends one field's part of an entry to the column buffers.
type fieldWriter interface {
	// bind ties the writer to the value it reads each entry from. Only the
	// top-level writers are bound; the ones under them are handed the
	// value to write.
	bind(v reflect.Value)

	// write appends v.
	write(v reflect.Value) error

	// value returns what this writer was bound to.
	value() reflect.Value
}

type bound struct{ v reflect.Value }

func (b *bound) bind(v reflect.Value) { b.v = v }
func (b *bound) value() reflect.Value { return b.v }

// scalarWriter writes a field backed by a single column.
type scalarWriter struct {
	bound
	buf  *colBuf
	kind reflect.Kind
}

func (s *scalarWriter) write(v reflect.Value) error {
	b := s.buf
	switch s.kind {
	case reflect.Bool:
		b.bits = append(b.bits, v.Bool())
	case reflect.Int8:
		b.data = append(b.data, byte(int8(v.Int())))
	case reflect.Uint8:
		b.data = append(b.data, byte(v.Uint()))
	case reflect.Int16:
		b.data = binary.LittleEndian.AppendUint16(b.data, uint16(int16(v.Int())))
	case reflect.Uint16:
		b.data = binary.LittleEndian.AppendUint16(b.data, uint16(v.Uint()))
	case reflect.Int32:
		b.data = binary.LittleEndian.AppendUint32(b.data, uint32(int32(v.Int())))
	case reflect.Uint32:
		b.data = binary.LittleEndian.AppendUint32(b.data, uint32(v.Uint()))
	case reflect.Int64:
		b.data = binary.LittleEndian.AppendUint64(b.data, uint64(v.Int()))
	case reflect.Uint64:
		b.data = binary.LittleEndian.AppendUint64(b.data, v.Uint())
	case reflect.Float32:
		b.data = binary.LittleEndian.AppendUint32(b.data, math.Float32bits(float32(v.Float())))
	case reflect.Float64:
		b.data = binary.LittleEndian.AppendUint64(b.data, math.Float64bits(v.Float()))
	default:
		return fmt.Errorf("rntup: cannot write a %s", s.kind)
	}
	b.n++
	return nil
}

// stringWriter writes a string as its characters and where each one ends.
type stringWriter struct {
	bound
	index *colBuf
	chars *colBuf
}

func (s *stringWriter) write(v reflect.Value) error {
	str := v.String()
	s.chars.data = append(s.chars.data, str...)
	s.chars.n += uint64(len(str))

	s.index.running += uint64(len(str))
	s.index.data = binary.LittleEndian.AppendUint64(s.index.data, s.index.running)
	s.index.n++
	return nil
}

// sliceWriter writes a collection as where each one ends, and its elements.
type sliceWriter struct {
	bound
	index *colBuf
	elem  fieldWriter
}

func (s *sliceWriter) write(v reflect.Value) error {
	n := v.Len()
	for i := range n {
		if err := s.elem.write(v.Index(i)); err != nil {
			return err
		}
	}

	s.index.running += uint64(n)
	s.index.data = binary.LittleEndian.AppendUint64(s.index.data, s.index.running)
	s.index.n++
	return nil
}

// arrayWriter writes a fixed-size array, whose elements follow one another
// with nothing to mark them out.
type arrayWriter struct {
	bound
	n    int
	elem fieldWriter
}

func (a *arrayWriter) write(v reflect.Value) error {
	for i := range a.n {
		if err := a.elem.write(v.Index(i)); err != nil {
			return err
		}
	}
	return nil
}

type member struct {
	index int
	w     fieldWriter
}

// recordWriter writes a struct, member by member.
type recordWriter struct {
	bound
	members []member
}

func (r *recordWriter) write(v reflect.Value) error {
	for _, m := range r.members {
		if err := m.w.write(v.Field(m.index)); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- writing

// Write appends one entry, taking each field from the value it was bound to.
func (w *Writer) Write() error {
	if w.closed {
		return fmt.Errorf("rntup: %q is closed", w.name)
	}

	for i, f := range w.fields {
		if err := f.write(f.value()); err != nil {
			return fmt.Errorf("rntup: could not write field %q: %w", w.schema.Fields[topLevelID(w.schema, i)].Name, err)
		}
	}

	w.entries++
	w.inClust++

	if w.inClust >= w.cfg.entries {
		return w.flush()
	}
	return nil
}

// topLevelID returns the field ID of the i-th top-level field.
func topLevelID(s *Schema, i int) int {
	tops := s.TopLevel()
	if i < len(tops) {
		return tops[i]
	}
	return 0
}

// Close writes out whatever is still gathered, along with the footer and the
// anchor that points at it all.
func (w *Writer) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true

	if w.inClust > 0 {
		if err := w.flush(); err != nil {
			return err
		}
	}

	if err := w.writeFooter(); err != nil {
		return err
	}

	if w.own {
		if err := w.f.Close(); err != nil {
			return fmt.Errorf("rntup: could not close the file: %w", err)
		}
	}
	return nil
}

// flush writes the cluster that has been gathered: a page for each column,
// and a summary of what the cluster holds.
func (w *Writer) flush() error {
	cl := Cluster{
		FirstEntry: w.entries - w.inClust,
		NEntries:   w.inClust,
	}

	for _, cb := range w.cols {
		pages, err := w.writePages(cb)
		if err != nil {
			return err
		}
		cl.Columns = append(cl.Columns, ColumnPages{
			Pages:     pages,
			FirstElem: int64(cb.first),
		})

		cb.first += cb.n
		cb.n = 0
		cb.data = cb.data[:0]
		cb.bits = cb.bits[:0]
		cb.running = 0
	}

	w.clusters = append(w.clusters, cl)
	w.inClust = 0
	return nil
}

// writePages writes a column's elements for one cluster.
//
// Everything a cluster holds for a column goes in one page: a page is the
// unit a reader decodes whole, and there is nothing to be gained from
// splitting one until the encodings that work page by page are used.
func (w *Writer) writePages(cb *colBuf) ([]Page, error) {
	if cb.n == 0 {
		return nil, nil
	}

	data := cb.data
	if cb.col.Type == ColBit {
		data = packBits(cb.bits)
	}

	data, err := encodePage(cb.col, data, int(cb.n))
	if err != nil {
		return nil, err
	}

	off, nbytes, err := w.writeBlock(data)
	if err != nil {
		return nil, fmt.Errorf("rntup: could not write a page of column %d: %w", cb.col.ID, err)
	}

	return []Page{{
		NElements: int32(cb.n),
		Loc:       locator{size: nbytes, offset: off},
	}}, nil
}

// writeBlock compresses a block if that makes it smaller and writes it,
// giving back where it landed and how much room it took.
//
// A block that did not shrink is written as it stands, which is exactly what
// tells a reader to take it as it finds it: the format says a block whose
// compressed size equals its uncompressed size holds the bytes themselves.
func (w *Writer) writeBlock(data []byte) (offset uint64, nbytes int64, err error) {
	out := data
	if w.cfg.compr != 0 {
		z, err := rcompress.Compress(nil, data, w.cfg.compr)
		if err != nil {
			return 0, 0, fmt.Errorf("rntup: could not compress a block: %w", err)
		}
		if len(z) < len(data) {
			out = z
		}
	}

	off, err := riofs.WriteBlob(w.f, out)
	if err != nil {
		return 0, 0, err
	}
	return uint64(off), int64(len(out)), nil
}

// packBits puts a boolean column's values one to a bit, least significant
// first, which is how they are stored.
func packBits(bits []bool) []byte {
	out := make([]byte, (len(bits)+7)/8)
	for i, b := range bits {
		if b {
			out[i/8] |= 1 << (i % 8)
		}
	}
	return out
}

// ---------------------------------------------------------------- envelopes

func (w *Writer) writeHeader() error {
	var b wbuf

	b.u64(0) // feature flags: nothing that would stop an older reader
	b.str(w.schema.Name)
	b.str(w.schema.Description)
	b.str(w.schema.Writer)

	w.writeSchemaDescription(&b, true)

	env := envelope(envHeader, b.bytes())
	off, nbytes, err := w.writeBlock(env)
	if err != nil {
		return fmt.Errorf("rntup: could not write the header: %w", err)
	}

	w.hdr = envelopeLink{
		length: uint64(len(env)),
		loc:    locator{size: nbytes, offset: off},
	}
	// the checksum the envelope ends with, which the footer and the page
	// list both repeat so that a reader can tell they belong together.
	w.hdrSum = xxh3.Hash(env[:len(env)-checksumLen])
	return nil
}

// writeSchemaDescription writes the four lists that describe the fields and
// the columns. They end the header, and the footer carries an empty set of
// them for the schema this one did not grow.
func (w *Writer) writeSchemaDescription(b *wbuf, full bool) {
	var (
		fields  []Field
		columns []Column
	)
	if full {
		fields = w.schema.Fields
		columns = w.schema.Columns
	}

	mark := b.list(len(fields))
	for i := range fields {
		f := &fields[i]
		rec := b.record()
		b.u32(f.Version)
		b.u32(f.TypeVersion)
		b.u32(uint32(f.Parent))
		b.u16(uint16(f.Role))
		b.u16(f.Flags)
		b.str(f.Name)
		b.str(f.Type)
		b.str(f.TypeAlias)
		b.str(f.Description)
		if f.Flags&fieldRepetitive != 0 {
			b.u64(f.ArraySize)
		}
		b.close(rec)
	}
	b.close(mark)

	mark = b.list(len(columns))
	for i := range columns {
		c := &columns[i]
		rec := b.record()
		b.u16(uint16(c.Type))
		b.u16(c.Bits)
		b.u32(uint32(c.Field))
		b.u16(c.Flags)
		b.u16(c.RepIdx)
		b.close(rec)
	}
	b.close(mark)

	// no alias columns and no extra type information: nothing here is
	// projected, and nothing is written through the ROOT streamer.
	b.close(b.list(0))
	b.close(b.list(0))
}

func (w *Writer) writeFooter() error {
	pl, err := w.writePageList()
	if err != nil {
		return err
	}

	var b wbuf
	b.u64(0) // feature flags

	// the checksum of the header, so that a reader can tell the two belong
	// together.
	b.u64(w.hdrSum)

	// the schema extension, which is empty: nothing was added to the
	// schema after the header was written.
	ext := b.record()
	w.writeSchemaDescription(&b, false)
	b.close(ext)

	// one cluster group, holding every cluster.
	mark := b.list(1)
	rec := b.record()
	b.u64(0)         // the lowest entry any of its clusters holds
	b.u64(w.entries) // how many entries it covers
	b.u32(uint32(len(w.clusters)))
	b.envelopeLink(pl.length, pl.loc.size, pl.loc.offset)
	b.close(rec)
	b.close(mark)

	// the list of linked attribute sets that a 1.1.0.0 footer ends with is
	// left off: what is written here says it is 1.0.0.0, and a reader of
	// that version takes what follows the cluster groups for the
	// checksum.
	env := envelope(envFooter, b.bytes())
	off, nbytes, err := w.writeBlock(env)
	if err != nil {
		return fmt.Errorf("rntup: could not write the footer: %w", err)
	}

	return w.writeAnchor(envelopeLink{
		length: uint64(len(env)),
		loc:    locator{size: nbytes, offset: off},
	})
}

// writePageList writes where every page of every cluster is.
func (w *Writer) writePageList() (envelopeLink, error) {
	var b wbuf

	// the checksum of the header, again.
	b.u64(w.hdrSum)

	// the cluster summaries.
	mark := b.list(len(w.clusters))
	for _, cl := range w.clusters {
		rec := b.record()
		b.u64(cl.FirstEntry)
		b.u64(cl.NEntries) // the flags, in the top byte, are all zero
		b.close(rec)
	}
	b.close(mark)

	// and where the pages are: a list of clusters, of columns, of pages.
	top := b.list(len(w.clusters))
	for _, cl := range w.clusters {
		outer := b.list(len(cl.Columns))
		for _, col := range cl.Columns {
			inner := b.list(len(col.Pages))
			for _, pg := range col.Pages {
				b.i32(pg.NElements)
				b.locator(pg.Loc.size, pg.Loc.offset)
			}
			// the element offset and the compression settings sit
			// inside the frame, after the pages.
			b.i64(col.FirstElem)
			b.u32(uint32(w.cfg.compr))
			b.close(inner)
		}
		b.close(outer)
	}
	b.close(top)

	env := envelope(envPageList, b.bytes())
	off, nbytes, err := w.writeBlock(env)
	if err != nil {
		return envelopeLink{}, fmt.Errorf("rntup: could not write the page list: %w", err)
	}

	return envelopeLink{
		length: uint64(len(env)),
		loc:    locator{size: nbytes, offset: off},
	}, nil
}

// writeAnchor writes the object a ROOT file holds to point at everything
// else, which is the only part of an RNTuple a directory lists.
func (w *Writer) writeAnchor(ftr envelopeLink) error {
	a := &Anchor{
		VersionEpoch: 1,
		SeekHeader:   w.hdr.loc.offset,
		NBytesHeader: uint64(w.hdr.loc.size),
		LenHeader:    w.hdr.length,
		SeekFooter:   ftr.loc.offset,
		NBytesFooter: uint64(ftr.loc.size),
		LenFooter:    ftr.length,
		MaxKeySize:   1 << 30,
		name:         w.name,
	}

	err := w.f.Put(w.name, a)
	if err != nil {
		return fmt.Errorf("rntup: could not write the anchor of %q: %w", w.name, err)
	}
	return nil
}
