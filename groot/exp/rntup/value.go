// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rntup

import (
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"strings"
)

// fieldReader reads one field of the schema into a Go value.
//
// Every index a reader is handed is cluster-local, counted in elements of
// the field's own columns, which is how the offsets stored in the index
// columns are counted too.
type fieldReader interface {
	// rtype returns the Go type the field reads into.
	rtype() reflect.Type

	// read fills v, which is of the type rtype returns, with element i of
	// the field in the given cluster.
	read(cs *clusterState, i uint64, v reflect.Value) error
}

// goType returns the Go type a column's elements read into.
func goType(c *Column) (reflect.Type, error) {
	switch c.Type {
	case ColBit:
		return reflect.TypeFor[bool](), nil
	case ColByte, ColUInt8:
		return reflect.TypeFor[uint8](), nil
	case ColChar, ColInt8:
		return reflect.TypeFor[int8](), nil
	case ColInt16, ColSplitInt16:
		return reflect.TypeFor[int16](), nil
	case ColUInt16, ColSplitUInt16:
		return reflect.TypeFor[uint16](), nil
	case ColInt32, ColSplitInt32:
		return reflect.TypeFor[int32](), nil
	case ColUInt32, ColSplitUInt32, ColIndex32, ColSplitIndex32:
		return reflect.TypeFor[uint32](), nil
	case ColInt64, ColSplitInt64:
		return reflect.TypeFor[int64](), nil
	case ColUInt64, ColSplitUInt64, ColIndex64, ColSplitIndex64:
		return reflect.TypeFor[uint64](), nil
	case ColReal16, ColSplitReal16, ColReal32, ColSplitReal32,
		ColReal32Trunc, ColReal32Quant:
		return reflect.TypeFor[float32](), nil
	case ColReal64, ColSplitReal64:
		return reflect.TypeFor[float64](), nil
	default:
		return nil, fmt.Errorf("rntup: no Go type for a %v column", c.Type)
	}
}

// scalar reads a field backed by a single column.
type scalar struct {
	slot []int // one column per representation
	col  *Column
	typ  reflect.Type
}

func (s *scalar) rtype() reflect.Type { return s.typ }

func (s *scalar) read(cs *clusterState, i uint64, v reflect.Value) error {
	cur, err := cs.cursor(s.slot)
	if err != nil {
		return err
	}
	p, err := cur.at(i)
	if err != nil {
		return err
	}

	switch cur.col.Type {
	case ColBit:
		v.SetBool(p[0] != 0)
	case ColReal16, ColSplitReal16:
		v.SetFloat(float64(halfToFloat(binary.LittleEndian.Uint16(p))))
	default:
		switch s.typ.Kind() {
		case reflect.Int8:
			v.SetInt(int64(int8(p[0])))
		case reflect.Uint8:
			v.SetUint(uint64(p[0]))
		case reflect.Int16:
			v.SetInt(int64(int16(binary.LittleEndian.Uint16(p))))
		case reflect.Uint16:
			v.SetUint(uint64(binary.LittleEndian.Uint16(p)))
		case reflect.Int32:
			v.SetInt(int64(int32(binary.LittleEndian.Uint32(p))))
		case reflect.Uint32:
			v.SetUint(uint64(binary.LittleEndian.Uint32(p)))
		case reflect.Int64:
			v.SetInt(int64(binary.LittleEndian.Uint64(p)))
		case reflect.Uint64:
			v.SetUint(binary.LittleEndian.Uint64(p))
		case reflect.Float32:
			v.SetFloat(float64(math.Float32frombits(binary.LittleEndian.Uint32(p))))
		case reflect.Float64:
			v.SetFloat(math.Float64frombits(binary.LittleEndian.Uint64(p)))
		default:
			return fmt.Errorf("rntup: cannot read a %v column into a %v", cur.col.Type, s.typ)
		}
	}
	return nil
}

// halfToFloat widens an IEEE-754 half precision float, which Go has no type
// for, into a single precision one.
func halfToFloat(h uint16) float32 {
	var (
		sign = uint32(h>>15) << 31
		exp  = uint32(h>>10) & 0x1f
		frac = uint32(h) & 0x3ff
	)
	switch {
	case exp == 0:
		if frac == 0 {
			return math.Float32frombits(sign)
		}
		// subnormal: renormalize into the wider exponent.
		e := uint32(1)
		for frac&0x400 == 0 {
			frac <<= 1
			e++
		}
		frac &= 0x3ff
		return math.Float32frombits(sign | (127-15-e+1)<<23 | frac<<13)
	case exp == 0x1f:
		// infinity or not-a-number.
		return math.Float32frombits(sign | 0xff<<23 | frac<<13)
	default:
		return math.Float32frombits(sign | (exp+127-15)<<23 | frac<<13)
	}
}

// offsets reads the cluster-local range an index column marks out for
// element i: the element before it ends where this one begins.
func offsets(cs *clusterState, slot []int, i uint64) (beg, end uint64, err error) {
	cur, err := cs.cursor(slot)
	if err != nil {
		return 0, 0, err
	}

	read := func(j uint64) (uint64, error) {
		p, err := cur.at(j)
		if err != nil {
			return 0, err
		}
		switch cur.width {
		case 4:
			return uint64(binary.LittleEndian.Uint32(p)), nil
		case 8:
			return binary.LittleEndian.Uint64(p), nil
		default:
			return 0, fmt.Errorf("rntup: index column %d has %d byte elements", cur.col.ID, cur.width)
		}
	}

	if i > 0 {
		beg, err = read(i - 1)
		if err != nil {
			return 0, 0, err
		}
	}
	end, err = read(i)
	if err != nil {
		return 0, 0, err
	}
	if end < beg {
		return 0, 0, fmt.Errorf("rntup: index column %d runs backwards at element %d", cur.col.ID, i)
	}
	return beg, end, nil
}

// str reads a std::string, held as an index column of offsets into a column
// of characters.
type str struct {
	index []int // the columns of offsets, one per representation
	chars []int // the columns of characters
}

func (*str) rtype() reflect.Type { return reflect.TypeFor[string]() }

func (s *str) read(cs *clusterState, i uint64, v reflect.Value) error {
	beg, end, err := offsets(cs, s.index, i)
	if err != nil {
		return err
	}

	cur, err := cs.cursor(s.chars)
	if err != nil {
		return err
	}

	var sb strings.Builder
	sb.Grow(int(end - beg))
	for j := beg; j < end; j++ {
		p, err := cur.at(j)
		if err != nil {
			return err
		}
		sb.WriteByte(p[0])
	}
	v.SetString(sb.String())
	return nil
}

// slice reads a collection, held as an index column of offsets into the
// columns of the element field.
type slice struct {
	index []int
	elem  fieldReader
	typ   reflect.Type
}

func (s *slice) rtype() reflect.Type { return s.typ }

func (s *slice) read(cs *clusterState, i uint64, v reflect.Value) error {
	beg, end, err := offsets(cs, s.index, i)
	if err != nil {
		return err
	}

	n := int(end - beg)
	if v.Cap() < n {
		v.Set(reflect.MakeSlice(s.typ, n, n))
	} else {
		v.Set(v.Slice(0, n))
	}

	for k := range n {
		err := s.elem.read(cs, beg+uint64(k), v.Index(k))
		if err != nil {
			return err
		}
	}
	return nil
}

// array reads a fixed-size array, whose elements follow one another in the
// element field with no offsets to mark them out.
type array struct {
	n    uint64
	elem fieldReader
	typ  reflect.Type
}

func (a *array) rtype() reflect.Type { return a.typ }

func (a *array) read(cs *clusterState, i uint64, v reflect.Value) error {
	for k := range a.n {
		err := a.elem.read(cs, i*a.n+k, v.Index(int(k)))
		if err != nil {
			return err
		}
	}
	return nil
}

// record reads a struct, whose members share the element index of the
// struct itself.
type record struct {
	members []fieldReader
	typ     reflect.Type
}

func (r *record) rtype() reflect.Type { return r.typ }

func (r *record) read(cs *clusterState, i uint64, v reflect.Value) error {
	for k, m := range r.members {
		err := m.read(cs, i, v.Field(k))
		if err != nil {
			return err
		}
	}
	return nil
}

// fieldReader builds the reader for a field and everything under it.
func (r *Reader) fieldReader(f *Field) (fieldReader, error) {
	s := r.schema

	if f.Role == RoleStreamer {
		return nil, fmt.Errorf(
			"rntup: field %q holds objects written with the ROOT streamer, which is not supported yet",
			f.Name,
		)
	}

	// a fixed-size array holds its elements either in a column of its own,
	// as std::bitset does, or in a subfield.
	if f.Repetitive() {
		elem, err := r.arrayElem(f)
		if err != nil {
			return nil, err
		}
		if f.ArraySize > uint64(maxArrayLen) {
			return nil, fmt.Errorf("rntup: field %q is an array of %d elements", f.Name, f.ArraySize)
		}
		return &array{
			n:    f.ArraySize,
			elem: elem,
			typ:  reflect.ArrayOf(int(f.ArraySize), elem.rtype()),
		}, nil
	}

	switch f.Role {
	case RoleVariant:
		if f.nslots() != 1 {
			return nil, fmt.Errorf("rntup: variant %q has %d columns, want 1", f.Name, f.nslots())
		}
		if col := s.Columns[f.Reps[0][0]]; col.Type != ColSwitch {
			return nil, fmt.Errorf("rntup: variant %q is backed by a %v column, want Switch", f.Name, col.Type)
		}
		va := &variant{slot: f.slot(0)}
		for _, id := range f.Children {
			alt, err := r.fieldReader(s.Field(id))
			if err != nil {
				return nil, err
			}
			va.alts = append(va.alts, alt)
		}
		return va, nil

	case RoleCollection:
		if f.nslots() != 1 {
			return nil, fmt.Errorf("rntup: collection %q has %d columns, want 1", f.Name, f.nslots())
		}
		if len(f.Children) != 1 {
			return nil, fmt.Errorf("rntup: collection %q has %d subfields, want 1", f.Name, len(f.Children))
		}
		elem, err := r.fieldReader(s.Field(f.Children[0]))
		if err != nil {
			return nil, err
		}
		return &slice{
			index: f.slot(0),
			elem:  elem,
			typ:   reflect.SliceOf(elem.rtype()),
		}, nil

	case RoleRecord:
		return r.recordReader(f)
	}

	// a plain field: a scalar, a string, or a wrapper around one subfield,
	// which is how an enum is held.
	switch {
	case f.nslots() == 2 && isIndex(s, f.Reps[0][0]) && isChar(s, f.Reps[0][1]):
		return &str{index: f.slot(0), chars: f.slot(1)}, nil

	case f.nslots() == 1:
		col := &s.Columns[f.Reps[0][0]]
		typ, err := goType(col)
		if err != nil {
			return nil, fmt.Errorf("rntup: field %q: %w", f.Name, err)
		}
		return &scalar{slot: f.slot(0), col: col, typ: typ}, nil

	case f.nslots() == 0 && len(f.Children) == 1:
		return r.fieldReader(s.Field(f.Children[0]))

	case f.nslots() == 0 && len(f.Children) > 1:
		return r.recordReader(f)

	default:
		return nil, fmt.Errorf(
			"rntup: cannot read field %q of type %q: %d columns, %d subfields",
			f.Name, f.Type, f.nslots(), len(f.Children),
		)
	}
}

// maxArrayLen caps how big a fixed-size array may be, so that a corrupt
// size does not ask Go for an unbuildable type.
const maxArrayLen = 1 << 20

// arrayElem returns the reader for the elements of a fixed-size array.
func (r *Reader) arrayElem(f *Field) (fieldReader, error) {
	s := r.schema
	switch {
	case len(f.Children) == 1:
		return r.fieldReader(s.Field(f.Children[0]))
	case f.nslots() == 1:
		// std::bitset and friends keep their elements in a column of
		// the array field itself.
		col := &s.Columns[f.Reps[0][0]]
		typ, err := goType(col)
		if err != nil {
			return nil, fmt.Errorf("rntup: field %q: %w", f.Name, err)
		}
		return &scalar{slot: f.slot(0), col: col, typ: typ}, nil
	default:
		return nil, fmt.Errorf(
			"rntup: cannot read the elements of array field %q: %d columns, %d subfields",
			f.Name, f.nslots(), len(f.Children),
		)
	}
}

// recordReader builds the reader for a struct-like field.
func (r *Reader) recordReader(f *Field) (fieldReader, error) {
	if f.nslots() != 0 {
		return nil, fmt.Errorf("rntup: record %q has %d columns, want none", f.Name, f.nslots())
	}

	var (
		rec    record
		fields []reflect.StructField
	)
	for _, id := range f.Children {
		sub := r.schema.Field(id)
		m, err := r.fieldReader(sub)
		if err != nil {
			return nil, err
		}
		rec.members = append(rec.members, m)
		fields = append(fields, reflect.StructField{
			Name: exported(sub.Name),
			Type: m.rtype(),
			Tag:  reflect.StructTag(fmt.Sprintf("rntup:%q", sub.Name)),
		})
	}

	rec.typ = reflect.StructOf(fields)
	return &rec, nil
}

// exported turns a C++ member name into one Go will accept on a struct.
func exported(name string) string {
	var sb strings.Builder
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			sb.WriteRune(r)
		case r >= '0' && r <= '9' && i > 0:
			sb.WriteRune(r)
		default:
			sb.WriteByte('_')
		}
	}
	out := sb.String()
	if out == "" {
		return "X"
	}
	if c := out[0]; c >= 'a' && c <= 'z' {
		return strings.ToUpper(out[:1]) + out[1:]
	}
	if c := out[0]; !(c >= 'A' && c <= 'Z') {
		return "X" + out
	}
	return out
}

func isIndex(s *Schema, id int) bool {
	return id >= 0 && id < len(s.Columns) && s.Columns[id].Type.index()
}

func isChar(s *Schema, id int) bool {
	if id < 0 || id >= len(s.Columns) {
		return false
	}
	switch s.Columns[id].Type {
	case ColChar, ColByte, ColInt8, ColUInt8:
		return true
	}
	return false
}

// variant reads a std::variant, whose Switch column says, for every entry,
// which of the alternatives is live and where its value sits.
//
// The alternatives have no common Go type, so the value is handed back as an
// any holding whichever alternative was live. A variant in the invalid
// state, holding none of them, reads as nil.
type variant struct {
	slot []int // the Switch column
	alts []fieldReader
}

func (*variant) rtype() reflect.Type { return reflect.TypeFor[any]() }

func (va *variant) read(cs *clusterState, i uint64, v reflect.Value) error {
	cur, err := cs.cursor(va.slot)
	if err != nil {
		return err
	}
	p, err := cur.at(i)
	if err != nil {
		return err
	}

	var (
		idx = binary.LittleEndian.Uint64(p[0:])
		tag = binary.LittleEndian.Uint32(p[8:])
	)
	if tag == 0 {
		// the variant holds none of its alternatives.
		v.Set(reflect.Zero(v.Type()))
		return nil
	}
	if int(tag) > len(va.alts) {
		return fmt.Errorf("rntup: variant dispatch tag %d, but it has %d alternatives", tag, len(va.alts))
	}

	// the tag counts from one; the index is the element's own position in
	// the alternative's columns, not an offset just past it.
	alt := va.alts[tag-1]

	out := reflect.New(alt.rtype()).Elem()
	err = alt.read(cs, idx, out)
	if err != nil {
		return err
	}
	v.Set(out)
	return nil
}
