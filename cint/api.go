// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cint

import (
	"fmt"
	"strings"
)

// mapping is how one ROOT call is written in Go.
type mapping struct {
	// emit writes the Go expression. recv is the receiver, empty for a
	// static or a free function, and args are the arguments already
	// translated.
	emit func(recv string, args []string) string

	// min and max are how many arguments ROOT takes; max of -1 means as
	// many as are given.
	min, max int

	// imports are the packages the Go expression needs.
	imports []string

	// result is the ROOT class the call gives back, so that what is done
	// with it afterwards can be translated too. Empty when the call gives
	// back a number, a string or nothing.
	result string

	// gotype is the Go type the call gives back, for the calls that give
	// back a number. It lets the arithmetic around them be translated
	// without guessing.
	gotype string

	// argTypes is the Go type each argument has to have. C++ promotes a
	// whole number to a real wherever one is wanted and Go does not, so
	// the conversions C++ left unwritten are written here.
	argTypes []string
}

// takes says what Go types the arguments have to have. A name left empty
// takes whatever it is given.
func (m *mapping) takes(types ...string) *mapping {
	out := *m
	out.argTypes = types
	return &out
}

// gives says what Go type the call gives back.
func (m *mapping) gives(gotype string) *mapping {
	out := *m
	out.gotype = gotype
	return &out
}

// floats is the common case: every argument is a real.
func (m *mapping) floats() *mapping {
	out := *m
	n := out.max
	if n < 0 {
		n = 8
	}
	out.argTypes = make([]string, n)
	for i := range out.argTypes {
		out.argTypes[i] = "float64"
	}
	return &out
}

// call is the ordinary case: a method of the same shape under another name.
func call(name string, min, max int, imports ...string) *mapping {
	return &mapping{
		min: min, max: max, imports: imports,
		emit: func(recv string, args []string) string {
			return fmt.Sprintf("%s.%s(%s)", recv, name, strings.Join(args, ", "))
		},
	}
}

// withDefaults is a method whose trailing arguments ROOT lets you leave out.
func withDefaults(name string, min int, dflts ...string) *mapping {
	max := min + len(dflts)
	return &mapping{
		min: min, max: max,
		emit: func(recv string, args []string) string {
			for i := len(args) - min; i < len(dflts); i++ {
				args = append(args, dflts[i])
			}
			return fmt.Sprintf("%s.%s(%s)", recv, name, strings.Join(args, ", "))
		},
	}
}

// free is a function with no receiver.
func free(name string, min, max int, imports ...string) *mapping {
	return &mapping{
		min: min, max: max, imports: imports,
		emit: func(_ string, args []string) string {
			return fmt.Sprintf("%s(%s)", name, strings.Join(args, ", "))
		},
	}
}

// returns says what class a call gives back.
func (m *mapping) returns(class string) *mapping {
	out := *m
	out.result = class
	return &out
}

// needs adds the packages a call imports.
func (m *mapping) needs(imports ...string) *mapping {
	out := *m
	out.imports = append(append([]string(nil), out.imports...), imports...)
	return &out
}

// rootType says how a ROOT class is written in Go.
type rootType struct {
	gotype  string // the Go type a variable of it has
	imports []string

	ctor    *mapping            // "new T(...)"
	statics map[string]*mapping // "T::f(...)"
	methods map[string]*mapping // "x->f(...)"
}

const (
	pkgGroot = "go-hep.org/x/hep/groot"
	pkgRiofs = "go-hep.org/x/hep/groot/riofs"
	pkgRhist = "go-hep.org/x/hep/groot/rhist"
	pkgRtree = "go-hep.org/x/hep/groot/rtree"
	pkgRdraw = "go-hep.org/x/hep/groot/rtree/rdraw"
	pkgHbook = "go-hep.org/x/hep/hbook"
	pkgRt    = "go-hep.org/x/hep/cint/rt"
	pkgFmt   = "fmt"
	pkgMath  = "math"
)

// histMethods are the calls every TH1, TH2 and TH3 answers to.
//
// The ROOT histogram classes carry the same interface in go-hep, since the
// generated types were given ROOT's own method names, so most of this is a
// rename and nothing more.
func histMethods(dim int) map[string]*mapping {
	out := map[string]*mapping{
		"GetName":        call("Name", 0, 0),
		"GetTitle":       call("Title", 0, 0),
		"SetName":        call("SetName", 1, 1),
		"SetTitle":       call("SetTitle", 1, 1),
		"GetEntries":     call("Entries", 0, 0),
		"GetNbinsX":      call("NbinsX", 0, 0),
		"Integral":       call("Integral", 0, 0),
		"Scale":          call("Scale", 1, 1),
		"Reset":          call("Reset", 0, 0),
		"GetMaximum":     call("Maximum", 0, 0),
		"GetMaximumBin":  call("MaximumBin", 0, 0),
		"GetMinimum":     call("Minimum", 0, 0),
		"Write":          {min: 0, max: 1, emit: func(recv string, _ []string) string { return "rt.Write(" + recv + ")" }, imports: []string{pkgRt}},
		"Draw":           {min: 0, max: 1, emit: func(recv string, args []string) string { return "rt.Draw(" + join(recv, args) + ")" }, imports: []string{pkgRt}},
		"SetLineColor":   ignored("SetLineColor"),
		"SetLineWidth":   ignored("SetLineWidth"),
		"SetFillColor":   ignored("SetFillColor"),
		"SetMarkerStyle": ignored("SetMarkerStyle"),
		"SetStats":       ignored("SetStats"),
		"Sumw2":          ignored("Sumw2"),
	}

	switch dim {
	case 1:
		out["Fill"] = withDefaults("Fill", 1, "1").floats().gives("float64")
		out["FindBin"] = call("FindBin", 1, 1).floats().gives("int")
		out["GetBinContent"] = call("BinContent", 1, 1).takes("int").gives("float64")
		out["SetBinContent"] = call("SetBinContent", 2, 2).takes("int", "float64")
		out["GetBinError"] = call("BinError", 1, 1).takes("int").gives("float64")
		out["SetBinError"] = call("SetBinError", 2, 2).takes("int", "float64")
		out["GetMean"] = call("Mean", 0, 0).gives("float64")
		out["GetRMS"] = call("StdDev", 0, 0).gives("float64")
		out["GetStdDev"] = call("StdDev", 0, 0).gives("float64")
	case 2:
		out["Fill"] = withDefaults("Fill", 2, "1").floats().gives("float64")
		out["GetNbinsY"] = call("NbinsY", 0, 0).gives("int")
		out["GetBinContent"] = call("BinContent", 2, 2).takes("int", "int").gives("float64")
		out["SetBinContent"] = call("SetBinContent", 3, 3).takes("int", "int", "float64")
		out["GetBinError"] = call("BinError", 2, 2).takes("int", "int").gives("float64")
		out["SetBinError"] = call("SetBinError", 3, 3).takes("int", "int", "float64")
		out["GetMean"] = call("MeanX", 0, 0).gives("float64")
		out["ProjectionX"] = withDefaults("ProjectionX", 0, `"_px"`).returns("TH1D")
		out["ProjectionY"] = withDefaults("ProjectionY", 0, `"_py"`).returns("TH1D")
	case 3:
		out["Fill"] = withDefaults("Fill", 3, "1").floats().gives("float64")
		out["GetNbinsY"] = call("NbinsY", 0, 0).gives("int")
		out["GetNbinsZ"] = call("NbinsZ", 0, 0).gives("int")
		out["GetBinContent"] = call("BinContent", 3, 3).takes("int", "int", "int").gives("float64")
		out["SetBinContent"] = call("SetBinContent", 4, 4).takes("int", "int", "int", "float64")
		out["GetMean"] = call("MeanX", 0, 0).gives("float64")
		out["ProjectionZ"] = withDefaults("ProjectionZ", 0, `"_pz"`).returns("TH1D")
	}
	return out
}

// ignored is a call that styles a plot in a way this translation has nowhere
// to put. It is dropped, and the translation says so.
func ignored(name string) *mapping {
	return &mapping{
		min: 0, max: -1,
		emit: func(recv string, args []string) string { return "" },
	}
}

func join(recv string, args []string) string {
	all := append([]string{recv}, args...)
	return strings.Join(all, ", ")
}

// histType builds the entry for one of the histogram classes.
func histType(class, gotype string, dim int) *rootType {
	return &rootType{
		gotype:  "*" + gotype,
		imports: []string{pkgRhist},
		ctor: (&mapping{
			min: 3, max: 10, imports: []string{pkgRhist},
			emit: func(_ string, args []string) string {
				return fmt.Sprintf("rhist.New%s(%s)", strings.TrimPrefix(gotype, "rhist."), strings.Join(args, ", "))
			},
			result: class,
		}).takes("string", "string",
			"int", "float64", "float64",
			"int", "float64", "float64",
			"int", "float64", "float64"),
		methods: histMethods(dim),
	}
}

// rootTypes is every ROOT class this package knows how to translate.
var rootTypes = map[string]*rootType{}

func init() {
	for _, h := range []struct {
		class, gotype string
		dim           int
	}{
		{"TH1C", "rhist.H1C", 1}, {"TH1S", "rhist.H1S", 1}, {"TH1I", "rhist.H1I", 1},
		{"TH1F", "rhist.H1F", 1}, {"TH1D", "rhist.H1D", 1},
		{"TH2C", "rhist.H2C", 2}, {"TH2S", "rhist.H2S", 2}, {"TH2I", "rhist.H2I", 2},
		{"TH2F", "rhist.H2F", 2}, {"TH2D", "rhist.H2D", 2},
		{"TH3C", "rhist.H3C", 3}, {"TH3S", "rhist.H3S", 3}, {"TH3I", "rhist.H3I", 3},
		{"TH3F", "rhist.H3F", 3}, {"TH3D", "rhist.H3D", 3},
	} {
		rootTypes[h.class] = histType(h.class, h.gotype, h.dim)
	}

	rootTypes["TFile"] = &rootType{
		gotype:  "*riofs.File",
		imports: []string{pkgRiofs},
		ctor: &mapping{
			min: 1, max: 3, imports: []string{pkgRt, pkgGroot},
			emit: func(_ string, args []string) string {
				mode := `""`
				if len(args) > 1 {
					mode = args[1]
				}
				return fmt.Sprintf("rt.OpenFile(%s, %s)", args[0], mode)
			},
			result: "TFile",
		},
		statics: map[string]*mapping{
			"Open": {
				min: 1, max: 3, imports: []string{pkgRt, pkgGroot},
				emit: func(_ string, args []string) string {
					mode := `""`
					if len(args) > 1 {
						mode = args[1]
					}
					return fmt.Sprintf("rt.OpenFile(%s, %s)", args[0], mode)
				},
				result: "TFile",
			},
		},
		methods: map[string]*mapping{
			"Get":      call("Get", 1, 1).needs(pkgRt),
			"Close":    {min: 0, max: 1, emit: func(recv string, _ []string) string { return recv + ".Close()" }},
			"Write":    {min: 0, max: 3, emit: func(recv string, _ []string) string { return "rt.WriteFile(" + recv + ")" }, imports: []string{pkgRt}},
			"cd":       ignored("cd"),
			"GetName":  call("Name", 0, 0),
			"IsZombie": {min: 0, max: 0, emit: func(recv string, _ []string) string { return "(" + recv + " == nil)" }},
		},
	}

	rootTypes["TTree"] = &rootType{
		gotype:  "rtree.Tree",
		imports: []string{pkgRtree},
		methods: map[string]*mapping{
			"GetEntries": call("Entries", 0, 0).gives("int64"),
			"GetName":    call("Name", 0, 0).gives("string"),
			"GetTitle":   call("Title", 0, 0).gives("string"),
			"Draw": {
				min: 1, max: 3, imports: []string{pkgRt, pkgRdraw},
				emit: func(recv string, args []string) string {
					cut := `""`
					if len(args) > 1 {
						cut = args[1]
					}
					return fmt.Sprintf("rt.TreeDraw(%s, %s, %s)", recv, args[0], cut)
				},
				result: "TH1D",
			},
			"Scan": {
				min: 0, max: 3, imports: []string{pkgRt},
				emit: func(recv string, args []string) string {
					e := `""`
					if len(args) > 0 {
						e = args[0]
					}
					return fmt.Sprintf("rt.TreeScan(%s, %s)", recv, e)
				},
			},
		},
	}
	rootTypes["TNtuple"] = rootTypes["TTree"]
	rootTypes["TChain"] = rootTypes["TTree"]

	rootTypes["TF1"] = &rootType{
		gotype:  "*rhist.F1",
		imports: []string{pkgRhist},
		ctor: &mapping{
			min: 2, max: 5, imports: []string{pkgRt, pkgRhist},
			emit: func(_ string, args []string) string {
				lo, hi := "0", "1"
				if len(args) > 3 {
					lo, hi = args[2], args[3]
				}
				return fmt.Sprintf("rt.NewF1(%s, %s, %s, %s)", args[0], args[1], lo, hi)
			},
			result: "TF1",
		},
		methods: map[string]*mapping{
			"Eval":         {min: 1, max: 1, imports: []string{pkgRt}, emit: func(recv string, args []string) string { return fmt.Sprintf("rt.Eval(%s, %s)", recv, args[0]) }},
			"GetName":      call("Name", 0, 0),
			"Draw":         {min: 0, max: 1, emit: func(recv string, args []string) string { return "rt.Draw(" + join(recv, args) + ")" }, imports: []string{pkgRt}},
			"SetLineColor": ignored("SetLineColor"),
		},
	}

	rootTypes["TCanvas"] = &rootType{
		gotype:  "*rt.Canvas",
		imports: []string{pkgRt},
		ctor: &mapping{
			min: 0, max: 5, imports: []string{pkgRt},
			emit: func(_ string, args []string) string {
				name := `"c"`
				if len(args) > 0 {
					name = args[0]
				}
				w, h := "800", "600"
				if len(args) >= 4 {
					w, h = args[2], args[3]
				}
				return fmt.Sprintf("rt.NewCanvas(%s, %s, %s)", name, w, h)
			},
			result: "TCanvas",
		},
		methods: map[string]*mapping{
			"SaveAs":  call("SaveAs", 1, 1).takes("string"),
			"Print":   call("SaveAs", 1, 1).takes("string"),
			"Divide":  call("Divide", 1, 2).takes("int", "int"),
			"cd":      withDefaults("Cd", 0, "1").takes("int"),
			"Update":  ignored("Update"),
			"Clear":   call("Clear", 0, 0),
			"SetLogy": ignored("SetLogy"),
			"SetGrid": ignored("SetGrid"),
		},
	}

	rootTypes["TRandom"] = randomType()
	rootTypes["TRandom1"] = rootTypes["TRandom"]
	rootTypes["TRandom2"] = rootTypes["TRandom"]
	rootTypes["TRandom3"] = rootTypes["TRandom"]

	rootTypes["TGraph"] = &rootType{
		gotype:  "*rt.Graph",
		imports: []string{pkgRt},
		ctor: &mapping{
			min: 0, max: 4, imports: []string{pkgRt},
			emit:   func(_ string, args []string) string { return "rt.NewGraph()" },
			result: "TGraph",
		},
		methods: map[string]*mapping{
			"SetPoint":       call("SetPoint", 3, 3),
			"AddPoint":       call("AddPoint", 2, 2),
			"GetN":           call("N", 0, 0),
			"Draw":           {min: 0, max: 1, emit: func(recv string, args []string) string { return "rt.Draw(" + join(recv, args) + ")" }, imports: []string{pkgRt}},
			"SetTitle":       call("SetTitle", 1, 1),
			"SetMarkerStyle": ignored("SetMarkerStyle"),
		},
	}
}

func randomType() *rootType {
	return &rootType{
		gotype:  "*rt.Random",
		imports: []string{pkgRt},
		ctor: &mapping{
			min: 0, max: 1, imports: []string{pkgRt},
			emit: func(_ string, args []string) string {
				seed := "0"
				if len(args) > 0 {
					seed = args[0]
				}
				return fmt.Sprintf("rt.NewRandom(%s)", seed)
			},
			result: "TRandom",
		},
		methods: map[string]*mapping{
			"Gaus":    withDefaults("Gaus", 0, "0", "1").floats().gives("float64"),
			"Rndm":    call("Rndm", 0, 1).gives("float64"),
			"Uniform": withDefaults("Uniform", 0, "0", "1").floats().gives("float64"),
			"Exp":     call("Exp", 1, 1).floats().gives("float64"),
			"Poisson": call("Poisson", 1, 1).floats().gives("float64"),
			"Landau":  withDefaults("Landau", 0, "0", "1").floats().gives("float64"),
			"Integer": call("Integer", 1, 1).takes("int").gives("int"),
			"SetSeed": call("SetSeed", 1, 1).takes("uint64"),
		},
	}
}

// globals are ROOT's global objects, and what they are in a translation.
var globals = map[string]struct {
	class string
	expr  string
	pkg   string
}{
	"gRandom": {"TRandom", "rt.GRandom", pkgRt},
	"gPad":    {"TCanvas", "rt.GPad()", pkgRt},
	"gStyle":  {"TStyle", "rt.GStyle", pkgRt},
	"gFile":   {"TFile", "rt.GFile()", pkgRt},
}

// mathFuncs are TMath's, which Go's own maths library already has.
//
// Every one of them takes and gives a real, which is what lets the
// translation write the conversions C++ left unwritten.
var mathFuncs = map[string]*mapping{}

// freeFuncs are the functions a macro calls without a receiver.
var freeFuncs = map[string]*mapping{
	"printf": free("fmt.Printf", 1, -1, pkgFmt),
	"Printf": free("fmt.Printf", 1, -1, pkgFmt),
	"puts":   free("fmt.Println", 1, 1, pkgFmt),
}

func init() {
	// the one-argument functions of a real, under both the name TMath
	// gives them and the one C gives them.
	for _, fn := range []struct{ root, c, go_ string }{
		{"Abs", "fabs", "math.Abs"},
		{"Abs", "abs", "math.Abs"},
		{"Sqrt", "sqrt", "math.Sqrt"},
		{"Exp", "exp", "math.Exp"},
		{"Log", "log", "math.Log"},
		{"Log10", "log10", "math.Log10"},
		{"Log2", "", "math.Log2"},
		{"Sin", "sin", "math.Sin"},
		{"Cos", "cos", "math.Cos"},
		{"Tan", "tan", "math.Tan"},
		{"ASin", "asin", "math.Asin"},
		{"ACos", "acos", "math.Acos"},
		{"ATan", "atan", "math.Atan"},
		{"SinH", "sinh", "math.Sinh"},
		{"CosH", "cosh", "math.Cosh"},
		{"TanH", "tanh", "math.Tanh"},
		{"Floor", "floor", "math.Floor"},
		{"Ceil", "ceil", "math.Ceil"},
		{"Erf", "erf", "math.Erf"},
		{"Erfc", "erfc", "math.Erfc"},
	} {
		m := free(fn.go_, 1, 1, pkgMath).floats().gives("float64")
		if fn.root != "" {
			mathFuncs[fn.root] = m
		}
		if fn.c != "" {
			freeFuncs[fn.c] = m
		}
	}

	// and the two-argument ones.
	for _, fn := range []struct{ root, c, go_ string }{
		{"ATan2", "atan2", "math.Atan2"},
		{"Power", "pow", "math.Pow"},
		{"Hypot", "hypot", "math.Hypot"},
		{"Max", "fmax", "math.Max"},
		{"Min", "fmin", "math.Min"},
	} {
		m := free(fn.go_, 2, 2, pkgMath).floats().gives("float64")
		mathFuncs[fn.root] = m
		if fn.c != "" {
			freeFuncs[fn.c] = m
		}
	}

	mathFuncs["IsNaN"] = free("math.IsNaN", 1, 1, pkgMath).floats().gives("bool")
	mathFuncs["Sq"] = (&mapping{
		min: 1, max: 1, gotype: "float64",
		emit: func(_ string, args []string) string {
			return fmt.Sprintf("(%[1]s)*(%[1]s)", args[0])
		},
	}).floats()

	// the constants, which are named like calls in a macro and are not.
	for _, c := range []struct{ name, expr string }{
		{"Pi", "math.Pi"},
		{"TwoPi", "2*math.Pi"},
		{"PiOver2", "math.Pi/2"},
		{"PiOver4", "math.Pi/4"},
		{"E", "math.E"},
		{"Sqrt2", "math.Sqrt2"},
		{"Ln10", "math.Ln10"},
	} {
		mathFuncs[c.name] = &mapping{
			min: 0, max: 0, gotype: "float64", imports: []string{pkgMath},
			emit: func(string, []string) string { return c.expr },
		}
	}
}
