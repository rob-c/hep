// Copyright ©2015 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rio

import (
	"fmt"
	"io"
)

// Selector selects Records based on their name
type Selector struct {
	Name   string // Record name
	Unpack bool   // Whether to unpack the Record
}

// Scanner provides a convenient interface for reading records of a rio-stream.
type Scanner struct {
	r   *Reader
	err error   // first non-EOF error encountered while reading the rio-stream.
	rec *Record // last record encountered while reading the rio-stream.

	filter map[string]Selector // records to read. if nil, return everything.
}

// NewScanner returns a new Scanner to read from r.
func NewScanner(r *Reader) *Scanner {
	scan := &Scanner{
		r:      r,
		err:    nil,
		rec:    newRecord("<N/A>", 0),
		filter: make(map[string]Selector),
	}
	scan.rec.unpack = false
	scan.rec.r = r
	return scan
}

// Select sets the records selection function.
func (s *Scanner) Select(selectors []Selector) {
	s.filter = make(map[string]Selector, len(selectors))
	for _, sel := range selectors {
		s.filter[sel.Name] = sel
	}
}

// Scan scans the next Record until io.EOF
func (s *Scanner) Scan() bool {
	if s.err != nil {
		return false
	}

	for {
		var hdr rioHeader
		err := hdr.RioUnmarshal(s.r.r)
		if err != nil {
			s.err = err
			return false
		}

		switch hdr.Frame {
		case ftrFrame:
			ftr := rioFooter{Header: hdr}
			err = ftr.unmarshalData(s.r.r)
			if err != nil {
				s.err = err
				return false
			}
			continue
		case recFrame:
			s.rec.raw.Header = hdr
			err := s.rec.raw.unmarshalData(s.r.r)
			if err != nil {
				s.err = err
				return false
			}

			clen := int64(rioAlignU32(s.rec.raw.CLen))

			name := s.rec.Name()
			if len(s.filter) > 0 {
				_, ok := s.filter[name]
				if !ok {
					err = s.skip(clen)
					if err != nil {
						s.err = err
						return false
					}
					continue
				}
			}

			s.rec.unpack = s.filter[name].Unpack

			switch s.rec.unpack {

			case true:
				err = s.rec.readBlocks(s.r.r)
				if err != nil {
					s.err = err
					return false
				}
				return true

			case false:
				err = s.skip(clen)
				if err != nil {
					s.err = err
					return false
				}
				return true
			}

		default:
			// a stream that is not a rio stream, or one that has been
			// truncated or corrupted somewhere before this point.
			s.err = fmt.Errorf("rio: read header corrupted (frame=%#v)", hdr.Frame)
			return false
		}
	}
}

// skip discards the next n bytes of the rio stream, which is how a record
// that was not asked for, or one that was not asked to be unpacked, is
// stepped over.
func (s *Scanner) skip(n int64) error {
	_, err := io.CopyN(io.Discard, s.r.r, n)
	return err
}

// Err returns the first non-EOF error encountered by the reader.
func (s *Scanner) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

// Record returns the last Record read by the Scanner.
func (s *Scanner) Record() *Record {
	return s.rec
}
