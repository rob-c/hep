// Copyright ©2023 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vgop_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"runtime"
	"testing"

	"go-hep.org/x/hep/hplot"
	"go-hep.org/x/hep/hplot/vgop"
	"go-hep.org/x/hep/internal/diff"
	"gonum.org/v1/plot/font"
	"gonum.org/v1/plot/vg"
)

func TestJSON(t *testing.T) {
	sr := font.Font{Typeface: "Liberation", Variant: "Serif"}
	tr := font.From(sr, 12)
	ft13 := hplot.DefaultStyle.Fonts.Cache.Lookup(tr, 13)
	ft20 := hplot.DefaultStyle.Fonts.Cache.Lookup(tr, 20)

	c := vgop.NewJSON(vgop.WithSize(10, 20))

	c.Push()
	c.SetLineWidth(2)
	c.SetLineDash([]vg.Length{1, 2}, 4)
	c.SetColor(color.RGBA{R: 255, A: 255})
	c.SetColor(color.Gray{Y: 100})
	c.Rotate(math.Pi / 2)
	c.Translate(vg.Point{X: 10, Y: 20})
	c.Scale(15, 25)
	c.Pop()
	p0 := vg.Path(nil)
	p1 := vg.Path([]vg.PathComp{
		{Type: vg.MoveComp, Pos: vg.Point{X: 1, Y: 2}},
		{Type: vg.LineComp, Pos: vg.Point{X: 2, Y: 3}},
		{Type: vg.ArcComp, Pos: vg.Point{X: 3, Y: 4}, Radius: 5, Start: 6, Angle: 7},
		{Type: vg.CurveComp, Pos: vg.Point{X: 4, Y: 5}, Control: []vg.Point{{X: 6, Y: 7}}},
		{Type: vg.CurveComp, Pos: vg.Point{X: 5, Y: 6}, Control: []vg.Point{{X: 7, Y: 8}, {X: 9, Y: 10}}},
		{Type: vg.CloseComp},
	})
	c.Stroke(p0)
	c.Stroke(p1)
	c.Fill(p0)
	c.Fill(p1)

	c.FillString(ft13, vg.Point{X: 10, Y: 20}, "hello\nworld")
	c.FillString(ft20, vg.Point{X: 20, Y: 30}, "BYE.")

	img := image.NewRGBA(image.Rect(0, 0, 20, 30))
	draw.Draw(img, img.Rect, image.NewUniform(color.RGBA{0x66, 0x66, 0x66, 0xff}), image.Point{}, draw.Src)

	c.DrawImage(vg.Rectangle{Min: vg.Point{X: 1, Y: 2}, Max: vg.Point{X: 3, Y: 4}}, img)

	f, err := os.Create("testdata/simple.json")
	if err != nil {
		t.Fatalf("could not create output JSON file: %+v", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")

	err = enc.Encode(c)
	if err != nil {
		t.Fatalf("could not encode canvas: %+v", err)
	}

	err = f.Close()
	if err != nil {
		t.Fatalf("could not close output JSON file: %+v", err)
	}

	// The canvas holds an image, and an image is written out as a PNG.
	// The bytes a PNG encoder produces are not part of Go's compatibility
	// promise and do change between releases of it, while the picture they
	// hold does not — so the comparison is over the picture.
	if got, want := canonical(t, "testdata/simple.json"), canonical(t, "testdata/simple_golden.json"); got != want {
		t.Fatalf("JSON files differ:\n%s", diff.Format(got, want))
	}

	c = vgop.NewJSON()
	got, err := os.ReadFile("testdata/simple.json")
	if err != nil {
		t.Fatalf("could not read-back JSON file: %+v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(got))
	err = dec.Decode(c)
	if err != nil {
		t.Fatalf("could not decode JSON canvas: %+v", err)
	}

	bak := new(bytes.Buffer)
	enc = json.NewEncoder(bak)
	enc.SetIndent("", "  ")

	err = enc.Encode(c)
	if err != nil {
		t.Fatalf("could not re-encode JSON canvas: %+v", err)
	}

	if got, want := bak.String(), string(got); got != want {
		o := diff.Format(got, want)
		t.Fatalf("JSON roundtrip failed:\n%s", o)
	}

	defer os.Remove("testdata/simple.json")
}

func TestSaveJSON(t *testing.T) {
	p := hplot.New()
	p.Title.Text = "Title"
	p.X.Min = -1
	p.X.Max = +1
	p.X.Label.Text = "X"
	p.Y.Min = -10
	p.Y.Max = +10
	p.Y.Label.Text = "Y"

	err := hplot.Save(p, 10*vg.Centimeter, 20*vg.Centimeter, "testdata/plot.json")
	if err != nil {
		t.Fatalf("could not save plot to JSON: %+v", err)
	}

	err = diff.Files("testdata/plot.json", "testdata/plot_golden.json")
	if err != nil {
		fatalf := t.Fatalf
		if runtime.GOOS == "darwin" {
			// ignore errors for darwin and mac-silicon
			fatalf = t.Logf
		}
		fatalf("JSON files differ:\n%s", err)
	}

	defer os.Remove("testdata/plot.json")
}

// canonical reads a canvas written as JSON and returns it with every
// embedded image replaced by what the image holds.
//
// Everything else is compared as it was written: the operations, their
// order and their arguments are all this package's to decide. An image is
// not, beyond the pixels going in and coming out.
func canonical(t *testing.T, fname string) string {
	t.Helper()

	raw, err := os.ReadFile(fname)
	if err != nil {
		t.Fatalf("could not read %s: %+v", fname, err)
	}

	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("could not read %s as JSON: %+v", fname, err)
	}

	out, err := json.MarshalIndent(canonValue(t, v), "", "  ")
	if err != nil {
		t.Fatalf("could not write %s back as JSON: %+v", fname, err)
	}
	return string(out)
}

// canonValue walks a decoded JSON value, replacing any string that turns out
// to be an image with a description of it.
func canonValue(t *testing.T, v any) any {
	t.Helper()

	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, sub := range v {
			out[k] = canonValue(t, sub)
		}
		return out

	case []any:
		out := make([]any, len(v))
		for i, sub := range v {
			out[i] = canonValue(t, sub)
		}
		return out

	case string:
		if img, ok := decodeImage(v); ok {
			return describeImage(img)
		}
		return v
	}
	return v
}

// decodeImage says whether a string holds a PNG, and what it holds if it
// does.
//
// The canvas encodes an image as base64 and keeps it in a []byte, which the
// JSON encoder then encodes again, so what arrives here is base64 twice
// over. Rather than rely on that staying true, the layers are peeled off
// until a PNG turns up or the string stops being base64.
func decodeImage(s string) (image.Image, bool) {
	raw := []byte(s)
	for range 3 {
		if img, err := png.Decode(bytes.NewReader(raw)); err == nil {
			return img, true
		}
		next, err := base64.StdEncoding.DecodeString(string(raw))
		if err != nil {
			return nil, false
		}
		raw = next
	}
	return nil, false
}

// describeImage returns what an image holds, in a form two encoders of the
// same picture agree on: its bounds and a digest of its pixels.
func describeImage(img image.Image) string {
	var (
		b = img.Bounds()
		h = sha256.New()
	)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			fmt.Fprintf(h, "%d %d %d %d;", r, g, bl, a)
		}
	}
	return fmt.Sprintf("image %v sha256:%x", b, h.Sum(nil))
}
