// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rntup

import (
	"fmt"
	"reflect"
	"strings"
)

// copier moves a value of the type a field reads into over to the type the
// caller asked for.
//
// A field whose C++ type is a struct reads into a Go struct this package
// builds from the schema, which callers have no way of naming. Rather than
// make them take what they are given, a caller may bind a struct of their
// own as long as its members line up with the field's, and this is what
// bridges the two.
type copier func(dst, src reflect.Value) error

// newCopier returns a copier from src to dst, or an error saying why the
// two do not line up.
func newCopier(dst, src reflect.Type) (copier, error) {
	if dst == src {
		return func(d, s reflect.Value) error {
			d.Set(s)
			return nil
		}, nil
	}

	switch {
	case dst.Kind() == reflect.Struct && src.Kind() == reflect.Struct:
		return structCopier(dst, src)

	case dst.Kind() == reflect.Slice && src.Kind() == reflect.Slice:
		elem, err := newCopier(dst.Elem(), src.Elem())
		if err != nil {
			return nil, err
		}
		return func(d, s reflect.Value) error {
			n := s.Len()
			if d.Cap() < n {
				d.Set(reflect.MakeSlice(d.Type(), n, n))
			} else {
				d.Set(d.Slice(0, n))
			}
			for i := range n {
				err := elem(d.Index(i), s.Index(i))
				if err != nil {
					return err
				}
			}
			return nil
		}, nil

	case dst.Kind() == reflect.Array && src.Kind() == reflect.Array:
		if dst.Len() != src.Len() {
			return nil, fmt.Errorf("rntup: cannot read a %v into a %v: different lengths", src, dst)
		}
		elem, err := newCopier(dst.Elem(), src.Elem())
		if err != nil {
			return nil, err
		}
		return func(d, s reflect.Value) error {
			for i := range s.Len() {
				err := elem(d.Index(i), s.Index(i))
				if err != nil {
					return err
				}
			}
			return nil
		}, nil

	case isNumeric(dst.Kind()) && isNumeric(src.Kind()) && src.ConvertibleTo(dst):
		return func(d, s reflect.Value) error {
			d.Set(s.Convert(dst))
			return nil
		}, nil

	case src.AssignableTo(dst):
		return func(d, s reflect.Value) error {
			d.Set(s)
			return nil
		}, nil

	default:
		return nil, fmt.Errorf("rntup: cannot read a %v into a %v", src, dst)
	}
}

func isNumeric(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// structCopier lines up the members of two structs.
//
// Members are matched by name, ignoring case and underscores, so that a Go
// struct written the way Go structs are written binds to a C++ one written
// the way C++ ones are. Failing that, and only if both have the same number
// of members, they are matched in order.
func structCopier(dst, src reflect.Type) (copier, error) {
	byName := make(map[string]int, src.NumField())
	for i := range src.NumField() {
		f := src.Field(i)
		byName[normalize(f.Name)] = i
		// the tag carries the name the field has in the RNTuple, which
		// is what a caller is most likely to have written down.
		if tag, ok := f.Tag.Lookup("rntup"); ok {
			byName[normalize(tag)] = i
		}
	}

	type pair struct {
		dst, src int
		cp       copier
	}

	var pairs []pair
	for i := range dst.NumField() {
		f := dst.Field(i)
		if f.PkgPath != "" {
			continue // unexported: nothing to fill
		}

		name := f.Name
		if tag, ok := f.Tag.Lookup("rntup"); ok {
			name = tag
		}

		j, ok := byName[normalize(name)]
		if !ok {
			if dst.NumField() != src.NumField() {
				return nil, fmt.Errorf(
					"rntup: cannot read a %v into a %v: no member matches %q",
					src, dst, f.Name,
				)
			}
			// fall back on position, which is how tuples and pairs,
			// whose members are named _0 and _1, are bound.
			j = i
		}

		cp, err := newCopier(f.Type, src.Field(j).Type)
		if err != nil {
			return nil, fmt.Errorf("rntup: member %q: %w", f.Name, err)
		}
		pairs = append(pairs, pair{dst: i, src: j, cp: cp})
	}
	return func(d, s reflect.Value) error {
		for _, p := range pairs {
			err := p.cp(d.Field(p.dst), s.Field(p.src))
			if err != nil {
				return err
			}
		}
		return nil
	}, nil
}

// normalize strips what differs between a C++ member name and the Go one a
// caller is likely to have written for it.
func normalize(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "_", ""))
}
