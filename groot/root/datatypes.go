// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package root // import "go-hep.org/x/hep/groot/root"

import (
	"encoding/binary"
	"math"
)

// Float16 is a float32 in memory, written with a truncated mantissa.
type Float16 float32

func (Float16) DescrNPy() string { return "'f2'" }

func (f Float16) MarshalNPy(bo binary.ByteOrder) ([]byte, error) {
	var buf [2]byte
	bo.PutUint16(buf[:], float16bits(float32(f)))
	return buf[:], nil
}

func (f *Float16) UnmarshalNPy(bo binary.ByteOrder, buf []byte) error {
	*f = Float16(float16frombits(bo.Uint16(buf)))
	return nil
}

func float16bits(f float32) uint16 {
	var (
		bits = math.Float32bits(f)
		sign = uint16((bits >> 31) & 0x1)
		exp  = (bits >> 23) & 0xff
		res  = int16(exp) - 127 + 15
		fc   = uint16(bits>>13) & 0x3ff
	)
	switch {
	case exp == 0:
		res = 0
	case exp == 0xff:
		res = 0x1f
	case res > 0x1e:
		res = 0x1f
		fc = 0
	case res < 0x01:
		res = 0
		fc = 0
	}
	return (sign << 15) | uint16(res<<10) | fc
}

func float16frombits(bits uint16) float32 {
	var (
		sign = uint32((bits >> 15) & 0x1)
		exp  = (bits >> 10) & 0x1f
		res  = uint32(exp) + 127 - 15
		fc   = uint32(bits & 0x3ff)
	)
	switch {
	case exp == 0:
		res = 0
	case exp == 0x1f:
		res = 0xff
	}
	return math.Float32frombits((sign << 31) | (res << 23) | (fc << 13))

}

// Double32 is a float64 in memory, written as a float32 to disk.
type Double32 float64

func (Double32) DescrNPy() string { return "'f4'" }

func (f Double32) MarshalNPy(bo binary.ByteOrder) ([]byte, error) {
	var buf [4]byte
	bo.PutUint32(buf[:], math.Float32bits(float32(f)))
	return buf[:], nil
}

func (f *Double32) UnmarshalNPy(bo binary.ByteOrder, buf []byte) error {
	*f = Double32(math.Float32frombits(bo.Uint32(buf)))
	return nil
}
